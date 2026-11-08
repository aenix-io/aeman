package server

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// takes it up again with the next.
func TestTheWatchForgetsDependenciesWhenTheLastOneGoes(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", depWatchBoard())
	change(t, e, "epic", func(c *board.Card) { c.DependsOn = nil })
	e.mu.Lock()
	anyDeps, depOf := e.anyDeps, e.depOf
	e.mu.Unlock()
	if anyDeps || depOf != nil {
		t.Fatalf("no dependency left: anyDeps %v, depOf %d entries", anyDeps, len(depOf))
	}
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	change(t, e, "w", func(c *board.Card) { c.DependsOn = []string{"db"} })
	if db, ok := cardFrames(raw)["db"]; !ok || len(db.Status.Blocks) != 1 {
		t.Fatalf("the next dependency is tracked again: sent %v, %+v", ok, db.Status)
	}
}
