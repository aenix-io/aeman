package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A card has two homes and the × empties one of them: the working area (a
// sprint and its days) and the weekly plan (a band and its week). Removing
// it from one leaves it in the other; removing it from its last one deletes
// it. A card filed under a Project-board column always has that column to
// go home to, so the × never destroys it.
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
	if err := svc.Remove(t.Context(), "o", "c1", "grid"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("a card with no plan band and no column must be deleted by the grid ×")
	}
}

// The same card, once it is also in the weekly plan, has somewhere to stay:
// the grid × takes it out of the working area — off the day grid, dates and
// all — and leaves it in the plan.
func TestTheGridRemoveLeavesACardThatIsAlsoInThePlan(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "taken from the plan", Team: "platform", Assignees: []string{"kvaps"},
			Plan: board.PlanFri, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1", "grid"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("a card that is in the plan must not be deleted by the grid ×")
	}
	if c.Plan != board.PlanFri || c.Week != board.MondayOf(today) {
		t.Fatalf("it must keep its place in the plan: plan=%q week=%q", c.Plan, c.Week)
	}
	if c.SprintStart != "" || len(c.Assignees) != 0 {
		t.Fatalf("it must leave the working area: sprint=%q assignees=%v", c.SprintStart, c.Assignees)
	}
	if onGrid(t, fake, "c1") {
		t.Fatal("it is still on the day grid — the dates keep it there")
	}
}

// And the other way round, which already held and must keep holding: the
// plan × on a card someone is working on takes it out of the plan and
// leaves it in the working area.
func TestThePlanRemoveLeavesACardThatIsAlsoInTheWorkingArea(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "in both", Team: "platform", Assignees: []string{"kvaps"}, Progress: 30,
			Plan: board.PlanFri, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("a card in the working area must not be deleted by the plan ×")
	}
	if c.Plan != board.PlanNone {
		t.Fatalf("it must leave the plan: plan=%q", c.Plan)
	}
	if c.SprintStart != today || !onGrid(t, fake, "c1") {
		t.Fatalf("it must stay in the working area: sprint=%q onGrid=%v", c.SprintStart, onGrid(t, fake, "c1"))
	}
}

// Emptying both homes deletes the card: out of the working area first, then
// out of the plan, and it is gone.
func TestRemovedFromBothHomesTheCardIsGone(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "in both", Team: "platform",
			Plan: board.PlanFri, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1", "grid"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove(t.Context(), "o", "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("emptied of both homes, the card must be gone")
	}
}

// A card filed under a Project-board column is never destroyed by either ×:
// its column is a home it cannot be removed from here.
func TestNeitherRemoveDeletesAProjectCard(t *testing.T) {
	today := board.TodayIso()
	for _, from := range []string{"grid", "plan"} {
		fake := gridBoard([]board.Card{
			{ItemID: "c1", Title: "a slot", Team: "platform", Project: "core", Epic: "Auth",
				Plan: board.PlanFri, Week: board.MondayOf(today),
				SprintStart: today, StartDate: today, Day: today},
		})
		svc := New(fake)
		if err := svc.Remove(t.Context(), "o", "c1", from); err != nil {
			t.Fatalf("%s: %v", from, err)
		}
		b, _ := fake.LoadBoard(t.Context(), "o")
		c, ok := findCard(b, "c1")
		if !ok {
			t.Fatalf("the %s × deleted a project card", from)
		}
		if c.Project != "core" || c.Epic != "Auth" {
			t.Fatalf("%s: the column must stay: project=%q epic=%q", from, c.Project, c.Epic)
		}
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
	if err := svc.Remove(t.Context(), "o", "c1", "grid"); err != nil {
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

// The plan × must obey the same two-homes rule from its side: a card that is
// in the working area — on the day grid by its sprint — stays there, even
// when nobody took it and nobody worked it. The plan branch used to spare
// only an assigned-or-worked card and deleted this one outright, sprint and
// all: reachable through take_into_plan with no engineer, or a drag into the
// grid's Unassigned column.
func TestThePlanRemoveLeavesAnUnassignedCardThatIsInTheWorkingArea(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "in both, nobody's", Team: "platform",
			Plan: board.PlanFri, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
	})
	svc := New(fake)
	if err := svc.Remove(t.Context(), "o", "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("a card on the day grid must not be deleted by the plan ×")
	}
	if c.Plan != board.PlanNone {
		t.Fatalf("it must leave the plan: plan=%q", c.Plan)
	}
	if c.SprintStart != today || !onGrid(t, fake, "c1") {
		t.Fatalf("it must stay in the working area: sprint=%q onGrid=%v", c.SprintStart, onGrid(t, fake, "c1"))
	}
}

// A card carrying only a project name is on no board of its own: the
// Project board renders columns by epic, and the weekly plan derives a
// slot's band by epic. So the name is a label, not a home — the × treats
// the card like any other, and with nowhere else to be it is deleted rather
// than left alive where no board would show it.
func TestACardThatOnlyNamesAProjectIsNotSparedByThatName(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "project, no column", Team: "platform", Project: "core",
			Assignees: []string{"kvaps"}, SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "c1", "grid"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("the project name is no home: with nowhere else to be, the card goes")
	}
}

