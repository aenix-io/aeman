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

func with(c Card, f func(*Card)) Card {
	f(&c)
	return c
}
