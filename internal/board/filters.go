package board

import "slices"

// TeamGrid returns the cards shown on the Team board's people×zones grid for a
// single team on a given day: cards matching the team filter ("" = the no-team
// group) that are active on the day across their [startDate, sprintStart] span.
// A card carried into a later sprint (sprintStart advanced past startDate) shows
// on every day of that widened span, so it appears in both its original sprint
// and the new one. It mirrors filteredCards/passesFilter in TeamBoard.tsx.
func TeamGrid(b Board, team, day string) []Card {
	out := []Card{}
	for _, c := range b.Cards {
		if c.Team != team {
			continue
		}
		if ActiveOnDay(c.StartDate, c.SprintStart, day) {
			out = append(out, c)
		}
	}
	return out
}

// MeView returns the cards on the personal day board for a user on a given day:
// the user's cards (user = "" means everyone) whose start (startDate, or
// sprintStart when absent) and sprintStart are both set, where the day is on or
// after the start, and the day is on or before the sprint — or that sprint is
// still the team's current one, so a card stays visible until a new sprint is
// started. It mirrors mine/myCards in MeBoard.tsx.
func MeView(b Board, user, day string) []Card {
	out := []Card{}
	for _, c := range b.Cards {
		if user != "" && !slices.Contains(c.Assignees, user) {
			continue
		}
		start := c.StartDate
		if start == "" {
			start = c.SprintStart
		}
		sprint := c.SprintStart
		if start == "" || sprint == "" || day < start {
			continue
		}
		if day <= sprint || CurrentSprint(b, c.Team) == sprint {
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
