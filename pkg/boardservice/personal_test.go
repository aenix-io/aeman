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
