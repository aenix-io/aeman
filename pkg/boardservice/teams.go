package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrTeamExists refuses a team name another team already carries. A team's
// name is its identity — cards say `team: portal` — so two of them cannot
// coexist, whichever repository each is declared in.
var ErrTeamExists = errors.New("team already exists")

// ErrTeamNotFound names a team the board does not declare.
var ErrTeamNotFound = errors.New("team not found")

// RenameTeam renames a team in place: its own declaration (the sprint
// pointer stays), and the Team field of every card and process task that
// names it — one action, so the roster and the cards never disagree. The
// no-team group has no name to rename.
func (s *Service) RenameTeam(ctx context.Context, boardID, from, to string) error {
	to = strings.TrimSpace(to)
	if from == "" {
		return errors.New("the no-team group cannot be renamed")
	}
	if to == "" {
		return errors.New("team name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	st, ok := b.SprintStates[from]
	if !ok {
		return fmt.Errorf("%w: %q", ErrTeamNotFound, from)
	}
	if from == to {
		return nil
	}
	for name := range b.SprintStates {
		if name != from && strings.EqualFold(name, to) {
			return fmt.Errorf("%w: %q", ErrTeamExists, name)
		}
	}
	stub := board.Card{ItemID: st.ItemID, Title: board.SprintStateTitle, Team: from}
	if err := s.backend.SetTeam(ctx, b, stub, to); err != nil {
		return err
	}
	for _, c := range b.Cards {
		if c.Team == from {
			if err := s.backend.SetTeam(ctx, b, c, to); err != nil {
				return err
			}
		}
	}
	for _, c := range b.Tasks {
		if c.Team == from {
			if err := s.backend.SetTeam(ctx, b, c, to); err != nil {
				return err
			}
		}
	}
	return nil
}
