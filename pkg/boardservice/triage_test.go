package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A card sent to a week ahead leaves the day board — that is what makes the
// Triage board a regulator rather than a list (B1). Bringing it back into the
// week the team is working has to undo exactly that, or the card is left
// holding a week and standing on no board at all: not on the team's day, not
// on anyone's Me, findable by its id alone.
func TestPlacingBackInThisWeekPutsTheCardOnTheDayBoardAgain(t *testing.T) {
	today := board.TodayIso()
	thisWeek := board.MondayOf(today)
	ahead := board.AddDays(thisWeek, 7)

	f := newFake([]board.Card{{
		ItemID: "c1", Team: "alpha", Assignees: []string{"kvaps"},
		StartDate: today, Day: today, SprintStart: thisWeek,
	}}, map[string]board.SprintState{"alpha": {Current: thisWeek}})
	svc := f2svc(f)

	if err := svc.Place(ctx, "acme", "c1", ahead); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c.StartDate != "" || c.Day != "" || c.SprintStart != "" {
		t.Fatalf("a week ahead is off the day board: %+v", c)
	}

	if err := svc.Place(ctx, "acme", "c1", thisWeek); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	if c.Week != thisWeek {
		t.Fatalf("week = %q, want %q", c.Week, thisWeek)
	}
	if c.SprintStart != thisWeek {
		t.Fatalf("back in this week is back in the sprint being worked: %+v", c)
	}
	b, err := f.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(board.TeamGrid(b, "alpha", thisWeek)) != 1 {
		t.Fatalf("the card stands on the team's day again: %+v", c)
	}
}

// A card already standing on a day keeps the day it has: placing it in the
// week it is already in says nothing new about when it is being done.
func TestPlacingInThisWeekLeavesADayAlreadyChosenAlone(t *testing.T) {
	today := board.TodayIso()
	thisWeek := board.MondayOf(today)

	f := newFake([]board.Card{{
		ItemID: "c1", Team: "alpha",
		StartDate: today, Day: today, SprintStart: thisWeek,
	}}, map[string]board.SprintState{"alpha": {Current: thisWeek}})

	if err := f2svc(f).Place(ctx, "acme", "c1", thisWeek); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c.StartDate != today || c.Day != today || c.SprintStart != thisWeek {
		t.Fatalf("card = %+v", c)
	}
}

// A team whose sprint pointer has not been set yet: the week itself seeds it,
// the same fallback SetDates makes, so the card still lands somewhere.
func TestPlacingBackWithNoSprintPointerSeedsOneOnTheWeek(t *testing.T) {
	today := board.TodayIso()
	thisWeek := board.MondayOf(today)

	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Week: board.AddDays(thisWeek, 7)}},
		map[string]board.SprintState{})

	if err := f2svc(f).Place(ctx, "acme", "c1", thisWeek); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c.SprintStart != thisWeek {
		t.Fatalf("sprintStart = %q, want the week itself: %+v", c.SprintStart, c)
	}
}
