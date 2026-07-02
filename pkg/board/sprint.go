package board

// CurrentSprint returns a team's current sprint start from its sprint-state card,
// or "" when the team has no sprint yet. team = "" is the no-team group. It
// mirrors currentSprint in web/src/sprint.ts.
func CurrentSprint(b Board, team string) string {
	return b.SprintStates[team].Current
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
