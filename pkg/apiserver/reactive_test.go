package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A team's sprint resource carries, beside the card count the capacity bar
// always had, its week in POINTS — the number somebody set for the team, and
// the one the Triage board holds a week's scheduled points against.
//
// The two are different measurements, not two views of one, and the points
// are never derived: a team whose number nobody has set carries none, whatever
// its people have closed.
func TestSprintCapacityCarriesTheTeamsPoints(t *testing.T) {
	today := board.TodayIso()
	lastWeek := board.AddDays(board.MondayOf(today), -3)
	b := board.Board{
		SprintStates: map[string]board.SprintState{
			"portal": {Current: today, ItemID: "s1", Capacity: board.Capacity{Points: 30}},
			"cozy":   {Current: today, ItemID: "s2"},
		},
		People: map[string]board.Person{"kvaps": {Capacity: 30}},
		Cards: []board.Card{
			{ItemID: "a", Team: "cozy", Assignees: []string{"kvaps"}, Zone: board.ZoneGray,
				Size: board.SizeXL, Progress: 100, DoneAt: lastWeek},
		},
	}
	byTeam := map[string]*SprintCapacity{}
	for _, s := range SprintResources(b) {
		byTeam[s.Metadata.Team] = s.Spec.Capacity
	}
	if cap := byTeam["portal"]; cap == nil || cap.Points != 30 {
		t.Fatalf("portal = %+v, want the 30 points a week somebody set", cap)
	}
	// A team with a person and a closed record, and no number: none. The
	// arithmetic that used to fill this in lives in the derive-capacity skill,
	// where how thin the record was can be said out loud.
	if cap := byTeam["cozy"]; cap == nil || cap.Points != 0 {
		t.Fatalf("cozy = %+v, want no points until somebody says", cap)
	}
}
