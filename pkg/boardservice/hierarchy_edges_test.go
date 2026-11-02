package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A subtask usually has no person of its own — it rides its task's. Pulled
// out of the group by the ungroup gesture it takes that person with it
// (S8), or it stands in Unassigned and on nobody's Me board, which from the
// person who pulled it looks like the card vanishing. Dragged from its task
// to an EPIC on the Roadmap it leaves the group the same way, and the
// hand-over has to be the same.
func TestASubtaskLeavingForAnEpicTakesItsTasksPerson(t *testing.T) {
	ctx := WithActor(ctx, "kvaps")
	f := newFake([]board.Card{
		{ItemID: "task", Title: "the task", Team: "alpha", Assignees: []string{"ann"}},
		{ItemID: "ownerless", Title: "a piece", Parent: "task", Team: "alpha"},
		{ItemID: "owned", Title: "another piece", Parent: "task", Team: "alpha", Assignees: []string{"bob"}},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
	}, nil)
	svc := f2svc(f)
	if err := svc.SetParent(ctx, "acme", "ownerless", "epic"); err != nil {
		t.Fatal(err)
	}
	c := f.get("ownerless")
	if c.Parent != "epic" {
		t.Fatalf("not under the epic: %+v", c)
	}
	if len(c.Assignees) != 1 || c.Assignees[0] != "ann" {
		t.Fatalf("the subtask left its task ownerless: %v, want its task's person", c.Assignees)
	}
	// One with a person of its own keeps that person.
	if err := svc.SetParent(ctx, "acme", "owned", "epic"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("owned"); len(c.Assignees) != 1 || c.Assignees[0] != "bob" {
		t.Fatalf("a subtask with its own person lost it: %v", c.Assignees)
	}
}

// A review card follows the card it reviews: it exists because somebody sent
// that card for review, it stands where the original stands and goes when
// the original goes. It sits under nothing of its own — filed under an epic
// it would count as one more task of the epic (done/total off by one) while
// the Roadmap draws it beside the original it is a copy of.
func TestAReviewCardSitsUnderNothingOfItsOwn(t *testing.T) {
	ctx := WithActor(ctx, "kvaps")
	f := newFake([]board.Card{
		{ItemID: "orig", Title: "the work", Team: "alpha", Assignees: []string{"ann"}},
		{ItemID: "rev", Title: "review: the work", ReviewOf: "orig", Team: "alpha", Assignees: []string{"bob"}},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
	}, nil)
	if err := f2svc(f).SetParent(ctx, "acme", "rev", "epic"); !errors.Is(err, ErrReviewCardLink) {
		t.Fatalf("filing a review card under an epic = %v, want ErrReviewCardLink", err)
	}
	if c := f.get("rev"); c.Parent != "" {
		t.Fatalf("the refusal wrote the link anyway: %+v", c)
	}
}
