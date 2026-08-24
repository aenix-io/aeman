package server

import (
	"context"
	"fmt"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/ghprojects"
)

// The write-behind queue: a mutation is applied to the shared board cache
// immediately (the response and every watcher see it at once) and the actual
// GitHub Projects write is queued and pushed in the background. Only a write
// that still fails after retries rolls the board back — by reloading it from
// GitHub (the authority) and telling every client what happened.
//
// pendingOp is one queued write. The change is already live in the cache;
// exec pushes it upstream, and apply re-imposes it onto a freshly loaded
// board — a full reload must not undo changes GitHub has not seen yet.
type pendingOp struct {
	// key coalesces queued writes, DeltaFIFO-style: a new op replaces a
	// not-yet-executing queued op with the same key in place, so dragging a
	// slider five times costs one GitHub write carrying the final value.
	// "" never coalesces.
	key string
	// compose chains the replaced op's apply before this op's on coalesce:
	// body ops are DELTAS (append a note, log an event, set the description),
	// so a reload replay must re-impose all of them, not just the newest —
	// while their shared exec writes the final cached state once either way.
	compose bool
	// desc names the operation for the sync-error message ("set progress on
	// «title»").
	desc  string
	apply func(bd *board.Board)
	exec  func(ctx context.Context) error
	// itemID names the card the op writes, so a FAILED write can drop that
	// card's recent-guard before the rollback reload (or the guard would keep
	// re-imposing the value GitHub just refused).
	itemID string
	// requeues counts fresh-node-lag retries (unresolved node, see drain).
	requeues int
}

// queueBackoff is the wait between upstream retries (variable for tests).
var queueBackoff = []time.Duration{time.Second, 3 * time.Second}

// enqueue applies the op to the cache (the caller already did that part),
// appends it to the board's queue and makes sure a drain worker is running.
// A queued (not yet executing) op with the same key is replaced in place —
// its slot keeps the FIFO position, its payload becomes the newest value.
// The context is detached so the upstream write survives the request.
func (b *storeBackend) enqueue(ctx context.Context, e *boardEntry, op pendingOp) {
	bctx := context.WithoutCancel(ctx)
	e.mu.Lock()
	merged := false
	if op.key != "" {
		for i := range e.pending {
			if e.pending[i].key == op.key {
				if op.compose {
					prev, next := e.pending[i].apply, op.apply
					op.apply = func(bd *board.Board) {
						prev(bd)
						next(bd)
					}
				}
				e.pending[i] = op
				merged = true
				break
			}
		}
	}
	if !merged {
		e.pending = append(e.pending, op)
		e.queueChanged()
	}
	starting := !e.draining
	e.draining = true
	e.mu.Unlock()
	if starting {
		go b.drain(bctx, e)
	}
}

// drain pushes queued writes upstream one at a time (FIFO keeps dependent
// changes to one card in order, and the modest pace stays clear of GitHub's
// secondary rate limits). A write that fails after retries is dropped: every
// client is told, and the board is reloaded from GitHub so the cache — with
// the remaining queue replayed on top — matches reality again.
func (b *storeBackend) drain(ctx context.Context, e *boardEntry) {
	for {
		e.mu.Lock()
		if len(e.pending) == 0 {
			e.draining = false
			e.mu.Unlock()
			return
		}
		// Pop before executing (so a same-key enqueue cannot coalesce into an
		// op already on the wire) and keep it visible as in-flight — the
		// counter and reload replays must still cover it.
		op := e.pending[0]
		e.pending = e.pending[1:]
		e.inflight = &op
		e.mu.Unlock()

		err := execRetry(ctx, op)
		// An unresolvable node id is either a target deleted mid-queue (drop
		// the write: the cache already matches the user's intent) or a FRESH
		// node GitHub's read path has not caught up with yet — requeue that
		// with a longer pause instead of silently losing the write, or the
		// order/fields roll back on the next full reload.
		if ghprojects.IsUnresolvedNode(err) {
			e.mu.Lock()
			_, gone := e.recentGone[op.itemID]
			if !gone && op.requeues < 3 {
				op.requeues++
				e.pending = append(e.pending, op)
			}
			e.mu.Unlock()
			err = nil
		}

		e.mu.Lock()
		e.inflight = nil
		e.queueChanged()
		owner, number := e.board.Owner, e.board.Number
		if err != nil {
			e.syncError(fmt.Sprintf("%s failed: %v", op.desc, err))
			e.loaded = false
			// The failed write's card must NOT keep outweighing the reload:
			// GitHub refused the value, so GitHub's truth wins for it.
			if op.itemID != "" {
				delete(e.recentCards, op.itemID)
			}
			e.recentMove = time.Time{}
		}
		e.mu.Unlock()
		if err != nil {
			// Reload now (not on the next read) so every open board rolls
			// back to GitHub's reality right away; the remaining queue is
			// replayed on top by install.
			_, _ = b.LoadBoard(ctx, owner, number)
		}
	}
}

