package boardservice

import (
	"context"

	"github.com/aenix-io/aeman/pkg/board"
)

// requestReview makes the review card of a task put on review WITHOUT a
// reviewer (K10, docs/design/backlog.md "Reviews"): «review: <title>», of
// the task's team, with nobody on it, parked in that team's backlog — the
// "needs a reviewer" item the Backlog pins above its ranked tasks. It joins
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
