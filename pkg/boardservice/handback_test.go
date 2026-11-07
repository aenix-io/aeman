package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The grid × asked nothing (RemoveAuto) empties the working area, which was
// the card's only home, so it is deleted. The × that says UNASSIGN puts it
// back in its team's backlog instead (K3, intent_test.go).
//
// The Unassigned column is not special in any of this: it is the day grid
// like every other column, and a card made there and removed there is gone
// — it was nowhere else.

// gridBoard is a team whose sprint opened today, with yesterday's sprint
// behind it.
func gridBoard(cards []board.Card) *fakeBackend {
	today := board.TodayIso()
	return newFake(cards, map[string]board.SprintState{
		"platform": {Current: today, Previous: board.AddDays(today, -1), ItemID: "st-platform"},
	})
}

// onGrid reports whether the card still shows on the team's day grid today.
func onGrid(t *testing.T, fake *fakeBackend, id string) bool {
	t.Helper()
	b, _ := fake.LoadBoard(t.Context(), "o")
	for _, c := range board.TeamGrid(b, "platform", board.TodayIso()) {
		if c.ItemID == id {
			return true
		}
	}
	return false
}

// A card made in the working area today and removed there was nowhere else:
// the × deletes it. It used to be filed into this week's plan instead —
// where nobody had put it — and, keeping its dates, it stayed on the grid as
// well: one card in two places, which is how the trouble was reported.
func TestTheGridRemoveDeletesACardThatIsNowhereElse(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "made today", Team: "platform", Zone: board.ZoneYellow,
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1", board.ViewAll, RemoveAuto); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("a card with no week and no column must be deleted by the ×")
	}
}

// What a card carries does not buy it a different outcome here: the working
// area was its only home, so the × empties it and the card is gone. Its
// progress goes with it — a loss worth asking about before the request is
// made (deleteWarning in removal.ts), not a reason for the board to move
// the card somewhere nobody put it. Its subtasks go with it too: they are
// pieces of the same work (an explicit DELETE of one card frees them
// instead — see TestDeleteParentReleasedChildInheritsAssignee).
func TestWorkDoesNotTurnARemoveIntoAMove(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "started today", Team: "platform", Assignees: []string{"kvaps"}, Progress: 40,
			SprintStart: today, StartDate: today, Day: today},
		{ItemID: "c2", Title: "a subtask", Team: "platform", Parent: "c1",
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1", board.ViewAll, RemoveAuto); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("the card was nowhere else: the × empties its last home")
	}
	if sub, ok := findCard(b, "c2"); ok {
		t.Fatalf("the subtask is a piece of the same work and goes with it: %+v", sub)
	}
}

// The invariant behind all of it: a card the × keeps is a card some board
// still shows. Sparing one for what it carries — a person, a progress bar —
// left it alive with no sprint and no dates: findable nowhere,
// deletable nowhere. Unassigned, it is in its backlog; ×'d there, it goes.
func TestACardTheRemoveKeepsIsAlwaysOnSomeBoard(t *testing.T) {
	today := board.TodayIso()
	shown := func(t *testing.T, fake *fakeBackend, id string) bool {
		t.Helper()
		b, _ := fake.LoadBoard(t.Context(), "o")
		if onGrid(t, fake, id) {
			return true
		}
		for _, c := range board.MeView(b, "", today) {
			if c.ItemID == id {
				return true
			}
		}
		c, ok := findCard(b, id)
		// A parked task lives in its team's backlog.
		return ok && board.InBacklog(c)
	}
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "worked", Team: "platform",
			Assignees: []string{"kvaps"}, Progress: 40,
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1", board.ViewAll, Unassign); err != nil {
		t.Fatal(err)
	}
	if !shown(t, fake, "c1") {
		t.Fatal("after the ×, the card is on no board")
	}
	if err := svc.Remove(t.Context(), "o", "c1", board.ViewAll, RemoveAuto); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatalf("emptied of its last home the card must be gone, not invisible: shown=%v", shown(t, fake, "c1"))
	}
}

// The subtasks leave the working area with their parent: left behind in the
// sprint it has left, they stand under a card the board no longer shows.
func TestSubtasksLeaveTheWorkingAreaWithTheirParent(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform",
			SprintStart: today, StartDate: today, Day: today},
		{ItemID: "s", Title: "child", Team: "platform", Parent: "p",
			SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "p", board.ViewAll, Unassign); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	sub, ok := findCard(b, "s")
	if !ok {
		t.Fatal("the subtask must survive")
	}
	if sub.SprintStart != "" {
		t.Fatalf("it stayed in the sprint its parent left: %+v", sub)
	}
	if sub.StartDate != "" || sub.Day != "" {
		t.Fatalf("its dates would keep it on a day its parent has left: %+v", sub)
	}
}

// Leaving the working area is written down. A demote logs its sprint move
// because a card that leaves today's board without a word in its own history
// is a card nobody can account for (W6); the × that empties the working area
// is the same departure and was silent.
func TestLeavingTheWorkingAreaIsRecorded(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "in work", Team: "platform",
			SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "c1", board.ViewAll, Unassign); err != nil {
		t.Fatal(err)
	}
	if !fake.saw("AppendEvent c1 sprint " + today + "->") {
		t.Fatalf("the sprint it left is not in its history; log=%v", fake.log)
	}
	if !fake.saw("AppendEvent c1 dates " + board.DateRange(today, today) + "->") {
		t.Fatalf("the dates it left are not in its history; log=%v", fake.log)
	}
}

// The × on a subtask deletes it. Demoting it alone would leave it in a
// sprint its parent is not in — and since a subtask keeps rendering under
// its parent wherever it sits, the × would look like it had done nothing at
// all.
func TestTheRemoveOnASubtaskDeletesItRatherThanDemotingIt(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform",
			SprintStart: today, StartDate: board.AddDays(today, -2), Day: today},
		{ItemID: "s", Title: "child", Team: "platform", Parent: "p",
			SprintStart: today, StartDate: board.AddDays(today, -2), Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "s", board.ViewAll, RemoveAuto); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "s"); ok {
		t.Fatal("the subtask must be gone, not demoted away from its parent")
	}
	p, ok := findCard(b, "p")
	if !ok || p.SprintStart != today {
		t.Fatalf("the parent stays where it was: %+v", p)
	}
}
