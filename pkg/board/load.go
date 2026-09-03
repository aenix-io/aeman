package board

// CarryingNow is how much work a person is holding at this moment, counted
// across EVERY team.
//
// A board is nearly always read through a filter — one team's columns, one
// project's grid — and a person is not. Somebody with four cards in the team
// on screen may have eleven altogether, and handing them a fifth is a
// decision made in the dark unless the whole number is in front of the
// reader. So this one ignores the filter on purpose.
//
// What counts is work that is theirs, open, and not put off to a week that
// has not arrived: a card placed ahead is on no day board until its Monday
// (B1) and is not what they are carrying today. A subtask rides its parent
// and is not a card of its own; a state card is the board's own bookkeeping;
// a personal board's card is nobody else's business.
func CarryingNow(b Board, today string) map[string]int {
	out := map[string]int{}
	for _, c := range b.Cards {
		if len(c.Assignees) == 0 || c.Assignees[0] == "" {
			continue
		}
		if c.Parent != "" || IsStateTitle(c.Title) || IsPersonalDomain(c.Domain) {
			continue
		}
		if Complete(c.Stage, c.Progress) || PlacedAhead(c, today) {
			continue
		}
		out[c.Assignees[0]]++
	}
	return out
}
