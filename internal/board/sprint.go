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
