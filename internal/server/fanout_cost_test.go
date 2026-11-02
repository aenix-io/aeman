package server

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

// One change under a planning card is announced to every raw watcher with
// the card's resource and each ancestor's — and each of those is built
// ONCE for the change, not once per watcher. An ancestor's standing is a
// walk over the board (its leaf tasks), so built per watcher it cost
// W × A walks under the entry lock for a single card edit; a carry-over
// burst over a board of thousands of cards with a few tabs open stalled
// every other write behind it.
func TestAChangeBuildsEachResourceOnceForEveryWatcher(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	e := seedEntry(store, "k", hierarchyWatchBoard(today))
	const watchers = 3
	for i := range watchers {
		_, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
		defer cancel()
		_ = i
	}
	builds := map[string]int{}
	orig := buildCard
	buildCard = func(b board.Board, ix *board.Index, c board.Card) apiserver.Card {
		builds[c.ItemID]++
		return orig(b, ix, c)
	}
	defer func() { buildCard = orig }()

	c1, _ := board.Find(e.board, "c1")
	c1.Parent = "epic"
	e.mu.Lock()
	e.upsertCard(c1)
	e.cardChanged("x", c1, "MODIFIED")
	e.mu.Unlock()

	for _, id := range []string{"c1", "epic", "init", "goal"} {
		if builds[id] != 1 {
			t.Errorf("%s built %d times for %d watchers, want once", id, builds[id], watchers)
		}
	}
}

// A change reads the board's hierarchy through ONE index: the chain the card
// joined, the chain it left, and the resources it is announced with. The
// ancestor walk used to go through board.Lineage, which builds an index of
// the whole board on every call — two or three full-board builds per card
// change under the entry lock, where one would do; a carry-over burst over
// a large board paid that for every card it moved.
func TestAChangeBuildsTheBoardIndexAtMostOnce(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	e := seedEntry(store, "k", hierarchyWatchBoard(today))
	_, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	builds := 0
	orig := newIndex
	newIndex = func(b board.Board) *board.Index {
		builds++
		return orig(b)
	}
	defer func() { newIndex = orig }()

	// A card that joins a chain of three planning cards.
	c1, _ := board.Find(e.board, "c1")
	c1.Parent = "epic"
	e.mu.Lock()
	e.upsertCard(c1)
	e.cardChanged("x", c1, "MODIFIED")
	e.mu.Unlock()
	if builds > 1 {
		t.Errorf("a card joining a chain built the board index %d times, want at most once", builds)
	}

	// A card with no parent at all names no chain to walk.
	builds = 0
	for _, c := range e.board.Cards {
		if c.Parent == "" && !board.IsPlanning(c) {
			e.mu.Lock()
			e.cardChanged("x", c, "MODIFIED")
			e.mu.Unlock()
			break
		}
	}
	if builds > 1 {
		t.Errorf("a parentless card built the board index %d times, want at most once", builds)
	}
}

// The walk to a card's planning ancestors reads the entry's own parent map:
// it costs the depth of the chain, not the size of the board. Built through
// board.Lineage it indexed the whole board — every card copied into a map —
// for each change, and once more for the chain a re-parented card left.
func TestTheAncestorWalkDoesNotIndexTheBoard(t *testing.T) {
	today := board.TodayIso()
	b := hierarchyWatchBoard(today)
	for i := range 5000 {
		b.Cards = append(b.Cards, board.Card{ItemID: fmt.Sprintf("filler-%d", i), Title: "filler", Team: "alpha",
			Description: strings.Repeat("x", 200)})
	}
	store := newBoardStore()
	e := seedEntry(store, "k", b)
	c1, _ := board.Find(e.board, "c1")
	c1.Parent = "epic"
	e.mu.Lock()
	e.upsertCard(c1)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := e.ancestorsChanged(c1, "MODIFIED")
	runtime.ReadMemStats(&after)
	e.mu.Unlock()
	if !reflect.DeepEqual(got, []string{"goal", "init", "epic"}) {
		t.Fatalf("ancestors = %v, want the chain root first", got)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 512<<10 {
		t.Errorf("the ancestor walk allocated %d KiB on a board of %d cards; it should cost the chain, not the board", grew>>10, len(e.board.Cards))
	}
}
