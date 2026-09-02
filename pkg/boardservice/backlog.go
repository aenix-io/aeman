package boardservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrLaneDerived is a lane written on a card whose links decide it — a
// column slot, a process turn, a subtask, a review card (B2).
var ErrLaneDerived = errors.New("the card's lane follows its links")

// ErrNotAMonday is a backlog week that is not a Monday.
var ErrNotAMonday = errors.New("a backlog week is a Monday")

// Place puts a card in a week of the Backlog board, and in a lane when one
// is given (docs/design/backlog.md, B4, B9, B10). A slot moves its dates
// and its week follows, as on the Project board; every other card takes
// the week and keeps its band — by Friday when it has none — and, placed
// in a week AHEAD, leaves the day board: its dates and sprint are cleared,
// since a card in a week to come is on no day (B1). An empty week changes
// the lane alone.
func (s *Service) Place(ctx context.Context, boardID, itemID, week string, lane board.Lane, band board.PlanBand) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if lane != board.LaneNone {
		if board.LaneDerives(card) {
			return fmt.Errorf("%w: %s", ErrLaneDerived, itemID)
		}
		if lane != card.Lane {
			if err := s.backend.SetLane(ctx, b, card, lane); err != nil {
				return err
			}
			card.Lane = lane
		}
	}
	if week == "" {
		return nil
	}
	if !board.IsDayIso(week) || board.MondayOf(week) != week {
		return fmt.Errorf("%w: %q", ErrNotAMonday, week)
	}
	today := board.TodayIso()
	slot := card.Epic != "" && card.Week != "" && card.Day != ""
	if slot {
		// The slot's week is its start's week: move the dates, the week
		// follows (syncSlotWeek).
		from := board.MondayOf(card.StartDate)
		if from == "" {
			from = card.Week
		}
		delta := board.DaysBetween(from, week)
		if delta == 0 {
			return nil
		}
		return s.SetDates(ctx, boardID, itemID, board.AddDays(card.StartDate, delta), board.AddDays(card.Day, delta))
	}
	if card.Parent != "" {
		if err := s.ungroup(ctx, b, card); err != nil {
			return err
		}
		card.Parent = ""
	}
	if band == board.PlanNone {
		band = card.Plan
	}
	if band == board.PlanNone {
		band = board.PlanFri
	}
	// A card owed by Wednesday, moved into a week whose Wednesday has
	// passed, is owed by Friday: the earlier deadline is gone (B10).
	if band == board.PlanWed && week == board.MondayOf(today) && today > board.AddDays(week, 2) {
		band = board.PlanFri
	}
	if band != card.Plan {
		if err := s.backend.SetPlan(ctx, b, card, band); err != nil {
			return err
		}
		if card.Plan == board.PlanNone {
			s.logEvent(ctx, b, card, board.EventPlanAdded, "", string(band))
		} else {
			s.logEvent(ctx, b, card, board.EventPlanBand, string(card.Plan), string(band))
		}
		card.Plan = band
	}
	if week != card.Week {
		if err := s.backend.SetWeek(ctx, b, card, week); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventWeek, card.Week, week)
		card.Week = week
	}
	// Placed ahead: off the day board. The dates would keep showing the
	// card on its sprint day and its own day beside the week it now waits
	// in, and the week is where it is.
	if week > board.MondayOf(today) && (card.StartDate != "" || card.Day != "" || card.SprintStart != "") {
		if card.StartDate != "" {
			if err := s.backend.SetStart(ctx, b, card, ""); err != nil {
				return err
			}
		}
		if card.Day != "" {
			if err := s.backend.SetDay(ctx, b, card, ""); err != nil {
				return err
			}
		}
		if card.SprintStart != "" {
			if err := s.backend.SetSprintStart(ctx, b, card, ""); err != nil {
				return err
			}
			if err := s.syncChildrenSprint(ctx, b, card.ItemID, ""); err != nil {
				return err
			}
		}
		s.logEvent(ctx, b, card, board.EventDates, card.StartDate+"…"+card.Day, "")
	}
	return nil
}

// Untriage takes a card out of every week: it is back in the triage strip,
// its lane kept (B4). A slot is refused — its week is its row.
func (s *Service) Untriage(ctx context.Context, boardID, itemID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if card.Epic != "" {
		return fmt.Errorf("%w: a slot's week is its row — take it off the column instead", ErrWeekDerived)
	}
	if card.Plan != board.PlanNone {
		if err := s.backend.SetPlan(ctx, b, card, board.PlanNone); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventPlanReleased, string(card.Plan), "")
	}
	if card.Week != "" {
		if err := s.backend.SetWeek(ctx, b, card, ""); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventWeek, card.Week, "")
	}
	return nil
}
