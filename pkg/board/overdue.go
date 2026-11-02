package board

// DueDate is the day a card that came from a plan was owed by — "" for a
// card that has no such day. Two kinds of card come from a plan, and each
// has its own clock:
//
//   - a Project-board slot is owed by its end date;
//   - a card scheduled for a WEEK is owed by the end of that week.
//
// A card with no week is not scheduled work and has no due date here: the day
// board's carry-over is how those move, and it is not this rule's business.
func DueDate(c Card) string {
	switch {
	case c.Epic != "":
		return c.Day
	case c.Week != "":
		// A card the Triage board scheduled. Being placed in a week IS the
		// promise, so it is owed by the end of that week — its Friday.
		// Without this a backlog debt read as work with all the time in the
		// world.
		//
		// A card STRETCHED over several weeks is owed by the end of its
		// reach: stretching it is saying it takes longer, and reading the
		// first week's Friday would call it late while it is still running.
		if end := MondayOf(c.Day); end > c.Week {
			return c.Day
		}
		return AddDays(c.Week, 4)
	}
	return ""
}

// Overdue reports whether a card has broken a promise somebody else is
// holding — which is what the mark means, and why only one kind of card can
// carry it: a Project-board SLOT, owed by the end date its row was drawn to.
// It is a commitment made on another board, by somebody who is not the
// person the card sits on. (A process turn used to be the other kind, owed
// by the end of its week; the feature is gone, and a turn it left behind is
// an ordinary card of its week now.)
//
// A card scheduled into a week on the Triage board is not one of those. Its
// week is that board's own planning, and planning is what the next sync
// redoes — so calling it late for being open on Monday paints most of a
// normal board red, and a mark that is everywhere says nothing where it
// matters. Such a card is not marked and is not hidden either: a week gone by
// holds its own record, and the day boards go on drawing the work until it is
// finished (TeamGrid holds back the weeks AHEAD, never the ones behind).
//
// It is derived, never stored — the card's own dates are the truth, and a
// flag beside them would be one more thing to drift.
func Overdue(c Card, today string) bool {
	if c.Epic == "" {
		return false
	}
	if Complete(c.Stage, c.Progress) {
		return false
	}
	due := DueDate(c)
	return due != "" && due < today
}
