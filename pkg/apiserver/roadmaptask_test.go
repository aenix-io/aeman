package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A task born on the Roadmap under an epic with no dates is PARKED in the
// no-team group's backlog (K3): it stands in that backlog — where a lead
// takes it into work — and on the Roadmap under its parent, and on no day
// board until somebody gives it a day, a sprint or a person.
func TestARoadmapBornTaskStandsInTheNoTeamBacklogAndOnTheRoadmap(t *testing.T) {
	t.Parallel()
	today := board.TodayIso()
	b := board.NewBoard([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "born", Title: "Receipts", Parent: "epic", Parked: true},
	})
	in := func(sel Selector) bool {
		for _, c := range FilterCards(b, sel) {
			if c.ItemID == "born" {
				return true
			}
		}
		return false
	}
	if !in(Selector{View: "backlog", Team: ",other"}) {
		t.Error("the Roadmap-born task is not in the no-team group's backlog")
	}
	if !in(Selector{View: "roadmap"}) {
		t.Error("the Roadmap-born task is not on the Roadmap under its epic")
	}
	if in(Selector{View: "team", Team: "", Day: today}) || in(Selector{View: "me", Day: today}) {
		t.Error("a parked task is on no day board")
	}
}

// The Backlog card shows the nearest planning card above a task as a tag and
// that card's end date as the task's due date, so the lineage carries each
// ancestor's end (status.lineage[].end) — absent when the ancestor has none.
func TestTheLineageCarriesEachAncestorsEndDate(t *testing.T) {
	t.Parallel()
	b := board.NewBoard([]board.Card{
		{ItemID: "goal", Title: "Grow", Type: board.TypeGoal},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "goal", StartDate: "2026-11-02", Day: "2026-11-27"},
		{ItemID: "task", Title: "Receipts", Parent: "epic", Team: "alpha", Parked: true},
	})
	c, _ := board.Find(b, "task")
	got := CardResource(b, c).Status.Lineage
	if len(got) != 2 || got[0].UID != "goal" || got[0].End != "" || got[1].UID != "epic" || got[1].End != "2026-11-27" {
		t.Fatalf("lineage = %+v, want goal (no end) then epic ending 2026-11-27", got)
	}
}
