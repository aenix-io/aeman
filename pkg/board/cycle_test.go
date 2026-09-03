package board

import "testing"

// A turn somebody MOVED is still that occurrence's turn. The projection used
// to ask "does this WEEK hold a turn", so moving one left a ghost standing in
// the week it came from: the same work drawn twice, once as a card where it
// now is and once as a projection where it was.
func TestAMovedTurnLeavesNoGhostBehind(t *testing.T) {
	t.Parallel()
	// Monthly, anchored on the 2nd: the occurrence covers the whole month.
	task := Card{ItemID: "t1", Recurrence: RecurrenceMonth, StartDate: "2026-09-02"}
	// September's turn, moved a week on inside its own cycle.
	moved := Card{ItemID: "i1", Task: "t1", Week: "2026-09-07"}
	b := Board{Cards: []Card{task, moved}}

	got := UpcomingTurns(b, task, "2026-08-31", 8)
	for _, w := range got {
		if w == "2026-08-31" {
			t.Fatalf("September's turn is on the board already: %v", got)
		}
	}
	// October's is still coming — moving September's says nothing about it.
	if len(got) != 1 || got[0] != "2026-09-28" {
		t.Fatalf("UpcomingTurns = %v, want October's alone", got)
	}
}

// The window a turn may be moved within: its own occurrence, from the week it
// came due in through the week before the next one does. It is what keeps a
// turn one occurrence's work — and what the board draws the grip against.
func TestCycleWindowIsTheOccurrenceTheTurnBelongsTo(t *testing.T) {
	t.Parallel()
	monthly := Card{ItemID: "t1", Recurrence: RecurrenceMonth, StartDate: "2026-09-02"}

	from, to := CycleWindow(monthly, "2026-09-07")
	if from != "2026-08-31" || to != "2026-09-21" {
		t.Fatalf("window = %s..%s, want 2026-08-31..2026-09-21", from, to)
	}
	// The occurrence's own first week answers the same window.
	if f, tt := CycleWindow(monthly, "2026-08-31"); f != from || tt != to {
		t.Fatalf("window from its first week = %s..%s, want the same", f, tt)
	}
	// A weekly turn has one week and nowhere to go inside it.
	weekly := Card{ItemID: "t2", Recurrence: RecurrenceWeek, StartDate: "2026-09-02"}
	if f, tt := CycleWindow(weekly, "2026-09-07"); f != "2026-09-07" || tt != "2026-09-07" {
		t.Fatalf("weekly window = %s..%s, want the one week", f, tt)
	}
	// A task with no calendar bounds nothing: per-sprint recurrence has no
	// dates to reckon with, and neither has a task with none at all.
	if f, tt := CycleWindow(Card{ItemID: "t3"}, "2026-09-07"); f != "" || tt != "" {
		t.Fatalf("no recurrence = %s..%s, want no window", f, tt)
	}
	sprint := Card{ItemID: "t4", Recurrence: RecurrenceSprint, StartDate: "2026-09-02"}
	if f, tt := CycleWindow(sprint, "2026-09-07"); f != "" || tt != "" {
		t.Fatalf("per-sprint = %s..%s, want no window", f, tt)
	}
}
