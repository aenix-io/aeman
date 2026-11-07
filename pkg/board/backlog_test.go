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

// Ordering inside a list is the reader's own: dragging is how a backlog gets
// prioritised, so the rank decides, and cards without one fall back to age —
// oldest first, since a card that has waited longest is the one being asked
// about.
func TestABacklogIsOrderedByHandThenByAge(t *testing.T) {
	cards := []Card{
		{ItemID: "c", CreatedAt: "2026-01-03T00:00:00Z"},
		{ItemID: "a", CreatedAt: "2026-01-01T00:00:00Z"},
		{ItemID: "b", CreatedAt: "2026-01-02T00:00:00Z"},
	}
	if got := ids(BacklogOrder(cards)); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("with no ranks the oldest is first: %v", got)
	}

	ranked := []Card{
		{ItemID: "old", CreatedAt: "2026-01-01T00:00:00Z"},
		{ItemID: "pinned", CreatedAt: "2026-01-09T00:00:00Z", Rank: "a"},
	}
	if got := ids(BacklogOrder(ranked)); !reflect.DeepEqual(got, []string{"pinned", "old"}) {
		t.Fatalf("a card somebody placed by hand outranks age: %v", got)
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
