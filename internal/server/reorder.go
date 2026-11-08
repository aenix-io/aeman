package server

import (
	"context"
	"fmt"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// ORDERING ACTIONS ON THE CACHE (docs/design/ordering.md, "Atomicity"). The
// plan is computed over the cached board, applied to it and announced in ONE
// Ordering frame, all under the entry's lock — so no reader, no watch frame
// and no other request's plan ever sees half of a mass move — and its write
// is ONE queued op of the request's action: every file of the plan staged
// under one git scope, one commit per repository touched. A write that fails
// commits nothing (the scope is never flushed) and the queue's failure path
// reloads the cache from the branch tip.

// Reorder plans over the cache, applies and announces under one lock, and
// queues the write of exactly those keys.
func (b *storeBackend) Reorder(ctx context.Context, bd board.Board, plan board.Planner) ([]board.RankChange, error) {
	e := b.store.entry(storeKey(bd.Board))
	e.mu.Lock()
	cur := e.board
	cur.Cards = append([]board.Card(nil), e.board.Cards...)
	board.SortCards(cur.Cards)
	changes, err := plan(cur)
	if err != nil || len(changes) == 0 {
		e.mu.Unlock()
		return nil, err
	}
	e.board = board.ApplyRankChanges(cur, changes)
	e.recentMove = time.Now()
	e.orderingChanged(clientIDFrom(ctx))
	desc := fmt.Sprintf("reorder %d cards", len(changes))
	itemID := ""
	if len(changes) == 1 {
		desc, itemID = "move "+changes[0].ID, changes[0].ID
	}
	written := append([]board.RankChange(nil), changes...)
	// Queued under the same lock as the plan: keys are absolute, so two
	// plans must reach git in the order the cache took them.
	start := b.enqueueLocked(ctx, e, pendingOp{
		itemID: itemID,
		kind:   "move",
		desc:   desc,
		apply: func(target *board.Board) {
			*target = board.ApplyRankChanges(*target, written)
		},
		exec: func(ctx context.Context) error {
			_, err := b.inner.Reorder(ctx, bd, board.Fixed(written))
			return err
		},
	})
	e.mu.Unlock()
	start()
	return changes, nil
}
