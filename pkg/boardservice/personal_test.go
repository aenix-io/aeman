package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A personal card is created into the actor's own domain — the service
// names it, the caller only says "personal" — and carries none of the team
// board's placement: no team, no column, no plan band. It is a backlog item
// with a zone, dates and a description.
func TestCreatePersonalCardGoesToTheActorsDomain(t *testing.T) {
	fake := newFake(nil, nil)
	svc := New(fake)
	ctx := board.WithActor(t.Context(), "kvaps")
	c, err := svc.CreateCard(ctx, "o", CreateCardArgs{Title: "read the paper", Zone: board.ZoneYellow, Personal: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.Domain != "~kvaps" {
		t.Fatalf("personal card's domain = %q, want ~kvaps", c.Domain)
	}
	if c.Team != "" || c.SprintStart != "" {
		t.Fatalf("a personal card joins no team and no sprint: %+v", c)
	}
	if len(fake.creates) != 1 || !fake.creates[0].Personal || fake.creates[0].Domain != "~kvaps" {
		t.Fatalf("backend got %+v", fake.creates)
	}
	// It is still a card: zone and description are its own.
	if c.Zone != board.ZoneYellow {
		t.Fatalf("zone = %q", c.Zone)
	}
}

// The team board's placement has no meaning on a personal card, and an
// anonymous caller has no domain to file into.
// The owner reading their personal board is what turns the day over there:
// a finished recurrent card gets its fresh copy — same title, zone and body,
// 0%, assigned to the owner, in the personal domain — once its cycle is due,
// and only once, whatever the number of reads.
func TestReseedPersonalSeedsTheNextIterationOnce(t *testing.T) {
	daily := board.Card{ItemID: "d1", Title: "inbox zero", Domain: "~kvaps", Zone: board.ZoneGreen,
		Stage: board.StageRecurrent, Progress: 100, StartDate: "2026-08-27", Day: "2026-08-27", DoneAt: "2026-08-27",
		Assignees: []string{"kvaps"}, Description: "clear the inbox"}
	weekly := board.Card{ItemID: "w1", Title: "weekly review", Domain: "~kvaps", Zone: board.ZoneYellow,
		Stage: board.StageRecurrent, Recurrence: board.RecurrenceWeek, Progress: 100,
		StartDate: "2026-08-26", DoneAt: "2026-08-26", Assignees: []string{"kvaps"}}
	fake := newFake([]board.Card{daily, weekly}, nil)
	svc := New(fake)
	ctx := board.WithActor(t.Context(), "kvaps")

	n, err := svc.ReseedPersonal(ctx, "o", "kvaps", "2026-08-28")
	if err != nil || n != 1 {
		t.Fatalf("reseed = %d, %v; want 1 (the daily card), log %v", n, err, fake.log)
	}
	if len(fake.creates) != 1 {
		t.Fatalf("creates = %+v", fake.creates)
	}
	in := fake.creates[0]
	if !in.Personal || in.Domain != "~kvaps" || in.Title != "inbox zero" || in.Zone != board.ZoneGreen ||
		in.Start != "2026-08-28" || in.Day != "2026-08-28" || in.Assignee != "kvaps" || in.Team != "" || in.SprintStart != "" {
		t.Fatalf("the copy was created as %+v", in)
	}
	fresh := fake.get("new1")
	if fresh == nil || fresh.Stage != board.StageRecurrent || fresh.Progress != 0 || fresh.Description != "clear the inbox" {
		t.Fatalf("the copy = %+v", fresh)
	}
	if old := fake.get("d1"); old.Progress != 100 || old.DoneAt != "2026-08-27" {
		t.Fatalf("the finished card must stay as it was: %+v", old)
	}

	// The same day again: the copy is there, nothing to do.
	if n, err := svc.ReseedPersonal(ctx, "o", "kvaps", "2026-08-28"); err != nil || n != 0 {
		t.Fatalf("second read of the day reseeded %d, %v", n, err)
	}
	// The next day: the fresh daily copy is open, so it is not due; the
	// weekly card is still resting.
	if n, err := svc.ReseedPersonal(ctx, "o", "kvaps", "2026-08-29"); err != nil || n != 0 {
		t.Fatalf("with the daily copy open, reseeded %d, %v", n, err)
	}
	// A week after the weekly card's start it comes due, with its cycle.
	if n, err := svc.ReseedPersonal(ctx, "o", "kvaps", "2026-09-02"); err != nil || n != 1 {
		t.Fatalf("weekly = %d, %v", n, err)
	}
	if c := fake.get("new2"); c == nil || c.Title != "weekly review" || c.Recurrence != board.RecurrenceWeek || c.Stage != board.StageRecurrent {
		t.Fatalf("the weekly copy = %+v", c)
	}
	// Nobody's board, or no login at all: a no-op, never an error.
	if n, err := svc.ReseedPersonal(ctx, "o", "", "2026-09-02"); err != nil || n != 0 {
		t.Fatalf("no login: %d, %v", n, err)
	}
}

func TestCreatePersonalCardRefusesPlacementAndAnonymity(t *testing.T) {
	fake := newFake(nil, map[string]board.SprintState{"portal": {Current: "2026-08-24", ItemID: "st"}})
	svc := New(fake)
	ctx := board.WithActor(t.Context(), "kvaps")
	for name, args := range map[string]CreateCardArgs{
		"team":   {Title: "x", Personal: true, Team: "portal"},
		"column": {Title: "x", Personal: true, Project: "freedom", Epic: "Docs"},
		"plan":   {Title: "x", Personal: true, Plan: board.PlanWed, Week: "2026-08-24"},
	} {
		if _, err := svc.CreateCard(ctx, "o", args); !errors.Is(err, ErrPersonalPlacement) {
			t.Fatalf("%s on a personal card: err = %v, want ErrPersonalPlacement", name, err)
		}
	}
	if _, err := svc.CreateCard(t.Context(), "o", CreateCardArgs{Title: "x", Personal: true}); err == nil {
		t.Fatal("a personal card without an actor has no domain and must be refused")
	}
	if fake.count("CreateCard") != 0 {
		t.Fatalf("refused creates must write nothing; log = %v", fake.log)
	}
}
