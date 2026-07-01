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
		// A deferred card (its sprint pushed past today) hides from today until
		// its new sprint day; its history (past days) and its future slot stay.
		if c.SprintStart != "" && c.SprintStart > today && day >= today && day < c.SprintStart {
			continue
		}
		future := c.StartDate != "" && c.StartDate > today
		eff := c.SprintStart
		if future {
			eff = c.StartDate
		}
		if eff == day {
			out = append(out, c)
			continue
		}
		if future {
			continue
		}
		// A materialized card also shows on its scheduled day, so a card created
		// on a later day of its sprint appears both on the sprint's start day and
		// on the day it was actually created.
		if c.StartDate != "" && c.StartDate == day {
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
		// A deferred card (its sprint pushed past today) hides from today until
		// its new sprint day; past days and days from that one on still show it.
		if c.SprintStart != "" && c.SprintStart > today && day >= today && day < c.SprintStart {
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
		if c.Plan == PlanNone || c.Week != week || c.Team != team {
			continue
		}
		if c.Plan == PlanFri {
			bands.Fri = append(bands.Fri, c)
		} else {
			bands.Wed = append(bands.Wed, c)
		}
	}
	return bands
}
