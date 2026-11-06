package server

import (
	"encoding/json"
	"reflect"
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

// A CARD FRAME CARRIES ONLY WHAT ITS WATCHER MAY READ. status.lineage,
// status.reviewed and status.dueDate name OTHER cards — their titles and
// dates — and a watcher who cannot read the repository one of those lives
// in is sent the frame without it: the crumb dropped, the reviewed card
// unnamed, the due date that came from it gone. A watcher who may read
// everything is sent all of it.
func TestACardFrameNamesOnlyCardsItsWatcherMayRead(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "epic", Title: "Secret epic", Type: board.TypeEpic, Day: "2026-12-18", Domain: "closed"},
		{ItemID: "t", Title: "Open task", Team: "alpha", Parent: "epic", Parked: true},
		{ItemID: "hidden", Title: "Secret task", Team: "alpha", Stage: board.StageReview, Domain: "closed"},
		{ItemID: "r", Title: "review: something", Team: "alpha", ReviewOf: "hidden", Parked: true},
	})
	e := seedEntry(store, "k", b)
	cards := map[string]bool{"cards": true}
	narrow, cancel := store.subscribeAs("k", "bob", nil, cards, rightsOn([]string{"shared"}, nil))
	defer cancel()
	wide, cancel2 := store.subscribeAs("k", "lead", nil, cards, rightsOn([]string{"shared", "closed"}, nil))
	defer cancel2()

	for _, id := range []string{"t", "r"} {
		e.mu.Lock()
		c, _ := board.Find(e.board, id)
		c.Title += "!"
		e.upsertCard(c)
		e.cardChanged("", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}
	frames := func(sub *subscription) map[string]apiserver.Card {
		out := map[string]apiserver.Card{}
		for {
			select {
			case raw := <-sub.ch:
				var f struct {
					Kind   string          `json:"kind"`
					Object json.RawMessage `json:"object"`
				}
				_ = json.Unmarshal(raw, &f)
				var c apiserver.Card
				if f.Kind == "Card" && json.Unmarshal(f.Object, &c) == nil {
					out[c.Metadata.UID] = c
				}
			default:
				return out
			}
		}
	}
	n, w := frames(narrow), frames(wide)
	if len(n["t"].Status.Lineage) != 0 || n["t"].Status.DueDate != "" {
		t.Fatalf("the narrow watcher was told of the secret epic: %+v", n["t"].Status)
	}
	if n["r"].Status.Reviewed != nil {
		t.Fatalf("the narrow watcher was told of the secret task: %+v", n["r"].Status.Reviewed)
	}
	if len(w["t"].Status.Lineage) != 1 || w["t"].Status.DueDate != "2026-12-18" || w["r"].Status.Reviewed == nil {
		t.Fatalf("the wide watcher was not told everything: %+v / %+v", w["t"].Status, w["r"].Status)
	}
}

// And the effective team and customers are worked out from those other
// cards too (O3, O4, O6): a watcher who cannot read the epic above a task is
// not told the epic's team or customers through the task, nor a hidden
// task's customers through its review card. What is left is what the
// watcher may read — the card's own, and its readable ancestors'.
func TestACardFrameInheritsOnlyFromCardsItsWatcherMayRead(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "epic", Title: "Secret epic", Type: board.TypeEpic, Team: "portal", Customers: []string{"cu1"}, Domain: "closed"},
		{ItemID: "t", Title: "Open task", Parent: "epic", Customers: []string{"cu2"}, Parked: true},
		{ItemID: "hidden", Title: "Secret task", Team: "alpha", Customers: []string{"cu1"}, Stage: board.StageReview, Domain: "closed"},
		{ItemID: "r", Title: "review: something", Team: "alpha", ReviewOf: "hidden", Parked: true},
	})
	b.Customers = []board.Customer{{ID: "cu1", Name: "Hidora"}, {ID: "cu2", Name: "Acme"}}
	e := seedEntry(store, "k", b)
	cards := map[string]bool{"cards": true}
	narrow, cancel := store.subscribeAs("k", "bob", nil, cards, rightsOn([]string{"shared"}, nil))
	defer cancel()
	wide, cancel2 := store.subscribeAs("k", "lead", nil, cards, rightsOn([]string{"shared", "closed"}, nil))
	defer cancel2()
	for _, id := range []string{"t", "r"} {
		e.mu.Lock()
		c, _ := board.Find(e.board, id)
		c.Title += "!"
		e.upsertCard(c)
		e.cardChanged("", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}
	n, w := cardFrames(narrow), cardFrames(wide)
	if st := n["t"].Status; st.EffectiveTeam != "" || !reflect.DeepEqual(st.EffectiveCustomers, []string{"cu2"}) {
		t.Fatalf("the narrow watcher inherits from the secret epic: team %q customers %v", st.EffectiveTeam, st.EffectiveCustomers)
	}
	if st := n["r"].Status; len(st.EffectiveCustomers) != 0 {
		t.Fatalf("the narrow watcher is told the secret task's customers: %v", st.EffectiveCustomers)
	}
	if st := w["t"].Status; st.EffectiveTeam != "portal" || !reflect.DeepEqual(st.EffectiveCustomers, []string{"cu1", "cu2"}) {
		t.Fatalf("the wide watcher was not told everything: team %q customers %v", st.EffectiveTeam, st.EffectiveCustomers)
	}
	if st := w["r"].Status; !reflect.DeepEqual(st.EffectiveCustomers, []string{"cu1"}) {
		t.Fatalf("the wide watcher's review card = %v, want the task's", st.EffectiveCustomers)
	}
}

