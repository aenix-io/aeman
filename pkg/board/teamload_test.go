package board

import "testing"

// A TEAM's capacity is its PEOPLE's, added up — not a number of its own and
// not a reading of the team's own closed record. A team is the people in it,
// so a person away is a team short by exactly what that person gets through,
// and a lead who fixes one person's number moves the team's by the same
// amount. Nothing has to be kept in step by hand.
//
// A person split across teams is split by their RECORD: their capacity is
// shared out in proportion to the points they closed in each team over the
// same four weeks, so the shares add back up to the person and no team
// counts the whole of somebody it only half has.
func TestATeamsCapacityIsThePeopleInIt(t *testing.T) {
	today := "2026-09-08" // Tuesday; the window is 08-10 .. 09-06
	b := Board{
		SprintStates: map[string]SprintState{"portal": {Current: today}, "cozy": {Current: today}},
		People:       map[string]Person{"solo": {Capacity: 10}, "split": {Capacity: 20}},
		Cards: []Card{
			// solo works only for portal
			{ItemID: "a", Team: "portal", Assignees: []string{"solo"}, Size: SizeM, Progress: 100, DoneAt: "2026-08-12"},
			// split closed three quarters of their points in portal, a quarter in cozy
			{ItemID: "b", Team: "portal", Assignees: []string{"split"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-19"},
			{ItemID: "c", Team: "portal", Assignees: []string{"split"}, Size: SizeM, Progress: 100, DoneAt: "2026-08-20"},
			{ItemID: "d", Team: "cozy", Assignees: []string{"split"}, Size: SizeM, Progress: 100, DoneAt: "2026-08-21"},
		},
	}
	// portal = solo's 10 + three quarters of split's 20 = 25
	if got, derived := PointsAWeekOf(b, "portal", today); got != 25 || !derived {
		t.Errorf("portal = %d (derived %v), want 25 — solo's 10 plus split's ¾ of 20", got, derived)
	}
	// cozy = the remaining quarter of split's 20
	if got, _ := PointsAWeekOf(b, "cozy", today); got != 5 {
		t.Errorf("cozy = %d, want 5 — split's ¼ of 20", got)
	}
	// The shares add back up to each person: nobody is counted twice.
	portal, _ := PointsAWeekOf(b, "portal", today)
	cozy, _ := PointsAWeekOf(b, "cozy", today)
	if portal+cozy != 30 {
		t.Errorf("the teams together = %d, want the people together (10+20)", portal+cozy)
	}
}

// A person with no closed record in the window is not invisible: they are
// carrying work for a team right now, and that team is the one their whole
// capacity counts for. Without this a newcomer — whose capacity a lead has
// just set BECAUSE there is no record — would add nothing to their team.
func TestAPersonWithNoRecordCountsWhereTheyAreCarrying(t *testing.T) {
	today := "2026-09-08"
	b := Board{
		SprintStates: map[string]SprintState{"portal": {Current: today}},
		People:       map[string]Person{"newbie": {Capacity: 8}},
		Cards: []Card{
			{ItemID: "a", Team: "portal", Assignees: []string{"newbie"}, Size: SizeM, SprintStart: today},
		},
	}
	if got, _ := PointsAWeekOf(b, "portal", today); got != 8 {
		t.Errorf("portal = %d, want the newcomer's whole 8", got)
	}
}

// A team nobody works for is 0, and says so as DERIVED: the board knows of
// no limit rather than a limit of none — the same answer a person with no
// history gets, for the same reason.
func TestATeamWithNobodyInItHasNoCapacity(t *testing.T) {
	b := Board{SprintStates: map[string]SprintState{"empty": {Current: "2026-09-08"}}}
	if got, derived := PointsAWeekOf(b, "empty", "2026-09-08"); got != 0 || !derived {
		t.Errorf("an empty team = %d (derived %v), want 0 derived", got, derived)
	}
}
