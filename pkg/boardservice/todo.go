package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
// board.UnassignTargets) — or, given uids, for exactly those of them: the
// confirm counted the tasks the reader could see, and a Roadmap filter shows
// fewer than the board holds. Every task is checked before anything is
// written — reachable from the row, and the actor's to write — so a gesture
// is applied whole or not at all, off one read of the board.

// ErrNotParkedWork is TO DO on a task that is not parked open work: the flag
// orders a backlog, and the task is in none.
var ErrNotParkedWork = errors.New("TO DO marks a task waiting in its backlog: this one is not")

// ErrNotInWork is UNASSIGNED on a task that is not work in hand: there is
// nothing to take out of work.
var ErrNotInWork = errors.New("this task is not in work: there is nothing to take out of it")

// ErrNotATarget is a uid the gesture cannot reach from the row it was made
// on: not under it, or not of the kind the gesture acts on.
var ErrNotATarget = errors.New("this task is not one the gesture reaches from that row")

// gestureTargets is the tasks a status gesture acts on: all it reaches, or —
// given uids — exactly those, each of which it must reach.
func gestureTargets(reach []board.Card, uids []string) ([]board.Card, error) {
	if uids == nil {
		return reach, nil
	}
	byID := make(map[string]board.Card, len(reach))
	for _, c := range reach {
		byID[c.ItemID] = c
	}
	out := make([]board.Card, 0, len(uids))
	seen := map[string]bool{}
	for _, id := range uids {
		c, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrNotATarget, id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, c)
		}
	}
	return out, nil
}

// checkWrites refuses the whole gesture when any task is not the actor's to
// write, naming how many and which, before anything is written.
func (s *Service) checkWrites(ctx context.Context, b board.Board, targets []board.Card) error {
	wc, ok := s.backend.(WriteChecker)
	if !ok {
		return nil
	}
	var refused []string
	for _, t := range targets {
		if err := wc.CheckWrite(ctx, b, t); err != nil {
			if !errors.Is(err, ErrForbidden) {
				return err
			}
			refused = append(refused, fmt.Sprintf("%q", t.Title))
		}
	}
	if len(refused) > 0 {
		return fmt.Errorf("%w: %d of %d tasks are not yours to write (%s); nothing was changed",
			ErrForbidden, len(refused), len(targets), strings.Join(refused, ", "))
	}
	return nil
}

// SetTodo marks the card's parked work TO DO, or clears the mark, and says
// how many tasks it changed. A task already marked the way asked is not
// counted, so asking twice changes nothing the second time. On a task that
// is not parked open work it is refused (ErrNotParkedWork); on a plan item
// with nothing parked under it there is simply nothing to change. uids, when
// given, names exactly the tasks to change (nil: every one it reaches).
func (s *Service) SetTodo(ctx context.Context, boardID string, itemID string, todo bool, uids []string) (int, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return 0, err
	}
	reach := board.NewIndex(b).ToDoTargets(card)
	if !board.IsPlanning(card) && len(reach) == 0 {
		return 0, fmt.Errorf("%w: %q", ErrNotParkedWork, card.Title)
	}
	targets, err := gestureTargets(reach, uids)
	if err != nil {
		return 0, err
	}
	var change []board.Card
	for _, t := range targets {
		if t.Todo != todo {
			change = append(change, t)
		}
	}
	if err := s.checkWrites(ctx, b, change); err != nil {
		return 0, err
	}
	for i, t := range change {
		if err := s.backend.SetTodo(ctx, b, t, todo); err != nil {
			return i, err
		}
	}
	return len(change), nil
}

// UnassignWork takes the card's work in hand back to its team's backlog and
// says how many tasks it moved. Each goes the ×'s unassign way (SetBacklog):
// nobody on it, no day, no sprint, the review it asked for withdrawn, parked
// where it stands in the board's order. Finished work is never touched. On a
// task that is not work in hand it is refused (ErrNotInWork); on a plan item
// with none under it nothing moves. uids, when given, names exactly the
// tasks to move (nil: every one it reaches).
func (s *Service) UnassignWork(ctx context.Context, boardID string, itemID string, uids []string) (int, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return 0, err
	}
	reach := board.NewIndex(b).UnassignTargets(card)
	if !board.IsPlanning(card) && len(reach) == 0 {
		return 0, fmt.Errorf("%w: %q", ErrNotInWork, card.Title)
	}
	targets, err := gestureTargets(reach, uids)
	if err != nil {
		return 0, err
	}
	if err := s.checkWrites(ctx, b, targets); err != nil {
		return 0, err
	}
	for i, t := range targets {
		if err := s.setBacklogOn(ctx, b, t, true); err != nil {
			return i, err
		}
	}
	return len(targets), nil
}
