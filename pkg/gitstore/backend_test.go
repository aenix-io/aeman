package gitstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// Backend satisfies boardservice.Backend over one domain's repository. The
// service speaks the state-card protocol — a stub card whose Title is one of
// the aeman:*-state markers stands for a team, epic, project, deadline or
// process — and the backend routes those onto roster files, so the rules
// engine does not change while the storage does.

var _ boardservice.Backend = (*Backend)(nil)

func newBackend(t *testing.T) (*Backend, *Repo) {
	t.Helper()
	r := newRepo(t)
	seedBoard(t, r)
	clock := at("2026-08-27T12:00:00Z")
	b := NewBackend(r, BackendOptions{Now: func() time.Time { clock = clock.Add(time.Second); return clock }})
	return b, r
}

func ctxAs(login string) context.Context { return board.WithActor(context.Background(), login) }

func TestBackendLoadBoardFromFiles(t *testing.T) {
	be, _ := newBackend(t)
	b, err := be.LoadBoard(context.Background(), "acme", 7)
	if err != nil {
		t.Fatal(err)
	}
	if b.Owner != "acme" || b.Number != 7 {
		t.Fatalf("identity = %s/%d", b.Owner, b.Number)
	}
	if len(b.Cards) != 2 || b.Cards[0].Title != "second" || b.Cards[1].Title != "first" {
		t.Fatalf("cards = %+v", b.Cards)
	}
	st, ok := b.SprintStates["portal"]
	if !ok || st.Current != "2026-08-24" || st.Previous != "2026-08-21" || st.ItemID != "01JB4TEAM" {
		t.Fatalf("sprint state = %+v %v", st, ok)
	}
	if strings.Join(b.TeamOrder, ",") != ",portal" {
		t.Fatalf("team order = %v (rank a then b)", b.TeamOrder)
	}
	if len(b.Epics) != 1 || b.Epics[0].Name != "Bugs" || b.Epics[0].Project != "portal" || b.Epics[0].ItemID != "01JB4EPIC" {
		t.Fatalf("epics = %+v", b.Epics)
	}
	if len(b.Projects) != 1 || b.Projects[0] != "portal" || b.ProjectStates["portal"] != "01JB4PROJ" {
		t.Fatalf("projects = %v %v", b.Projects, b.ProjectStates)
	}
	if len(b.Deadlines) != 1 || b.Deadlines[0].Week != "2026-09-07" || b.Deadlines[0].Project != "portal" || b.Deadlines[0].ItemID != "01JB4DL" {
		t.Fatalf("deadlines = %+v", b.Deadlines)
	}
	if len(b.Processes) != 1 || b.Processes[0].Name != "Invoicing" || b.Processes[0].Project != "portal" || b.Processes[0].ItemID != "01JB4PROC" {
		t.Fatalf("processes = %+v", b.Processes)
	}
	if len(b.Tasks) != 1 || b.Tasks[0].ItemID != "01JB4TASK" || b.Tasks[0].Process != "Invoicing" || b.Tasks[0].Recurrence != "month" {
		t.Fatalf("tasks = %+v", b.Tasks)
	}
}

