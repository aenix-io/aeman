package board

import (
	"reflect"
	"testing"
)

// A task is in exactly one of two places (docs/design/backlog.md, K1).
//
// In WORK it has a date or a sprint and stands on the Team and Me boards by
// the day rules. In its team's BACKLOG it has neither and is parked: one
// ranked list per team, top is next. There is no third place — the Triage
// strip, which held open tasks nobody had dated, weeked or parked, is gone,
// and every door that used to leave a task there parks it instead (K3).
//
// Every team HAS a backlog, the no-team group included. It is not declared
// anywhere and cannot be made, renamed or removed: a team that plans has work
// it is not planning, so the list is a fact about the team.

// TestATaskIsInWorkOrInItsBacklogNeverBothNeverNeither pins K1 as the three
// questions the board asks of a task: in work (a date or a sprint), in the
// backlog (parked), or UNPLACED — the strip's old shape, which no door leaves
// a task in and schema 3 parks. A parked card carrying dates (a direct writer
// can write one) reads as parked: the day boards leave it out (below), so it
// is drawn once, where it can be dealt with.
func TestATaskIsInWorkOrInItsBacklogNeverBothNeverNeither(t *testing.T) {
	today := TodayIso()
	cases := []struct {
		name                     string
		card                     Card
		inWork, parked, unplaced bool
	}{
		{"a dated task is in work", Card{StartDate: today, Day: today}, true, false, false},
		{"a task in a sprint is in work", Card{SprintStart: today}, true, false, false},
		{"an end date alone is a date", Card{Day: today}, true, false, false},
		{"a parked task is in its backlog", Card{Team: "alpha", Parked: true}, false, true, false},
		{"the no-team group has a backlog too", Card{Parked: true}, false, true, false},
		{"open, undated, unparked is unplaced", Card{Team: "alpha"}, false, false, true},
		{"progress does not place a card", Card{Team: "alpha", Progress: 40}, false, false, true},
		{"finished work is not asked to be placed", Card{Progress: 100}, false, false, false},
		{"a planning card is not a task", Card{Type: TypeEpic}, false, false, false},
		{"a review card with a reviewer follows its original", Card{ReviewOf: "o", Assignees: []string{"bob"}}, false, false, false},
		{"a state card is the roster", Card{Title: SprintStateTitle}, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InWork(tc.card); got != tc.inWork {
				t.Errorf("InWork = %v, want %v", got, tc.inWork)
			}
			if got := InBacklog(tc.card); got != tc.parked {
				t.Errorf("InBacklog = %v, want %v", got, tc.parked)
			}
			if got := Unplaced(tc.card, false); got != tc.unplaced {
				t.Errorf("Unplaced = %v, want %v", got, tc.unplaced)
			}
		})
	}
	// A subtask rides its parent: it has no place of its own to be missing.
	if Unplaced(Card{Team: "alpha", Parent: "p"}, true) {
		t.Error("a subtask is placed by its parent, never unplaced on its own")
	}
}

// The backlog is not a plan, so it draws on no day board. This is the same
// answer a week AHEAD gets (B1), and for the same reason: nobody is doing this
// work today.
func TestABacklogCardIsOnNoDayBoard(t *testing.T) {
	today := TodayIso()
	b := Board{
		Cards: []Card{{ItemID: "listed", Team: "alpha", Parked: true,
			Assignees: []string{"kvaps"}, StartDate: today, Day: today, SprintStart: today}},
		SprintStates: map[string]SprintState{"alpha": {Current: today}},
	}
	if got := TeamGrid(b, "alpha", today); len(got) != 0 {
		t.Fatalf("TeamGrid = %d card(s); a parked card is not this week's work", len(got))
	}
	if got := MeView(b, "kvaps", today); len(got) != 0 {
		t.Fatalf("MeView = %d card(s); a parked card is nobody's day", len(got))
	}
}

