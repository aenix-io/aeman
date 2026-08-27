package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// wbBackend is an inner backend for write-behind tests: LoadBoard serves a
// configurable board, SetProgress can block (gate) or fail, and every upstream
// call is counted.
type wbBackend struct {
	boardservice.Backend // nil: only the methods below are exercised
	mu                   sync.Mutex
	board                board.Board
	loads                int
	progressCalls        int
	gate                 chan struct{} // nil = no gating
	fail                 bool
	sprintWrites         []string
}

func (w *wbBackend) LoadBoard(_ context.Context, _ string, _ int) (board.Board, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.loads++
	// A fresh Cards slice per load, like a real backend: the store mutates its
	// cached copy in place and must not reach the fixture through shared
	// backing arrays.
	b := w.board
	b.Cards = append([]board.Card(nil), w.board.Cards...)
	return b, nil
}

func (w *wbBackend) LoadCards(_ context.Context, _ board.Board, ids []string) ([]board.Card, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []board.Card
	for _, c := range w.board.Cards {
		for _, id := range ids {
			if c.ItemID == id {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (w *wbBackend) SetProgress(_ context.Context, _ board.Board, _ board.Card, _ int) error {
	if w.gate != nil {
		<-w.gate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.progressCalls++
	if w.fail {
		return errors.New("github: boom")
	}
	return nil
}

func (w *wbBackend) counts() (loads, progress int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.loads, w.progressCalls
}

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A field mutation returns instantly with the cache (and watchers) updated,
// while the slow GitHub write drains in the background; the unsynced counter
// rises and falls back to zero.
func TestWriteBehindInstant(t *testing.T) {
	inner := &wbBackend{board: watchBoard(), gate: make(chan struct{})}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	bd, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	sub, cancel := store.subscribe("acme/1", "", nil, map[string]bool{"cards": true})
	defer cancel()

	start := time.Now()
	if err := be.SetProgress(context.Background(), bd, bd.Cards[0], 80); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("SetProgress blocked on the upstream write: %v", elapsed)
	}
	// The cache already holds the change and the watcher heard about it.
	e := store.entry("acme/1")
	e.mu.Lock()
	got := e.board.Cards[0].Progress
	pending := e.unsynced()
	e.mu.Unlock()
	if got != 80 || pending != 1 {
		t.Fatalf("cache progress = %d, unsynced = %d; want 80, 1", got, pending)
	}
	f := readFrame(t, sub)
	if f.Kind != "Card" || f.Type != "MODIFIED" {
		t.Fatalf("want a MODIFIED Card frame, got %s %s", f.Type, f.Kind)
	}
	if f = readFrame(t, sub); f.Kind != "Queue" {
		t.Fatalf("want a Queue frame, got %s %s", f.Type, f.Kind)
	}

	// Release the gated write: the queue drains to zero.
	close(inner.gate)
	waitFor(t, "queue drain", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.unsynced() == 0
	})
	if _, progress := inner.counts(); progress != 1 {
		t.Fatalf("upstream SetProgress calls = %d, want 1", progress)
	}
}

// Same-key queued writes coalesce, DeltaFIFO-style: dragging a slider N times
// while an earlier write is on the wire costs one more GitHub call carrying
// the final value, not N.
func TestWriteBehindCoalesces(t *testing.T) {
	inner := &wbBackend{board: watchBoard(), gate: make(chan struct{})}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	bd, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")

	// First write goes on the wire (gated); the rest coalesce in the queue.
	if err := be.SetProgress(context.Background(), bd, bd.Cards[0], 40); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first write in flight", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.inflight != nil
	})
	for _, p := range []int{60, 80, 95} {
		if err := be.SetProgress(context.Background(), bd, bd.Cards[0], p); err != nil {
			t.Fatal(err)
		}
	}
	e.mu.Lock()
	queued := len(e.pending)
	e.mu.Unlock()
	if queued != 1 {
		t.Fatalf("queued ops = %d, want 1 (coalesced)", queued)
	}

	close(inner.gate)
	waitFor(t, "queue drain", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.unsynced() == 0
	})
	if _, progress := inner.counts(); progress != 2 {
		t.Fatalf("upstream SetProgress calls = %d, want 2 (in-flight + merged)", progress)
	}
	e.mu.Lock()
	got := e.board.Cards[0].Progress
	e.mu.Unlock()
	if got != 95 {
		t.Fatalf("final cached progress = %d, want 95", got)
	}
}

