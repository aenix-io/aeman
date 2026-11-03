package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The × asks which of two things is meant, so the request carries the answer:
// UNASSIGN puts the card back in its team's backlog (K3), OFF THE BOARD takes
// it away.

func intentBoard() *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "core"},
		{ItemID: "ep", Title: board.ScopeStateTitle, Scope: "Auth", Project: "core"},
		// An ordinary card, in work.
		{ItemID: "plain", Team: "alpha", Assignees: []string{"kvaps"},
			SprintStart: today, StartDate: today, Day: today, Zone: board.ZoneRed},
		// Its subtask: a piece of the same work.
		{ItemID: "kid", Team: "alpha", Parent: "plain", SprintStart: today, StartDate: today},
		// A project card.
		{ItemID: "slot", Team: "alpha", Project: "core", Scope: "Auth",
			Assignees: []string{"kvaps"}, SprintStart: today, StartDate: today, Day: today},
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

// A PROJECT card is never destroyed by the ×: it is its Project board's
// commitment. Asking for it is refused rather than quietly turned into an
// unassign — a gesture that does something other than what it says is how
// the × came to be mistrusted.
func TestOffTheBoardIsRefusedForWorkThatIsNotThisBoardsToDestroy(t *testing.T) {
	for _, id := range []string{"slot"} {
		f := intentBoard()
		if err := New(f).Remove(ctx, "acme", id, board.ViewAll, OffBoard); !errors.Is(err, ErrNotYoursToDestroy) {
			t.Fatalf("%s off the board = %v, want ErrNotYoursToDestroy", id, err)
		}
		if f.get(id) == nil {
			t.Fatalf("%s: the refusal fires before the write", id)
		}
		// Unassign is the answer they do take.
		if err := New(f).Remove(ctx, "acme", id, board.ViewAll, Unassign); err != nil {
			t.Fatalf("%s unassign: %v", id, err)
		}
		if got := f.get(id); got == nil || len(got.Assignees) != 0 {
			t.Fatalf("%s: unassign leaves it, without a person: %+v", id, got)
		}
	}
}

// UNASSIGN destroys nothing, whatever the card is: a card nobody holds, a
// card the old Triage board left with nothing but a week (the week is read as
// nothing now, K7), a card already parked, and a SUBTASK — pulled out of its
// group and parked as a task of its own, since a subtask has no backlog place
// while it rides a parent. A REVIEW card is the one refusal: it follows the
// card it reviews and has no place of its own to be parked in.
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
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", SprintStart: today, StartDate: today},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Assignees: []string{"bob"}, SprintStart: today, StartDate: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	if err := New(f).Remove(ctx, "acme", "rev", board.ViewAll, Unassign); !errors.Is(err, ErrNoPlaceOfItsOwn) {
		t.Fatalf("unassign a review card = %v, want ErrNoPlaceOfItsOwn", err)
	}
}
