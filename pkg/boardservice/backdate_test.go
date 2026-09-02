package boardservice

import (
	"context"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A date set into the past must not take the card off the board. The calendar
// used to pin the card to a sprint STARTING on the chosen day: for a day the
// team has already passed, that is a sprint which closed — the day grid draws
// the current and the previous one, the Me board gates on them, and a
// carry-over only moves the closing sprint's own cards. The card was on no
// board at all, and the only way back was knowing its id.
//
// It happened in production the morning this was found: three cards, two of
// them made the day before, went that way in one working day.
//
// The dates are the person's to choose; the SPRINT is where the work stands,
// and that is the team's current one.
func TestADateIntoThePastKeepsTheCardOnTheBoard(t *testing.T) {
	const cur, prev, gone = "2026-09-02", "2026-09-01", "2026-08-24"
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "portal", Progress: 40, SprintStart: cur, StartDate: cur, Day: cur},
	}, map[string]board.SprintState{"portal": {Current: cur, Previous: prev}})

	if err := f2svc(f).SetDates(context.Background(), "acme", "c1", gone, gone); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	if c.StartDate != gone || c.Day != gone {
		t.Fatalf("the dates are the person's: start %q day %q, want %q", c.StartDate, c.Day, gone)
	}
	if c.SprintStart != cur {
		t.Fatalf("sprint = %q, want the team's current %q — a sprint that closed is on no board", c.SprintStart, cur)
	}
}

// A day the team can still reach keeps the rule it always had: the sprint
// active on that day, so re-dating inside the current or previous sprint puts
// the card where that day belongs.
func TestADateInsideTheTeamsReachTakesThatDaysSprint(t *testing.T) {
	const cur, prev = "2026-09-02", "2026-09-01"
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "portal", SprintStart: cur, StartDate: cur, Day: cur},
	}, map[string]board.SprintState{"portal": {Current: cur, Previous: prev}})

	if err := f2svc(f).SetDates(context.Background(), "acme", "c1", prev, prev); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c.SprintStart != prev {
		t.Fatalf("sprint = %q, want the previous sprint %q that day belongs to", c.SprintStart, prev)
	}
}

// A team with no sprint pointer at all has nothing to fall back on: the day
// itself seeds the card's sprint, as it always did.
func TestADateOnATeamWithNoSprintSeedsFromTheDay(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "fresh"},
	}, map[string]board.SprintState{})

	if err := f2svc(f).SetDates(context.Background(), "acme", "c1", "2026-08-24", "2026-08-24"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c.SprintStart != "2026-08-24" {
		t.Fatalf("sprint = %q, want the day itself", c.SprintStart)
	}
}
