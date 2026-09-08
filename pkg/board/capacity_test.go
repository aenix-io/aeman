package board

import "testing"

// A person's CAPACITY is how many points a week they get through, and it is
// the roster's number — somebody's judgement, written down. The board does
// NOT derive one from its own record. It could: the arithmetic is a median
// over the last four complete weeks, and it is written down in the
// derive-capacity skill for a lead to run. But a number the board prints
// beside a name reads as a fact, and the record it would come from is
// young — doneAt has only been written since a board started keeping it, so
// the window is mostly empty of RECORDS while being full of WORK, and the
// median of what little is there lands far under the truth. Better to say
// nothing: 0 is "nobody has said", which a client draws as the load alone.
func TestAPersonsCapacityIsTheRostersNumberAndNothingElse(t *testing.T) {
	b := Board{
		People: map[string]Person{"lead": {Capacity: 30}, "zeroed": {Capacity: 0}},
		Cards: []Card{
			// A long, sized, closed record — and no roster number. It counts
			// for the load and for nothing else: the board does not guess.
			{ItemID: "a1", Assignees: []string{"kvaps"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-11"},
			{ItemID: "a2", Assignees: []string{"kvaps"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-12"},
			{ItemID: "b1", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-08-18"},
			{ItemID: "b2", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-08-21"},
			// The lead has a record too, and it changes nothing either way.
			{ItemID: "l1", Assignees: []string{"lead"}, Size: SizeS, Progress: 100, DoneAt: "2026-08-20"},
		},
	}
	if got := CapacityOfPerson(b, "lead"); got != 30 {
		t.Errorf("lead: capacity = %d, want the roster's 30", got)
	}
	if got := CapacityOfPerson(b, "kvaps"); got != 0 {
		t.Errorf("kvaps: capacity = %d, want 0 — a record is not a capacity", got)
	}
	// A file that says 0 and no file at all are the same thing: unset.
	if got := CapacityOfPerson(b, "zeroed"); got != 0 {
		t.Errorf("zeroed: capacity = %d, want 0", got)
	}
	if got := CapacityOfPerson(b, "nobody"); got != 0 {
		t.Errorf("a login the board never saw has capacity 0, got %d", got)
	}
}

// A team's points a week are its people's numbers added up, so a team of
// people nobody has sized has no capacity either — 0, not a guess from the
// record its people happen to have left.
func TestATeamOfUnsetPeopleHasNoCapacity(t *testing.T) {
	b := Board{Cards: []Card{
		{ItemID: "a", Team: "platform", Assignees: []string{"x"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-11"},
		{ItemID: "b", Team: "platform", Assignees: []string{"y"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-18"},
	}}
	if got, _ := PointsAWeekOf(b, "platform", "2026-09-08"); got != 0 {
		t.Errorf("points a week = %d, want 0 until somebody sets a number", got)
	}
	b.People = map[string]Person{"x": {Capacity: 20}}
	if got, _ := PointsAWeekOf(b, "platform", "2026-09-08"); got != 20 {
		t.Errorf("points a week = %d, want the one number that is set", got)
	}
}
