package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// SETTING A REVIEW CARD'S PERSON DIRECTLY IS THE SAME GESTURE AS THE OWNER
// PICKER'S (K10, K12). A person named on a review nobody has taken (PATCH
// assignees, MCP update_card assignee, a Team drag into a person's column)
// TAKES it — on the reviewer, today, in work, out of the backlog — rather
// than leaving it parked with somebody on it, on no board. Clearing the
// person of a taken review while its task is on review hands it BACK to the
// backlog, nobody on it, rather than leaving it in work with nobody.
func TestSettingAReviewCardsPersonTakesOrHandsItBack(t *testing.T) {
	t.Run("a person on an unclaimed review takes it", func(t *testing.T) {
		f := reviewSeed()
		svc := New(f)
		if err := svc.SetStage(ctx, "acme", "t", board.StageReview); err != nil {
			t.Fatal(err)
		}
		r := reviewOf(f, "t")
		if err := svc.SetAssignee(ctx, "acme", r.ItemID, "bob"); err != nil {
			t.Fatal(err)
		}
		got := f.get(r.ItemID)
		if got.Parked || !board.InWork(*got) || len(got.Assignees) != 1 || got.Assignees[0] != "bob" {
			t.Fatalf("bob's review, in work, out of the backlog: %+v", got)
		}
	})
	t.Run("no person on a taken review hands it back", func(t *testing.T) {
		f := reviewSeed()
		svc := New(f)
		if _, err := svc.SendToReview(ctx, "acme", "t", "bob", "", ""); err != nil {
			t.Fatal(err)
		}
		r := reviewOf(f, "t")
		if err := svc.SetAssignee(ctx, "acme", r.ItemID, ""); err != nil {
			t.Fatal(err)
		}
		got := f.get(r.ItemID)
		if got == nil || !board.NeedsReviewer(*got) || board.InWork(*got) {
			t.Fatalf("back in the backlog with nobody on it: %+v", got)
		}
		if f.get("t").Stage != board.StageReview {
			t.Fatal("the task stays on review")
		}
	})
}
