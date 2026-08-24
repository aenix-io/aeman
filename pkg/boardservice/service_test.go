package boardservice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/ghprojects"
)

// *ghprojects.Client must satisfy Backend structurally (no boardservice import in
// ghprojects). This line is the compile-time proof.
var _ Backend = (*ghprojects.Client)(nil)

// fakeBackend implements Backend over an in-memory board, logging every call and
// mutating its board so the service's views reflect the result.
type fakeBackend struct {
	// mu guards every field: the service runs background goroutines (the
	// async title resolve, carry-over sprint writes), so the fake is hit
	// concurrently with the test's assertions.
	mu      sync.Mutex
	refs    map[string]board.Link
	b       board.Board
	log     []string
	creates []board.CreateInput
	nextID  int
}

func newFake(cards []board.Card, states map[string]board.SprintState) *fakeBackend {
	if states == nil {
		states = map[string]board.SprintState{}
	}
	return &fakeBackend{b: board.Board{ID: "B1", Number: 1, Owner: "acme", Cards: cards, SprintStates: states}}
}

func (f *fakeBackend) rec(format string, a ...any) { f.log = append(f.log, fmt.Sprintf(format, a...)) }

func (f *fakeBackend) saw(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.log {
		if e == s {
			return true
		}
	}
	return false
}

func (f *fakeBackend) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.log {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeBackend) get(itemID string) *board.Card {
	for i := range f.b.Cards {
		if f.b.Cards[i].ItemID == itemID {
			return &f.b.Cards[i]
		}
	}
	return nil
}

func (f *fakeBackend) ResolveIssueRef(_ context.Context, link board.Link) (board.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("ResolveIssueRef %s", link.URL)
	resolved, ok := f.refs[link.URL]
	if !ok {
		return link, fmt.Errorf("unresolvable ref %s", link.URL)
	}
	return resolved, nil
}

func (f *fakeBackend) LoadBoard(_ context.Context, _ string, _ int) (board.Board, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("LoadBoard")
	cards := make([]board.Card, 0, len(f.b.Cards))
	var epics []board.EpicCol
	var projects []string
	var deadlines []board.Deadline
	var processes []board.Process
	var tasks []board.Card
	seenEpic := map[string]bool{}
	projectStates := map[string]string{}
	for _, c := range f.b.Cards {
		// Mirror board.NewBoard's split for the hidden state cards, so the
		// service sees the same Projects/Epics rosters it would on a real
		// board. Sprint states stay seeded directly (tests set them without
		// state cards).
		if c.Title == board.ProcessStateTitle {
			if c.Process != "" {
				processes = append(processes, board.Process{
					Name: c.Process, Project: c.Project, Paused: c.Paused, ItemID: c.ItemID,
				})
			}
			continue
		}
		if c.Title == board.ProcessTaskTitle {
			tasks = append(tasks, c)
			continue
		}
		if c.Title == board.DeadlineStateTitle {
			if c.Week != "" {
				deadlines = append(deadlines, board.Deadline{
					Week: c.Week, Project: c.Project, ItemID: c.ItemID,
				})
			}
			continue
		}
		if c.Title == board.ProjectStateTitle {
			if c.Project != "" && projectStates[c.Project] == "" {
				projects = append(projects, c.Project)
				projectStates[c.Project] = c.ItemID
			}
			continue
		}
		if c.Title == board.EpicStateTitle {
			if k := c.Project + "\x00" + c.Epic; c.Epic != "" && !seenEpic[k] {
				seenEpic[k] = true
				epics = append(epics, board.EpicCol{
					Name: c.Epic, Project: c.Project, ItemID: c.ItemID,
				})
			}
			continue
		}
		cards = append(cards, c)
	}
	states := map[string]board.SprintState{}
	for k, v := range f.b.SprintStates {
		states[k] = v
	}
	return board.Board{ID: f.b.ID, Number: f.b.Number, Owner: f.b.Owner, Cards: cards,
		SprintStates: states, Epics: epics,
		Projects: projects, ProjectStates: projectStates, Deadlines: deadlines,
		Processes: processes, Tasks: tasks}, nil
}

func (f *fakeBackend) LoadCards(_ context.Context, _ board.Board, ids []string) ([]board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("LoadCards")
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []board.Card
	for i := range f.b.Cards {
		if want[f.b.Cards[i].ItemID] {
			out = append(out, f.b.Cards[i])
		}
	}
	return out, nil
}

func (f *fakeBackend) CreateCard(_ context.Context, _ board.Board, in board.CreateInput) (board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	card := board.Card{
		ItemID: fmt.Sprintf("new%d", f.nextID), Title: in.Title, IsDraft: true,
		Zone: in.Zone, StartDate: in.Start, Day: in.Day, SprintStart: in.SprintStart,
		Plan: in.Plan, Week: in.Week, Epic: in.Epic, Project: in.Project, Team: in.Team, ReviewOf: in.ReviewOf,
		Process: in.Process, Task: in.Task, Recurrence: in.Recurrence,
		Paused:      in.Paused,
		Description: in.Body,
		Assignees:   []string{},
	}
	if in.Assignee != "" {
		card.Assignees = []string{in.Assignee}
	}
	f.b.Cards = append(f.b.Cards, card)
	f.creates = append(f.creates, in)
	f.rec("CreateCard %s", card.ItemID)
	return card, nil
}

func (f *fakeBackend) DeleteCard(_ context.Context, _ board.Board, card board.Card) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("DeleteCard %s", card.ItemID)
	out := f.b.Cards[:0]
	for _, c := range f.b.Cards {
		if c.ItemID != card.ItemID {
			out = append(out, c)
		}
	}
	f.b.Cards = out
	return nil
}

func (f *fakeBackend) MoveCard(_ context.Context, _ board.Board, card board.Card, afterID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("MoveCard %s after=%s", card.ItemID, afterID)
	return nil
}

func (f *fakeBackend) AppendEvent(_ context.Context, _ board.Board, card board.Card, e board.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("AppendEvent %s %s %s->%s", card.ItemID, e.Kind, e.From, e.To)
	if c := f.get(card.ItemID); c != nil {
		c.Events = append(c.Events, e)
	}
	return nil
}

func (f *fakeBackend) AddNote(_ context.Context, _ board.Board, card board.Card, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("AddNote %s %s", card.ItemID, text)
	return nil
}

func (f *fakeBackend) EditNote(_ context.Context, _ board.Board, card board.Card, note board.Note, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("EditNote %s %s %s", card.ItemID, note.ID, text)
	return nil
}

func (f *fakeBackend) DeleteNote(_ context.Context, _ board.Board, card board.Card, note board.Note) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("DeleteNote %s %s", card.ItemID, note.ID)
	return nil
}

func (f *fakeBackend) SetDescription(_ context.Context, _ board.Board, card board.Card, description string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetDescription %s %s", card.ItemID, description)
	if c := f.get(card.ItemID); c != nil {
		c.Description = description
	}
	return nil
}

func (f *fakeBackend) RenameCard(_ context.Context, _ board.Board, card board.Card, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("RenameCard %s", card.ItemID)
	if c := f.get(card.ItemID); c != nil {
		c.Title = title
	}
	return nil
}

func (f *fakeBackend) SetStage(_ context.Context, _ board.Board, card board.Card, stage board.StageKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetStage %s %s", card.ItemID, stage)
	if c := f.get(card.ItemID); c != nil {
		c.Stage = stage
	}
	return nil
}

func (f *fakeBackend) SetProgress(_ context.Context, _ board.Board, card board.Card, progress int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetProgress %s %d", card.ItemID, progress)
	if c := f.get(card.ItemID); c != nil {
		c.Progress = progress
	}
	return nil
}

func (f *fakeBackend) SetZone(_ context.Context, _ board.Board, card board.Card, zone board.ZoneKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetZone %s %s", card.ItemID, zone)
	if c := f.get(card.ItemID); c != nil {
		c.Zone = zone
	}
	return nil
}

func (f *fakeBackend) SetDay(_ context.Context, _ board.Board, card board.Card, day string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetDay %s %s", card.ItemID, day)
	if c := f.get(card.ItemID); c != nil {
		c.Day = day
	}
	return nil
}

func (f *fakeBackend) SetStart(_ context.Context, _ board.Board, card board.Card, date string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetStart %s %s", card.ItemID, date)
	if c := f.get(card.ItemID); c != nil {
		c.StartDate = date
	}
	return nil
}

func (f *fakeBackend) SetSprintStart(_ context.Context, _ board.Board, card board.Card, date string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetSprintStart %s %s", card.ItemID, date)
	if c := f.get(card.ItemID); c != nil {
		c.SprintStart = date
	}
	return nil
}

func (f *fakeBackend) SetPlan(_ context.Context, _ board.Board, card board.Card, plan board.PlanBand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetPlan %s %s", card.ItemID, plan)
	if c := f.get(card.ItemID); c != nil {
		c.Plan = plan
	}
	return nil
}

func (f *fakeBackend) SetWeek(_ context.Context, _ board.Board, card board.Card, week string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetWeek %s %s", card.ItemID, week)
	if c := f.get(card.ItemID); c != nil {
		c.Week = week
	}
	return nil
}

func (f *fakeBackend) SetTeam(_ context.Context, _ board.Board, card board.Card, team string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetTeam %s %s", card.ItemID, team)
	if c := f.get(card.ItemID); c != nil {
		c.Team = team
	}
	return nil
}

func (f *fakeBackend) SetEpic(_ context.Context, _ board.Board, card board.Card, epic string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetEpic %s %s", card.ItemID, epic)
	if c := f.get(card.ItemID); c != nil {
		c.Epic = epic
	}
	return nil
}

func (f *fakeBackend) SetProject(_ context.Context, _ board.Board, card board.Card, project string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetProject %s %s", card.ItemID, project)
	if c := f.get(card.ItemID); c != nil {
		c.Project = project
	}
	return nil
}

func (f *fakeBackend) SetRecurrence(_ context.Context, _ board.Board, card board.Card, cycle string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetRecurrence %s %s", card.ItemID, cycle)
	if c := f.get(card.ItemID); c != nil {
		c.Recurrence = cycle
	}
	return nil
}

func (f *fakeBackend) SetAssignee(_ context.Context, _ board.Board, card board.Card, login string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetAssignee %s %s", card.ItemID, login)
	if c := f.get(card.ItemID); c != nil {
		if login == "" {
			c.Assignees = []string{}
		} else {
			c.Assignees = []string{login}
		}
	}
	return nil
}

func (f *fakeBackend) SetParent(_ context.Context, _ board.Board, card board.Card, parent string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetParent %s %s", card.ItemID, parent)
	if c := f.get(card.ItemID); c != nil {
		c.Parent = parent
	}
	return nil
}

func (f *fakeBackend) SetReviewOf(_ context.Context, _ board.Board, card board.Card, reviewOf string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetReviewOf %s %s", card.ItemID, reviewOf)
	if c := f.get(card.ItemID); c != nil {
		c.ReviewOf = reviewOf
	}
	return nil
}

func (f *fakeBackend) SetReviewRound(_ context.Context, _ board.Board, card board.Card, round int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetReviewRound %s %d", card.ItemID, round)
	if c := f.get(card.ItemID); c != nil {
		c.ReviewRound = round
	}
	return nil
}

func (f *fakeBackend) SetSprintState(_ context.Context, _ board.Board, team, current, previous string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetSprintState %s cur=%s prev=%s", team, current, previous)
	st := f.b.SprintStates[team]
	st.Current = current
	st.Previous = previous
	if st.ItemID == "" {
		f.nextID++
		st.ItemID = fmt.Sprintf("state%d", f.nextID)
	}
	f.b.SprintStates[team] = st
	return nil
}

// ctx is a shared test context.
var ctx = context.Background()

// --- CreateCard ------------------------------------------------------------

