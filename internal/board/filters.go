package board

import "slices"

// TeamGrid returns the cards shown on the Team board's people×zones grid for a
// single team on a given day: cards matching the team filter ("" = the no-team
// group) whose effective day is the viewed day. A card's effective day is its
// sprint (sprintStart) once it has materialized, but its scheduled day (startDate)
// while that is still in the future — so a materialized card sits on its sprint's
// start date (including ones created on later days), and a deferred card shows on
// its own future day instead, rejoining the sprint day once today catches up. It
// mirrors filteredCards in TeamBoard.tsx.
func TeamGrid(b Board, team, day string) []Card {
	today := TodayIso()
	out := []Card{}
	for _, c := range b.Cards {
		if c.Team != team {
			continue
		}
		eff := c.SprintStart
		if c.StartDate != "" && c.StartDate > today {
			eff = c.StartDate
		}
		if eff == day {
			out = append(out, c)
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
	out := []Card{}
	for _, c := range b.Cards {
		if user != "" && !slices.Contains(c.Assignees, user) {
			continue
		}
		as := ActiveSprint(b, c.Team, day)
		if as != "" && c.SprintStart == as && (c.StartDate == "" || c.StartDate <= day) {
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
