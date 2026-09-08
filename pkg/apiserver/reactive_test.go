package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A team's sprint resource carries, beside the card count the capacity bar
// always had, its week in POINTS and the share of them that arrive outside
// the plan — the two numbers the Triage board needs to say how much of a
// week can still be planned.
func TestSprintCapacityCarriesPointsAndTheReactiveShare(t *testing.T) {
	today := board.TodayIso()
	lastWeek := board.AddDays(board.MondayOf(today), -3)
	b := board.Board{
		SprintStates: map[string]board.SprintState{"portal": {Current: today, ItemID: "s1"}},
		Cards: []board.Card{
			{ItemID: "a", Team: "portal", Zone: board.ZoneGray, Size: board.SizeXL, Progress: 100, DoneAt: lastWeek},
			{ItemID: "b", Team: "portal", Zone: board.ZoneRed, Size: board.SizeXL, Progress: 100, DoneAt: lastWeek},
		},
	}
	var cap *SprintCapacity
	for _, s := range SprintResources(b) {
		if s.Metadata.Team == "portal" {
			cap = s.Spec.Capacity
		}
	}
	if cap == nil {
		t.Fatal("no capacity on the sprint resource")
	}
	// 16 points over four weeks = 4 a week; half of them reactive.
	if cap.Points != 4 || !cap.PointsDerived || cap.Reactive != 50 || !cap.ReactiveKnown {
		t.Fatalf("capacity = %+v, want 4 points a week (derived), 50%% reactive (known)", *cap)
	}
}
