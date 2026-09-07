package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A card whose days RAN OUT is off every day board: its range ended before
// this week began and no sprint holds it. The Triage board is where it is
// still visible — a debt standing in the week it was owed in — so the gesture
// that says "this is being done now" is dragging it into the week the team is
// working. That gesture has to put it back on a day board, or the drag lies:
// the card takes the current week and stays invisible everywhere work is
// done, which is exactly where nine process turns sat on the production
// board, three of them a month past their week.
//
// The dates it gets are the ones a turn filed for this week would have: from
// TODAY (not the week's Monday — a start before the current sprint began
// would join the previous one) through the end of the week.
func TestPlacingBackAWeekWhoseDaysRanOutPutsTheCardOnTheDayBoard(t *testing.T) {
	today := board.TodayIso()
	thisWeek := board.MondayOf(today)
	oldWeek := board.AddDays(thisWeek, -14)

	f := newFake([]board.Card{{
		ItemID: "turn", Team: "alpha", Assignees: []string{"kvaps"}, Task: "t1",
		Stage: board.StageRecurrent, Week: oldWeek,
		StartDate: oldWeek, Day: board.AddDays(oldWeek, 6),
	}}, map[string]board.SprintState{"alpha": {Current: thisWeek}})
	svc := f2svc(f)

	if err := svc.Place(ctx, "acme", "turn", thisWeek); err != nil {
		t.Fatal(err)
	}
	c := f.get("turn")
	if c.StartDate != today || c.Day != board.AddDays(thisWeek, 6) {
		t.Fatalf("re-dated to this week: got %s…%s, want %s…%s",
			c.StartDate, c.Day, today, board.AddDays(thisWeek, 6))
	}
	if c.SprintStart != thisWeek {
		t.Fatalf("and into the sprint being worked, got %q", c.SprintStart)
	}
	b, err := f.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(board.MeView(b, "kvaps", today)) != 1 {
		t.Fatalf("the owner's day board must carry it again: %+v", c)
	}
}

// The card's own days are not touched when they still have life in them: one
// that runs INTO this week is being worked on the days somebody chose, and
// re-dating it from a drag would move work nobody asked to move.
func TestPlacingBackLeavesDatesThatStillReachThisWeek(t *testing.T) {
	today := board.TodayIso()
	thisWeek := board.MondayOf(today)
	lastWeek := board.AddDays(thisWeek, -7)

	f := newFake([]board.Card{{
		ItemID: "long", Team: "alpha", Assignees: []string{"kvaps"},
		Week: lastWeek, StartDate: lastWeek, Day: board.AddDays(thisWeek, 2),
		SprintStart: thisWeek,
	}}, map[string]board.SprintState{"alpha": {Current: thisWeek}})
	svc := f2svc(f)

	if err := svc.Place(ctx, "acme", "long", thisWeek); err != nil {
		t.Fatal(err)
	}
	c := f.get("long")
	if c.StartDate != lastWeek || c.Day != board.AddDays(thisWeek, 2) {
		t.Fatalf("a range still reaching this week is left alone, got %s…%s", c.StartDate, c.Day)
	}
}

// A Project-board SLOT is not re-dated by this either: its dates ARE its row,
// and Place moves them by the delta of the drag (the branch above), which is
// the Project board's own rule. A dated slot dragged from a week whose days
// ran out must keep that arithmetic, not be dropped into today.
func TestPlacingBackASlotStillMovesItsRow(t *testing.T) {
	today := board.TodayIso()
	thisWeek := board.MondayOf(today)
	oldWeek := board.AddDays(thisWeek, -14)

	f := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "P"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "E", Project: "P"},
		{ItemID: "slot", Team: "alpha", Epic: "E", Project: "P", Week: oldWeek,
			StartDate: oldWeek, Day: board.AddDays(oldWeek, 13)},
	}, map[string]board.SprintState{"alpha": {Current: thisWeek}})
	svc := f2svc(f)

	if err := svc.Place(ctx, "acme", "slot", thisWeek); err != nil {
		t.Fatal(err)
	}
	c := f.get("slot")
	if c.StartDate != thisWeek || c.Day != board.AddDays(thisWeek, 13) {
		t.Fatalf("a slot's row moves by the drag's delta, got %s…%s", c.StartDate, c.Day)
	}
}
