package boardservice

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The day feed asks one question — what happened on this day, across the
// cards it shows — and the answer used to cost a full history read per card.
// DayLogs answers all of them at once and only for the day: the notes come
// from the cards the board already holds, the events from a history walk that
// stops at the day's start.
func TestDayLogsAnswersManyCardsForOneDay(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "a", Title: "a", Team: "portal", Notes: []board.Note{
			{ID: "n1", Body: "yesterday", CreatedAt: "2026-08-27T09:00:00Z"},
			{ID: "n2", Body: "today", CreatedAt: "2026-08-28T09:30:00Z"},
		}},
		{ItemID: "b", Title: "b", Team: "portal"},
		{ItemID: "c", Title: "c", Team: "portal"},
	}, nil)
	fake.events = map[string][]board.Event{
		"a": {
			{ID: "e0", Kind: "created", At: "2026-08-20T09:00:00Z"},
			{ID: "e1", Kind: "progress", At: "2026-08-28T10:00:00Z"},
		},
		"b": {{ID: "e2", Kind: "stage", At: "2026-08-27T10:00:00Z"}},
	}
	svc := New(fake)

	got, err := svc.DayLogs(ctx, "acme", []string{"a", "b", "c", "gone"}, "2026-08-28")
	if err != nil {
		t.Fatal(err)
	}
	// A card with something to say on the day: its note and its event, and
	// nothing from the days around it.
	if len(got["a"].Notes) != 1 || got["a"].Notes[0].ID != "n2" {
		t.Fatalf("a's notes = %+v, want only today's", got["a"].Notes)
	}
	if len(got["a"].Events) != 1 || got["a"].Events[0].ID != "e1" {
		t.Fatalf("a's events = %+v, want only today's", got["a"].Events)
	}
	// A card that was quiet that day is present and empty — the client must
	// be able to tell "asked and nothing happened" from "not asked".
	if _, ok := got["b"]; !ok || len(got["b"].Events) != 0 || len(got["b"].Notes) != 0 {
		t.Fatalf("b = %+v, want present and empty", got["b"])
	}
	if _, ok := got["c"]; !ok {
		t.Fatalf("a card with no history at all must still be answered for")
	}
	// A card the visitor cannot see (or that does not exist) is simply absent.
	if _, ok := got["gone"]; ok {
		t.Fatal("an unknown card must not be in the answer")
	}
	// The store was asked with the day's first moment — in the BOARD's zone,
	// which is what a board day is measured in — not for whole histories.
	if !sawPrefix(fake, "CardLogSince a 2026-08-28T00:00:00") {
		t.Fatalf("the walk must be cut at the day; log = %v", fake.log)
	}
	for _, line := range fake.log {
		if strings.HasPrefix(line, "CardLog ") {
			t.Fatalf("the day feed must not read a whole log; log = %v", fake.log)
		}
	}
}

// sawPrefix reports whether the fake recorded a call starting with s — the
// boundary's zone offset is the board's, not the test's business.
func sawPrefix(f *fakeBackend, s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, line := range f.log {
		if strings.HasPrefix(line, s) {
			return true
		}
	}
	return false
}

// No day means today; no uids means nothing to answer.
func TestDayLogsDefaultsAndEmptyInput(t *testing.T) {
	fake := newFake([]board.Card{{ItemID: "a", Title: "a"}}, nil)
	svc := New(fake)
	got, err := svc.DayLogs(ctx, "acme", nil, "2026-08-28")
	if err != nil || len(got) != 0 {
		t.Fatalf("no uids = %v, %v", got, err)
	}
	if _, err := svc.DayLogs(ctx, "acme", []string{"a"}, ""); err != nil {
		t.Fatalf("an empty day is today, not an error: %v", err)
	}
	if !sawPrefix(fake, "CardLogSince a "+board.TodayIso()+"T00:00:00") {
		t.Fatalf("an empty day must ask about today; log = %v", fake.log)
	}
	if _, err := svc.DayLogs(ctx, "acme", []string{"a"}, "not-a-day"); err == nil {
		t.Fatal("a day that is not a date must be refused")
	}
}
