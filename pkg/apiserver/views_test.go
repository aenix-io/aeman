package apiserver

import (
	"testing"

	"github.com/aenix-org/aeman/pkg/board"
)

func TestWithSubtasksDeliversAllChildren(t *testing.T) {
	today := board.TodayIso()
	old := board.AddDays(today, -5)
	future := board.AddDays(today, 3)
	b := board.Board{
		Cards: []board.Card{
			// Carried from the old sprint: startDate keeps its history day.
			{ItemID: "p", Team: "alpha", StartDate: old, SprintStart: today},
			{ItemID: "open", Team: "alpha", Parent: "p", SprintStart: today},
			// Done subtask left behind in the previous sprint by carry-over,
			// and a deferred one: BOTH still ride with the parent — per-day
			// visibility is the client's rendering rule, and the client needs
			// the full set for its derived-progress math.
			{ItemID: "done", Team: "alpha", Parent: "p", SprintStart: old,
				Progress: 100},
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
	for _, id := range []string{"p", "open", "done", "later"} {
		if !got[id] {
			t.Fatalf("%s missing from the view: %v", id, got)
		}
	}
}
