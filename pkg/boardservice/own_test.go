package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// OWN is the Roadmap's one answer to "whose is this": a team, and a person on
// that team, or neither (docs/design/hierarchy.md "Owner", H10). What the
// answer DOES depends on whether the card has a plan — a day, a week or a
// sprint — because the Roadmap is where loose work gets a home, and a home
// is somewhere the day boards draw.

func ownSeed(today string) *fakeBackend {
	lastWeek := board.AddDays(board.MondayOf(today), -7)
	return newFake([]board.Card{
		// In the strip: nobody has said when, or whose.
		{ItemID: "strip", Title: "Arrived"},
		{ItemID: "strip-sub", Title: "Arrived piece", Parent: "strip"},
		// Planned: dated last week, on beta's board, in beta's sprint.
		{ItemID: "planned", Title: "Underway", Team: "beta", Assignees: []string{"old"},
			StartDate: lastWeek, Day: lastWeek, SprintStart: lastWeek, Zone: board.ZoneRed},
		// Parked on alpha's shelf.
		{ItemID: "parked", Title: "Someday", Team: "alpha", Parked: true, Zone: board.ZoneGray},
		// The kinds OWN refuses.
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "slot", Title: "Legal", Scope: "Compliance", Project: "eu"},
		{ItemID: "review", Title: "review: Underway", ReviewOf: "planned", Team: "beta", Assignees: []string{"rev"},
			StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{
		"alpha": {Current: today},
		"beta":  {Current: lastWeek},
	})
}

func TestOwnGivesLooseWorkAHome(t *testing.T) {
	today := board.TodayIso()
	ctx := WithActor(ctx, "kvaps")

	t.Run("a person on an unplanned card files it like a Me-board create", func(t *testing.T) {
		f := ownSeed(today)
		if err := f2svc(f).Own(ctx, "acme", "strip", "alpha", "ann"); err != nil {
			t.Fatal(err)
		}
		c := *f.get("strip")
		if c.Team != "alpha" || len(c.Assignees) != 1 || c.Assignees[0] != "ann" {
			t.Fatalf("owner = %s/%v", c.Team, c.Assignees)
		}
		// Today, in the unplanned band, in the team's sprint, off the shelf:
		// exactly what typing it into the Me board would have made.
		if c.StartDate != today || c.Day != today || c.SprintStart != today || c.Zone != board.ZoneYellow || c.Parked {
			t.Fatalf("filed as %s..%s sprint=%s zone=%s parked=%t", c.StartDate, c.Day, c.SprintStart, c.Zone, c.Parked)
		}
		// Its subtask follows: the team and the person, as on the Team board.
		sub := *f.get("strip-sub")
		if sub.Team != "alpha" || len(sub.Assignees) != 1 || sub.Assignees[0] != "ann" {
			t.Fatalf("the subtask was left behind: %s/%v", sub.Team, sub.Assignees)
		}
	})

	t.Run("a person on a parked card takes it off the shelf and files it", func(t *testing.T) {
		f := ownSeed(today)
		if err := f2svc(f).Own(ctx, "acme", "parked", "alpha", "ann"); err != nil {
			t.Fatal(err)
		}
		c := *f.get("parked")
		if c.Parked || c.StartDate != today || c.Zone != board.ZoneYellow || c.Assignees[0] != "ann" {
			t.Fatalf("still shelved or not filed: %+v", c)
		}
	})

	t.Run("a person on a planned card changes hands and keeps the plan", func(t *testing.T) {
		f := ownSeed(today)
		before := *f.get("planned")
		if err := f2svc(f).Own(ctx, "acme", "planned", "alpha", "ann"); err != nil {
			t.Fatal(err)
		}
		c := *f.get("planned")
		if c.Team != "alpha" || c.Assignees[0] != "ann" {
			t.Fatalf("owner = %s/%v", c.Team, c.Assignees)
		}
		if c.StartDate != before.StartDate || c.Day != before.Day || c.Zone != before.Zone {
			t.Fatalf("the plan was rewritten: %s..%s %s", c.StartDate, c.Day, c.Zone)
		}
		// Moving team joins that team's sprint, as the Team board's team menu does.
		if c.SprintStart != today {
			t.Fatalf("sprint = %s, want alpha's current %s", c.SprintStart, today)
		}
	})

	t.Run("a team alone on an unplanned card parks it on that team's shelf", func(t *testing.T) {
		f := ownSeed(today)
		if err := f2svc(f).Own(ctx, "acme", "strip", "alpha", ""); err != nil {
			t.Fatal(err)
		}
		c := *f.get("strip")
		if c.Team != "alpha" || len(c.Assignees) != 0 || !c.Parked {
			t.Fatalf("owner = %s/%v parked=%t", c.Team, c.Assignees, c.Parked)
		}
		// On the shelf and on no day: no dates, no sprint — the Triage
		// backlog of alpha is where it waits (H9).
		if c.StartDate != "" || c.Day != "" || c.SprintStart != "" {
			t.Fatalf("a parked card got a plan: %s..%s sprint=%s", c.StartDate, c.Day, c.SprintStart)
		}
	})

	t.Run("a team alone on a planned card moves it and takes the person off", func(t *testing.T) {
		f := ownSeed(today)
		if err := f2svc(f).Own(ctx, "acme", "planned", "alpha", ""); err != nil {
			t.Fatal(err)
		}
		c := *f.get("planned")
		if c.Team != "alpha" || len(c.Assignees) != 0 || c.Parked || c.StartDate == "" {
			t.Fatalf("got %+v", c)
		}
	})

	t.Run("nobody on a parked card hands it to the strip", func(t *testing.T) {
		f := ownSeed(today)
		if err := f2svc(f).Own(ctx, "acme", "parked", "", ""); err != nil {
			t.Fatal(err)
		}
		c := *f.get("parked")
		if c.Team != "" || len(c.Assignees) != 0 || c.Parked || c.SprintStart != "" {
			t.Fatalf("not the strip's: %+v", c)
		}
	})

	t.Run("nobody on a planned card keeps its plan in the no-team group", func(t *testing.T) {
		f := ownSeed(today)
		if err := f2svc(f).Own(ctx, "acme", "planned", "", ""); err != nil {
			t.Fatal(err)
		}
		c := *f.get("planned")
		if c.Team != "" || len(c.Assignees) != 0 || c.StartDate == "" {
			t.Fatalf("got %+v", c)
		}
	})

	t.Run("a person needs a team", func(t *testing.T) {
		if err := f2svc(ownSeed(today)).Own(ctx, "acme", "strip", "", "ann"); !errors.Is(err, ErrOwnerNeedsTeam) {
			t.Fatalf("got %v, want ErrOwnerNeedsTeam", err)
		}
	})

	t.Run("a planning card, a project card, a subtask and a review card have no owner of their own", func(t *testing.T) {
		for id, want := range map[string]error{
			"epic":      ErrPlanningCard,
			"slot":      ErrProjectCard,
			"strip-sub": ErrOwnerFollowsLink,
			"review":    ErrOwnerFollowsLink,
		} {
			if err := f2svc(ownSeed(today)).Own(ctx, "acme", id, "alpha", "ann"); !errors.Is(err, want) {
				t.Errorf("own %s = %v, want %v", id, err, want)
			}
		}
	})
}

// The gesture is a BOARD's: the Roadmap's first, and every board that draws
// a task offers it too, so the drawer can ask the same question from
// wherever the card was opened. The Project board draws no task.
func TestOwnIsOfferedWhereATaskIsDrawn(t *testing.T) {
	t.Parallel()
	for _, v := range []board.View{board.ViewRoadmap, board.ViewMe, board.ViewTeam, board.ViewTriage, board.ViewBacklog, board.ViewAll} {
		if !Offers(v, GestureOwn) {
			t.Errorf("%s does not offer own", v)
		}
	}
	if Offers(board.ViewProject, GestureOwn) {
		t.Error("the Project board offers own, and draws no task")
	}
	if got := Gestures(board.ViewRoadmap); len(got) != 2 || got[0] != GestureRemove || got[1] != GestureOwn {
		t.Fatalf("the Roadmap draws %v, want remove, own", got)
	}
}
