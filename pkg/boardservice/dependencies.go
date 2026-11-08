package boardservice

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// Card dependencies (docs/design/dependencies.md, N1–N7): a card waits for
// other cards. The list is written whole; the lock and the customers it
// passes on are derived on reading.

// DependencyBackend is what a backend offers for dependency lists. A
// separate interface, as OwnershipBackend is, so an embedder's backend
// written before the feature still satisfies Backend.
type DependencyBackend interface {
	SetDependsOn(ctx context.Context, b board.Board, card board.Card, ids []string) error
}

// ErrDependenciesUnsupported is a backend that keeps no dependency lists.
var ErrDependenciesUnsupported = errors.New("this board's storage keeps no dependencies")

// ErrDependencyTarget is a dependency on a card that cannot be waited for:
// not on the board, the card itself, a review card (or from one), or the
// card's own ancestor or descendant (N1).
var ErrDependencyTarget = errors.New("a card cannot wait for that card")

// ErrDependencyCycle is a dependency that would close a cycle (N2).
var ErrDependencyCycle = errors.New("that dependency would make the cards wait for each other")

// SetDependsOn replaces the list of cards a card waits for. Each id once, in
// the order given; an id new to the list must be a lawful target that closes
// no cycle, checked before anything is written; an id already on the list
// the board no longer holds is dangling and is dropped (N6). An unchanged
// list writes nothing.
func (s *Service) SetDependsOn(ctx context.Context, boardID string, itemID string, ids []string) error {
	db, ok := s.backend.(DependencyBackend)
	if !ok {
		return ErrDependenciesUnsupported
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	ix := board.NewIndex(b)
	var clean []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || slices.Contains(clean, id) {
			continue
		}
		if _, held := ix.Find(id); !held && slices.Contains(card.DependsOn, id) {
			continue // dangling: the target is gone (N6)
		}
		clean = append(clean, id)
	}
	if slices.Equal(clean, card.DependsOn) {
		return nil
	}
	if len(clean) > 0 && card.ReviewOf != "" {
		return fmt.Errorf("%w: %q is a review card", ErrDependencyTarget, card.Title)
	}
	// Checked on the board as the write would leave it, so two new ids that
	// close a cycle only together are found too.
	after := b
	after.Cards = slices.Clone(b.Cards)
	for i := range after.Cards {
		if after.Cards[i].ItemID == card.ItemID {
			after.Cards[i].DependsOn = clean
		}
	}
	tentative := board.NewIndex(after)
	self, _ := tentative.Find(card.ItemID)
	for _, id := range clean {
		if slices.Contains(card.DependsOn, id) {
			continue // on the list already: lawful when it was written
		}
		if !ix.DependencyTargetOK(card, id) {
			return fmt.Errorf("%w: %q", ErrDependencyTarget, id)
		}
		if tentative.WouldCycle(self, id) {
			return fmt.Errorf("%w: %q", ErrDependencyCycle, id)
		}
	}
	if err := db.SetDependsOn(ctx, b, card, clean); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventDependsOn, strings.Join(card.DependsOn, ","), strings.Join(clean, ","))
	return nil
}
