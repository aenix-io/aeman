package boardservice

import (
	"context"

	"github.com/aenix-io/aeman/pkg/board"
)

// requestReview makes the review card of a task put on review WITHOUT a
// reviewer (K10, docs/design/backlog.md "Reviews"): «review: <title>», of
// the task's team, with nobody on it, parked in that team's backlog — the
// "needs a reviewer" item, ranked at the TOP of the board's order so it shows
// first in its backlog until somebody drags it elsewhere. It joins
// no sprint and stands on no day until somebody takes it (takeReview). A task
// that already has a review card gets no second one: sendToReview, the
// reassignment and the re-review reactivation are its doors.
func (s *Service) requestReview(ctx context.Context, b board.Board, card board.Card) error {
	if _, ok := findReviewCard(b, card.ItemID); ok {
		return nil
	}
	created, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:    "review: " + card.Title,
		Zone:     board.ZoneGray,
		Team:     card.Team,
		ReviewOf: card.ItemID,
		Parked:   true,
	})
	if err != nil {
		return err
	}
	// Its default place is the TOP of the board's order: it shows first in
	// its backlog, asked for now, until somebody drags it elsewhere.
	if err := s.backend.MoveCard(ctx, b, created, ""); err != nil {
		return err
	}
	// The same context a reviewer's card carries (sendToReview).
	if desc := board.StripEventLines(card.Description); desc != "" {
		_ = s.backend.SetDescription(ctx, b, created, desc)
	}
	s.logEvent(ctx, b, created, board.EventCreated, "", "")
	s.logEvent(ctx, b, created, board.EventBacklog, "", backlogLabel(created))
	return nil
}

// takeReview hands a review nobody has taken to a reviewer and takes it into
// work exactly as `own` with a person takes a task (K4, K10): on the
// reviewer, today, in the reviewed task's sprint (the team's current one when
// it has none, today when the team has none either), in the unplanned band —
// a review is work that turned up in the reviewer's day — and out of the
// backlog. It is the one owner answer a review card takes.
func (s *Service) takeReview(ctx context.Context, b board.Board, review board.Card, reviewer string) error {
	today := board.TodayIso()
	sprint := ""
	if original, ok := findCard(b, review.ReviewOf); ok {
		sprint = original.SprintStart
	}
	if sprint == "" {
		sprint = board.CurrentSprint(b, review.Team)
	}
	if sprint == "" {
		sprint = today
	}
	if err := s.backend.SetAssignee(ctx, b, review, reviewer); err != nil {
		return err
	}
	if err := s.backend.SetStart(ctx, b, review, today); err != nil {
		return err
	}
	if err := s.backend.SetDay(ctx, b, review, today); err != nil {
		return err
	}
	if err := s.backend.SetSprintStart(ctx, b, review, sprint); err != nil {
		return err
	}
	if review.Zone != board.ZoneYellow {
		if err := s.backend.SetZone(ctx, b, review, board.ZoneYellow); err != nil {
			return err
		}
	}
	if review.Parked {
		if err := s.backend.SetBacklog(ctx, b, review, false); err != nil {
			return err
		}
		s.logEvent(ctx, b, review, board.EventBacklog, backlogLabel(review), "")
	}
	s.logEvent(ctx, b, review, board.EventDates, "", board.DateRange(today, today))
	if original, ok := findCard(b, review.ReviewOf); ok {
		s.logEvent(ctx, b, original, board.EventReviewSent, "", reviewer)
	}
	return nil
}

// returnReview is the × on a review card answered "back to the backlog"
// (K10): the reviewer is taken off, and the review becomes the
// needs-a-reviewer item again — nobody's, on no day and in no sprint, PLANNED,
// parked in the backlog of the REVIEWED task's team (the reviewer's team
// may be another), at the top of the board's order as a fresh one
// (requestReview). The task stays on review; nobody reviews it any more,
// which the original's log records. A review already waiting for a reviewer
// is left as it is.
func (s *Service) returnReview(ctx context.Context, b board.Board, review board.Card) error {
	if review.Parked && len(review.Assignees) == 0 {
		return nil
	}
	original, found := findCard(b, review.ReviewOf)
	if found && original.Team != review.Team {
		if err := s.backend.SetTeam(ctx, b, review, original.Team); err != nil {
			return err
		}
		s.logEvent(ctx, b, review, board.EventTeam, review.Team, original.Team)
		review.Team = original.Team
	}
	if !review.Parked {
		if err := s.backend.SetBacklog(ctx, b, review, true); err != nil {
			return err
		}
		s.logEvent(ctx, b, review, board.EventBacklog, "", teamLabel(review.Team))
	}
	if err := s.backend.MoveCard(ctx, b, review, ""); err != nil {
		return err
	}
	if review.Zone != board.ZoneGray {
		if err := s.backend.SetZone(ctx, b, review, board.ZoneGray); err != nil {
			return err
		}
		s.logEvent(ctx, b, review, board.EventZone, string(review.Zone), string(board.ZoneGray))
	}
	reviewer := ""
	if len(review.Assignees) > 0 {
		reviewer = review.Assignees[0]
		if err := s.backend.SetAssignee(ctx, b, review, ""); err != nil {
			return err
		}
		s.logEvent(ctx, b, review, board.EventAssignee, reviewer, "")
	}
	if err := s.leaveWorkingArea(ctx, b, review); err != nil {
		return err
	}
	if found && reviewer != "" {
		s.logEvent(ctx, b, original, board.EventReviewerRemoved, reviewer, "")
	}
	return nil
}

// withdrawReview is the × on a review nobody has taken (K10): the review is
// called off. The task leaves the review stage back to work in progress —
// the In progress the stage menu sets (SetInProgress: the stage cleared,
// the progress nudged into its band), whose cancellation
// takes the review card away and records it on the task — and a review card
// that cancellation spares (somebody put progress on it) goes too, since
// the × asked for exactly that. A review whose task is gone just goes.
func (s *Service) withdrawReview(ctx context.Context, boardID string, b board.Board, review board.Card) error {
	original, found := findCard(b, review.ReviewOf)
	if found && original.Stage == board.StageReview {
		if err := s.SetInProgress(ctx, boardID, original.ItemID); err != nil {
			return err
		}
		after, err := s.backend.LoadBoard(ctx, boardID)
		if err != nil {
			return err
		}
		if _, still := findCard(after, review.ItemID); !still {
			return nil
		}
		b = after
	}
	if err := s.deleteWithCascade(ctx, b, review); err != nil {
		return err
	}
	if found {
		s.logReviewCancelled(ctx, b, original, review)
	}
	return nil
}
