package boardservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrNotAMonday is a triage week that is not a Monday.
var ErrNotAMonday = errors.New("a week on the Triage board is a Monday")

// Place puts a card in a week of the Triage board — which is what TRIAGING
// a card means: until then nobody has said when the work is due, and the
// board holds it in the strip.
//
// What the week does depends on what the card already is. A Project-board
// SLOT moves its dates and its week follows them, as on the Project board. A
// weekly-plan card keeps its band — the plan is a commitment to a day of the
// week, and moving the card between weeks does not undo it. Every other card
// takes the week ALONE: it is work of the board scheduled for that week, not
// a promise on the weekly panel, and giving it a band would put it there.
//
// A card placed in a week AHEAD leaves the day board: its dates and sprint go,
// since a card in a week to come is on no day (B1). Placed in the CURRENT
// week it keeps them — that is the week the board is working.
func (s *Service) Place(ctx context.Context, boardID, itemID, week string, band board.PlanBand) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if week == "" {
		return nil
	}
	if !board.IsDayIso(week) || board.MondayOf(week) != week {
		return fmt.Errorf("%w: %q", ErrNotAMonday, week)
	}
	today := board.TodayIso()
	if card.Epic != "" && card.Week != "" && card.Day != "" {
		// A slot's week is its start's week: move the dates, the week
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
	// The band is the WEEKLY PLAN's: a card that has one keeps it, a card
	// that has none is not given one by being scheduled. Only an explicit
	// band (the plan's own drop) sets it.
	if band == board.PlanNone {
		band = card.Plan
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
		return nil
	}
	// Brought back into the week the team is working, a card that stands on
	// no day at all joins the sprint being worked — which is where a card of
	// the current week with no day of its own belongs, and where carry-over
	// puts one. Without this the round trip out to a week ahead and back left
	// the card holding a week and standing on no board: not on the team's
	// day, not on anyone's Me, findable by its id alone. A card that already
	// has a day keeps it: placing it in the week it is already in says
	// nothing new about when it is being done.
	if card.StartDate == "" && card.Day == "" && card.SprintStart == "" &&
		card.Epic == "" && !board.IsPersonalDomain(card.Domain) {
		sprint := board.ActiveSprint(b, card.Team, today)
		if sprint == "" {
			sprint = board.CurrentSprint(b, card.Team)
		}
		if sprint == "" {
			// A team with no sprint pointer at all: the week seeds it, the
			// same fallback SetDates makes.
			sprint = week
		}
		if err := s.backend.SetSprintStart(ctx, b, card, sprint); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventSprint, "", sprint)
		if err := s.syncChildrenSprint(ctx, b, card.ItemID, sprint); err != nil {
			return err
		}
	}
	return nil
}

// Untriage takes a card out of its week: it is back in the strip, waiting for
// someone to say when the work is due. A slot is refused — its week is its
// row on the Project board.
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