func TestCreateCardStartsNewSprintForTeamWithNone(t *testing.T) {
	f := newFake(nil, nil)
	today := board.TodayIso()
	card, err := f2svc(f).CreateCard(ctx, "acme", 1, CreateCardArgs{Team: "alpha", Zone: board.ZoneGray, Title: "task", Assignee: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.saw(fmt.Sprintf("SetSprintState alpha cur=%s prev=", today)) {
		t.Fatalf("expected new sprint recorded; log=%v", f.log)
	}
	if len(f.creates) != 1 || f.creates[0].Start != today || f.creates[0].SprintStart != today {
		t.Fatalf("create input = %+v", f.creates)
	}
	if card.Team != "alpha" || card.SprintStart != today {
		t.Fatalf("card = %+v", card)
	}
}

func TestCreateCardOnTheSprintsOwnDay(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20"}})
	// Creating on the sprint's own day: Start (scheduled day) and SprintStart (the
	// sprint) coincide, and the team's sprint pointer is left untouched.
	if _, err := f2svc(f).CreateCard(ctx, "acme", 1, CreateCardArgs{Team: "alpha", Title: "task", Day: "2026-06-20"}); err != nil {
		t.Fatal(err)
	}
	if f.count("SetSprintState") != 0 {
		t.Fatalf("should not touch sprint state; log=%v", f.log)
	}
	if f.creates[0].SprintStart != "2026-06-20" || f.creates[0].Start != "2026-06-20" {
		t.Fatalf("create input = %+v", f.creates[0])
	}
}

func TestCreateCardJoinsCurrentSprintOnLaterDay(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20"}})
	// Creating on a later day joins the running sprint: Start is the scheduled day
	// while SprintStart stays the team's current sprint; the pointer is left alone.
	if _, err := f2svc(f).CreateCard(ctx, "acme", 1, CreateCardArgs{Team: "alpha", Title: "task", Day: "2026-06-30"}); err != nil {
		t.Fatal(err)
	}
	if f.count("SetSprintState") != 0 {
		t.Fatalf("creating with an existing sprint should not touch it; log=%v", f.log)
	}
	if f.creates[0].Start != "2026-06-30" || f.creates[0].SprintStart != "2026-06-20" {
		t.Fatalf("want Start 2026-06-30 / SprintStart 2026-06-20; got %+v", f.creates[0])
	}
}

func TestCreateCardForceNewSprintDemotesCurrent(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20"}})
	today := board.TodayIso()
	yes := true
	if _, err := f2svc(f).CreateCard(ctx, "acme", 1, CreateCardArgs{Team: "alpha", Title: "task", StartNewSprint: &yes}); err != nil {
		t.Fatal(err)
	}
	if !f.saw(fmt.Sprintf("SetSprintState alpha cur=%s prev=2026-06-20", today)) {
		t.Fatalf("force-new should demote current to previous; log=%v", f.log)
	}
	if f.creates[0].SprintStart != today {
		t.Fatalf("create input = %+v", f.creates[0])
	}
}

func TestCreateCardNextSprint(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20"}})
	// A "next sprint" create: scheduled for its day, joins no sprint and never
	// touches the pointer — the next carry-over to reach the day adopts it.
	if _, err := f2svc(f).CreateCard(ctx, "acme", 1, CreateCardArgs{Team: "alpha", Title: "task", Day: "2026-06-21", NoSprint: true}); err != nil {
		t.Fatal(err)
	}
	if f.count("SetSprintState") != 0 {
		t.Fatalf("a no-sprint create must not touch the pointer; log=%v", f.log)
	}
	if f.creates[0].Start != "2026-06-21" || f.creates[0].SprintStart != "" {
		t.Fatalf("want Start 2026-06-21 / no SprintStart; got %+v", f.creates[0])
	}
}

func TestCreateCardNoSprintSkipsFirstSprintRecord(t *testing.T) {
	f := newFake(nil, nil)
	// Even a team with no sprint yet must not have one started by a
	// "next sprint" create.
	if _, err := f2svc(f).CreateCard(ctx, "acme", 1, CreateCardArgs{Team: "alpha", Title: "task", NoSprint: true}); err != nil {
		t.Fatal(err)
	}
	if f.count("SetSprintState") != 0 {
		t.Fatalf("a no-sprint create must not record a first sprint; log=%v", f.log)
	}
	if f.creates[0].SprintStart != "" {
		t.Fatalf("create input = %+v", f.creates[0])
	}
}

// --- CarryOver -------------------------------------------------------------

func TestCarryOverAdvancesAndCarriesUnfinished(t *testing.T) {
	old := "2026-01-01"
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", SprintStart: old, Stage: board.StageNone},
		{ItemID: "c2", Team: "alpha", SprintStart: old, Stage: board.StageDone},
		{ItemID: "c3", Team: "alpha", SprintStart: "2026-02-01"},
		{ItemID: "c4", Team: "beta", SprintStart: old},
		{ItemID: "c5", Team: "alpha", SprintStart: "2027-01-01"},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	today := board.TodayIso()
	if _, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false); err != nil {
		t.Fatal(err)
	}
	if !f.saw(fmt.Sprintf("SetSprintState alpha cur=%s prev=%s", today, old)) {
		t.Fatalf("expected advance; log=%v", f.log)
	}
	// Only c1 (the current sprint being closed) carries; c2 is done, c3 is not on
	// the current sprint, c4 is another team, and c5 is future-dated — none move,
	// so a card removed from the current sprint never boomerangs back.
	if f.count("SetSprintStart") != 1 ||
		!f.saw(fmt.Sprintf("SetSprintStart c1 %s", today)) {
		t.Fatalf("only c1 should carry; log=%v", f.log)
	}
	if f.get("c1").SprintStart != today {
		t.Fatalf("c1 not carried: %+v", f.get("c1"))
	}
	if f.get("c3").SprintStart != "2026-02-01" {
		t.Fatalf("off-sprint c3 should stay: %+v", f.get("c3"))
	}
	if f.get("c5").SprintStart != "2027-01-01" {
		t.Fatalf("future-dated c5 should not carry: %+v", f.get("c5"))
	}
}

func TestCarryOverAdoptsSprintlessDayCards(t *testing.T) {
	old := "2026-01-01"
	today := board.TodayIso()
	f := newFake([]board.Card{
		// A "next sprint" create whose day has arrived: adopted.
		{ItemID: "n1", Team: "alpha", StartDate: today},
		// Still ahead of today: stays sprint-less for a later carry-over.
		{ItemID: "n2", Team: "alpha", StartDate: "2999-01-01"},
		// A finished sprint-less card is not work to adopt.
		{ItemID: "n3", Team: "alpha", StartDate: today, Stage: board.StageDone},
		// A plan card without dates has no sprint by design.
		{ItemID: "p1", Team: "alpha", Plan: board.PlanWed, Week: "2026-01-05"},
		// Another team's sprint-less card is not this carry-over's business.
		{ItemID: "n4", Team: "beta", StartDate: today},
		// An old sprint-less stray (scheduled before the closing sprint even
		// started — a report card, a legacy card) is not this sprint's work.
		{ItemID: "n5", Team: "alpha", StartDate: "2025-12-15"},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if f.count("SetSprintStart") != 1 || !f.saw(fmt.Sprintf("SetSprintStart n1 %s", today)) {
		t.Fatalf("only n1 should be adopted; log=%v", f.log)
	}
	if rep.Carried != 1 {
		t.Fatalf("Carried = %d, want 1", rep.Carried)
	}
	if f.get("n2").SprintStart != "" || f.get("n3").SprintStart != "" {
		t.Fatalf("future/finished sprint-less cards must stay: %+v %+v", f.get("n2"), f.get("n3"))
	}
	if f.get("n5").SprintStart != "" {
		t.Fatalf("a pre-sprint stray must not be adopted: %+v", f.get("n5"))
	}
}

func TestCarryOverIdempotentWhenAlreadyToday(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", SprintStart: today}},
		map[string]board.SprintState{"alpha": {Current: today}})
	if _, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false); err != nil {
		t.Fatal(err)
	}
	if f.count("SetSprintState") != 0 || f.count("SetSprintStart") != 0 {
		t.Fatalf("idempotent carry should do nothing; log=%v", f.log)
	}
}

func TestCarryOverWithNothingToCarryStillAdvances(t *testing.T) {
	old := "2026-01-01"
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", SprintStart: old, Stage: board.StageDone}},
		map[string]board.SprintState{"alpha": {Current: old}})
	if _, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false); err != nil {
		t.Fatal(err)
	}
	if f.count("SetSprintState") != 1 {
		t.Fatalf("expected the sprint to advance; log=%v", f.log)
	}
	if f.count("SetSprintStart") != 0 {
		t.Fatalf("nothing unfinished to carry; log=%v", f.log)
	}
}

// --- Stage / progress ------------------------------------------------------

func TestSetStageDoneFillsProgress(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Progress: 50}}, nil)
	if err := f2svc(f).SetStage(ctx, "acme", 1, "c1", board.StageDone); err != nil {
		t.Fatal(err)
	}
	// Done is derived, never stored: picking it clears the stage and fills the
	// bar, and Complete reports the card finished.
	if f.get("c1").Stage != board.StageNone || f.get("c1").Progress != 100 {
		t.Fatalf("card = %+v", f.get("c1"))
	}
	if !board.Complete(f.get("c1").Stage, f.get("c1").Progress) {
		t.Fatalf("a stage-less 100%% card should be complete: %+v", f.get("c1"))
	}
}

func TestSetStageReviewKnocksFullCardTo90(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Progress: 100}}, nil)
	if err := f2svc(f).SetStage(ctx, "acme", 1, "c1", board.StageReview); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Stage != board.StageReview || f.get("c1").Progress != 90 {
		t.Fatalf("card = %+v", f.get("c1"))
	}
}

func TestSetProgressDoneLink(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Progress: 50}}, nil)
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "c1", 100); err != nil {
		t.Fatal(err)
	}
	// Done is derived (no stage + 100%), never stored.
	if f.get("c1").Progress != 100 || f.get("c1").Stage != board.StageNone {
		t.Fatalf("100%% should stay stage-less: %+v", f.get("c1"))
	}
	// A legacy stored done clears itself when progress drops below full.
	f.get("c1").Stage = board.StageDone
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "c1", 80); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Progress != 80 || f.get("c1").Stage != board.StageNone {
		t.Fatalf("below 100 should clear a stored done: %+v", f.get("c1"))
	}
}

func TestSetProgressClampsReviewCard(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Progress: 50, Stage: board.StageReview}}, nil)
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "c1", 5); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Progress != 10 {
		t.Fatalf("review progress clamps up to 10: %+v", f.get("c1"))
	}
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "c1", 95); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Progress != 90 || f.get("c1").Stage != board.StageReview {
		t.Fatalf("review progress clamps down to 90: %+v", f.get("c1"))
	}
}

func TestSetInProgressClampsDoneCard(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Progress: 100, Stage: board.StageDone}}, nil)
	if err := f2svc(f).SetInProgress(ctx, "acme", 1, "c1"); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Stage != board.StageNone || f.get("c1").Progress != 90 {
		t.Fatalf("in progress should clear done and drop to 90: %+v", f.get("c1"))
	}
}

func TestSetProgressReviewLinkClearsOriginal(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Stage: board.StageReview},
		{ItemID: "rev", ReviewOf: "orig", Stage: board.StageNone, Progress: 50},
	}, nil)
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "rev", 100); err != nil {
		t.Fatal(err)
	}
	if f.get("orig").Stage != board.StageNone {
		t.Fatalf("review at 100%% should take the original out of review: %+v", f.get("orig"))
	}
}

func TestSetProgressReviewLinkReopensOriginal(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Stage: board.StageNone},
		{ItemID: "rev", ReviewOf: "orig", Stage: board.StageNone, Progress: 50},
	}, nil)
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "rev", 40); err != nil {
		t.Fatal(err)
	}
	if f.get("orig").Stage != board.StageReview {
		t.Fatalf("review below 100%% should put the original on review: %+v", f.get("orig"))
	}
}

// --- Review linkage --------------------------------------------------------

func TestSendToReviewCreatesLinkedCardAndStagesOriginal(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "orig", Title: "ship it", Team: "alpha", Zone: board.ZoneRed, Progress: 100}},
		map[string]board.SprintState{"alpha": {Current: "2026-06-20"}})
	day := "2026-06-25"
	rev, err := f2svc(f).SendToReview(ctx, "acme", 1, "orig", "carol", day, "")
	if err != nil {
		t.Fatal(err)
	}
	in := f.creates[0]
	if in.Title != "review: ship it" || in.Assignee != "carol" || in.ReviewOf != "orig" {
		t.Fatalf("review create input = %+v", in)
	}
	// Without an explicit zone the review card inherits the original's (the
	// Team board's behaviour).
	if in.Zone != board.ZoneRed || in.Team != "alpha" || in.Start != day || in.SprintStart != "2026-06-20" {
		t.Fatalf("review create input = %+v", in)
	}
	if rev.ReviewOf != "orig" {
		t.Fatalf("returned review card = %+v", rev)
	}
	// Original goes to review; a full card is knocked to 90.
	if f.get("orig").Stage != board.StageReview || f.get("orig").Progress != 90 {
		t.Fatalf("original = %+v", f.get("orig"))
	}
}