// The invariant behind all of it: a card the × keeps is a card some board
// still shows. Sparing one for what it carries — a person, a progress bar —
// left it alive with no sprint, no dates, no band and no column: findable
// nowhere, deletable nowhere.
func TestACardTheRemoveKeepsIsAlwaysOnSomeBoard(t *testing.T) {
	today := board.TodayIso()
	shown := func(t *testing.T, fake *fakeBackend, id string) bool {
		t.Helper()
		b, _ := fake.LoadBoard(t.Context(), "o")
		if onGrid(t, fake, id) {
			return true
		}
		bands := board.WeeklyPlanAt(b, "platform", board.MondayOf(today), today)
		for _, c := range append(append([]board.Card{}, bands.Wed...), bands.Fri...) {
			if c.ItemID == id {
				return true
			}
		}
		for _, c := range board.MeView(b, "", today) {
			if c.ItemID == id {
				return true
			}
		}
		c, ok := findCard(b, id)
		return ok && c.Epic != "" // a slot lives on the Project board
	}
	cases := []struct{ name, first, second string }{
		{"out of the working area, then out of the plan", "grid", "plan"},
		{"out of the plan, then out of the working area", "plan", "grid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := gridBoard([]board.Card{
				{ItemID: "c1", Title: "worked, in both", Team: "platform",
					Assignees: []string{"kvaps"}, Progress: 40,
					Plan: board.PlanFri, Week: board.MondayOf(today),
					SprintStart: today, StartDate: today, Day: today},
			})
			svc := New(fake)
			if err := svc.Remove(t.Context(), "o", "c1", tc.first); err != nil {
				t.Fatal(err)
			}
			if !shown(t, fake, "c1") {
				t.Fatalf("after the %s ×, the card is on no board", tc.first)
			}
			if err := svc.Remove(t.Context(), "o", "c1", tc.second); err != nil {
				t.Fatal(err)
			}
			b, _ := fake.LoadBoard(t.Context(), "o")
			if _, ok := findCard(b, "c1"); ok {
				t.Fatalf("emptied of both homes the card must be gone, not invisible: shown=%v", shown(t, fake, "c1"))
			}
		})
	}
}

// The subtasks leave the working area with their parent: left behind in the
// sprint it has left, they stand under a card the board no longer shows.
func TestSubtasksLeaveTheWorkingAreaWithTheirParent(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform", Plan: board.PlanFri, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
		{ItemID: "s", Title: "child", Team: "platform", Parent: "p",
			SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "p", "grid"); err != nil {
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

// A personal card has no weekly plan to be released from, and the plan × is
// the door release_from_plan now goes through — so the personal path must
// not be reachable by it. The invariant is stated here rather than assumed:
// a personal card handed to it is left behind or deleted by the personal
// rule, never treated as a plan card.
func TestReleaseFromPlanOnAPersonalCardFollowsThePersonalRule(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "p1", Title: "mine", Domain: "~kvaps", Progress: 40,
			StartDate: board.AddDays(today, -3), Day: board.AddDays(today, -3)},
	})
	if err := New(fake).ReleaseFromPlan(t.Context(), "o", "p1"); err != nil {
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

// release_from_plan is the same gesture as the plan × through another door
// (REST /actions/release-from-plan, MCP release_from_plan) and obeys the
// same rule — it is now the same code. It used to know only about a person
// and progress, so a card standing on the day grid that nobody had taken
// was destroyed by it: the very hole that was closed in Remove.
func TestReleaseFromPlanLeavesACardThatIsSomewhereElse(t *testing.T) {
	today := board.TodayIso()
	cases := []struct {
		name string
		card board.Card
	}{
		{"in the working area, nobody's", board.Card{ItemID: "c1", Team: "platform",
			Plan: board.PlanFri, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today}},
		{"dated but not in a sprint", board.Card{ItemID: "c1", Team: "platform",
			Plan: board.PlanFri, Week: board.MondayOf(today), StartDate: today, Day: today}},
		{"a slot, which its column keeps", board.Card{ItemID: "c1", Team: "platform",
			Project: "core", Epic: "Auth", Plan: board.PlanFri, Week: board.MondayOf(today),
			StartDate: today, Day: today}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := gridBoard([]board.Card{tc.card})
			if err := New(fake).ReleaseFromPlan(t.Context(), "o", "c1"); err != nil {
				t.Fatal(err)
			}
			b, _ := fake.LoadBoard(t.Context(), "o")
			c, ok := findCard(b, "c1")
			if !ok {
				t.Fatal("release_from_plan destroyed a card that was somewhere else")
			}
			if c.Plan != board.PlanNone {
				t.Fatalf("it must still leave the plan: plan=%q", c.Plan)
			}
		})
	}
	// A card that is nowhere else is still deleted by it.
	fake := gridBoard([]board.Card{{ItemID: "c1", Team: "platform",
		Plan: board.PlanFri, Week: board.MondayOf(today)}})
	if err := New(fake).ReleaseFromPlan(t.Context(), "o", "c1"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("a pure plan card, nowhere else, is deleted")
	}
}

// Leaving the working area is written down. A demote logs its sprint move
// because a card that leaves today's board without a word in its own history
// is a card nobody can account for (W6); the × that empties the working area
// is the same departure and was silent.
func TestLeavingTheWorkingAreaIsRecorded(t *testing.T) {
	today := board.TodayIso()
	fake := gridBoard([]board.Card{
		{ItemID: "c1", Title: "in both", Team: "platform",
			Plan: board.PlanFri, Week: board.MondayOf(today),
			SprintStart: today, StartDate: today, Day: today},
	})
	if err := New(fake).Remove(t.Context(), "o", "c1", "grid"); err != nil {
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
	if err := New(fake).Remove(t.Context(), "o", "s", "grid"); err != nil {
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
