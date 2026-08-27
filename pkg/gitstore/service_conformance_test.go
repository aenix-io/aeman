package gitstore

import (
	"context"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// The rules engine over the git backend, end to end. Nothing here tests a
// rule — those tests live in boardservice — it tests that the backend gives
// the service exactly the shape it expects, through the actions that lean
// hardest on the state-card protocol: roster changes, a review, a carry-over,
// a process with a task, done and reopen.

func newService(t *testing.T) (*boardservice.Service, *Backend, *Repo) {
	t.Helper()
	r := newRepo(t)
	if _, err := r.Commit(Action{Name: "init", Summary: "init"}, []FileWrite{
		{Path: BoardPath, Data: []byte("schema: 1\ntitle: conformance\n")},
		{Path: TeamPath("_"), Data: []byte("rank: a\ncreated: 2026-06-01T08:00:00Z\n")},
	}); err != nil {
		t.Fatal(err)
	}
	be := NewBackend(r, BackendOptions{})
	return boardservice.New(be), be, r
}

func TestServiceRosterThroughTheBackend(t *testing.T) {
	svc, _, r := newService(t)
	ctx := ctxAs("kvaps")

	if err := svc.AddProject(ctx, "acme", 1, "portal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddEpic(ctx, "acme", 1, "Bugs", "portal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddEpic(ctx, "acme", 1, "Bugs", "portal"); err == nil {
		t.Fatal("a duplicate column must be refused by the service")
	}
	card, err := svc.CreateCard(ctx, "acme", 1, boardservice.CreateCardArgs{Title: "leak", Team: "portal", Epic: "Bugs", Project: "portal", Start: "2026-08-24", Day: "2026-08-28"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RenameEpic(ctx, "acme", 1, "portal", "Bugs", "Defects"); err != nil {
		t.Fatal(err)
	}
	b, err := svc.Board(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Epics) != 1 || b.Epics[0].Name != "Defects" {
		t.Fatalf("epics after rename = %+v", b.Epics)
	}
	got, ok := findByID(b, card.ItemID)
	if !ok || got.Epic != "Defects" || got.Project != "portal" {
		t.Fatalf("the card did not follow the rename: %+v", got)
	}
	// One rename = one commit, whatever it touched, when the server scopes it;
	// bare, the service's writes each commit — the files still agree.
	p, _ := CardPath(card.ItemID)
	if data, _ := r.ReadFile(p); !strings.Contains(string(data), "epic: Defects") {
		t.Fatalf("card file not renamed:\n%s", data)
	}
}

func TestServiceDoneAndReopenThroughTheBackend(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := ctxAs("kvaps")
	card, err := svc.CreateCard(ctx, "acme", 1, boardservice.CreateCardArgs{Title: "ship", Team: "portal", Zone: board.ZoneYellow, Start: "2026-08-24", Day: "2026-08-24"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProgress(ctx, "acme", 1, card.ItemID, 40); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProgress(ctx, "acme", 1, card.ItemID, 100); err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme", 1)
	done, _ := findByID(b, card.ItemID)
	if done.Progress != 100 || done.DoneFrom != 40 {
		t.Fatalf("done card = progress %d doneFrom %d", done.Progress, done.DoneFrom)
	}
	// G23 — the reopen reads doneFrom from the file, not from a log that a
	// horizon could have cut.
	if err := svc.Reopen(ctx, "acme", 1, card.ItemID); err != nil {
		t.Fatal(err)
	}
	b, _ = svc.Board(ctx, "acme", 1)
	back, _ := findByID(b, card.ItemID)
	if back.Progress != 40 || back.DoneFrom != 0 {
		t.Fatalf("reopened card = progress %d doneFrom %d, want 40 and cleared", back.Progress, back.DoneFrom)
	}
}

func TestServiceReviewThroughTheBackend(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := ctxAs("kvaps")
	card, err := svc.CreateCard(ctx, "acme", 1, boardservice.CreateCardArgs{Title: "feature", Team: "portal", Zone: board.ZoneYellow, Start: "2026-08-24", Day: "2026-08-24"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProgress(ctx, "acme", 1, card.ItemID, 100); err != nil {
		t.Fatal(err)
	}
	review, err := svc.SendToReview(ctx, "acme", 1, card.ItemID, "timur", "2026-08-25", board.ZoneRed)
	if err != nil {
		t.Fatal(err)
	}
	if review.ReviewOf != card.ItemID || len(review.Assignees) != 1 || review.Assignees[0] != "timur" || review.Team != "portal" {
		t.Fatalf("review card = %+v", review)
	}
	b, _ := svc.Board(ctx, "acme", 1)
	orig, _ := findByID(b, card.ItemID)
	if orig.Stage != board.StageReview {
		t.Fatalf("original not on review: %+v", orig)
	}
	if _, ok := findByID(b, review.ItemID); !ok {
		t.Fatal("review card missing from the board")
	}
	if err := svc.AddNote(ctx, "acme", 1, review.ItemID, "looks fine"); err != nil {
		t.Fatal(err)
	}
	b, _ = svc.Board(ctx, "acme", 1)
	rc, _ := findByID(b, review.ItemID)
	if len(rc.Notes) != 1 || rc.Notes[0].Author != "kvaps" {
		t.Fatalf("note = %+v", rc.Notes)
	}
}

func TestServiceCarryOverThroughTheBackend(t *testing.T) {
	svc, _, r := newService(t)
	ctx := ctxAs("kvaps")
	if err := svc.SetSprintState(ctx, "acme", 1, "portal", "2026-08-20", "2026-08-17"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCard(ctx, "acme", 1, boardservice.CreateCardArgs{Title: "open", Team: "portal", Zone: board.ZoneYellow, Start: "2026-08-20", Day: "2026-08-20", SprintStart: "2026-08-20"}); err != nil {
		t.Fatal(err)
	}
	before := r.Head()
	if _, err := svc.CarryOver(ctx, "acme", 1, "portal", false); err != nil {
		t.Fatal(err)
	}
	if r.Head() == before {
		t.Fatal("carry-over wrote nothing")
	}
	b, _ := svc.Board(ctx, "acme", 1)
	st := b.SprintStates["portal"]
	if st.Current == "2026-08-20" || st.Previous != "2026-08-20" {
		t.Fatalf("pointer did not advance: %+v", st)
	}
	// The team file carries the pointer.
	data, err := r.ReadFile(TeamPath(st.ItemID))
	if err != nil || !strings.Contains(string(data), "previous: 2026-08-20") {
		t.Fatalf("team file: %v\n%s", err, data)
	}
}

func TestServiceProcessAndTaskThroughTheBackend(t *testing.T) {
	svc, _, r := newService(t)
	ctx := ctxAs("kvaps")
	if err := svc.AddProject(ctx, "acme", 1, "portal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddProcess(ctx, "acme", 1, "Invoicing", "portal"); err != nil {
		t.Fatal(err)
	}
	task, err := svc.AddProcessTask(ctx, "acme", 1, "Invoicing", boardservice.TaskArgs{Title: "Invoice ACME", Description: "monthly", Recurrence: "month", Start: "2026-08-03", Team: "portal", Accumulate: true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme", 1)
	if len(b.Processes) != 1 || b.Processes[0].Name != "Invoicing" || b.Processes[0].Project != "portal" {
		t.Fatalf("processes = %+v", b.Processes)
	}
	if len(b.Tasks) != 1 || b.Tasks[0].ItemID != task.ItemID || !b.Tasks[0].Accumulate || b.Tasks[0].Recurrence != "month" {
		t.Fatalf("tasks = %+v", b.Tasks)
	}
	if _, err := r.ReadFile(TaskPath(b.Processes[0].ItemID, task.ItemID)); err != nil {
		t.Fatalf("task file: %v", err)
	}
	if err := svc.DeleteProcessTask(ctx, "acme", 1, task.ItemID); err != nil {
		t.Fatal(err)
	}
	b, _ = svc.Board(ctx, "acme", 1)
	if len(b.Tasks) != 0 {
		t.Fatalf("task not deleted: %+v", b.Tasks)
	}
}

func TestServiceDeleteThroughTheBackend(t *testing.T) {
	svc, _, r := newService(t)
	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "acme", 1, boardservice.CreateCardArgs{Title: "gone", Team: "portal", Zone: board.ZoneGray, Start: "2026-08-24", Day: "2026-08-24"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteCard(ctx, "acme", 1, card.ItemID); err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath(card.ItemID)
	if _, err := r.ReadFile(p); err == nil {
		t.Fatal("deleted card still on disk")
	}
	if _, err := svc.Card(ctx, "acme", 1, card.ItemID); err == nil {
		t.Fatal("deleted card still served")
	}
}
