package board

import "testing"

// A card's SIZE is what somebody said it weighs — S, M, L or XL — and the
// points are derived from it, never stored: the letter is the decision a
// person makes on a daily sync, the number is what the board sums. The scale
// doubles at each step (1/2/4/8) so that a week of S-work and a week of
// L-work can be compared at all.
func TestPointsFollowTheScale(t *testing.T) {
	cases := map[SizeKey]int{SizeNone: 0, SizeS: 1, SizeM: 2, SizeL: 4, SizeXL: 8}
	for size, want := range cases {
		if got := Points(size); got != want {
			t.Errorf("Points(%q) = %d, want %d", size, got, want)
		}
	}
	// The empty size is 0 on the SCALE and M on a BOARD: what a card nobody
	// sized weighs is a separate decision from what the letters mean, and
	// every sum goes through PointsOf, which applies it.
	if got := PointsOf(Board{}, Card{ItemID: "x"}); got != Points(DefaultSize) {
		t.Errorf("an unsized card weighs %d, want the default %d", got, Points(DefaultSize))
	}
	if DefaultSize != SizeM {
		t.Errorf("the default is %q; the board's own record makes M the middle card", DefaultSize)
	}
}

// The letter comes from people and tools in whatever case they typed it;
// anything that is not one of the four is refused rather than stored, since a
// size nothing knows would weigh nothing and read as unsized.
func TestParseSizeTakesTheFourLettersAndNothingElse(t *testing.T) {
	accepted := []struct {
		raw  string
		want SizeKey
	}{{"S", SizeS}, {"m", SizeM}, {" L ", SizeL}, {"xl", SizeXL}, {"XL", SizeXL}, {"", SizeNone}}
	for _, c := range accepted {
		got, ok := ParseSize(c.raw)
		if !ok || got != c.want {
			t.Errorf("ParseSize(%q) = %q,%v, want %q", c.raw, got, ok, c.want)
		}
	}
	for _, raw := range []string{"XXL", "4", "large", "S/M"} {
		if _, ok := ParseSize(raw); ok {
			t.Errorf("ParseSize(%q) must refuse", raw)
		}
	}
}

// A card with SUBTASKS weighs what its children weigh — the umbrella rule.
// Umbrellas are not born as umbrellas: forty percent get their first child
// three days or more after creation, and nothing in a card's text predicts
// it. So the rule is dynamic, the way a parent's progress already derives
// from its subtasks: the parent's own size stands until children exist and
// is replaced by their sum once they do, and the total never counts twice.
// On the production history a parent's own estimate and its children's sum
// agree (median ratio 1.0), which is what makes the swap safe.
func TestAParentWithSubtasksWeighsWhatItsChildrenWeigh(t *testing.T) {
	b := Board{Cards: []Card{
		{ItemID: "p", Size: SizeL},
		{ItemID: "k1", Parent: "p", Size: SizeM},
		{ItemID: "k2", Parent: "p", Size: SizeS},
		{ItemID: "k3", Parent: "p"}, // unsized: weighs the default, like any card
	}}
	if got := PointsOf(b, b.Cards[0]); got != 5 {
		t.Errorf("a parent weighs its children (2+1+default 2), got %d want 5", got)
	}
	if got := PointsOf(b, b.Cards[1]); got != 2 {
		t.Errorf("a child weighs its own size, got %d", got)
	}
}

// Once a card HAS subtasks, they are what it weighs — sized or not. The
// parent's own estimate described the whole of the work, and the pieces now
// describe it instead; keeping the larger of the two would let a card weigh
// its own guess plus its parts.
func TestAParentWithSubtasksStopsWeighingItself(t *testing.T) {
	b := Board{Cards: []Card{
		{ItemID: "p", Size: SizeXL},
		{ItemID: "k1", Parent: "p"},
		{ItemID: "k2", Parent: "p"},
	}}
	if got := PointsOf(b, b.Cards[0]); got != 4 {
		t.Errorf("two unsized children weigh the default each (2+2), got %d", got)
	}
}

