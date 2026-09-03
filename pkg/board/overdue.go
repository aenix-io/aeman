package board

// DueDate is the day a card that came from a plan was owed by — "" for a
// card that has no such day. Three kinds of card come from a plan, and each
// has its own clock:
//
//   - a Project-board slot is owed by its end date;
//   - a process turn is owed by the end of the week it was filed in;
//   - a card scheduled for a WEEK is owed by the end of that week.
//
// A card with no week is not scheduled work and has no due date here: the day
// board's carry-over is how those move, and it is not this rule's business.
func DueDate(c Card) string {
	switch {
	case c.Epic != "":
		return c.Day
	case c.Task != "":
		if c.Week == "" {
			return ""
		}
		return AddDays(c.Week, 6)
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

// Overdue reports whether a card from a plan is still open past the day it
// was owed by. It is derived, never stored — the card's own dates are the
// truth, and a flag beside them would be one more thing to drift.
func Overdue(c Card, today string) bool {
	if Complete(c.Stage, c.Progress) {
		return false
	}
	due := DueDate(c)
	return due != "" && due < today
}
