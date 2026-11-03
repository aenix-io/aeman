package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// THE BACKLOG'S INSERT "+" IS ONE CREATE. A card typed into the gap between
// two backlog cards is born there: the create request names its place —
// AFTER the card above it, or BEFORE the card below it (the top of the
// list has nothing above) — and the service creates and places it in the
// same request, one commit, instead of a client creating it last and
// moving it. An anchor the board does not have is refused before anything
// is written, and so is a place given twice. A board whose cards are not a
// ranked list it draws refuses a place by name.
func TestABacklogCreateIsBornWhereItWasTyped(t *testing.T) {
	seed := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "a", Title: "first", Team: "alpha", Parked: true},
			{ItemID: "b", Title: "second", Team: "alpha", Parked: true},
		}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
	}

	t.Run("after the card above it", func(t *testing.T) {
		f := seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog,
			CreateCardArgs{Title: "between", Team: "alpha", After: "a"})
		if err != nil {
			t.Fatal(err)
		}
		if !c.Parked || c.Team != "alpha" {
			t.Fatalf("it is parked on alpha: %+v", c)
		}
		if !logged(f, "MoveCard "+c.ItemID+" after=a") {
			t.Fatalf("it was not placed after «first»; calls: %v", f.log)
		}
	})

	t.Run("before the card below it, the top of the list included", func(t *testing.T) {
		f := seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog,
			CreateCardArgs{Title: "on top", Team: "alpha", Before: "a"})
		if err != nil {
			t.Fatal(err)
		}
		if !logged(f, "MoveCard "+c.ItemID+" after=") {
			t.Fatalf("it was not put at the top; calls: %v", f.log)
		}
		f = seed()
		c, err = f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog,
			CreateCardArgs{Title: "between", Team: "alpha", Before: "b"})
		if err != nil {
			t.Fatal(err)
		}
		if !logged(f, "MoveCard "+c.ItemID+" after=a") {
			t.Fatalf("before «second» is after «first»; calls: %v", f.log)
		}
	})

	t.Run("no place is the last, as before", func(t *testing.T) {
		f := seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog, CreateCardArgs{Title: "last", Team: "alpha"})
		if err != nil {
			t.Fatal(err)
		}
		if logged(f, "MoveCard "+c.ItemID) {
			t.Fatalf("a create with no place moved the card; calls: %v", f.log)
		}
	})

	t.Run("an anchor the board does not have is refused, and nothing is made", func(t *testing.T) {
		f := seed()
		_, err := f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog,
			CreateCardArgs{Title: "x", Team: "alpha", After: "nope"})
		if !errors.Is(err, ErrCardNotFound) {
			t.Fatalf("an unknown anchor = %v, want ErrCardNotFound", err)
		}
		if len(f.b.Cards) != 2 {
			t.Fatalf("a card was made anyway: %d cards", len(f.b.Cards))
		}
	})

	t.Run("a place given twice is refused", func(t *testing.T) {
		f := seed()
		_, err := f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog,
			CreateCardArgs{Title: "x", Team: "alpha", After: "a", Before: "b"})
		if !errors.Is(err, ErrTwoPlaces) {
			t.Fatalf("after and before = %v, want ErrTwoPlaces", err)
		}
	})

	// The Team and Me boards' cells are slices of the same board order: a
	// card typed into the gap between two cards of a cell is born there, in
	// that cell — the person, the team and the band the cell is — and in the
	// create's own request.
	t.Run("a day board's cell takes it where it was typed, its person and band kept", func(t *testing.T) {
		f := newFake([]board.Card{
			{ItemID: "x", Title: "first", Team: "alpha", Assignees: []string{"bob"}, Zone: board.ZoneGray,
				StartDate: board.TodayIso(), Day: board.TodayIso(), SprintStart: board.TodayIso()},
			{ItemID: "y", Title: "second", Team: "alpha", Assignees: []string{"bob"}, Zone: board.ZoneGray,
				StartDate: board.TodayIso(), Day: board.TodayIso(), SprintStart: board.TodayIso()},
		}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewTeam,
			CreateCardArgs{Title: "between", Team: "alpha", Assignee: "bob", Zone: board.ZoneGray, After: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if c.Zone != board.ZoneGray || len(c.Assignees) != 1 || c.Assignees[0] != "bob" || !board.InWork(c) {
			t.Fatalf("born in bob's planned cell, in work: %+v", c)
		}
		if !logged(f, "MoveCard "+c.ItemID+" after=x") {
			t.Fatalf("not placed after «first»; calls: %v", f.log)
		}
		me := board.WithActor(ctx, "bob")
		m, err := f2svc(f).CreateInView(me, "acme", board.ViewMe,
			CreateCardArgs{Title: "came up", Team: "alpha", Zone: board.ZoneYellow, Before: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if m.Zone != board.ZoneYellow || len(m.Assignees) != 1 || m.Assignees[0] != "bob" {
			t.Fatalf("born on bob's Me board, unplanned: %+v", m)
		}
		if !logged(f, "MoveCard "+m.ItemID+" after=") {
			t.Fatalf("not placed above «first»; calls: %v", f.log)
		}
	})

	t.Run("a board that is not a ranked list refuses a place by name", func(t *testing.T) {
		for _, view := range []board.View{board.ViewRoadmap, board.ViewProject} {
			f := seed()
			_, err := f2svc(f).CreateInView(ctx, "acme", view,
				CreateCardArgs{Title: "x", After: "a", Type: map[bool]string{true: board.TypeEpic}[view == board.ViewRoadmap],
					Scope: map[bool]string{true: "Auth"}[view == board.ViewProject]})
			if !errors.Is(err, ErrNotOnThisBoard) {
				t.Fatalf("%s with a place = %v, want ErrNotOnThisBoard", view, err)
			}
		}
	})
}
