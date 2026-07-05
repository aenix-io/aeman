package board

import "slices"

// TeamGrid returns the cards shown on the Team board's people×zones grid for a
// single team on a given day: cards matching the team filter ("" = the no-team
// group) shown on the viewed day. A materialized card (startDate <= today) shows
// on its sprint's start day AND on its own scheduled day, so a card created on a
// later day of the sprint appears on both; a deferred card (future startDate)
// shows on its own future day only, rejoining the sprint day once today catches
// up. It mirrors filteredCards in TeamBoard.tsx.
func TeamGrid(b Board, team, day string) []Card {
	today := TodayIso()
	out := []Card{}
	for _, c := range b.Cards {
		if c.Team != team {
			continue
		}
		// A card with an end date spans a range: it shows on every day from its
		// start through its end (the calendar sets start…end).
		inRange := c.StartDate != "" && c.Day != "" && c.Day >= c.StartDate &&
			day >= c.StartDate && day <= c.Day
		// A deferred / future-scheduled card (startDate past today) lives on its
		// own day (or range), and its past sprint day keeps it as history; it is
		// hidden everywhere else until that day arrives.
		if c.StartDate != "" && c.StartDate > today {
			if day == c.StartDate || inRange || (c.SprintStart != "" && day == c.SprintStart && c.SprintStart < today) {
				out = append(out, c)
			}
			continue
		}
		if c.SprintStart != "" && c.SprintStart == day {
			out = append(out, c)
			continue
		}
		// A materialized card also shows on its scheduled day (and through its
		// range when it has an end date), so a card created on a later day of its
		// sprint appears both on the sprint's start day and on its own days.
		if inRange || (c.StartDate != "" && c.StartDate == day) {
			out = append(out, c)
			continue
		}
		// A card also shows on a sprint day it passed through — a sprint-pointer
		// day S (current or previous) with origin <= S < sprintStart — so
		// carried-over and deferred cards keep their sprint history.
		if c.SprintStart == "" {
			continue
		}
		start := c.StartDate
		if start == "" {
			start = c.SprintStart
		}
		origin := ActiveSprint(b, team, start)
		for _, s := range []string{CurrentSprint(b, team), PreviousSprint(b, team)} {
			if s != "" && day == s && s < c.SprintStart && origin <= s {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// MeView returns the cards on the personal day board for a user on a given day:
// the user's cards (user = "" means everyone) that belong to the sprint that was
// active on the viewed day (activeSprint) and whose scheduled day has arrived
// (startDate empty or on or before the viewed day). Today shows the current
// sprint; rolling back into the previous sprint's days shows that sprint's cards.
// A card whose team had no active sprint on the day, or that is deferred to the
// future, never shows. It mirrors myCards in MeBoard.tsx.
func MeView(b Board, user, day string) []Card {
	today := TodayIso()
	out := []Card{}
	for _, c := range b.Cards {
		if user != "" && !slices.Contains(c.Assignees, user) {
			continue
		}
		// A deferred / future-scheduled card (startDate past today) is hidden
		// until that day, then shows from it on (Carry Over re-syncs its sprint).
		if c.StartDate != "" && c.StartDate > today {
			if day >= c.StartDate {
				out = append(out, c)
			}
			continue
		}
		// A card with an end date spans a range: it shows on every day from its
		// start through its end regardless of sprint boundaries.
		if c.StartDate != "" && c.Day != "" && c.Day >= c.StartDate &&
			day >= c.StartDate && day <= c.Day {
			out = append(out, c)
			continue
		}
		as := ActiveSprint(b, c.Team, day)
		// A card shows on every day of the sprints it spans — from the one it
		// started in up to the sprint it now belongs to — so a carried-over card
		// still appears on the previous sprint's days it came from.
		if as != "" && as <= c.SprintStart && (c.StartDate == "" || c.StartDate <= day) {
			out = append(out, c)
		}
	}
	return out
}

// WeeklyBands holds a week's plan cards split into the two deadline bands. It
// mirrors the { wed, fri } shape of the `weekly` memo in TeamBoard.tsx.
type WeeklyBands struct {
	Wed []Card `json:"wed"`
	Fri []Card `json:"fri"`
}

// WeeklyPlan returns a single team's weekly-plan cards for a given week (a Monday
// from MondayOf), split into the Wed/Fri bands. A card qualifies when it has a
// plan band, its week equals `week`, and it matches the team ("" = the no-team
// group). Only the Fri band collects cards explicitly marked PlanFri; every other
// plan card falls into Wed. It mirrors the `weekly` memo in TeamBoard.tsx.
func WeeklyPlan(b Board, team, week string) WeeklyBands {
	bands := WeeklyBands{Wed: []Card{}, Fri: []Card{}}
	for _, c := range b.Cards {
		if c.Plan == PlanNone || c.Team != team || !planShowsInWeek(c, week) {
			continue
		}
		// A week-history entry (the card has moved on to a later week) sits in
		// the by-Friday band of the past week: it stayed open through that
		// week's end, and its current band describes the week it lives in now.
		if c.Plan == PlanFri || c.Week != week {
			bands.Fri = append(bands.Fri, c)
		} else {
			bands.Wed = append(bands.Wed, c)
		}
	}
	return bands
}

// planShowsInWeek reports whether a plan card belongs on week W's panel: its
// own week, or — mirroring the day grid's sprint history — any past week it was
// actually worked in. A card taken into work (it has a start date) that was
// carried forward keeps showing in every week from the one it started in up to
// (but excluding) the week it now belongs to, so carrying the plan forward does
// not erase the weeks it was worked in. A pure (never-started) plan card moves
// with its week and leaves no history.
func planShowsInWeek(c Card, week string) bool {
	if c.Week == week {
		return true
	}
	// The "began work" anchor is the earliest of the start date and the sprint
	// join — take-into-plan sets the sprint but not necessarily a start date.
	started := c.StartDate
	if c.SprintStart != "" && (started == "" || c.SprintStart < started) {
		started = c.SprintStart
	}
	return started != "" && c.Week > week && MondayOf(started) <= week
}
