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
		People:       map[string]board.Person{"kvaps": {Capacity: 30}},
		Cards: []board.Card{
			{ItemID: "a", Team: "portal", Assignees: []string{"kvaps"}, Zone: board.ZoneGray,
				Size: board.SizeXL, Progress: 100, DoneAt: lastWeek},
			{ItemID: "b", Team: "portal", Assignees: []string{"kvaps"}, Zone: board.ZoneRed,
				Size: board.SizeXL, Progress: 100, DoneAt: lastWeek},
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
	// The team is one person, whose capacity a lead set to 30; half of what
	// the team closed came in outside the plan.
	if cap.Points != 30 || !cap.PointsDerived || cap.Reactive != 50 || !cap.ReactiveKnown {
		t.Fatalf("capacity = %+v, want the person's 30 points a week, 50%% reactive (known)", *cap)
	}
}
