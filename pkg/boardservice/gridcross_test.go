package boardservice

import (
	"context"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The grid × on a worked card DELETES it. It used to demote the card into the
// previous sprint instead — dates and all — which took it off today's board
// while leaving it alive in a sprint no live view reaches: not on the day
// grid (its sprint is neither the current nor the previous one), not on the
// Me board (the sprint gate), and never picked up by a carry-over, which
// only moves the closing sprint's own cards. The board's history is git now,
// so the day the card was worked on holds it whole (G60) and there is
// nothing left for the demote to preserve.
func TestTheGridCrossDeletesAWorkedCard(t *testing.T) {
	today := board.TodayIso()
	prev := board.AddDays(today, -7)
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Progress: 100, SprintStart: today,
			StartDate: board.AddDays(today, -1), Day: today, Assignees: []string{"kvaps"}},
		{ItemID: "k1", Team: "alpha", Parent: "c1", Progress: 100, SprintStart: today,
			StartDate: board.AddDays(today, -1), Day: today},
	}, map[string]board.SprintState{"alpha": {Current: today, Previous: prev}})

	if err := f2svc(f).Remove(context.Background(), "acme", "c1", RemoveAuto); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c != nil {
		t.Fatalf("the card is still here: sprint %q, start %q, leftAt %q — the × deletes it, and the day it stood on keeps it",
			c.SprintStart, c.StartDate, c.LeftAt)
	}
	// Its subtasks go with it: they were the same piece of work.
	if k := f.get("k1"); k != nil {
		t.Fatalf("the subtask outlived its parent: %+v", k)
	}
}

// The same × on a card that was never worked deletes it too — it always did.
// The two answers are one now: the × means "off the board" and the board's
// history is what remembers.
func TestTheGridCrossDeletesAnUntouchedCardToo(t *testing.T) {
	today := board.TodayIso()
	prev := board.AddDays(today, -7)
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", SprintStart: today,
			StartDate: board.AddDays(today, -1), Day: today},
	}, map[string]board.SprintState{"alpha": {Current: today, Previous: prev}})

	if err := f2svc(f).Remove(context.Background(), "acme", "c1", RemoveAuto); err != nil {
		t.Fatal(err)
	}
	if c := f.get("c1"); c != nil {
		t.Fatalf("the card is still here: %+v", c)
	}
}

// A card in a Project-board COLUMN is still handed back to that column: the
// column is a home of its own, and the × empties the working area, not the
// plan. Deleting it would take a piece of the roadmap with it.
func TestTheGridCrossStillHandsAColumnCardBack(t *testing.T) {
	today := board.TodayIso()
	prev := board.AddDays(today, -7)
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Progress: 40, Epic: "Auth", Project: "freedom",
			SprintStart: today, StartDate: board.AddDays(today, -1), Day: today,
			Assignees: []string{"kvaps"}},
	}, map[string]board.SprintState{"alpha": {Current: today, Previous: prev}})

	if err := f2svc(f).Remove(context.Background(), "acme", "c1", RemoveAuto); err != nil {
		t.Fatal(err)
	}
	c := f.get("c1")
	if c == nil {
		t.Fatal("a card standing in a column is not deleted by the grid ×")
	}
	if c.Epic != "Auth" {
		t.Fatalf("it keeps its column: %+v", c)
	}
}