// A write that fails rolls the board back: every watcher gets a SyncError and
// the board reloads from the backend (the authority).
func TestWriteBehindFailureRollsBack(t *testing.T) {
	inner := &wbBackend{board: watchBoard(), fail: true}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	bd, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	sub, cancel := store.subscribe("acme/1", "", nil, map[string]bool{"cards": true})
	defer cancel()

	if err := be.SetProgress(context.Background(), bd, bd.Cards[0], 80); err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")
	waitFor(t, "rollback reload", func() bool {
		loads, _ := inner.counts()
		return loads >= 2
	})
	waitFor(t, "cache rollback", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return len(e.pending) == 0 && e.board.Cards[0].Progress == 0
	})

	// Somewhere in the stream there must be the SyncError; drain the channel.
	sawError := false
	for i := 0; i < 10; i++ {
		select {
		case data := <-sub.ch:
			var f frame
			if json.Unmarshal(data, &f) == nil && f.Kind == "SyncError" {
				sawError = true
			}
		default:
		}
	}
	if !sawError {
		t.Fatal("no SyncError frame reached the watcher")
	}
}

// The single-card refresh after a note write must replay the queue too: with
// several rapid note adds in flight, the upstream copy read back after the
// first write predates the rest — without the replay they vanish from the
// cache (and every open tab) until the queue drains.
func TestTouchedPreservesQueuedNotes(t *testing.T) {
	inner := &wbBackend{board: watchBoard()}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	bd, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")

	// Two note adds still queued (their writes have not reached upstream).
	for _, text := range []string{"w", "e"} {
		note := board.Note{ID: "c1:wb:" + text, Body: text, Source: "draft"}
		e.mu.Lock()
		e.pending = append(e.pending, pendingOp{
			desc: "add a note",
			apply: func(target *board.Board) {
				for i := range target.Cards {
					if target.Cards[i].ItemID != "c1" {
						continue
					}
					for _, n := range target.Cards[i].Notes {
						if n.ID == note.ID {
							return
						}
					}
					target.Cards[i].Notes = append(target.Cards[i].Notes, note)
				}
			},
			exec: func(context.Context) error { return nil },
		})
		e.mu.Unlock()
	}
	e.mu.Lock()
	for _, op := range e.pending {
		op.apply(&e.board)
	}
	e.mu.Unlock()

	// Upstream has only the first, already-written note ("q"): the refresh
	// after its write must not erase the queued w/e from the cache.
	upstream := watchBoard()
	upstream.Cards[0].Notes = []board.Note{{ID: "c1:0", Body: "q", Source: "draft"}}
	inner.mu.Lock()
	inner.board = upstream
	inner.mu.Unlock()
	be.touched(context.Background(), bd, "c1")

	e.mu.Lock()
	var bodies []string
	for _, c := range e.board.Cards {
		if c.ItemID == "c1" {
			for _, n := range c.Notes {
				bodies = append(bodies, n.Body)
			}
		}
	}
	e.mu.Unlock()
	if len(bodies) != 3 || bodies[0] != "q" || bodies[1] != "w" || bodies[2] != "e" {
		t.Fatalf("cached notes after refresh = %v, want [q w e]", bodies)
	}
}

// A full reload replays the still-pending queue on top of the fresh board, so
// unconfirmed changes never roll back mid-flight.
func TestReloadReplaysPending(t *testing.T) {
	inner := &wbBackend{board: watchBoard()}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	if _, err := be.LoadBoard(context.Background(), "acme", 1); err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")
	e.mu.Lock()
	e.pending = append(e.pending, pendingOp{
		desc: "test",
		apply: func(target *board.Board) {
			for i := range target.Cards {
				if target.Cards[i].ItemID == "c1" {
					target.Cards[i].Progress = 55
				}
			}
		},
		exec: func(context.Context) error { return nil },
	})
	e.mu.Unlock()

	got := be.install(e, watchBoard())
	for _, c := range got.Cards {
		if c.ItemID == "c1" && c.Progress != 55 {
			t.Fatalf("pending change lost on reload: %+v", c)
		}
	}
}

