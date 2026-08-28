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

func idsOf(cards []Card) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.ItemID)
	}
	return out
}
