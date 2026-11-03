package server

import (
	"encoding/json"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

// A SELECTION IS SHARED BOARD-WIDE, BOTH WAYS (#290). Presence is keyed by
// the client, so a lead selecting on the Team board and a person selecting
// on their Me board both show; a selection made on the Team board reaches a
// Me board's watch as much as the other way round — presence is not scoped
// to a view. Each frame names the CLIENT (the tab) it is from, the key the
// server holds it by, so two tabs of one person do not erase each other on
// the boards that draw them. A viewer who may not read the card's repository is told the
// person has selected nothing they can see (the mark clears), never the uid.
func TestASelectionReachesEveryBoardThatMayReadIt(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "open", Title: "open", Team: "alpha"},
		{ItemID: "secret", Title: "secret", Team: "alpha", Domain: "closed"},
	})
	seedEntry(store, "k", b)
	presence := map[string]bool{"presence": true}
	me := apiserver.Selector{View: "me", User: "bob"}
	bobMe, cancel := store.subscribeAs("k", "bob-me", &me, presence, rightsOn([]string{"shared"}, nil))
	defer cancel()
	team := apiserver.Selector{View: "team", Team: "alpha"}
	leadTeam, cancel2 := store.subscribeAs("k", "lead-team", &team, presence, rightsOn([]string{"shared", "closed"}, nil))
	defer cancel2()

	read := func(sub *subscription) presenceEntry {
		t.Helper()
		f := readFrame(t, sub)
		if f.Kind != "Presence" {
			t.Fatalf("frame kind = %q, want Presence", f.Kind)
		}
		var p presenceEntry
		if err := json.Unmarshal(f.Object, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// The lead selects on the Team board: bob's Me board hears it; the
	// lead's own tab does not echo it.
	store.SetPresence("k", "lead-team", "lead", "open")
	if p := read(bobMe); p.Login != "lead" || p.Card != "open" || p.Client != "lead-team" {
		t.Fatalf("bob's Me board heard %+v, want lead on «open» from the lead's tab", p)
	}
	noFrame(t, leadTeam)

	// Bob selects on his Me board: the lead's Team board hears it.
	store.SetPresence("k", "bob-me", "bob", "open")
	if p := read(leadTeam); p.Login != "bob" || p.Card != "open" {
		t.Fatalf("the lead's Team board heard %+v, want bob on «open»", p)
	}

	// The lead selects a card bob may not read: bob is told the lead's mark
	// cleared, not the card.
	store.SetPresence("k", "lead-team", "lead", "secret")
	if p := read(bobMe); p.Login != "lead" || p.Card != "" {
		t.Fatalf("bob heard %+v, want lead's mark cleared", p)
	}

	// And a newcomer is replayed only what it may read.
	late, cancel3 := store.subscribeAs("k", "bob-2", &me, presence, rightsOn([]string{"shared"}, nil))
	defer cancel3()
	seen := map[string]string{}
	for i := 0; i < 2; i++ {
		p := read(late)
		seen[p.Login] = p.Card
	}
	if seen["lead"] != "" || seen["bob"] != "open" {
		t.Fatalf("the replay = %v, want bob on «open» and lead's mark cleared", seen)
	}
}
