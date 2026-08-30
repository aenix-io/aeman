package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrProjectInUse is returned when deleting a project that still has epic
// columns: the epics (and every card under them) would be orphaned into the
// no-project bucket, quietly detaching planned work from the plan it belongs
// to. Delete or move the epics first.
var ErrProjectInUse = errors.New("project still has epics")

// ErrProjectExists guards AddProject against doubling a project.
var ErrProjectExists = errors.New("project already exists")

// ErrProjectNotFound is filing an epic under a project that does not exist —
// a typo must not mint a phantom project. It is a rejected input (422), not an
// upstream failure.
var ErrProjectNotFound = errors.New("unknown project")

// AddProject declares a project — the Project board's top-level grouping — by
// creating its hidden project-state card, whose board position IS the order
// its chip appears in (the same mechanism as the team roster). A project may
// be created empty and filled with epics afterwards.
func (s *Service) AddProject(ctx context.Context, boardID string, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("project name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	for _, p := range b.Projects {
		if strings.EqualFold(p, name) {
			return fmt.Errorf("%w: %q", ErrProjectExists, p)
		}
	}
	_, err = s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:   board.ProjectStateTitle,
		Project: name,
	})
	return err
}

// DeleteProject removes an EMPTY project by deleting its project-state card.
// A project that still owns epic columns is protected (ErrProjectInUse).
func (s *Service) DeleteProject(ctx context.Context, boardID string, name string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if epics := board.EpicsOf(b, name); len(epics) > 0 {
		names := make([]string, 0, len(epics))
		for _, e := range epics {
			names = append(names, e.Name)
		}
		return fmt.Errorf("%w: %d (%s) still under %q — delete or move them first",
			ErrProjectInUse, len(epics), strings.Join(names, ", "), name)
	}
	itemID, ok := b.ProjectStates[name]
	if !ok || itemID == "" {
		return nil
	}
	stub := board.Card{ItemID: itemID, Title: board.ProjectStateTitle, Project: name}
	return s.backend.DeleteCard(ctx, b, stub)
}

// ReorderProjects persists the chip order by walking the project-state cards
// into the given sequence (mirrors ReorderTeams and ReorderEpics).
func (s *Service) ReorderProjects(ctx context.Context, boardID string, names []string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	prev := ""
	for _, name := range names {
		itemID, ok := b.ProjectStates[name]
		if !ok || itemID == "" {
			continue
		}
		stub := board.Card{ItemID: itemID, Title: board.ProjectStateTitle, Project: name}
		if err := s.backend.MoveCard(ctx, b, stub, prev); err != nil {
			return err
		}
		prev = itemID
	}
	return nil
}