// execRetry runs the upstream write with a few retries — a transient GitHub
// hiccup (secondary rate limit, 502) must not roll a user's change back.
func execRetry(ctx context.Context, op pendingOp) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = op.exec(ctx); err == nil {
			return nil
		}
		if attempt >= len(queueBackoff) {
			return err
		}
		select {
		case <-time.After(queueBackoff[attempt]):
		case <-ctx.Done():
			return err
		}
	}
}

// unsynced counts the writes GitHub has not confirmed yet, the in-flight one
// included. The caller holds e.mu.
func (e *boardEntry) unsynced() int {
	n := len(e.pending)
	if e.inflight != nil {
		n++
	}
	return n
}

// queueChanged tells every watcher how many writes are still unsynced; the
// UI shows the number and hides it at zero. The caller holds e.mu.
func (e *boardEntry) queueChanged() {
	frame := watchFrame{Type: "MODIFIED", Kind: "Queue", Object: map[string]int{
		"pending": e.unsynced(),
	}}
	for sub := range e.watchers {
		sub.send(frame)
	}
}

// syncError announces a write that was lost after retries. No echo
// suppression: the originator needs the rollback most of all. The caller
// holds e.mu.
func (e *boardEntry) syncError(message string) {
	frame := watchFrame{Type: "ERROR", Kind: "SyncError", Object: map[string]string{
		"message": message,
	}}
	for sub := range e.watchers {
		sub.send(frame)
	}
}

// mutateCard applies fn to the cached card, announces the change and queues
// the upstream write: the instant half of a write-behind field mutation.
// kind builds the coalescing key (kind per card; "" never coalesces),
// desc/exec describe and perform the GitHub write; origin echo suppression
// matches the direct-mutation path.
func (b *storeBackend) mutateCard(ctx context.Context, bd board.Board, itemID, kind, desc string, fn func(c *board.Card), exec func(ctx context.Context) error) {
	b.mutateCardOp(ctx, bd, itemID, kind, desc, false, fn, exec)
}

// mutateCardOp is mutateCard with an explicit compose choice (see pendingOp).
func (b *storeBackend) mutateCardOp(ctx context.Context, bd board.Board, itemID, kind, desc string, compose bool, fn func(c *board.Card), exec func(ctx context.Context) error) {
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	apply := func(target *board.Board) {
		for i := range target.Cards {
			if target.Cards[i].ItemID == itemID {
				fn(&target.Cards[i])
				return
			}
		}
		// A process task is a whole card kept out of the rows; every
		// card setter (description, cycle, start, team, owner) applies to it
		// the same way, so it is reached here rather than in each setter.
		for i := range target.Tasks {
			if target.Tasks[i].ItemID == itemID {
				fn(&target.Tasks[i])
				return
			}
		}
	}
	e.mu.Lock()
	apply(&e.board)
	announced := false
	for i := range e.board.Cards {
		if e.board.Cards[i].ItemID == itemID {
			e.markRecent(itemID)
			e.cardChanged(echoOrigin(ctx, itemID), e.board.Cards[i], "MODIFIED")
			announced = true
			break
		}
	}
	if !announced {
		for _, t := range e.board.Tasks {
			if t.ItemID == itemID {
				// A task changed: clients re-read the board's structure.
				e.rosterBroadcast()
				break
			}
		}
	}
	e.mu.Unlock()
	key := ""
	if kind != "" {
		key = kind + ":" + itemID
	}
	b.enqueue(ctx, e, pendingOp{key: key, desc: desc, compose: compose, apply: apply, exec: exec, itemID: itemID})
}

// waitDrained blocks until every board's write-behind queue is empty or the
// context expires — the shutdown path calls it so a restart does not drop
// changes users already saw applied.
func (s *boardStore) waitDrained(ctx context.Context) {
	for {
		s.mu.Lock()
		pending := 0
		for _, e := range s.entries {
			e.mu.Lock()
			pending += e.unsynced()
			e.mu.Unlock()
		}
		s.mu.Unlock()
		if pending == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// cardRef names a card for sync-error messages.
func cardRef(c board.Card) string {
	if c.Title == "" {
		return c.ItemID
	}
	return "«" + c.Title + "»"
}
