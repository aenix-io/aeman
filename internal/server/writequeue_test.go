package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aenix-org/aeman/pkg/board"
	"github.com/aenix-org/aeman/pkg/boardservice"
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

// A write that still fails after retries rolls the board back: every watcher
// gets a SyncError and the board reloads from GitHub (the authority).
func TestWriteBehindFailureRollsBack(t *testing.T) {
	old := queueBackoff
	queueBackoff = nil // fail fast: one attempt
	defer func() { queueBackoff = old }()

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

	got := be.install(e, watchBoard(), "")
	for _, c := range got.Cards {
		if c.ItemID == "c1" && c.Progress != 55 {
			t.Fatalf("pending change lost on reload: %+v", c)
		}
	}
}
