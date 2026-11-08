package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// ORDERING ACTIONS ON THE CACHE (docs/design/ordering.md, "Atomicity"). The
// plan is computed over the cached board, applied to it and announced in ONE
// Ordering frame, all under the entry's lock — so no reader, no watch frame
// and no other request's plan ever sees half of a mass move — and its write
// is ONE queued op of the request's action: every file of the plan staged
// under one git scope, one commit per repository touched. A write that fails
// commits nothing (the scope is never flushed) and the queue's failure path
// reloads the cache from the branch tip.

// actionRecord is an ordering action this process made: its changes and
// when, so an Undo pressed before the commit has drained still finds it.
type actionRecord struct {
	changes []board.RankChange
	at      time.Time
}

// recentActions remembers this process's ordering actions for the undo
// window, by action id.
type recentActions struct {
	mu   sync.Mutex
	byID map[string]actionRecord
}

func (r *recentActions) remember(id string, changes []board.RankChange, now time.Time) {
	if id == "" || len(changes) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID == nil {
		r.byID = map[string]actionRecord{}
	}
	for k, rec := range r.byID {
		if now.Sub(rec.at) > gitstore.UndoWindow {
			delete(r.byID, k)
		}
	}
	rec := r.byID[id]
	for _, ch := range changes {
		merged := false
		for i := range rec.changes {
			if rec.changes[i].ID == ch.ID {
				rec.changes[i].After = ch.After
				merged = true
			}
		}
		if !merged {
			rec.changes = append(rec.changes, ch)
		}
	}
	if rec.at.IsZero() {
		rec.at = now
	}
	r.byID[id] = rec
}

func (r *recentActions) lookup(id string) (actionRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.byID[id]
	return rec, ok
}

// processActions is the record of this process's ordering actions, shared
// by every store backend of the process.
var processActions recentActions

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
	e.mu.Unlock()
	processActions.remember(actionFrom(ctx).ID, changes, time.Now())
	desc := fmt.Sprintf("reorder %d cards", len(changes))
	itemID := ""
	if len(changes) == 1 {
		desc, itemID = "move "+changes[0].ID, changes[0].ID
	}
	written := append([]board.RankChange(nil), changes...)
	b.enqueue(ctx, e, pendingOp{
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
	return changes, nil
}

// ActionRanks is an ordering action's changes: this process's record when it
// made the action, else what the repositories' commits say.
func (b *storeBackend) ActionRanks(ctx context.Context, id string) ([]board.RankChange, time.Time, error) {
	if rec, ok := processActions.lookup(id); ok {
		return append([]board.RankChange(nil), rec.changes...), rec.at, nil
	}
	if ar, ok := b.inner.(interface {
		ActionRanks(context.Context, string) ([]board.RankChange, time.Time, error)
	}); ok {
		return ar.ActionRanks(ctx, id)
	}
	return nil, time.Time{}, gitstore.ErrActionNotFound
}

// ActionRanks forwards an Undo's question to the store behind the rights
// check: the action's changes are the board's, and the Undo's own write is
// judged card by card by Reorder.
func (v *visibleBackend) ActionRanks(ctx context.Context, id string) ([]board.RankChange, time.Time, error) {
	if ar, ok := v.Backend.(boardservice.ActionRanker); ok {
		return ar.ActionRanks(ctx, id)
	}
	return nil, time.Time{}, gitstore.ErrActionNotFound
}
