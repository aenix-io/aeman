package board

import "testing"

// A person's CAPACITY is how many points a week they get through. The
// roster's number wins when somebody set one — a lead knows a person is on a
// half week, or new, or covering for two — and otherwise the board reads it
// off its own record: the median of the points the person closed in each of
// the last four complete weeks in which they closed anything. Weeks with
// nothing closed are left out on purpose: a week off is not a slow week, and
// the number beside a person on Monday must not be the memory of their
// holiday.
func TestAPersonsCapacityIsTheRostersNumberOrWhatTheyHaveBeenClosing(t *testing.T) {
	today := "2026-09-08" // a Tuesday; the last complete weeks are 08-10 .. 08-31
	b := Board{
		People: map[string]Person{"lead": {Capacity: 30}},
		Cards: []Card{
			// kvaps: 4 weeks, points 10 / 20 / 0 (away) / 40 → median of {10,20,40} = 20
			{ItemID: "a1", Assignees: []string{"kvaps"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-11"},
			{ItemID: "a2", Assignees: []string{"kvaps"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-12"},
			{ItemID: "a3", Assignees: []string{"kvaps"}, Size: SizeM, Progress: 100, DoneAt: "2026-08-14"},
			{ItemID: "b1", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-08-18"},
			{ItemID: "b2", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-08-21"},
			{ItemID: "b3", Assignees: []string{"kvaps"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-22"},
			{ItemID: "d1", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-09-01"},
			{ItemID: "d2", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-09-02"},
			{ItemID: "d3", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-09-03"},
			{ItemID: "d4", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-09-04"},
			{ItemID: "d5", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-09-05"},
			// this week does not count: it is not over
			{ItemID: "e1", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-09-07"},
			// too old: five weeks back
			{ItemID: "z1", Assignees: []string{"kvaps"}, Size: SizeXL, Progress: 100, DoneAt: "2026-08-05"},
			// the lead has a record too, and it loses to the roster's number
			{ItemID: "l1", Assignees: []string{"lead"}, Size: SizeS, Progress: 100, DoneAt: "2026-08-20"},
			// an UNSIZED closed card still weighs the default, so a week with a
			// closure in it always has something to average
			{ItemID: "n1", Assignees: []string{"newbie"}, Progress: 100, DoneAt: "2026-08-27"},
		},
	}
	if got, derived := CapacityOfPerson(b, "kvaps", today); got != 20 || !derived {
		t.Errorf("kvaps: capacity = %d (derived %v), want 20 from the median of 10/20/40", got, derived)
	}
	if got, derived := CapacityOfPerson(b, "lead", today); got != 30 || derived {
		t.Errorf("lead: capacity = %d (derived %v), want the roster's 30", got, derived)
	}
	// One closure of an unsized card is still a week worth two points: the
	// default is what makes a board nobody has sized readable at all.
	if got, derived := CapacityOfPerson(b, "newbie", today); got != 2 || !derived {
		t.Errorf("newbie: capacity = %d (derived %v), want the default 2, derived", got, derived)
	}
	// Nothing closed at all is nothing to know: zero, and derived, so a client
	// can show "no history" rather than a limit of none.
	if got, derived := CapacityOfPerson(b, "nobody", today); got != 0 || !derived {
		t.Errorf("a login the board never saw has capacity 0 derived, got %d (%v)", got, derived)
	}
}

// The window is the four complete weeks before this one — never the running
// week, which would read a Monday as a slow week — and the median is over the
// weeks in the window the person closed anything in, so a fortnight away
// leaves two weeks to average rather than two zeros to sink the number.
func TestACapacityWindowSkipsThisWeekAndWeeksAway(t *testing.T) {
	b := Board{Cards: []Card{
		{ItemID: "a", Assignees: []string{"x"}, Size: SizeL, Progress: 100, DoneAt: "2026-08-13"},  // 8 weeks back? no: week of 08-10
		{ItemID: "b", Assignees: []string{"x"}, Size: SizeM, Progress: 100, DoneAt: "2026-08-31"},  // week of 08-31
		{ItemID: "c", Assignees: []string{"x"}, Size: SizeXL, Progress: 100, DoneAt: "2026-09-08"}, // this week: out
	}}
	// weeks 08-10 (4) and 08-31 (2) count; 08-17 and 08-24 had nothing and are skipped
	if got, _ := CapacityOfPerson(b, "x", "2026-09-08"); got != 3 {
		t.Errorf("median of {4, 2} = 3, got %d", got)
	}
	// On Monday the same window applies: the week that just began is not a record.
	if got, _ := CapacityOfPerson(b, "x", "2026-09-07"); got != 3 {
		t.Errorf("on Monday, got %d, want 3", got)
	}
}

// Points closed are counted with the umbrella rule, like everything else: a
// parent closed with sized subtasks weighs its children, once.
func TestClosedPointsUseTheUmbrellaRule(t *testing.T) {
	b := Board{Cards: []Card{
		{ItemID: "p", Assignees: []string{"x"}, Size: SizeXL, Progress: 100, DoneAt: "2026-08-20"},
		{ItemID: "k1", Parent: "p", Assignees: []string{"x"}, Size: SizeS, Progress: 100, DoneAt: "2026-08-19"},
		{ItemID: "k2", Parent: "p", Assignees: []string{"x"}, Size: SizeS, Progress: 100, DoneAt: "2026-08-20"},
	}}
	if got, _ := CapacityOfPerson(b, "x", "2026-09-08"); got != 2 {
		t.Errorf("the umbrella closed at its children's weight once, got %d want 2", got)
	}
}
