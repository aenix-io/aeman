package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Strategy board's badge menu (docs/design/backlog.md, "UNASSIGNED"): the
// answer a person gives about a row.
//
//   - UNASSIGNED (K16) takes work OUT of hand: back to its team's backlog,
//     nobody's, undated, out of every sprint — the ×'s unassign (K3) — for a
//     task, or every task in work under a plan item at any depth
//     (board.UnassignTargets); given uids, for exactly those of them: the
//     confirm counted the tasks the reader could see, and a Strategy filter
//     shows fewer than the board holds. Every task is checked before
//     anything is written — reachable from the row, and the actor's to
//     write — so it is applied whole or not at all, off one read of the
//     board.

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

// UnassignPreview names the tasks UnassignWork would take from the card,
// writing nothing — what the confirm counts. The reader's Strategy may be
// filtered and hold fewer of them; only the whole board says what makes the
// plan item IN PROGRESS. Refused like UnassignWork on a task not in work.
func (s *Service) UnassignPreview(ctx context.Context, boardID string, itemID string) ([]string, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return nil, err
	}
	reach := board.NewIndex(b).UnassignTargets(card)
	if !board.IsPlanning(card) && len(reach) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNotInWork, card.Title)
	}
	out := make([]string, 0, len(reach))
	for _, t := range reach {
		out = append(out, t.ItemID)
	}
	return out, nil
}

// UnassignWork takes the card's work in hand back to its team's backlog and
// names the tasks it moved (their uids, in board order). Each goes the ×'s unassign way (SetBacklog):
// nobody on it, no day, no sprint, the review it asked for withdrawn, parked
// where it stands in the board's order. Finished work is never touched. On a
// task that is not work in hand it is refused (ErrNotInWork); on a plan item
// with none under it nothing moves. uids, when given, names exactly the
// tasks to move (nil: every one it reaches).
func (s *Service) UnassignWork(ctx context.Context, boardID string, itemID string, uids []string) ([]string, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return nil, err
	}
	reach := board.NewIndex(b).UnassignTargets(card)
	if !board.IsPlanning(card) && len(reach) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNotInWork, card.Title)
	}
	targets, err := gestureTargets(reach, uids)
	if err != nil {
		return nil, err
	}
	if err := s.checkWrites(ctx, b, targets); err != nil {
		return nil, err
	}
	done := make([]string, 0, len(targets))
	for _, t := range targets {
		if err := s.setBacklogOn(ctx, b, t, true); err != nil {
			return done, err
		}
		done = append(done, t.ItemID)
	}
	return done, nil
}
