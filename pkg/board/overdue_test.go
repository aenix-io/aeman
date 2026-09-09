package board

import "testing"

// OVERDUE is "somebody else's promise is broken", which is what paints a card
// late — and a week the Triage board gave a card is this board's own
// planning, not somebody else's promise. DueDate is the clock each kind is
// read against; the ordinary card has one and still never wears the mark.
func TestOverdue(t *testing.T) {
	today := "2026-08-27" // a Thursday
	cases := []struct {
		name string
		card Card
		due  string
		late bool
	}{
		{"a slot past its end", Card{Epic: "E", StartDate: "2026-08-03", Day: "2026-08-21"}, "2026-08-21", true},
		{"a slot still running", Card{Epic: "E", StartDate: "2026-08-03", Day: "2026-09-04"}, "2026-09-04", false},
		{"a slot ending today is not late yet", Card{Epic: "E", Day: today}, today, false},
		{"a finished slot is never late", Card{Epic: "E", Day: "2026-08-01", Progress: 100}, "2026-08-01", false},
		{"a turn of last week", Card{Task: "t", Week: "2026-08-17"}, "2026-08-23", true},
		{"a turn of this week", Card{Task: "t", Week: "2026-08-24"}, "2026-08-30", false},
		// A card the Triage board scheduled: it has a day it was owed by, and
		// nobody outside this board promised it, so it is not late.
		{"a card scheduled for last week", Card{Week: "2026-08-17"}, "2026-08-21", false},
		{"a card scheduled for this week", Card{Week: "2026-08-24"}, "2026-08-28", false},
		{"an ordinary day card is owed by nothing", Card{StartDate: "2026-08-01", Day: "2026-08-01"}, "", false},
	}
	for _, c := range cases {
		if got := DueDate(c.card); got != c.due {
			t.Errorf("%s: DueDate = %q, want %q", c.name, got, c.due)
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
	stretched := Card{Week: "2026-08-17", Day: "2026-09-04"}
	if due := DueDate(stretched); due != "2026-09-04" {
		t.Fatalf("due = %q, want the end of its reach", due)
	}
	// The same, for a kind that DOES wear the mark: a slot inside its reach
	// is not late, and one past it is.
	today := "2026-08-27" // Thursday of the week of 24 Aug
	running := Card{Epic: "E", Week: "2026-08-17", Day: "2026-09-04"}
	if Overdue(running, today) {
		t.Fatal("a slot still inside its reach is not late")
	}
	over := Card{Epic: "E", Week: "2026-08-10", Day: "2026-08-21"}
	if !Overdue(over, today) {
		t.Fatal("a slot past the end of its reach is late")
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
// Not marked is not the same as not shown. The card goes on standing on the
// day boards until it is finished — the week gate holds back the weeks AHEAD,
// never the ones behind — and its own week keeps it on the Triage board, as
// the record of what was missed.
func TestOnlyAPromiseMadeElsewhereGoesOverdue(t *testing.T) {
	today := "2026-09-09" // Wednesday; this week began 2026-09-07
	last := "2026-08-31"  // the Monday before
	open := func(c Card) Card { c.Progress = 40; return c }

	slot := open(Card{ItemID: "slot", Team: "t", Epic: "Storage", Project: "cozy", Week: last, Day: "2026-09-04"})
	turn := open(Card{ItemID: "turn", Team: "t", Task: "01TASK", Process: "Publishing", Week: last})
	plain := open(Card{ItemID: "plain", Team: "t", Week: last, Day: "2026-09-04"})

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

	// All three are still WORK, so the day board goes on drawing them.
	b := NewBoard([]Card{slot, turn, plain})
	on := map[string]bool{}
	for _, c := range TeamGrid(b, "t", today) {
		on[c.ItemID] = true
	}
	for _, id := range []string{"slot", "turn", "plain"} {
		if !on[id] {
			t.Errorf("work owed in a week gone by does not leave the board: %s", id)
		}
	}
	// And finished work is not late.
	done := plain
	done.Progress = 100
	if Overdue(done, today) {
		t.Error("finished work is not late")
	}
}
