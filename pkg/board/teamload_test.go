package board

import "testing"

// A TEAM's capacity in POINTS is a number somebody set, exactly like a
// person's. It was arithmetic once — the people's capacities added up, each
// person split between their teams in proportion to what they had closed in
// each over four weeks, the whole then cut by the share history said arrives
// unplanned. Three derivations stacked on one record, and that record is
// eleven days old on a real board: one busy week could hand a person's whole
// number to one team. The same reason the person's own number stopped being
// derived applies twice over here.
//
// So the board stores the number and does no arithmetic. Working the number
// OUT is the derive-capacity skill's job — it can say how thin the record it
// used was, which the board never could.
func TestATeamsPointsAWeekIsTheNumberSomebodySet(t *testing.T) {
	today := "2026-09-08"
	b := Board{
		SprintStates: map[string]SprintState{
			"portal": {Current: today, Capacity: Capacity{Points: 40}},
			"cozy":   {Current: today},
		},
		People: map[string]Person{"someone": {Capacity: 20}},
		Cards: []Card{
			// A long closed record in cozy changes nothing: a team's number is
			// not read off its people, or off its own history.
			{ItemID: "a", Team: "cozy", Assignees: []string{"someone"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-19"},
			{ItemID: "b", Team: "cozy", Assignees: []string{"someone"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-26"},
		},
	}
	if got := PointsAWeekOf(b, "portal"); got != 40 {
		t.Errorf("portal = %d, want the 40 somebody set", got)
	}
	if got := PointsAWeekOf(b, "cozy"); got != 0 {
		t.Errorf("cozy = %d, want 0 — nobody has said, and the record is not an answer", got)
	}
	if got := PointsAWeekOf(b, "nosuchteam"); got != 0 {
		t.Errorf("a team the board never heard of = %d, want 0", got)
	}
}

// 0 is "nobody has said", which a client draws as the week's points ALONE —
// "12", never "12/0", which would read as a week with no room at all. It is
// the same rule the person's number follows, for the same reason.
func TestATeamWithNoNumberHasNone(t *testing.T) {
	b := Board{SprintStates: map[string]SprintState{"empty": {Current: "2026-09-08"}}}
	if got := PointsAWeekOf(b, "empty"); got != 0 {
		t.Errorf("an empty team = %d, want 0", got)
	}
	b.SprintStates["empty"] = SprintState{Capacity: Capacity{Points: 7}}
	if got := PointsAWeekOf(b, "empty"); got != 7 {
		t.Errorf("a team with a number = %d, want the 7 somebody set", got)
	}
}
