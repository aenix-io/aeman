package server

import (
	"context"
	"slices"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// Dependencies through the cache (docs/design/dependencies.md): a card's
// dependency list is a card edit like any other — answered from the cache
// at once, written by the queue; what it moves on other cards reaches the
// watch through dependencyFanout.

// SetDependsOn edits a card's dependency list in the cache and queues the
// write. The list is judged against the cache under the write lock — the
// whole board, as every write before this one left it — so two requests
// that each read the board before the other's write cannot close a cycle
// between them (N2).
func (b *storeBackend) SetDependsOn(ctx context.Context, bd board.Board, card board.Card, ids []string) error {
	return b.setDependsOnKeeping(ctx, bd, card, ids, nil)
}

// setDependsOnKeeping is SetDependsOn for a visitor's list: when hidden is
// set, the dependencies of the card AS THE CACHE HOLDS IT NOW that the
// visitor cannot read are kept where they stand (keepHidden), merged under
// the same lock the check and the edit take — a hidden dependency another
// request added after the visitor's board was read is not lost.
func (b *storeBackend) setDependsOnKeeping(ctx context.Context, bd board.Board, card board.Card, ids []string, hidden func(string) bool) error {
	db, ok := b.inner.(boardservice.DependencyBackend)
	if !ok {
		return boardservice.ErrDependenciesUnsupported
	}
	ids = slices.Clone(ids)
	e := b.store.entry(storeKey(bd.Board))
	apply := func(target *board.Board) {
		for i := range target.Cards {
			if target.Cards[i].ItemID == card.ItemID {
				target.Cards[i].DependsOn = slices.Clone(ids)
				return
			}
		}
	}
	e.mu.Lock()
	if hidden != nil {
		now := card.DependsOn
		if c, found := board.Find(e.board, card.ItemID); found {
			now = c.DependsOn
		}
		ids = keepHidden(now, ids, hidden)
	}
	if e.loaded {
		if problem, _ := board.CheckDependsOn(e.board, card.ItemID, ids); problem != board.DependencyFine {
			e.mu.Unlock()
			return boardservice.DependencyRefusal(problem)
		}
	}
	apply(&e.board)
	for i := range e.board.Cards {
		if e.board.Cards[i].ItemID == card.ItemID {
			e.markRecent(card.ItemID)
			e.cardChanged(echoOrigin(ctx, card.ItemID), e.board.Cards[i], "MODIFIED")
			break
		}
	}
	e.mu.Unlock()
	b.enqueue(ctx, e, pendingOp{key: "depends-on:" + card.ItemID, kind: "depends-on",
		desc: "set what " + cardRef(card) + " waits for", apply: apply, itemID: card.ItemID,
		exec: func(ctx context.Context) error { return db.SetDependsOn(ctx, bd, card, ids) }})
	return nil
}

// SetDependsOn needs the right to write the card, and keeps what the visitor
// cannot see: their list is the readable part, and a dependency on a card in
// a repository they cannot read — which their board does not hold, and so
// which their list cannot name — stays on the card, where it stood (N4).
// What they add is judged on the WHOLE board: theirs breaks a lineage at a
// card they cannot read and ends a cycle's walk there. Refused without naming
// anything, since the card that decides may be one they cannot read.
func (v *visibleBackend) SetDependsOn(ctx context.Context, bd board.Board, card board.Card, ids []string) error {
	db, ok := v.Backend.(boardservice.DependencyBackend)
	if !ok {
		return boardservice.ErrDependenciesUnsupported
	}
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	if r := rightsFrom(ctx); r != nil {
		whole, err := v.full(ctx, bd)
		if err != nil {
			return err
		}
		ix := board.NewIndex(whole)
		if full, ok := ix.Find(card.ItemID); ok {
			card = full
		}
		hidden := func(id string) bool {
			t, held := ix.Find(id)
			return held && !r.canRead(t.Domain)
		}
		merged := keepHidden(card.DependsOn, ids, hidden)
		if problem, _ := board.CheckDependsOn(whole, card.ItemID, merged); problem != board.DependencyFine {
			return boardservice.DependencyRefusal(problem)
		}
		// The cache merges again under its write lock, with the card as it
		// stands then; a backend without one takes this snapshot's merge.
		if sb, ok := v.Backend.(*storeBackend); ok {
			return sb.setDependsOnKeeping(ctx, bd, card, ids, hidden)
		}
		ids = merged
	}
	return db.SetDependsOn(ctx, bd, card, ids)
}

// keepHidden merges a visitor's list into the card's: a hidden dependency
// keeps its place, the visitor's ids fill the places of the readable ones in
// the visitor's order, and what is left of theirs follows.
func keepHidden(old, visitor []string, hidden func(string) bool) []string {
	out := make([]string, 0, len(old)+len(visitor))
	next := 0
	for _, id := range old {
		switch {
		case hidden(id):
			out = append(out, id)
		case next < len(visitor):
			out = append(out, visitor[next])
			next++
		}
	}
	return append(out, visitor[next:]...)
}
