package boardservice

import (
	"errors"
	"testing"
	"time"

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

// A planning card is in no sprint (H3): its dates are its bar on the
// Roadmap. Deferring one made today moved a SPRINT onto it, the "a card
// created today relocates fully" branch meant for a task — a sprint every
// other door refuses on a planning card and the reading strips again.
func TestDeferringAPlanningCardWritesNoSprint(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, StartDate: today, Day: board.AddDays(today, 5),
			CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	}, nil)
	if err := f2svc(f).Defer(ctx, "acme", "epic", 1); err != nil {
		t.Fatal(err)
	}
	if c := f.get("epic"); c.SprintStart != "" {
		t.Fatalf("a deferred planning card carries sprint %q, want none", c.SprintStart)
	}
	if c := f.get("epic"); c.StartDate != board.AddDays(today, 1) {
		t.Fatalf("the epic's bar did not move: start %q", c.StartDate)
	}
}

// The repository rule asks where the child will live AFTER the link: a
// subtask lives where its task lives (rule 2), but once it is filed under a
// planning card it is no subtask, and its own team or project places it. A
// check made against the task's repository — the one the card is leaving —
// let a piece of a closed task go under an epic that stands in the closed
// repository, and the move that followed put the card in the primary, whose
// readers then saw a parent they could not open.
func TestTheRepositoryRuleJudgesWhereTheChildWillLive(t *testing.T) {
	today := board.TodayIso()
	ctx := WithActor(ctx, "kvaps")
	f := newFake([]board.Card{
		{ItemID: "closedEpic", Title: "Secret", Type: board.TypeEpic, Domain: "closed"},
		{ItemID: "closedTask", Title: "Invoices", Team: "beta", Domain: "closed", StartDate: today, Day: today},
		// A piece of the closed task whose own team lives in the primary (a
		// shape a direct writer can leave): it is in the closed repository
		// only because its task is.
		{ItemID: "piece", Title: "a piece", Parent: "closedTask", Team: "alpha", Domain: "closed"},
		{ItemID: "s-alpha", Title: board.SprintStateTitle, Team: "alpha", Domain: "primary"},
		{ItemID: "s-beta", Title: board.SprintStateTitle, Team: "beta", Domain: "closed"},
	}, nil)
	f.b.Primary = "primary"
	f.b.Domains = map[string]string{"s-alpha": "primary", "s-beta": "closed"}
	f.b.SprintStates = map[string]board.SprintState{"alpha": {ItemID: "s-alpha"}, "beta": {ItemID: "s-beta"}}
	if err := f2svc(f).SetParent(ctx, "acme", "piece", "closedEpic"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a piece that will live in the primary under a closed epic = %v, want ErrCrossDomain", err)
	}
}
