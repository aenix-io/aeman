package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A card can be started ON THE TRIAGE BOARD, which schedules by the week and
// says nothing about a day: the week is the whole of the decision, and a
// create that drops it leaves the card where nobody put it.
func TestCreateWithAWeekAndNoDatesTakesTheWeekAlone(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: board.TodayIso()}})
	week := board.AddDays(board.MondayOf(board.TodayIso()), 14)

	c, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "Two weeks out", Team: "alpha", Zone: board.ZoneGray, Week: week,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.get(c.ItemID)
	if got.Week != week {
		t.Fatalf("week = %q, want %q", got.Week, week)
	}
	// A week ahead is on no day board until its Monday (B1): dates would put
	// it on today's, which is what choosing a week was for.
	if got.StartDate != "" || got.Day != "" || got.SprintStart != "" {
		t.Fatalf("a card of a week to come carries no day: %+v", got)
	}
}

// Started in the week being worked, a card belongs to today as well — the
// caller says so by giving it the day, and both must survive.
func TestCreateWithThisWeekAndADayKeepsBoth(t *testing.T) {
	today := board.TodayIso()
	week := board.MondayOf(today)
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: week}})

	c, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "For now", Team: "alpha", Zone: board.ZoneGray,
		Week: week, Start: today, Day: today,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.get(c.ItemID)
	if got.Week != week || got.StartDate != today || got.Day != today {
		t.Fatalf("card = %+v", got)
	}
	if got.SprintStart == "" {
		t.Fatalf("a card of this week joins the sprint being worked: %+v", got)
	}
}
