package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A PERSON creating a card does it on a BOARD, and the board is what makes the
// card what it is. The API asked for the fields instead — `personal`, `week`,
// `parked`, `epic` — so a caller had to know which combinations are boards and
// which are states no board draws. These are the four add-boxes, as the views
// they belong to (docs/design/view-scoped-api.md).
func TestEachBoardCreatesItsOwnKindOfCard(t *testing.T) {
	today := board.TodayIso()
	ctx := WithActor(ctx, "kvaps")
	seed := func() *fakeBackend {
		return newFake(nil, map[string]board.SprintState{"alpha": {Current: today}})
	}

	t.Run("the Me board makes it mine, today, in the sprint", func(t *testing.T) {
		f := seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewMe, CreateCardArgs{Title: "mine", Team: "alpha"})
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Assignees) != 1 || c.Assignees[0] != board.ActorFrom(ctx) {
			t.Fatalf("assignees = %v, want the caller alone", c.Assignees)
		}
		if c.Day != today || c.SprintStart != today {
			t.Fatalf("day/sprint = %q/%q, want today's board", c.Day, c.SprintStart)
		}
	})

	// The lead's grid files for somebody — or for nobody, which is the
	// Unassigned column and a real place on that board.
	t.Run("the Team board files for the team, not for me", func(t *testing.T) {
		f := seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewTeam, CreateCardArgs{Title: "theirs", Team: "alpha"})
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Assignees) != 0 {
			t.Fatalf("assignees = %v, want nobody — the Unassigned column", c.Assignees)
		}
		if c.Team != "alpha" || c.SprintStart != today {
			t.Fatalf("team/sprint = %q/%q", c.Team, c.SprintStart)
		}
	})

	t.Run("a Triage week is a week and no day", func(t *testing.T) {
		f := seed()
		week := board.MondayOf(today)
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewTriage,
			CreateCardArgs{Title: "later", Team: "alpha", Week: week})
		if err != nil {
			t.Fatal(err)
		}
		if c.Week != week {
			t.Fatalf("week = %q, want %q", c.Week, week)
		}
		if c.Day != "" || c.StartDate != "" || c.SprintStart != "" {
			t.Fatalf("a week card stands on a day: %q/%q/%q", c.StartDate, c.Day, c.SprintStart)
		}
	})

	// The week is the whole gesture there: a column with no week is not a
	// column, so the board cannot mean anything by it.
	t.Run("and a Triage create without one is refused", func(t *testing.T) {
		f := seed()
		_, err := f2svc(f).CreateInView(ctx, "acme", board.ViewTriage, CreateCardArgs{Title: "later", Team: "alpha"})
		if !errors.Is(err, ErrViewNeedsField) {
			t.Fatalf("a week card with no week = %v, want ErrViewNeedsField", err)
		}
	})

	t.Run("the drawer parks it", func(t *testing.T) {
		f := seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog, CreateCardArgs{Title: "someday", Team: "alpha"})
		if err != nil {
			t.Fatal(err)
		}
		if !c.Parked || c.Week != "" || c.SprintStart != "" {
			t.Fatalf("parked=%v week=%q sprint=%q, want the shelf alone", c.Parked, c.Week, c.SprintStart)
		}
	})

	t.Run("the personal board files it in my own repository", func(t *testing.T) {
		f := seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewPersonal, CreateCardArgs{Title: "mine alone"})
		if err != nil {
			t.Fatal(err)
		}
		if !board.IsPersonalDomain(c.Domain) {
			t.Fatalf("domain = %q, want the caller's own", c.Domain)
		}
	})

	// Every board refuses what it does not own, and says which field it was:
	// a create that quietly dropped the field would answer with a card that
	// is not the one the caller described.
	t.Run("a board refuses the fields it does not own", func(t *testing.T) {
		for _, tc := range []struct {
			view  board.View
			args  CreateCardArgs
			field string
		}{
			{board.ViewMe, CreateCardArgs{Title: "x", Parked: true}, "parked"},
			{board.ViewMe, CreateCardArgs{Title: "x", Epic: "Auth"}, "epic"},
			{board.ViewTeam, CreateCardArgs{Title: "x", Personal: true}, "personal"},
			{board.ViewTriage, CreateCardArgs{Title: "x", Week: "2026-09-07", Day: "2026-09-08"}, "day"},
			{board.ViewBacklog, CreateCardArgs{Title: "x", Week: "2026-09-07"}, "week"},
			{board.ViewPersonal, CreateCardArgs{Title: "x", Team: "alpha"}, "team"},
			{board.ViewProject, CreateCardArgs{Title: "x", Epic: "Auth", Parked: true}, "parked"},
		} {
			f := seed()
			_, err := f2svc(f).CreateInView(ctx, "acme", tc.view, tc.args)
			if !errors.Is(err, ErrNotOnThisBoard) {
				t.Fatalf("%s + %s = %v, want ErrNotOnThisBoard", tc.view, tc.field, err)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("%s + %s: %q does not name the field", tc.view, tc.field, err)
			}
		}
	})

	// The escape hatch passes everything through, because a caller that says
	// "all" has said it is not standing on a board.
	t.Run("the escape hatch refuses nothing", func(t *testing.T) {
		f := seed()
		if _, err := f2svc(f).CreateInView(ctx, "acme", board.ViewAll,
			CreateCardArgs{Title: "x", Team: "alpha", Parked: true}); err != nil {
			t.Fatalf("view=all = %v, want it taken", err)
		}
	})

	t.Run("a view nobody has is not a board", func(t *testing.T) {
		f := seed()
		if _, err := f2svc(f).CreateInView(ctx, "acme", board.View("process"), CreateCardArgs{Title: "x"}); !errors.Is(err, ErrNoSuchView) {
			t.Fatalf("view=process = %v, want ErrNoSuchView", err)
		}
	})
}

// Which board draws which press. The × is on every board; the others belong to
// the board whose gesture they are, and asking another for one is asking for a
// route that is not there.
func TestAGestureIsOfferedByTheBoardThatDrawsIt(t *testing.T) {
	t.Parallel()
	for _, v := range board.Views() {
		if !Offers(v, GestureRemove) {
			t.Fatalf("%s draws no \u00d7", v)
		}
	}
	for _, tc := range []struct {
		g    Gesture
		view board.View
		want bool
	}{
		{GesturePlace, board.ViewTriage, true},
		{GesturePlace, board.ViewMe, false},
		{GesturePlace, board.ViewTeam, false},
		{GestureUntriage, board.ViewTriage, true},
		{GestureUntriage, board.ViewProject, false},
		{GestureFinishedEarlier, board.ViewMe, true},
		{GestureFinishedEarlier, board.ViewTeam, true},
		{GestureFinishedEarlier, board.ViewTriage, false},
	} {
		if got := Offers(tc.view, tc.g); got != tc.want {
			t.Fatalf("%s on %s = %v, want %v", tc.g, tc.view, got, tc.want)
		}
	}
	// The escape hatch draws every one of them: a caller that said "all" has
	// no board to be refused by.
	for _, g := range []Gesture{GestureRemove, GesturePlace, GestureUntriage, GestureFinishedEarlier} {
		if !Offers(board.ViewAll, g) {
			t.Fatalf("view=all refused %s", g)
		}
	}
	if len(Gestures(board.ViewTriage)) != 3 {
		t.Fatalf("the Triage board draws %v", Gestures(board.ViewTriage))
	}
}
