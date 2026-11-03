package board

// DueDate is the day a card that came from a plan was owed by — "" for a
// card that has no such day. One kind of card comes from a plan: a
// Project-board slot, owed by its end date. A task has no week to be owed by
// (K7); the day board's carry-over is how it moves, and that is not this
// rule's business.
func DueDate(c Card) string {
	if c.Scope != "" {
		return c.Day
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
// A task's own dates are not one of those: they are the day board's planning,
// and planning is what the next sync redoes — so calling a task late paints
// most of a normal board red, and a mark that is everywhere says nothing
// where it matters. Such a card is not marked and is not hidden either: the
// day boards go on drawing open work until it is finished.
//
// It is derived, never stored — the card's own dates are the truth, and a
// flag beside them would be one more thing to drift.
func Overdue(c Card, today string) bool {
	if c.Scope == "" {
		return false
	}
	if Complete(c.Stage, c.Progress) {
		return false
	}
	due := DueDate(c)
	return due != "" && due < today
}
