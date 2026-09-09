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

func with(c Card, f func(*Card)) Card {
	f(&c)
	return c
}

// A REVIEW card has no week of its own — the week belongs to the card it
// reviews — but it is real work in the reviewer's hands, and until now it
// stood on no board at all once its dates ran out: not the day boards (the
// dates are past), not the strip (nobody is waiting on a week for it), not
// the grid (no week). It still counted in the reviewer's load, which is how
// a person with two cards in front of them read as carrying four.
//
// So when a board asks to see reviews, a review stands in the week its own
// DATES fall in — and one whose week has gone comes into the current column
// by the same debt rule as everything else.
func TestAReviewStandsInTheWeekItsDatesFallIn(t *testing.T) {
	b := Board{}
	today := "2026-09-08"
	r := Card{ItemID: "r", ReviewOf: "orig", StartDate: "2026-07-23", Day: "2026-07-23"}
	if got := TriageWeekOf(b, r, today); got != "2026-07-20" {
		t.Errorf("a review's column = %q, want the Monday of its own dates", got)
	}
	// Its end date is what it is drawn by when it has no start — a review
	// mirrors the day of the card it reviews and may carry either.
	if got := TriageWeekOf(b, Card{ReviewOf: "o", Day: "2026-09-09"}, today); got != "2026-09-07" {
		t.Errorf("a review dated only by its end = %q, want 2026-09-07", got)
	}
	// A week of its own — only a direct write to the repository makes one —
	// still wins: it is the more explicit statement of the two.
	withWeek := Card{ReviewOf: "o", Week: "2026-09-07", StartDate: "2026-07-23"}
	if got := TriageWeekOf(b, withWeek, today); got != "2026-09-07" {
		t.Errorf("a review carrying a week = %q, want that week", got)
	}
	// Nothing to place it by is no column: it is not invented from nothing.
	if got := TriageWeekOf(b, Card{ReviewOf: "o"}, today); got != "" {
		t.Errorf("an undated review = %q, want no column", got)
	}
	// And it is never in the STRIP. The strip asks its reader to say WHEN,
	// and nobody is waiting on that for a review.
	if NeedsTriage(b, r, today) {
		t.Error("a review must not stand in the triage strip")
	}
	// An ordinary card is untouched: dates are not a week, and a card nobody
	// gave a week to belongs in the strip, not in the column its day lands in.
	if got := TriageWeekOf(b, Card{StartDate: "2026-07-23", Day: "2026-07-23"}, today); got != "" {
		t.Errorf("an ordinary dated card = %q, want no column", got)
	}
}

// WORK THAT WAS DONE IN A WEEK IS THAT WEEK'S WORK, whether or not anybody
// planned it. A card closed today without ever being given a week stood in no
// column and in no strip — the strip is for work still to be looked at, and
// this work is finished — so it was on the Triage board nowhere at all, and a
// lead reading the week saw less than the week had done.
//
// It stands in the week it was FINISHED in. That is where the reader is
// looking for it, it is the week whose points it is part of (a finished card
// WITH a week already counts there), and a card finished long ago falls
// outside the window like anything else of that week rather than piling into
// the current one.
func TestFinishedWorkNobodyPlannedStandsInTheWeekItWasDoneIn(t *testing.T) {
	var b Board
	done := Card{ItemID: "done", Team: "t", Progress: 100, DoneAt: "2026-09-09",
		StartDate: "2026-09-09", Day: "2026-09-09"}
	if got := TriageWeekOf(b, done, "2026-09-09"); got != "2026-09-07" {
		t.Errorf("the week it was finished in = %q, want its Monday", got)
	}
	// Still open, still unplaced: the strip, which is where it is asked
	// about — no week is invented for work nobody has looked at.
	open := done
	open.Progress, open.DoneAt = 40, ""
	if got := TriageWeekOf(b, open, "2026-09-09"); got != "" {
		t.Errorf("open work nobody placed stands in no column, got %q", got)
	}
	if !NeedsTriage(b, open, "2026-09-09") {
		t.Error("and it is in the strip")
	}
	// A week somebody DID give it wins: the plan is what they said, and the
	// day it was finished does not move the card out of it.
	placed := done
	placed.Week = "2026-08-31"
	if got := TriageWeekOf(b, placed, "2026-09-09"); got != "2026-08-31" {
		t.Errorf("the week somebody gave it = %q", got)
	}
	// Finished work is never in the strip, placed or not.
	if NeedsTriage(b, done, "2026-09-09") {
		t.Error("finished work is not waiting to be looked at")
	}
	// And a card that recorded no day is where it always was: nowhere. There
	// is nothing to place it by.
	silent := done
	silent.DoneAt = ""
	if got := TriageWeekOf(b, silent, "2026-09-09"); got != "" {
		t.Errorf("nothing says which week finished it, got %q", got)
	}
}
