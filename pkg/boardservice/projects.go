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
func (s *Service) AddProject(ctx context.Context, owner string, project int, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("project name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, owner, project)
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
func (s *Service) DeleteProject(ctx context.Context, owner string, project int, name string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	if epics := board.EpicsOf(b, name); len(epics) > 0 {
		return fmt.Errorf("%w: %d (%s) still under %q — delete or move them first",
			ErrProjectInUse, len(epics), strings.Join(epics, ", "), name)
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
func (s *Service) ReorderProjects(ctx context.Context, owner string, project int, names []string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
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

// SetEpicProject moves an epic column to another project ("" detaches it into
// the no-project bucket, where only the all-projects view shows it). The epic's
// cards ride along: a card's project is never stored, it follows its epic.
func (s *Service) SetEpicProject(ctx context.Context, owner string, project int, epic, projectName string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	itemID, ok := b.EpicStates[epic]
	if !ok || itemID == "" {
		return fmt.Errorf("%w %q", ErrEpicNotFound, epic)
	}
	if projectName != "" {
		if err := knownProject(b, projectName); err != nil {
			return err
		}
	}
	if b.EpicProjects[epic] == projectName {
		return nil
	}
	stub := board.Card{ItemID: itemID, Title: board.EpicStateTitle, Epic: epic}
	return s.backend.SetProject(ctx, b, stub, projectName)
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
