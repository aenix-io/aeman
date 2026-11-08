package server

import (
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

// removeAndAnnounce deletes a card from the cache the way a write does — the
// row gone from the board, then the DELETED fan-out.
func removeAndAnnounce(t *testing.T, e *boardEntry, id string) {
	t.Helper()
	e.mu.Lock()
	c, _ := board.Find(e.board, id)
	e.removeCard(id)
	e.cardChanged("", c, "DELETED")
	e.mu.Unlock()
	settled(t, e)
}

// arrive puts a card into the cache the way a fetch or a reload does: a card
// the cache has never seen, announced as ADDED.
func arrive(t *testing.T, e *boardEntry, c board.Card) {
	t.Helper()
	e.mu.Lock()
	e.upsertCard(c)
	e.cardChanged("", c, "ADDED")
	e.mu.Unlock()
	settled(t, e)
}

// targetEpicBoard is depWatchBoard with a finished task beside the target
// epic: its one open task is all that keeps the epic undone.
func targetEpicBoard() board.Board {
	b := depWatchBoard()
	b.Cards = append(b.Cards, board.Card{ItemID: "infra-done", Title: "Cables", Parent: "infra", Team: "alpha", Progress: 100})
	return b
}

func wantUnlocked(t *testing.T, got map[string]apiserver.Card, ids ...string) {
	t.Helper()
	for _, id := range ids {
		f, ok := got[id]
		if !ok || f.Status.Locked {
			t.Fatalf("%s: sent %v, %+v; want a frame with locked=false (sent %v)", id, ok, f.Status, keys(got))
		}
	}
}

// M6(a). Deleting the last open task of a target epic finishes the epic, so
// the cards that wait for it are unlocked on the watch.
func TestDeletingATargetsLastOpenTaskUnlocksItsWaiters(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", targetEpicBoard())
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	removeAndAnnounce(t, e, "infra-t")
	wantUnlocked(t, cardFrames(raw), "epic", "t", "sub")
}

// M6(b). Moving that task out from under the target epic finishes the epic
// too: the chain it LEFT is judged, not only the one it joined.
func TestMovingATargetsLastOpenTaskOutUnlocksItsWaiters(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", targetEpicBoard())
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	change(t, e, "infra-t", func(c *board.Card) { c.Parent = "" })
	wantUnlocked(t, cardFrames(raw), "epic", "t", "sub")
}

// M6(c). Deleting a card that waits re-sends what it waited for: it no
// longer blocks it, nor is it for its customers.
func TestDeletingAWaitingCardReachesItsTargets(t *testing.T) {
	store := newBoardStore()
	b := depWatchBoard()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "w" {
			b.Cards[i].DependsOn = []string{"db"}
		}
	}
	e := seedEntry(store, "k", b)
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	removeAndAnnounce(t, e, "w")
	got := cardFrames(raw)
	db, ok := got["db"]
	if !ok || len(db.Status.Blocks) != 0 || len(db.Status.EffectiveCustomers) != 0 || len(db.Status.DependentCustomers) != 0 {
		t.Fatalf("db after w was deleted: sent %v, %+v", ok, db.Status)
	}
}

// M6(d). Renaming a target re-sends what waits for it, its blockedBy naming
// the new title where it names it, and the waiting card renamed in the
// target's blocks.
func TestRenamingATargetReachesWhatWaitsForIt(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", depWatchBoard())
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	change(t, e, "infra", func(c *board.Card) { c.Title = "Infrastructure" })
	got := cardFrames(raw)
	for _, id := range []string{"epic", "t", "sub"} {
		f, ok := got[id]
		if !ok || len(f.Status.BlockedBy) != 1 || f.Status.BlockedBy[0].Title != "Infrastructure" {
			t.Fatalf("%s after the target was renamed: sent %v, %+v", id, ok, f.Status.BlockedBy)
		}
	}
	change(t, e, "infra", func(c *board.Card) { c.Type = board.TypeInitiative })
	if f := cardFrames(raw)["epic"]; len(f.Status.BlockedBy) != 1 || f.Status.BlockedBy[0].Type != board.TypeInitiative {
		t.Fatalf("epic after the target was re-typed: %+v", f.Status.BlockedBy)
	}
	change(t, e, "epic", func(c *board.Card) { c.Title = "Payments" })
	if f, ok := cardFrames(raw)["infra"]; !ok || len(f.Status.Blocks) != 1 || f.Status.Blocks[0].Title != "Payments" {
		t.Fatalf("infra after its waiter was renamed: sent %v, %+v", ok, f.Status.Blocks)
	}
}

// M6(e). A card that arrives already waiting — from a fetch or a reload's
// diff) reaches its targets like a written one.
func TestAFetchedWaitingCardReachesItsTargets(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", depWatchBoard())
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	arrive(t, e, board.Card{ItemID: "w2", Title: "Fetched", Team: "alpha", Parked: true,
		Customers: []string{"acme"}, DependsOn: []string{"db"}})
	db, ok := cardFrames(raw)["db"]
	if !ok || len(db.Status.Blocks) != 1 || !reflect.DeepEqual(db.Status.EffectiveCustomers, []string{"acme"}) {
		t.Fatalf("db after a waiting card arrived: sent %v, %+v", ok, db.Status)
	}
}

// M6(f). Moving a waiting card under another parent changes its inherited
// customers, and passes them on: its targets are re-sent.
func TestReparentingAWaitingCardReachesItsTargets(t *testing.T) {
	store := newBoardStore()
	b := depWatchBoard()
	b.Customers = append(b.Customers, board.Customer{ID: "zeta", Name: "Zeta"})
	b.Cards = append(b.Cards,
		board.Card{ItemID: "plan", Title: "Plan", Type: board.TypeEpic, Customers: []string{"zeta"}},
		board.Card{ItemID: "w3", Title: "Mover", Team: "alpha", Parked: true, DependsOn: []string{"db"}})
	e := seedEntry(store, "k", b)
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	change(t, e, "w3", func(c *board.Card) { c.Parent = "plan" })
	db, ok := cardFrames(raw)["db"]
	if !ok || !reflect.DeepEqual(db.Status.EffectiveCustomers, []string{"zeta"}) {
		t.Fatalf("db after its waiter moved under a plan for Zeta: sent %v, %+v", ok, db.Status)
	}
}
