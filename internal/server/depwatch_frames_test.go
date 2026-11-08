package server

import (
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

// listing its watcher would get on their visible board.
func restrictedFrameAgrees(t *testing.T, e *boardEntry, rights *domainRights, frames map[string]apiserver.Card, id string) apiserver.Card {
	t.Helper()
	e.mu.Lock()
	visible := board.Visible(e.board, rights.primary, rights.readable)
	e.mu.Unlock()
	c, _ := board.Find(visible, id)
	want := apiserver.CardResource(visible, c)
	got, ok := frames[id]
	if !ok {
		t.Fatalf("%s not sent: %v", id, keys(frames))
	}
	if !sameList(got.Spec.DependsOn, want.Spec.DependsOn) ||
		got.Status.Locked != want.Status.Locked || got.Status.HiddenBlockers != want.Status.HiddenBlockers ||
		!sameList(got.Status.BlockedBy, want.Status.BlockedBy) || !sameList(got.Status.Blocks, want.Status.Blocks) ||
		!sameList(got.Status.EffectiveCustomers, want.Status.EffectiveCustomers) ||
		!sameList(got.Status.DependentCustomers, want.Status.DependentCustomers) {
		t.Fatalf("%s frame spec %v status %+v\nlisting spec %v status %+v", id, got.Spec.DependsOn, got.Status, want.Spec.DependsOn, want.Status)
	}
	return got
}

// is JSON, and an empty list is left out of it).
func sameList(a, b any) bool {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Len() == 0 && vb.Len() == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// is what their listing serves.
func TestADependencyFrameNamesNoTargetItsWatcherCannotRead(t *testing.T) {
	store := newBoardStore()
	b := depWatchBoard()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "infra" || b.Cards[i].ItemID == "infra-t" {
			b.Cards[i].Domain = "closed"
		}
	}
	e := seedEntry(store, "k", b)
	rights := rightsOn([]string{"shared"}, nil)
	narrow, cancel := store.subscribeAs("k", "bob", nil, map[string]bool{"cards": true}, rights)
	defer cancel()
	change(t, e, "epic", func(c *board.Card) { c.Title += "!" })
	got := restrictedFrameAgrees(t, e, rights, cardFrames(narrow), "epic")
	if len(got.Spec.DependsOn) != 0 {
		t.Fatalf("epic frame names %v", got.Spec.DependsOn)
	}
}

// nothing waits for it and nothing is a dependent customer of it.
func TestAFrameKeepsTheCustomersWaitingWorkGivesItsWatcher(t *testing.T) {
	store := newBoardStore()
	b := depWatchBoard()
	b.Cards = append(b.Cards,
		board.Card{ItemID: "secret", Title: "Secret", Type: board.TypeInitiative, Customers: []string{"acme"}, Domain: "closed"},
		board.Card{ItemID: "mid", Title: "Mid", Type: board.TypeEpic, Parent: "secret"},
		board.Card{ItemID: "kid", Title: "Kid", Parent: "mid", Team: "alpha", Parked: true},
		board.Card{ItemID: "waiter", Title: "Waiter", Team: "alpha", Parked: true, Customers: []string{"acme"}, DependsOn: []string{"mid"}})
	e := seedEntry(store, "k", b)
	rights := rightsOn([]string{"shared"}, nil)
	narrow, cancel := store.subscribeAs("k", "bob", nil, map[string]bool{"cards": true}, rights)
	defer cancel()
	change(t, e, "kid", func(c *board.Card) { c.Title += "!" })
	got := restrictedFrameAgrees(t, e, rights, cardFrames(narrow), "kid")
	if !reflect.DeepEqual(got.Status.EffectiveCustomers, []string{"acme"}) {
		t.Fatalf("kid frame customers %v, want acme through the waiter", got.Status.EffectiveCustomers)
	}
}

// not on the same basis in the listing and on the frame.
func TestAnEpicTargetWithHiddenLeavesCountsAlikeInListingAndFrame(t *testing.T) {
	store := newBoardStore()
	// On the watcher's board the epic's one readable task is done; on the
	// whole board a task they cannot read is still open.
	b := targetEpicBoard()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "infra-t" {
			b.Cards[i].Domain = "closed"
		}
	}
	e := seedEntry(store, "k", b)
	rights := rightsOn([]string{"shared"}, nil)
	narrow, cancel := store.subscribeAs("k", "bob", nil, map[string]bool{"cards": true}, rights)
	defer cancel()
	change(t, e, "t", func(c *board.Card) { c.Title += "!" })
	restrictedFrameAgrees(t, e, rights, cardFrames(narrow), "t")
}
