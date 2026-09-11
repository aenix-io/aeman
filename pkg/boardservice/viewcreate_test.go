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

	// THE ME BOARD ADDS WORK AS UNPLANNED and in no other band. Something that
	// came up today is unplanned by definition; the other three zones are the
	// PLAN, and the plan is the lead's to make on the Team board — a person
	// filing their own work under Urgent or Planned is planning, on a board
	// with no room to argue with it. The browser has drawn its add form in
	// that one band all along (web/src/meboard.ts, ADD_ZONE); an agent could
	// type into any of them.
	t.Run("the Me board adds work as unplanned", func(t *testing.T) {
		f := seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewMe, CreateCardArgs{Title: "came up", Team: "alpha"})
		if err != nil {
			t.Fatal(err)
		}
		if c.Zone != board.ZoneYellow {
			t.Fatalf("zone = %q, want the unplanned band", c.Zone)
		}
	})

	t.Run("and refuses the bands the plan owns", func(t *testing.T) {
		for _, zone := range []board.ZoneKey{board.ZoneRed, board.ZoneGray, board.ZoneGreen} {
			f := seed()
			_, err := f2svc(f).CreateInView(ctx, "acme", board.ViewMe,
				CreateCardArgs{Title: "planning", Team: "alpha", Zone: zone})
			if !errors.Is(err, ErrNotOnThisBoard) {
				t.Fatalf("%s typed into the Me board = %v, want ErrNotOnThisBoard", zone, err)
			}
		}
		// Asking for the band it adds in anyway is not a refusal.
		f := seed()
		if _, err := f2svc(f).CreateInView(ctx, "acme", board.ViewMe,
			CreateCardArgs{Title: "came up", Team: "alpha", Zone: board.ZoneYellow}); err != nil {
			t.Fatalf("the unplanned band = %v, want it taken", err)
		}
	})

	// The personal column stands beside the Me day and shares its add form, so
	// it shares the band. The LEAD's grid is where the other three are typed:
	// planning is what that board is for.
	t.Run("the personal column follows it, the team's grid does not", func(t *testing.T) {
		f := seed()
		if _, err := f2svc(f).CreateInView(ctx, "acme", board.ViewPersonal,
			CreateCardArgs{Title: "mine", Zone: board.ZoneGray}); !errors.Is(err, ErrNotOnThisBoard) {
			t.Fatalf("a planned personal card = %v, want ErrNotOnThisBoard", err)
		}
		f = seed()
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewTeam,
			CreateCardArgs{Title: "planned work", Team: "alpha", Zone: board.ZoneGray})
		if err != nil {
			t.Fatalf("the lead planning on their own grid = %v, want it taken", err)
		}
		if c.Zone != board.ZoneGray {
			t.Fatalf("zone = %q, want the planned band", c.Zone)
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

// THE × ON THE ME BOARD IS NARROWER THAN THE ONE ON THE TEAM'S GRID, and only
// the browser knew (web/src/meboard.ts, mayRemove). A person removes from
// their own board what they put there themselves and what the plan has not
// taken up: their own card, still standing in the band this board adds in.
// Anything else is somebody's plan — work another person scheduled for them,
// or work they scheduled on the Team or Triage board, where that × lives —
// and their answer to work they will not do is the refused stage, which
// leaves the card standing for their lead.
func TestTheMeBoardsRemovalIsTheNarrowOne(t *testing.T) {
	me := WithActor(ctx, "kvaps")
	today := board.TodayIso()
	seed := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "mine", Title: "came up today", Team: "alpha", Author: "kvaps",
				Assignees: []string{"kvaps"}, Zone: board.ZoneYellow, Week: board.MondayOf(today),
				StartDate: today, Day: today, SprintStart: today},
			{ItemID: "planned", Title: "in the plan", Team: "alpha", Author: "kvaps",
				Assignees: []string{"kvaps"}, Zone: board.ZoneGray, Week: board.MondayOf(today),
				StartDate: today, Day: today, SprintStart: today},
		}, map[string]board.SprintState{"alpha": {Current: today}})
	}

	t.Run("takes a card this person added here", func(t *testing.T) {
		f := seed()
		if err := f2svc(f).Remove(me, "acme", "mine", board.ViewMe, Unassign); err != nil {
			t.Fatalf("removing my own unplanned card = %v, want it taken", err)
		}
	})

	// The lead moved it into the plan: it is the plan's now, and the answer to
	// "I am not doing this" is the refused stage.
	t.Run("leaves a card the plan has taken up", func(t *testing.T) {
		f := seed()
		if err := f2svc(f).Remove(me, "acme", "planned", board.ViewMe, Unassign); !errors.Is(err, ErrNotYoursToRemove) {
			t.Fatalf("removing planned work from my own board = %v, want ErrNotYoursToRemove", err)
		}
	})

	// The same card, from the board where planning is done, is the lead's to
	// take off — that × is the wide one and always was.
	t.Run("and the team's grid takes it", func(t *testing.T) {
		f := seed()
		if err := f2svc(f).Remove(me, "acme", "planned", board.ViewTeam, Unassign); err != nil {
			t.Fatalf("the team board's × on planned work = %v, want it taken", err)
		}
	})

	// A card of the person's OWN personal board is all theirs, whatever band
	// it stands in: there is no lead's plan there to be unmade, and the
	// column beside the Me day draws its × on every card.
	t.Run("a personal card is all its owner's", func(t *testing.T) {
		f := seed()
		f.b.Cards = append(f.b.Cards, board.Card{ItemID: "own", Title: "read the paper",
			Author: "kvaps", Assignees: []string{"kvaps"}, Zone: board.ZoneGray,
			Domain: board.PersonalDomain("kvaps"), StartDate: today, Day: today})
		if err := f2svc(f).Remove(me, "acme", "own", board.ViewMe, RemoveAuto); err != nil {
			t.Fatalf("removing my own personal card = %v, want it taken", err)
		}
	})

	// A SUBTASK is out of the rule's reach: it is a piece of the card it hangs
	// under rather than work assigned to anyone.
	t.Run("a subtask is a piece of its parent, not somebody's plan", func(t *testing.T) {
		f := seed()
		f.b.Cards = append(f.b.Cards, board.Card{ItemID: "step", Title: "a step", Team: "alpha",
			Parent: "planned", Author: "carol", Zone: board.ZoneGray, StartDate: today, Day: today, SprintStart: today})
		if err := f2svc(f).Remove(me, "acme", "step", board.ViewMe, RemoveAuto); err != nil {
			t.Fatalf("removing a subtask from my own board = %v, want it taken", err)
		}
	})
}
