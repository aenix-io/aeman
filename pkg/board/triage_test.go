package board

import "testing"

// NeedsTriage is the question the strip asks: has anybody said WHEN this
// card's work is due? Everything below is a reason the answer is "nobody has
// to" — the card is not the reader's decision to make.
func TestNeedsTriageIsACardNobodyPlaced(t *testing.T) {
	t.Parallel()
	open := Card{ItemID: "c1", Title: "A card", Team: "core"}

	for _, tc := range []struct {
		name string
		card Card
		want bool
	}{
		{"an open card of its own, with no week", open, true},
		{"one merely under way is still unplanned", with(open, func(c *Card) { c.Progress = 40 }), true},
		{
			// The day's planning put it there, not the week's: how long the
			// work takes is still nobody's answer.
			"a day on the board is not an answer",
			with(open, func(c *Card) { c.Day = "2026-09-02" }),
			true,
		},
		{"a card that already has a week", with(open, func(c *Card) { c.Week = "2026-08-31" }), false},
		{"a subtask, which follows its parent", with(open, func(c *Card) { c.Parent = "c9" }), false},
		{
			"a review card, which follows the card it reviews",
			with(open, func(c *Card) { c.ReviewOf = "c9" }),
			false,
		},
		{
			// It is waiting on a reviewer, not on a week: asking for one
			// would be asking the reader to decide something already decided.
			"a card sent to review",
			with(open, func(c *Card) { c.Stage = StageReview; c.Progress = 85 }),
			false,
		},
		{"a card on a personal board", with(open, func(c *Card) { c.Domain = "~kvaps" }), false},
		{"work already finished", with(open, func(c *Card) { c.Stage = StageDone }), false},
		{"work at a hundred percent", with(open, func(c *Card) { c.Progress = 100 }), false},
		{"a state card the board keeps for itself", with(open, func(c *Card) { c.Title = SprintStateTitle }), false},
		{
			// Locked is not done and not in review: somebody still has to say
			// when it is due.
			"a locked card",
			with(open, func(c *Card) { c.Stage = StageLocked }),
			true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NeedsTriage(Board{}, tc.card, "2026-09-02"); got != tc.want {
				t.Fatalf("NeedsTriage = %v, want %v", got, tc.want)
			}
		})
	}
}

// B1. A card placed in a week AHEAD is on no day board until its Monday —
// that is what makes the strip a regulator rather than a list. The rule has
// three readers (the Team grid, the Me view, and what a carry-over counts as
// being carried now), so it is pinned here on its own rather than only
// through them.
func TestPlacedAheadIsOffEveryDayBoardUntilItsMonday(t *testing.T) {
	today := "2026-09-03" // a Thursday; its week begins 2026-08-31
	for _, tc := range []struct {
		name string
		week string
		want bool
	}{
		{"the week after this one", "2026-09-07", true},
		{"a month out", "2026-10-05", true},
		// The boundary the rule turns on: this Monday is NOT ahead, so the
		// card is on the board the moment its week begins.
		{"this very week", "2026-08-31", false},
		{"a week gone by, still owed", "2026-08-24", false},
		{"no week at all — it is in the strip, not ahead", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlacedAhead(Card{Week: tc.week}, today); got != tc.want {
				t.Fatalf("PlacedAhead(week %q) = %v, want %v", tc.week, got, tc.want)
			}
		})
	}
}

// B7. A team's capacity is what the board measures the week against, so it
// has to be right when nobody has set it: it is read off the cards the team
// FINISHED, from the tree alone, over the four complete weeks before this one.
// The current week is never counted — it is not over, and counting it would
// read a Monday as a slow week.
func TestCapacityIsTheRostersNumberOrWhatTheTeamHasBeenDoing(t *testing.T) {
	today := "2026-09-03" // Thursday; this week began 2026-08-31
	done := func(id, day string) Card {
		return Card{ItemID: id, Team: "alpha", Title: id, DoneAt: day}
	}

	t.Run("the roster's own number wins, and says it was not derived", func(t *testing.T) {
		b := Board{
			SprintStates: map[string]SprintState{"alpha": {Capacity: Capacity{Week: 12}}},
			Cards:        []Card{done("a", "2026-08-24")},
		}
		got, derived := CapacityOf(b, "alpha", today)
		if got.Week != 12 || derived {
			t.Fatalf("CapacityOf = %+v, derived=%v; the roster's number is the answer", got, derived)
		}
	})

	t.Run("with no number it averages the four complete weeks", func(t *testing.T) {
		b := Board{Cards: []Card{
			// Eight cards over the four weeks 08-03 … 08-30: two a week.
			done("a", "2026-08-03"), done("b", "2026-08-07"),
			done("c", "2026-08-10"), done("d", "2026-08-14"),
			done("e", "2026-08-17"), done("f", "2026-08-21"),
			done("g", "2026-08-24"), done("h", "2026-08-28"),
			// This week is not over and is not counted.
			done("i", "2026-08-31"), done("j", today),
			// Older than the window, and another team's work.
			done("old", "2026-07-01"),
			{ItemID: "other", Team: "beta", Title: "other", DoneAt: "2026-08-24"},
		}}
		got, derived := CapacityOf(b, "alpha", today)
		if got.Week != 2 || !derived {
			t.Fatalf("CapacityOf = %+v, derived=%v; want 2 a week, derived", got, derived)
		}
	})

	// A team a fortnight old is not a slow team: averaging its two weeks over
	// four would halve a number nothing is wrong with.
	t.Run("a short record averages over the weeks there are", func(t *testing.T) {
		b := Board{Cards: []Card{
			done("a", "2026-08-17"), done("b", "2026-08-19"),
			done("c", "2026-08-24"), done("d", "2026-08-26"),
		}}
		got, derived := CapacityOf(b, "alpha", today)
		if got.Week != 2 || !derived {
			t.Fatalf("CapacityOf = %+v, derived=%v; want 2 over the two weeks it has", got, derived)
		}
	})

	// Nothing finished, ever: the board does not know this team's pace, and a
	// limit it invented would paint the week red on no evidence.
	t.Run("a team with no record at all has no limit", func(t *testing.T) {
		got, derived := CapacityOf(Board{}, "alpha", today)
		if got.Week != 0 || !derived {
			t.Fatalf("CapacityOf = %+v, derived=%v; want no limit", got, derived)
		}
		// The shares still come back, so a caller never divides by zero.
		if got.Client != DefaultClientShare || got.Internal != DefaultInternalShare {
			t.Fatalf("CapacityOf = %+v; the default shares stand", got)
		}
	})

	// The window's edges, which decide which week a card counts towards: four
	// weeks back is IN, this Monday is OUT.
	t.Run("counts the window's first day and not its last", func(t *testing.T) {
		b := Board{Cards: []Card{done("first", "2026-08-03"), done("this-week", "2026-08-31")}}
		got, _ := CapacityOf(b, "alpha", today)
		if got.Week != 1 {
			t.Fatalf("CapacityOf = %+v; the card four weeks back counts, this week's does not", got)
		}
	})
}

func with(c Card, f func(*Card)) Card {
	f(&c)
	return c
}
