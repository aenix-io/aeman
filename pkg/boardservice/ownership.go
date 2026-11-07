package boardservice

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// Ownership — docs/design/ownership.md, rows O1–O10: who answers for a plan
// item (its OWNER, one person, a planning card's alone), which team carries
// it out (its TEAM), and which customers the work is for (CUSTOMERS, from
// the board's roster, named by id).

// OwnershipBackend is what a backend offers for owners, customers and the
// customer roster. It is a separate interface so an embedder's backend
// written before the feature still satisfies Backend; the service answers
// ErrOwnershipUnsupported where a backend does not offer it.
type OwnershipBackend interface {
	SetOwner(ctx context.Context, b board.Board, card board.Card, login string) error
	SetCustomers(ctx context.Context, b board.Board, card board.Card, ids []string) error
	AddCustomer(ctx context.Context, b board.Board, name string) (board.Customer, error)
	RenameCustomer(ctx context.Context, b board.Board, id, name string) error
	MoveCustomer(ctx context.Context, b board.Board, id, afterID string) error
	DeleteCustomer(ctx context.Context, b board.Board, id string) error
}

// ErrOwnershipUnsupported is a backend that keeps no owners or customers.
var ErrOwnershipUnsupported = errors.New("this board's storage keeps no owners or customers")

// ErrOwnerIsThePlans is an owner named on a card that is not a goal, an
// initiative or an epic: an owner answers for a plan item (O1); a task's
// people are its assignees.
var ErrOwnerIsThePlans = errors.New("an owner is a goal's, an initiative's or an epic's; a task's people are its assignees")

// ErrCustomerNotFound is a customer id the board's roster does not hold.
var ErrCustomerNotFound = errors.New("no such customer on the board's roster")

// ErrCustomerExists is a customer name the roster already has (case aside).
var ErrCustomerExists = errors.New("the board already has a customer of that name")

// ErrCustomerName is a customer with no name.
var ErrCustomerName = errors.New("a customer needs a name")

func (s *Service) ownership() (OwnershipBackend, error) {
	ob, ok := s.backend.(OwnershipBackend)
	if !ok {
		return nil, ErrOwnershipUnsupported
	}
	return ob, nil
}

// SetOwner names the person who answers for a planning card ("" takes them
// off). Never inherited, and it puts nothing on any day board (O2).
func (s *Service) SetOwner(ctx context.Context, boardID string, itemID, login string) error {
	ob, err := s.ownership()
	if err != nil {
		return err
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	login = strings.TrimSpace(login)
	if !board.IsPlanning(card) {
		if login == "" && card.Owner == "" {
			return nil
		}
		return fmt.Errorf("%w: %q", ErrOwnerIsThePlans, card.Title)
	}
	return s.setOwner(ctx, ob, b, card, login)
}

func (s *Service) setOwner(ctx context.Context, ob OwnershipBackend, b board.Board, card board.Card, login string) error {
	if card.Owner == login {
		return nil
	}
	if err := ob.SetOwner(ctx, b, card, login); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventOwner, card.Owner, login)
	return nil
}

// ownPlan is `own` on a planning card (O9): the answer is its team and its
// owner — a person needs no team here, since they answer for the plan item
// rather than doing it — and nothing about it reaches a day board. Each half
// is set only when it is given: a person alone keeps the card's team, a team
// alone keeps its owner, and nobody (neither) takes both off. Clearing one
// half alone is the update's (SetTeam, SetOwner with "").
func (s *Service) ownPlan(ctx context.Context, b board.Board, card board.Card, team, person string) error {
	ob, err := s.ownership()
	if err != nil {
		return err
	}
	if team == "" && person != "" {
		team = card.Team
	}
	if person == "" && team != "" {
		person = card.Owner
	}
	if card.Team != team {
		if err := s.setPlanTeam(ctx, b, card, team); err != nil {
			return err
		}
	}
	return s.setOwner(ctx, ob, b, card, person)
}

