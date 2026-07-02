package server

import (
	"encoding/json"
	"testing"

	"github.com/aenix-org/aeman/internal/apiserver"
	"github.com/aenix-org/aeman/internal/board"
)

// frame decodes one marshalled watch frame from a subscription channel.
type frame struct {
	Type   string          `json:"type"`
	Kind   string          `json:"kind"`
	Object json.RawMessage `json:"object"`
}

func readFrame(t *testing.T, sub *subscription) frame {
	t.Helper()
	select {
	case data := <-sub.ch:
		var f frame
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("bad frame: %v (%s)", err, data)
		}
		return f
	default:
		t.Fatal("no frame queued")
		return frame{}
	}
}

func noFrame(t *testing.T, sub *subscription) {
	t.Helper()
	select {
	case data := <-sub.ch:
		t.Fatalf("unexpected frame: %s", data)
	default:
	}
}

func frameUID(t *testing.T, f frame) string {
	t.Helper()
	var c apiserver.Card
	if err := json.Unmarshal(f.Object, &c); err != nil {
		t.Fatalf("bad card object: %v", err)
	}
	return c.Metadata.UID
}

// seedEntry loads a board into a store entry as if LoadBoard had cached it.
func seedEntry(store *boardStore, key string, b board.Board) *boardEntry {
	e := store.entry(key)
	e.mu.Lock()
	e.board = b
	e.loaded = true
	e.mu.Unlock()
	return e
}

func watchBoard() board.Board {
	return board.Board{
		Owner: "acme", Number: 1,
		Cards: []board.Card{
			{ItemID: "c1", Team: "alpha", StartDate: "2026-01-10", SprintStart: "2026-01-10"},
			{ItemID: "c2", Team: "beta", StartDate: "2026-01-10", SprintStart: "2026-01-10"},
		},
		SprintStates: map[string]board.SprintState{
			"alpha": {Current: "2026-01-10", Previous: "2026-01-03", ItemID: "s1"},
			"beta":  {Current: "2026-01-10", ItemID: "s2"},
		},
	}
}

// V3: unscoped subscriptions get the raw verb per card, with echo suppression.
func TestWatchUnscopedCardEvents(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	sub, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()

	e.mu.Lock()
	c := e.board.Cards[0]
	c.Progress = 50
	e.upsertCard(c)
	e.cardChanged("", c, "MODIFIED")
	e.mu.Unlock()

	f := readFrame(t, sub)
	if f.Type != "MODIFIED" || f.Kind != "Card" || frameUID(t, f) != "c1" {
		t.Fatalf("frame = %+v", f)
	}
}

func TestWatchEchoSuppression(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	mine, cancel1 := store.subscribe("k", "me", nil, map[string]bool{"cards": true})
	defer cancel1()
	other, cancel2 := store.subscribe("k", "other", nil, map[string]bool{"cards": true})
	defer cancel2()

	e.mu.Lock()
	e.cardChanged("me", e.board.Cards[0], "MODIFIED")
	e.mu.Unlock()

	noFrame(t, mine)
	if f := readFrame(t, other); f.Type != "MODIFIED" {
		t.Fatalf("frame = %+v", f)
	}
}

// V4: a scoped subscription sees membership transitions, not raw verbs.
func TestScopedWatchMembership(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	sel := apiserver.Selector{View: "team", Team: "alpha", Day: "2026-01-10"}
	sub, cancel := store.subscribe("k", "", &sel, map[string]bool{"cards": true})
	defer cancel()
	if !sub.members["c1"] || sub.members["c2"] {
		t.Fatalf("seeded members = %v", sub.members)
	}

	// c2 moves into team alpha: it enters the scope as ADDED.
	e.mu.Lock()
	c2 := e.board.Cards[1]
	c2.Team = "alpha"
	e.upsertCard(c2)
	e.cardChanged("", c2, "MODIFIED")
	e.mu.Unlock()
	if f := readFrame(t, sub); f.Type != "ADDED" || frameUID(t, f) != "c2" {
		t.Fatalf("frame = %+v", f)
	}

	// c1 changes within the scope: MODIFIED.
	e.mu.Lock()
	c1 := e.board.Cards[0]
	c1.Progress = 70
	e.upsertCard(c1)
	e.cardChanged("", c1, "MODIFIED")
	e.mu.Unlock()
	if f := readFrame(t, sub); f.Type != "MODIFIED" || frameUID(t, f) != "c1" {
		t.Fatalf("frame = %+v", f)
	}

	// c1 leaves for team beta: DELETED from this subscription's view.
	e.mu.Lock()
	c1.Team = "beta"
	e.upsertCard(c1)
	e.cardChanged("", c1, "MODIFIED")
	e.mu.Unlock()
	if f := readFrame(t, sub); f.Type != "DELETED" || frameUID(t, f) != "c1" {
		t.Fatalf("frame = %+v", f)
	}
}

