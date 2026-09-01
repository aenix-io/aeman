package board

// CurrentSprint returns a team's current sprint start from its sprint-state card,
// or "" when the team has no sprint yet. team = "" is the no-team group. It
// mirrors currentSprint in web/src/sprint.ts.
func CurrentSprint(b Board, team string) string {
	return b.SprintStates[team].Current
}

// RunningSprintStart is the day the sprint that is still being worked opened
// — the EARLIEST current sprint among the teams named. It is where the past
// ends: a day inside a running sprint is not history, whatever the calendar
// says, because the sprint lays itself out on its own day and the team works
// it from there (G60). Freezing such a day would take the board away from
// the people still standing in it, so the earliest of them wins.
//
// A team the board has no sprint for decides nothing; no teams at all gives
// "", and the caller falls back to today.
func RunningSprintStart(b Board, teams []string) string {
	out := ""
	for _, t := range teams {
		cur := b.SprintStates[t].Current
		if cur == "" {
			continue
		}
		if out == "" || cur < out {
			out = cur
		}
	}
	return out
}

// PreviousSprint returns a team's previous sprint start from its sprint-state
// card, or "" when the team has no prior sprint. team = "" is the no-team group.
// It mirrors previousSprint in web/src/sprint.ts.
func PreviousSprint(b Board, team string) string {
	return b.SprintStates[team].Previous
}

// ActiveSprint returns which sprint was current for a team on a given day: the
// team's current sprint when day is on or after it, else the previous sprint when
// day is on or after that, else "" (only the last two sprints are tracked). The
// Me view groups a day's cards by this. It mirrors activeSprint in
// web/src/sprint.ts. team = "" is the no-team group.
func ActiveSprint(b Board, team, day string) string {
	cur := CurrentSprint(b, team)
	prev := PreviousSprint(b, team)
	if cur != "" && day >= cur {
		return cur
	}
	if prev != "" && day >= prev {
		return prev
	}
	return ""
}