// SetEpicProject moves a column from one project to another ("" detaches it
// into the no-project bucket, where only the all-projects view shows it). The
// column's cards are rewritten too: a card names the (project, epic) pair, so
// leaving them behind would file them under a column that no longer exists.
func (s *Service) SetEpicProject(ctx context.Context, boardID string, from, epic, to string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	col, ok := board.FindEpic(b, from, epic)
	if !ok || col.ItemID == "" {
		return fmt.Errorf("%w %q in project %q", ErrEpicNotFound, epic, from)
	}
	if from == to {
		return nil
	}
	if to != "" {
		if err := knownProject(b, to); err != nil {
			return err
		}
		// The target project may already have a column of this name, and two
		// columns with one name inside a project cannot be told apart.
		if err := epicNameFree(b, to, epic, ""); err != nil {
			return err
		}
	}
	// EVERY refusal fires before anything is written: a guard that speaks
	// after the column's stub has moved leaves the stub in one project and
	// the cards in another — half a column gone. The destination must not
	// put a card in a repository its team does not live in (G46), a card
	// mirrored INTO this column may not follow across repositories, and a
	// card whose HOME is here may not be carried away from its own mirrors
	// (G15, both directions). Unbinding to the no-project bucket under
	// mirrors is refused with the act that fixes it.
	for _, c := range b.Cards {
		if !board.InEpic(c, from, epic) {
			continue
		}
		if err := guardRoster(b, c.Team, to); err != nil {
			return err
		}
		if c.Project == from && c.Epic == epic {
			// A HOME card's process tie must not leave its repository with
			// the column: the move re-files the card, and the tie would
			// stay behind — refused before the stub is re-parented, like
			// the mirror guards below.
			if err := refileGuard(b, c, func(a *board.Card) { a.Project = to }); err != nil {
				return fmt.Errorf("%w (card %q)", err, c.Title)
			}
			// And the card must be able to FOLLOW the column. An ordinary
			// card does — its project decides where it lives — but one
			// whose file is held by a link (a subtask riding its parent, a
			// review card its original) stays behind while its project
			// field is rewritten to name a column that has left: the state
			// S4 refuses at every other door. refileGuard cannot see this
			// one, because the move changes nothing about THIS card's
			// domain and the target column does not exist yet.
			r := board.Resolver(b, "")
			after := c
			after.Project = to
			if board.DomainOf(after, r) != board.ProjectDomain(b, to) {
				return fmt.Errorf("%w: %q cannot follow this column — its file is held in another repository",
					ErrCrossDomain, c.Title)
			}
		}
		if len(c.Mirrors) == 0 {
			continue
		}
		if to == "" {
			return fmt.Errorf("%w: %q is mirrored — unmirror it before unbinding the column", ErrCrossDomain, c.Title)
		}
		if board.Mirrored(c, from, epic) && !columnLands(b, c.Project, c.Epic, to) {
			return fmt.Errorf("%w: %q mirrors this column and lives in another repository than %q",
				ErrCrossDomain, c.Title, to)
		}
		if c.Project == from && c.Epic == epic &&
			!columnLands(b, c.Mirrors[0].Project, c.Mirrors[0].Epic, to) {
			return fmt.Errorf("%w: %q mirrors %q — unmirror it before moving its column to %q",
				ErrCrossDomain, c.Title, c.Mirrors[0].Project, to)
		}
	}
	stub := board.Card{ItemID: col.ItemID, Title: board.EpicStateTitle, Epic: epic, Project: from}
	if err := s.backend.SetProject(ctx, b, stub, to); err != nil {
		return err
	}
	for _, c := range b.Cards {
		if !board.InEpic(c, from, epic) {
			continue
		}
		if board.Mirrored(c, from, epic) {
			if err := s.renameMirror(ctx, b, c, from, epic, to, epic); err != nil {
				return err
			}
			continue
		}
		if err := s.backend.SetProject(ctx, b, c, to); err != nil {
			return err
		}
	}
	return nil
}

// RenameProject renames a project in place: its own state card, the Project
// field of every column it owns, and of every card under those columns. As
// with epics the name is the reference, so all of it moves together.
func (s *Service) RenameProject(ctx context.Context, boardID string, from, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("project name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	itemID, ok := b.ProjectStates[from]
	if !ok || itemID == "" {
		return fmt.Errorf("%w %q", ErrProjectNotFound, from)
	}
	if from == to {
		return nil
	}
	for _, p := range b.Projects {
		if p != from && strings.EqualFold(p, to) {
			return fmt.Errorf("%w: %q", ErrProjectExists, p)
		}
	}
	stub := board.Card{ItemID: itemID, Title: board.ProjectStateTitle, Project: from}
	if err := s.backend.SetProject(ctx, b, stub, to); err != nil {
		return err
	}
	for _, col := range board.EpicsOf(b, from) {
		colStub := board.Card{ItemID: col.ItemID, Title: board.EpicStateTitle, Epic: col.Name, Project: from}
		if err := s.backend.SetProject(ctx, b, colStub, to); err != nil {
			return err
		}
	}
	for _, c := range b.Cards {
		// A mirror under the renamed project follows it — independently of
		// where the card's home is (issue #124: a rename must not strand a
		// reference).
		mirrored := false
		for _, m := range c.Mirrors {
			if m.Project == from {
				mirrored = true
				break
			}
		}
		if mirrored {
			if err := s.renameMirror(ctx, b, c, from, "", to, ""); err != nil {
				return err
			}
		}
		if c.Project != from {
			continue
		}
		if err := s.backend.SetProject(ctx, b, c, to); err != nil {
			return err
		}
	}
	return nil
}

// knownProject rejects a project the board does not declare.
func knownProject(b board.Board, name string) error {
	for _, p := range b.Projects {
		if p == name {
			return nil
		}
	}
	return fmt.Errorf("%w %q — add it first (add_project / POST /projects)", ErrProjectNotFound, name)
}
