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
// no cycle, checked before anything is written; an id on the list the board
// does not hold — dangling (N6), or a card the caller cannot read — is not
// the caller's to name, and the backend keeps what it must of it. The list is
// compared with the card's readable part as a SET: reordering, or leaving out
// what the caller cannot name, writes nothing. The event names only what the
// caller's board holds.
//
// The check here is on the caller's board and names what it refuses; the
// backend judges again on the whole board, and where writes are put in order
// (DependencyBackend), and refuses there without naming anything.
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
	var held []string
	for _, id := range card.DependsOn {
		if _, ok := ix.Find(id); ok {
			held = append(held, id)
		}
	}
	var clean []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || slices.Contains(clean, id) {
			continue
		}
		if _, ok := ix.Find(id); !ok && slices.Contains(card.DependsOn, id) {
			continue // dangling, or not the caller's to name (N6)
		}
		clean = append(clean, id)
	}
	if sameSet(clean, held) {
		return nil
	}
	switch problem, id := board.CheckDependsOn(b, card.ItemID, clean); problem {
	case board.DependencyBadTarget:
		if id == "" {
			return fmt.Errorf("%w: %q is a review card", ErrDependencyTarget, card.Title)
		}
		return fmt.Errorf("%w: %q", ErrDependencyTarget, id)
	case board.DependencyClosesCycle:
		return fmt.Errorf("%w: %q", ErrDependencyCycle, id)
	}
	if err := db.SetDependsOn(ctx, b, card, clean); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventDependsOn, strings.Join(held, ","), strings.Join(clean, ","))
	return nil
}

// sameSet reports two id lists holding the same ids, order aside.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		if !slices.Contains(b, id) {
			return false
		}
	}
	return true
}

// DependencyRefusal turns a whole-board check's answer into the error a
// caller gets: a backend that judged on cards the caller may not read names
// none of them.
func DependencyRefusal(problem board.DependencyProblem) error {
	switch problem {
	case board.DependencyBadTarget:
		return ErrDependencyTarget
	case board.DependencyClosesCycle:
		return ErrDependencyCycle
	}
	return nil
}