// wbBodyBackend extends wbBackend with the one-shot draft body writer, so the
// store's DeltaFIFO body merge kicks in.
type wbBodyBackend struct {
	wbBackend
	bodyGate  chan struct{}
	syncCalls int
	lastDesc  string
	lastNotes []board.Note
	lastEvs   []board.Event
}

func (w *wbBodyBackend) SyncDraftBody(_ context.Context, _ board.Card, description string, notes []board.Note, events []board.Event) ([]board.Note, []board.Event, error) {
	if w.bodyGate != nil {
		<-w.bodyGate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.syncCalls++
	w.lastDesc = description
	w.lastNotes = append([]board.Note(nil), notes...)
	w.lastEvs = append([]board.Event(nil), events...)
	return notes, events, nil
}

// Rapid body-affecting changes on one draft card — notes, an event line, a
// description edit — merge into coalesced body writes carrying the final
// state, instead of one racing read-modify-write per change.
func TestWriteBehindMergesDraftBodyOps(t *testing.T) {
	fixture := watchBoard()
	fixture.Cards[0].IsDraft = true
	fixture.Cards[0].ContentID = "D_1"
	inner := &wbBodyBackend{
		wbBackend: wbBackend{board: fixture},
		bodyGate:  make(chan struct{}),
	}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	bd, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")
	card := bd.Cards[0]

	// First write goes on the wire (gated); everything after coalesces into
	// ONE queued body op.
	if err := be.AddNote(context.Background(), bd, card, "first"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first body write in flight", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.inflight != nil
	})
	for _, text := range []string{"second", "third"} {
		if err := be.AddNote(context.Background(), bd, card, text); err != nil {
			t.Fatal(err)
		}
	}
	if err := be.AppendEvent(context.Background(), bd, card, board.Event{
		Kind: board.EventParent, To: "big card", At: "2026-01-10T10:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := be.SetDescription(context.Background(), bd, card, "the plan"); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	queued := len(e.pending)
	e.mu.Unlock()
	if queued != 1 {
		t.Fatalf("queued ops = %d, want 1 (merged body op)", queued)
	}

	close(inner.bodyGate)
	waitFor(t, "queue drain", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.unsynced() == 0
	})
	inner.mu.Lock()
	calls, desc := inner.syncCalls, inner.lastDesc
	notes := len(inner.lastNotes)
	evs := len(inner.lastEvs)
	inner.mu.Unlock()
	if calls != 2 {
		t.Fatalf("SyncDraftBody calls = %d, want 2 (in-flight + merged)", calls)
	}
	if desc != "the plan" || notes != 3 || evs != 1 {
		t.Fatalf("final body write: desc=%q notes=%d events=%d; want the plan/3/1", desc, notes, evs)
	}
}

func (w *wbBackend) MoveCard(_ context.Context, _ board.Board, _ board.Card, _ string) error {
	return nil
}

// A background revalidation that reads GitHub's lagging replicas must not
// roll back writes the queue already confirmed: within the grace window the
// cached field values and card order outweigh the stale fresh read.
func TestRevalidateKeepsRecentWrites(t *testing.T) {
	inner := &wbBackend{board: watchBoard()}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	bd, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")

	// Confirmed writes: progress on c1, and c2 moved to the front.
	if err := be.SetProgress(context.Background(), bd, bd.Cards[0], 80); err != nil {
		t.Fatal(err)
	}
	if err := be.MoveCard(context.Background(), bd, bd.Cards[1], ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "queue drain", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.unsynced() == 0
	})

	// The fixture backend never mutated its stored board, so its next read
	// IS the stale replica: progress 0, original order. Install it exactly
	// like a background revalidation would.
	stale, err := inner.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	got := be.install(e, stale)
	if got.Cards[0].ItemID != "c2" || got.Cards[1].ItemID != "c1" {
		t.Fatalf("order rolled back: got %s, %s", got.Cards[0].ItemID, got.Cards[1].ItemID)
	}
	if p := got.Cards[1].Progress; p != 80 {
		t.Fatalf("progress rolled back: got %d, want 80", p)
	}
}

