package board

import "testing"

func TestOverdue(t *testing.T) {
	today := "2026-08-27" // a Thursday
	cases := []struct {
		name string
		card Card
		want bool
	}{
		{"a slot past its end", Card{Epic: "E", StartDate: "2026-08-03", Day: "2026-08-21"}, true},
		{"a slot still running", Card{Epic: "E", StartDate: "2026-08-03", Day: "2026-09-04"}, false},
		{"a slot ending today is not overdue yet", Card{Epic: "E", Day: today}, false},
		{"a finished slot is never overdue", Card{Epic: "E", Day: "2026-08-01", Progress: 100}, false},
		{"a turn of last week", Card{Task: "t", Week: "2026-08-17"}, true},
		{"a turn of this week", Card{Task: "t", Week: "2026-08-24"}, false},
		// A card the Triage board scheduled. It is owed by the end of the
		// week it was placed in — being scheduled for a week IS the promise,
		// and without this a debt read as work with all the time in the
		// world.
		{"a card scheduled for last week", Card{Week: "2026-08-17"}, true},
		{"a card scheduled for this week", Card{Week: "2026-08-24"}, false},
		{"a card scheduled for next week", Card{Week: "2026-08-31"}, false},
		{"one finished after its week is not a debt", Card{Week: "2026-08-17", Progress: 100}, false},
		{"an ordinary day card is not this rule's business", Card{StartDate: "2026-08-01", Day: "2026-08-01"}, false},
	}
	for _, c := range cases {
		if got := Overdue(c.card, today); got != c.want {
			t.Errorf("%s: Overdue = %v, want %v (due %q)", c.name, got, c.want, DueDate(c.card))
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
	if Overdue(stretched, today) {
		t.Fatal("a card still inside its reach is not a debt")
	}
	// One whose reach has run out is.
	short := Card{Week: "2026-08-10", Day: "2026-08-21"}
	if !Overdue(short, today) {
		t.Fatal("a card past the end of its reach is a debt")
	}
}
