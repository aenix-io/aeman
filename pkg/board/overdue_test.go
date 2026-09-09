package board

import "testing"

// Two questions, and they part company on the ordinary card. OWED is "its
// day has passed and it is still open", which is what keeps it in the current
// week's column. OVERDUE is "somebody else's promise is broken", which is
// what paints it late — and a week the Triage board gave a card is this
// board's own planning, not somebody else's promise.
func TestOverdue(t *testing.T) {
	today := "2026-08-27" // a Thursday
	cases := []struct {
		name       string
		card       Card
		owed, late bool
	}{
		{"a slot past its end", Card{Epic: "E", StartDate: "2026-08-03", Day: "2026-08-21"}, true, true},
		{"a slot still running", Card{Epic: "E", StartDate: "2026-08-03", Day: "2026-09-04"}, false, false},
		{"a slot ending today is not late yet", Card{Epic: "E", Day: today}, false, false},
		{"a finished slot is neither", Card{Epic: "E", Day: "2026-08-01", Progress: 100}, false, false},
		{"a turn of last week", Card{Task: "t", Week: "2026-08-17"}, true, true},
		{"a turn of this week", Card{Task: "t", Week: "2026-08-24"}, false, false},
		// A card the Triage board scheduled: owed by the end of its week, so
		// it stays in view — but nobody outside this board promised it, so
		// it is not late.
		{"a card scheduled for last week", Card{Week: "2026-08-17"}, true, false},
		{"a card scheduled for this week", Card{Week: "2026-08-24"}, false, false},
		{"a card scheduled for next week", Card{Week: "2026-08-31"}, false, false},
		{"one finished after its week is neither", Card{Week: "2026-08-17", Progress: 100}, false, false},
		{"an ordinary day card is not this rule's business", Card{StartDate: "2026-08-01", Day: "2026-08-01"}, false, false},
	}
	for _, c := range cases {
		if got := Owed(c.card, today); got != c.owed {
			t.Errorf("%s: Owed = %v, want %v (due %q)", c.name, got, c.owed, DueDate(c.card))
		}
		if got := Overdue(c.card, today); got != c.late {
			t.Errorf("%s: Overdue = %v, want %v", c.name, got, c.late)
		}
	}
}

// A card stretched over several weeks is owed by the END of its reach:
// stretching it is saying it takes longer, and reading the first week's
// Friday would call it late while it is still running.
func TestAStretchedCardIsOwedByItsEnd(t *testing.T) {
	today := "2026-08-27" // Thursday of the week of 24 Aug
	stretched := Card{Week: "2026-08-17", Day: "2026-09-04"}
	if due := DueDate(stretched); due != "2026-09-04" {
		t.Fatalf("due = %q, want the end of its reach", due)
	}
	if Owed(stretched, today) {
		t.Fatal("a card still inside its reach is not a debt")
	}
	// One whose reach has run out is — owed, and still not LATE: nobody
	// outside this board promised the week it was given.
	short := Card{Week: "2026-08-10", Day: "2026-08-21"}
	if !Owed(short, today) {
		t.Fatal("a card past the end of its reach is a debt")
	}
	if Overdue(short, today) {
		t.Fatal("a week this board gave is not a promise made elsewhere")
	}
}

// OVERDUE is a promise broken, and only two kinds of card carry a promise
// somebody else is holding: a Project-board slot, owed by the end date its
// row was drawn to, and a process turn, owed by the end of the week its
// process filed it into. Both are commitments made on another board.
//
// A card scheduled into a week on Triage is not one of those. Its week is
// this board's own planning, and planning is what the daily sync redoes:
// calling it late for still being open on Monday paints most of a normal
// board red and makes the mark mean nothing where it does matter.
//
// It is still OWED, and that is a different word for a different job: a card
// owed in a week gone by stands in the current week's column beside its work,
// which is what keeps it from disappearing when its week passes.
func TestOnlyAPromiseMadeElsewhereGoesOverdue(t *testing.T) {
	today := "2026-09-09" // Wednesday; this week began 2026-09-07
	last := "2026-08-31"  // the Monday before
	open := func(c Card) Card { c.Progress = 40; return c }

	slot := open(Card{Epic: "Storage", Project: "cozy", Week: last, Day: "2026-09-04"})
	turn := open(Card{Task: "01TASK", Process: "Publishing", Week: last})
	plain := open(Card{Week: last, Day: "2026-09-04"})

	for _, c := range []struct {
		name string
		card Card
		want bool
	}{
		{"a project slot past its end date", slot, true},
		{"a process turn past its week", turn, true},
		{"a card somebody planned into a week", plain, false},
		{"a card with no week at all", open(Card{Day: "2026-08-01"}), false},
	} {
		if got := Overdue(c.card, today); got != c.want {
			t.Errorf("%s: Overdue = %v, want %v", c.name, got, c.want)
		}
	}

	// All three are still OWED, so the week's column keeps them.
	for _, c := range []Card{slot, turn, plain} {
		if !Owed(c, today) {
			t.Errorf("a card open past the day it was owed by must still be owed: %+v", c)
		}
		if !InWeek(c, MondayOf(today), today) {
			t.Errorf("a debt must stand in the current week: %+v", c)
		}
	}
	// And finished work is neither.
	done := plain
	done.Progress = 100
	if Owed(done, today) || Overdue(done, today) {
		t.Error("finished work is not owed and not late")
	}
}
