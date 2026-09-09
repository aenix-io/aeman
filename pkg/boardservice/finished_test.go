package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Work finished LATE belongs to the sprint it was done in.
//
// An engineer finishes a card and never moves the bar. Carry Over takes it for
// open work and pulls it into the new sprint. Somebody notices and marks it
// 100 — and there it stands, in a sprint it was never worked in, counting
// against it. The board goes on showing it, which is the only prompt anybody
// gets; this is how a person answers the prompt.
//
// It is the old DEMOTE, brought back for the one case it was right for. The
// demote used to answer every ×, and the board grew a pile of OPEN work nobody
// could see — no live view reaches a card two sprints back, and no carry-over
// takes one. A finished card is not open: nothing is waiting for it, and being
// out of the way is what is wanted. That is the whole of the difference, and
// the reopen rule below is what keeps it true.

func finishedBoard() *fakeBackend {
	return newFake([]board.Card{
		// Finished, and standing in the current sprint after a carry-over.
		{ItemID: "late", Team: "alpha", Progress: 100, DoneFrom: 60,
			SprintStart: "2026-08-24", StartDate: "2026-08-28", Day: "2026-08-28",
			DoneAt: "2026-08-28", Assignees: []string{"kvaps"}},
		// Still going.
		{ItemID: "open", Team: "alpha", Progress: 60,
			SprintStart: "2026-08-24", StartDate: "2026-08-28"},
	}, map[string]board.SprintState{
		"alpha": {Current: "2026-08-24", Previous: "2026-08-17", ItemID: "s1"},
	})
}

func TestFinishedEarlierSendsTheWorkBackToTheSprintItWasDoneIn(t *testing.T) {
	f := finishedBoard()
	if err := New(f).FinishedEarlier(ctx, "acme", "late"); err != nil {
		t.Fatal(err)
	}
	c := f.get("late")
	if c.SprintStart != "2026-08-17" {
		t.Fatalf("the card joins the earlier sprint: %+v", c)
	}
	// Its DATES go with it, or the day grid would go on drawing it on a day of
	// the sprint it just left — the demote's own lesson: dates and all.
	if c.StartDate != "2026-08-17" || c.Day != "2026-08-17" {
		t.Fatalf("the dates go with it: %+v", c)
	}
	// And so does the day it counts as done on: the week's velocity reads
	// DoneAt (board.CapacityOf), so leaving it in this sprint would move the
	// card and leave the credit for it behind.
	if c.DoneAt != "2026-08-17" {
		t.Fatalf("the credit goes with it: %+v", c)
	}
	// It stays FINISHED. This answer says where work was done, not that it
	// was undone.
	if c.Progress != 100 {
		t.Fatalf("the work is still done: %+v", c)
	}
}

// The refusals, each fired before anything is written.
func TestFinishedEarlierRefusesWhatItWouldNotMove(t *testing.T) {
	f := finishedBoard()
	if err := New(f).FinishedEarlier(ctx, "acme", "open"); !errors.Is(err, ErrNotFinished) {
		t.Fatalf("work still going = %v, want ErrNotFinished", err)
	}
	if c := f.get("open"); c.SprintStart != "2026-08-24" {
		t.Fatalf("and nothing moved: %+v", c)
	}

	// A team whose first sprint is its only one has nothing behind it.
	g := newFake([]board.Card{
		{ItemID: "late", Team: "alpha", Progress: 100, SprintStart: "2026-08-24"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-08-24", ItemID: "s1"}})
	if err := New(g).FinishedEarlier(ctx, "acme", "late"); !errors.Is(err, ErrNoEarlierSprint) {
		t.Fatalf("no earlier sprint = %v, want ErrNoEarlierSprint", err)
	}

	// Already there: the answer would do nothing, and one that does nothing
	// reads as one that failed.
	h := finishedBoard()
	if c := h.get("late"); c != nil {
		c.SprintStart = "2026-08-17"
	}
	if err := New(h).FinishedEarlier(ctx, "acme", "late"); !errors.Is(err, ErrNoEarlierSprint) {
		t.Fatalf("already in the earlier sprint = %v, want ErrNoEarlierSprint", err)
	}
}

// The rule that keeps the pile from coming back.
//
// A card sent to an earlier sprint is invisible to every live view — that is
// the point, and it is safe only while the card is DONE. Reopening one there
// would make it exactly what the demote used to leave behind: open work, two
// sprints back, that nobody can see and no carry-over will ever take.
//
// So reopening brings it to the current sprint. The work is being picked up
// again, and it is being picked up NOW.
func TestReopeningACardSentBackBringsItToTheCurrentSprint(t *testing.T) {
	// BOTH exits from Reopen. A card done from a recorded value goes back to
	// it; a card done from nothing — 0 to 100, which is the ordinary way a
	// card is finished — falls back to the In Progress nudge through an early
	// return, and that is the exit the pull-back first slipped through.
	for _, doneFrom := range []int{60, 0} {
		f := finishedBoard()
		if c := f.get("late"); c != nil {
			c.DoneFrom = doneFrom
		}
		svc := New(f)
		if err := svc.FinishedEarlier(ctx, "acme", "late"); err != nil {
			t.Fatalf("doneFrom %d: %v", doneFrom, err)
		}
		if err := svc.Reopen(ctx, "acme", "late"); err != nil {
			t.Fatalf("doneFrom %d: %v", doneFrom, err)
		}
		c := f.get("late")
		if c.SprintStart != "2026-08-24" {
			t.Fatalf("doneFrom %d: reopened work is this sprint's: %+v", doneFrom, c)
		}
		if c.Progress >= 100 {
			t.Fatalf("doneFrom %d: and it is open again: %+v", doneFrom, c)
		}
	}
}

// A card reopened where it already stands is not moved: only one sent BACK is,
// and the rule must not drag every reopened card into the current sprint.
func TestReopeningInPlaceMovesNothing(t *testing.T) {
	f := finishedBoard()
	if err := New(f).Reopen(ctx, "acme", "late"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("late"); c.SprintStart != "2026-08-24" {
		t.Fatalf("it was already in this sprint: %+v", c)
	}
}

// The lead's answer has to be FINDABLE, or it is not an answer. Carry Over
// pulls the unfinished forward; the lead spots one that was actually done and
// nobody moved the bar, sets it to 100 and takes it off today's board — and
// the × asks whether to delete it or leave it finished in the sprint it was
// done in. Choosing the second must put the card where that sprint can be
// read: on the day the sprint began, which is the day the lead opens.
//
// This pins the loop the fields alone do not: FinishedEarlier writes them,
// and the day board draws by them.
func TestWorkSentBackStandsOnThatSprintsDay(t *testing.T) {
	f := finishedBoard()
	if err := New(f).FinishedEarlier(ctx, "acme", "late"); err != nil {
		t.Fatal(err)
	}
	// The board the way every reader gets it, sprint pointers and all.
	b, err := f.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	on := func(day string) bool {
		for _, c := range board.TeamGrid(b, "alpha", day) {
			if c.ItemID == "late" {
				return true
			}
		}
		return false
	}
	if !on("2026-08-17") {
		t.Error("the sprint it was done in must show it — that is what the answer meant")
	}
	// And it is off the sprint it was pulled into, which is the other half.
	if on("2026-08-24") {
		t.Error("the sprint it was carried into no longer counts it")
	}
}
