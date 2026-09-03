package server

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A drain that GIVES UP says so.
//
// waitDrained returned the same nothing whether it had emptied the queue or
// run out of time, so every caller read "the queue is empty" from a deadline
// that had merely expired. The shutdown path then pushed whatever had become
// commits and exited zero — no error, no log line — while anything still in
// the queue existed in memory only. A card the person had been told was saved
// was gone from the remote and from the clone alike, and nothing said so.
// /healthz could not help: it counts commits in the clone, so a write that
// never became one is invisible to it.
//
// The queue wedges when a network fetch holds the apply lock — a laptop that
// slept, a VPN that dropped, a half-open socket to the forge. This pins the
// state that follows: not "the queue is slow" but "the queue did not empty
// and we are leaving anyway".
func TestADrainThatGivesUpSaysSo(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	e.mu.Lock()
	// One write nothing will ever carry out: the shape a wedged queue has.
	e.pending = append(e.pending, pendingOp{})
	e.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if left := store.waitDrained(ctx); left != 1 {
		t.Fatalf("waitDrained = %d after giving up on its deadline, want the 1 write still unsynced", left)
	}
}

// A drain that finishes reports nothing left — which is what lets a caller
// tell the two outcomes apart at all.
func TestADrainThatFinishesReportsNothingLeft(t *testing.T) {
	store := newBoardStore()
	seedEntry(store, "k", watchBoard())

	if left := store.waitDrained(context.Background()); left != 0 {
		t.Fatalf("waitDrained = %d on an empty queue, want 0", left)
	}
}

// And the shutdown turns that answer into something a person can read.
// Losing work silently is the failure; losing it loudly is a bug report.
func TestShutdownSaysWhatItCouldNotSave(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	e.mu.Lock()
	e.pending = append(e.pending, pendingOp{}, pendingOp{})
	e.mu.Unlock()

	var buf bytes.Buffer
	srv := &Server{store: store, log: slog.New(slog.NewTextHandler(&buf, nil))}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = srv.drainAndPush(ctx)

	got := buf.String()
	if !strings.Contains(got, "unsaved=2") {
		t.Fatalf("the shutdown logged %q; it must name how many writes it could not save", got)
	}
}
