package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A card can be in two places at once: the working area (a sprint and its
// days) and the WEEK it is scheduled for, which is its column on the Triage
// board. The × empties the working area, and where the card goes is decided
// by what it has left — its week, its Project-board column, or nothing, in
// which case it is deleted. A card filed under a column always has that
// column to go home to, so the × never destroys it.
//
// The Unassigned column is not special in any of this: it is the day grid
// like every other column, and a card made there and removed there is gone
// — it was nowhere else.

// gridBoard is a team whose sprint opened today, with yesterday's sprint
// behind it, plus a project column to file cards under.
func gridBoard(cards []board.Card) *fakeBackend {
	today := board.TodayIso()
	roster := []board.Card{
		{ItemID: "pr-core", Title: board.ProjectStateTitle, Project: "core"},
		{ItemID: "ep-auth", Title: board.EpicStateTitle, Epic: "Auth", Project: "core"},
	}
	return newFake(append(roster, cards...), map[string]board.SprintState{
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
	if err := svc.Remove(t.Context(), "o", "c1"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("a card with no week and no column must be deleted by the ×")
	}
}

// The same card, once it is also scheduled for a WEEK, has somewhere to
// stay: the × takes it out of the working area — off the day grid, dates and
// all — and leaves it in that week, on the Triage board.
func TestTheRemoveLeavesACardThatIsScheduledForAWeek(t *testing.T) {
	today := board.TodayIso()
	week := board.MondayOf(today)
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "taken out of its week", Team: "platform", Assignees: []string{"kvaps"},
			Week: week, SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("a card scheduled for a week must not be deleted by the ×")
	}
	if c.Week != week {
		t.Fatalf("it must keep the week it is scheduled for: week=%q", c.Week)
	}
	if c.SprintStart != "" || len(c.Assignees) != 0 {
		t.Fatalf("it must leave the working area: sprint=%q assignees=%v", c.SprintStart, c.Assignees)
	}
	if onGrid(t, fake, "c1") {
		t.Fatal("it is still on the day grid — the dates keep it there")
	}
}

// And pressed again on the card that is now nothing but its week, the ×
// deletes it: there is no second home left to hand it to. An × that always
// answered "back to your week" could never remove such a card at all.
func TestTheRemovePressedTwiceEmptiesTheLastHome(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "in both", Team: "platform", Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := findCard(mustLoad(t, fake), "c1"); !ok {
		t.Fatal("the first × leaves the card in its week")
	}
	if err := svc.Remove(t.Context(), "o", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := findCard(mustLoad(t, fake), "c1"); ok {
		t.Fatal("emptied of its last home, the card must be gone")
	}
}

func mustLoad(t *testing.T, fake *fakeBackend) board.Board {
	t.Helper()
	b, err := fake.LoadBoard(t.Context(), "o")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A card filed under a Project-board column is never destroyed by the ×: its
// column is a home it cannot be removed from here.
func TestTheRemoveNeverDeletesAProjectCard(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "a slot", Team: "platform", Project: "core", Epic: "Auth",
			SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "c1"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("the × deleted a project card")
	}
	if c.Project != "core" || c.Epic != "Auth" {
		t.Fatalf("the column must stay: project=%q epic=%q", c.Project, c.Epic)
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
	if err := svc.Remove(t.Context(), "o", "c1"); err != nil {
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

// A card carrying only a project name is on no board of its own: the
// Project board renders columns by epic. So the name is a label, not a home
// — the × treats the card like any other, and with nowhere else to be it is
// deleted rather than left alive where no board would show it.
func TestACardThatOnlyNamesAProjectIsNotSparedByThatName(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "project, no column", Team: "platform", Project: "core",
			Assignees: []string{"kvaps"}, SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "c1"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("the project name is no home: with nowhere else to be, the card goes")
	}
}

// The invariant behind all of it: a card the × keeps is a card some board
// still shows. Sparing one for what it carries — a person, a progress bar —
// left it alive with no sprint, no dates, no week and no column: findable
// nowhere, deletable nowhere.
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
		// A slot lives on the Project board; a card with a week stands in
		// that week's column on Triage.
		return ok && (c.Epic != "" || c.Week != "")
	}
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "worked, in both", Team: "platform",
			Assignees: []string{"kvaps"}, Progress: 40, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1"); err != nil {
		t.Fatal(err)
	}
	if !shown(t, fake, "c1") {
		t.Fatal("after the ×, the card is on no board")
	}
	if err := svc.Remove(t.Context(), "o", "c1"); err != nil {
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
		{ItemID: "p", Title: "parent", Team: "platform", Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
		{ItemID: "s", Title: "child", Team: "platform", Parent: "p",
			SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "p"); err != nil {
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

// A personal card follows the personal rule, never the board's: the × on one
// leaves it behind on the day it was worked, or deletes it.
func TestTheRemoveOnAPersonalCardFollowsThePersonalRule(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "p1", Title: "mine", Domain: "~kvaps", Progress: 40,
			StartDate: board.AddDays(today, -3), Day: board.AddDays(today, -3)},
	})
	if err := New(fake).Remove(t.Context(), "o", "p1"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	c, ok := findCard(b, "p1")
	if !ok {
		t.Fatal("a worked personal card is left behind, not deleted")
	}
	if c.LeftAt == "" {
		t.Fatalf("the personal rule leaves it on yesterday's board: %+v", c)
	}
}

// Leaving the working area is written down. A demote logs its sprint move
// because a card that leaves today's board without a word in its own history
// is a card nobody can account for (W6); the × that empties the working area
// is the same departure and was silent.
func TestLeavingTheWorkingAreaIsRecorded(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "in both", Team: "platform", Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "c1"); err != nil {
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
	if err := New(fake).Remove(t.Context(), "o", "s"); err != nil {
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
