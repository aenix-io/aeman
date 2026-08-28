package board

import "testing"

// A personal domain is named after its owner, with a marker no repository
// name carries, so the two never collide and the owner is readable off the
// name wherever it shows up (status.domain, the badge, a filter).
func TestPersonalDomainNaming(t *testing.T) {
	if PersonalDomain("kvaps") != "~kvaps" {
		t.Fatalf("PersonalDomain = %q", PersonalDomain("kvaps"))
	}
	for name, want := range map[string]bool{"~kvaps": true, "aeman-db": false, "~": false, "": false, "portal~": false} {
		if IsPersonalDomain(name) != want {
			t.Errorf("IsPersonalDomain(%q) = %v, want %v", name, !want, want)
		}
	}
	if PersonalOwner("~kvaps") != "kvaps" || PersonalOwner("aeman-db") != "" {
		t.Fatalf("PersonalOwner = %q / %q", PersonalOwner("~kvaps"), PersonalOwner("aeman-db"))
	}
}

// The personal view is the owner's repository as a backlog: every open card,
// plus the ones finished today — a done card is seen the day it was done and
// is gone the next morning (no carry-over sweeps it). Nothing from any other
// domain, nothing of another person's personal repository.
func TestPersonalViewOpenAndDoneToday(t *testing.T) {
	today, yesterday := "2026-08-28", "2026-08-27"
	b := Board{Cards: []Card{
		{ItemID: "open", Title: "open", Domain: "~kvaps", Zone: ZoneRed, Progress: 40},
		{ItemID: "fresh", Title: "not started", Domain: "~kvaps"},
		{ItemID: "done-today", Title: "done today", Domain: "~kvaps", Progress: 100, DoneAt: today},
		{ItemID: "done-yesterday", Title: "done yesterday", Domain: "~kvaps", Progress: 100, DoneAt: yesterday},
		{ItemID: "done-unknown", Title: "done, no date", Domain: "~kvaps", Progress: 100},
		{ItemID: "sub", Title: "a step", Domain: "~kvaps", Parent: "open"},
		{ItemID: "team", Title: "team card", Domain: "aeman-db", Progress: 40},
		{ItemID: "bobs", Title: "bob's", Domain: "~bob", Progress: 40},
	}}
	got := PersonalView(b, "kvaps", today)
	want := []string{"open", "fresh", "done-today", "sub"}
	if len(got) != len(want) {
		t.Fatalf("personal view = %v, want %v", idsOf(got), want)
	}
	for i, id := range want {
		if got[i].ItemID != id {
			t.Fatalf("personal view = %v, want %v (board order kept)", idsOf(got), want)
		}
	}
	if len(PersonalView(b, "nobody", today)) != 0 {
		t.Fatal("a person without a personal repository has an empty personal view")
	}
	// Tomorrow the card done today is gone too.
	if len(PersonalView(b, "kvaps", "2026-08-29")) != 3 {
		t.Fatalf("the next day = %v", idsOf(PersonalView(b, "kvaps", "2026-08-29")))
	}
}

// A personal board has no carry-over, so its recurrent cards reseed by the
// calendar when the owner reads the board: the day after they were finished
// for the default cycle, once the week/month has elapsed since the card's
// start otherwise — and never twice in one day.
func TestPersonalReseedDueTheDayAfterAndByCycle(t *testing.T) {
	const day = "2026-08-28"
	rec := func(id, cycle, start, doneAt string, progress int) Card {
		return Card{ItemID: id, Title: id, Domain: "~kvaps", Stage: StageRecurrent, Recurrence: cycle,
			StartDate: start, DoneAt: doneAt, Progress: progress}
	}
	b := Board{Cards: []Card{
		rec("daily-yesterday", "", "2026-08-27", "2026-08-27", 100),               // finished yesterday: due today
		rec("daily-today", "", "2026-08-28", "2026-08-28", 100),                   // finished today: due tomorrow
		rec("daily-open", "", "2026-08-27", "", 40),                               // this iteration is still on
		rec("daily-late", "", "2026-08-20", "2026-08-28", 100),                    // a week late but finished today: not twice a day
		rec("daily-undated", "", "2026-08-20", "", 100),                           // done without a day on record: never
		rec("weekly-rests", RecurrenceWeek, "2026-08-25", "2026-08-25", 100),      // three days in
		rec("weekly-due", RecurrenceWeek, "2026-08-21", "2026-08-21", 100),        // a week on
		rec("weekly-late", RecurrenceWeek, "2026-08-10", "2026-08-27", 100),       // overdue, finished yesterday
		rec("weekly-late-today", RecurrenceWeek, "2026-08-10", "2026-08-28", 100), // overdue, finished today
		rec("monthly-rests", RecurrenceMonth, "2026-07-31", "2026-07-31", 100),    // Jul 31 + 1 month = Aug 31
		rec("monthly-due", RecurrenceMonth, "2026-07-28", "2026-07-28", 100),      // Jul 28 + 1 month = today
		rec("reseeded", "", "2026-08-26", "2026-08-26", 100),                      // has its copy below
		{ItemID: "reseeded-copy", Title: "reseeded", Domain: "~kvaps", Stage: StageRecurrent, StartDate: "2026-08-27", Progress: 20},
		{ItemID: "plain-done", Title: "plain", Domain: "~kvaps", Progress: 100, DoneAt: "2026-08-27"}, // not recurrent
		{ItemID: "bobs", Title: "bobs", Domain: "~bob", Stage: StageRecurrent, Progress: 100, StartDate: "2026-08-27", DoneAt: "2026-08-27"},
		{ItemID: "team", Title: "team", Domain: "aeman-db", Stage: StageRecurrent, Progress: 100, StartDate: "2026-08-27", DoneAt: "2026-08-27"},
	}}
	got := idsOf(PersonalReseed(b, "kvaps", day))
	want := []string{"daily-yesterday", "weekly-due", "weekly-late", "monthly-due"}
	if len(got) != len(want) {
		t.Fatalf("due for reseed = %v, want %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("due for reseed = %v, want %v (board order kept)", got, want)
		}
	}
	// Tomorrow the three finished today join (daily-today, daily-late,
	// weekly-late-today); weekly-rests (due Sep 1) and monthly-rests (due
	// Aug 31) still rest, the reseeded one stays reseeded.
	if got := PersonalReseed(b, "kvaps", "2026-08-29"); len(got) != 7 {
		t.Fatalf("tomorrow = %v", idsOf(got))
	}
	if len(PersonalReseed(b, "nobody", day)) != 0 {
		t.Fatal("a person without a personal repository has nothing to reseed")
	}
}

func idsOf(cards []Card) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.ItemID)
	}
	return out
}
