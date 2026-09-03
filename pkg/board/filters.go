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
		// A card placed in a week ahead is in the backlog, not on any day,
		// until its Monday (B1).
		if PlacedAhead(c, today) {
			continue
		}
		// The WEEK's own work stands on the grid all week — in its person's
		// column, or in Unassigned when nobody has taken it. This is the set
		// the Triage board shows for that week: what the weekly panel used
		// to hold beside the grid, now in the grid itself, so a card placed
		// in a week is not invisible until somebody gives it a day. A slot
		// covering the week is part of that set, which is why this stands
		// above the epic gate rather than below it.
		//
		// A DEFERRED card is not, though. Deferring is the act of taking a
		// card off the board until a later day, and its week says when the
		// work is due, not that it should still be drawn today: a card pushed
		// a month out went on standing in this week's grid, on a board the
		// person had just cleared it from. The rule below says the same thing
		// about the days; this says it about the week.
		if !deferred(c, today) && InWeek(c, MondayOf(day), today) {
			out = append(out, c)
			continue
		}
		// An epic card lives on the Project board until it joins a sprint (see
		// MeView) — its multi-week span must not smear across the day grid.
		// The COLUMN is what keeps it there, and a column needs the epic
		// side: a card carrying only a project name is on no Project board
		// (ProjectBoard.tsx renders columns by the epic), so hiding it here
		// would leave it nowhere at all.
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
		if deferred(c, today) {
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
		// The column is what holds it, and a column needs the epic side —
		// TeamGrid draws the same line. Unless someone owns it: then it is
		// that person's work, and their day board is where they look for it;
		// it shows across the days its own dates cover, like any other dated
		// card.
		if c.Epic != "" && c.SprintStart == "" && len(c.Assignees) == 0 {
			continue
		}
		if user != "" && !slices.Contains(c.Assignees, user) && !childAssigned(b, c.ItemID, user) {
			continue
		}
		// A card placed in a week ahead waits in the backlog (B1).
		if PlacedAhead(c, today) {
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
		if c.SprintStart == "" && c.StartDate != "" &&
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

// deferred reports that a card has been scheduled AWAY from today: its start
// date is still to come. Such a card lives on that day (and through its range)
// and is hidden everywhere else until the day arrives — on the day grid, and
// equally in the week it is due in, which is where it went on being drawn
// after somebody had deliberately taken it off today's board.
func deferred(c Card, today string) bool {
	return c.StartDate != "" && c.StartDate > today
}
