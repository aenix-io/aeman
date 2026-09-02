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
		{"a turn of last week", Card{Task: "t", Week: "2026-08-17", Plan: PlanFri}, true},
		{"a turn of this week", Card{Task: "t", Week: "2026-08-24", Plan: PlanFri}, false},
		{"by-Wednesday, on Thursday", Card{Plan: PlanWed, Week: "2026-08-24"}, true},
		{"by-Friday, on Thursday", Card{Plan: PlanFri, Week: "2026-08-24"}, false},
		{"by-Friday of last week", Card{Plan: PlanFri, Week: "2026-08-17"}, true},
		{"a plan card with no week", Card{Plan: PlanFri}, false},
		// A card the Backlog board scheduled: a week and no band. It is owed
		// by the end of the week it was placed in — being scheduled for a
		// week IS the promise, and without this a debt from the backlog read
		// as work with all the time in the world.
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

// An overdue plan card shows on the current week's panel beside that week's
// work — in the BY-WEDNESDAY band, since it is already late — and stays on
// the panel of the week it was owed in, under the band it carries there.
// Nothing moves: the card's own week and band are untouched.
func TestADebtFollowsYou(t *testing.T) {
	today := "2026-08-27" // Thursday of the week of 24 Aug
	thisWeek := "2026-08-24"
	debt := Card{ItemID: "debt", Team: "alpha", Plan: PlanFri, Week: "2026-08-17"}
	paid := Card{ItemID: "paid", Team: "alpha", Plan: PlanFri, Week: "2026-08-17", Progress: 100, Stage: StageDone}
	b := NewBoard([]Card{debt, paid})

	now := WeeklyPlanAt(b, "alpha", thisWeek, today)
	if len(now.Wed) != 1 || now.Wed[0].ItemID != "debt" {
		t.Fatalf("this week's panel must carry the open debt by Wednesday, and nothing closed; got %+v", now)
	}
	if len(now.Fri) != 0 {
		t.Fatalf("a debt is not this week's by-Friday work: %+v", now.Fri)
	}
	then := WeeklyPlanAt(b, "alpha", "2026-08-17", today)
	if len(then.Fri) != 2 {
		t.Fatalf("the week it was owed in keeps both; got %d", len(then.Fri))
	}
	// Not on some other future week.
	if later := WeeklyPlanAt(b, "alpha", "2026-08-31", today); len(later.Fri) != 0 {
		t.Fatalf("a debt is not next week's work; got %d", len(later.Fri))
	}
	// And the card itself did not move.
	if debt.Week != "2026-08-17" {
		t.Fatal("the card moved")
	}
}
