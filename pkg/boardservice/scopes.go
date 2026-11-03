package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrScopeInUse is returned when deleting an scope that still has cards filed
// under it: silently orphaning them would hide planned work, exactly the
// failure mode the Project board exists to prevent.
var ErrScopeInUse = errors.New("scope still has cards")

// ErrScopeExists guards against doubling a column WITHIN a project. Scope names
// repeat across projects on purpose — every project has its own "Docs".
var ErrScopeExists = errors.New("scope already exists")

// ErrProjectCard is asking a PROJECT CARD — a card in a Scope column — to be
// something it is not (H4, docs/design/hierarchy.md). It is the Project
// board's plan and stands there alone: no team, no owner, no week of the
// weekly plan, no parent and no subtasks, never sent to review. Giving it
// any of those, or filing a card that carries one into a column, is refused:
// a rejected input (422), not an upstream failure. The project card's dates
// are its row; those it keeps.
var ErrProjectCard = errors.New("a project card stands in its column alone: no team, no owner, no band, no size, no week, no task parent, no review")

// ErrScopeNotFound is filing a card under a column that does not exist — a
// typo must not mint a phantom column the way a stray team value used to
// mint a team. It is a rejected input (422), not an upstream failure.
var ErrScopeNotFound = errors.New("unknown scope")

// AddScope declares a new column by creating its hidden scope-state card (the
// exact team-roster mechanism: the card's position IS the column order).
//
// A column normally belongs to a project, and naming one is how it ends up
// somewhere people look. An empty project is allowed but deliberate: it files
// the column in the no-project bucket, which the board shows behind its own
// chip. A project that does not exist is still refused — that is a typo.
func (s *Service) AddScope(ctx context.Context, boardID string, name, projectName string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("scope name must not be empty")
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
	if err := scopeNameFree(b, projectName, name, ""); err != nil {
		return err
	}
	_, err = s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:   board.ScopeStateTitle,
		Scope:   name,
		Project: projectName,
	})
	return err
}

// DeleteScope removes an empty column by deleting its scope-state card. A column
// still referenced by cards is protected (ErrScopeInUse).
func (s *Service) DeleteScope(ctx context.Context, boardID string, name, projectName string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	inUse := 0
	for _, c := range b.Cards {
		if board.InScope(c, projectName, name) {
			inUse++
		}
	}
	if inUse > 0 {
		return fmt.Errorf("%w: %d card(s) still under %q — move or clear them first", ErrScopeInUse, inUse, name)
	}
	col, ok := board.FindScope(b, projectName, name)
	if !ok || col.ItemID == "" {
		return nil
	}
	stub := board.Card{ItemID: col.ItemID, Title: board.ScopeStateTitle, Scope: name, Project: projectName}
	return s.backend.DeleteCard(ctx, b, stub)
}