// LoadNow is CarryingNow in points: the same cards a person is carrying
// today — theirs, open, not put off to a week ahead, subtasks riding their
// parent — weighed instead of counted. A card nobody sized weighs the
// DEFAULT, so the number is honest on a board nobody has sized yet: weighing
// unsized work as nothing would tell a person their week is empty while they
// are drowning in it.
func TestLoadNowWeighsWhatAPersonIsCarrying(t *testing.T) {
	today := "2026-09-08"
	b := Board{Cards: []Card{
		{ItemID: "a", Assignees: []string{"kvaps"}, Size: SizeL, SprintStart: today},
		{ItemID: "b", Assignees: []string{"kvaps"}, Size: SizeM, Progress: 100},       // done: not carried
		{ItemID: "c", Assignees: []string{"kvaps"}, Size: SizeXL, Week: "2026-09-21"}, // a week ahead: not today's
		{ItemID: "d", Assignees: []string{"kvaps"}, SprintStart: today},               // unsized: weighs the default (M)
		{ItemID: "p", Assignees: []string{"tym83"}, Size: SizeXL, SprintStart: today},
		{ItemID: "p1", Parent: "p", Assignees: []string{"tym83"}, Size: SizeS},
		{ItemID: "p2", Parent: "p", Assignees: []string{"tym83"}, Size: SizeS},
	}}
	got := LoadNow(b, today)
	if got["kvaps"] != 6 {
		t.Errorf("kvaps carries 6 points (an L and an unsized card at the default; done and ahead weigh nothing), got %d", got["kvaps"])
	}
	// The umbrella is one card carried once, at its children's weight — the
	// subtasks ride it and are not counted again on their own.
	if got["tym83"] != 2 {
		t.Errorf("tym83 carries the umbrella at its children's weight (2), got %d", got["tym83"])
	}
}

// A REVIEW card that nobody sized weighs S, not the default M. A review is
// somebody reading somebody else's finished work and saying yes or no — the
// sizing rubric already calls it S by definition — and it is the one kind of
// card that is created in bulk by the board itself, so weighing it as M put
// two points on a reviewer for every card they were asked to look at. On a
// board where nothing is sized that is the difference between "4 in hand"
// and "8 in hand" for a person holding two reviews and two cards.
func TestAnUnsizedReviewCardWeighsS(t *testing.T) {
	b := Board{Cards: []Card{
		{ItemID: "r", ReviewOf: "orig"},
		{ItemID: "c"},
		{ItemID: "big", ReviewOf: "orig2", Size: SizeL},
	}}
	if got := PointsOf(b, b.Cards[0]); got != 1 {
		t.Errorf("an unsized review weighs %d, want 1", got)
	}
	if got := PointsOf(b, b.Cards[1]); got != 2 {
		t.Errorf("an unsized ordinary card weighs %d, want the default 2", got)
	}
	// Said out loud, a size stands: a review somebody called L is L. The
	// default is a guess about the usual, not a cap on the unusual.
	if got := PointsOf(b, b.Cards[2]); got != 4 {
		t.Errorf("a review sized L weighs %d, want 4", got)
	}
}

// The umbrella rule weighs CHILDREN the same way: a parent whose subtasks are
// reviews weighs one point each, not two.
func TestAnUmbrellaOfReviewsWeighsThemAsReviews(t *testing.T) {
	b := Board{Cards: []Card{
		{ItemID: "p"},
		{ItemID: "k1", Parent: "p", ReviewOf: "x"},
		{ItemID: "k2", Parent: "p", ReviewOf: "y"},
	}}
	if got := PointsOf(b, b.Cards[0]); got != 2 {
		t.Errorf("two unsized review children weigh %d, want 2", got)
	}
}

// The storage is open — anything may commit to these repositories — so a
// card can arrive carrying a size nothing knows: `size: large`, or the
// lower-case `l` a writer copying the API's "any case on the way in" rule
// would produce. It must read as UNSIZED and weigh the default, which is what
// the storage contract promises. It weighed NOTHING instead: the scale is a
// map, a miss is 0, and the check was "did somebody write something" rather
// than "is what they wrote a size". A board of such cards told every person
// their week was empty — the one thing DefaultSize exists to prevent.
func TestASizeNothingKnowsReadsAsUnsized(t *testing.T) {
	b := Board{Cards: []Card{
		{ItemID: "junk", Size: SizeKey("large")},
		{ItemID: "case", Size: SizeKey("l")},
		{ItemID: "rev", ReviewOf: "orig", Size: SizeKey("HUGE")},
		{ItemID: "good", Size: SizeL},
	}}
	for _, tc := range []struct {
		id   string
		want int
	}{{"junk", 2}, {"case", 2}, {"rev", 1}, {"good", 4}} {
		var c Card
		for _, k := range b.Cards {
			if k.ItemID == tc.id {
				c = k
			}
		}
		if got := PointsOf(b, c); got != tc.want {
			t.Errorf("%s (size %q) weighs %d, want %d", tc.id, c.Size, got, tc.want)
		}
	}
}
