package server

import (
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
