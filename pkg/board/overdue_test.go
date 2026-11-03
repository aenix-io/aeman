package board

import "testing"

// OVERDUE is "somebody else's promise is broken", which is what paints a card
// late. DueDate is the clock it is read against: a Project-board slot's end
// date, and nothing for a task, which has no week to be owed by (K7).
func TestOverdue(t *testing.T) {
	today := "2026-08-27" // a Thursday
	cases := []struct {
		name string
		card Card
		due  string
		late bool
	}{
		{"a slot past its end", Card{Scope: "E", StartDate: "2026-08-03", Day: "2026-08-21"}, "2026-08-21", true},
		{"a slot still running", Card{Scope: "E", StartDate: "2026-08-03", Day: "2026-09-04"}, "2026-09-04", false},
		{"a slot ending today is not late yet", Card{Scope: "E", Day: today}, today, false},
		{"a finished slot is never late", Card{Scope: "E", Day: "2026-08-01", Progress: 100}, "2026-08-01", false},
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

// A slot stretched over several weeks is owed by the END of its reach:
// stretching it is saying it takes longer.
func TestAStretchedCardIsOwedByItsEnd(t *testing.T) {
	today := "2026-08-27" // Thursday of the week of 24 Aug
	running := Card{Scope: "E", StartDate: "2026-08-17", Day: "2026-09-04"}
	if Overdue(running, today) {
		t.Fatal("a slot still inside its reach is not late")
	}
	over := Card{Scope: "E", StartDate: "2026-08-10", Day: "2026-08-21"}
	if !Overdue(over, today) {
		t.Fatal("a slot past the end of its reach is late")
	}
}

// OVERDUE is a promise broken, and only one kind of card carries a promise
// somebody else is holding: a Project-board slot, owed by the end date its
// row was drawn to — a commitment made on another board. (A process turn
// used to be the other kind; with the feature gone, a turn it left behind is
// a card planned into a week like any other, and is not marked.)
//
// A task's own end date is the day board's planning, not somebody else's
// promise: calling it late paints most of a normal board red. Not marked is
// not the same as not shown — open work goes on standing on the day boards
// until it is finished. A project slot is late on the Project board, the one
// board that draws it (H4).
func TestOnlyAPromiseMadeElsewhereGoesOverdue(t *testing.T) {
	today := "2026-09-09" // Wednesday; this week began 2026-09-07
	last := "2026-08-31"  // the Monday before
	open := func(c Card) Card { c.Progress = 40; return c }

	slot := open(Card{ItemID: "slot", Team: "t", Scope: "Storage", Project: "cozy", StartDate: last, Day: "2026-09-04"})
	// A turn the retired Process feature left behind: a recurrent card.
	turn := open(Card{ItemID: "turn", Team: "t", Stage: StageRecurrent, StartDate: last})
	plain := open(Card{ItemID: "plain", Team: "t", StartDate: last, Day: "2026-09-04"})

	for _, c := range []struct {
		name string
		card Card
		want bool
	}{
		{"a project slot past its end date", slot, true},
		{"an orphaned recurrent turn past its days", turn, false},
		{"a task past its own end date", plain, false},
	} {
		if got := Overdue(c.card, today); got != c.want {
			t.Errorf("%s: Overdue = %v, want %v", c.name, got, c.want)
		}
	}

	// The two tasks are still WORK, so the day board goes on drawing them;
	// the slot is the Project board's and is late there.
	b := NewBoard([]Card{slot, turn, plain})
	on := map[string]bool{}
	for _, c := range TeamGrid(b, "t", today) {
		on[c.ItemID] = true
	}
	for _, id := range []string{"turn", "plain"} {
		if !on[id] {
			t.Errorf("open work past its days does not leave the board: %s", id)
		}
	}
	if on["slot"] {
		t.Error("a project slot is late on the Project board, not in hand on a day")
	}
	// And finished work is not late.
	done := plain
	done.Progress = 100
	if Overdue(done, today) {
		t.Error("finished work is not late")
	}
}
