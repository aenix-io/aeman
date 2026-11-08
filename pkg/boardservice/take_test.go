package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// K11 — TAKE is the backlog's own gesture: "I take this into work". The
// actor is whoever makes the request — the identity every write records as
// its author — and the card becomes theirs on its OWN team (the no-team
// group's card stays in it): a parked task is taken into work as `own` with
// a person takes it (today, the unplanned band, the team's sprint, out of
// the backlog), and a review nobody has taken makes the actor its reviewer.
// It is refused where there is nothing to take: a planning card, a project
// card, a subtask, a review somebody already has, finished work, and a task
// already in work.

func takeSeed() *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		{ItemID: "p", Title: "parked", Team: "alpha", Parked: true},
		{ItemID: "n", Title: "no team", Parked: true},
		{ItemID: "w", Title: "in work", Team: "alpha", Assignees: []string{"ann"}, StartDate: today, Day: today, SprintStart: today},
		{ItemID: "t", Title: "on review", Team: "alpha", Stage: board.StageReview, Progress: 50, SprintStart: today, StartDate: today},
		{ItemID: "r", Title: "review: on review", Team: "alpha", ReviewOf: "t", Parked: true},
		{ItemID: "t2", Title: "on review too", Team: "alpha", Stage: board.StageReview, Progress: 50, SprintStart: today, StartDate: today},
		{ItemID: "r2", Title: "review: on review too", Team: "alpha", ReviewOf: "t2", Assignees: []string{"bob"}, SprintStart: today, StartDate: today},
		{ItemID: "e", Title: "epic", Type: board.TypeEpic},
		{ItemID: "k", Title: "a piece", Team: "alpha", Parent: "w"},
		{ItemID: "d", Title: "done", Team: "alpha", Parked: true, Progress: 100},
		// Parked with somebody named on it (a direct writer, an old file): it
		// is somebody's, not the taker's.
		{ItemID: "h", Title: "somebody's", Team: "alpha", Parked: true, Assignees: []string{"ann"}},
		{ItemID: "m", Title: "mine already", Team: "alpha", Parked: true, Assignees: []string{"kvaps"}},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}, "": {Current: today, ItemID: "s0"}})
}

func TestTakeMakesAParkedTaskTheActorsOnItsOwnTeam(t *testing.T) {
	today := board.TodayIso()
	me := WithActor(ctx, "kvaps")
	for _, id := range []string{"p", "n"} {
		f := takeSeed()
		was := f.get(id).Team
		if err := New(f).Take(me, "acme", id); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		c := f.get(id)
		if len(c.Assignees) != 1 || c.Assignees[0] != "kvaps" || c.Team != was {
			t.Fatalf("%s: taken = %s/%v, want %q/kvaps", id, c.Team, c.Assignees, was)
		}
		if c.Parked || c.StartDate != today || c.SprintStart == "" || c.Zone != board.ZoneYellow {
			t.Fatalf("%s: taken into work today in the team's sprint, out of the backlog: %+v", id, c)
		}
	}
}

func TestTakeMakesTheActorTheReviewerOfAReviewNobodyHadTaken(t *testing.T) {
	f := takeSeed()
	if err := New(f).Take(WithActor(ctx, "kvaps"), "acme", "r"); err != nil {
		t.Fatal(err)
	}
	c := f.get("r")
	if len(c.Assignees) != 1 || c.Assignees[0] != "kvaps" || c.Parked || !board.InWork(*c) {
		t.Fatalf("the actor reviews it, in work: %+v", c)
	}
}

func TestTakeIsRefusedWhereThereIsNothingToTake(t *testing.T) {
	me := WithActor(ctx, "kvaps")
	for id, want := range map[string]error{
		"w":  ErrAlreadyInWork,
		"r2": ErrOwnerFollowsLink,
		"e":  ErrPlanningCard,
		"k":  ErrOwnerFollowsLink,
		"d":  ErrAlreadyFinished,
		"h":  ErrSomebodysCard,
	} {
		f := takeSeed()
		before := *f.get(id)
		if err := New(f).Take(me, "acme", id); !errors.Is(err, want) {
			t.Errorf("take %s = %v, want %v", id, err, want)
		}
		if c := f.get(id); len(c.Assignees) != len(before.Assignees) || c.Parked != before.Parked {
			t.Errorf("take %s wrote before it refused: %+v", id, c)
		}
	}
	// A parked card already named on the taker is still theirs to take.
	if err := New(takeSeed()).Take(me, "acme", "m"); err != nil {
		t.Fatalf("take a parked card named on me = %v", err)
	}
	// Nobody to take it: a request with no actor.
	if err := New(takeSeed()).Take(ctx, "acme", "p"); !errors.Is(err, ErrNobodyToTake) {
		t.Fatalf("take with no actor = %v, want ErrNobodyToTake", err)
	}
}

func TestTakeIsTheBacklogsGesture(t *testing.T) {
	t.Parallel()
	for _, v := range []board.View{board.ViewBacklog, board.ViewAll} {
		if !Offers(v, GestureTake) {
			t.Errorf("%s does not offer take", v)
		}
	}
	for _, v := range []board.View{board.ViewMe, board.ViewTeam, board.ViewStrategy} {
		if Offers(v, GestureTake) {
			t.Errorf("%s offers take; it is the backlog's", v)
		}
	}
}