// The Me board sends the review card to the reviewer's unplanned zone
// explicitly: for the reviewer it is work that popped up during the day.
func TestSendToReviewWithExplicitZone(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "orig", Title: "ship it", Team: "alpha", Zone: board.ZoneRed}},
		map[string]board.SprintState{"alpha": {Current: "2026-06-20"}})
	if _, err := f2svc(f).SendToReview(ctx, "acme", 1, "orig", "carol", "2026-06-25", board.ZoneYellow); err != nil {
		t.Fatal(err)
	}
	if f.creates[0].Zone != board.ZoneYellow {
		t.Fatalf("review create input = %+v", f.creates[0])
	}
}

func TestReassignReviewerOnExistingReview(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Title: "x"},
		{ItemID: "rev", ReviewOf: "orig", Assignees: []string{"carol"}},
	}, nil)
	if err := f2svc(f).ReassignReviewer(ctx, "acme", 1, "orig", "dave", "", ""); err != nil {
		t.Fatal(err)
	}
	if f.count("CreateCard") != 0 {
		t.Fatalf("reassign should not create a new review card; log=%v", f.log)
	}
	if got := f.get("rev").Assignees; len(got) != 1 || got[0] != "dave" {
		t.Fatalf("review assignee = %v", got)
	}
}

func TestReassignReviewerWithoutReviewSendsToReview(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "orig", Title: "x", Team: "alpha"}}, nil)
	if err := f2svc(f).ReassignReviewer(ctx, "acme", 1, "orig", "dave", "2026-06-25", ""); err != nil {
		t.Fatal(err)
	}
	if f.count("CreateCard") != 1 || f.creates[0].Assignee != "dave" || f.creates[0].ReviewOf != "orig" {
		t.Fatalf("expected a review card to be created; creates=%+v", f.creates)
	}
}

func TestRemoveReviewerDeletesReviewCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Title: "x"},
		{ItemID: "rev", ReviewOf: "orig"},
	}, nil)
	if err := f2svc(f).RemoveReviewer(ctx, "acme", 1, "orig"); err != nil {
		t.Fatal(err)
	}
	if !f.saw("DeleteCard rev") || f.get("rev") != nil {
		t.Fatalf("review card should be deleted; log=%v", f.log)
	}
}

// --- Delete cascade --------------------------------------------------------

func TestDeleteCardCascadesToReview(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Title: "x"},
		{ItemID: "rev", ReviewOf: "orig"},
	}, nil)
	if err := f2svc(f).DeleteCard(ctx, "acme", 1, "orig"); err != nil {
		t.Fatal(err)
	}
	if f.get("orig") != nil || f.get("rev") != nil {
		t.Fatalf("both cards should be gone; remaining=%+v", f.b.Cards)
	}
	if f.count("DeleteCard") != 2 {
		t.Fatalf("expected two deletes; log=%v", f.log)
	}
}

func TestDeleteCardUnknownItem(t *testing.T) {
	f := newFake(nil, nil)
	err := f2svc(f).DeleteCard(ctx, "acme", 1, "nope")
	if err == nil || !strings.Contains(err.Error(), "card not found") {
		t.Fatalf("err = %v, want card not found", err)
	}
}

// --- SetTeam ---------------------------------------------------------------

func TestSetTeamJoinsNewTeamSprint(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", SprintStart: "2026-01-01"}},
		map[string]board.SprintState{"beta": {Current: "2026-06-20"}})
	if err := f2svc(f).SetTeam(ctx, "acme", 1, "c1", "beta", ""); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Team != "beta" || f.get("c1").SprintStart != "2026-06-20" {
		t.Fatalf("card = %+v", f.get("c1"))
	}
}

// --- Weekly plan -----------------------------------------------------------

func TestTakeIntoPlanAssignsAndJoinsSprint(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "p1", Plan: board.PlanWed, Week: "2026-06-15", Zone: board.ZoneGray, Team: "alpha", Assignees: []string{}}},
		map[string]board.SprintState{"alpha": {Current: "2026-06-20"}})
	if err := f2svc(f).TakeIntoPlan(ctx, "acme", 1, "p1", "bob", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.get("p1").Assignees; len(got) != 1 || got[0] != "bob" {
		t.Fatalf("assignee = %v", got)
	}
	if f.get("p1").SprintStart != "2026-06-20" {
		t.Fatalf("sprint = %+v", f.get("p1"))
	}
	if f.count("SetZone") != 0 {
		t.Fatalf("zone unchanged should not call SetZone; log=%v", f.log)
	}
}

func TestTakeIntoPlanChangesZone(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "p1", Plan: board.PlanWed, Zone: board.ZoneGray, Team: "alpha", Assignees: []string{}}}, nil)
	if err := f2svc(f).TakeIntoPlan(ctx, "acme", 1, "p1", "bob", board.ZoneRed, "2026-06-25"); err != nil {
		t.Fatal(err)
	}
	if f.get("p1").Zone != board.ZoneRed {
		t.Fatalf("zone = %+v", f.get("p1"))
	}
}

func TestReleaseFromPlanClearsMarkersWhenAssigned(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Plan: board.PlanWed, Week: "2026-06-15", Assignees: []string{"bob"}}}, nil)
	if err := f2svc(f).ReleaseFromPlan(ctx, "acme", 1, "c1"); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Plan != board.PlanNone || f.get("c1").Week != "" {
		t.Fatalf("assigned card should keep but lose plan markers: %+v", f.get("c1"))
	}
	if f.count("DeleteCard") != 0 {
		t.Fatalf("assigned card must not be deleted; log=%v", f.log)
	}
}

// Removing a pure (unassigned, never-worked) plan card deletes it for real —
// the old demote-to-previous-week made the next carry-week boomerang it back.
func TestReleaseFromPlanDeletesPureCardOutright(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p1", Plan: board.PlanWed, Week: "2026-06-15", Team: "alpha", Assignees: []string{}},
		{ItemID: "p2", Plan: board.PlanWed, Week: "2026-06-08", Team: "alpha", Assignees: []string{}},
	}, nil)
	if err := f2svc(f).ReleaseFromPlan(ctx, "acme", 1, "p1"); err != nil {
		t.Fatal(err)
	}
	if f.count("DeleteCard") != 1 {
		t.Fatalf("pure plan card must be deleted for real; log=%v", f.log)
	}
}

func TestReleaseFromPlanDeletesPureCardWithNoPreviousWeek(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "p1", Plan: board.PlanWed, Week: "2026-06-15", Team: "alpha", Assignees: []string{}}}, nil)
	if err := f2svc(f).ReleaseFromPlan(ctx, "acme", 1, "p1"); err != nil {
		t.Fatal(err)
	}
	if f.get("p1") != nil {
		t.Fatalf("a pure plan card with no earlier week should be deleted: %+v", f.b.Cards)
	}
}

// An unfinished card from an earlier week is a debt: carry-week counts it and
// leaves it in the week it was owed in. It reaches the current week's panel
// through the overdue rule, not by moving.
func TestCarryWeekCountsDebtsWithoutMovingThem(t *testing.T) {
	week := "2026-06-22"
	f := newFake([]board.Card{
		{ItemID: "a1", Team: "alpha", Plan: board.PlanWed, Week: "2026-06-15"},
		{ItemID: "a2", Team: "alpha", Plan: board.PlanFri, Week: "2026-06-15", Progress: 100},
		{ItemID: "a3", Team: "alpha", Plan: board.PlanFri, Week: week},
		{ItemID: "b1", Team: "beta", Plan: board.PlanWed, Week: "2026-06-15"},
	}, map[string]board.SprintState{"alpha": {Current: week, ItemID: "s1"}})
	svc := New(f)
	rep, err := svc.CarryWeek(context.Background(), "acme", 1, "alpha", week, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 1 {
		t.Fatalf("one open debt for alpha: rep = %+v", rep)
	}
	if a1 := f.get("a1"); a1.Week != "2026-06-15" || a1.Plan != board.PlanWed {
		t.Fatalf("the debt must not move or change band: %+v", a1)
	}
	if b1 := f.get("b1"); b1.Week != "2026-06-15" {
		t.Fatalf("another team's card is untouched: %+v", b1)
	}
}

func TestCarryWeekNothingToCarry(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "a1", Plan: board.PlanWed, Week: "2026-06-22", Team: "alpha"}}, nil)
	rep, err := f2svc(f).CarryWeek(ctx, "acme", 1, "alpha", "2026-06-22", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 0 || f.count("SetWeek") != 0 {
		t.Fatalf("nothing to carry; rep=%+v log=%v", rep, f.log)
	}
}

// --- Misc actions + views --------------------------------------------------

func TestRenameAddNoteAndMove(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Title: "old"}, {ItemID: "c2"}}, nil)
	svc := f2svc(f)
	if err := svc.Rename(ctx, "acme", 1, "c1", "new"); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Title != "new" {
		t.Fatalf("rename failed: %+v", f.get("c1"))
	}
	if err := svc.AddNote(ctx, "acme", 1, "c1", "a note"); err != nil {
		t.Fatal(err)
	}
	if !f.saw("AddNote c1 a note") {
		t.Fatalf("note not recorded; log=%v", f.log)
	}
	if err := svc.MoveCard(ctx, "acme", 1, "c1", "c2"); err != nil {
		t.Fatal(err)
	}
	if !f.saw("MoveCard c1 after=c2") {
		t.Fatalf("move not recorded; log=%v", f.log)
	}
}

func TestViewsReflectCreatedCard(t *testing.T) {
	f := newFake(nil, nil)
	today := board.TodayIso()
	if _, err := f2svc(f).CreateCard(ctx, "acme", 1, CreateCardArgs{Team: "alpha", Zone: board.ZoneGray, Title: "task", Assignee: "bob"}); err != nil {
		t.Fatal(err)
	}
	me, err := f2svc(f).MeView(ctx, "acme", 1, "bob", today)
	if err != nil {
		t.Fatal(err)
	}
	if len(me) != 1 || me[0].Title != "task" {
		t.Fatalf("MeView should show the new card: %+v", me)
	}
	team, err := f2svc(f).TeamView(ctx, "acme", 1, "alpha", today)
	if err != nil {
		t.Fatal(err)
	}
	if len(team) != 1 {
		t.Fatalf("TeamView should show the new card: %+v", team)
	}
}

func TestWeeklyPlanView(t *testing.T) {
	week := "2026-06-22"
	f := newFake([]board.Card{
		{ItemID: "a1", Plan: board.PlanWed, Week: week, Team: "alpha"},
		{ItemID: "a2", Plan: board.PlanFri, Week: week, Team: "alpha"},
	}, nil)
	bands, err := f2svc(f).WeeklyPlan(ctx, "acme", 1, "alpha", week)
	if err != nil {
		t.Fatal(err)
	}
	if len(bands.Wed) != 1 || len(bands.Fri) != 1 {
		t.Fatalf("bands = %+v", bands)
	}
}

// f2svc wraps a fake backend in a Service.
func f2svc(f *fakeBackend) *Service { return New(f) }

