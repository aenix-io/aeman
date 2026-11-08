package server

import (
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

func depWatchBoard() board.Board {
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "infra", Title: "Infra", Type: board.TypeEpic},
		{ItemID: "infra-t", Title: "Racks", Parent: "infra", Team: "alpha", Parked: true},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, DependsOn: []string{"infra"}},
		{ItemID: "t", Title: "Invoices", Parent: "epic", Team: "alpha", Parked: true},
		{ItemID: "sub", Title: "PDF", Parent: "t", Team: "alpha"},
		{ItemID: "db", Title: "DB", Team: "alpha", Parked: true},
		{ItemID: "w", Title: "Waits", Team: "alpha", Parked: true, Customers: []string{"acme"}},
	})
	b.Customers = []board.Customer{{ID: "acme", Name: "Acme"}}
	return b
}

func change(t *testing.T, e *boardEntry, id string, edit func(*board.Card)) {
	t.Helper()
	e.mu.Lock()
	c, _ := board.Find(e.board, id)
	edit(&c)
	e.upsertCard(c)
	e.cardChanged("", c, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
}

// N8 — the last open task of a target finishing unlocks every card that waits
// for it, and everything under them: each gets a frame with locked=false.
// Undoing it locks them again.
func TestATargetBecomingDoneUnlocksItsWaitersOnTheWatch(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", depWatchBoard())
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	change(t, e, "infra-t", func(c *board.Card) { c.Progress = 100 })
	got := cardFrames(raw)
	for _, id := range []string{"epic", "t", "sub"} {
		f, ok := got[id]
		if !ok || f.Status.Locked {
			t.Fatalf("%s after its target was done: sent %v, %+v", id, ok, f.Status)
		}
	}
	change(t, e, "infra-t", func(c *board.Card) { c.Progress = 40 })
	got = cardFrames(raw)
	for _, id := range []string{"epic", "t", "sub"} {
		if f, ok := got[id]; !ok || !f.Status.Locked {
			t.Fatalf("%s after its target reopened: sent %v, %+v", id, ok, f.Status)
		}
	}
}

// N8 — writing a card's dependsOn re-sends it, the cards under it and the
// targets added or removed (their blocks and inherited customers).
func TestADependencyChangeReachesTheCardItsSubtreeAndItsTargets(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", depWatchBoard())
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	change(t, e, "w", func(c *board.Card) { c.DependsOn = []string{"db"} })
	got := cardFrames(raw)
	if !got["w"].Status.Locked {
		t.Fatalf("w = %+v, want locked by db", got["w"].Status)
	}
	db, ok := got["db"]
	if !ok || len(db.Status.Blocks) != 1 || !reflect.DeepEqual(db.Status.EffectiveCustomers, []string{"acme"}) {
		t.Fatalf("db = sent %v %+v, want it to block w and be for Acme", ok, db.Status)
	}
	change(t, e, "epic", func(c *board.Card) { c.DependsOn = nil })
	got = cardFrames(raw)
	for _, id := range []string{"epic", "t", "sub", "infra"} {
		if _, ok := got[id]; !ok {
			t.Fatalf("%s not re-sent when the epic stopped waiting: %v", id, keys(got))
		}
	}
	if got["t"].Status.Locked || len(got["infra"].Status.Blocks) != 0 {
		t.Fatalf("after: t %+v, infra %+v", got["t"].Status, got["infra"].Status)
	}
}

// N4 — a frame is counted again for its subscriber: a blocker they cannot
// read counts and is not named, as their listing says.
func TestADependencyFrameHidesWhatItsWatcherCannotRead(t *testing.T) {
	store := newBoardStore()
	b := depWatchBoard()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "infra" || b.Cards[i].ItemID == "infra-t" || b.Cards[i].ItemID == "w" {
			b.Cards[i].Domain = "closed"
		}
	}
	b.Cards = append(b.Cards, board.Card{ItemID: "x", Title: "X", Team: "alpha", Parked: true})
	e := seedEntry(store, "k", b)
	rights := rightsOn([]string{"shared"}, nil)
	narrow, cancel := store.subscribeAs("k", "bob", nil, map[string]bool{"cards": true}, rights)
	defer cancel()
	change(t, e, "t", func(c *board.Card) { c.Title += "!" })
	change(t, e, "w", func(c *board.Card) { c.DependsOn = []string{"db"} })
	e.mu.Lock()
	visible := board.Visible(e.board, rights.primary, rights.readable)
	e.mu.Unlock()
	frames := cardFrames(narrow)
	for _, id := range []string{"t", "db"} {
		c, _ := board.Find(visible, id)
		want := apiserver.CardResource(visible, c).Status
		got, ok := frames[id]
		if !ok {
			t.Fatalf("%s not sent: %v", id, keys(frames))
		}
		if got.Status.Locked != want.Locked || got.Status.HiddenBlockers != want.HiddenBlockers ||
			!reflect.DeepEqual(got.Status.BlockedBy, want.BlockedBy) || !reflect.DeepEqual(got.Status.Blocks, want.Blocks) ||
			!reflect.DeepEqual(got.Status.EffectiveCustomers, want.EffectiveCustomers) ||
			!reflect.DeepEqual(got.Status.DependentCustomers, want.DependentCustomers) {
			t.Fatalf("%s frame %+v\nlisting %+v", id, got.Status, want)
		}
	}
	if frames["t"].Status.HiddenBlockers != 1 || len(frames["t"].Status.BlockedBy) != 0 {
		t.Fatalf("t = %+v, want one hidden blocker", frames["t"].Status)
	}
	if len(frames["db"].Status.Blocks) != 0 || len(frames["db"].Status.EffectiveCustomers) != 0 {
		t.Fatalf("db = %+v, want nothing of the hidden w", frames["db"].Status)
	}
}

func keys[T any](m map[string]T) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
