package board

import "testing"

// A PLAN ITEM ROLLS UP FROM WHERE ITS OPEN TASKS STAND (H5, amended with
// K15/K16, 2026-11-07). The Roadmap's status menu — TO DO and UNASSIGNED —
// moves tasks between "in work" and "waiting in a backlog", and the plan
// item above them must show it; a roll-up read off progress showed neither
// (an epic whose last task was taken out of work at 49% still read "in
// progress"). Over the plan item's OPEN leaf tasks:
//
//   - any of them in work (a date or a sprint, not parked) → in progress;
//   - none in work, every one marked TO DO → TO DO ("ready");
//   - none in work, not every one marked → BACKLOG;
//
// and as before: every leaf done → done; no leaf at all → no items. The
// done/total counts are unchanged. Progress, a person on a task, a stage do
// not make a plan item in progress by themselves: a parked task at 49% is
// waiting, not being worked.
func TestAPlanItemRollsUpFromWhereItsOpenTasksStand(t *testing.T) {
	today := TodayIso()
	inWork := Card{ItemID: "w", Parent: "e", Team: "a", SprintStart: today, Day: today}
	parked := Card{ItemID: "p", Parent: "e", Team: "a", Parked: true}
	ready := Card{ItemID: "r", Parent: "e", Team: "a", Parked: true, Todo: true}
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
		{"a task in work beside one marked TO DO", []Card{inWork, ready}, HierarchyInProgress},
		{"every open task marked TO DO", []Card{ready, with(ready, func(c *Card) { c.ItemID = "r2" })}, HierarchyReady},
		{"TO DO with the rest done", []Card{ready, done}, HierarchyReady},
		{"some marked TO DO, some not", []Card{ready, parked}, HierarchyBacklog},
		{"waiting, none marked", []Card{parked}, HierarchyBacklog},
		{"done and waiting: still waiting", []Card{done, parked}, HierarchyBacklog},
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

// The status menu, seen from the epic: UNASSIGNED takes its last task out of
// work and the epic reads BACKLOG; TO DO on it reads TO DO; one task taken
// back into work reads IN PROGRESS. The counts never move.
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
	// UNASSIGNED: back to the backlog, its 49% kept.
	b.Cards[1].Parked, b.Cards[1].Assignees, b.Cards[1].SprintStart, b.Cards[1].Day = true, nil, "", ""
	check("after UNASSIGNED", HierarchyBacklog)
	b.Cards[1].Todo, b.Cards[2].Todo = true, true
	check("after TO DO", HierarchyReady)
	b.Cards[2].Parked, b.Cards[2].Todo, b.Cards[2].SprintStart = false, false, today
	check("one taken into work", HierarchyInProgress)
}
