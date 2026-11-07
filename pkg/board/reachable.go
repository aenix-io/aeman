package board

// Reachable is every card some board still shows: the day boards for the days
// a person opens (today, and each team's current and previous sprint day), the
// Roadmap and every team's backlog, plus the
// cards that ride one of those — a subtask under its parent, a review card
// beside its original.
//
// What it does NOT name is a card on no board at all: open, not parked, and standing on none of those days — an UNPLACED task (K1) among
// them, the shape the Triage strip used to draw and schema 3 parks (K8). The × used to make them by
// the dozen — it demoted a worked card into the previous sprint, out of
// today's way and into a sprint the day grid does not draw, the Me board's
// sprint gate hides and no carry-over ever picks up (a carry-over moves the
// CLOSING sprint's own cards) — and the production board held a hundred and
// thirty-two when the rule was found. The × deletes now (G60); this is what
// names the ones already made, for the migration's report and its cleanup.
func Reachable(b Board, today string) map[string]bool {
	out := make(map[string]bool, len(b.Cards))
	// A day is opened FOR A TEAM, so each team answers for its OWN days:
	// today, and the two its sprint pointer names. Read as one set across
	// the board, a team left behind in June made every other team's cards of
	// that June day look reachable — which is exactly the state this names.
	for team, st := range b.SprintStates {
		days := map[string]bool{today: true}
		if st.Current != "" {
			days[st.Current] = true
		}
		if st.Previous != "" {
			days[st.Previous] = true
		}
		for day := range days {
			for _, c := range TeamGrid(b, team, day) {
				out[c.ItemID] = true
			}
		}
	}
	planning := PlanningIDs(b)
	for _, c := range b.Cards {
		switch {
		// A state card is the roster.
		case IsStateTitle(c.Title):
		// A goal, an initiative or an epic: the Roadmap draws it (H3).
		case IsPlanning(c):
		// A team's BACKLOG, which is a board — the whole point of parking is
		// that the card waits somewhere a person opens. The day boards
		// deliberately do not draw it, so nothing else here would name it,
		// and this list is handed to a cleanup that DELETES what it is given:
		// everything schema 3 parks has to stay here.
		case InBacklog(c):
		// Planned for a day still to come: it arrives on its own.
		case c.StartDate > today:
		// Done is off the board on purpose.
		case Complete(c.Stage, c.Progress):
		default:
			continue
		}
		out[c.ItemID] = true
	}
	// A card that rides another is reachable exactly when that one is: a
	// subtask is drawn under its parent, a review card beside its original.
	// Both are one link deep, so one more pass settles them. A task under
	// an epic rides nothing — it answered for itself above (H1).
	for _, c := range b.Cards {
		owner := ""
		if IsSubtaskIn(c, planning) {
			owner = c.Parent
		}
		if owner == "" {
			owner = c.ReviewOf
		}
		if owner == "" {
			continue
		}
		if out[owner] {
			out[c.ItemID] = true
			continue
		}
		delete(out, c.ItemID)
	}
	return out
}

// Unreachable is Reachable's complement over the board's own cards, in board
// order: the cards no view shows.
func Unreachable(b Board, today string) []Card {
	seen := Reachable(b, today)
	var out []Card
	for _, c := range b.Cards {
		// FINISHED work is not lost work. A day board draws a finished card
		// on the day it was finished and no other, so one whose doneAt was
		// never written — every card completed before the field existed —
		// stands on no day. That makes it unreachable in the letter and
		// nothing at all in the spirit: this list exists to find OPEN work
		// nobody can get to, and a cleanup fed the other kind would take a
		// board's whole history off it.
		if Complete(c.Stage, c.Progress) {
			continue
		}
		if !seen[c.ItemID] {
			out = append(out, c)
		}
	}
	return out
}