// Every team has a shelf of its own, and nothing declares it. That is the
// point: a backlog nobody had to create is one that is there the first time
// somebody needs it, and a team cannot end up in the state "no backlog yet"
// which is where a shelf goes unused.
func TestEveryTeamHasABacklogWithoutDeclaringOne(t *testing.T) {
	// Nothing is declared anywhere: no board file, no roster entry.
	for _, team := range []string{"alpha", "beta", ""} {
		c := Card{ItemID: "x", Team: team, Parked: true}
		if !InBacklog(c) {
			t.Fatalf("team %q: a parked card is on its team's shelf, declared or not", team)
		}
	}
	// The no-team group is a team like any other here, and keeps its own.
	if !InBacklog(Card{Parked: true}) {
		t.Fatal("the no-team group has a shelf too")
	}
}

// The backlog lists a team's PARKED OPEN TASKS and nothing else (K2): no
// planning card (the Roadmap's), no
// subtask (it rides its parent), no review card with a reviewer (it follows
// the card it reviews) and nothing finished — the backlog never shows work
// that is over. The one review card it does list is a review nobody has
// taken (K10, TestAReviewNobodyHasTakenIsInTheBacklog).
func TestTheBacklogListsATeamsParkedOpenTasksOnly(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "epic", Type: TypeEpic},
		{ItemID: "task", Team: "alpha", Parked: true},
		{ItemID: "under-epic", Team: "alpha", Parked: true, Parent: "epic"},
		{ItemID: "kid", Team: "alpha", Parked: true, Parent: "task"},
		{ItemID: "review", Team: "alpha", Parked: true, ReviewOf: "task", Assignees: []string{"bob"}},
		{ItemID: "done", Team: "alpha", Parked: true, Progress: 100},
		{ItemID: "in-work", Team: "alpha", StartDate: "2026-09-01"},
		{ItemID: "unplaced", Team: "alpha"},
		{ItemID: "planning-parked", Type: TypeGoal, Parked: true},
	})
	ix := NewIndex(b)
	var got []string
	for _, c := range b.Cards {
		if OnBacklog(c, ix.IsSubtask) {
			got = append(got, c.ItemID)
		}
	}
	if want := []string{"task", "under-epic"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("OnBacklog = %v, want %v (a task under an epic is a task, H1)", got, want)
	}
}

// A backlog reads in the ROADMAP's order (K14): the path of sibling places
// from the root plan item down — goal order, then initiative within its goal,
// epic within its initiative, task within its epic — so the backlog and the
// Roadmap are one order, decided by the tree. A task with no plan item above
// it comes after every planned one, in its own place in the board's order.
// The Roadmap draws its roots level by level (goals, then initiatives with no
// goal, then epics with no initiative), so the first step of the path is
// read the same way.
func TestABacklogReadsInTheRoadmapsOrder(t *testing.T) {
	b := NewBoard([]Card{
		// Board order (the rank order every board reads): deliberately not
		// the tree's, so only the path can put the tasks right.
		{ItemID: "loose-1", Parked: true},
		{ItemID: "t-e2", Parent: "e2", Parked: true},
		{ItemID: "g2", Type: TypeGoal},
		{ItemID: "e2", Type: TypeEpic, Parent: "i1"},
		{ItemID: "t-g2", Parent: "g2", Parked: true},
		{ItemID: "g1", Type: TypeGoal},
		{ItemID: "loose-2", Parked: true},
		{ItemID: "i1", Type: TypeInitiative, Parent: "g1"},
		{ItemID: "e1", Type: TypeEpic, Parent: "i1"},
		{ItemID: "t-e1-b", Parent: "e1", Parked: true},
		{ItemID: "t-e1-a", Parent: "e1", Parked: true},
		{ItemID: "t-i1", Parent: "i1", Parked: true},
		// A root initiative: the Roadmap draws it after every goal.
		{ItemID: "i-root", Type: TypeInitiative},
		{ItemID: "t-iroot", Parent: "i-root", Parked: true},
	})
	ix := NewIndex(b)
	var listed []Card
	for _, c := range b.Cards {
		if ix.OnBacklog(c) {
			listed = append(listed, c)
		}
	}
	want := []string{
		// g2 stands before g1 in the board's order: its task first.
		"t-g2",
		// g1 ▸ i1: e2 stands before e1 under i1, and the task directly under
		// i1 comes after both epics (it is after them among i1's children).
		"t-e2", "t-e1-b", "t-e1-a", "t-i1",
		// The root initiative after every goal.
		"t-iroot",
		// No plan item above: after every planned task, in board order.
		"loose-1", "loose-2",
	}
	if got := ids(BacklogOrder(ix, listed)); !reflect.DeepEqual(got, want) {
		t.Fatalf("BacklogOrder = %v\nwant          %v", got, want)
	}
}