func TestBackendCreateCardCommitsOnce(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)
	before := r.Head()
	c, err := be.CreateCard(ctx, b, board.CreateInput{Title: "third", Team: "portal", Zone: board.ZoneRed, Start: "2026-08-27", Day: "2026-08-28", SprintStart: "2026-08-24", Assignee: "timur", Body: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ItemID) != 26 {
		t.Fatalf("id = %q", c.ItemID)
	}
	if c.Rank == "" || c.Rank <= "b" {
		t.Fatalf("rank %q must sort after the existing cards (a, b)", c.Rank)
	}
	if c.Title != "third" || c.Zone != board.ZoneRed || c.Description != "do it" || len(c.Assignees) != 1 || c.Assignees[0] != "timur" || c.CreatedAt == "" {
		t.Fatalf("created = %+v", c)
	}
	head := r.Head()
	if head == before {
		t.Fatal("no commit")
	}
	commit, _ := r.CommitObject(head)
	if commit.ParentHashes[0] != before {
		t.Fatal("more than one commit for one create")
	}
	tr := ParseTrailers(commit.Message)
	if tr.Action != "create" || tr.Actor != "kvaps" || len(tr.Cards) != 1 || tr.Cards[0] != c.ItemID {
		t.Fatalf("trailers = %+v", tr)
	}
	p, _ := CardPath(c.ItemID)
	if data, err := r.ReadFile(p); err != nil || !strings.Contains(string(data), "title: third") {
		t.Fatalf("file: %v\n%s", err, data)
	}
	again, _ := be.LoadBoard(ctx, "acme", 7)
	if len(again.Cards) != 3 || again.Cards[2].ItemID != c.ItemID {
		t.Fatalf("new card not last: %+v", again.Cards)
	}
}

