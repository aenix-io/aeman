package boardservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// The ORDERING ACTIONS (docs/design/ordering.md). Every one is a planner
// handed to Backend.Reorder: the keys are worked out over the merged board
// as it stands at the write, and the backend writes exactly those — one
// action, one commit per repository touched.

// MoveResult is what an ordering gesture did: how many cards it re-keyed and
// the keys themselves (what an Undo puts back).
type MoveResult struct {
	Moved   int
	Changes []board.RankChange
}

// ErrNoSuchAction is an Undo of an action the undo window does not hold.
var ErrNoSuchAction = errors.New("no such ordering action in the last 24 hours")

// ErrNotUnderAnEpic is a reorder-in-epic of a card that is under no epic.
var ErrNotUnderAnEpic = errors.New("the card is under no epic")

// ActionRanker is a backend that can say what an ordering action changed —
// from its own record or from the action's commits.
type ActionRanker interface {
	ActionRanks(ctx context.Context, actionID string) ([]board.RankChange, time.Time, error)
}

// Move is the move gesture (Q4, Q5, Q7): a card put right after spot.After,
// or right before spot.Before (the top of the board with neither). On a
// task, a goal or an initiative it writes the card's own key. On an EPIC it
// is the epic move — the same operation from the Strategy board and the
// Backlog: the epic takes its new place in the epic order and its parked
// tasks are gathered into one block per team, in one action.
func (s *Service) Move(ctx context.Context, boardID, itemID string, spot board.Spot) (MoveResult, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return MoveResult{}, err
	}
	after := spot.After
	if spot.Before != "" {
		after = ""
		for _, c := range b.Cards {
			if c.ItemID == spot.Before {
				break
			}
			if c.ItemID != itemID && !board.IsStateTitle(c.Title) {
				after = c.ItemID
			}
		}
	}
	if card.Type == board.TypeEpic {
		changes, err := s.backend.Reorder(ctx, b, func(cur board.Board) ([]board.RankChange, error) {
			return board.MoveEpicPlan(cur, itemID, after)
		})
		return MoveResult{Moved: len(changes), Changes: changes}, err
	}
	if err := s.backend.MoveCard(ctx, b, card, after); err != nil {
		return MoveResult{}, err
	}
	return MoveResult{Moved: 1}, nil
}

// MoveUnder is a Strategy drop under another parent (Q6, Q7): the card is
// re-parented through the parent door — every rule of SetParent holds — and
// then placed: at the anchor when the drop named one (an epic dropped among
// another initiative's epics: re-parent and epic move), else a task in its
// team's queue by Place — after the new epic's last parked task, or the
// epic's best gap when it has none. One request, one action.
func (s *Service) MoveUnder(ctx context.Context, boardID, itemID, parent string, spot board.Spot) (MoveResult, error) {
	_, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return MoveResult{}, err
	}
	if card.Parent != parent {
		if err := s.SetParent(ctx, boardID, itemID, parent); err != nil {
			return MoveResult{}, err
		}
	}
	if spot.After != "" || spot.Before != "" {
		return s.Move(ctx, boardID, itemID, spot)
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return MoveResult{}, err
	}
	card.Parent = parent
	if !queued(b, card) {
		return MoveResult{}, nil
	}
	changes, err := s.placeIn(ctx, b, card, board.PlacedTeam(b, card))
	return MoveResult{Moved: len(changes), Changes: changes}, err
}

// ReorderInEpic is a task moved WITHIN its epic on the Strategy board (Q6):
// the epic's parked tasks of the task's team permute over the slots they
// hold in the queue; a task in work or done takes an ordinary key between
// its drawn neighbours.
func (s *Service) ReorderInEpic(ctx context.Context, boardID, itemID, after, before string) (MoveResult, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return MoveResult{}, err
	}
	if board.NewIndex(b).EpicOf(card) == "" {
		return MoveResult{}, fmt.Errorf("%w: %q", ErrNotUnderAnEpic, card.Title)
	}
	changes, err := s.backend.Reorder(ctx, b, func(cur board.Board) ([]board.RankChange, error) {
		return board.ReorderInEpicPlan(cur, itemID, after, before)
	})
	return MoveResult{Moved: len(changes), Changes: changes}, err
}

// UndoResult is what an Undo did: the cards it put back, and the ones it
// left because they were moved again since (compare-and-swap).
type UndoResult struct {
	Reverted []string
	Skipped  []string
}

// UndoAction reverts an ordering action (Q13): every card whose key is
// still the one the action wrote gets its old key back; a card moved since
// is skipped and reported. The changes come from the backend — its own
// record, or the action's commits — within the undo window.
func (s *Service) UndoAction(ctx context.Context, boardID, actionID string) (UndoResult, error) {
	ar, ok := s.backend.(ActionRanker)
	if !ok {
		return UndoResult{}, ErrNoSuchAction
	}
	record, _, err := ar.ActionRanks(ctx, actionID)
	if err != nil || len(record) == 0 {
		return UndoResult{}, fmt.Errorf("%w: %s", ErrNoSuchAction, actionID)
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return UndoResult{}, err
	}
	var res UndoResult
	_, err = s.backend.Reorder(ctx, b, func(cur board.Board) ([]board.RankChange, error) {
		res = UndoResult{}
		keys := make(map[string]string, len(cur.Cards))
		for _, c := range cur.Cards {
			keys[c.ItemID] = c.Rank
		}
		var plan []board.RankChange
		for _, ch := range record {
			k, present := keys[ch.ID]
			if !present || k != ch.After {
				res.Skipped = append(res.Skipped, ch.ID)
				continue
			}
			plan = append(plan, board.RankChange{ID: ch.ID, Before: ch.After, After: ch.Before})
			res.Reverted = append(res.Reverted, ch.ID)
		}
		return plan, nil
	})
	return res, err
}

// Normalize places every open parked task with no valid key (Q11) — a
// direct writer's file — by Place, as one action. It is what the server runs
// at start-up and after a fetch brought other writers' commits in.
func (s *Service) Normalize(ctx context.Context, boardID string) (int, error) {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return 0, err
	}
	changes, err := s.backend.Reorder(ctx, b, board.Normalize)
	return len(changes), err
}

// queued reports whether a card, as it will be, stands in its team's queue.
func queued(b board.Board, c board.Card) bool {
	ix := board.NewIndex(b)
	return c.Parked && c.ReviewOf == "" && !board.IsPlanning(c) && !ix.IsSubtask(c) &&
		!board.Complete(c.Stage, c.Progress)
}

// placeIn puts a task in team's queue at its default place (Q3).
func (s *Service) placeIn(ctx context.Context, b board.Board, c board.Card, team string) ([]board.RankChange, error) {
	return s.backend.Reorder(ctx, b, func(cur board.Board) ([]board.RankChange, error) {
		return board.Place(cur, c, team)
	})
}

// placeReturned puts a task coming back to its team's queue (Q8): its old
// key while still inside its epic's area, else Place.
func (s *Service) placeReturned(ctx context.Context, b board.Board, c board.Card) error {
	if !queued(b, c) {
		return nil
	}
	_, err := s.backend.Reorder(ctx, b, func(cur board.Board) ([]board.RankChange, error) {
		if k, ok := board.Find(cur, c.ItemID); ok {
			c.Rank = k.Rank
		}
		return board.ReturnChanges(cur, c, board.PlacedTeam(cur, c))
	})
	return err
}