// A backlog reads by PRIORITY first (K15): the EFFECTIVE priority — a
// card's own, else the nearest ancestor's — P0, then P1 … P4, then none;
// within each group the Roadmap's order. A task with no plan item above it
// takes its place in its group after the planned ones.
func TestABacklogReadsByPriorityThenInTheRoadmapsOrder(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "g1", Type: TypeGoal, Priority: PriorityP2},
		{ItemID: "g2", Type: TypeGoal},
		{ItemID: "a", Parent: "g1", Parked: true},                       // P2 from g1
		{ItemID: "b", Parent: "g1", Parked: true, Priority: PriorityP1}, // its own P1 wins over g1's P2
		{ItemID: "c", Parent: "g2", Parked: true},                       // none
		{ItemID: "d", Parent: "g2", Parked: true, Priority: PriorityP3},
		{ItemID: "loose", Parked: true, Priority: PriorityP1},
		{ItemID: "e", Parent: "g1", Parked: true, Priority: PriorityP1},
		{ItemID: "y", Parent: "g2", Parked: true, Priority: PriorityP4},
		{ItemID: "z", Parent: "g2", Parked: true, Priority: PriorityP0},
	})
	ix := NewIndex(b)
	var listed []Card
	for _, c := range b.Cards {
		if ix.OnBacklog(c) {
			listed = append(listed, c)
		}
	}
	// P0: z; P1: b, e (planned, Roadmap order), then loose; P2: a; P3: d;
	// P4: y; none: c.
	if got, want := ids(BacklogOrder(ix, listed)), []string{"z", "b", "e", "loose", "a", "d", "y", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("BacklogOrder = %v, want %v", got, want)
	}
}

// The EFFECTIVE priority (K15, like the effective team, O3): a card's own
// when it has one, else the nearest ancestor's that has one, else none —
// computed on reading, never written down. It names where it came from.
func TestTheEffectivePriorityIsOwnElseTheNearestAncestors(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "g", Type: TypeGoal, Priority: PriorityP3},
		{ItemID: "i", Type: TypeInitiative, Parent: "g", Priority: PriorityP2},
		{ItemID: "e", Type: TypeEpic, Parent: "i"},
		{ItemID: "t", Parent: "e"},
		{ItemID: "own", Parent: "e", Priority: PriorityP1},
		{ItemID: "bare", Type: TypeGoal},
		{ItemID: "u", Parent: "bare"},
		{ItemID: "o", Stage: StageReview, Parent: "e"},
		{ItemID: "r", ReviewOf: "o"},
	})
	ix := NewIndex(b)
	for _, tc := range []struct {
		id, want, from string
	}{
		{"g", "P3", ""},  // its own: from nobody
		{"i", "P2", ""},  // its own beats the goal's
		{"e", "P2", "i"}, // the nearest ancestor's, not the goal's
		{"t", "P2", "i"},
		{"own", "P1", ""},
		{"bare", "", ""},
		{"u", "", ""},    // nobody above has one
		{"r", "P2", "i"}, // a review card counts under its task's
	} {
		c, _ := ix.Find(tc.id)
		got, from := ix.EffectivePriority(c)
		if string(got) != tc.want || from.ItemID != tc.from {
			t.Errorf("%s: %q from %q, want %q from %q", tc.id, got, from.ItemID, tc.want, tc.from)
		}
	}
	for _, ok := range []string{"P0", "P1", "P2", "P3", "P4", ""} {
		if !ValidPriority(ok) {
			t.Errorf("%q is a priority", ok)
		}
	}
	for _, bad := range []string{"P5", "p1", "P-1", "high"} {
		if ValidPriority(bad) {
			t.Errorf("%q is not a priority", bad)
		}
	}
}

