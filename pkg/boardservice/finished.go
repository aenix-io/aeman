package boardservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// Work finished LATE belongs to the sprint it was done in.
//
// The shape it comes in: an engineer finishes a card and never moves the bar.
// Carry Over takes it for open work and pulls it into the new sprint. Somebody
// notices and marks it 100 — and there it stands, in a sprint it was never
// worked in, counting against it.
//
// The board goes on SHOWING it, and that is deliberate: a finished card
// standing in the current sprint is the only prompt anybody gets that this
// happened, and dropping it quietly would take the prompt away and leave the
// wrong sprint credited. What clears it is a person saying where the work
// belongs, which is what this door is.
//
// It is the old DEMOTE, brought back for the one case it was right for. The
// demote used to answer every ×, and the board grew a pile of OPEN work nobody
// could see: no live view reaches a card two sprints back, and no carry-over
// takes one — three hundred cards on the production board, thirty-six with
// progress on them. A finished card is not open: nothing is waiting for it,
// and being out of the way is exactly what is wanted. That is the whole of the
// difference, and Reopen is what keeps it true.

// ErrNotFinished is work still going, sent back as if it were done. Where the
// work happened is not settled until the work is over.
var ErrNotFinished = errors.New("this work is not finished")

// ErrNoEarlierSprint is a card with nowhere to be sent: a team's first sprint
// has nothing behind it, and a card already in the earlier one is where the
// answer would put it. An option that does nothing reads as one that failed.
var ErrNoEarlierSprint = errors.New("there is no earlier sprint to send it to")

// FinishedEarlier sends a finished card back to the sprint before this one —
// its dates and the day it counts as done along with it.
//
// The dates go because the day grid draws a card by them: leaving them would
// keep the card standing on a day of the sprint it just left, which is the
// demote's own lesson (dates and all). DoneAt goes because the week's velocity
// is read off it (board.CapacityOf), and moving the card while leaving the
// credit behind would answer the question backwards.
//
// The card stays FINISHED. This says where work was done, not that it was
// undone.
func (s *Service) FinishedEarlier(ctx context.Context, boardID string, itemID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if !board.Complete(card.Stage, card.Progress) {
		return fmt.Errorf("%w: %q", ErrNotFinished, card.Title)
	}
	// A Project-board slot and a process turn carry their own dates — a slot's
	// week follows its start, a turn's is its process's record — and the
	// sprint is not what places them. Moving one would edit a plan this board
	// does not own.
	if hasColumn(card) || card.Task != "" {
		return fmt.Errorf("%w: %q", ErrNotYoursToPark, card.Title)
	}
	previous := board.PreviousSprint(b, card.Team)
	if previous == "" || card.SprintStart != board.CurrentSprint(b, card.Team) {
		return fmt.Errorf("%w: %q", ErrNoEarlierSprint, card.Title)
	}
	if err := s.backend.SetSprintStart(ctx, b, card, previous); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventSprint, card.SprintStart, previous)
	if card.StartDate != previous || card.Day != previous {
		if err := s.backend.SetStart(ctx, b, card, previous); err != nil {
			return err
		}
		if err := s.backend.SetDay(ctx, b, card, previous); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventDates,
			card.StartDate+".."+card.Day, previous+".."+previous)
	}
	if card.DoneAt != previous {
		if err := s.backend.SetDoneAt(ctx, b, card, previous); err != nil {
			return err
		}
	}
	// The pieces go with the whole: a subtask left in this sprint would split
	// the family across two, which is what syncChildrenSprint prevents
	// everywhere else, and half a finished card would go on counting here.
	for _, kid := range board.Children(b, card.ItemID) {
		if kid.SprintStart == previous {
			continue
		}
		if err := s.backend.SetSprintStart(ctx, b, kid, previous); err != nil {
			return err
		}
		s.logEvent(ctx, b, kid, board.EventSprint, kid.SprintStart, previous)
	}
	return nil
}

// pullBackFromAnEarlierSprint brings a reopened card into the current sprint.
//
// This is the rule that keeps the pile from coming back. A card sent to an
// earlier sprint is invisible to every live view — the point of sending it —
// and that is safe only while it is DONE. Reopened there it becomes exactly
// what the old demote left behind: open work, sprints back, that nobody can
// see and no carry-over will ever take. The work is being picked up again, and
// it is being picked up NOW.
//
// Only a card that is actually behind is moved: reopening one where it already
// stands must not drag it anywhere.
func (s *Service) pullBackFromAnEarlierSprint(ctx context.Context, b board.Board, card board.Card) error {
	current := board.CurrentSprint(b, card.Team)
	if current == "" || card.SprintStart == "" || card.SprintStart >= current {
		return nil
	}
	if err := s.backend.SetSprintStart(ctx, b, card, current); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventSprint, card.SprintStart, current)
	return nil
}
