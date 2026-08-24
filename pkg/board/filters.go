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
		// Subtasks are never placed on their own: they ride with their parent
		// (the API layer appends the children of every delivered parent).
		if c.Parent != "" {
			continue
		}
		if c.Team != team {
			continue
		}
		// An epic card lives on the Project board until it joins a sprint (see
		// MeView) — its multi-week span must not smear across the day grid.
		if c.Epic != "" && c.SprintStart == "" {
			continue
		}
		// A card with an end date spans a range: it shows on every day from its
		// start through its end (the calendar sets start…end).
		inRange := c.StartDate != "" && c.Day != "" && c.Day >= c.StartDate &&
			day >= c.StartDate && day <= c.Day
		// A deferred / future-scheduled card (startDate past today) lives on its
		// own day (or range), and a CLOSED sprint's day keeps it as history; it
		// is hidden everywhere else until that day arrives. The team's CURRENT
		// sprint is never history: deferring a card is precisely the act of
		// taking it out of the sprint in progress, so it must leave that day at
		// once — even when the sprint opened days ago (no carry-over since).
		if c.StartDate != "" && c.StartDate > today {
			pastSprintDay := c.SprintStart != "" && day == c.SprintStart &&
				c.SprintStart < today && c.SprintStart != CurrentSprint(b, c.Team)
			if day == c.StartDate || inRange || pastSprintDay {
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
		// Subtasks are never listed on their own; they ride with their parent.
		if c.Parent != "" {
			continue
		}
		// An epic card lives on the Project board until it joins a sprint: its
		// week-spanning dates would otherwise smear it across the day boards.
		// Unless someone owns it — then it is that person's work, and their
		// day board is where they look for it; it shows across the days its
		// own dates cover, like any other dated card.
		if c.Epic != "" && c.SprintStart == "" && len(c.Assignees) == 0 {
			continue
		}
		if user != "" && !slices.Contains(c.Assignees, user) && !childAssigned(b, c.ItemID, user) {
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
		// A sprint-less day card (a "next sprint" create) stays visible from its
		// scheduled day on — the sprint gate below would otherwise hide it right
		// when its day arrives, until a carry-over adopts it into a sprint. Only
		// cards scheduled into the sprint active on the viewed day (or later)
		// qualify: an old sprint-less stray stays on its own past days instead
		// of resurfacing on today's board.
		if c.SprintStart == "" && c.Plan == PlanNone && c.StartDate != "" &&
			c.StartDate <= day && c.StartDate >= as {
			out = append(out, c)
			continue
		}
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
	return WeeklyPlanAt(b, team, week, TodayIso())
}

// WeeklyPlanAt is WeeklyPlan against an explicit "today" (testable).
func WeeklyPlanAt(b Board, team, week, today string) WeeklyBands {
	bands := WeeklyBands{Wed: []Card{}, Fri: []Card{}}
	for _, c := range b.Cards {
		// A Project-board slot needs no stored band to be the week's work:
		// its span IS its plan, and the band derives from the end date. A
		// band-less card that is not a slot still stays off the panel.
		slot := c.Epic != "" && c.Week != "" && c.Day != ""
		if (c.Plan == PlanNone && !slot) || c.Team != team || !planShowsInWeekAt(c, week, today) {
			continue
		}
		switch {
		// The derived band. Only the week the slot ENDS in can be a
		// by-Wednesday week; every earlier covered week holds the slot open
		// through its Friday. A stored band never reaches this arm — hand
		// placement outranks derivation, so deriving cannot move a card.
		case c.Plan == PlanNone:
			if MondayOf(c.Day) == week && c.Day <= AddDays(week, 2) {
				bands.Wed = append(bands.Wed, c)
			} else {
				bands.Fri = append(bands.Fri, c)
			}
		// A week-history entry (the card has moved on to a later week) sits in
		// the by-Friday band of the past week: it stayed open through that
		// week's end, and its current band describes the week it lives in now.
		case c.Plan == PlanFri || c.Week != week:
			bands.Fri = append(bands.Fri, c)
		default:
			bands.Wed = append(bands.Wed, c)
		}
	}
	return bands
}

// planShowsInWeekAt reports whether a plan card belongs on week W's panel: its
// own week, or — mirroring the day grid's sprint history — any FINISHED week
// it was actually worked in. A card taken into work that was moved forward
// keeps showing in the weeks it was worked once those weeks are over (their
// Friday has passed); while a week is still running, a card deliberately
// pushed to a future week leaves its panel — it is not this week's work
// anymore. A pure (never-started) plan card moves with its week and leaves no
// history.
func planShowsInWeekAt(c Card, week, today string) bool {
	if c.Week == week {
		return true
	}
	// A debt follows you: a plan card still open past the day it was owed by
	// shows on the CURRENT week's panel too, beside that week's own work,
	// without leaving the week it was owed in. It used to take a carry to
	// bring it forward, and the carry moved it — so the week that was missed
	// forgot it had been. Only the current week: the panel of some other
	// future week is not where anyone settles debts.
	if week == MondayOf(today) && c.Week < week && Overdue(c, today) {
		return true
	}
	// A Project-board slot spans weeks by design: it belongs to every week
	// between its two boundaries, not only to the one it starts in. Otherwise
	// a slot carried over (which moves its END) would vanish from the panel of
	// the very week it was carried into.
	if c.Epic != "" && c.Week != "" && c.Day != "" &&
		c.Week <= week && week <= MondayOf(c.Day) {
		return true
	}
	// History only in weeks whose working days are over: past the week's
	// Friday (the plan's bands are wed/fri deadlines; the weekend belongs to
	// wrap-up, not new placement).
	if today <= AddDays(week, 4) {
		return false
	}
	// The "began work" anchor is the earliest of the start date and the sprint
	// join — take-into-plan sets the sprint but not necessarily a start date.
	started := c.StartDate
	if c.SprintStart != "" && (started == "" || c.SprintStart < started) {
		started = c.SprintStart
	}
	return started != "" && c.Week > week && MondayOf(started) <= week
}

// childAssigned reports whether any subtask of a card is assigned to user —
// the personal board shows the parent when the person owns only a subtask.
func childAssigned(b Board, itemID, user string) bool {
	for _, c := range b.Cards {
		if c.Parent == itemID && slices.Contains(c.Assignees, user) {
			return true
		}
	}
	return false
}