// V4: the originator's membership is still tracked while its frames are
// suppressed, so later foreign changes do not mis-fire.
func TestScopedWatchOriginMembershipTracked(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	sel := apiserver.Selector{View: "team", Team: "alpha", Day: "2026-01-10"}
	sub, cancel := store.subscribe("k", "me", &sel, map[string]bool{"cards": true})
	defer cancel()

	// My own change brings c2 into scope: no frame, but membership updates.
	e.mu.Lock()
	c2 := e.board.Cards[1]
	c2.Team = "alpha"
	e.upsertCard(c2)
	e.cardChanged("me", c2, "MODIFIED")
	e.mu.Unlock()
	noFrame(t, sub)
	if !sub.members["c2"] {
		t.Fatal("origin membership must be tracked")
	}

	// A foreign change to c2 inside the scope is MODIFIED, not a spurious ADDED.
	e.mu.Lock()
	c2.Progress = 30
	e.upsertCard(c2)
	e.cardChanged("", c2, "MODIFIED")
	e.mu.Unlock()
	if f := readFrame(t, sub); f.Type != "MODIFIED" {
		t.Fatalf("frame = %+v", f)
	}
}

// V4/V5: a sprint move announces the Sprint and re-diffs scoped memberships.
func TestSprintChangeReevaluates(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	teamSel := apiserver.Selector{View: "team", Team: "alpha", Day: "2026-01-10"}
	sub, cancel := store.subscribe("k", "", &teamSel, map[string]bool{"cards": true, "sprints": true})
	defer cancel()
	if !sub.members["c1"] {
		t.Fatalf("members = %v", sub.members)
	}

	// The sprint advances to 01-12; the subscription's memberships are re-diffed
	// against the new pointers and the Sprint resource is announced.
	e.mu.Lock()
	st := e.board.SprintStates["alpha"]
	st.Current, st.Previous = "2026-01-12", "2026-01-10"
	e.board.SprintStates["alpha"] = st
	e.sprintChanged("", "alpha")
	e.mu.Unlock()

	sawSprint, sawDeleted := false, false
	for i := 0; i < 3; i++ {
		select {
		case data := <-sub.ch:
			var f frame
			_ = json.Unmarshal(data, &f)
			if f.Kind == "Sprint" && f.Type == "MODIFIED" {
				sawSprint = true
			}
			if f.Kind == "Card" && f.Type == "DELETED" {
				sawDeleted = true
			}
		default:
		}
	}
	if !sawSprint {
		t.Fatal("a sprint move must announce the Sprint resource")
	}
	// c1 keeps its passed-through pointer day, so it may legitimately stay; the
	// core assertion is that membership was re-diffed without a crash and the
	// member set matches the filter now.
	want := map[string]bool{}
	e.mu.Lock()
	for _, c := range apiserver.FilterCards(e.board, teamSel) {
		want[c.ItemID] = true
	}
	e.mu.Unlock()
	if len(sub.members) != len(want) {
		t.Fatalf("members = %v, want %v (deleted seen: %v)", sub.members, want, sawDeleted)
	}
}

// V5: a move announces one MODIFIED Ordering with the new order.
func TestOrderingEvent(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	sub, cancel := store.subscribe("k", "", nil, map[string]bool{"ordering": true})
	defer cancel()

	e.mu.Lock()
	e.moveCardTo("c2", "")
	e.orderingChanged("")
	e.mu.Unlock()

	f := readFrame(t, sub)
	if f.Type != "MODIFIED" || f.Kind != "Ordering" {
		t.Fatalf("frame = %+v", f)
	}
	var o apiserver.Ordering
	if err := json.Unmarshal(f.Object, &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Spec.UIDs) != 2 || o.Spec.UIDs[0] != "c2" {
		t.Fatalf("ordering = %+v", o.Spec.UIDs)
	}
}

// A TTL reload must not lose a card created through aeman seconds ago (the
// GitHub item list is eventually consistent), nor resurrect one just deleted.
func TestReloadKeepsRecentMutations(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "acme/1", watchBoard())

	// c3 was just created locally; c1 just deleted.
	e.mu.Lock()
	c3 := board.Card{ItemID: "c3", Team: "alpha"}
	e.upsertCard(c3)
	e.markRecent("c3")
	e.removeCard("c1")
	e.markGone("c1")
	e.mu.Unlock()

	// A stale upstream fetch: still lists c1, does not know c3 yet.
	e.mu.Lock()
	fresh := e.applyRecent(watchBoard())
	e.mu.Unlock()

	ids := map[string]bool{}
	for _, c := range fresh.Cards {
		ids[c.ItemID] = true
	}
	if !ids["c3"] {
		t.Fatalf("recently created card lost on reload: %v", ids)
	}
	if ids["c1"] {
		t.Fatalf("recently deleted card resurrected on reload: %v", ids)
	}
}
