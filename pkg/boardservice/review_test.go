package boardservice

import (
	"context"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A review card can be taken off the board, and the guard must not stand in
// the way.
//
// The × refuses a card somebody else made and this person is only carrying —
// work planned for them is not theirs to take off the board, and their answer
// to it is the refused stage. A review card is neither of those things: it
// exists only because somebody sent their card to this person, it is the
// artefact of that asking, and "I am not doing this" is not an answer to a
// review. The guard shielded it all the same, and a LEAD who happened to be
// the reviewer — which is most reviews — could not take it off the Team
// board at all.
//
// Where that × is offered is the client's answer and it differs by board: the
// TEAM board draws one on every card, and the ME board draws none here
// (meboard.mayRemove — a review is authored by whoever sent it). This is the
// server's half: it refuses nobody.
func TestAReviewCardIsNotShieldedFromTheCross(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "orig", Title: "the work", Author: "ivan", Assignees: []string{"ivan"},
			Team: "t", Progress: 90, Stage: board.StageReview},
		{ItemID: "rev", Title: "review: the work", Author: "ivan", Assignees: []string{"kvaps"},
			Team: "t", ReviewOf: "orig", Progress: 40},
		// An ordinary card of ivan's that kvaps is only carrying: still not
		// kvaps's to remove, which is the rule this one is an exception to.
		{ItemID: "theirs", Title: "planned for me", Author: "ivan", Assignees: []string{"kvaps"}, Team: "t"},
	}, nil)
	svc := New(f)
	ctx := board.WithActor(context.Background(), "kvaps")

	if err := svc.DeleteCard(ctx, "acme", "rev"); err != nil {
		t.Fatalf("the reviewer must be able to close their review: %v", err)
	}
	if f.get("rev") != nil {
		t.Fatal("the review card is gone")
	}
	// The original stays ON REVIEW and simply has no reviewer any more: the
	// work still waits for one, and who it waits for is the review card.
	orig := f.get("orig")
	if orig.Stage != board.StageReview {
		t.Fatalf("the original's stage = %q, want it still on review", orig.Stage)
	}
	if err := svc.DeleteCard(ctx, "acme", "theirs"); !errors.Is(err, ErrNotYoursToRemove) {
		t.Fatalf("an ordinary card of somebody else's = %v, want ErrNotYoursToRemove", err)
	}
}

// Taking the review stage OFF the original takes the review with it — unless
// the reviewer had started. A review nobody has touched is the asking, and
// withdrawing the ask withdraws it; one that has been worked is somebody's
// record of having looked, and that is not the asker's to delete.
func TestClearingTheReviewStageWithdrawsAnUntouchedReview(t *testing.T) {
	fresh := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "orig", Title: "the work", Author: "ivan", Assignees: []string{"ivan"},
				Team: "t", Progress: 90, Stage: board.StageReview},
			{ItemID: "rev", Title: "review: the work", Author: "ivan", Assignees: []string{"kvaps"},
				Team: "t", ReviewOf: "orig"},
		}, nil)
	}
	ctx := board.WithActor(context.Background(), "ivan")

	// Untouched: the ask is withdrawn whole.
	f := fresh()
	if f.get("rev") == nil {
		t.Fatal("the fixture must start with a review card")
	}
	if err := New(f).SetStage(ctx, "acme", "orig", ""); err != nil {
		t.Fatal(err)
	}
	if f.get("rev") != nil {
		t.Fatal("a review nobody had started must go with the stage that asked for it")
	}

	// Started: it stays, and so does the reviewer's work on it.
	f = fresh()
	f.get("rev").Progress = 40
	if err := New(f).SetStage(ctx, "acme", "orig", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.get("rev"); got == nil || got.Progress != 40 {
		t.Fatalf("a review somebody has worked stays as it is, got %+v", got)
	}
}