func TestCarryOverReseedsFinishedRecurrent(t *testing.T) {
	old := "2026-01-01"
	f := newFake([]board.Card{
		{ItemID: "r1", Team: "alpha", SprintStart: old, Stage: board.StageRecurrent, Progress: 100,
			Title: "standup", Description: "daily sync", Assignees: []string{"bob"}, Zone: board.ZoneGray},
		{ItemID: "r2", Team: "alpha", SprintStart: old, Stage: board.StageRecurrent, Progress: 40},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	today := board.TodayIso()
	if _, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false); err != nil {
		t.Fatal(err)
	}
	// The unfinished recurrent r2 carries like any card; the finished r1 stays.
	if !f.saw(fmt.Sprintf("SetSprintStart r2 %s", today)) {
		t.Fatalf("unfinished recurrent should carry; log=%v", f.log)
	}
	if f.get("r1").SprintStart != old {
		t.Fatalf("finished recurrent should stay behind: %+v", f.get("r1"))
	}
	// The finished one seeds a fresh copy in the new sprint: same title and
	// description, recurrent again, assignee kept, on today's dates.
	if len(f.creates) != 1 {
		t.Fatalf("expected one reseed create; creates=%v log=%v", f.creates, f.log)
	}
	in := f.creates[0]
	if in.Title != "standup" || in.Team != "alpha" || in.Assignee != "bob" ||
		in.SprintStart != today || in.Start != today || in.Day != today {
		t.Fatalf("reseed input = %+v", in)
	}
	if f.count("SetStage") != 1 || !f.saw("SetStage new2 recurrent") {
		t.Fatalf("reseed should be recurrent; log=%v", f.log)
	}
	if !f.saw("SetDescription new2 daily sync") {
		t.Fatalf("reseed should copy the description; log=%v", f.log)
	}
}

// A finished recurrent plan card seeds the target week with a fresh copy; an
// unfinished one is a debt and stays where it is.
func TestCarryWeekReseedsFinishedRecurrent(t *testing.T) {
	week := board.MondayOf(board.TodayIso())
	prev := board.AddDays(week, -7)
	f := newFake([]board.Card{
		{ItemID: "p1", Title: "standup", Team: "alpha", Plan: board.PlanWed, Week: prev,
			Stage: board.StageRecurrent, Progress: 100},
		{ItemID: "p2", Title: "feature", Team: "alpha", Plan: board.PlanFri, Week: prev, Progress: 20},
	}, map[string]board.SprintState{"alpha": {Current: week, ItemID: "s1"}})
	svc := New(f)
	rep, err := svc.CarryWeek(context.Background(), "acme", 1, "alpha", week, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reseeded != 1 || rep.Carried != 1 {
		t.Fatalf("rep = %+v", rep)
	}
	if p1 := f.get("p1"); p1.Week != prev || p1.Progress != 100 {
		t.Fatalf("the finished recurrent stays as history: %+v", p1)
	}
	if p2 := f.get("p2"); p2.Week != prev {
		t.Fatalf("the unfinished card is a debt and stays: %+v", p2)
	}
	var seeded *board.Card
	for i := range f.b.Cards {
		if f.b.Cards[i].Title == "standup" && f.b.Cards[i].ItemID != "p1" {
			seeded = &f.b.Cards[i]
		}
	}
	if seeded == nil || seeded.Week != week || seeded.Stage != board.StageRecurrent || seeded.Progress != 0 {
		t.Fatalf("the reseeded copy lands in the target week, recurrent and fresh: %+v", seeded)
	}
}

// --- Defer (matrix D9/D10): the frontend moveStart/handleDefer semantics -----

func TestDeferMovesStartFromToday(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", StartDate: "2000-01-05",
		SprintStart: "2000-01-01", Day: "2000-01-05", CreatedAt: "2000-01-05T10:00:00Z"}}, nil)
	if err := f2svc(f).Defer(ctx, "acme", 1, "c1", 1); err != nil {
		t.Fatal(err)
	}
	want := board.AddDays(today, 1)
	c := f.get("c1")
	if c.StartDate != want {
		t.Fatalf("start = %s, want %s (defer counts from today, not the old start)", c.StartDate, want)
	}
	// An old card keeps its history: sprint and end date untouched.
	if c.SprintStart != "2000-01-01" || c.Day != "2000-01-05" {
		t.Fatalf("history must stay: %+v", c)
	}
}

func TestDeferStacksFromDeferredSlot(t *testing.T) {
	today := board.TodayIso()
	slot := board.AddDays(today, 2)
	f := newFake([]board.Card{{ItemID: "c1", StartDate: slot,
		SprintStart: "2000-01-01", CreatedAt: "2000-01-05T10:00:00Z"}}, nil)
	if err := f2svc(f).Defer(ctx, "acme", 1, "c1", 7); err != nil {
		t.Fatal(err)
	}
	if want := board.AddDays(slot, 7); f.get("c1").StartDate != want {
		t.Fatalf("start = %s, want %s (presses stack)", f.get("c1").StartDate, want)
	}
}

func TestDeferSameDayCardRelocatesFully(t *testing.T) {
	today := board.TodayIso()
	created := time.Now().Format(time.RFC3339)
	f := newFake([]board.Card{{ItemID: "c1", StartDate: today, SprintStart: today,
		Day: today, CreatedAt: created}}, nil)
	if err := f2svc(f).Defer(ctx, "acme", 1, "c1", 1); err != nil {
		t.Fatal(err)
	}
	want := board.AddDays(today, 1)
	c := f.get("c1")
	if c.StartDate != want || c.SprintStart != want {
		t.Fatalf("a same-day card relocates fully: %+v, want %s", c, want)
	}
	if !f.saw("SetDay c1 " + want) {
		t.Fatalf("stale end date should be pulled along; log=%v", f.log)
	}
}

func TestDeferSameDayKeepsLaterEnd(t *testing.T) {
	today := board.TodayIso()
	created := time.Now().Format(time.RFC3339)
	end := board.AddDays(today, 5)
	f := newFake([]board.Card{{ItemID: "c1", StartDate: today, SprintStart: today,
		Day: end, CreatedAt: created}}, nil)
	if err := f2svc(f).Defer(ctx, "acme", 1, "c1", 1); err != nil {
		t.Fatal(err)
	}
	if f.count("SetDay") != 0 {
		t.Fatalf("an end date past the target stays; log=%v", f.log)
	}
}

// --- Calendar set-dates (matrix D11): handleSetDates semantics ---------------

func TestSetDatesJoinsSprintActiveOnStart(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", StartDate: "2026-01-05",
		SprintStart: "2026-01-05", Day: "2026-01-05"}},
		map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).SetDates(ctx, "acme", 1, "c1", "2026-01-04", "2026-01-06"); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	// 01-04 falls inside the previous sprint [01-03, 01-10) — the card joins it.
	if c.StartDate != "2026-01-04" || c.SprintStart != "2026-01-03" || c.Day != "2026-01-06" {
		t.Fatalf("card = %+v", c)
	}
}

func TestSetDatesInsideCurrentSprintJoinsIt(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha"}},
		map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).SetDates(ctx, "acme", 1, "c1", "2026-01-12", "2026-01-12"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c.SprintStart != "2026-01-10" {
		t.Fatalf("start inside the current sprint joins it: %+v", c)
	}
}

func TestSetDatesBeforeTrackedSprintsStandsAlone(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha"}},
		map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).SetDates(ctx, "acme", 1, "c1", "2026-01-01", "2026-01-02"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c.SprintStart != "2026-01-01" {
		t.Fatalf("start before tracked sprints becomes its own day: %+v", c)
	}
}

func TestSetDatesEmptyClears(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", StartDate: "2026-01-05",
		SprintStart: "2026-01-03", Day: "2026-01-06"}}, nil)
	if err := f2svc(f).SetDates(ctx, "acme", 1, "c1", "", ""); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	if c.StartDate != "" || c.SprintStart != "" {
		t.Fatalf("empty start clears start and sprint: %+v", c)
	}
	if !f.saw("SetDay c1 ") {
		t.Fatalf("empty end clears the end date; log=%v", f.log)
	}
}

// --- Smart remove (matrix A1–A3): handleGridDelete/removeFromPlan semantics --

func TestRemoveGridDemotesFromCurrentSprint(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", StartDate: "2026-01-10",
		SprintStart: "2026-01-10", Day: "2026-01-12"}},
		map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).Remove(ctx, "acme", 1, "c1", ""); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	if c == nil || c.StartDate != "2026-01-03" || c.SprintStart != "2026-01-03" {
		t.Fatalf("first x demotes to the previous sprint: %+v", c)
	}
	if !f.saw("SetDay c1 2026-01-03") {
		t.Fatalf("the end date is pulled back too; log=%v", f.log)
	}
}

func TestRemoveGridDeletesOutsideCurrentSprint(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", SprintStart: "2026-01-03"}},
		map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).Remove(ctx, "acme", 1, "c1", ""); err != nil {
		t.Fatal(err)
	}
	if f.get("c1") != nil {
		t.Fatalf("a card no longer in the current sprint is deleted for real")
	}
}

func TestRemoveGridDeletesWhenNoPreviousSprint(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", SprintStart: "2026-01-10"}},
		map[string]board.SprintState{"alpha": {Current: "2026-01-10"}})
	if err := f2svc(f).Remove(ctx, "acme", 1, "c1", ""); err != nil {
		t.Fatal(err)
	}
	if f.get("c1") != nil {
		t.Fatalf("no earlier sprint to demote into: delete")
	}
}

func TestRemoveGridReleasesPlanTakenCard(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Plan: board.PlanWed,
		Week: "2026-01-05", SprintStart: "2026-01-10", Assignees: []string{"bob"}}},
		map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).Remove(ctx, "acme", 1, "c1", ""); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	if c == nil || len(c.Assignees) != 0 || c.SprintStart != "" {
		t.Fatalf("a taken plan card is released, not deleted: %+v", c)
	}
	if c.Plan != board.PlanWed || c.Week != "2026-01-05" {
		t.Fatalf("it stays in the weekly plan: %+v", c)
	}
}

func TestRemovePlanClearsMarkerWhenAssigned(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Plan: board.PlanFri,
		Week: "2026-01-05", Assignees: []string{"bob"}}}, nil)
	if err := f2svc(f).Remove(ctx, "acme", 1, "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	if c == nil || c.Plan != board.PlanNone || c.Week != "" {
		t.Fatalf("removing an assigned card from the plan clears only the marker: %+v", c)
	}
}

// The plan × on a pure card deletes it for real even when earlier plan weeks
// exist — the old demote-to-previous-week boomeranged on the next carry-week.
func TestRemovePlanDeletesPureCardDespiteEarlierWeeks(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Plan: board.PlanWed, Week: "2026-01-12"},
		{ItemID: "c2", Team: "alpha", Plan: board.PlanFri, Week: "2026-01-05"},
	}, nil)
	if err := f2svc(f).Remove(ctx, "acme", 1, "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	if f.count("DeleteCard") != 1 {
		t.Fatalf("pure plan card must be deleted for real; log=%v", f.log)
	}
}

func TestRemovePlanDeletesWithoutEarlierWeek(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Plan: board.PlanWed, Week: "2026-01-05"}}, nil)
	if err := f2svc(f).Remove(ctx, "acme", 1, "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	if f.get("c1") != nil {
		t.Fatalf("no earlier week: the plan card is deleted")
	}
}

// --- Leaving review cancels the linked review card (matrix A5) ---------------

func TestStageOffReviewDemotesLinkedReview(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 50},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig",
			StartDate: "2026-01-10", SprintStart: "2026-01-10", Day: "2026-01-10"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).SetStage(ctx, "acme", 1, "orig", board.StageNone); err != nil {
		t.Fatal(err)
	}
	r := f.get("rev")
	if r == nil || r.SprintStart != "2026-01-03" || r.StartDate != "2026-01-03" {
		t.Fatalf("the linked review card demotes with the x logic: %+v", r)
	}
	if r.ReviewOf != "" {
		t.Fatalf("the review link must break, or the original keeps showing On review: %+v", r)
	}
}

func TestStageOffReviewDeletesLinkedOutsideCurrentSprint(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 50},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", SprintStart: "2026-01-03"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).SetStage(ctx, "acme", 1, "orig", board.StageLocked); err != nil {
		t.Fatal(err)
	}
	if f.get("rev") != nil {
		t.Fatalf("a linked review card outside the current sprint is deleted")
	}
}

func TestStageOffReviewKeepsFinishedReview(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 90},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 100, SprintStart: "2026-01-10"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).SetStage(ctx, "acme", 1, "orig", board.StageDone); err != nil {
		t.Fatal(err)
	}
	if r := f.get("rev"); r == nil || r.ReviewOf != "orig" {
		t.Fatalf("a finished review card stays as a record: %+v", r)
	}
}

func TestInProgressCancelsLinkedReview(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 50},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", SprintStart: "2026-01-03"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).SetInProgress(ctx, "acme", 1, "orig"); err != nil {
		t.Fatal(err)
	}
	if f.get("rev") != nil {
		t.Fatalf("In Progress leaves review too: the linked card cancels")
	}
}

func TestReviewerFinishingKeepsReviewCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 90},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 90, SprintStart: "2026-01-10"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	// The reviewer completes the review card: the original leaves review via the
	// review link, and the (now finished) review card must survive.
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "rev", 100); err != nil {
		t.Fatal(err)
	}
	if f.get("orig").Stage != board.StageNone {
		t.Fatalf("the original leaves review: %+v", f.get("orig"))
	}
	if r := f.get("rev"); r == nil || r.ReviewOf != "orig" {
		t.Fatalf("the finished review card stays: %+v", r)
	}
}

// --- Carry dry runs (matrix D16) ---------------------------------------------

func TestCarryOverDryRun(t *testing.T) {
	old := "2026-01-01"
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", SprintStart: old},
		{ItemID: "r1", Team: "alpha", SprintStart: old, Stage: board.StageRecurrent, Progress: 100},
		{ItemID: "d1", Team: "alpha", SprintStart: old, Stage: board.StageDone},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 1 || rep.Reseeded != 1 {
		t.Fatalf("report = %+v, want carried 1 (c1) reseeded 1 (r1)", rep)
	}
	if f.count("SetSprintState") != 0 || f.count("SetSprintStart") != 0 || f.count("CreateCard") != 0 {
		t.Fatalf("dry run must not write; log=%v", f.log)
	}
}

