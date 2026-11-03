package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// K10 — PUTTING A TASK ON REVIEW ALWAYS MAKES ITS REVIEW CARD. With a
// reviewer named (send-to-review) the card is theirs and stands on their
// day, as ever. Without one — the stage set to review from any door: the
// drawer, PATCH stage, MCP update_card — the card is made with NOBODY on it
// and parked in the task's team's backlog: the "needs a reviewer" item. A
// task that already has a live review card gets no second one.

func reviewSeed() *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		{ItemID: "t", Title: "Invoices", Team: "alpha", Assignees: []string{"ann"}, Progress: 60,
			SprintStart: today, StartDate: today, Day: today, Description: "the context"},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
}

func reviewOf(f *fakeBackend, id string) *board.Card {
	b, _ := f.LoadBoard(ctx, "acme")
	r, ok := findReviewCard(b, id)
	if !ok {
		return nil
	}
	return f.get(r.ItemID)
}

func TestReviewWithoutAReviewerParksAnUnassignedReviewCard(t *testing.T) {
	f := reviewSeed()
	svc := New(f)
	if err := svc.SetStage(ctx, "acme", "t", board.StageReview); err != nil {
		t.Fatal(err)
	}
	r := reviewOf(f, "t")
	if r == nil {
		t.Fatal("setting the stage to review made no review card")
	}
	if r.Title != "review: Invoices" || r.Team != "alpha" || len(r.Assignees) != 0 {
		t.Fatalf("the review card = %+v, want «review: Invoices» of alpha with nobody on it", r)
	}
	if !r.Parked || board.InWork(*r) {
		t.Fatalf("a review nobody has taken waits in its team's backlog, on no day: %+v", r)
	}
	if !board.NeedsReviewer(*r) {
		t.Fatalf("it is the needs-a-reviewer item: %+v", r)
	}
	if f.get("t").Stage != board.StageReview {
		t.Fatal("the task is on review")
	}
	// Set again, or set once more from another door: no second card.
	if err := svc.SetStage(ctx, "acme", "t", board.StageReview); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range f.b.Cards {
		if c.ReviewOf == "t" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("review cards of the task = %d, want 1", n)
	}
}

// Naming a reviewer still makes THEIR review card, as ever: assigned, on
// today, in the task's sprint.
func TestReviewWithAReviewerIsTheirs(t *testing.T) {
	f := reviewSeed()
	if _, err := New(f).SendToReview(ctx, "acme", "t", "bob", "", ""); err != nil {
		t.Fatal(err)
	}
	r := reviewOf(f, "t")
	if r == nil || len(r.Assignees) != 1 || r.Assignees[0] != "bob" || r.Parked || !board.InWork(*r) {
		t.Fatalf("the reviewer's review card = %+v", r)
	}
}

// Assigning a person to the review nobody has taken — the backlog's picker,
// which is `own` with a person — takes it into work exactly like a task:
// on the reviewer, today, in the task's sprint, out of the backlog. Every
// other owner answer on a review card is refused, as before: its owner is
// its reviewer and nothing else.
func TestAReviewerTakesTheReviewNobodyHadTaken(t *testing.T) {
	today := board.TodayIso()
	f := reviewSeed()
	svc := New(f)
	if err := svc.SetStage(ctx, "acme", "t", board.StageReview); err != nil {
		t.Fatal(err)
	}
	r := reviewOf(f, "t")
	if err := svc.Own(ctx, "acme", r.ItemID, "beta", ""); !errors.Is(err, ErrOwnerFollowsLink) {
		t.Fatalf("a team alone on a review card = %v, want ErrOwnerFollowsLink", err)
	}
	if err := svc.Own(ctx, "acme", r.ItemID, "alpha", "bob"); err != nil {
		t.Fatal(err)
	}
	got := f.get(r.ItemID)
	if len(got.Assignees) != 1 || got.Assignees[0] != "bob" {
		t.Fatalf("the reviewer = %v, want bob", got.Assignees)
	}
	if got.Parked || got.StartDate != today || got.Day != today || got.SprintStart != today {
		t.Fatalf("taken into work today in the task's sprint, out of the backlog: %+v", got)
	}
	if got.Team != "alpha" {
		t.Fatalf("a review card keeps the reviewed task's team: %+v", got)
	}
	// Taken, it is a reviewer's card like any: another owner is refused.
	if err := svc.Own(ctx, "acme", r.ItemID, "alpha", "carol"); !errors.Is(err, ErrOwnerFollowsLink) {
		t.Fatalf("own on a taken review card = %v, want ErrOwnerFollowsLink", err)
	}
}

// Withdrawing the review takes the card nobody had taken away, as it takes
// an untouched assigned one: the task leaving review, finished, or deleted.
func TestWithdrawingTheReviewTakesTheUnassignedCardAway(t *testing.T) {
	for _, tc := range []struct {
		door string
		do   func(*Service) error
	}{
		{"leaving the review stage", func(s *Service) error { return s.SetStage(ctx, "acme", "t", board.StageNone) }},
		{"finishing the task", func(s *Service) error { return s.SetStage(ctx, "acme", "t", board.StageDone) }},
		{"deleting the task", func(s *Service) error { return s.DeleteCard(ctx, "acme", "t") }},
	} {
		t.Run(tc.door, func(t *testing.T) {
			f := reviewSeed()
			svc := New(f)
			if err := svc.SetStage(ctx, "acme", "t", board.StageReview); err != nil {
				t.Fatal(err)
			}
			r := reviewOf(f, "t")
			if err := tc.do(svc); err != nil {
				t.Fatal(err)
			}
			if f.get(r.ItemID) != nil {
				t.Fatalf("the review nobody had taken is still on the board")
			}
		})
	}
}
