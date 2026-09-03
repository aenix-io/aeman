package board

import "testing"

func TestCarryingNowCountsAcrossEveryTeam(t *testing.T) {
	t.Parallel()
	today := "2026-09-02"

	b := Board{Cards: []Card{
		// The same person, in two teams: the number is theirs, not a team's.
		{ItemID: "a", Assignees: []string{"kvaps"}, Team: "portal"},
		{ItemID: "b", Assignees: []string{"kvaps"}, Team: "features"},
		// Somebody else's.
		{ItemID: "c", Assignees: []string{"lexfrei"}, Team: "portal"},
		// Nobody's: a card waiting for an owner counts against no one.
		{ItemID: "d", Team: "portal"},
	}}
	got := CarryingNow(b, today)
	if got["kvaps"] != 2 || got["lexfrei"] != 1 {
		t.Fatalf("CarryingNow = %v, want kvaps 2 and lexfrei 1", got)
	}
	if len(got) != 2 {
		t.Fatalf("CarryingNow = %v, want nobody else in it", got)
	}
}

func TestCarryingNowLeavesOutWhatIsNotBeingCarried(t *testing.T) {
	t.Parallel()
	today := "2026-09-02"

	for _, tc := range []struct {
		name string
		card Card
	}{
		{"work already finished", Card{Stage: StageDone}},
		{"work at a hundred percent", Card{Progress: 100}},
		{"a card put off to a week that has not arrived", Card{Week: "2026-09-14"}},
		{"a subtask, which rides its parent", Card{Parent: "p1"}},
		{"a card on a personal board", Card{Domain: "~kvaps"}},
		{"the board's own bookkeeping", Card{Title: SprintStateTitle}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := tc.card
			c.ItemID, c.Assignees = "x", []string{"kvaps"}
			if got := CarryingNow(Board{Cards: []Card{c}}, today); len(got) != 0 {
				t.Fatalf("CarryingNow = %v, want nothing", got)
			}
		})
	}
}

func TestCarryingNowCountsWorkOwedAndWorkOfThisWeek(t *testing.T) {
	t.Parallel()
	today := "2026-09-02"
	b := Board{Cards: []Card{
		// Owed in a week gone by: still theirs, and still not done.
		{ItemID: "a", Assignees: []string{"kvaps"}, Week: "2026-08-17"},
		// This week's own.
		{ItemID: "b", Assignees: []string{"kvaps"}, Week: "2026-08-31"},
		// Under way.
		{ItemID: "c", Assignees: []string{"kvaps"}, Progress: 40},
	}}
	if got := CarryingNow(b, today); got["kvaps"] != 3 {
		t.Fatalf("CarryingNow = %v, want 3", got)
	}
}