func TestCarryWeekDryRun(t *testing.T) {
	week := board.MondayOf(board.TodayIso())
	prev := board.AddDays(week, -7)
	f := newFake([]board.Card{
		{ItemID: "p1", Team: "alpha", Plan: board.PlanWed, Week: prev, Progress: 20},
		{ItemID: "p2", Team: "alpha", Plan: board.PlanFri, Week: prev, Stage: board.StageRecurrent, Progress: 100},
	}, nil)
	rep, err := f2svc(f).CarryWeek(ctx, "acme", 1, "alpha", week, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 1 || rep.Reseeded != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if f.count("SetWeek") != 0 || f.count("CreateCard") != 0 {
		t.Fatalf("dry run must not write; log=%v", f.log)
	}
}

// Links: extraction + resolution through the backend resolver (L1).
func TestCardLinks(t *testing.T) {
	fake := newFake([]board.Card{{
		ItemID: "c1",
		Description: "Docs: https://example.com/wiki\n" +
			"Blocked by https://github.com/acme/repo/issues/5 and https://github.com/acme/repo/pull/6",
	}}, nil)
	fake.refs = map[string]board.Link{
		"https://github.com/acme/repo/issues/5": {
			URL: "https://github.com/acme/repo/issues/5", Kind: "issue",
			Owner: "acme", Repo: "repo", Number: 5, Title: "Fix the flux capacitor", State: "open"},
	}
	svc := New(fake)
	links, err := svc.CardLinks(context.Background(), "acme", 1, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 {
		t.Fatalf("links = %+v", links)
	}
	// GitHub refs first; the resolved one carries its title, the unresolvable
	// PR stays as-is instead of being dropped; the plain link closes the list.
	if links[0].Title != "Fix the flux capacitor" || links[0].State != "open" {
		t.Fatalf("resolved = %+v", links[0])
	}
	if links[1].Kind != "pull" || links[1].Title != "" {
		t.Fatalf("unresolved = %+v", links[1])
	}
	if links[2].Kind != "link" || links[2].URL != "https://example.com/wiki" {
		t.Fatalf("plain = %+v", links[2])
	}
}

// Create-by-URL: a title that is nothing but a GitHub issue/PR URL becomes
// that item's title, with the link moved into the description (L2).
func TestCreateCardFromGitHubURL(t *testing.T) {
	fake := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	fake.refs = map[string]board.Link{
		"https://github.com/acme/repo/pull/7": {
			URL: "https://github.com/acme/repo/pull/7", Kind: "pull",
			Owner: "acme", Repo: "repo", Number: 7, Title: "feat: warp drive", State: "open"},
	}
	defer inlineSpawn(t)()
	svc := New(fake)
	card, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Team: "alpha", Title: "  https://github.com/acme/repo/pull/7 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The create answers at once under the readable fallback — it never waits
	// on GitHub — and the background resolve renames the card afterwards.
	if card.Title != "Pull: acme/repo#7" {
		t.Fatalf("create should answer under the fallback, got %q", card.Title)
	}
	if !fake.saw("RenameCard " + card.ItemID) {
		t.Fatalf("the background resolve must rename the card; log=%v", fake.log)
	}
	if got := fake.get(card.ItemID).Title; got != "feat: warp drive" {
		t.Fatalf("resolved title = %q", got)
	}
	if !fake.saw("SetDescription " + card.ItemID + " https://github.com/acme/repo/pull/7") {
		t.Fatal("description must carry the source link")
	}
}

// inlineSpawn makes the background title resolve run inline for the duration
// of a test, and returns the restore func.
func inlineSpawn(t *testing.T) func() {
	t.Helper()
	old := spawn
	spawn = func(fn func()) { fn() }
	return func() { spawn = old }
}

// A person retitling the card before the resolve lands keeps their words: the
// background rename only replaces the untouched fallback.
func TestCreateCardFromURLKeepsUserRename(t *testing.T) {
	fake := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	fake.refs = map[string]board.Link{
		"https://github.com/acme/repo/pull/7": {
			URL: "https://github.com/acme/repo/pull/7", Kind: "pull",
			Owner: "acme", Repo: "repo", Number: 7, Title: "feat: warp drive", State: "open"},
	}
	// Retitle the card the moment it exists, before the resolve runs.
	old := spawn
	spawn = func(fn func()) {
		for i := range fake.b.Cards {
			if fake.b.Cards[i].Title == "Pull: acme/repo#7" {
				fake.b.Cards[i].Title = "my own words"
			}
		}
		fn()
	}
	defer func() { spawn = old }()

	svc := New(fake)
	card, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Team: "alpha", Title: "https://github.com/acme/repo/pull/7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.get(card.ItemID).Title; got != "my own words" {
		t.Fatalf("a user rename must survive the resolve, got %q", got)
	}
	if fake.saw("RenameCard " + card.ItemID) {
		t.Fatalf("the resolve must not rename a retitled card; log=%v", fake.log)
	}
}

// Create-by-URL degrades gracefully: an unresolvable link (no repo access on a
// private repo) still yields a usable card — a readable "Issue: owner/repo#N"
// title, with the source URL filed in the body — instead of a bare-URL title.
func TestCreateCardFromURLUnresolved(t *testing.T) {
	fake := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	svc := New(fake)
	card, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Team: "alpha", Title: "https://github.com/acme/private/issues/9",
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.Title != "Issue: acme/private#9" {
		t.Fatalf("title = %q, want the fallback label", card.Title)
	}
	if card.Description != "https://github.com/acme/private/issues/9" {
		t.Fatalf("the source URL must be filed in the body, got %q", card.Description)
	}
	if fake.count("SetDescription") != 1 {
		t.Fatal("the URL should have been written to the description")
	}
}

// A PR URL falls back to a "Pull: owner/repo#N" label.
func TestCreateCardFromPullURLUnresolved(t *testing.T) {
	fake := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	svc := New(fake)
	card, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Team: "alpha", Title: "https://github.com/acme/webapp/pull/1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.Title != "Pull: acme/webapp#1234" {
		t.Fatalf("title = %q, want the pull fallback label", card.Title)
	}
}

// Send-to-review copies the original's description onto the review card (a
// one-time copy, so the reviewer sees the same context and links).
func TestSendToReviewCopiesDescription(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Title: "Work", Progress: 50,
			Description: "context: https://github.com/acme/repo/issues/5"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	svc := New(fake)
	review, err := svc.SendToReview(context.Background(), "acme", 1, "c1", "lllamnyp", "2026-06-21", "")
	if err != nil {
		t.Fatal(err)
	}
	if review.Description != "context: https://github.com/acme/repo/issues/5" {
		t.Fatalf("review description = %q", review.Description)
	}
	if !fake.saw("SetDescription " + review.ItemID + " context: https://github.com/acme/repo/issues/5") {
		t.Fatal("description copy not persisted")
	}
}

// The description live-syncs across the review link, both directions; notes
// are untouched (they stay per-card).
func TestSetDescriptionSyncsAcrossReviewLink(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Title: "Work"},
		{ItemID: "r1", Team: "alpha", Title: "review: Work", ReviewOf: "c1"},
	}, nil)
	svc := New(fake)

	// Original -> review card.
	if err := svc.SetDescription(context.Background(), "acme", 1, "c1", "new context"); err != nil {
		t.Fatal(err)
	}
	if !fake.saw("SetDescription c1 new context") || !fake.saw("SetDescription r1 new context") {
		t.Fatal("original edit must sync onto the review card")
	}

	// Review card -> original.
	if err := svc.SetDescription(context.Background(), "acme", 1, "r1", "reviewer note"); err != nil {
		t.Fatal(err)
	}
	if !fake.saw("SetDescription c1 reviewer note") {
		t.Fatal("review-card edit must sync back onto the original")
	}

	// A card with no counterpart writes only itself.
	fake2 := newFake([]board.Card{{ItemID: "solo"}}, nil)
	svc2 := New(fake2)
	if err := svc2.SetDescription(context.Background(), "acme", 1, "solo", "x"); err != nil {
		t.Fatal(err)
	}
	if fake2.count("SetDescription") != 1 {
		t.Fatal("no counterpart, no extra write")
	}
}

// --- Worked-on review cards are never auto-removed (lifecycle rules) ---------

// A review card the reviewer already put progress into stays untouched when
// the original leaves review (done, locked, in-progress) — link intact.
func TestStageOffReviewKeepsWorkedReviewCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 50},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 40,
			StartDate: "2026-01-10", SprintStart: "2026-01-10"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", Previous: "2026-01-03"}})
	if err := f2svc(f).SetStage(ctx, "acme", 1, "orig", board.StageDone); err != nil {
		t.Fatal(err)
	}
	r := f.get("rev")
	if r == nil || r.SprintStart != "2026-01-10" || r.ReviewOf != "orig" || r.Progress != 40 {
		t.Fatalf("a worked-on review card must stay untouched: %+v", r)
	}
}

// Reassigning a reviewer who already worked keeps their card (released from
// the link) and spawns a fresh review card for the new reviewer.
func TestReassignWorkedReviewerSpawnsNewCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Title: "Work", Stage: board.StageReview, Progress: 50},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 40, Assignees: []string{"old"}},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", ItemID: "s1"}})
	if err := f2svc(f).ReassignReviewer(ctx, "acme", 1, "orig", "new", "2026-01-10", ""); err != nil {
		t.Fatal(err)
	}
	if !f.saw("SetReviewOf rev ") {
		t.Fatal("the worked-on card must be released from the link")
	}
	if len(f.creates) != 1 || f.creates[0].Assignee != "new" || f.creates[0].ReviewOf != "orig" {
		t.Fatalf("a fresh review card for the new reviewer: %+v", f.creates)
	}
	if r := f.get("rev"); r == nil || r.Progress != 40 {
		t.Fatalf("the old reviewer's work stays: %+v", r)
	}
}

// An untouched (0%) review card is still simply handed over.
func TestReassignUntouchedReviewerInPlace(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 50},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Assignees: []string{"old"}},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", ItemID: "s1"}})
	if err := f2svc(f).ReassignReviewer(ctx, "acme", 1, "orig", "new", "2026-01-10", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.creates) != 0 || !f.saw("SetAssignee rev new") {
		t.Fatalf("an untouched review card is reassigned in place; creates=%v", f.creates)
	}
}

// Carry-over moves a review card only while its review is still required: the
// original unfinished on the review stage and a reviewer assigned.
func TestCarryOverReviewCardsOnlyWhileRequired(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "origDone", Team: "alpha", Progress: 100, SprintStart: "2026-01-01"},
		{ItemID: "revStale", Team: "alpha", ReviewOf: "origDone", Progress: 40,
			Assignees: []string{"bob"}, SprintStart: "2026-01-01"},
		{ItemID: "origLive", Team: "alpha", Stage: board.StageReview, Progress: 50, SprintStart: "2026-01-01"},
		{ItemID: "revLive", Team: "alpha", ReviewOf: "origLive", Progress: 40,
			Assignees: []string{"bob"}, SprintStart: "2026-01-01"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-01", ItemID: "s1"}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	// origLive + revLive carry; origDone finished; revStale left behind.
	if rep.Carried != 2 {
		t.Fatalf("carried = %d, want 2 (the stale review card stays)", rep.Carried)
	}
	if f.get("revStale").SprintStart != "2026-01-01" {
		t.Fatal("stale review card must not be dragged forward")
	}
	if f.get("revLive").SprintStart == "2026-01-01" {
		t.Fatal("a still-required review card carries")
	}
}

// A review card cannot be made recurrent — the backend rejects it.
func TestRecurrentRejectedOnReviewCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha"},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 40},
	}, nil)
	err := f2svc(f).SetStage(ctx, "acme", 1, "rev", board.StageRecurrent)
	if err == nil || !errors.Is(err, ErrInvalidStage) {
		t.Fatalf("expected ErrInvalidStage, got %v", err)
	}
	if f.count("SetStage") != 0 {
		t.Fatal("nothing must be written when the stage is rejected")
	}
	// A non-review card can still go recurrent.
	if err := f2svc(f).SetStage(ctx, "acme", 1, "orig", board.StageRecurrent); err != nil {
		t.Fatal(err)
	}
}

