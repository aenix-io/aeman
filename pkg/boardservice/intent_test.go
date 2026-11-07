package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The × asks which of two things is meant, so the request carries the answer:
// UNASSIGN puts the card back in its team's backlog (K3), OFF THE BOARD takes
// it away.

func intentBoard() *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		// An ordinary card, in work.
		{ItemID: "plain", Team: "alpha", Assignees: []string{"kvaps"},
			SprintStart: today, StartDate: today, Day: today, Zone: board.ZoneRed},
		// Its subtask: a piece of the same work.
		{ItemID: "kid", Team: "alpha", Parent: "plain", SprintStart: today, StartDate: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
}

// UNASSIGN puts the card back in its team's BACKLOG (K3): parked, no
// person, no day, no sprint. It used to put the card "back in its week" and
// was refused when it had none; a card can always go to its backlog, so it
// never needs somewhere else to land.
func TestUnassignPutsTheCardBackInItsTeamsBacklog(t *testing.T) {
	f := intentBoard()
	if err := New(f).Remove(ctx, "acme", "plain", board.ViewAll, Unassign); err != nil {
		t.Fatal(err)
	}
	c := f.get("plain")
	if c == nil {
		t.Fatal("unassign never destroys a card")
	}
	if !c.Parked || c.Team != "alpha" {
		t.Fatalf("it is parked in its own team's backlog: %+v", c)
	}
	if len(c.Assignees) != 0 || c.SprintStart != "" || c.StartDate != "" || c.Day != "" {
		t.Fatalf("and it leaves work — no person, no day, no sprint: %+v", c)
	}
	// Its subtask rides it into the backlog: out of the sprint and off the day.
	if k := f.get("kid"); k == nil || k.SprintStart != "" || k.StartDate != "" {
		t.Fatalf("the subtask was left in work under a parked parent: %+v", k)
	}
}

// OFF THE BOARD is the other answer, and it is taken at the word: the card
// goes even though its week would have kept it, and the subtasks that were
// pieces of it go along — which is what the dialog says will happen.
func TestOffTheBoardTakesTheCardAndItsPieces(t *testing.T) {
	f := intentBoard()
	if err := New(f).Remove(ctx, "acme", "plain", board.ViewAll, OffBoard); err != nil {
		t.Fatal(err)
	}
	if f.get("plain") != nil {
		t.Fatal("the card was to come off the board, week or no week")
	}
	if f.get("kid") != nil {
		t.Fatal("its subtasks are pieces of the same work and go with it")
	}
}

// UNASSIGN destroys nothing, whatever the card is: a card nobody holds, a
// card the old Triage board left with nothing but a week (the week is read as
// nothing now, K7), a card already parked, and a SUBTASK — pulled out of its
// group and parked as a task of its own, since a subtask has no backlog place
// while it rides a parent. A REVIEW card is answered by its own rule below.
func TestUnassignNeverDeletes(t *testing.T) {
	today := board.TodayIso()
	for _, c := range []board.Card{
		{ItemID: "nobody", Team: "alpha", Progress: 60, SprintStart: today, StartDate: today},
		{ItemID: "weekonly", Team: "alpha", Assignees: []string{"bob"}, Progress: 60},
		{ItemID: "already", Team: "alpha", Parked: true},
		{ItemID: "kid", Team: "alpha", Parent: "parent", SprintStart: today, StartDate: today},
	} {
		f := newFake([]board.Card{
			{ItemID: "parent", Team: "alpha", SprintStart: today, StartDate: today},
			c,
		}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
		if err := New(f).Remove(ctx, "acme", c.ItemID, board.ViewAll, Unassign); err != nil {
			t.Fatalf("%s: unassign = %v", c.ItemID, err)
		}
		got := f.get(c.ItemID)
		if got == nil {
			t.Fatalf("%s: unassign deleted the card; it destroys nothing", c.ItemID)
		}
		if !got.Parked || len(got.Assignees) != 0 || board.InWork(*got) || got.Parent != "" {
			t.Fatalf("%s: want it parked, nobody's, out of work and in no group: %+v", c.ItemID, got)
		}
	}
}

// UNASSIGN on a REVIEW card is "back to the backlog" (K10): the reviewer is
// taken off and the review becomes the needs-a-reviewer item again —
// nobody's, on no day and in no sprint, parked in the backlog of the REVIEWED
// task's team (not the reviewer's), at the top of the board's order as a
// fresh one. The task stays on review, and nobody reviews it any more. A
// review already waiting there is left as it is.
func TestUnassignHandsAReviewBackToItsTeamsBacklog(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Assignees: []string{"ann"},
			SprintStart: today, StartDate: today},
		{ItemID: "rev", Team: "beta", ReviewOf: "orig", Assignees: []string{"bob"}, Zone: board.ZoneYellow,
			SprintStart: today, StartDate: today, Day: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	if err := New(f).Remove(ctx, "acme", "rev", board.ViewAll, Unassign); err != nil {
		t.Fatalf("back to the backlog = %v", err)
	}
	r := f.get("rev")
	if r == nil {
		t.Fatal("back to the backlog deleted the review; it destroys nothing")
	}
	if !r.Parked || len(r.Assignees) != 0 || board.InWork(*r) || r.Zone != board.ZoneGray {
		t.Fatalf("want it parked, nobody's, out of work and planned: %+v", r)
	}
	if r.Team != "alpha" {
		t.Fatalf("it waits in the reviewed task's team backlog, alpha; got %q", r.Team)
	}
	if !board.NeedsReviewer(*r) {
		t.Fatalf("it is the needs-a-reviewer item again: %+v", r)
	}
	if !logged(f, "MoveCard rev after=") {
		t.Fatalf("it goes to the top of the order like a fresh review; calls: %v", f.log)
	}
	o := f.get("orig")
	if o.Stage != board.StageReview || len(o.Assignees) != 1 || o.Assignees[0] != "ann" {
		t.Fatalf("the task stays on review, as it was: %+v", o)
	}
	if got := board.NewIndex(f.b).ReviewedBy(*o); got != "" {
		t.Fatalf("nobody reviews it any more; reviewedBy = %q", got)
	}

	// Already waiting for a reviewer: nothing to hand back.
	f.log = nil
	if err := New(f).Remove(ctx, "acme", "rev", board.ViewAll, Unassign); err != nil {
		t.Fatalf("again = %v", err)
	}
	if logged(f, "SetBacklog") || logged(f, "MoveCard") {
		t.Fatalf("a review already in its backlog is left as it is; calls: %v", f.log)
	}
}
