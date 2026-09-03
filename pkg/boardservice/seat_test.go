package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Me board is a narrower seat than the Team board, and the narrowing is
// the server's, not the browser's: an agent reaches the same doors. Two
// rules, both about work a person did not choose for themselves.

// A person may plan work for THEMSELVES, and the server does not argue.
//
// It used to refuse a create that filed work for the actor into a planned
// zone — the Me board offers its add form in the unplanned zone alone, and
// the rule was put in the service so an agent met the same door. That was a
// mistake, and a live one: the Team and Triage boards are where planning is
// DONE, a lead plans their own week there like anybody else's, and the three
// boards send the same create — the same zone, day and assignee — so the
// server cannot tell which one is asking. The refusal reached all of them and
// a lead could not put a card in their own column (reported on the live board:
// `work you plan for yourself is unplanned work: "gray"`).
//
// It also protected nothing. The guard sat on the create alone, so the same
// card could be made in the unplanned zone and moved with a zone patch a
// moment later, which is two steps and no refusal.
//
// What remains is the Me board's own offer: its add form appears in the
// unplanned zone only (web/src/meboard.ts, acceptsNewCard). That is a
// statement about what that board is for, and it belongs where the board is
// drawn rather than in a rule every caller meets.
func TestAPersonMayPlanTheirOwnWork(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "core"},
		{ItemID: "ep", Title: board.EpicStateTitle, Epic: "Auth", Project: "core"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso()}})
	svc := f2svc(f)
	me := WithActor(ctx, "kvaps")

	// Every zone, for oneself: this is the gesture that was refused.
	for _, zone := range []board.ZoneKey{board.ZoneRed, board.ZoneGray, board.ZoneGreen, board.ZoneYellow} {
		if _, err := svc.CreateCard(me, "acme", CreateCardArgs{
			Title: "mine", Team: "alpha", Zone: zone, Assignee: "kvaps",
		}); err != nil {
			t.Fatalf("zone %q for oneself = %v; a person plans their own week too", zone, err)
		}
	}

	// And for somebody else, which was never in question.
	if _, err := svc.CreateCard(me, "acme", CreateCardArgs{
		Title: "for them", Team: "alpha", Zone: board.ZoneRed, Assignee: "lllamnyp",
	}); err != nil {
		t.Fatalf("planning another person's work: %v", err)
	}
}

// A person REMOVES only what they put on their own board. Work somebody else
// planned for them is not theirs to take off it — their answer is the
// refused stage, which leaves the card standing for the lead to see.
func TestAPersonRemovesOnlyTheirOwnCard(t *testing.T) {
	today := board.TodayIso()
	newBoard := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "theirs", Team: "alpha", Author: "lllamnyp", Assignees: []string{"kvaps"},
				SprintStart: today, StartDate: today},
			{ItemID: "mine", Team: "alpha", Author: "kvaps", Assignees: []string{"kvaps"},
				SprintStart: today, StartDate: today},
			{ItemID: "somebody-elses", Team: "alpha", Author: "lllamnyp",
				Assignees: []string{"dan"}, SprintStart: today, StartDate: today},
			{ItemID: "kid", Team: "alpha", Author: "lllamnyp", Assignees: []string{"kvaps"},
				Parent: "theirs", SprintStart: today, StartDate: today},
		}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	}
	me := WithActor(ctx, "kvaps")

	f := newBoard()
	if err := f2svc(f).Remove(me, "acme", "theirs", RemoveAuto); !errors.Is(err, ErrNotYoursToRemove) {
		t.Fatalf("removing work planned for me = %v, want ErrNotYoursToRemove", err)
	}
	if f.get("theirs") == nil {
		t.Fatal("the refusal fires before the write")
	}

	// My own card, which I made, is mine to take off again.
	f = newBoard()
	if err := f2svc(f).Remove(me, "acme", "mine", RemoveAuto); err != nil {
		t.Fatalf("removing my own card: %v", err)
	}

	// A card on SOMEBODY ELSE is the lead's to remove: the rule speaks about
	// the person carrying the work, and that is not me here.
	f = newBoard()
	if err := f2svc(f).Remove(me, "acme", "somebody-elses", RemoveAuto); err != nil {
		t.Fatalf("the lead's x on another person's card: %v", err)
	}

	// A SUBTASK is a piece of the card it hangs under, not work assigned to
	// anyone: whoever sees the parent may take it away.
	f = newBoard()
	if err := f2svc(f).Remove(me, "acme", "kid", RemoveAuto); err != nil {
		t.Fatalf("a subtask of somebody else's card: %v", err)
	}

	// And the hard delete is the same door by another name.
	f = newBoard()
	if err := f2svc(f).DeleteCard(me, "acme", "theirs"); !errors.Is(err, ErrNotYoursToRemove) {
		t.Fatalf("deleting work planned for me = %v, want ErrNotYoursToRemove", err)
	}
}