// A revalidation whose fresh read predates a just-created card must restore
// it at its cached slot, not append it to the bottom of the board — the
// append re-ordered everything below it on every reload right after a create.
func TestRevalidateRestoresCreatedCardInPlace(t *testing.T) {
	inner := &wbBackend{board: watchBoard()}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	if _, err := be.LoadBoard(context.Background(), "acme", 1); err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")

	// A card born between c1 and c2 (as CreateCard + the slotting move leave
	// it), known to the recency guard.
	e.mu.Lock()
	cards := e.board.Cards
	inserted := make([]board.Card, 0, len(cards)+1)
	inserted = append(inserted, cards[0], board.Card{ItemID: "cNew", Team: "alpha"})
	inserted = append(inserted, cards[1:]...)
	e.board.Cards = inserted
	e.markRecent("cNew")
	e.mu.Unlock()

	// The lagging replica still serves the board without the new card.
	stale, err := inner.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	got := be.install(e, stale)
	ids := make([]string, len(got.Cards))
	for i, c := range got.Cards {
		ids[i] = c.ItemID
	}
	if len(ids) != 3 || ids[0] != "c1" || ids[1] != "cNew" || ids[2] != "c2" {
		t.Fatalf("restored order = %v, want [c1 cNew c2]", ids)
	}
}

// The single-card refresh after a note write must not resurrect a card
// deleted while the op drained: GitHub's lagging replicas still return it,
// and the old unconditional upsert also stripped its recentGone protection,
// so the ghost survived every reload for the whole grace window.
func TestTouchedSkipsDeletedCard(t *testing.T) {
	inner := &wbBackend{board: watchBoard()}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	bd, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")

	// The user deleted c1; the fixture backend (the "stale replica") still
	// serves it to LoadCards.
	e.mu.Lock()
	e.removeCard("c1")
	e.markGone("c1")
	e.mu.Unlock()

	be.touched(context.Background(), bd, "c1")

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.board.Cards {
		if c.ItemID == "c1" {
			t.Fatal("touched resurrected the deleted card")
		}
	}
	if _, gone := e.recentGone["c1"]; !gone {
		t.Fatal("touched stripped the card's recentGone protection")
	}
}

func (w *wbBackend) SetSprintState(_ context.Context, b board.Board, team, current, previous string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sprintWrites = append(w.sprintWrites, team+"@"+b.SprintStates[team].ItemID+" "+current+"/"+previous)
	return nil
}

// A queued sprint-pointer write must resolve the sprint-state card at WRITE
// time from the live cache — the enqueue-era snapshot may point at a card
// that a fresher load replaced (the duplicate-scatter incident).
func TestSprintStateWriteResolvesLive(t *testing.T) {
	inner := &wbBackend{board: watchBoard(), gate: make(chan struct{})}
	store := newBoardStore()
	be := &storeBackend{inner: inner, store: store}
	bd, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")

	// Hold the queue on a gated op so the sprint write stays queued while the
	// cache moves under it.
	if err := be.SetProgress(context.Background(), bd, bd.Cards[0], 40); err != nil {
		t.Fatal(err)
	}
	// Enqueue the pointer roll with the CURRENT snapshot (sprint-state s1)...
	if err := be.SetSprintState(context.Background(), bd, "alpha", "2026-01-11", "2026-01-10"); err != nil {
		t.Fatal(err)
	}
	// ...then the cache learns the team's card is really s1-NEW (a fresher
	// load replaced the duplicate) before the queue drains.
	e.mu.Lock()
	st := e.board.SprintStates["alpha"]
	st.ItemID = "s1-new"
	e.board.SprintStates["alpha"] = st
	e.mu.Unlock()

	close(inner.gate)
	waitFor(t, "queue drain", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.unsynced() == 0
	})
	inner.mu.Lock()
	writes := append([]string(nil), inner.sprintWrites...)
	inner.mu.Unlock()
	if len(writes) != 1 || writes[0] != "alpha@s1-new 2026-01-11/2026-01-10" {
		t.Fatalf("sprint write = %v, want the LIVE card s1-new", writes)
	}
}
