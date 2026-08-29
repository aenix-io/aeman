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
// failure mode the Project board exists to prevent.
var ErrEpicInUse = errors.New("epic still has cards")

// ErrEpicExists guards against doubling a column WITHIN a project. Epic names
// repeat across projects on purpose — every project has its own "Docs".
var ErrEpicExists = errors.New("epic already exists")

// ErrWeekDerived is setting the week of a Project-board slot by hand: it is
// derived from the card's start date, so there is nothing to set. A rejected
// input (422), not an upstream failure.
var ErrWeekDerived = errors.New("a slot's week is derived from its dates")

// ErrEpicNotFound is filing a card under a column that does not exist — a
// typo must not mint a phantom column the way a stray team value used to
// mint a team. It is a rejected input (422), not an upstream failure.
var ErrEpicNotFound = errors.New("unknown epic")

// AddEpic declares a new column by creating its hidden epic-state card (the
// exact team-roster mechanism: the card's position IS the column order).
//
// A column normally belongs to a project, and naming one is how it ends up
// somewhere people look. An empty project is allowed but deliberate: it files
// the column in the no-project bucket, which the board shows behind its own
// chip. A project that does not exist is still refused — that is a typo.
func (s *Service) AddEpic(ctx context.Context, boardID string, name, projectName string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("epic name must not be empty")
	}
	projectName = strings.TrimSpace(projectName)
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if projectName != "" {
		if err := knownProject(b, projectName); err != nil {
			return err
		}
	}
	if err := epicNameFree(b, projectName, name, ""); err != nil {
		return err
	}
	_, err = s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:   board.EpicStateTitle,
		Epic:    name,
		Project: projectName,
	})
	return err
}

// DeleteEpic removes an empty column by deleting its epic-state card. A column
// still referenced by cards is protected (ErrEpicInUse).
func (s *Service) DeleteEpic(ctx context.Context, boardID string, name, projectName string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	inUse := 0
	for _, c := range b.Cards {
		if board.InEpic(c, projectName, name) {
			inUse++
		}
	}
	if inUse > 0 {
		return fmt.Errorf("%w: %d card(s) still under %q — move or clear them first", ErrEpicInUse, inUse, name)
	}
	col, ok := board.FindEpic(b, projectName, name)
	if !ok || col.ItemID == "" {
		return nil
	}
	stub := board.Card{ItemID: col.ItemID, Title: board.EpicStateTitle, Epic: name, Project: projectName}
	return s.backend.DeleteCard(ctx, b, stub)
}