// Re-review reactivates the same reviewer's finished review card: the original
// goes back on review (progress clamped off 100), the review card resets to 0,
// and the round counter ticks up (round 1 implicit → 2 → 3).
func TestReReviewReactivatesReviewCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Title: "Work", Progress: 100},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 100, Assignees: []string{"bob"}},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", ItemID: "s1"}})
	svc := f2svc(f)
	if err := svc.ReassignReviewer(ctx, "acme", 1, "orig", "bob", "2026-01-10", ""); err != nil {
		t.Fatal(err)
	}
	orig := f.get("orig")
	if orig.Stage != board.StageReview || orig.Progress != 90 {
		t.Fatalf("original back on review, clamped: %+v", orig)
	}
	rev := f.get("rev")
	if rev.Progress != 0 || rev.ReviewRound != 2 || rev.ReviewOf != "orig" {
		t.Fatalf("review card reactivated at round 2, progress 0: %+v", rev)
	}
	// A second re-review advances to round 3.
	f.get("rev").Progress = 100
	if err := svc.ReassignReviewer(ctx, "acme", 1, "orig", "bob", "2026-01-10", ""); err != nil {
		t.Fatal(err)
	}
	if r := f.get("rev"); r.ReviewRound != 3 || r.Progress != 0 {
		t.Fatalf("second re-review → round 3: %+v", r)
	}
}

// Re-review to a DIFFERENT reviewer is not a reactivation: the finished card is
// released as a record and a fresh review card is created (PR #20 behaviour).
func TestReReviewDifferentReviewerDoesNotReactivate(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Title: "Work", Progress: 100},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 100, Assignees: []string{"bob"}},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", ItemID: "s1"}})
	svc := f2svc(f)
	if err := svc.ReassignReviewer(ctx, "acme", 1, "orig", "carol", "2026-01-10", ""); err != nil {
		t.Fatal(err)
	}
	if !f.saw("SetReviewOf rev ") {
		t.Fatal("the finished card is released for a different reviewer")
	}
	if f.count("SetReviewRound") != 0 {
		t.Fatal("no round bump on a different-reviewer re-review")
	}
	if len(f.creates) != 1 || f.creates[0].Assignee != "carol" {
		t.Fatalf("a fresh review card for the new reviewer: %+v", f.creates)
	}
}

// Re-review via the STAGE menu (setting a passed original back to review, no
// reviewer re-pick) also reactivates the completed review card.
func TestReReviewViaStageMenuReactivates(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Title: "Work", Progress: 100},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 100, ReviewRound: 2, Assignees: []string{"bob"}},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-10", ItemID: "s1"}})
	svc := f2svc(f)
	if err := svc.SetStage(ctx, "acme", 1, "orig", board.StageReview); err != nil {
		t.Fatal(err)
	}
	if o := f.get("orig"); o.Stage != board.StageReview {
		t.Fatalf("original on review: %+v", o)
	}
	if r := f.get("rev"); r.Progress != 0 || r.ReviewRound != 3 {
		t.Fatalf("review card reactivated (0, round 3): %+v", r)
	}
}

// Entering review with no completed review card (a fresh send handled
// elsewhere, or a still-in-progress review) does not reset anything.
func TestEnterReviewNoCompletedCardIsNoop(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Progress: 40},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 40, Assignees: []string{"bob"}},
	}, nil)
	if err := f2svc(f).SetStage(ctx, "acme", 1, "orig", board.StageReview); err != nil {
		t.Fatal(err)
	}
	if f.count("SetReviewRound") != 0 || f.get("rev").Progress != 40 {
		t.Fatal("an in-progress review card is left alone when the original enters review")
	}
}

// A review card is created in the SAME sprint as the card it reviews, not the
// team's current pointer.
func TestSendToReviewUsesOriginalSprint(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Title: "Work", SprintStart: "2026-01-01"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-08", ItemID: "s1"}})
	rev, err := f2svc(f).SendToReview(ctx, "acme", 1, "orig", "bob", "2026-01-08", "")
	if err != nil {
		t.Fatal(err)
	}
	if rev.SprintStart != "2026-01-01" {
		t.Fatalf("review card sprintStart = %q, want the original's 2026-01-01", rev.SprintStart)
	}
}

// Carry-over leaves a completed review card behind, pinned to the closing
// sprint's day, so it does not linger on the new sprint (and its unfinished
// original carries).
func TestCarryOverPinsCompletedReviewCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Progress: 50, StartDate: "2026-07-03", Day: "2026-07-03", SprintStart: "2026-07-02"},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 100, Assignees: []string{"bob"},
			StartDate: "2026-07-03", Day: "2026-07-03", SprintStart: "2026-07-02"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-07-02", ItemID: "s1"}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 1 {
		t.Fatalf("carried = %d, want 1 (only the original)", rep.Carried)
	}
	r := f.get("rev")
	if r.StartDate != "2026-07-02" || r.Day != "2026-07-02" || r.SprintStart != "2026-07-02" {
		t.Fatalf("completed review card must be pinned to the closing sprint: %+v", r)
	}
}

// A re-review in the new sprint pulls the review card into the original's
// current sprint and onto today, and bumps the round counter.
func TestReReviewRelocatesToNewSprintWithCounter(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Progress: 50, SprintStart: today},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 100, ReviewRound: 2,
			Assignees: []string{"bob"}, StartDate: "2026-07-02", Day: "2026-07-02", SprintStart: "2026-07-02"},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	if err := f2svc(f).SetStage(ctx, "acme", 1, "orig", board.StageReview); err != nil {
		t.Fatal(err)
	}
	r := f.get("rev")
	if r.SprintStart != today || r.StartDate != today || r.Day != today {
		t.Fatalf("re-review must relocate the review card to the current sprint/today: %+v", r)
	}
	if r.Progress != 0 || r.ReviewRound != 3 {
		t.Fatalf("re-review resets to 0 and bumps the round: %+v", r)
	}
}

// The plan × never deletes a card someone worked on: even unassigned, a card
// with progress sheds only its weekly membership and survives.
func TestPlanRemoveKeepsWorkedCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", Plan: board.PlanWed, Week: "2026-07-06", Progress: 40},
	}, nil)
	if err := f2svc(f).Remove(ctx, "acme", 1, "p", "plan"); err != nil {
		t.Fatal(err)
	}
	c := f.get("p")
	if f.count("DeleteCard") != 0 {
		t.Fatal("a worked card must not be deleted by the plan ×")
	}
	if c.Plan != board.PlanNone || c.Week != "" {
		t.Fatalf("plan membership must be shed: %+v", c)
	}
}

// The grid × on a taken plan card: untouched → released back to the plan
// (person + sprint cleared); already worked → keeps the person and its sprint
// history, shedding only the plan membership.
func TestGridRemoveOnTakenPlanCard(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "fresh", Team: "alpha", Plan: board.PlanWed, Week: "2026-07-06",
			Assignees: []string{"bob"}, SprintStart: "2026-07-03", StartDate: "2026-07-03"},
		{ItemID: "worked", Team: "alpha", Plan: board.PlanWed, Week: "2026-07-06",
			Assignees: []string{"bob"}, SprintStart: "2026-07-03", StartDate: "2026-07-03", Progress: 40},
	}, nil)
	svc := f2svc(f)
	if err := svc.Remove(ctx, "acme", 1, "fresh", "grid"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("fresh"); len(c.Assignees) != 0 || c.SprintStart != "" || c.Plan == board.PlanNone {
		t.Fatalf("untouched taken card must release back to the plan: %+v", c)
	}
	if err := svc.Remove(ctx, "acme", 1, "worked", "grid"); err != nil {
		t.Fatal(err)
	}
	c := f.get("worked")
	if len(c.Assignees) == 0 || c.SprintStart == "" {
		t.Fatalf("worked card must keep its person and sprint: %+v", c)
	}
	if c.Plan != board.PlanNone || c.Week != "" {
		t.Fatalf("worked card sheds only the plan membership: %+v", c)
	}
	if f.count("DeleteCard") != 0 {
		t.Fatal("nothing must be deleted")
	}
}

// A debt keeps its band: tightening a carried by-Friday card to by-Wednesday
// made sense when the card moved into the target week; now it stays in its
// own, and its band there is a fact about that week.
func TestCarryWeekKeepsADebtsBand(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "fri", Team: "alpha", Plan: board.PlanFri, Week: "2026-06-29", Progress: 20},
		{ItemID: "wed", Team: "alpha", Plan: board.PlanWed, Week: "2026-06-29", Progress: 20},
	}, map[string]board.SprintState{"alpha": {Current: "2026-07-06", ItemID: "s1"}})
	svc := New(f)
	if _, err := svc.CarryWeek(context.Background(), "acme", 1, "alpha", "2026-07-06", false); err != nil {
		t.Fatal(err)
	}
	if got := f.get("fri"); got.Plan != board.PlanFri || got.Week != "2026-06-29" {
		t.Fatalf("the by-Friday debt keeps its band and week: %+v", got)
	}
	if got := f.get("wed"); got.Plan != board.PlanWed || got.Week != "2026-06-29" {
		t.Fatalf("the by-Wednesday debt keeps its band and week: %+v", got)
	}
}

// Carry-week's reseed is idempotent: a copy already in the target week blocks a
// second one, and a torn reseed (plan band set, week empty — an interrupted
// earlier run) is finished by setting its week instead of duplicating.
func TestCarryWeekReseedDedupAndTornRepair(t *testing.T) {
	week := "2026-07-06"
	// Case 1: copy already in the target week -> nothing new.
	f := newFake([]board.Card{
		{ItemID: "src", Team: "alpha", Title: "Habit", Plan: board.PlanFri, Week: "2026-06-29",
			Stage: board.StageRecurrent, Progress: 100},
		{ItemID: "copy", Team: "alpha", Title: "Habit", Plan: board.PlanFri, Week: week},
	}, nil)
	if _, err := f2svc(f).CarryWeek(ctx, "acme", 1, "alpha", week, false); err != nil {
		t.Fatal(err)
	}
	if f.count("CreateCard") != 0 {
		t.Fatal("an existing target-week copy must block the reseed")
	}
	// Case 2: torn reseed (week empty) -> repaired, not duplicated.
	f = newFake([]board.Card{
		{ItemID: "src", Team: "alpha", Title: "Habit", Plan: board.PlanFri, Week: "2026-06-29",
			Stage: board.StageRecurrent, Progress: 100},
		{ItemID: "stray", Team: "alpha", Title: "Habit", Plan: board.PlanWed, Week: ""},
	}, nil)
	if _, err := f2svc(f).CarryWeek(ctx, "acme", 1, "alpha", week, false); err != nil {
		t.Fatal(err)
	}
	if f.count("CreateCard") != 0 {
		t.Fatal("a torn reseed must be repaired, not duplicated")
	}
	if c := f.get("stray"); c.Week != week {
		t.Fatalf("the torn copy must be finished into the target week: %+v", c)
	}
}

// Mutations record activity events with the acting user from the context; the
// event log is best-effort and no-op changes are not recorded.
func TestMutationsRecordEvents(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Progress: 40, SprintStart: today},
		{ItemID: "p1", Team: "alpha", Plan: board.PlanWed, Week: "2026-07-06"},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := f2svc(f)
	actx := WithActor(ctx, "kvaps")

	if err := svc.SetProgress(actx, "acme", 1, "c1", 60); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStage(actx, "acme", 1, "c1", board.StageLocked); err != nil {
		t.Fatal(err)
	}
	if err := svc.TakeIntoPlan(actx, "acme", 1, "p1", "dan", "", today); err != nil {
		t.Fatal(err)
	}
	evs := f.get("c1").Events
	if len(evs) != 2 {
		t.Fatalf("c1 events = %+v, want progress + stage", evs)
	}
	if evs[0].Kind != board.EventProgress || evs[0].From != "40" || evs[0].To != "60" || evs[0].Actor != "kvaps" {
		t.Fatalf("progress event = %+v", evs[0])
	}
	if evs[1].Kind != board.EventStage || evs[1].To != "locked" || evs[1].Actor != "kvaps" {
		t.Fatalf("stage event = %+v", evs[1])
	}
	pevs := f.get("p1").Events
	if len(pevs) != 1 || pevs[0].Kind != board.EventPlanTaken || pevs[0].To != "dan" {
		t.Fatalf("plan-taken event = %+v", pevs)
	}
}

