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
// ReseedPersonal turns the day over on login's personal board as of day (today
// when empty): every finished recurrent card whose cycle came due gets its
// fresh copy — same title, zone and body, 0%, assigned to the owner, in the
// personal domain — the way carry-over reseeds a team's. A personal board has
// no carry-over, so its readers do this: the personal view calls it before
// listing. Idempotent — a copy already seeded is never seeded again — and a
// no-op without a login. Returns how many copies were created.
func (s *Service) ReseedPersonal(ctx context.Context, boardID, login, day string) (int, error) {
	if login == "" {
		return 0, nil
	}
	if day == "" {
		day = board.TodayIso()
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range board.PersonalReseed(b, login, day) {
		if err := s.reseedRecurrent(ctx, b, c, board.CreateInput{
			Title:    c.Title,
			Zone:     c.Zone,
			Day:      day,
			Start:    day,
			Assignee: login,
			Personal: true,
			Domain:   board.PersonalDomain(login),
		}); err != nil {
			return n, fmt.Errorf("reseed %q on %s's personal board: %w", c.Title, login, err)
		}
		n++
	}
	return n, nil
}

func (s *Service) createPersonalCard(ctx context.Context, b board.Board, args CreateCardArgs, linkDescription string, pendingRef *board.Link) (board.Card, error) {
	if args.Team != "" || args.Project != "" || args.Epic != "" || args.Week != "" {
		return board.Card{}, ErrPersonalPlacement
	}
	actor := board.ActorFrom(ctx)
	if actor == "" {
		return board.Card{}, errors.New("a personal card needs a signed-in person to belong to")
	}
	// A personal card's file is the ACTOR's own repository, whatever its
	// parent is — so a parent living anywhere else would put the pair in
	// two repositories, with the linked-cards rule (G14) saying one thing
	// and the file saying another.
	if args.Parent != "" {
		p, ok := findCard(b, args.Parent)
		if !ok {
			return board.Card{}, ErrParentNotFound
		}
		// The card's own stored domain, not DomainOf: a personal card is
		// placed by nothing — no team, no project, no link — so the rule
		// that reads placement answers "" for it, and the file is what
		// says whose board it is.
		if p.Domain != board.PersonalDomain(actor) {
			return board.Card{}, fmt.Errorf("%w: a personal card can only join a group of your own board",
				ErrCrossDomain)
		}
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
		// Born parented — a create-then-group pair broadcasts a parentless
		// instant — and grouped below through SetParent, for the side
		// effects the field alone does not carry: the sprint and person
		// sync, the plan slot, the riders a subtask may not keep.
		Parent:   args.Parent,
		Personal: true,
		Domain:   board.PersonalDomain(actor),
	})
	card, err = s.withLinkDescription(ctx, b, card, err, linkDescription)
	if err == nil {
		s.resolveTitleAsync(ctx, b, card, pendingRef)
		s.logEvent(ctx, b, card, board.EventCreated, "", "")
	}
	if err == nil && args.Parent != "" {
		if perr := s.groupOrUndo(ctx, b, card, args.Parent); perr != nil {
			return board.Card{}, perr
		}
		card.Parent = args.Parent
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