// RenameEpic renames a column in place: its epic-state card and every card
// filed under it. The name IS the reference (cards store the text, not an id),
// so the two must move together or the cards would point at a column that no
// longer exists.
func (s *Service) RenameEpic(ctx context.Context, boardID string, projectName, from, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("epic name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	col, ok := board.FindEpic(b, projectName, from)
	if !ok || col.ItemID == "" {
		return fmt.Errorf("%w %q", ErrEpicNotFound, from)
	}
	if from == to {
		return nil
	}
	if err := epicNameFree(b, projectName, to, from); err != nil {
		return err
	}
	stub := board.Card{ItemID: col.ItemID, Title: board.EpicStateTitle, Epic: from, Project: projectName}
	if err := s.backend.SetEpic(ctx, b, stub, to); err != nil {
		return err
	}
	for _, c := range b.Cards {
		if !board.InEpic(c, projectName, from) {
			continue
		}
		// InEpic sees mirrors too: a mirrored card follows the rename in its
		// MIRROR entry — rewriting its home fields here would tear the card
		// out of its own column.
		if board.Mirrored(c, projectName, from) {
			if err := s.renameMirror(ctx, b, c, projectName, from, projectName, to); err != nil {
				return err
			}
			continue
		}
		if err := s.backend.SetEpic(ctx, b, c, to); err != nil {
			return err
		}
		s.logEvent(ctx, b, c, board.EventEpic, from, to)
	}
	return nil
}

// ReorderEpics persists the column order of ONE project by walking its
// epic-state cards into the given sequence (mirrors ReorderTeams). Names are
// scoped to the project, since they repeat across projects.
func (s *Service) ReorderEpics(ctx context.Context, boardID string, projectName string, epics []string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	// Where this project's block begins in the board-wide roster: moving its
	// first column to "the top" would hoist the whole project above every
	// other one, because b.Epics is one list for every project.
	prev := ""
	for _, e := range b.Epics {
		if e.Project == projectName {
			break
		}
		prev = e.ItemID
	}
	for _, name := range epics {
		col, ok := board.FindEpic(b, projectName, name)
		if !ok || col.ItemID == "" {
			continue
		}
		stub := board.Card{ItemID: col.ItemID, Title: board.EpicStateTitle, Epic: name, Project: projectName}
		if err := s.backend.MoveCard(ctx, b, stub, prev); err != nil {
			return err
		}
		prev = col.ItemID
	}
	return nil
}

// SetEpic files a card under a column — the (project, epic) pair, since the
// same epic name in another project is another column. An empty epic clears
// the filing. A nil projectName keeps the card where it is, which is what
// filing inside one project means; crossing projects names both halves. The
// column must exist: a typo must not mint a phantom one.
func (s *Service) SetEpic(ctx context.Context, boardID string, itemID, epic string, inProject *string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	projectName := card.Project
	if inProject != nil {
		projectName = *inProject
	}
	if epic == "" {
		projectName = ""
	} else if _, ok := board.FindEpic(b, projectName, epic); !ok {
		return fmt.Errorf("%w %q in project %q — add it first (add_epic / POST /epics)",
			ErrEpicNotFound, epic, projectName)
	}
	if card.Epic == epic && card.Project == projectName {
		return nil
	}
	if err := guardRoster(b, card.Team, projectName); err != nil {
		return err
	}
	// A tied card cannot be re-filed out of its process's repository — the
	// project decides where a teamless card lives, so an attach (or a
	// cleared column) can be a repository move in disguise.
	if err := tiedMoveGuard(b, card, func(a *board.Card) {
		a.Project = projectName
		a.Epic = epic
	}); err != nil {
		return err
	}
	// The mirrors ride the re-file, and the invariant holds through it:
	//   - the home moving where the card already mirrors drops that mirror
	//     (a mirror equal to the home drew the slot twice and made the ×
	//     unmirror instead of remove);
	//   - a cleared column clears the mirrors — a card outside every
	//     project has none, or DeleteEpic refuses for cards nobody sees;
	//   - the home cannot leave its mirrors' repository (G15): unmirror
	//     first, then move.
	if len(card.Mirrors) > 0 {
		if epic == "" {
			if err := s.backend.SetMirrors(ctx, b, card, nil); err != nil {
				return err
			}
			card.Mirrors = nil
		} else {
			if projectName != card.Project && !board.MirrorAllowed(b, projectName, card.Mirrors[0].Project) {
				return fmt.Errorf("%w: the card mirrors %q — unmirror it before moving to %q",
					ErrCrossDomain, card.Mirrors[0].Project, projectName)
			}
			if board.Mirrored(card, projectName, epic) {
				kept := make([]board.Placement, 0, len(card.Mirrors)-1)
				for _, m := range card.Mirrors {
					if m.Project == projectName && m.Epic == epic {
						continue
					}
					kept = append(kept, m)
				}
				if err := s.backend.SetMirrors(ctx, b, card, kept); err != nil {
					return err
				}
				card.Mirrors = kept
			}
		}
	}
	if card.Epic != epic {
		if err := s.backend.SetEpic(ctx, b, card, epic); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventEpic, card.Epic, epic)
	}
	if card.Project != projectName {
		if err := s.backend.SetProject(ctx, b, card, projectName); err != nil {
			return err
		}
	}
	// Filing a card under a column makes it a slot, and a slot's row is its
	// start date's week. A weekly-plan card has no dates of its own — its
	// place is the band and the week — so it takes the SLOT OF THAT WEEK on
	// the way: start on its Monday, end on its band's day. Without this it
	// would land in the column with no row at all and jump to another week
	// on the next full load.
	if epic != "" {
		card.Epic = epic
		if card.StartDate == "" && card.Plan != board.PlanNone && card.Week != "" {
			end := board.AddDays(card.Week, 4) // by Friday
			if card.Plan == board.PlanWed {
				end = board.AddDays(card.Week, 2)
			}
			if err := s.backend.SetStart(ctx, b, card, card.Week); err != nil {
				return err
			}
			if err := s.backend.SetDay(ctx, b, card, end); err != nil {
				return err
			}
			s.logEvent(ctx, b, card, board.EventDates, "", board.DateRange(card.Week, end))
			card.StartDate = card.Week
		}
		return s.syncSlotWeek(ctx, b, card, card.StartDate)
	}
	return nil
}

// epicNameFree rejects a column name already taken WITHIN the project
// (case-insensitively); except is the column being renamed, if any.
func epicNameFree(b board.Board, project, name, except string) error {
	for _, e := range board.EpicsOf(b, project) {
		if e.Name != except && strings.EqualFold(e.Name, name) {
			return fmt.Errorf("%w in %q: %q", ErrEpicExists, project, e.Name)
		}
	}
	return nil
}
