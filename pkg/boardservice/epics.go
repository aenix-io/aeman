package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrEpicInUse is returned when deleting an epic that still has cards filed
// under it: silently orphaning them would hide planned work, exactly the
// failure mode the Plan board exists to prevent.
var ErrEpicInUse = errors.New("epic still has cards")

// ErrEpicExists guards AddEpic against doubling a column.
var ErrEpicExists = errors.New("epic already exists")

// ErrEpicNotFound is filing a card under a column that does not exist — a
// typo must not mint a phantom column the way a stray team value used to
// mint a team. It is a rejected input (422), not an upstream failure.
var ErrEpicNotFound = errors.New("unknown epic")

// AddEpic declares a new Plan-board column by creating its hidden epic-state
// card (the exact team-roster mechanism: the card's position IS the column
// order). The name is the epic's identity — renames are a delete+add while
// the column is empty.
func (s *Service) AddEpic(ctx context.Context, owner string, project int, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("epic name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	for _, e := range b.Epics {
		if strings.EqualFold(e, name) {
			return fmt.Errorf("%w: %q", ErrEpicExists, e)
		}
	}
	_, err = s.backend.CreateCard(ctx, b, board.CreateInput{
		Title: board.EpicStateTitle,
		Epic:  name,
	})
	return err
}

// DeleteEpic removes an empty epic column by deleting its epic-state card. An
// epic still referenced by cards is protected (ErrEpicInUse).
func (s *Service) DeleteEpic(ctx context.Context, owner string, project int, name string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	inUse := 0
	for _, c := range b.Cards {
		if c.Epic == name {
			inUse++
		}
	}
	if inUse > 0 {
		return fmt.Errorf("%w: %d card(s) still under %q — move or clear them first", ErrEpicInUse, inUse, name)
	}
	itemID, ok := b.EpicStates[name]
	if !ok || itemID == "" {
		return nil
	}
	stub := board.Card{ItemID: itemID, Title: board.EpicStateTitle, Epic: name}
	return s.backend.DeleteCard(ctx, b, stub)
}

// ReorderEpics persists a column order by walking the epic-state cards into
// the given sequence (mirrors ReorderTeams).
func (s *Service) ReorderEpics(ctx context.Context, owner string, project int, epics []string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	prev := ""
	for _, name := range epics {
		itemID, ok := b.EpicStates[name]
		if !ok || itemID == "" {
			continue
		}
		stub := board.Card{ItemID: itemID, Title: board.EpicStateTitle, Epic: name}
		if err := s.backend.MoveCard(ctx, b, stub, prev); err != nil {
			return err
		}
		prev = itemID
	}
	return nil
}

// SetEpic files a card under a Plan-board column ("" clears it). The column
// must exist — a typo must not mint a phantom column the way a stray team
// value used to mint a team.
func (s *Service) SetEpic(ctx context.Context, owner string, project int, itemID, epic string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	if epic != "" {
		known := false
		for _, e := range b.Epics {
			if e == epic {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("%w %q — add it first (add_epic / POST /epics)", ErrEpicNotFound, epic)
		}
	}
	if card.Epic == epic {
		return nil
	}
	if err := s.backend.SetEpic(ctx, b, card, epic); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventEpic, card.Epic, epic)
	return nil
}