// The review cycle records review-sent on the original, created on the review
// card, and review-passed on the original when the reviewer finishes.
func TestReviewCycleRecordsEvents(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Title: "Work", Progress: 50, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := f2svc(f)
	actx := WithActor(ctx, "kvaps")

	rev, err := svc.SendToReview(actx, "acme", 1, "orig", "lllamnyp", today, "")
	if err != nil {
		t.Fatal(err)
	}
	var sent bool
	for _, e := range f.get("orig").Events {
		if e.Kind == board.EventReviewSent && e.To == "lllamnyp" && e.Actor == "kvaps" {
			sent = true
		}
	}
	if !sent {
		t.Fatalf("orig events = %+v, want review-sent", f.get("orig").Events)
	}
	if revEvs := f.get(rev.ItemID).Events; len(revEvs) == 0 || revEvs[0].Kind != board.EventCreated {
		t.Fatalf("review card events = %+v, want created", f.get(rev.ItemID).Events)
	}
	// The reviewer finishes: the original records review-passed.
	rctx := WithActor(ctx, "lllamnyp")
	if err := svc.SetProgress(rctx, "acme", 1, rev.ItemID, 100); err != nil {
		t.Fatal(err)
	}
	var passed bool
	for _, e := range f.get("orig").Events {
		if e.Kind == board.EventReviewPassed && e.From == "lllamnyp" {
			passed = true
		}
	}
	if !passed {
		t.Fatalf("orig events = %+v, want review-passed", f.get("orig").Events)
	}
}

// Date, sprint and plan-cycle changes are recorded with full from->to values —
// the day-state replay feature reconstructs a card's state per day from these.
func TestDateAndSprintEvents(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: "2026-07-01", Day: "2026-07-02", SprintStart: "2026-07-01"},
		{ItemID: "p1", Team: "alpha", Plan: board.PlanWed, Week: "2026-06-29"},
	}, nil)
	svc := f2svc(f)
	actx := WithActor(ctx, "kvaps")
	if err := svc.SetDates(actx, "acme", 1, "c1", "2026-07-03", "2026-07-04"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetWeek(actx, "acme", 1, "p1", "2026-07-06"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPlan(actx, "acme", 1, "p1", board.PlanFri); err != nil {
		t.Fatal(err)
	}
	var dates, sprint bool
	for _, e := range f.get("c1").Events {
		if e.Kind == board.EventDates && e.From == "2026-07-01..2026-07-02" && e.To == "2026-07-03..2026-07-04" {
			dates = true
		}
		if e.Kind == board.EventSprint {
			sprint = true
		}
	}
	if !dates || !sprint {
		t.Fatalf("c1 events = %+v, want dates + sprint", f.get("c1").Events)
	}
	var week, band bool
	for _, e := range f.get("p1").Events {
		if e.Kind == board.EventWeek && e.From == "2026-06-29" && e.To == "2026-07-06" {
			week = true
		}
		if e.Kind == board.EventPlanBand && e.From == "wed" && e.To == "fri" {
			band = true
		}
	}
	if !week || !band {
		t.Fatalf("p1 events = %+v, want week + plan-band", f.get("p1").Events)
	}
}

// The review cross-links are logged on both sides: reactivation records the
// round reset on the REVIEW card; the original leaving review records the
// cancelled reviewer on the ORIGINAL.
func TestReviewCrossEvents(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Progress: 100, SprintStart: today},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 100, ReviewRound: 2,
			Assignees: []string{"bob"}, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := f2svc(f)
	actx := WithActor(ctx, "kvaps")
	// Stage-menu re-review: the review card records its round reset.
	if err := svc.SetStage(actx, "acme", 1, "orig", board.StageReview); err != nil {
		t.Fatal(err)
	}
	var round bool
	for _, e := range f.get("rev").Events {
		if e.Kind == board.EventReviewRound && e.From == "2" && e.To == "3" {
			round = true
		}
	}
	if !round {
		t.Fatalf("rev events = %+v, want review-round", f.get("rev").Events)
	}
	// Leaving review cancels the fresh round: the original records it.
	if err := svc.SetStage(actx, "acme", 1, "orig", board.StageNone); err != nil {
		t.Fatal(err)
	}
	var removed bool
	for _, e := range f.get("orig").Events {
		if e.Kind == board.EventReviewerRemoved && e.From == "bob" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("orig events = %+v, want reviewer-removed", f.get("orig").Events)
	}
}

// Creating a weekly-plan card records the created event too (the plan branch
// returns earlier than the day branch and must not skip the hook).
func TestPlanCreateRecordsEvent(t *testing.T) {
	f := newFake(nil, nil)
	card, err := f2svc(f).CreateCard(WithActor(ctx, "kvaps"), "acme", 1, CreateCardArgs{
		Title: "Plan it", Team: "alpha", Plan: board.PlanWed, Week: "2026-07-06",
	})
	if err != nil {
		t.Fatal(err)
	}
	evs := f.get(card.ItemID).Events
	if len(evs) != 1 || evs[0].Kind != board.EventCreated || evs[0].Actor != "kvaps" {
		t.Fatalf("plan card events = %+v, want created", evs)
	}
}

// SetPlan records the semantic transition: a regular card gaining a band was
// added to the weekly plan, one losing it was released; band-to-band is a
// deadline move.
func TestSetPlanRecordsSemanticEvents(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha"},
	}, nil)
	svc := f2svc(f)
	actx := WithActor(ctx, "kvaps")
	if err := svc.SetPlan(actx, "acme", 1, "c1", board.PlanWed); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPlan(actx, "acme", 1, "c1", board.PlanFri); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPlan(actx, "acme", 1, "c1", board.PlanNone); err != nil {
		t.Fatal(err)
	}
	evs := f.get("c1").Events
	if len(evs) != 3 ||
		evs[0].Kind != board.EventPlanAdded || evs[0].To != "wed" ||
		evs[1].Kind != board.EventPlanBand || evs[1].From != "wed" || evs[1].To != "fri" ||
		evs[2].Kind != board.EventPlanReleased || evs[2].From != "fri" {
		t.Fatalf("events = %+v, want plan-added/plan-band/plan-released", evs)
	}
}

// Carries are logged with the same kinds as manual moves: carry-over records a
// sprint event on each carried card, carry-week a week event (plus the band
// tighten), and a reseeded recurrent copy records created.
func TestCarryRecordsEvents(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Progress: 40, SprintStart: "2026-07-01",
			StartDate: "2026-07-01", CreatedAt: "2026-07-01T08:00:00Z"},
		{ItemID: "p1", Team: "alpha", Plan: board.PlanFri, Week: "2026-06-29", Progress: 20},
		{ItemID: "habit", Team: "alpha", Plan: board.PlanWed, Week: "2026-06-29",
			Stage: board.StageRecurrent, Progress: 100},
	}, map[string]board.SprintState{"alpha": {Current: "2026-07-01", ItemID: "s1"}})
	svc := f2svc(f)
	actx := WithActor(ctx, "kvaps")
	if _, err := svc.CarryOver(actx, "acme", 1, "alpha", false); err != nil {
		t.Fatal(err)
	}
	var sprint bool
	for _, e := range f.get("c1").Events {
		if e.Kind == board.EventSprint && e.From == "2026-07-01" && e.Actor == "kvaps" {
			sprint = true
		}
	}
	if !sprint {
		t.Fatalf("c1 events = %+v, want a sprint event from the carry", f.get("c1").Events)
	}
	if _, err := svc.CarryWeek(actx, "acme", 1, "alpha", "2026-07-06", false); err != nil {
		t.Fatal(err)
	}
	// A debt is not moved, so nothing about it is logged: the card's own
	// week and band are unchanged, and an event saying otherwise would lie.
	if evs := f.get("p1").Events; len(evs) != 0 {
		t.Fatalf("p1 events = %+v, want none — the debt did not move", evs)
	}
	// The reseeded recurrent copy records created.
	var reseeded bool
	for _, c := range f.b.Cards {
		if c.ItemID != "habit" && c.Title == f.get("habit").Title && c.Week == "2026-07-06" {
			for _, e := range c.Events {
				if e.Kind == board.EventCreated {
					reseeded = true
				}
			}
		}
	}
	if !reseeded {
		t.Fatal("the reseeded recurrent copy must record created")
	}
}

// flakyEventBackend fails AppendEvent a few times before delegating — the way
// GitHub's secondary rate limit behaves on carry bursts.
type flakyEventBackend struct {
	*fakeBackend
	failures int
}

func (f *flakyEventBackend) AppendEvent(ctx context.Context, b board.Board, card board.Card, e board.Event) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("secondary rate limit")
	}
	return f.fakeBackend.AppendEvent(ctx, b, card, e)
}

// Event writes retry transient failures with backoff instead of silently
// dropping the event; a persistent failure still never fails the mutation.
func TestLogEventRetriesTransientFailures(t *testing.T) {
	prev := eventRetryBackoff
	eventRetryBackoff = time.Millisecond
	defer func() { eventRetryBackoff = prev }()

	inner := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Progress: 40}}, nil)
	f := &flakyEventBackend{fakeBackend: inner, failures: 2}
	svc := New(f)
	if err := svc.SetProgress(WithActor(ctx, "kvaps"), "acme", 1, "c1", 60); err != nil {
		t.Fatal(err)
	}
	evs := inner.get("c1").Events
	if len(evs) != 1 || evs[0].Kind != board.EventProgress {
		t.Fatalf("events = %+v, want the retried progress event", evs)
	}
	// Persistent failure: the mutation still succeeds, the event is dropped.
	f.failures = 100
	if err := svc.SetProgress(WithActor(ctx, "kvaps"), "acme", 1, "c1", 80); err != nil {
		t.Fatal(err)
	}
	if got := len(inner.get("c1").Events); got != 1 {
		t.Fatalf("events = %d, want still 1 (dropped after retries)", got)
	}
}

// A description over the cap is rejected before anything is written — it
// shares the draft body with the note/event logs, which GitHub caps at ~64K.
func TestDescriptionLengthLimit(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha"}}, nil)
	svc := f2svc(f)
	// A multi-byte rune (3 bytes) verifies the cap counts runes, not bytes.
	long := strings.Repeat("€", MaxDescriptionLen+1)
	if err := svc.SetDescription(ctx, "acme", 1, "c1", long); !errors.Is(err, ErrDescriptionTooLong) {
		t.Fatalf("err = %v, want ErrDescriptionTooLong", err)
	}
	if f.count("SetDescription") != 0 {
		t.Fatal("nothing must be written on rejection")
	}
	if err := svc.SetDescription(ctx, "acme", 1, "c1", strings.Repeat("€", MaxDescriptionLen)); err != nil {
		t.Fatalf("at the limit must pass: %v", err)
	}
}

// MoveCardBefore resolves the true global anchor server-side: the card lands
// right before the named card, skipping itself when scanning for the
// predecessor. Clients rendering a filtered slice (a weekly-plan band) use it
// for "move to the top of my group" without knowing the full board order.
func TestMoveCardBefore(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name         string
		move, before string
		wantAfter    string
	}{
		{"before the first card = board top", "c3", "c1", ""},
		{"anchors on the true predecessor", "c1", "c3", "c2"},
		{"skips itself when already adjacent", "c2", "c3", "c1"},
	}
	for _, tc := range cases {
		f := newFake([]board.Card{{ItemID: "c1"}, {ItemID: "c2"}, {ItemID: "c3"}, {ItemID: "c4"}}, nil)
		svc := New(f)
		if err := svc.MoveCardBefore(ctx, "acme", 1, tc.move, tc.before); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		want := fmt.Sprintf("MoveCard %s after=%s", tc.move, tc.wantAfter)
		if !f.saw(want) {
			t.Errorf("%s: want %q; log=%v", tc.name, want, f.log)
		}
	}
}

// A note over the cap is rejected before anything is written — add and edit
// alike — mirroring the description guard.
func TestNoteLengthLimit(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha",
		Notes: []board.Note{{ID: "n1", Body: "hi"}}}}, nil)
	svc := f2svc(f)
	long := strings.Repeat("x", MaxNoteLen+1)
	if err := svc.AddNote(ctx, "acme", 1, "c1", long); !errors.Is(err, ErrNoteTooLong) {
		t.Fatalf("AddNote err = %v, want ErrNoteTooLong", err)
	}
	if err := svc.EditNote(ctx, "acme", 1, "c1", "n1", long); !errors.Is(err, ErrNoteTooLong) {
		t.Fatalf("EditNote err = %v, want ErrNoteTooLong", err)
	}
	if f.count("AddNote") != 0 || f.count("EditNote") != 0 {
		t.Fatal("nothing must be written on rejection")
	}
	if err := svc.AddNote(ctx, "acme", 1, "c1", strings.Repeat("x", MaxNoteLen)); err != nil {
		t.Fatalf("at the limit must pass: %v", err)
	}
}