// cardFrames drains a subscription into the last card frame per uid.
func cardFrames(sub *subscription) map[string]apiserver.Card {
	out := map[string]apiserver.Card{}
	for {
		select {
		case raw := <-sub.ch:
			var f struct {
				Kind   string          `json:"kind"`
				Object json.RawMessage `json:"object"`
			}
			_ = json.Unmarshal(raw, &f)
			var c apiserver.Card
			if f.Kind == "Card" && json.Unmarshal(f.Object, &c) == nil {
				out[c.Metadata.UID] = c
			}
		default:
			return out
		}
	}
}

// And the frame agrees with the LISTING the same watcher would get, which is
// built on their visible board: there a card's chain ends at the first
// ancestor they cannot read (board.Visible drops it, Index.Lineage stops),
// so a readable goal ABOVE an unreadable epic gives the task below nothing.
// The frame used to skip the unreadable epic and inherit from the goal. A
// review card with no team of its own reads the same way.
func TestAReadableAncestorAboveAnUnreadableOneGivesNothing(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "goal", Title: "Grow", Type: board.TypeGoal, Team: "portal", Customers: []string{"cu1"}},
		{ItemID: "epic", Title: "Secret epic", Type: board.TypeEpic, Parent: "goal", Domain: "closed"},
		{ItemID: "t", Title: "Open task", Parent: "epic", Parked: true},
		{ItemID: "r", Title: "review: Open task", ReviewOf: "t", Parked: true},
	})
	b.Customers = []board.Customer{{ID: "cu1", Name: "Hidora"}}
	e := seedEntry(store, "k", b)
	rights := rightsOn([]string{"shared"}, nil)
	narrow, cancel := store.subscribeAs("k", "bob", nil, map[string]bool{"cards": true}, rights)
	defer cancel()
	for _, id := range []string{"t", "r"} {
		e.mu.Lock()
		c, _ := board.Find(e.board, id)
		c.Title += "!"
		e.upsertCard(c)
		e.cardChanged("", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}
	got := cardFrames(narrow)
	e.mu.Lock()
	visible := board.Visible(e.board, rights.primary, rights.readable)
	e.mu.Unlock()
	for _, id := range []string{"t", "r"} {
		c, _ := board.Find(visible, id)
		listed := apiserver.CardResource(visible, c).Status
		st := got[id].Status
		if st.EffectiveTeam != "" || len(st.EffectiveCustomers) != 0 {
			t.Errorf("%s's frame inherits past the unreadable epic: team %q customers %v", id, st.EffectiveTeam, st.EffectiveCustomers)
		}
		if st.EffectiveTeam != listed.EffectiveTeam || !reflect.DeepEqual(st.EffectiveCustomers, listed.EffectiveCustomers) {
			t.Errorf("%s: the frame says %q %v, the listing %q %v", id, st.EffectiveTeam, st.EffectiveCustomers, listed.EffectiveTeam, listed.EffectiveCustomers)
		}
	}
}

// And a review card with no team of its own counts under its task's (O13) in
// a cut frame too — the task's own, else what the task inherits from a plan
// item the watcher may read — as the listing on their visible board says.
func TestACutFramesReviewCardCountsUnderItsTasksTeam(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Parked: true},
		{ItemID: "hidden", Title: "Secret", Team: "alpha", Domain: "closed"},
		{ItemID: "r", Title: "review: Receipts", ReviewOf: "t", Parked: true},
		{ItemID: "rh", Title: "review: Secret", ReviewOf: "hidden", Parked: true},
		// A task of alpha under an epic the watcher cannot read: its review
		// card's frame is cut by that crumb, and still counts under alpha.
		{ItemID: "closedEpic", Title: "Secret epic", Type: board.TypeEpic, Domain: "closed"},
		{ItemID: "tx", Title: "Alpha task", Team: "alpha", Parent: "closedEpic", Parked: true},
		{ItemID: "rx", Title: "review: Alpha task", ReviewOf: "tx", Parked: true},
	})
	e := seedEntry(store, "k", b)
	rights := rightsOn([]string{"shared"}, nil)
	narrow, cancel := store.subscribeAs("k", "bob", nil, map[string]bool{"cards": true}, rights)
	defer cancel()
	for _, id := range []string{"r", "rh", "rx"} {
		e.mu.Lock()
		c, _ := board.Find(e.board, id)
		c.Title += "!"
		e.upsertCard(c)
		e.cardChanged("", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}
	got := cardFrames(narrow)
	e.mu.Lock()
	visible := board.Visible(e.board, rights.primary, rights.readable)
	e.mu.Unlock()
	for _, id := range []string{"r", "rh", "rx"} {
		c, _ := board.Find(visible, id)
		want := apiserver.CardResource(visible, c).Status.EffectiveTeam
		if got[id].Status.EffectiveTeam != want {
			t.Errorf("%s: the frame says %q, the listing %q", id, got[id].Status.EffectiveTeam, want)
		}
	}
	if got["rx"].Status.EffectiveTeam != "alpha" {
		t.Errorf("rx counts under %q, want its task's alpha", got["rx"].Status.EffectiveTeam)
	}
	if got["r"].Status.EffectiveTeam != "portal" {
		t.Errorf("r counts under %q, want its task's portal", got["r"].Status.EffectiveTeam)
	}
}