// setPlanTeam gives a planning card the team that carries it out (O1). It
// places nothing itself — the card lives in the primary (H3) — and joins no
// sprint: a planning card is on no day board. But the tasks below it with
// no team of their own stand under it (O13), so those in work follow it
// into its sprint (followPlacement). A team the roster lacks is declared,
// as at every other door.
func (s *Service) setPlanTeam(ctx context.Context, b board.Board, card board.Card, team string) error {
	if err := s.declareTeam(ctx, b, team); err != nil {
		return err
	}
	if err := s.backend.SetTeam(ctx, b, card, team); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventTeam, card.Team, team)
	return s.followPlacement(ctx, b, editCard(card.ItemID, func(c *board.Card) { c.Team = team }))
}

// SetCustomers names the customers a card is for (ids from the roster; nil
// takes them all off). Any card (O6); each id once,
// in the order given; an id new to the card that the roster lacks is
// refused before anything is written, and one the card already names that
// the roster no longer holds is dropped (O5). The cards below inherit them
// (O4) without a write.
func (s *Service) SetCustomers(ctx context.Context, boardID string, itemID string, ids []string) error {
	ob, err := s.ownership()
	if err != nil {
		return err
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	return s.setCustomers(ctx, ob, b, card, ids)
}

func (s *Service) setCustomers(ctx context.Context, ob OwnershipBackend, b board.Board, card board.Card, ids []string) error {
	var clean []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || slices.Contains(clean, id) {
			continue
		}
		if b.CustomerName(id) == "" {
			// One the card already names is a customer deleted since — a
			// delete that raced this edit, or a write replayed after the
			// other side's delete — and it names nobody: dropped, never a
			// reason to refuse the edit. Only a NEW id must be on the roster.
			if slices.Contains(card.Customers, id) {
				continue
			}
			return fmt.Errorf("%w: %q", ErrCustomerNotFound, id)
		}
		clean = append(clean, id)
	}
	if slices.Equal(clean, card.Customers) {
		return nil
	}
	if err := ob.SetCustomers(ctx, b, card, clean); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventCustomers, customerNames(b, card.Customers), customerNames(b, clean))
	return nil
}

func customerNames(b board.Board, ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if n := b.CustomerName(id); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

// AddCustomer declares a customer, last in the roster (O5).
func (s *Service) AddCustomer(ctx context.Context, boardID string, name string) (board.Customer, error) {
	ob, err := s.ownership()
	if err != nil {
		return board.Customer{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return board.Customer{}, ErrCustomerName
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return board.Customer{}, err
	}
	if err := customerNameFree(b, name, ""); err != nil {
		return board.Customer{}, err
	}
	return ob.AddCustomer(ctx, b, name)
}

func customerNameFree(b board.Board, name, except string) error {
	for _, c := range b.Customers {
		if c.ID != except && strings.EqualFold(c.Name, name) {
			return fmt.Errorf("%w: %q", ErrCustomerExists, c.Name)
		}
	}
	return nil
}

func hasCustomer(b board.Board, id string) error {
	if b.CustomerName(id) == "" {
		return fmt.Errorf("%w: %q", ErrCustomerNotFound, id)
	}
	return nil
}

// RenameCustomer renames a customer; every card that names it reads the new
// name at once, since cards hold the id (O5).
func (s *Service) RenameCustomer(ctx context.Context, boardID string, id, name string) error {
	ob, err := s.ownership()
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrCustomerName
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if err := hasCustomer(b, id); err != nil {
		return err
	}
	if err := customerNameFree(b, name, id); err != nil {
		return err
	}
	return ob.RenameCustomer(ctx, b, id, name)
}

// ReorderCustomers puts the roster in the order given: the ids named first,
// in that order, the rest after them as they stood.
func (s *Service) ReorderCustomers(ctx context.Context, boardID string, ids []string) error {
	ob, err := s.ownership()
	if err != nil {
		return err
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := hasCustomer(b, id); err != nil {
			return err
		}
	}
	after := ""
	for _, id := range ids {
		if err := ob.MoveCustomer(ctx, b, id, after); err != nil {
			return err
		}
		after = id
	}
	return nil
}

// DeleteCustomer takes a customer out of the roster and off every card that
// names it (O5).
func (s *Service) DeleteCustomer(ctx context.Context, boardID string, id string) error {
	ob, err := s.ownership()
	if err != nil {
		return err
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if err := hasCustomer(b, id); err != nil {
		return err
	}
	return ob.DeleteCustomer(ctx, b, id)
}
