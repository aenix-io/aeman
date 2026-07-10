package boardservice

import (
	"context"
	"errors"
	"strconv"

	"github.com/aenix-org/aeman/pkg/board"
)

// ErrSubtaskDepth is returned when grouping would nest deeper than one level:
// a subtask cannot get subtasks of its own, and a card with subtasks cannot
// become one (nor can a card be grouped under itself).
var ErrSubtaskDepth = errors.New("subtasks are one level deep")

// ErrParentNotFound is returned when the requested parent card is not on the
// board (or cannot hold subtasks).
var ErrParentNotFound = errors.New("parent card not found")

// ErrOpenSubtasks is returned when a card with unfinished subtasks is being
// completed — closing the parent is the human's final call, made only once
// every subtask is done.
var ErrOpenSubtasks = errors.New("the card still has unfinished subtasks")

// SetParent groups a card under a parent (parent != "") or pulls a subtask
// back out as a standalone card (parent == ""). Grouping a weekly-plan card
// hands its weekly slot to the parent — the parent replaces it in the plan —
// since a subtask is never a plan card; the subtask also joins its parent's
// sprint, so the pair always carries over together.
func (s *Service) SetParent(ctx context.Context, owner string, project int, itemID, parent string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	if parent == "" {
		if card.Parent == "" {
			return nil
		}
		if err := s.backend.SetParent(ctx, b, card, ""); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventParent, card.Parent, "")
		return s.syncParentProgress(ctx, b, card.Parent, nil, card.ItemID)
	}
	if parent == card.ItemID {
		return ErrSubtaskDepth
	}
	p, ok := findCard(b, parent)
	if !ok || p.Title == board.SprintStateTitle || card.Title == board.SprintStateTitle {
		return ErrParentNotFound
	}
	if p.Parent != "" || len(board.Children(b, card.ItemID)) > 0 {
		return ErrSubtaskDepth
	}
	// A weekly-plan card grouped under a parent hands its slot to the parent
	// (the parent replaces it in the Weekly plan); a parent already in the
	// plan keeps its own slot and the subtask's simply clears.
	if card.Plan != board.PlanNone {
		if p.Plan == board.PlanNone {
			if err := s.backend.SetPlan(ctx, b, p, card.Plan); err != nil {
				return err
			}
			if err := s.backend.SetWeek(ctx, b, p, card.Week); err != nil {
				return err
			}
		}
		if err := s.backend.SetPlan(ctx, b, card, board.PlanNone); err != nil {
			return err
		}
		if err := s.backend.SetWeek(ctx, b, card, ""); err != nil {
			return err
		}
	}
	// The subtask rides its parent's sprint from now on.
	if card.SprintStart != p.SprintStart {
		if err := s.backend.SetSprintStart(ctx, b, card, p.SprintStart); err != nil {
			return err
		}
	}
	if err := s.backend.SetParent(ctx, b, card, parent); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventParent, card.Parent, parent)
	// Moving between parents: the old parent's derived bar loses this child.
	if card.Parent != "" && card.Parent != parent {
		if err := s.syncParentProgress(ctx, b, card.Parent, nil, card.ItemID); err != nil {
			return err
		}
	}
	child := card
	child.Parent = parent
	return s.syncParentProgress(ctx, b, parent, &child, "")
}

// syncParentProgress recomputes a parent's derived progress after one of its
// subtasks changed: the mean of the children's effective progress scaled into
// 0..90 (board.DerivedProgress). changed overrides that child's stale board
// copy (and is appended when it just joined); removedID excludes a child that
// just left. A complete parent is left alone — done/100% stays the human's
// final call.
func (s *Service) syncParentProgress(ctx context.Context, b board.Board, parentID string, changed *board.Card, removedID string) error {
	p, ok := findCard(b, parentID)
	if !ok || board.Complete(p.Stage, p.Progress) {
		return nil
	}
	var children []board.Card
	seen := false
	for _, c := range b.Cards {
		if c.Parent != parentID || c.ItemID == removedID {
			continue
		}
		if changed != nil && c.ItemID == changed.ItemID {
			c = *changed
			seen = true
		}
		children = append(children, c)
	}
	if changed != nil && !seen && changed.Parent == parentID {
		children = append(children, *changed)
	}
	if len(children) == 0 {
		return nil
	}
	derived := board.DerivedProgress(children)
	if derived == p.Progress {
		return nil
	}
	if err := s.backend.SetProgress(ctx, b, p, derived); err != nil {
		return err
	}
	s.logEvent(ctx, b, p, board.EventProgress,
		strconv.Itoa(p.Progress), strconv.Itoa(derived))
	return nil
}
