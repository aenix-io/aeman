package board

import "testing"

// stateBoard builds a snapshot with two hidden sprint-state cards (team "A" and
// the no-team group) plus a few ordinary cards, exercising the mapProject split.
func stateBoard() Board {
	return NewBoard(nil, []Card{
		{ItemID: "ss-a", Title: SprintStateTitle, Team: "A", SprintStart: "2026-06-22", StartDate: "2026-06-15"},
		{ItemID: "ss-none", Title: SprintStateTitle, Team: "", SprintStart: "2026-06-20", StartDate: "2026-06-13"},
		{ItemID: "1", Title: "real card", Team: "A", SprintStart: "2026-06-22", StartDate: "2026-06-22"},
		{ItemID: "2", Title: "no team card", Team: "", SprintStart: "2026-06-20", StartDate: "2026-06-20"},
	})
}

func TestNewBoardSplitsSprintState(t *testing.T) {
	b := stateBoard()

	if len(b.Cards) != 2 {
		t.Fatalf("Cards = %d, want 2 (state cards excluded)", len(b.Cards))
	}
	for _, c := range b.Cards {
		if c.Title == SprintStateTitle {
			t.Errorf("sprint-state card %q leaked into Cards", c.ItemID)
		}
	}

	a, ok := b.SprintStates["A"]
	if !ok {
		t.Fatalf("SprintStates missing team A")
	}
	if a.Current != "2026-06-22" || a.Previous != "2026-06-15" || a.ItemID != "ss-a" {
		t.Errorf("team A state = %+v, want current 06-22 / previous 06-15 / itemId ss-a", a)
	}

	none, ok := b.SprintStates[""]
	if !ok {
		t.Fatalf("SprintStates missing the no-team group")
	}
	if none.Current != "2026-06-20" || none.Previous != "2026-06-13" {
		t.Errorf("no-team state = %+v, want current 06-20 / previous 06-13", none)
	}
}

func TestCurrentAndPreviousSprint(t *testing.T) {
	b := stateBoard()

	cases := []struct {
		team         string
		wantCurrent  string
		wantPrevious string
	}{
		{"A", "2026-06-22", "2026-06-15"},
		{"", "2026-06-20", "2026-06-13"}, // the no-team group
		{"missing", "", ""},              // a team with no state card
	}
	for _, c := range cases {
		if got := CurrentSprint(b, c.team); got != c.wantCurrent {
			t.Errorf("CurrentSprint(%q) = %q, want %q", c.team, got, c.wantCurrent)
		}
		if got := PreviousSprint(b, c.team); got != c.wantPrevious {
			t.Errorf("PreviousSprint(%q) = %q, want %q", c.team, got, c.wantPrevious)
		}
	}
}

func TestActiveSprint(t *testing.T) {
	b := NewBoard(nil, []Card{
		// Team A: current 06-26, previous 06-20 (both tracked).
		{ItemID: "ss-a", Title: SprintStateTitle, Team: "A", SprintStart: "2026-06-26", StartDate: "2026-06-20"},
		// Team B: only a current sprint, no previous.
		{ItemID: "ss-b", Title: SprintStateTitle, Team: "B", SprintStart: "2026-06-26", StartDate: ""},
	})
	cases := []struct {
		name      string
		team, day string
		want      string
	}{
		{"day after current returns current", "A", "2026-06-27", "2026-06-26"},
		{"day equal current returns current", "A", "2026-06-26", "2026-06-26"},
		{"day in [previous,current) returns previous", "A", "2026-06-22", "2026-06-20"},
		{"day equal previous returns previous", "A", "2026-06-20", "2026-06-20"},
		{"day before previous returns empty", "A", "2026-06-19", ""},
		{"only-current team before current returns empty", "B", "2026-06-22", ""},
		{"only-current team at current returns current", "B", "2026-06-26", "2026-06-26"},
		{"team with no state (empty pointers) returns empty", "missing", "2026-06-30", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ActiveSprint(b, c.team, c.day); got != c.want {
				t.Errorf("ActiveSprint(%q,%q) = %q, want %q", c.team, c.day, got, c.want)
			}
		})
	}
}
