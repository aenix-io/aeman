package server

import (
	"context"
	"slices"

	"github.com/aenix-io/aeman/internal/server/apiv1"
	"github.com/aenix-io/aeman/pkg/apiserver"
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
//
// It does not wait for the card writes queued before it: a SetCustomers
// still in the queue may write the id back onto a card after the delete
// landed. That leaves a dangling id, which is inert by design — no read
// serves it (board.Index.OnRoster, EffectiveCustomers) and the next edit of
// the card's customers drops it (boardservice.setCustomers) — the same state
// a rebase replaying an edit over the other side's delete leaves behind.
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

// DeleteCustomer needs the right to write the primary, and every
// repository holding a card that names the customer: the delete takes the
// id off those cards in the same stroke, with the server's credential, so a
// visitor who may not write one of them is refused (403) rather than
// writing there through the roster (O5). Read off the whole board, not the
// visitor's projection: a card they cannot read is still written.
func (v *visibleBackend) DeleteCustomer(ctx context.Context, bd board.Board, id string) error {
	ob, err := v.ownership()
	if err != nil {
		return err
	}
	if err := v.rosterRight(ctx); err != nil {
		return err
	}
	if rightsFrom(ctx) != nil {
		whole, err := v.full(ctx, bd)
		if err != nil {
			return err
		}
		for _, c := range whole.Cards {
			if !slices.Contains(c.Customers, id) {
				continue
			}
			if err := v.write(ctx, whole, c); err != nil {
				return err
			}
		}
	}
	return ob.DeleteCustomer(ctx, bd, id)
}

// ---- the customer roster's routes (ADR 0002) ----

// AddCustomer adds a customer to the board's roster.
func (a surface) AddCustomer(ctx context.Context, req apiv1.AddCustomerRequestObject) (apiv1.AddCustomerResponseObject, error) {
	var cu board.Customer
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		var err error
		cu, err = svc.AddCustomer(ctx, boardID, strOf(req.Body.Name))
		return err
	}); err != nil {
		return nil, err
	}
	return apiv1.AddCustomer201JSONResponse(apiserver.CustomerRef{ID: cu.ID, Name: cu.Name}), nil
}

// RenameCustomer renames a customer; cards hold its id and follow.
func (a surface) RenameCustomer(ctx context.Context, req apiv1.RenameCustomerRequestObject) (apiv1.RenameCustomerResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.RenameCustomer(ctx, boardID, strOf(req.Body.Customer), strOf(req.Body.To))
	}); err != nil {
		return nil, err
	}
	return apiv1.RenameCustomer204Response{}, nil
}

// ReorderCustomers applies the roster's order.
func (a surface) ReorderCustomers(ctx context.Context, req apiv1.ReorderCustomersRequestObject) (apiv1.ReorderCustomersResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.ReorderCustomers(ctx, boardID, listOf(req.Body.Customers))
	}); err != nil {
		return nil, err
	}
	return apiv1.ReorderCustomers204Response{}, nil
}

// DeleteCustomer deletes a customer and takes it off every card.
func (a surface) DeleteCustomer(ctx context.Context, req apiv1.DeleteCustomerRequestObject) (apiv1.DeleteCustomerResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.DeleteCustomer(ctx, boardID, strOf(req.Body.Customer))
	}); err != nil {
		return nil, err
	}
	return apiv1.DeleteCustomer204Response{}, nil
}