// A weekly recurrent card finished in the closing sprint does NOT reseed the
// next morning — it rests until a carry-over reaches its due day.
func TestCarryOverWeeklyRecurrenceRests(t *testing.T) {
	today := board.TodayIso()
	old := board.AddDays(today, -1) // closing sprint = yesterday
	f := newFake([]board.Card{
		{ItemID: "w1", Team: "alpha", SprintStart: old, Stage: board.StageRecurrent,
			Progress: 100, Recurrence: board.RecurrenceWeek, Title: "weekly report"},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reseeded != 0 || len(f.creates) != 0 {
		t.Fatalf("not-due weekly card must rest; rep=%+v creates=%v", rep, f.creates)
	}
	if f.get("w1").SprintStart != old {
		t.Fatalf("resting card must stay put: %+v", f.get("w1"))
	}
}

// Once the interval has elapsed, a resting weekly/monthly card is reseeded even
// though its sprint is long past the one being closed — and the fresh copy
// carries the cycle.
func TestCarryOverWeeklyRecurrenceReseedsWhenDue(t *testing.T) {
	today := board.TodayIso()
	anchor := board.AddDays(today, -7) // bound a week ago -> due today
	old := board.AddDays(today, -1)
	f := newFake([]board.Card{
		{ItemID: "w1", Team: "alpha", SprintStart: anchor, Stage: board.StageRecurrent,
			Progress: 100, Recurrence: board.RecurrenceWeek, Title: "weekly report"},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reseeded != 1 || len(f.creates) != 1 {
		t.Fatalf("due weekly card must reseed; rep=%+v creates=%v log=%v", rep, f.creates, f.log)
	}
	in := f.creates[0]
	if in.Title != "weekly report" || in.SprintStart != today {
		t.Fatalf("reseed input = %+v", in)
	}
	carried := false
	for _, l := range f.log {
		if strings.HasPrefix(l, "SetRecurrence new") && strings.HasSuffix(l, " week") {
			carried = true
		}
	}
	if !carried {
		t.Fatalf("the copy must carry the cycle; log=%v", f.log)
	}
}

// A backdated, still-unfinished monthly card (the "recurs on the 1st" calendar
// flow) also reseeds when due — completion is not required for cycle cards
// that already fell off the board.
func TestCarryOverMonthlyRecurrenceBackdated(t *testing.T) {
	today := board.TodayIso()
	anchor := board.AddDays(today, -31) // over a calendar month ago
	old := board.AddDays(today, -1)
	f := newFake([]board.Card{
		{ItemID: "m1", Team: "alpha", SprintStart: anchor, Stage: board.StageRecurrent,
			Progress: 0, Recurrence: board.RecurrenceMonth, Title: "monthly invoice"},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reseeded != 1 || len(f.creates) != 1 {
		t.Fatalf("due monthly card must reseed; rep=%+v log=%v", rep, f.log)
	}
}

// A due resting card with a FRESHER copy on the board (same team+title, later
// sprint) was already reseeded by a past carry-over: reruns must not duplicate.
func TestCarryOverRecurrenceNoDuplicateReseed(t *testing.T) {
	today := board.TodayIso()
	anchor := board.AddDays(today, -14)
	newer := board.AddDays(today, -7)
	old := board.AddDays(today, -1)
	f := newFake([]board.Card{
		{ItemID: "w1", Team: "alpha", SprintStart: anchor, Stage: board.StageRecurrent,
			Progress: 100, Recurrence: board.RecurrenceWeek, Title: "weekly report"},
		{ItemID: "w2", Team: "alpha", SprintStart: newer, Stage: board.StageRecurrent,
			Progress: 100, Recurrence: board.RecurrenceWeek, Title: "weekly report"},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	// Only w2 (the newest) is a legitimate reseed source; w1 is history.
	if rep.Reseeded != 1 || len(f.creates) != 1 {
		t.Fatalf("exactly one reseed expected; rep=%+v creates=%v", rep, f.creates)
	}
}

// Leaving the recurrent stage sheds the cycle, so a stale hidden marker can
// never resurrect the card at some future carry-over.
func TestStageChangeShedsRecurrence(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Stage: board.StageRecurrent,
			Recurrence: board.RecurrenceMonth, Progress: 40},
	}, nil)
	if err := f2svc(f).SetStage(ctx, "acme", 1, "c1", board.StageLocked); err != nil {
		t.Fatal(err)
	}
	if !f.saw("SetRecurrence c1 ") {
		t.Fatalf("cycle must be cleared on leaving recurrent; log=%v", f.log)
	}
}

// SetRecurrence validates the cycle and requires the recurrent stage.
func TestSetRecurrenceValidation(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Stage: board.StageRecurrent},
		{ItemID: "c2", Team: "alpha"},
	}, nil)
	svc := f2svc(f)
	if err := svc.SetRecurrence(ctx, "acme", 1, "c1", "week"); err != nil {
		t.Fatal(err)
	}
	if f.get("c1").Recurrence != "week" {
		t.Fatalf("cycle not stored: %+v", f.get("c1"))
	}
	if err := svc.SetRecurrence(ctx, "acme", 1, "c1", "yearly"); err == nil {
		t.Fatal("unknown cycle must be rejected")
	}
	if err := svc.SetRecurrence(ctx, "acme", 1, "c2", "week"); err == nil {
		t.Fatal("cycle on a non-recurrent card must be rejected")
	}
}

// ReorderTeams moves the hidden sprint-state cards into the given sequence —
// the shared, server-side team order.
func TestReorderTeamsMovesSprintStates(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{
		"alpha": {Current: "2026-01-01", ItemID: "s-alpha"},
		"beta":  {Current: "2026-01-01", ItemID: "s-beta"},
		"gamma": {Current: "2026-01-01", ItemID: "s-gamma"},
	})
	if err := f2svc(f).ReorderTeams(ctx, "acme", 1, []string{"gamma", "alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"MoveCard s-gamma after=",
		"MoveCard s-alpha after=s-gamma",
		"MoveCard s-beta after=s-alpha",
	}
	got := []string{}
	for _, l := range f.log {
		if strings.HasPrefix(l, "MoveCard") {
			got = append(got, l)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("moves = %v, want %v", got, want)
	}
}

// DeleteTeam removes the sprint-state card — but never while cards still use
// the team, and quietly succeeds when there is no pointer to delete.
func TestDeleteTeamGuardsAndDeletes(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha"},
	}, map[string]board.SprintState{
		"alpha": {Current: "2026-01-01", ItemID: "s-alpha"},
		"beta":  {Current: "2026-01-01", ItemID: "s-beta"},
	})
	svc := f2svc(f)
	if err := svc.DeleteTeam(ctx, "acme", 1, "alpha"); !errors.Is(err, ErrTeamInUse) {
		t.Fatalf("in-use team must be protected, got %v", err)
	}
	if err := svc.DeleteTeam(ctx, "acme", 1, "beta"); err != nil {
		t.Fatal(err)
	}
	if !f.saw("DeleteCard s-beta") {
		t.Fatalf("sprint-state not deleted; log=%v", f.log)
	}
	if err := svc.DeleteTeam(ctx, "acme", 1, "ghost"); err != nil {
		t.Fatalf("pointer-less team must be a no-op success, got %v", err)
	}
}

// Scheduling a card into the future parks it off the board until that day,
// and it joins NO sprint while it waits: no sprint covers that day yet, and
// pinning it to the sprint in progress only made it ride every daily
// carry-over (issue #82, then reported again from the boards). The
// carry-over that reaches its day adopts it.
func TestSetDatesFutureDayParksTheCardOffTheBoard(t *testing.T) {
	today := board.TodayIso()
	current := board.AddDays(today, -2) // the sprint in progress, opened earlier
	future := board.AddDays(today, 26)
	fake := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: today, Day: today, SprintStart: current, Assignees: []string{"ann"}},
	}, map[string]board.SprintState{"alpha": {Current: current, ItemID: "s1"}})
	svc := New(fake)

	if err := svc.SetDates(context.Background(), "acme", 1, "c1", future, future); err != nil {
		t.Fatal(err)
	}
	got := fake.get("c1")
	if got.StartDate != future {
		t.Fatalf("start = %q, want %q", got.StartDate, future)
	}
	if got.SprintStart != "" {
		t.Fatalf("sprint = %q, want none: no sprint covers a future day yet", got.SprintStart)
	}

	// The point of the dates: the card is gone from the sprint in progress —
	// on its own day and on today — and returns on the day it was scheduled for.
	b, err := svc.Board(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range []string{current, today} {
		if got := board.TeamGrid(b, "alpha", day); len(got) != 0 {
			t.Fatalf("a future-dated card must leave the board on %s, got %+v", day, got)
		}
		if got := board.MeView(b, "ann", day); len(got) != 0 {
			t.Fatalf("a future-dated card must leave the Me board on %s, got %+v", day, got)
		}
	}
	if got := board.TeamGrid(b, "alpha", future); len(got) != 1 {
		t.Fatalf("the card must show on its scheduled day, got %+v", got)
	}
	if got := board.MeView(b, "ann", future); len(got) != 1 {
		t.Fatalf("the card must show on its scheduled day in Me, got %+v", got)
	}
}

// A card scheduled for a future day must not join today's sprint. The sprint
// for that day does not exist yet, so joining the current one only makes the
// card ride every daily carry-over while it waits — its dates reading
// "current sprint" the whole time, which is what colleagues reported. It
// stays sprint-less until the carry-over that reaches its day adopts it.
func TestCreateCardFutureDayJoinsNoSprint(t *testing.T) {
	today := board.TodayIso()
	future := board.AddDays(today, 20)
	fake := newFake(nil, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)

	card, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Team: "alpha", Title: "ship it", Start: future, Day: future,
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.SprintStart != "" {
		t.Fatalf("sprint = %q, want none: a future day has no sprint yet", card.SprintStart)
	}
	if card.StartDate != future {
		t.Fatalf("start = %q, want %q", card.StartDate, future)
	}

	// Today's card is unaffected: it joins the sprint in progress.
	now, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Team: "alpha", Title: "today's work", Start: today, Day: today,
	})
	if err != nil {
		t.Fatal(err)
	}
	if now.SprintStart != today {
		t.Fatalf("a card for today must join the current sprint, got %q", now.SprintStart)
	}
}

// An explicit sprint (or an explicit new sprint) still wins over the
// future-day rule — the caller said what they wanted.
func TestCreateCardFutureDayRespectsExplicitSprint(t *testing.T) {
	today := board.TodayIso()
	future := board.AddDays(today, 20)
	fake := newFake(nil, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)

	card, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Team: "alpha", Title: "pinned", Start: future, Day: future, SprintStart: today,
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.SprintStart != today {
		t.Fatalf("an explicit sprint must win, got %q", card.SprintStart)
	}
}

// Rescheduling an existing card into the future parks it the same way, and
// the carry-over that reaches its day is what puts it back in a sprint.
func TestSetDatesIntoFutureLeavesTheSprint(t *testing.T) {
	today := board.TodayIso()
	future := board.AddDays(today, 9)
	fake := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: today, Day: today, SprintStart: today, Assignees: []string{"ann"}},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)

	if err := svc.SetDates(context.Background(), "acme", 1, "c1", future, future); err != nil {
		t.Fatal(err)
	}
	got := fake.get("c1")
	if got.SprintStart != "" {
		t.Fatalf("sprint = %q, want none while the card waits for its day", got.SprintStart)
	}

	// The day arrives and a carry-over opens the sprint: the card is adopted.
	b, err := svc.Board(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	carry, _, _ := selectCarry(b, "alpha", today, future)
	found := false
	for _, c := range carry {
		if c.ItemID == "c1" {
			found = true
		}
	}
	if !found {
		t.Fatal("the carry-over reaching the card's day must adopt it into the sprint it opens")
	}

	// Moving it back to today rejoins the sprint in progress.
	if err := svc.SetDates(context.Background(), "acme", 1, "c1", today, today); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("c1"); got.SprintStart != today {
		t.Fatalf("back on today's board, sprint = %q, want %q", got.SprintStart, today)
	}
}

func (f *fakeBackend) SetProcess(_ context.Context, _ board.Board, card board.Card, process string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetProcess %s %s", card.ItemID, process)
	if c := f.get(card.ItemID); c != nil {
		c.Process = process
	}
	return nil
}

func (f *fakeBackend) SetTask(_ context.Context, _ board.Board, card board.Card, task string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetTask %s %s", card.ItemID, task)
	if c := f.get(card.ItemID); c != nil {
		c.Task = task
	}
	return nil
}

func (f *fakeBackend) SetAccumulate(_ context.Context, _ board.Board, card board.Card, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetAccumulate %s %v", card.ItemID, on)
	if c := f.get(card.ItemID); c != nil {
		c.Accumulate = on
	}
	return nil
}

func (f *fakeBackend) SetPaused(_ context.Context, _ board.Board, card board.Card, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetPaused %s %v", card.ItemID, paused)
	if c := f.get(card.ItemID); c != nil {
		c.Paused = paused
	}
	return nil
}
