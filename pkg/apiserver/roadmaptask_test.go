package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A task born on the Roadmap under an epic has no team, no day, no week and
// no sprint: nobody has said WHEN. It therefore stands in the Triage strip
// of the no-team group — the inbox a lead places work from — and on the
// Roadmap under its parent; the day boards do not draw it until somebody
// dates it, which is what the strip is for.
func TestARoadmapBornTaskStandsInTheStripAndOnTheRoadmap(t *testing.T) {
	t.Parallel()
	today := board.TodayIso()
	b := board.NewBoard([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "born", Title: "Receipts", Parent: "epic"},
	})
	in := func(sel Selector) bool {
		for _, c := range FilterCards(b, sel) {
			if c.ItemID == "born" {
				return true
			}
		}
		return false
	}
	if !in(Selector{View: "triage", Team: "", From: board.MondayOf(today), Weeks: 1}) {
		t.Error("the Roadmap-born task is not in the no-team group's Triage strip")
	}
	if !in(Selector{View: "roadmap"}) {
		t.Error("the Roadmap-born task is not on the Roadmap under its epic")
	}
	if in(Selector{View: "team", Team: "", Day: today}) || in(Selector{View: "me", Day: today}) {
		t.Error("nobody has said when: a Roadmap-born task is on no day board")
	}
}
