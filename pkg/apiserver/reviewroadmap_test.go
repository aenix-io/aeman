package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Roadmap lists the open work that is not in the tree yet (H9) — and a
// review card is not work of its own: it is the asking, a copy of the card
// it reviews that stands beside the original and goes when it goes. Listed
// as loose work it drew a draggable row with an owner picker that `own`
// refuses (ErrOwnerFollowsLink), and dragged under an epic it would have
// counted as one more task of the epic.
func TestAReviewCardIsNotLooseRoadmapWork(t *testing.T) {
	t.Parallel()
	today := board.TodayIso()
	b := board.NewBoard([]board.Card{
		{ItemID: "orig", Title: "the work", Team: "alpha", Assignees: []string{"ann"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "rev", Title: "review: the work", ReviewOf: "orig", Team: "alpha", Assignees: []string{"bob"},
			StartDate: today, Day: today, SprintStart: today},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today}}
	in := map[string]bool{}
	for _, c := range FilterCards(b, Selector{View: "roadmap"}) {
		in[c.ItemID] = true
	}
	if !in["orig"] {
		t.Fatalf("the original is open work and on the roadmap: %v", in)
	}
	if in["rev"] {
		t.Fatalf("a review card is not loose work of its own: %v", in)
	}
	if Drawn(b, Selector{View: "roadmap"}, "rev") {
		t.Fatal("Drawn disagrees with the listing")
	}
}
