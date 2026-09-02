package board

// Reachable is every card some board still shows: the day boards for the days
// a person opens (today, and each team's current and previous sprint day), the
// Project board's columns, the weekly plan, the Process tab, and a personal
// board, plus the cards that ride one of those — a subtask under its parent, a
// review card beside its original.
//
// What it does NOT name is a card on no board at all: open, in no column, no
// plan week, and standing on none of those days. The × used to make them by
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
	for _, c := range b.Cards {
		switch {
		// Its own owner's board, which has no team and no sprint to judge it by.
		case IsPersonalDomain(c.Domain):
		// The Process tab: a task is what turns are copied from, and a state
		// card is the roster.
		case c.Title == ProcessTaskTitle || IsStateTitle(c.Title):
		// A Project-board column, and the weekly plan.
		case c.Epic != "":
		case c.Week != "":
		// Planned for a day still to come: it arrives on its own.
		case c.StartDate > today:
		// Done is off the board on purpose.
		case Complete(c.Stage, c.Progress):
		default:
			continue
		}
		out[c.ItemID] = true
	}
	// The process tasks are split out of Cards by NewBoard; a caller holding
	// a raw list has them among the cards, and either way they belong to the
	// Process tab rather than to a day.
	for _, t := range b.Tasks {
		out[t.ItemID] = true
	}
	// A card that rides another is reachable exactly when that one is: a
	// subtask is drawn under its parent, a review card beside its original.
	// Both are one link deep, so one more pass settles them.
	for _, c := range b.Cards {
		owner := c.Parent
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
		if !seen[c.ItemID] {
			out = append(out, c)
		}
	}
	return out
}
