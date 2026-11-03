package boardservice

import (
	"errors"
	"slices"
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
	// Its default place is the TOP of the board's order, so it shows first
	// in its backlog until somebody drags it elsewhere.
	if !slices.Contains(f.log, "MoveCard "+r.ItemID+" after=") {
		t.Fatalf("the review card was not put at the top of the order; calls were: %v", f.log)
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

// The × on a review NOBODY HAS TAKEN — the Backlog's needs-a-reviewer item —
// WITHDRAWS the review (K10): the review card goes, and the task leaves the
// review stage back to work in progress, through the same cancellation the
// task leaving review performs. A bare delete would leave the task on
// review with no review card at all. Taken off the board by name or by the
// × deciding for itself, it is the same withdrawal, and the task's log says
// it once: the stage left review. (No reviewer is recorded as removed —
// there was none.)
func TestTheRemoveOnAReviewNobodyHasTakenWithdrawsTheReview(t *testing.T) {
	for name, intent := range map[string]RemoveIntent{"off the board": OffBoard, "the × deciding": RemoveAuto} {
		t.Run(name, func(t *testing.T) {
			f := reviewSeed()
			svc := New(f)
			if err := svc.SetStage(ctx, "acme", "t", board.StageReview); err != nil {
				t.Fatal(err)
			}
			r := reviewOf(f, "t")
			before := len(f.eventsOf("t"))
			if err := svc.Remove(ctx, "acme", r.ItemID, board.ViewBacklog, intent); err != nil {
				t.Fatalf("remove = %v", err)
			}
			if f.get(r.ItemID) != nil {
				t.Fatal("the review card is still on the board")
			}
			task := f.get("t")
			if task == nil {
				t.Fatal("withdrawing the review deleted the task")
			}
			if task.Stage != board.StageNone {
				t.Fatalf("the task is still on %q; it goes back to work in progress", task.Stage)
			}
			if task.Progress != 60 {
				t.Fatalf("progress = %d, want the task's own 60 kept", task.Progress)
			}
			kinds := map[string]int{}
			for _, e := range f.eventsOf("t")[before:] {
				kinds[e.Kind]++
			}
			if kinds[board.EventStage] != 1 || len(kinds) != 1 {
				t.Fatalf("the task's log = %v, want the stage left review, once, and nothing else", kinds)
			}
		})
	}
}

// NO DOOR LEAVES A TASK ON REVIEW WITH NO REVIEW CARD (K10). The × "off the
// board" on a review somebody HAS — the Team and Me boards' — withdraws the
// review exactly as the Backlog's × on an unclaimed one does: the task goes
// back to In progress and the review card goes. So does a plain DELETE of a
// review card (delete_card, DELETE /cards/{uid}) — routed through the same
// withdrawal rather than refused, so every door that takes a review card
// away says the same thing about its task. Removing the REVIEWER
// (remove_reviewer) is not a withdrawal: the review goes back to its task's
// team backlog with nobody on it, the task still on review (K12).
func TestNoDoorLeavesATaskOnReviewWithoutItsReviewCard(t *testing.T) {
	sent := func() (*fakeBackend, *Service, string) {
		f := reviewSeed()
		svc := New(f)
		if _, err := svc.SendToReview(ctx, "acme", "t", "bob", "", ""); err != nil {
			t.Fatal(err)
		}
		return f, svc, reviewOf(f, "t").ItemID
	}
	withdrawn := func(t *testing.T, f *fakeBackend, rev string) {
		t.Helper()
		if f.get(rev) != nil {
			t.Fatal("the review card is still on the board")
		}
		if task := f.get("t"); task == nil || task.Stage != board.StageNone {
			t.Fatalf("the task is not back in progress: %+v", task)
		}
	}
	for name, do := range map[string]func(*Service, string) error{
		"the × off the board, on the reviewer's Team board": func(s *Service, id string) error {
			return s.Remove(ctx, "acme", id, board.ViewTeam, OffBoard)
		},
		"the × deciding for itself": func(s *Service, id string) error {
			return s.Remove(ctx, "acme", id, board.ViewAll, RemoveAuto)
		},
		"a plain delete": func(s *Service, id string) error { return s.DeleteCard(ctx, "acme", id) },
	} {
		t.Run(name, func(t *testing.T) {
			f, svc, rev := sent()
			if err := do(svc, rev); err != nil {
				t.Fatal(err)
			}
			withdrawn(t, f, rev)
		})
	}
	t.Run("removing the reviewer hands the review back to the backlog", func(t *testing.T) {
		f, svc, rev := sent()
		if err := svc.RemoveReviewer(ctx, "acme", "t"); err != nil {
			t.Fatal(err)
		}
		r := f.get(rev)
		if r == nil || !board.NeedsReviewer(*r) {
			t.Fatalf("the review waits for a reviewer: %+v", r)
		}
		if f.get("t").Stage != board.StageReview {
			t.Fatal("the task stays on review")
		}
	})
}
