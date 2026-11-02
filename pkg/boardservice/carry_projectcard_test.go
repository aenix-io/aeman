package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A project card is no sprint's work (H4): it has no team, so it sits in the
// no-team group by default, and its dates are its row on the Project board,
// not a day somebody planned. The "adopt a sprint-less day card whose day
// has arrived" rule of the no-team carry-over used to take it for exactly
// that — a next-sprint create — and give it a sprint every other door
// refuses to, logging a sprint event on it at every carry-over since.
func TestCarryOverLeavesAProjectCardAlone(t *testing.T) {
	old := "2026-01-01"
	today := board.TodayIso()
	f := newFake([]board.Card{
		// In a column, dated today, no team, no sprint — the shape the Project
		// board makes.
		{ItemID: "plan", Title: "a plan", Scope: "Infra", Project: "P", StartDate: today, Day: today},
		// A real next-sprint create in the no-team group: adopted, as before.
		{ItemID: "next", Title: "next sprint", StartDate: today, Day: today},
	}, map[string]board.SprintState{"": {Current: old}})
	if _, err := f2svc(f).CarryOver(ctx, "acme", "", false); err != nil {
		t.Fatal(err)
	}
	if f.saw("SetSprintStart plan " + today) {
		t.Fatalf("the carry-over gave a project card a sprint; log=%v", f.log)
	}
	if !f.saw("SetSprintStart next " + today) {
		t.Fatalf("the sprint-less day card was not adopted; log=%v", f.log)
	}
}
