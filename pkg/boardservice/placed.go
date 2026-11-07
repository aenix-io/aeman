package boardservice

import (
	"context"

	"github.com/aenix-io/aeman/pkg/board"
)

// keepPlaced holds K1 after a write: an open TASK that the write left with no
// date, no sprint and no parent to ride — UNPLACED, on no board at all — is
// parked in its team's backlog in the same request (docs/design/backlog.md,
// K1, K3).
//
// It is the "back to the backlog" fallback, and only that: a PLAIN park. The
// person, the band and the rank stay as they are; SetBacklog's own emptying
// (no person, the planned band, the top of the backlog) belongs to the
// gesture that asks for it, not to a write that happened to clear a date.
//
// c is the card as the write LEFT it, carried in hand rather than re-read:
// inside a gitstore scope a staged write is invisible to a bare store, and a
// re-read there would judge the card as it stood before (setParentOf).
// subtask is whether it still rides a TASK parent — a subtask is placed by
// its parent and never parked on its own.
//
// Every door that can take a task's last placement calls it: SetDates,
// SetStart, SetDay, SetSprintStart (a date or a sprint cleared), the
// pull-out of a group and a hierarchy move (ungroupWith, linkInHierarchy),
// the release of a deleted parent's subtasks (freeChildren), and the writes
// that make finished work open again (SetProgress, SetStage, SetInProgress,
// Reopen, an open subtask un-doing its parent in syncParentProgress, a review
// card sending its original back to review in syncReviewLink).
func (s *Service) keepPlaced(ctx context.Context, b board.Board, c board.Card, subtask bool) error {
	if err := s.settleTodo(ctx, b, c, subtask); err != nil {
		return err
	}
	if !board.Unplaced(c, subtask) {
		return nil
	}
	if err := s.backend.SetBacklog(ctx, b, c, true); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventBacklog, "", backlogLabel(board.Card{Team: c.Team, Parked: true}))
	return nil
}

// settleTodo drops the TO DO (K15) of a card a write has left where the flag
// means nothing — finished, or riding a task — in the same request as that
// write, from the card as the write left it (c): no second read of the
// board. keepPlaced runs it for every door that can finish a task (subtasks
// never finish their parent: the derived bar stops at 90), setParentOf for
// a task grouped under a task; taking a task into work clears it in
// takenIntoWork.
func (s *Service) settleTodo(ctx context.Context, b board.Board, c board.Card, subtask bool) error {
	if !c.Todo || (!subtask && !board.Complete(c.Stage, c.Progress)) {
		return nil
	}
	return s.backend.SetTodo(ctx, b, c, false)
}
