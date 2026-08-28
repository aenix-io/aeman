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

// ErrPersonalPlacement refuses a team, column or plan band on a personal
// card: those are the team board's coordinates, and a personal card has none.
var ErrPersonalPlacement = errors.New("a personal card has no team, column or plan band")

// createPersonalCard files a card in the actor's own domain — a backlog item
// with a zone, dates and a body, assigned to its owner, and none of the team
// board's placement. The service names the domain; the caller asked for
// "personal" and nothing more.
func (s *Service) createPersonalCard(ctx context.Context, b board.Board, args CreateCardArgs, linkDescription string, pendingRef *board.Link) (board.Card, error) {
	if args.Team != "" || args.Project != "" || args.Epic != "" || args.Plan != board.PlanNone || args.Week != "" {
		return board.Card{}, ErrPersonalPlacement
	}
	actor := board.ActorFrom(ctx)
	if actor == "" {
		return board.Card{}, errors.New("a personal card needs a signed-in person to belong to")
	}
	day, start := args.Day, args.Start
	switch {
	case start == "" && day != "":
		start = day
	case day == "" && start != "":
		day = start
	}
	card, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:    args.Title,
		Zone:     args.Zone,
		Day:      day,
		Start:    start,
		Assignee: actor,
		Parent:   args.Parent,
		Personal: true,
		Domain:   board.PersonalDomain(actor),
	})
	card, err = s.withLinkDescription(ctx, b, card, err, linkDescription)
	if err == nil {
		s.resolveTitleAsync(ctx, b, card, pendingRef)
		s.logEvent(ctx, b, card, board.EventCreated, "", "")
	}
	return card, err
}

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
