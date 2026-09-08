package board

import "testing"

// A team's REACTIVE share is how much of what it closes arrived outside the
// plan — the yellow and red zones: work that turned up that day, or had to
// be done by its end. It is read off the same four complete weeks the
// capacity is, as points: on the production history a third of an
// engineering team's closed points and two fifths of the portal team's came
// in that way, and a week planned to the team's whole capacity is a week
// that cannot absorb them. What can be PLANNED is the capacity less that
// share (PlannableOf).
func TestATeamsReactiveShareIsWhatItClosesOutsideThePlan(t *testing.T) {
	today := "2026-09-08" // Tuesday; the window is 08-10 .. 09-06
	b := Board{Cards: []Card{
		{ItemID: "a", Team: "portal", Zone: ZoneGray, Size: SizeL, Progress: 100, DoneAt: "2026-08-12"},    // 4 planned
		{ItemID: "b", Team: "portal", Zone: ZoneYellow, Size: SizeM, Progress: 100, DoneAt: "2026-08-19"},  // 2 reactive
		{ItemID: "c", Team: "portal", Zone: ZoneRed, Size: SizeM, Progress: 100, DoneAt: "2026-08-27"},     // 2 reactive
		{ItemID: "d", Team: "portal", Zone: ZoneGreen, Size: SizeM, Progress: 100, DoneAt: "2026-09-02"},   // 2 planned (if time left is still the plan)
		{ItemID: "e", Team: "portal", Zone: ZoneRed, Size: SizeXL, Progress: 100, DoneAt: "2026-09-07"},    // this week: out
		{ItemID: "f", Team: "portal", Zone: ZoneRed, Size: SizeXL, Progress: 100, DoneAt: "2026-08-05"},    // too old: out
		{ItemID: "g", Team: "cozystack", Zone: ZoneRed, Size: SizeXL, Progress: 100, DoneAt: "2026-08-20"}, // another team
		{ItemID: "h", Team: "portal", Zone: ZoneRed, Size: SizeXL, Progress: 50, DoneAt: ""},               // open: not closed
	}}
	share, known := ReactiveShareOf(b, "portal", today)
	if !known || share != 40 {
		t.Errorf("portal: reactive share = %d%% (known %v), want 40%% (4 of 10 points)", share, known)
	}
	if share, known := ReactiveShareOf(b, "cozystack", today); !known || share != 100 {
		t.Errorf("cozystack: share = %d%% (known %v), want 100%%", share, known)
	}
	// A team with nothing closed in the window has no share to speak of —
	// unknown, so a client plans against the whole capacity and says so.
	if _, known := ReactiveShareOf(b, "marketing", today); known {
		t.Error("marketing: a team with no record has no known share")
	}
}

// What a team can PLAN for a week is its capacity less the reactive share;
// with no share known, the whole of it.
func TestPlannableIsTheCapacityLessTheReactiveShare(t *testing.T) {
	if got := Plannable(100, 40, true); got != 60 {
		t.Errorf("100 points at 40%% reactive leaves 60 to plan, got %d", got)
	}
	if got := Plannable(100, 40, false); got != 100 {
		t.Errorf("no known share leaves the whole capacity, got %d", got)
	}
	if got := Plannable(0, 40, true); got != 0 {
		t.Errorf("no capacity leaves nothing, got %d", got)
	}
}
