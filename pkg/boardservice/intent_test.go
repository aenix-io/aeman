package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The × asks which of two things is meant, so the request carries the answer.
// It used to decide for itself — a card with a week was ALWAYS handed back to
// that week, and taking one off the board needed the × twice, the second
// press landing on a card that no longer looked like the one the person meant
// to remove.

func intentBoard() *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "core"},
		{ItemID: "ep", Title: board.EpicStateTitle, Epic: "Auth", Project: "core"},
		// An ordinary card, in the working area and scheduled for a week.
		{ItemID: "plain", Team: "alpha", Assignees: []string{"kvaps"}, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
		// Its subtask: a piece of the same work.
		{ItemID: "kid", Team: "alpha", Parent: "plain", SprintStart: today, StartDate: today},
		// A project card, and a process turn.
		{ItemID: "slot", Team: "alpha", Project: "core", Epic: "Auth",
			Assignees: []string{"kvaps"}, SprintStart: today, StartDate: today, Day: today},
		{ItemID: "turn", Team: "alpha", Task: "t1", Week: board.MondayOf(today),
			Assignees: []string{"kvaps"}, SprintStart: today, StartDate: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
}

// UNASSIGN empties the working area and leaves the card where it still
// belongs — its week, or its column. It never destroys anything.
func TestUnassignLeavesTheCardInItsWeek(t *testing.T) {
	f := intentBoard()
	if err := New(f).Remove(ctx, "acme", "plain", Unassign); err != nil {
		t.Fatal(err)
	}
	c := f.get("plain")
	if c == nil {
		t.Fatal("unassign never destroys a card")
	}
	if c.Week == "" {
		t.Fatalf("it keeps the week it is scheduled for: %+v", c)
	}
	if len(c.Assignees) != 0 || c.SprintStart != "" || c.StartDate != "" {
		t.Fatalf("and it leaves the working area: %+v", c)
	}
}

// OFF THE BOARD is the other answer, and it is taken at the word: the card
// goes even though its week would have kept it, and the subtasks that were
// pieces of it go along — which is what the dialog says will happen.
func TestOffTheBoardTakesTheCardAndItsPieces(t *testing.T) {
	f := intentBoard()
	if err := New(f).Remove(ctx, "acme", "plain", OffBoard); err != nil {
		t.Fatal(err)
	}
	if f.get("plain") != nil {
		t.Fatal("the card was to come off the board, week or no week")
	}
	if f.get("kid") != nil {
		t.Fatal("its subtasks are pieces of the same work and go with it")
	}
}

// A PROJECT card and a PROCESS TURN are never destroyed by the ×: the one is
// its Project board's commitment, the other its process's record of a week.
// Asking for it is refused rather than quietly turned into an unassign — a
// gesture that does something other than what it says is how the × came to be
// mistrusted.
func TestOffTheBoardIsRefusedForWorkThatIsNotThisBoardsToDestroy(t *testing.T) {
	for _, id := range []string{"slot", "turn"} {
		f := intentBoard()
		if err := New(f).Remove(ctx, "acme", id, OffBoard); !errors.Is(err, ErrNotYoursToDestroy) {
			t.Fatalf("%s off the board = %v, want ErrNotYoursToDestroy", id, err)
		}
		if f.get(id) == nil {
			t.Fatalf("%s: the refusal fires before the write", id)
		}
		// Unassign is the answer they do take.
		if err := New(f).Remove(ctx, "acme", id, Unassign); err != nil {
			t.Fatalf("%s unassign: %v", id, err)
		}
		if got := f.get(id); got == nil || len(got.Assignees) != 0 {
			t.Fatalf("%s: unassign leaves it, without a person: %+v", id, got)
		}
	}
}

// A card with nowhere to be handed back to has nothing to unassign INTO: the
// request is refused rather than leaving the card with no person, no dates,
// no week and no column — alive on no board anyone can open.
func TestUnassignNeedsSomewhereToLeaveTheCard(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Assignees: []string{"kvaps"},
			SprintStart: today, StartDate: today, Day: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	if err := New(f).Remove(ctx, "acme", "c1", Unassign); !errors.Is(err, ErrNowhereToLeaveIt) {
		t.Fatalf("unassign with nowhere to land = %v, want ErrNowhereToLeaveIt", err)
	}
	if f.get("c1") == nil {
		t.Fatal("and nothing is written")
	}
}

// UNASSIGN destroys nothing — including a card whose ONLY home is its week,
// which is what the Triage board makes every time somebody drags a card into
// a week ahead: a person and a week, no sprint and no dates.
//
// The guard admitted such a card (it has a week to be left in) and the arm
// that hands a card back to its week refused it (it is not in the working
// area to be taken out of), so it fell through to the delete at the end. Two
// clicks from the board — drag to next week, ×, "Move it to Unassigned", the
// safe-looking option that promises the card "stands in the week it is
// scheduled for" — and a card with work on it was gone. The fixture below
// hid it: every card in intentBoard() has a sprint and dates, so the test
// above only ever exercised the safe branch.
func TestUnassignNeverDeletes(t *testing.T) {
	today := board.TodayIso()
	for _, c := range []board.Card{
		// Dragged to a week ahead: Place clears the dates and the sprint.
		{ItemID: "ahead", Team: "alpha", Assignees: []string{"bob"},
			Week: board.MondayOf(today), Progress: 60},
		// The same card with nobody on it: still a week to stand in.
		{ItemID: "nobody", Team: "alpha", Week: board.MondayOf(today), Progress: 60},
		// A subtask with no column of its own, reachable through the API.
		{ItemID: "kid", Team: "alpha", Parent: "ahead", Week: board.MondayOf(today)},
	} {
		f := newFake([]board.Card{
			{ItemID: "ahead", Team: "alpha", Week: board.MondayOf(today)},
			c,
		}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
		if err := New(f).Remove(ctx, "acme", c.ItemID, Unassign); err != nil {
			t.Fatalf("%s: unassign = %v", c.ItemID, err)
		}
		got := f.get(c.ItemID)
		if got == nil {
			t.Fatalf("%s: unassign deleted the card; it destroys nothing", c.ItemID)
		}
		if got.Week == "" {
			t.Fatalf("%s: it keeps the week it stands in: %+v", c.ItemID, got)
		}
		if len(got.Assignees) != 0 {
			t.Fatalf("%s: and it comes off the person: %+v", c.ItemID, got)
		}
	}
}
