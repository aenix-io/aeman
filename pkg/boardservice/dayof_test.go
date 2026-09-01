package boardservice

import (
	"context"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Every door reads a past day through THIS: the HTTP API, an agent over MCP,
// an embedder. Two copies of "what did that day look like" drift the moment
// the second one is written — the MCP tool had its own, which replaced the
// whole board with the past one and knew nothing of teams still working.
func TestTheDayOfABoardIsOneAnswer(t *testing.T) {
	const day, prev = "2026-08-24", "2026-08-17"
	f := newFake([]board.Card{
		{ItemID: "moved-on", Team: "portal", Progress: 90, Rank: "a"},
		{ItemID: "still-here", Team: "backoffice", Progress: 90, Rank: "b"},
	}, map[string]board.SprintState{
		"portal":     {Current: "2026-09-01", Previous: day},
		"backoffice": {Current: day, Previous: prev},
	})
	p := &pastBackend{fakeBackend: f, past: board.Board{
		SprintStates: map[string]board.SprintState{
			"portal":     {Current: day},
			"backoffice": {Current: day},
		},
		Cards: []board.Card{
			{ItemID: "moved-on", Team: "portal", Progress: 20, Rank: "a"},
			{ItemID: "still-here", Team: "backoffice", Progress: 20, Rank: "b"},
		},
	}}
	svc := New(p)

	bd, over, at, err := svc.BoardOfDay(context.Background(), "acme", day)
	if err != nil {
		t.Fatal(err)
	}
	if at.IsZero() || !over["portal"] || over["backoffice"] {
		t.Fatalf("over = %v, at = %v; the day is over for portal alone", over, at)
	}
	got := map[string]int{}
	for _, c := range bd.Cards {
		got[c.ItemID] = c.Progress
	}
	if got["moved-on"] != 20 {
		t.Fatalf("the settled team's card = %d%%, want that evening's 20%%", got["moved-on"])
	}
	if got["still-here"] != 90 {
		t.Fatalf("the working team's card = %d%%, want today's 90%%", got["still-here"])
	}
	// The moment is the day's last, in the board's own zone.
	if want, _ := board.EndOfDay(day); !at.Equal(want) {
		t.Fatalf("at = %v, want %v", at, want)
	}

	// Today, and a day nobody has moved past, are the live board — no
	// moment claimed, nothing marked.
	for _, d := range []string{board.TodayIso(), ""} {
		_, over, at, err := svc.BoardOfDay(context.Background(), "acme", d)
		if err != nil {
			t.Fatal(err)
		}
		if !at.IsZero() || len(over) != 0 {
			t.Fatalf("day %q: over=%v at=%v — a live read claims no moment", d, over, at)
		}
	}
	if _, over, at, err := svc.BoardOfDay(context.Background(), "acme", "2026-09-01"); err != nil || !at.IsZero() || len(over) != 0 {
		t.Fatalf("a day every team is still in: over=%v at=%v err=%v", over, at, err)
	}
}
