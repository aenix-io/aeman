package boardservice

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Roadmap's status menu (docs/design/backlog.md, "TO DO and
// UNASSIGNED"): two answers a person gives about the work under a row.
//
//   - TO DO (K15) marks parked work READY: it stands at the top of its
//     backlog, in the Roadmap's order among the other TO DO tasks.
//   - UNASSIGNED (K16) takes work OUT of hand: back to its team's backlog,
//     nobody's, undated, out of every sprint — the ×'s unassign (K3).
//
// On a task each answers for the task itself; on a plan item for every task
// under it at any depth it can mean something for (board.ToDoTargets,
// board.UnassignTargets). Each is one request, so one commit.

// ErrNotParkedWork is TO DO on a task that is not parked open work: the flag
// orders a backlog, and the task is in none.
var ErrNotParkedWork = errors.New("TO DO marks a task waiting in its backlog: this one is not")

// ErrNotInWork is UNASSIGNED on a task that is not work in hand: there is
// nothing to take out of work.
var ErrNotInWork = errors.New("this task is not in work: there is nothing to take out of it")

// SetTodo marks the card's parked work TO DO, or clears the mark, and says
// how many tasks it changed. A task already marked the way asked is not
// counted, so asking twice changes nothing the second time. On a task that
// is not parked open work it is refused (ErrNotParkedWork); on a plan item
// with nothing parked under it there is simply nothing to change.
func (s *Service) SetTodo(ctx context.Context, boardID string, itemID string, todo bool) (int, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return 0, err
	}
	targets := board.NewIndex(b).ToDoTargets(card)
	if !board.IsPlanning(card) && len(targets) == 0 {
		return 0, fmt.Errorf("%w: %q", ErrNotParkedWork, card.Title)
	}
	n := 0
	for _, t := range targets {
		if t.Todo == todo {
			continue
		}
		if err := s.backend.SetTodo(ctx, b, t, todo); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// UnassignWork takes the card's work in hand back to its team's backlog and
// says how many tasks it moved. Each goes the ×'s unassign way (SetBacklog):
// nobody on it, no day, no sprint, the review it asked for withdrawn, at the
// top of the backlog's own order among its siblings. They are moved last to
// first, so the ones it moves keep their order among themselves. Finished
// work is never touched. On a task that is not work in hand it is refused
// (ErrNotInWork); on a plan item with none under it nothing moves.
func (s *Service) UnassignWork(ctx context.Context, boardID string, itemID string) (int, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return 0, err
	}
	targets := board.NewIndex(b).UnassignTargets(card)
	if !board.IsPlanning(card) && len(targets) == 0 {
		return 0, fmt.Errorf("%w: %q", ErrNotInWork, card.Title)
	}
	n := 0
	for _, t := range slices.Backward(targets) {
		if err := s.SetBacklog(ctx, boardID, t.ItemID, true); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// clearTodoOnceFinished drops the TO DO of a task that is finished now: a
// task reopened later is not marked ready by a flag nobody saw it carry.
func (s *Service) clearTodoOnceFinished(ctx context.Context, boardID string, itemID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil || !card.Todo || !board.Complete(card.Stage, card.Progress) {
		return err
	}
	return s.backend.SetTodo(ctx, b, card, false)
}
