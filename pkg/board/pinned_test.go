package board

import (
	"strings"
	"testing"
)

// A visitor's ordering action never re-keys a card they cannot write
// (Board.Pinned): a rebalance window stops at it instead of reaching into
// another repository, and an epic's gather leaves such a task where it
// stands (an exception, like any) instead of refusing the whole move.

func TestARebalanceWindowStopsAtACardTheMoverCannotWrite(t *testing.T) {
	tight := "k" + strings.Repeat("0", 30) + "1"
	list := []Ranked{
		{ID: "far", Rank: "b"}, {ID: "p", Rank: "c", Fixed: true}, {ID: "w1", Rank: "d"},
		{ID: "k0", Rank: "k"}, {ID: "k1", Rank: tight}, {ID: "w2", Rank: "m"},
		{ID: "q", Rank: "n", Fixed: true}, {ID: "mover", Rank: "x"},
	}
	got, err := PlaceAfter(list, "mover", "k0")
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range got {
		if ch.ID == "p" || ch.ID == "q" || ch.ID == "far" {
			t.Fatalf("the window reached %s: %+v", ch.ID, got)
		}
	}
	// No room at all between two pinned cards: refused, nothing re-keyed.
	if _, err := PlaceAfter([]Ranked{{ID: "p", Rank: "k", Fixed: true}, {ID: "q", Rank: tight, Fixed: true}, {ID: "m", Rank: "z"}}, "m", "p"); err == nil {
		t.Fatal("a move with no room but across pinned cards was not refused")
	}
}

func TestAGatherLeavesATaskTheMoverCannotWrite(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("B2", "B", "T", "d").task("A2", "A", "T", "e").task("B1", "B", "T", "f").board()
	b.Pinned = map[string]bool{"B1": true}
	changes, err := MoveEpicPlan(b, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range changes {
		if ch.ID == "B1" {
			t.Fatalf("the pinned task was re-keyed: %+v", changes)
		}
	}
	if got := queueOf(ApplyRankChanges(b, changes), "T"); got != "B2 A1 A2 B1" {
		t.Fatalf("queue = %s", got)
	}
}
