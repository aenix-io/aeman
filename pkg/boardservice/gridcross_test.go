package boardservice

import (
	"context"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The grid × demotes a worked card into the previous sprint — dates and all —
// and RECORDS the day it took it off. Nothing live reads that mark on a team
// card (today's board drops the card, which is the point of the ×), but a
// record of that day gives the card back by it: it was worked and finished
// there, and its dates no longer say so (G60, P10 in plugin-impact.md).
func TestTheGridCrossRecordsTheDayItTookTheCardOff(t *testing.T) {
	today := board.TodayIso()
	prev := board.AddDays(today, -7)
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Progress: 100, SprintStart: today,
			StartDate: board.AddDays(today, -1), Day: today, Assignees: []string{"kvaps"}},
		{ItemID: "k1", Team: "alpha", Parent: "c1", Progress: 100, SprintStart: today,
			StartDate: board.AddDays(today, -1), Day: today},
	}, map[string]board.SprintState{"alpha": {Current: today, Previous: prev}})

	if err := f2svc(f).Remove(context.Background(), "acme", "c1", "grid"); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	if c == nil {
		t.Fatal("a worked card is demoted, not deleted")
	}
	// The demote itself: back into the sprint it was worked in.
	if c.SprintStart != prev || c.StartDate != prev || c.Day != prev {
		t.Fatalf("dates = start %q day %q sprint %q, want the previous sprint's %q",
			c.StartDate, c.Day, c.SprintStart, prev)
	}
	// And the day it was taken off, which is the only thing that still knows
	// where it stood.
	if c.LeftAt != today {
		t.Fatalf("leftAt = %q, want %q — a record of that day gives the card back by it", c.LeftAt, today)
	}
	// Its subtasks ride along, mark included: they stood there too.
	if k := f.get("k1"); k == nil || k.LeftAt != today {
		t.Fatalf("the subtask = %+v; it was on that day as well", k)
	}
}
