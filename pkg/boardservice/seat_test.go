package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Me board is a narrower seat than the Team board, and the narrowing is
// the server's, not the browser's: an agent reaches the same doors. Two
// rules, both about work a person did not choose for themselves.

// A person ADDS work to their own board only as unplanned: something came up
// today. The other three zones are the plan, and planning is done with the
// team, not filed quietly into one's own column.
func TestAPersonPlansNoWorkForThemselves(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "core"},
		{ItemID: "ep", Title: board.EpicStateTitle, Epic: "Auth", Project: "core"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso()}})
	svc := f2svc(f)
	me := WithActor(ctx, "kvaps")

	for _, zone := range []board.ZoneKey{board.ZoneRed, board.ZoneGray, board.ZoneGreen} {
		_, err := svc.CreateCard(me, "acme", CreateCardArgs{
			Title: "mine", Team: "alpha", Zone: zone, Assignee: "kvaps",
		})
		if !errors.Is(err, ErrNotYoursToPlan) {
			t.Fatalf("zone %q for oneself = %v, want ErrNotYoursToPlan", zone, err)
		}
	}
	if len(f.creates) != 0 {
		t.Fatalf("the refusal fires before the write: %d creates", len(f.creates))
	}

	// Unplanned is the zone the seat has: something arrived today.
	if _, err := svc.CreateCard(me, "acme", CreateCardArgs{
		Title: "came up", Team: "alpha", Zone: board.ZoneYellow, Assignee: "kvaps",
	}); err != nil {
		t.Fatalf("unplanned work for oneself: %v", err)
	}

	// Planning work for SOMEBODY ELSE is the lead's own gesture and passes:
	// the rule is about a person quietly planning their own week.
	if _, err := svc.CreateCard(me, "acme", CreateCardArgs{
		Title: "for them", Team: "alpha", Zone: board.ZoneRed, Assignee: "lllamnyp",
	}); err != nil {
		t.Fatalf("planning another person's work: %v", err)
	}

	// So does a card that is not a standalone one: a review card, a subtask
	// and a card filed under a column are placed by the thing they belong
	// to, and their zone is not a plan anybody made here.
	if _, err := svc.CreateCard(me, "acme", CreateCardArgs{
		Title: "a piece of it", Team: "alpha", Zone: board.ZoneRed,
		Assignee: "kvaps", Epic: "Auth", Project: "core",
	}); err != nil {
		t.Fatalf("a columned create: %v", err)
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
	if err := f2svc(f).Remove(me, "acme", "theirs"); !errors.Is(err, ErrNotYoursToRemove) {
		t.Fatalf("removing work planned for me = %v, want ErrNotYoursToRemove", err)
	}
	if f.get("theirs") == nil {
		t.Fatal("the refusal fires before the write")
	}

	// My own card, which I made, is mine to take off again.
	f = newBoard()
	if err := f2svc(f).Remove(me, "acme", "mine"); err != nil {
		t.Fatalf("removing my own card: %v", err)
	}

	// A card on SOMEBODY ELSE is the lead's to remove: the rule speaks about
	// the person carrying the work, and that is not me here.
	f = newBoard()
	if err := f2svc(f).Remove(me, "acme", "somebody-elses"); err != nil {
		t.Fatalf("the lead's x on another person's card: %v", err)
	}

	// A SUBTASK is a piece of the card it hangs under, not work assigned to
	// anyone: whoever sees the parent may take it away.
	f = newBoard()
	if err := f2svc(f).Remove(me, "acme", "kid"); err != nil {
		t.Fatalf("a subtask of somebody else's card: %v", err)
	}

	// And the hard delete is the same door by another name.
	f = newBoard()
	if err := f2svc(f).DeleteCard(me, "acme", "theirs"); !errors.Is(err, ErrNotYoursToRemove) {
		t.Fatalf("deleting work planned for me = %v, want ErrNotYoursToRemove", err)
	}
}
