package board

import "testing"

// A PLAN ITEM ROLLS UP FROM WHERE ITS OPEN TASKS STAND (H5, amended with
// K16, 2026-11-07). The Roadmap's UNASSIGNED moves tasks from work to the
// backlog, and the plan item above them must show it; a roll-up read off
// progress showed nothing (an epic whose last task was taken out of work at
// 49% still read "in progress"). Over the plan item's leaf tasks:
//
//   - any OPEN one in work (a date or a sprint, not parked) → in progress;
//   - open ones, none in work → BACKLOG;
//   - every one done → done; none at all → no items.
//
// The done/total counts are unchanged. Progress, a person on a task, a stage
// and a priority do not make a plan item in progress by themselves: a parked
// task at 49% is waiting, not being worked.
func TestAPlanItemRollsUpFromWhereItsOpenTasksStand(t *testing.T) {
	today := TodayIso()
	inWork := Card{ItemID: "w", Parent: "e", Team: "a", SprintStart: today, Day: today}
	parked := Card{ItemID: "p", Parent: "e", Team: "a", Parked: true}
	done := Card{ItemID: "d", Parent: "e", Team: "a", Progress: 100}
	with := func(c Card, f func(*Card)) Card { f(&c); return c }
	for _, tc := range []struct {
		name   string
		leaves []Card
		want   string
	}{
		{"no task under it", nil, HierarchyNoItems},
		{"every task done", []Card{done}, HierarchyDone},
		{"one task in work", []Card{inWork, parked}, HierarchyInProgress},
		{"waiting", []Card{parked}, HierarchyBacklog},
		{"done and waiting: still waiting", []Card{done, parked}, HierarchyBacklog},
		{"a P1 task waiting is waiting", []Card{with(parked, func(c *Card) { c.Priority = PriorityP1 })}, HierarchyBacklog},
		{"a parked task at 49% is waiting, not in progress", []Card{with(parked, func(c *Card) { c.Progress = 49 })}, HierarchyBacklog},
		{"a parked task somebody is named on is waiting", []Card{with(parked, func(c *Card) { c.Assignees = []string{"ann"} })}, HierarchyBacklog},
		{"a task in work at 0%, nobody's, is in work", []Card{with(inWork, func(c *Card) { c.Progress = 0 })}, HierarchyInProgress},
		{"a task in work held on review", []Card{with(inWork, func(c *Card) { c.Stage = StageReview; c.Progress = 90 })}, HierarchyInProgress},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBoard(append([]Card{{ItemID: "e", Title: "Epic", Type: TypeEpic}}, tc.leaves...))
			epic, _ := Find(b, "e")
			if st, _, _ := HierarchyStatus(b, epic); st != tc.want {
				t.Fatalf("the epic = %q, want %q", st, tc.want)
			}
		})
	}
}

// UNASSIGNED, seen from the epic: its last task out of work and the epic
// reads BACKLOG; one task taken back into work reads IN PROGRESS. The
// counts never move.
func TestTheStatusMenuShowsOnThePlanItem(t *testing.T) {
	today := TodayIso()
	b := NewBoard([]Card{
		{ItemID: "e", Type: TypeEpic},
		{ItemID: "t1", Parent: "e", Team: "a", Assignees: []string{"ann"}, SprintStart: today, Day: today, Progress: 49},
		{ItemID: "t2", Parent: "e", Team: "a", Parked: true},
		{ItemID: "t3", Parent: "e", Team: "a", Progress: 100},
	})
	epic, _ := Find(b, "e")
	check := func(when, want string) {
		t.Helper()
		if st, done, total := HierarchyStatus(b, epic); st != want || done != 1 || total != 3 {
			t.Fatalf("%s: %s %d/%d, want %s 1/3", when, st, done, total, want)
		}
	}
	check("a task in work", HierarchyInProgress)
	b.Cards[1].Parked, b.Cards[1].Assignees, b.Cards[1].SprintStart, b.Cards[1].Day = true, nil, "", ""
	check("after UNASSIGNED", HierarchyBacklog)
	b.Cards[2].Parked, b.Cards[2].SprintStart = false, today
	check("one taken into work", HierarchyInProgress)
}
