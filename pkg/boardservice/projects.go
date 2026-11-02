package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrProjectInUse is returned when deleting a project that still has scope
// columns: the scopes (and every card under them) would be orphaned into the
// no-project bucket, quietly detaching planned work from the plan it belongs
// to. Delete or move the scopes first.
var ErrProjectInUse = errors.New("project still has scopes")

// ErrProjectExists guards AddProject against doubling a project.
var ErrProjectExists = errors.New("project already exists")

// ErrProjectNotFound is filing an scope under a project that does not exist —
// a typo must not mint a phantom project. It is a rejected input (422), not an
// upstream failure.
var ErrProjectNotFound = errors.New("unknown project")

// AddProject declares a project — the Project board's top-level grouping — by
// creating its hidden project-state card, whose board position IS the order
// its chip appears in (the same mechanism as the team roster). A project may
// be created empty and filled with scopes afterwards.
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
// A project that still owns scope columns is protected (ErrProjectInUse).
func (s *Service) DeleteProject(ctx context.Context, boardID string, name string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if scopes := board.ScopesOf(b, name); len(scopes) > 0 {
		names := make([]string, 0, len(scopes))
		for _, e := range scopes {
			names = append(names, e.Name)
		}
		return fmt.Errorf("%w: %d (%s) still under %q — delete or move them first",
			ErrProjectInUse, len(scopes), strings.Join(names, ", "), name)
	}
	itemID, ok := b.ProjectStates[name]
	if !ok || itemID == "" {
		return nil
	}
	stub := board.Card{ItemID: itemID, Title: board.ProjectStateTitle, Project: name}
	return s.backend.DeleteCard(ctx, b, stub)
}

// ReorderProjects persists the chip order by walking the project-state cards
// into the given sequence (mirrors ReorderTeams and ReorderScopes).
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

// SetScopeProject moves a column from one project to another ("" detaches it
// into the no-project bucket, where only the all-projects view shows it). The
// column's cards are rewritten too: a card names the (project, scope) pair, so
// leaving them behind would file them under a column that no longer exists.
func (s *Service) SetScopeProject(ctx context.Context, boardID string, from, scope, to string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	col, ok := board.FindScope(b, from, scope)
	if !ok || col.ItemID == "" {
		return fmt.Errorf("%w %q in project %q", ErrScopeNotFound, scope, from)
	}
	if from == to {
		return nil
	}
	if to != "" {
		if err := knownProject(b, to); err != nil {
			return err
		}
	}
	// The destination may already have a column of this name, and two
	// columns with one name in one project cannot be told apart. The
	// NO-PROJECT bucket is a destination like any other here: NewBoard
	// dedups columns by the (project, scope) pair, so an unbind onto a name
	// already there merges two stubs under one entry — and the losing
	// stub's cards are then refused at every door, with nothing in the
	// product to free them.
	if err := scopeNameFree(b, to, scope, ""); err != nil {
		return err
	}
	// A column cannot change REPOSITORY. Its stub is handed straight back to
	// the backend that holds it (gitstore isStub), so a move to a project of
	// another repository would leave the column declared where it was while
	// its project lives elsewhere — and every ordinary card in it, whose
	// project decides where IT lives, re-filed away from the column it
	// stands in: the state G57 forbids, and a trap, since from then on every
	// guard refuses that card and no gesture frees it. Unbinding (to = "")
	// moves nothing and is judged below, card by card.
	colDomain, _ := board.ColumnDomain(b, from, scope) // declared: FindScope said so
	if to != "" && colDomain != board.ProjectDomain(b, to) {
		return fmt.Errorf("%w: %q is in another repository than this column, which cannot follow it",
			ErrCrossDomain, to)
	}

	// EVERY refusal fires before anything is written: a guard that speaks
	// after the column's stub has moved leaves the stub in one project and
	// the cards in another — half a column gone. The destination must not
	// put a card in a repository its team does not live in (G46), and every
	// card standing here must be able to FOLLOW: an ordinary card does (its
	// project decides where it lives), one whose file a link holds does not.
	// The cross-repository question is settled above, for the column as a
	// whole.
	for _, c := range b.Cards {
		if !board.InScope(c, from, scope) {
			continue
		}
		if err := guardRoster(b, c.Team, to); err != nil {
			return err
		}
		// The columns of the cards that FOLLOW this one must not be left
		// behind by the move: refused before the stub is re-parented.
		if err := refileGuard(b, c, func(a *board.Card) { a.Project = to }); err != nil {
			return fmt.Errorf("%w (card %q)", err, c.Title)
		}
		// And the card must be able to FOLLOW the column. An ordinary card
		// does — its project decides where it lives — but one whose file is
		// held by a link (a subtask riding its parent, a review card its
		// original) stays behind while its project field is rewritten to
		// name a column that has left: the state G57 refuses at every other
		// door. refileGuard cannot see this one, because the move changes
		// nothing about THIS card's domain and the target column does not
		// exist yet.
		after := c
		after.Project = to
		if board.HomeDomain(b, after) != colDomain {
			return fmt.Errorf("%w: %q would leave this column's repository — its team or its links decide where it lives, not its project",
				ErrCrossDomain, c.Title)
		}
	}
	stub := board.Card{ItemID: col.ItemID, Title: board.ScopeStateTitle, Scope: scope, Project: from}
	if err := s.backend.SetProject(ctx, b, stub, to); err != nil {
		return err
	}
	for _, c := range b.Cards {
		if !board.InScope(c, from, scope) {
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
// with scopes the name is the reference, so all of it moves together.
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
	for _, col := range board.ScopesOf(b, from) {
		colStub := board.Card{ItemID: col.ItemID, Title: board.ScopeStateTitle, Scope: col.Name, Project: from}
		if err := s.backend.SetProject(ctx, b, colStub, to); err != nil {
			return err
		}
	}
	for _, c := range b.Cards {
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
