package server

import (
	"context"
	"slices"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// Ownership through the cache (docs/design/ownership.md): a card's owner and
// customers are card edits like any other — answered from the cache at once,
// written by the queue — and the customer roster's entries are written at
// once, as the other roster entries are, then read back into the cache.

func (b *storeBackend) ownership() (boardservice.OwnershipBackend, error) {
	ob, ok := b.inner.(boardservice.OwnershipBackend)
	if !ok {
		return nil, boardservice.ErrOwnershipUnsupported
	}
	return ob, nil
}

// SetOwner edits a planning card's owner in the cache and queues the write.
func (b *storeBackend) SetOwner(ctx context.Context, bd board.Board, card board.Card, login string) error {
	ob, err := b.ownership()
	if err != nil {
		return err
	}
	b.mutateCard(ctx, bd, card.ItemID, "owner", "set the owner of "+cardRef(card), func(c *board.Card) {
		c.Owner = login
	}, func(ctx context.Context) error {
		return ob.SetOwner(ctx, bd, card, login)
	})
	return nil
}

// SetCustomers edits a card's customers in the cache and queues the write.
func (b *storeBackend) SetCustomers(ctx context.Context, bd board.Board, card board.Card, ids []string) error {
	ob, err := b.ownership()
	if err != nil {
		return err
	}
	ids = slices.Clone(ids)
	b.mutateCard(ctx, bd, card.ItemID, "customers", "set the customers of "+cardRef(card), func(c *board.Card) {
		c.Customers = slices.Clone(ids)
	}, func(ctx context.Context) error {
		return ob.SetCustomers(ctx, bd, card, ids)
	})
	return nil
}

// rosterAfter re-reads the customer roster into the cache after a roster
// write, and announces it.
func (b *storeBackend) rosterAfter(ctx context.Context, bd board.Board) {
	fresh, err := b.inner.LoadBoard(ctx, bd.Board)
	if err != nil {
		return
	}
	e := b.store.entry(storeKey(bd.Board))
	e.mu.Lock()
	if e.loaded {
		e.board.Customers = slices.Clone(fresh.Customers)
		e.rosterBroadcast()
	}
	e.mu.Unlock()
}

// AddCustomer declares a customer.
func (b *storeBackend) AddCustomer(ctx context.Context, bd board.Board, name string) (board.Customer, error) {
	ob, err := b.ownership()
	if err != nil {
		return board.Customer{}, err
	}
	cu, err := ob.AddCustomer(ctx, bd, name)
	if err != nil {
		return cu, err
	}
	b.rosterAfter(ctx, bd)
	return cu, nil
}

// RenameCustomer renames a customer.
func (b *storeBackend) RenameCustomer(ctx context.Context, bd board.Board, id, name string) error {
	ob, err := b.ownership()
	if err != nil {
		return err
	}
	if err := ob.RenameCustomer(ctx, bd, id, name); err != nil {
		return err
	}
	b.rosterAfter(ctx, bd)
	return nil
}

// MoveCustomer reorders the roster.
func (b *storeBackend) MoveCustomer(ctx context.Context, bd board.Board, id, afterID string) error {
	ob, err := b.ownership()
	if err != nil {
		return err
	}
	if err := ob.MoveCustomer(ctx, bd, id, afterID); err != nil {
		return err
	}
	b.rosterAfter(ctx, bd)
	return nil
}

// DeleteCustomer deletes a customer and takes it off every cached card that
// names it, announcing each card that changed.
func (b *storeBackend) DeleteCustomer(ctx context.Context, bd board.Board, id string) error {
	ob, err := b.ownership()
	if err != nil {
		return err
	}
	if err := ob.DeleteCustomer(ctx, bd, id); err != nil {
		return err
	}
	e := b.store.entry(storeKey(bd.Board))
	e.mu.Lock()
	if e.loaded {
		var changed []board.Card
		for i := range e.board.Cards {
			c := &e.board.Cards[i]
			if slices.Contains(c.Customers, id) {
				c.Customers = slices.DeleteFunc(slices.Clone(c.Customers), func(x string) bool { return x == id })
				changed = append(changed, *c)
			}
		}
		for _, c := range changed {
			e.cardChanged(clientIDFrom(ctx), c, "MODIFIED")
		}
	}
	e.mu.Unlock()
	b.rosterAfter(ctx, bd)
	return nil
}

// ---- the visitor's rights ----

func (v *visibleBackend) ownership() (boardservice.OwnershipBackend, error) {
	ob, ok := v.Backend.(boardservice.OwnershipBackend)
	if !ok {
		return nil, boardservice.ErrOwnershipUnsupported
	}
	return ob, nil
}

// SetOwner needs the right to write the card.
func (v *visibleBackend) SetOwner(ctx context.Context, bd board.Board, card board.Card, login string) error {
	ob, err := v.ownership()
	if err != nil {
		return err
	}
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return ob.SetOwner(ctx, bd, card, login)
}

// SetCustomers needs the right to write the card.
func (v *visibleBackend) SetCustomers(ctx context.Context, bd board.Board, card board.Card, ids []string) error {
	ob, err := v.ownership()
	if err != nil {
		return err
	}
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return ob.SetCustomers(ctx, bd, card, ids)
}

// rosterRight is the right to write the customer roster: the primary's.
func (v *visibleBackend) rosterRight(ctx context.Context) error {
	if r := rightsFrom(ctx); r != nil && !r.canWrite(v.primary) {
		return boardservice.ErrForbidden
	}
	return nil
}

// AddCustomer needs the right to write the primary.
func (v *visibleBackend) AddCustomer(ctx context.Context, bd board.Board, name string) (board.Customer, error) {
	ob, err := v.ownership()
	if err != nil {
		return board.Customer{}, err
	}
	if err := v.rosterRight(ctx); err != nil {
		return board.Customer{}, err
	}
	return ob.AddCustomer(ctx, bd, name)
}

// RenameCustomer needs the right to write the primary.
func (v *visibleBackend) RenameCustomer(ctx context.Context, bd board.Board, id, name string) error {
	ob, err := v.ownership()
	if err != nil {
		return err
	}
	if err := v.rosterRight(ctx); err != nil {
		return err
	}
	return ob.RenameCustomer(ctx, bd, id, name)
}

// MoveCustomer needs the right to write the primary.
func (v *visibleBackend) MoveCustomer(ctx context.Context, bd board.Board, id, afterID string) error {
	ob, err := v.ownership()
	if err != nil {
		return err
	}
	if err := v.rosterRight(ctx); err != nil {
		return err
	}
	return ob.MoveCustomer(ctx, bd, id, afterID)
}

// DeleteCustomer needs the right to write the primary. It reaches cards of
// every domain, as a team's deletion does.
func (v *visibleBackend) DeleteCustomer(ctx context.Context, bd board.Board, id string) error {
	ob, err := v.ownership()
	if err != nil {
		return err
	}
	if err := v.rosterRight(ctx); err != nil {
		return err
	}
	return ob.DeleteCustomer(ctx, bd, id)
}