// RenameScope renames a column in place: its scope-state card and every card
// filed under it. The name IS the reference (cards store the text, not an id),
// so the two must move together or the cards would point at a column that no
// longer exists.
func (s *Service) RenameScope(ctx context.Context, boardID string, projectName, from, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("scope name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	col, ok := board.FindScope(b, projectName, from)
	if !ok || col.ItemID == "" {
		return fmt.Errorf("%w %q", ErrScopeNotFound, from)
	}
	if from == to {
		return nil
	}
	if err := scopeNameFree(b, projectName, to, from); err != nil {
		return err
	}
	stub := board.Card{ItemID: col.ItemID, Title: board.ScopeStateTitle, Scope: from, Project: projectName}
	if err := s.backend.SetScope(ctx, b, stub, to); err != nil {
		return err
	}
	for _, c := range b.Cards {
		if !board.InScope(c, projectName, from) {
			continue
		}
		if err := s.backend.SetScope(ctx, b, c, to); err != nil {
			return err
		}
		s.logEvent(ctx, b, c, board.EventScope, from, to)
	}
	return nil
}

// ReorderScopes persists the column order of ONE project by walking its
// scope-state cards into the given sequence (mirrors ReorderTeams). Names are
// scoped to the project, since they repeat across projects.
func (s *Service) ReorderScopes(ctx context.Context, boardID string, projectName string, scopes []string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	// Where this project's block begins in the board-wide roster: moving its
	// first column to "the top" would hoist the whole project above every
	// other one, because b.Scopes is one list for every project.
	prev := ""
	for _, e := range b.Scopes {
		if e.Project == projectName {
			break
		}
		prev = e.ItemID
	}
	for _, name := range scopes {
		col, ok := board.FindScope(b, projectName, name)
		if !ok || col.ItemID == "" {
			continue
		}
		stub := board.Card{ItemID: col.ItemID, Title: board.ScopeStateTitle, Scope: name, Project: projectName}
		if err := s.backend.MoveCard(ctx, b, stub, prev); err != nil {
			return err
		}
		prev = col.ItemID
	}
	return nil
}

// SetScope files a card under a column — the (project, scope) pair, since the
// same scope name in another project is another column. An empty scope clears
// the filing. A nil projectName keeps the card where it is, which is what
// filing inside one project means; crossing projects names both halves. The
// column must exist: a typo must not mint a phantom one.
func (s *Service) SetScope(ctx context.Context, boardID string, itemID, scope string, inProject *string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if scope != "" {
		if err := notAPlanningCard(card, "column"); err != nil {
			return err
		}
	}
	// A project card lives and dies in its column (H4): taking the column
	// AWAY would leave a card with no team, no owner, no band, no plan and
	// no column — on no board anyone can open — so it is refused, and the
	// person deletes the card instead. Another column is a move, and free.
	if scope == "" && card.Scope != "" {
		return fmt.Errorf("%w: %q stands in its column — delete it instead of taking the column away", ErrProjectCard, card.Title)
	}
	projectName := card.Project
	if inProject != nil {
		projectName = *inProject
	}
	// An empty scope here is a card in NO column (taking one out is refused
	// above): the project it names is a label, set — or cleared — by itself.
	// It was once read as "out of the column, and leave the project behind",
	// which turned a change of label into clearing it.
	if scope != "" {
		if _, ok := board.FindScope(b, projectName, scope); !ok {
			return fmt.Errorf("%w %q in project %q — add it first (add_scope / POST /scopes)",
				ErrScopeNotFound, scope, projectName)
		}
	}
	if card.Scope == scope && card.Project == projectName {
		return nil
	}
	// A label on a task that HAS a team: the team and the project must come
	// from one repository, the rule a create holds them to — the domain rule
	// files a card by its project before its team.
	if scope == "" {
		if err := guardRoster(b, card.Team, projectName); err != nil {
			return err
		}
	}
	// A card in a column is a PROJECT CARD and stands there alone (H4): one
	// that carries a team, an owner, a parent or a review link is somebody's
	// work in hand, and filing it into a column would make one card answer
	// two questions. The person takes those off first — or, more often,
	// makes a card in the column and links the work under the hierarchy.
	if scope != "" {
		if err := projectCardCannotCarry(b, card, "into a column"); err != nil {
			return err
		}
	}
	// No team/project pair to guard here: a card filed INTO a column has no
	// team (refused just above, H4), and one taken OUT of its column leaves
	// its project behind too — a Scope and a project card know no team.
	// The card's own column must name the repository that will hold the
	// file: a project card lives where its project lives, and a column of
	// the no-project bucket declared elsewhere cannot hold it.
	if err := refileGuard(b, card, func(a *board.Card) {
		a.Project = projectName
		a.Scope = scope
	}); err != nil {
		return err
	}
	if card.Scope != scope {
		if err := s.backend.SetScope(ctx, b, card, scope); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventScope, card.Scope, scope)
	}
	if card.Project != projectName {
		if err := s.backend.SetProject(ctx, b, card, projectName); err != nil {
			return err
		}
	}
	if scope == "" {
		return nil
	}
	// Filed into a column, the card leaves the places it was in. Its DATES
	// are its row on the Project board and it keeps them; the SPRINT and the
	// BACKLOG go, because neither a team's day board nor a backlog draws a
	// project card (H4). (A task has no week to convert any more, K7.)
	card.Scope = scope
	if board.InBacklog(card) {
		if err := s.backend.SetBacklog(ctx, b, card, false); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventBacklog, backlogLabel(card), "")
	}
	if card.SprintStart != "" {
		if err := s.backend.SetSprintStart(ctx, b, card, ""); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventSprint, card.SprintStart, "")
	}
	// And its BAND and SIZE go the same way: a project card has neither
	// (H4), and filing is the act that makes it one — it is not refused for
	// carrying what the column does not keep.
	if card.Zone != board.ZoneNone {
		if err := s.backend.SetZone(ctx, b, card, board.ZoneNone); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventZone, string(card.Zone), "")
	}
	if card.Size != "" {
		if err := s.backend.SetSize(ctx, b, card, ""); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventSize, string(card.Size), "")
	}
	return nil
}

// projectCardCannotCarry is the H4 admission in one place: a team, an owner,
// a TASK parent or a review link on a card that stands — or is about to
// stand — in a column. A planning parent is not one of them: a project card
// may sit under an epic, an initiative or a goal (hierarchy.md); it is never
// a subtask. `where` finishes the sentence for the door that asked.
func projectCardCannotCarry(b board.Board, c board.Card, where string) error {
	switch {
	case c.Team != "":
		return fmt.Errorf("%w: %q carries a team %s", ErrProjectCard, c.Title, where)
	case len(c.Assignees) > 0:
		return fmt.Errorf("%w: %q has an owner %s", ErrProjectCard, c.Title, where)
	case board.IsSubtask(b, c):
		return fmt.Errorf("%w: %q is a subtask %s", ErrProjectCard, c.Title, where)
	case len(board.Children(b, c.ItemID)) > 0:
		// Its pieces would be subtasks of a card no day board draws, hidden
		// with delete the only gesture left on them.
		return fmt.Errorf("%w: %q has subtasks %s", ErrProjectCard, c.Title, where)
	case c.ReviewOf != "":
		return fmt.Errorf("%w: %q is a review %s", ErrProjectCard, c.Title, where)
	}
	return nil
}

// scopeNameFree rejects a column name already taken WITHIN the project
// (case-insensitively); except is the column being renamed, if any.
func scopeNameFree(b board.Board, project, name, except string) error {
	for _, e := range board.ScopesOf(b, project) {
		if e.Name != except && strings.EqualFold(e.Name, name) {
			return fmt.Errorf("%w in %q: %q", ErrScopeExists, project, e.Name)
		}
	}
	return nil
}
