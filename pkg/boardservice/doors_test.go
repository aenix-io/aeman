package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// EVERY DOOR PASSES THROUGH THE SERVICE (ADR 0002). These rules were written
// in the HTTP handler, where only one of the two doors passes: the browser
// obeyed them and MCP walked straight past. An agent could file a card with
// no title, defer one BACKWARDS, send a card to review with nobody to review
// it, and leave an empty note — none of which a person can do with a mouse.
//
// They are not view rules. A card with no title is wrong on every board, so
// the check belongs where every caller arrives.
func TestTheServiceHoldsTheRulesTheHandlerHeld(t *testing.T) {
	seed := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "c1", Title: "the work", Team: "alpha", Assignees: []string{"kvaps"},
				StartDate: "2026-09-11", Day: "2026-09-11", SprintStart: "2026-09-11"},
		}, map[string]board.SprintState{"alpha": {Current: "2026-09-11"}})
	}

	t.Run("a card needs a title", func(t *testing.T) {
		f := seed()
		if _, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{Team: "alpha"}); err == nil {
			t.Fatal("a card with no title was created; nothing on a board can be named nothing")
		}
	})

	t.Run("defer moves a card forward, never back", func(t *testing.T) {
		f := seed()
		before := f.get("c1").StartDate
		err := f2svc(f).Defer(ctx, "acme", "c1", -5)
		if err == nil {
			t.Fatalf("defer -5 was accepted and moved the card to %s; putting work OFF is the whole gesture",
				f.get("c1").StartDate)
		}
		if got := f.get("c1").StartDate; got != before {
			t.Fatalf("a refused defer still moved the card: %s → %s", before, got)
		}
	})

	t.Run("a review needs somebody to review it", func(t *testing.T) {
		f := seed()
		if _, err := f2svc(f).SendToReview(ctx, "acme", "c1", "", "2026-09-11", ""); err == nil {
			t.Fatal("a review card was created with no reviewer; it is the artefact of asking somebody")
		}
	})

	t.Run("a note needs words", func(t *testing.T) {
		f := seed()
		if err := f2svc(f).AddNote(ctx, "acme", "c1", "   "); err == nil {
			t.Fatal("an empty note was filed")
		}
	})

	// Sending a card to review TWICE is how a reviewer is changed — the
	// handler folded that in by hand, so MCP grew a second review card on
	// the same original instead. Two reviewers for one card is a state no
	// board can draw.
	t.Run("sending again reassigns rather than growing a second review", func(t *testing.T) {
		f := seed()
		svc := f2svc(f)
		if _, err := svc.SendToReview(ctx, "acme", "c1", "carol", "2026-09-11", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SendToReview(ctx, "acme", "c1", "dave", "2026-09-11", ""); err != nil {
			t.Fatal(err)
		}
		reviews := 0
		var who []string
		bd, err := f.LoadBoard(ctx, "acme")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range bd.Cards {
			if c.ReviewOf == "c1" {
				reviews++
				who = append(who, c.Assignees...)
			}
		}
		if reviews != 1 {
			t.Fatalf("review cards on one original = %d %v, want one reassigned", reviews, who)
		}
		if len(who) != 1 || who[0] != "dave" {
			t.Fatalf("the review is on %v, want dave", who)
		}
	})

	// A size the scale does not know is refused when SET; it was waved
	// through on CREATE, so the same value arrived by the other door and
	// weighed nothing at all on every board that sums points.
	t.Run("a size the scale does not know is refused on create too", func(t *testing.T) {
		f := seed()
		_, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{
			Title: "sized wrong", Team: "alpha", Size: board.SizeKey("large"),
		})
		if err == nil {
			t.Fatal("an unknown size was stored by the create door")
		}
		if !errors.Is(err, ErrUnknownSize) && !strings.Contains(err.Error(), "size") {
			t.Fatalf("the refusal must name the size: %v", err)
		}
	})

	// The calendar cannot produce it and Defer refuses it, but SetDates wrote
	// whatever it was given: a card due before it begins is overdue for ever.
	t.Run("an end never lands before the start", func(t *testing.T) {
		f := seed()
		if err := f2svc(f).SetDates(ctx, "acme", "c1", "2026-09-20", "2026-09-01"); err == nil {
			c := f.get("c1")
			t.Fatalf("end before start was stored: start %s day %s", c.StartDate, c.Day)
		}
	})
}