// The state-card protocol: creating a stub with a marker title creates the
// roster file, and the loaded board shows the new entry with the file's id.
func TestBackendCreateStateCardsWriteRosterFiles(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)

	proj, err := be.CreateCard(ctx, b, board.CreateInput{Title: board.ProjectStateTitle, Project: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile(ProjectPath(proj.ItemID)); err != nil {
		t.Fatalf("project file: %v", err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	epic, err := be.CreateCard(ctx, b, board.CreateInput{Title: board.EpicStateTitle, Epic: "Reliability", Project: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile(EpicPath(proj.ItemID, epic.ItemID)); err != nil {
		t.Fatalf("epic file under its project: %v", err)
	}
	if _, err := be.CreateCard(ctx, b, board.CreateInput{Title: board.EpicStateTitle, Epic: "X", Project: "nope"}); err == nil {
		t.Fatal("an epic under an unknown project must be refused")
	}
	dl, err := be.CreateCard(ctx, b, board.CreateInput{Title: board.DeadlineStateTitle, Week: "2026-10-05", Project: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile(DeadlinePath(proj.ItemID, dl.ItemID)); err != nil {
		t.Fatalf("deadline file: %v", err)
	}
	proc, err := be.CreateCard(ctx, b, board.CreateInput{Title: board.ProcessStateTitle, Process: "Backups", Project: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	task, err := be.CreateCard(ctx, b, board.CreateInput{Title: board.ProcessTaskTitle, Process: "Backups", Recurrence: "week", Start: "2026-08-24", Team: "portal", Body: "Rotate keys\nAll of them."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile(TaskPath(proc.ItemID, task.ItemID)); err != nil {
		t.Fatalf("task file under its process: %v", err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	if len(b.Projects) != 2 || len(b.Epics) != 2 || len(b.Deadlines) != 2 || len(b.Processes) != 2 || len(b.Tasks) != 2 {
		t.Fatalf("roster after creates: %d projects, %d epics, %d deadlines, %d processes, %d tasks", len(b.Projects), len(b.Epics), len(b.Deadlines), len(b.Processes), len(b.Tasks))
	}
	var found *board.Card
	for i := range b.Tasks {
		if b.Tasks[i].ItemID == task.ItemID {
			found = &b.Tasks[i]
		}
	}
	if found == nil || found.Title != board.ProcessTaskTitle || found.Process != "Backups" || found.Description != "Rotate keys\nAll of them." || found.Recurrence != "week" {
		t.Fatalf("task = %+v", found)
	}
}

// Without a scope every setter is its own commit touching exactly its file.
func TestBackendSettersRewriteOnlyTheCard(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)
	card := b.Cards[1] // "first"
	before := r.Head()
	if err := be.SetProgress(ctx, b, card, 80); err != nil {
		t.Fatal(err)
	}
	if err := be.RenameCard(ctx, b, card, "first, renamed"); err != nil {
		t.Fatal(err)
	}
	paths, _ := r.ChangedPaths(before, r.Head())
	p, _ := CardPath(card.ItemID)
	if len(paths) != 1 || paths[0] != p {
		t.Fatalf("changed paths = %v", paths)
	}
	n := 0
	_ = r.Walk(r.Head(), func(c *object.Commit) (bool, error) {
		if c.Hash == before {
			return false, nil
		}
		n++
		return true, nil
	})
	if n != 2 {
		t.Fatalf("%d commits for two setters without a scope, want 2", n)
	}
	got, _ := be.LoadBoard(ctx, "acme", 7)
	c, _ := findByID(got, card.ItemID)
	if c.Progress != 80 || c.Title != "first, renamed" || c.Zone != board.ZoneYellow || c.Team != "portal" {
		t.Fatalf("card after setters = %+v", c)
	}
}

// G4 — inside a scope every write of one action lands in ONE commit, and a
// change that is not a field diff rides an Aeman-Change trailer.
func TestBackendScopeCommitsOnce(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)
	card := b.Cards[1]
	before := r.Head()

	ctx, flush := WithScope(ctx, Action{Name: "send-to-review", Summary: "send «first» to review", Cards: []string{card.ItemID}})
	if err := be.SetProgress(ctx, b, card, 90); err != nil {
		t.Fatal(err)
	}
	if err := be.SetStage(ctx, b, card, board.StageReview); err != nil {
		t.Fatal(err)
	}
	if err := be.AppendEvent(ctx, b, card, board.Event{Kind: board.EventReviewSent, To: "timur"}); err != nil {
		t.Fatal(err)
	}
	if err := be.AddNote(ctx, b, card, "sent for review"); err != nil {
		t.Fatal(err)
	}
	if r.Head() != before {
		t.Fatal("a scope must not commit before it is flushed")
	}
	h, err := flush()
	if err != nil {
		t.Fatal(err)
	}
	commit, _ := r.CommitObject(h)
	if commit.ParentHashes[0] != before {
		t.Fatal("the scope produced more than one commit")
	}
	tr := ParseTrailers(commit.Message)
	if tr.Action != "send-to-review" || tr.Actor != "kvaps" || len(tr.Changes) != 1 || tr.Changes[0].Kind != board.EventReviewSent || tr.Changes[0].To != "timur" {
		t.Fatalf("trailers = %+v", tr)
	}
	got, _ := be.LoadBoard(ctx, "acme", 7)
	c, _ := findByID(got, card.ItemID)
	// Each staged write saw the one before it: progress survived the stage.
	if c.Progress != 90 || c.Stage != board.StageReview || len(c.Notes) != 1 || c.Notes[0].Body != "sent for review" || c.Notes[0].Author != "kvaps" {
		t.Fatalf("card after scope = %+v", c)
	}
	// A flushed scope with nothing staged makes no commit.
	_, flush2 := WithScope(context.Background(), Action{Name: "noop"})
	if h2, err := flush2(); err != nil || !h2.IsZero() {
		t.Fatalf("empty scope: %v %v", h2, err)
	}
}

func TestBackendNotesLifecycle(t *testing.T) {
	be, _ := newBackend(t)
	ctx := ctxAs("timur")
	b, _ := be.LoadBoard(ctx, "acme", 7)
	card := b.Cards[0]
	if err := be.AddNote(ctx, b, card, "one"); err != nil {
		t.Fatal(err)
	}
	if err := be.AddNote(ctx, b, card, "two"); err != nil {
		t.Fatal(err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	c, _ := findByID(b, card.ItemID)
	if len(c.Notes) != 2 || c.Notes[0].Body != "one" || len(c.Notes[0].ID) != 26 || c.Notes[0].Author != "timur" || c.Notes[0].CreatedAt == "" {
		t.Fatalf("notes = %+v", c.Notes)
	}
	if err := be.EditNote(ctx, b, c, c.Notes[0], "uno"); err != nil {
		t.Fatal(err)
	}
	if err := be.DeleteNote(ctx, b, c, c.Notes[1]); err != nil {
		t.Fatal(err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	c, _ = findByID(b, card.ItemID)
	if len(c.Notes) != 1 || c.Notes[0].Body != "uno" || c.Notes[0].Author != "timur" {
		t.Fatalf("notes after edit/delete = %+v", c.Notes)
	}
	if err := be.DeleteNote(ctx, b, c, board.Note{ID: "01JB4NOSUCHNOTE00000000000"}); err == nil {
		t.Fatal("deleting an unknown note must fail")
	}
}

// G12 consumer — a move rewrites the moved card alone.
func TestBackendMoveCardRewritesOneFile(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)
	third, err := be.CreateCard(ctx, b, board.CreateInput{Title: "third", Team: "portal"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7) // order: second (a), first (b), third
	before := r.Head()
	// Move "third" after "second": second, third, first.
	if err := be.MoveCard(ctx, b, third, b.Cards[0].ItemID); err != nil {
		t.Fatal(err)
	}
	paths, _ := r.ChangedPaths(before, r.Head())
	p, _ := CardPath(third.ItemID)
	if len(paths) != 1 || paths[0] != p {
		t.Fatalf("a move changed %v, want only the moved card", paths)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	if b.Cards[0].Title != "second" || b.Cards[1].Title != "third" || b.Cards[2].Title != "first" {
		t.Fatalf("order after move = %s, %s, %s", b.Cards[0].Title, b.Cards[1].Title, b.Cards[2].Title)
	}
	// To the top.
	if err := be.MoveCard(ctx, b, b.Cards[2], ""); err != nil {
		t.Fatal(err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	if b.Cards[0].Title != "first" {
		t.Fatalf("move to top: %s first", b.Cards[0].Title)
	}
}

func TestBackendSetSprintStateWritesTeamFile(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)
	// Existing team: pointer rewritten in place.
	if err := be.SetSprintState(ctx, b, "portal", "2026-08-31", "2026-08-24"); err != nil {
		t.Fatal(err)
	}
	// New team: its file is created, named, ranked after the others.
	if err := be.SetSprintState(ctx, b, "infra", "2026-08-31", ""); err != nil {
		t.Fatal(err)
	}
	// The no-team group lives in "_".
	if err := be.SetSprintState(ctx, b, "", "2026-08-31", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile(TeamPath("_")); err != nil {
		t.Fatalf("no-team file: %v", err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	if b.SprintStates["portal"].Current != "2026-08-31" || b.SprintStates["portal"].Previous != "2026-08-24" || b.SprintStates["portal"].ItemID != "01JB4TEAM" {
		t.Fatalf("portal = %+v", b.SprintStates["portal"])
	}
	if b.SprintStates["infra"].Current != "2026-08-31" || b.SprintStates[""].Current != "2026-08-31" {
		t.Fatalf("states = %+v", b.SprintStates)
	}
	if strings.Join(b.TeamOrder, ",") != ",portal,infra" {
		t.Fatalf("team order = %v", b.TeamOrder)
	}
}

func TestBackendDeleteCardRemovesFile(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)
	card := b.Cards[0]
	if err := be.DeleteCard(ctx, b, card); err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath(card.ItemID)
	if _, err := r.ReadFile(p); err == nil {
		t.Fatal("deleted card still on disk")
	}
	// A stub for a roster entry deletes its file.
	stub := board.Card{ItemID: "01JB4DL", Title: board.DeadlineStateTitle, Week: "2026-09-07", Project: "portal"}
	if err := be.DeleteCard(ctx, b, stub); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadFile(DeadlinePath("01JB4PROJ", "01JB4DL")); err == nil {
		t.Fatal("deleted deadline still on disk")
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	if len(b.Cards) != 1 || len(b.Deadlines) != 0 {
		t.Fatalf("after deletes: %d cards, %d deadlines", len(b.Cards), len(b.Deadlines))
	}
}

// The roster stubs: rename, re-parent, pause, move a deadline, reorder.
func TestBackendRosterStubs(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)

	epic := board.Card{ItemID: "01JB4EPIC", Title: board.EpicStateTitle, Epic: "Bugs", Project: "portal"}
	if err := be.SetEpic(ctx, b, epic, "Defects"); err != nil {
		t.Fatal(err)
	}
	proj := board.Card{ItemID: "01JB4PROJ", Title: board.ProjectStateTitle, Project: "portal"}
	if err := be.SetProject(ctx, b, proj, "Portal"); err != nil {
		t.Fatal(err)
	}
	proc := board.Card{ItemID: "01JB4PROC", Title: board.ProcessStateTitle, Process: "Invoicing", Project: "portal"}
	if err := be.SetPaused(ctx, b, proc, true); err != nil {
		t.Fatal(err)
	}
	if err := be.SetProcess(ctx, b, proc, "Billing"); err != nil {
		t.Fatal(err)
	}
	dl := board.Card{ItemID: "01JB4DL", Title: board.DeadlineStateTitle, Week: "2026-09-07", Project: "portal"}
	if err := be.SetWeek(ctx, b, dl, "2026-09-14"); err != nil {
		t.Fatal(err)
	}
	task := board.Card{ItemID: "01JB4TASK", Title: board.ProcessTaskTitle, Process: "Invoicing"}
	if err := be.SetAccumulate(ctx, b, task, true); err != nil {
		t.Fatal(err)
	}

	b, _ = be.LoadBoard(ctx, "acme", 7)
	if b.Epics[0].Name != "Defects" || b.Projects[0] != "Portal" || b.Processes[0].Name != "Billing" || !b.Processes[0].Paused || b.Deadlines[0].Week != "2026-09-14" || !b.Tasks[0].Accumulate {
		t.Fatalf("roster after stubs: epics=%+v projects=%v processes=%+v deadlines=%+v tasks=%+v", b.Epics, b.Projects, b.Processes, b.Deadlines, b.Tasks[0].Accumulate)
	}
	// Renaming a project file does not move anything: its id and path stay.
	if _, err := r.ReadFile(ProjectPath("01JB4PROJ")); err != nil {
		t.Fatalf("project file moved on rename: %v", err)
	}

	// Reordering teams and epics goes through MoveCard on their stubs.
	if _, err := be.CreateCard(ctx, b, board.CreateInput{Title: board.EpicStateTitle, Epic: "Docs", Project: "Portal"}); err != nil {
		t.Fatal(err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	if b.Epics[0].Name != "Defects" || b.Epics[1].Name != "Docs" {
		t.Fatalf("epics before move = %+v", b.Epics)
	}
	docs := board.Card{ItemID: b.Epics[1].ItemID, Title: board.EpicStateTitle, Epic: "Docs", Project: "Portal"}
	if err := be.MoveCard(ctx, b, docs, ""); err != nil {
		t.Fatal(err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	if b.Epics[0].Name != "Docs" {
		t.Fatalf("epic move to top: %+v", b.Epics)
	}
	team := board.Card{ItemID: "01JB4TEAM", Title: board.SprintStateTitle, Team: "portal"}
	if err := be.MoveCard(ctx, b, team, ""); err != nil {
		t.Fatal(err)
	}
	b, _ = be.LoadBoard(ctx, "acme", 7)
	if b.TeamOrder[0] != "portal" {
		t.Fatalf("team move to top: %v", b.TeamOrder)
	}
}

// LoadCards is the partial read: the asked-for cards, in the order asked,
// deleted ones omitted.
func TestBackendLoadCards(t *testing.T) {
	be, _ := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme", 7)
	got, err := be.LoadCards(ctx, b, []string{b.Cards[1].ItemID, "01JB4NOSUCHCARD0000000000A", b.Cards[0].ItemID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ItemID != b.Cards[1].ItemID || got[1].ItemID != b.Cards[0].ItemID {
		t.Fatalf("LoadCards = %+v", got)
	}
}

func findByID(b board.Board, id string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ItemID == id {
			return c, true
		}
	}
	return board.Card{}, false
}
