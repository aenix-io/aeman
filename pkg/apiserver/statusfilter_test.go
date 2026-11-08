package apiserver

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// O14 — the Roadmap's STATUS filter, over each task's work status
// (board.WorkStatus): a task matches by its own, a subtask rides its parent
// task, a review card follows the task it reviews, and a plan item matches
// when any task under it does — a plan item with no tasks matches no status
// at all. Every match comes with its ancestors; several statuses are OR, and
// the filter is AND with team, customer and owner.
func statusBoard() board.Board {
	const day = "2026-11-02"
	b := board.NewBoard([]board.Card{
		{ItemID: "goal", Title: "Grow", Type: board.TypeGoal},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "goal", Team: "portal"},
		{ItemID: "work", Title: "Receipts", Parent: "epic", SprintStart: day, StartDate: day},
		{ItemID: "kid", Title: "Receipts PDF", Parent: "work", Parked: true},
		// A review filed under the epic by a direct writer (the Roadmap lists
		// it then): parked itself, it follows the task it reviews.
		{ItemID: "rev", Title: "review: Receipts", ReviewOf: "work", Parent: "epic", Assignees: []string{"bob"}, Parked: true},
		{ItemID: "shelf", Title: "Invoices", Parent: "epic", Team: "cozystack", Parked: true},
		{ItemID: "fin", Title: "Refunds", Parent: "epic", Progress: 100, StartDate: day},
		{ItemID: "empty", Title: "Docs", Type: board.TypeEpic, Parent: "goal"},
		{ItemID: "loose", Title: "Loose", Team: "ops", SprintStart: day},
	})
	return b
}

func TestTheRoadmapStatusFilter(t *testing.T) {
	t.Parallel()
	b := statusBoard()
	for _, tc := range []struct {
		status string
		want   []string
	}{
		// No filter: the listing as it was.
		{"", []string{"goal", "epic", "work", "kid", "rev", "shelf", "fin", "empty", "loose"}},
		// The work in hand, with its ancestors: the subtask rides its task
		// (parked itself), the review follows the task it reviews.
		{"in-progress", []string{"goal", "epic", "work", "kid", "rev", "loose"}},
		{"backlog", []string{"goal", "epic", "shelf"}},
		{"done", []string{"goal", "epic", "fin"}},
		// Hide done: everything but the finished task — and the plan item
		// with no tasks, which matches no status.
		{"in-progress,backlog", []string{"goal", "epic", "work", "kid", "rev", "shelf", "loose"}},
		{"in-progress,backlog,done", []string{"goal", "epic", "work", "kid", "rev", "shelf", "fin", "loose"}},
	} {
		if got := roadmapIDs(b, Selector{Status: tc.status}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("status=%q = %v, want %v", tc.status, got, tc.want)
		}
	}
	// AND with the team filter: the epic's own team draws the plan, the task
	// must be in progress.
	if got := roadmapIDs(b, Selector{Status: "backlog", Team: "cozystack"}); !reflect.DeepEqual(got, []string{"goal", "epic", "shelf"}) {
		t.Errorf("backlog of cozystack = %v", got)
	}
	if got := roadmapIDs(b, Selector{Status: "done", Team: "ops"}); len(got) != 0 {
		t.Errorf("nothing of ops is done, got %v", got)
	}
	// A filter is the server's alone: the client cannot mirror membership.
	if (Selector{View: "roadmap", Status: "done"}).Mirrored() {
		t.Error("a status-filtered roadmap is mirrored by the client")
	}
}

func TestTheStatusSelectorIsReadAndChecked(t *testing.T) {
	t.Parallel()
	sel, err := ParseSelector(url.Values{"view": {"roadmap"}, "status": {"in-progress,backlog"}})
	if err != nil || sel.Status != "in-progress,backlog" {
		t.Fatalf("parsed %+v, %v", sel, err)
	}
	if _, err := ParseSelector(url.Values{"view": {"roadmap"}, "status": {"in-progress,todo"}}); err == nil {
		t.Fatal("an unknown status was accepted")
	}
	if _, err := ParseSelector(url.Values{"view": {"roadmap"}, "status": {""}}); err != nil {
		t.Fatalf("an empty status is no filter: %v", err)
	}
}
