package server

import (
	"context"
	"fmt"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// The write-behind queue: a mutation is applied to the shared board cache
// immediately (the response and every watcher see it at once) and the actual
// write is queued and committed in the background. Only a write that fails
// rolls the board back — by reloading it from the branch tip (the authority)
// and telling every client what happened.
//
// pendingOp is one queued write. The change is already live in the cache;
// exec pushes it upstream, and apply re-imposes it onto a freshly loaded
// board — a full reload must not undo changes the tree has not seen yet.
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
	// re-imposing the value the backend just refused).
	itemID string
	// kind is the coalescing kind ("progress", "stage", …), kept apart from
	// key so the git commit can be named after it.
	kind string
	// action is the request the op belongs to (git backend): consecutive ops
	// of one action drain under one scope and become one commit.
	action actionRef
	// actor is the login behind the op — part of the coalescing key, so two
	// people on one slider are two commits, and the commit's author.
	actor string
	// notBefore delays a coalescable op so the next value of the same
	// gesture can replace it before it commits (coalesceWindow).
	notBefore time.Time
}

// enqueue applies the op to the cache (the caller already did that part),
// appends it to the board's queue and makes sure a drain worker is running.
// A queued (not yet executing) op with the same key is replaced in place —
// its slot keeps the FIFO position, its payload becomes the newest value.
// The context is detached so the upstream write survives the request.
func (b *storeBackend) enqueue(ctx context.Context, e *boardEntry, op pendingOp) {
	bctx := context.WithoutCancel(ctx)
	op.action = actionFrom(ctx)
	op.actor = board.ActorFrom(ctx)
	if op.key != "" {
		// Two people on one slider are two writes, both attributed — never
		// one silently overwriting the other.
		op.key += "@" + op.actor
	}
	if b.git != nil && op.kind == "progress" {
		// A drag is many writes for one intent: hold the commit open for
		// the next value. Order per card is kept — the queue is FIFO — so an
		// action that follows on the same card still commits after it.
		op.notBefore = time.Now().Add(coalesceWindow)
	}
	e.mu.Lock()
	merged := false
	if op.key != "" {
		// Coalesce only past the last op of ANOTHER kind on this card: once
		// an action on the card sits behind the earlier value, that value
		// must commit first — slider→100 then send-to-review commits the
		// 100, then the review's clamp, never the review over a stale 100.
		// Another actor's write of the same kind does not block: two people
		// on one slider are two coalesced writes, one each.
		barrier := -1
		for i := range e.pending {
			if op.itemID != "" && e.pending[i].itemID == op.itemID && e.pending[i].kind != op.kind {
				barrier = i
			}
		}
		for i := range e.pending {
			if e.pending[i].key == op.key && i > barrier {
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
// changes to one card in order). A write that fails is dropped: every client
// is told, and the board is reloaded so the cache — with the remaining queue
// replayed on top — matches reality again.
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
		if wait := time.Until(op.notBefore); wait > 0 {
			// A coalescable write is still collecting values; every op
			// behind it waits too, which is what keeps the card's order.
			e.mu.Unlock()
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return
			}
			continue
		}
		e.pending = e.pending[1:]
		e.inflight = &op
		e.mu.Unlock()

		err := b.execGroup(ctx, e, op)

		e.mu.Lock()
		e.inflight = nil
		e.queueChanged()
		boardID := e.board.Board
		if err != nil {
			e.syncError(fmt.Sprintf("%s failed: %v", op.desc, err))
			e.loaded = false
			// The failed write's card must NOT keep outweighing the reload:
			// the backend refused the value, so its truth wins for it.
			if op.itemID != "" {
				delete(e.recentCards, op.itemID)
			}
			e.recentMove = time.Time{}
		}
		e.mu.Unlock()
		if err != nil {
			// Reload now (not on the next read) so every open board rolls
			// back to the backend's reality right away; the remaining queue
			// is replayed on top by install.
			_, _ = b.LoadBoard(ctx, boardID)
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
	e := b.store.entry(storeKey(bd.Board))
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
	b.enqueue(ctx, e, pendingOp{key: key, kind: kind, desc: desc, compose: compose, apply: apply, exec: exec, itemID: itemID})
}

// waitDrained blocks until every board's write-behind queue is empty or the
// context expires — the shutdown path calls it so a restart does not drop
// changes users already saw applied. It returns how many writes are STILL
// unsynced: 0 when the queue emptied, and the count that remains when the
// deadline came first.
//
// It used to return nothing, and that was the whole of a data-loss bug. The
// two outcomes are not alike — one means every change is now a commit, the
// other means some change exists only in this process's memory and is about
// to be dropped — and a caller that cannot tell them apart reports success
// for both. The queue wedges when a network fetch holds the apply lock (a
// laptop that slept, a half-open socket to the forge), so this is not a
// theoretical branch: it is what a stop during a network stall does.
func (s *boardStore) waitDrained(ctx context.Context) int {
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
			return 0
		}
		select {
		case <-ctx.Done():
			return pending
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