// A review NOBODY HAS TAKEN (K10, amended 2026-11-07) takes the effective
// priority of the task it reviews and stands FIRST within that priority —
// before the tasks of the group — the reviews among themselves in the
// Roadmap's order of the tasks they review; a review of a task with no
// priority is first in the "no priority" group.
func TestAReviewNobodyHasTakenLeadsItsTasksPriority(t *testing.T) {
	today := "2026-11-02"
	b := NewBoard([]Card{
		{ItemID: "g1", Type: TypeGoal, Priority: PriorityP1},
		{ItemID: "g2", Type: TypeGoal},
		// The reviews first in the board's order: only the reviewed tasks'
		// places may put them right.
		{ItemID: "r-late", ReviewOf: "o-late", Parked: true},
		{ItemID: "r-none", ReviewOf: "o-none", Parked: true},
		{ItemID: "r-early", ReviewOf: "o-early", Parked: true},
		{ItemID: "o-early", Parent: "g1", Stage: StageReview, SprintStart: today},
		{ItemID: "t1", Parent: "g1", Parked: true},
		{ItemID: "o-late", Parent: "g1", Stage: StageReview, SprintStart: today},
		{ItemID: "t-none", Parent: "g2", Parked: true},
		{ItemID: "o-none", Parent: "g2", Stage: StageReview, SprintStart: today},
		{ItemID: "t0", Parked: true, Priority: PriorityP0},
	})
	ix := NewIndex(b)
	var listed []Card
	for _, c := range b.Cards {
		if ix.OnBacklog(c) {
			listed = append(listed, c)
		}
	}
	want := []string{
		"t0",                      // P0: no review there
		"r-early", "r-late", "t1", // P1 (from g1): its reviews first, in their tasks' order
		"r-none", "t-none", // none: its review first
	}
	if got := ids(BacklogOrder(ix, listed)); !reflect.DeepEqual(got, want) {
		t.Fatalf("BacklogOrder = %v, want %v", got, want)
	}
}

// BacklogKey is the place itself, comparable on its own: the client mirrors
// it card by card (backlogboard.ts), so the edges are pinned here too.
func TestTheBacklogKeyComparesPathsStepByStep(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "g", Type: TypeGoal},
		{ItemID: "under-g", Parent: "g", Parked: true},
		{ItemID: "e", Type: TypeEpic, Parent: "g"},
		{ItemID: "under-e", Parent: "e", Parked: true},
	})
	ix := NewIndex(b)
	k := func(id string) BacklogKey { c, _ := ix.Find(id); return ix.BacklogKey(c) }
	// under-g stands before e among g's children, so before everything in e.
	if !k("under-g").Less(k("under-e")) || k("under-e").Less(k("under-g")) {
		t.Fatalf("under-g %v must come before under-e %v", k("under-g"), k("under-e"))
	}
	if k("under-g").Less(k("under-g")) {
		t.Fatal("a key is not less than itself")
	}
}

// A parked card is off the day boards, and its SUBTASKS go with it.
//
// This half is the parent's own. The board filters never place a subtask by
// itself — they ride their parent, which the API layer appends — so a parked
// parent leaving the grid is what takes its pieces along. The delivery side is
// pinned in pkg/apiserver, where the children are actually appended.
//
// Nothing is written on the subtasks to achieve it. Writing the shelf on each
// child would make coming back a second operation that can half-succeed: a
// card in the plan with two of its five pieces still parked. Derived, the
// return is free and cannot come back wrong.
func TestAParkedCardLeavesTheDayBoards(t *testing.T) {
	today := TodayIso()
	b := Board{
		Cards: []Card{
			{ItemID: "parent", Team: "alpha", Parked: true,
				Assignees: []string{"kvaps"}, StartDate: today, Day: today, SprintStart: today},
			{ItemID: "kid", Team: "alpha", Parent: "parent",
				Assignees: []string{"kvaps"}, StartDate: today, Day: today, SprintStart: today},
		},
		SprintStates: map[string]SprintState{"alpha": {Current: today}},
	}
	if got := TeamGrid(b, "alpha", today); len(got) != 0 {
		t.Fatalf("TeamGrid = %v; a parked card is not this week's work", ids(got))
	}
	if got := MeView(b, "kvaps", today); len(got) != 0 {
		t.Fatalf("MeView = %v; a parked card is nobody's day", ids(got))
	}

	// Planned again, it is back — and nothing had to be undone to bring it.
	b.Cards[0].Parked = false
	if got := ids(TeamGrid(b, "alpha", today)); len(got) != 1 || got[0] != "parent" {
		t.Fatalf("TeamGrid = %v; the card returns as it was", got)
	}
}

