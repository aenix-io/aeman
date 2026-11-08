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
// write.
func (b *storeBackend) SetDependsOn(ctx context.Context, bd board.Board, card board.Card, ids []string) error {
	db, ok := b.inner.(boardservice.DependencyBackend)
	if !ok {
		return boardservice.ErrDependenciesUnsupported
	}
	ids = slices.Clone(ids)
	b.mutateCard(ctx, bd, card.ItemID, "depends-on", "set what "+cardRef(card)+" waits for", func(c *board.Card) {
		c.DependsOn = slices.Clone(ids)
	}, func(ctx context.Context) error {
		return db.SetDependsOn(ctx, bd, card, ids)
	})
	return nil
}

// SetDependsOn needs the right to write the card, and keeps what the visitor
// cannot see: their list is the readable part, and a dependency on a card in
// a repository they cannot read — which their board does not hold, and so
// which their list cannot name — stays on the card (N4).
func (v *visibleBackend) SetDependsOn(ctx context.Context, bd board.Board, card board.Card, ids []string) error {
	db, ok := v.Backend.(boardservice.DependencyBackend)
	if !ok {
		return boardservice.ErrDependenciesUnsupported
	}
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	if r := rightsFrom(ctx); r != nil && len(card.DependsOn) > 0 {
		whole, err := v.full(ctx, bd)
		if err != nil {
			return err
		}
		ix := board.NewIndex(whole)
		for _, id := range card.DependsOn {
			if t, held := ix.Find(id); held && !r.canRead(t.Domain) && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return db.SetDependsOn(ctx, bd, card, ids)
}
