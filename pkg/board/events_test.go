package board

import (
	"reflect"
	"testing"
)

// Every event kind round-trips through the log-line body format.
func TestEventBodyRoundTrip(t *testing.T) {
	events := []Event{
		{Kind: EventCreated, Actor: "kvaps"},
		{Kind: EventStage, Actor: "kvaps", From: "", To: "review"},
		{Kind: EventProgress, Actor: "bob", From: "40", To: "60"},
		{Kind: EventAssignee, Actor: "kvaps", From: "bob", To: "carol"},
		{Kind: EventTeam, Actor: "kvaps", From: "alpha", To: "beta"},
		{Kind: EventZone, Actor: "kvaps", From: "gray", To: "red"},
		{Kind: EventReviewSent, Actor: "kvaps", To: "lllamnyp"},
		{Kind: EventReviewPassed, Actor: "lllamnyp", From: "lllamnyp"},
		{Kind: EventReviewerRemoved, Actor: "kvaps", From: "lllamnyp"},
		{Kind: EventPlanTaken, Actor: "kvaps", To: "dan"},
		{Kind: EventPlanReleased, Actor: "kvaps", From: "wed"},
	}
	for _, e := range events {
		body := FormatEventBody(e)
		got, ok := ParseEventBody(body)
		if !ok {
			t.Fatalf("%s: body %q did not parse as an event", e.Kind, body)
		}
		want := e
		if got.Kind != want.Kind || got.Actor != want.Actor || got.From != want.From || got.To != want.To {
			t.Fatalf("%s round-trip: got %+v want %+v (body %q)", e.Kind, got, want, body)
		}
	}
}

// Pipes inside values cannot break the field framing.
func TestEventBodySanitizesPipes(t *testing.T) {
	body := FormatEventBody(Event{Kind: EventTeam, Actor: "a", From: "x|y", To: "z"})
	got, ok := ParseEventBody(body)
	if !ok || got.From != "x/y" || got.To != "z" {
		t.Fatalf("pipes must be sanitized: %+v (%q)", got, body)
	}
}

// A plain note body is not an event.
func TestParseEventBodyRejectsNotes(t *testing.T) {
	for _, body := range []string{"just a note", ":no", "", "review checklist:\n- item"} {
		if _, ok := ParseEventBody(body); ok {
			t.Fatalf("%q must not parse as an event", body)
		}
	}
}

// PartitionEvents splits a mixed log, keeping ids/timestamps on the events.
func TestPartitionEvents(t *testing.T) {
	notes := []Note{
		{ID: "n1", Body: "human note", CreatedAt: "2026-07-06T10:00:00Z"},
		{ID: "n2", Body: FormatEventBody(Event{Kind: EventProgress, Actor: "kvaps", From: "40", To: "60"}), CreatedAt: "2026-07-06T11:00:00Z"},
		{ID: "n3", Body: "another note", CreatedAt: "2026-07-06T12:00:00Z"},
	}
	keep, events := PartitionEvents(notes)
	if len(keep) != 2 || keep[0].ID != "n1" || keep[1].ID != "n3" {
		t.Fatalf("notes = %+v", keep)
	}
	want := []Event{{ID: "n2", Kind: EventProgress, Actor: "kvaps", From: "40", To: "60", At: "2026-07-06T11:00:00Z"}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
}

// Note author attribution round-trips through the stored body format; legacy
// bodies without it stay unattributed.
func TestNoteAuthorRoundTrip(t *testing.T) {
	body := RenderNoteBody("kvaps", "wrote the plan")
	author, text := SplitNoteAuthor(body)
	if author != "kvaps" || text != "wrote the plan" {
		t.Fatalf("round-trip = %q %q", author, text)
	}
	author, text = SplitNoteAuthor("legacy note body")
	if author != "" || text != "legacy note body" {
		t.Fatalf("legacy = %q %q", author, text)
	}
	if RenderNoteBody("", "bare") != "bare" {
		t.Fatal("empty author stores the bare text")
	}
}

// The weekly history rule against an explicit clock: a worked card moved to a
// later week shows in a week only once that week's Friday has passed — a card
// pushed forward mid-week leaves the running week's panel.
func TestPlanShowsInWeekAt(t *testing.T) {
	worked := Card{Plan: PlanWed, Week: "2026-07-13", StartDate: "2026-06-29", Progress: 30}
	cases := []struct {
		name  string
		card  Card
		week  string
		today string
		want  bool
	}{
		{"own week always shows", worked, "2026-07-13", "2026-07-06", true},
		{"running week hides a forward-moved card (Freedom bandwidth)", worked, "2026-07-06", "2026-07-06", false},
		{"still hidden on that week's Friday", worked, "2026-07-06", "2026-07-10", false},
		{"finished week shows the history", worked, "2026-07-06", "2026-07-11", true},
		{"Sunday after the closing Friday shows it (the carry-week sync case)", worked, "2026-06-29", "2026-07-05", true},
		{"never-started plan card leaves no history", Card{Plan: PlanFri, Week: "2026-07-13"}, "2026-07-06", "2026-07-11", false},
		{"weeks before the work anchor stay empty", worked, "2026-06-22", "2026-07-11", false},
	}
	for _, tc := range cases {
		if got := planShowsInWeekAt(tc.card, tc.week, tc.today); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