// A REVIEW NOBODY HAS TAKEN is the "needs a reviewer" item (K10): a review
// card with no assignee, open, and parked in the reviewed task's team's
// backlog. The backlog lists it (pinned above the ranked tasks — the
// listing's business); it is placed by being parked like a task, and one
// that is neither parked nor in work is unplaced, which keepPlaced parks. A
// review card with a reviewer follows the card it reviews, as ever.
func TestAReviewNobodyHasTakenIsInTheBacklog(t *testing.T) {
	waiting := Card{ItemID: "r", Team: "alpha", ReviewOf: "t", Parked: true}
	taken := Card{ItemID: "r2", Team: "alpha", ReviewOf: "t", Assignees: []string{"bob"}, StartDate: "2026-11-03"}
	if !NeedsReviewer(waiting) || NeedsReviewer(taken) {
		t.Fatal("a review with nobody on it needs a reviewer; one with a reviewer does not")
	}
	if NeedsReviewer(Card{ReviewOf: "t", Progress: 100}) {
		t.Fatal("a finished review needs nobody")
	}
	never := func(Card) bool { return false }
	if !OnBacklog(waiting, never) {
		t.Fatal("a parked review nobody has taken is in its team's backlog")
	}
	if OnBacklog(Card{ItemID: "r3", ReviewOf: "t", Assignees: []string{"bob"}, Parked: true}, never) {
		t.Fatal("a review with a reviewer is in no backlog: it follows its original")
	}
	if !Unplaced(Card{ReviewOf: "t", Team: "alpha"}, false) {
		t.Fatal("a review nobody has taken, not parked and not in work, is unplaced")
	}
	if Unplaced(waiting, false) || Unplaced(taken, false) {
		t.Fatal("a parked review and a reviewer's review are placed")
	}
	b := NewBoard([]Card{
		{ItemID: "st", Title: SprintStateTitle, Team: "alpha", SprintStart: "2026-11-03", StartDate: "2026-11-03"},
		{ItemID: "t", Team: "alpha", Stage: StageReview, Progress: 50, SprintStart: "2026-11-03"},
		waiting,
	})
	if !Reachable(b, "2026-11-03")["r"] {
		t.Fatal("the review waiting for a reviewer is on a board: its backlog")
	}
}

// A review of a SUBTASK is placed by the subtask's PLANNING chain and its own
// place — not its parent task's — exactly as the client reads it off the
// review card's status.lineage (the planning chain only). Pinned on both
// sides (backlogboard.test.ts, "a review of a subtask").
func TestAReviewOfASubtaskIsPlacedByThePlanningChain(t *testing.T) {
	today := "2026-11-02"
	b := NewBoard([]Card{
		{ItemID: "e", Type: TypeEpic},
		{ItemID: "t", Parent: "e", SprintStart: today},
		{ItemID: "t2", Parent: "e", Stage: StageReview, SprintStart: today},
		{ItemID: "s", Parent: "t", Stage: StageReview, SprintStart: today},
		{ItemID: "r-sub", ReviewOf: "s", Parked: true},
		{ItemID: "r-t2", ReviewOf: "t2", Parked: true},
	})
	ix := NewIndex(b)
	var listed []Card
	for _, c := range b.Cards {
		if ix.OnBacklog(c) {
			listed = append(listed, c)
		}
	}
	if got, want := ids(BacklogOrder(ix, listed)), []string{"r-t2", "r-sub"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("BacklogOrder = %v, want %v (s stands after t2 in the board's order)", got, want)
	}
}
