package apiserver

import (
	"testing"

	"github.com/aenix-org/aeman/pkg/board"
)

func TestWithSubtasksDayGate(t *testing.T) {
	today := board.TodayIso()
	old := board.AddDays(today, -5)
	future := board.AddDays(today, 3)
	b := board.Board{
		Cards: []board.Card{
			// Carried from the old sprint: startDate keeps its history day.
			{ItemID: "p", Team: "alpha", StartDate: old, SprintStart: today},
			// Open subtask riding the current sprint: visible today.
			{ItemID: "open", Team: "alpha", Parent: "p", SprintStart: today},
			// Done subtask left behind in the previous sprint by carry-over:
			// hidden today, visible on the old sprint's days.
			{ItemID: "done", Team: "alpha", Parent: "p", SprintStart: old,
				Progress: 100},
			// Deferred subtask: hidden until its future day arrives.
			{ItemID: "later", Team: "alpha", Parent: "p", SprintStart: today,
				StartDate: future},
		},
		SprintStates: map[string]board.SprintState{
			"alpha": {Current: today, Previous: old},
		},
	}
	got := map[string]bool{}
	for _, c := range FilterCards(b, Selector{View: "team", Team: "alpha", Day: today}) {
		got[c.ItemID] = true
	}
	if !got["p"] || !got["open"] {
		t.Fatalf("parent/open missing today: %v", got)
	}
	if got["done"] {
		t.Fatal("done subtask from the previous sprint shown today")
	}
	if got["later"] {
		t.Fatal("future-scheduled subtask shown today")
	}
	// The old sprint's day still shows the done subtask under the parent.
	oldDay := map[string]bool{}
	for _, c := range FilterCards(b, Selector{View: "team", Team: "alpha", Day: old}) {
		oldDay[c.ItemID] = true
	}
	if !oldDay["done"] {
		t.Fatalf("done subtask missing on its own sprint day: %v", oldDay)
	}
}
