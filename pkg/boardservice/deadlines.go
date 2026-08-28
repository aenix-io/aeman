package boardservice

import (
	"context"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// AddDeadline marks a week with one project's deadline line. A project holds
// at most one per week: asking twice is a no-op rather than a second line,
// which is also what makes dragging one onto another a merge. Two projects can
// both have something due the same week — those are two lines, not a clash.
func (s *Service) AddDeadline(ctx context.Context, boardID string, week, projectName string) error {
	week = board.MondayOf(week)
	if week == "" {
		return fmt.Errorf("a deadline needs a week")
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if projectName != "" {
		if err := knownProject(b, projectName); err != nil {
			return err
		}
	}
	if _, ok := board.FindDeadline(b, projectName, week); ok {
		return nil
	}
	_, err = s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:   board.DeadlineStateTitle,
		Week:    week,
		Project: projectName,
	})
	return err
}

// DeleteDeadline clears one project's deadline on a week (none is a no-op).
func (s *Service) DeleteDeadline(ctx context.Context, boardID string, week, projectName string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	return s.deleteDeadlineAt(ctx, b, projectName, board.MondayOf(week))
}

// MoveDeadline drags a project's deadline to another week. Landing on a week
// where that same project already has one leaves a single line: two deadlines
// of one project on one date are one deadline. Another project's line on that
// week is untouched — it is a different deadline.
func (s *Service) MoveDeadline(ctx context.Context, boardID string, projectName, from, to string) error {
	from, to = board.MondayOf(from), board.MondayOf(to)
	if to == "" {
		return fmt.Errorf("a deadline needs a week")
	}
	if from == to {
		return nil
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	moving, ok := board.FindDeadline(b, projectName, from)
	if !ok || moving.ItemID == "" {
		return fmt.Errorf("no deadline on %s", from)
	}
	// Merge: the target's line goes first, so a torn move never leaves two.
	if err := s.deleteDeadlineAt(ctx, b, projectName, to); err != nil {
		return err
	}
	stub := board.Card{
		ItemID: moving.ItemID, Title: board.DeadlineStateTitle,
		Week: from, Project: projectName,
	}
	return s.backend.SetWeek(ctx, b, stub, to)
}

// deleteDeadlineAt removes a project's deadline card for a week, if any.
func (s *Service) deleteDeadlineAt(ctx context.Context, b board.Board, projectName, week string) error {
	d, ok := board.FindDeadline(b, projectName, week)
	if !ok || d.ItemID == "" {
		return nil
	}
	stub := board.Card{
		ItemID: d.ItemID, Title: board.DeadlineStateTitle,
		Week: week, Project: projectName,
	}
	return s.backend.DeleteCard(ctx, b, stub)
}
