package boardservice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aenix-org/aeman/internal/board"
	"github.com/aenix-org/aeman/internal/ghprojects"
)

// *ghprojects.Client must satisfy Backend structurally (no boardservice import in
// ghprojects). This line is the compile-time proof.
var _ Backend = (*ghprojects.Client)(nil)

// fakeBackend implements Backend over an in-memory board, logging every call and
// mutating its board so the service's views reflect the result.
type fakeBackend struct {
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
	for _, e := range f.log {
		if e == s {
			return true
		}
	}
	return false
}

func (f *fakeBackend) count(prefix string) int {
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

func (f *fakeBackend) LoadBoard(_ context.Context, _ string, _ int) (board.Board, error) {
	f.rec("LoadBoard")
	cards := make([]board.Card, len(f.b.Cards))
	copy(cards, f.b.Cards)
	states := map[string]board.SprintState{}
	for k, v := range f.b.SprintStates {
		states[k] = v
	}
	return board.Board{ID: f.b.ID, Number: f.b.Number, Owner: f.b.Owner, Cards: cards, SprintStates: states}, nil
}

func (f *fakeBackend) LoadCards(_ context.Context, _ board.Board, ids []string) ([]board.Card, error) {
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
	f.nextID++
	card := board.Card{
		ItemID: fmt.Sprintf("new%d", f.nextID), Title: in.Title, IsDraft: true,
		Zone: in.Zone, StartDate: in.Start, SprintStart: in.SprintStart,
		Plan: in.Plan, Week: in.Week, Team: in.Team, ReviewOf: in.ReviewOf,
		Assignees: []string{},
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
	f.rec("MoveCard %s after=%s", card.ItemID, afterID)
	return nil
}

func (f *fakeBackend) AddNote(_ context.Context, _ board.Board, card board.Card, text string) error {
	f.rec("AddNote %s %s", card.ItemID, text)
	return nil
}

func (f *fakeBackend) EditNote(_ context.Context, _ board.Board, card board.Card, note board.Note, text string) error {
	f.rec("EditNote %s %s %s", card.ItemID, note.ID, text)
	return nil
}

func (f *fakeBackend) DeleteNote(_ context.Context, _ board.Board, card board.Card, note board.Note) error {
	f.rec("DeleteNote %s %s", card.ItemID, note.ID)
	return nil
}

func (f *fakeBackend) SetDescription(_ context.Context, _ board.Board, card board.Card, description string) error {
	f.rec("SetDescription %s %s", card.ItemID, description)
	return nil
}

func (f *fakeBackend) RenameCard(_ context.Context, _ board.Board, card board.Card, title string) error {
	f.rec("RenameCard %s", card.ItemID)
	if c := f.get(card.ItemID); c != nil {
		c.Title = title
	}
	return nil
}

func (f *fakeBackend) SetStage(_ context.Context, _ board.Board, card board.Card, stage board.StageKey) error {
	f.rec("SetStage %s %s", card.ItemID, stage)
	if c := f.get(card.ItemID); c != nil {
		c.Stage = stage
	}
	return nil
}

func (f *fakeBackend) SetProgress(_ context.Context, _ board.Board, card board.Card, progress int) error {
	f.rec("SetProgress %s %d", card.ItemID, progress)
	if c := f.get(card.ItemID); c != nil {
		c.Progress = progress
	}
	return nil
}

func (f *fakeBackend) SetZone(_ context.Context, _ board.Board, card board.Card, zone board.ZoneKey) error {
	f.rec("SetZone %s %s", card.ItemID, zone)
	if c := f.get(card.ItemID); c != nil {
		c.Zone = zone
	}
	return nil
}

func (f *fakeBackend) SetDay(_ context.Context, _ board.Board, card board.Card, day string) error {
	f.rec("SetDay %s %s", card.ItemID, day)
	return nil
}

func (f *fakeBackend) SetStart(_ context.Context, _ board.Board, card board.Card, date string) error {
	f.rec("SetStart %s %s", card.ItemID, date)
	if c := f.get(card.ItemID); c != nil {
		c.StartDate = date
	}
	return nil
}

func (f *fakeBackend) SetSprintStart(_ context.Context, _ board.Board, card board.Card, date string) error {
	f.rec("SetSprintStart %s %s", card.ItemID, date)
	if c := f.get(card.ItemID); c != nil {
		c.SprintStart = date
	}
	return nil
}

func (f *fakeBackend) SetPlan(_ context.Context, _ board.Board, card board.Card, plan board.PlanBand) error {
	f.rec("SetPlan %s %s", card.ItemID, plan)
	if c := f.get(card.ItemID); c != nil {
		c.Plan = plan
	}
	return nil
}

func (f *fakeBackend) SetWeek(_ context.Context, _ board.Board, card board.Card, week string) error {
	f.rec("SetWeek %s %s", card.ItemID, week)
	if c := f.get(card.ItemID); c != nil {
		c.Week = week
	}
	return nil
}

func (f *fakeBackend) SetTeam(_ context.Context, _ board.Board, card board.Card, team string) error {
	f.rec("SetTeam %s %s", card.ItemID, team)
	if c := f.get(card.ItemID); c != nil {
		c.Team = team
	}
	return nil
}

func (f *fakeBackend) SetAssignee(_ context.Context, _ board.Board, card board.Card, login string) error {
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

func (f *fakeBackend) SetReviewOf(_ context.Context, _ board.Board, card board.Card, reviewOf string) error {
	f.rec("SetReviewOf %s %s", card.ItemID, reviewOf)
	if c := f.get(card.ItemID); c != nil {
		c.ReviewOf = reviewOf
	}
	return nil
}

func (f *fakeBackend) SetSprintState(_ context.Context, _ board.Board, team, current, previous string) error {
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
	if err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha"); err != nil {
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

func TestCarryOverIdempotentWhenAlreadyToday(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", SprintStart: today}},
		map[string]board.SprintState{"alpha": {Current: today}})
	if err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha"); err != nil {
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
	if err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha"); err != nil {
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
	rev, err := f2svc(f).SendToReview(ctx, "acme", 1, "orig", "carol", day)
	if err != nil {
		t.Fatal(err)
	}
	in := f.creates[0]
	if in.Title != "review: ship it" || in.Assignee != "carol" || in.ReviewOf != "orig" {
		t.Fatalf("review create input = %+v", in)
	}
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

func TestReassignReviewerOnExistingReview(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Title: "x"},
		{ItemID: "rev", ReviewOf: "orig", Assignees: []string{"carol"}},
	}, nil)
	if err := f2svc(f).ReassignReviewer(ctx, "acme", 1, "orig", "dave", ""); err != nil {
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
	if err := f2svc(f).ReassignReviewer(ctx, "acme", 1, "orig", "dave", "2026-06-25"); err != nil {
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

func TestReleaseFromPlanDemotesPureCardToPreviousWeek(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p1", Plan: board.PlanWed, Week: "2026-06-15", Team: "alpha", Assignees: []string{}},
		{ItemID: "p2", Plan: board.PlanWed, Week: "2026-06-08", Team: "alpha", Assignees: []string{}},
	}, nil)
	if err := f2svc(f).ReleaseFromPlan(ctx, "acme", 1, "p1"); err != nil {
		t.Fatal(err)
	}
	if f.get("p1").Week != "2026-06-08" {
		t.Fatalf("pure plan card should demote to previous week: %+v", f.get("p1"))
	}
	if f.count("DeleteCard") != 0 {
		t.Fatalf("demote must not delete; log=%v", f.log)
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

func TestCarryWeekMovesUnfinishedEarlierCards(t *testing.T) {
	week := "2026-06-22"
	f := newFake([]board.Card{
		{ItemID: "a1", Plan: board.PlanWed, Week: "2026-06-15", Team: "alpha"},
		{ItemID: "a2", Plan: board.PlanFri, Week: "2026-06-15", Team: "alpha", Stage: board.StageDone},
		{ItemID: "a3", Plan: board.PlanWed, Week: week, Team: "alpha"},
		{ItemID: "a4", Plan: board.PlanWed, Week: "2026-06-15", Team: "beta"},
	}, nil)
	carried, err := f2svc(f).CarryWeek(ctx, "acme", 1, "alpha", week)
	if err != nil {
		t.Fatal(err)
	}
	if len(carried) != 1 || carried[0].ItemID != "a1" || carried[0].Week != week {
		t.Fatalf("carried = %+v", carried)
	}
	if f.get("a1").Week != week {
		t.Fatalf("a1 not moved: %+v", f.get("a1"))
	}
	if f.count("SetWeek") != 1 {
		t.Fatalf("only a1 should move; log=%v", f.log)
	}
}

func TestCarryWeekNothingToCarry(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "a1", Plan: board.PlanWed, Week: "2026-06-22", Team: "alpha"}}, nil)
	carried, err := f2svc(f).CarryWeek(ctx, "acme", 1, "alpha", "2026-06-22")
	if err != nil {
		t.Fatal(err)
	}
	if len(carried) != 0 || f.count("SetWeek") != 0 {
		t.Fatalf("nothing to carry; carried=%v log=%v", carried, f.log)
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
