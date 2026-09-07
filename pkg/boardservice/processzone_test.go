package boardservice

import (
	"context"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A process TURN is planned work by definition — the process is the plan — so
// it is filed carrying that zone rather than none. An owned turn is dated
// across its week and lands on somebody's day board, where the four bands are
// all there is: filed zone-less it arrived in the middle of a working day as
// work in no band, drawn in the planned one by each client's own fallback and
// reported by the API as having no zone at all.
//
// An UNOWNED turn is filed the same way: it waits in its week for whoever
// takes it, and the day it is taken is not the day to start asking what kind
// of work it was — a process turn is the plan, whoever ends up doing it.
func TestATurnIsFiledAsPlannedWork(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())

	owned, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Owned", Recurrence: "week", Start: week, Team: "alpha", Assignee: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	loose, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Unowned", Recurrence: "week", Start: week, Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme")

	for _, c := range []struct {
		name string
		task string
	}{{"an owned turn", owned.ItemID}, {"an unowned turn", loose.ItemID}} {
		its := board.Iterations(b, c.task)
		if len(its) != 1 {
			t.Fatalf("%s: iterations = %d", c.name, len(its))
		}
		if its[0].Zone != board.ZoneGray {
			t.Errorf("%s is filed with zone %q, want planned work", c.name, its[0].Zone)
		}
	}
}
