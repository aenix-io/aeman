package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Parking a card on its team's shelf, and the rules that keep the shelf
// honest. Every team has one and nothing declares it, so there is no list to
// look up and nothing that can be missing.

func backlogBoard() *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		// An ordinary card, in work.
		{ItemID: "plain", Team: "alpha", Assignees: []string{"kvaps"},
			SprintStart: today, StartDate: today, Day: today},
		// A card of the NO-TEAM group, which has a shelf like any other team.
		{ItemID: "loose", Assignees: []string{"kvaps"}},
		// And one of a SECOND team.
		{ItemID: "other", Team: "beta"},
	}, map[string]board.SprintState{
		"alpha": {Current: today, ItemID: "s1"},
		"beta":  {Current: today, ItemID: "s2"},
		"":      {Current: today, ItemID: "s0"},
	})
}

// logged reports whether the fake recorded a call with this prefix.
func logged(f *fakeBackend, prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, line := range f.log {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// Parking takes the card out of work and off the person: a card carrying an
// owner reads as somebody's work when it is not being worked on at all.
func TestParkingTakesTheCardOutOfWorkAndOffThePerson(t *testing.T) {
	f := backlogBoard()
	if err := New(f).SetBacklog(ctx, "acme", "plain", true); err != nil {
		t.Fatal(err)
	}
	c := f.get("plain")
	if c == nil || !board.InBacklog(*c) {
		t.Fatalf("the card is on the shelf: %+v", c)
	}
	if len(c.Assignees) != 0 || c.SprintStart != "" || c.StartDate != "" || c.Day != "" {
		t.Fatalf("parking empties the working area: %+v", c)
	}
}

// Every team has a shelf and nothing declares it, so parking can never be
// refused for want of one — which is the whole reason it is not a list.
func TestEveryTeamsBacklogTakesWorkWithoutBeingDeclared(t *testing.T) {
	f := backlogBoard()
	svc := New(f)
	for _, id := range []string{"plain", "loose", "other"} {
		was := f.get(id).Team
		if err := svc.SetBacklog(ctx, "acme", id, true); err != nil {
			t.Fatalf("%s: parking on its team's shelf = %v", id, err)
		}
		c := f.get(id)
		if !c.Parked {
			t.Fatalf("%s: on the shelf: %+v", id, c)
		}
		// Parking is not a reassignment: the shelf a card lands on is its own
		// team's, so the team is never touched on the way. Moving work to
		// ANOTHER team's shelf is a change of team, and goes through that door.
		if c.Team != was {
			t.Fatalf("%s: the team is untouched, was %q, now %q", id, was, c.Team)
		}
	}
}

// Parking makes the work PLANNED work, whatever zone it was in.
//
// The other three zones are statements about TODAY. Critical means today.
// Unplanned means it turned up today. "If time left" is about a day's spare
// capacity. None of them survives the card being put aside, and a shelf full
// of red is a shelf that lies to whoever opens it — the card was urgent the
// day somebody shelved it, and it has been on the shelf since. What is left
// is ordinary planned work, which is what a shelved card is: work that will be
// planned, some day.
func TestParkingMakesTheWorkPlannedWork(t *testing.T) {
	for _, from := range []board.ZoneKey{board.ZoneRed, board.ZoneYellow, board.ZoneGreen, board.ZoneGray, ""} {
		f := backlogBoard()
		if c := f.get("plain"); c != nil {
			c.Zone = from
		}
		if err := New(f).SetBacklog(ctx, "acme", "plain", true); err != nil {
			t.Fatalf("from %q: %v", from, err)
		}
		if got := f.get("plain").Zone; got != board.ZoneGray {
			t.Fatalf("from %q: zone = %q, want the planned zone", from, got)
		}
	}
	// Coming OFF the shelf changes nothing: the card is planned work now, and
	// whoever schedules it says what kind of day it is.
	g := backlogBoard()
	if c := g.get("plain"); c != nil {
		c.Zone = board.ZoneRed
	}
	svc := New(g)
	if err := svc.SetBacklog(ctx, "acme", "plain", false); err != nil {
		t.Fatal(err)
	}
	if got := g.get("plain").Zone; got != board.ZoneRed {
		t.Fatalf("unparking rezoned the card: %q", got)
	}
}

// A card BORN on the shelf is planned work too — the same rule, at the other
// door. Create does not go through SetBacklog, so a zone asked for here would
// otherwise stand: a card started in the backlog wearing "critical" says
// something about a day nobody is planning.
func TestACardBornOnTheShelfIsPlannedWork(t *testing.T) {
	for _, ask := range []board.ZoneKey{board.ZoneRed, board.ZoneYellow, board.ZoneGreen, ""} {
		f := backlogBoard()
		card, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{
			Title: "Some day", Team: "alpha", Zone: ask, Parked: true,
		})
		if err != nil {
			t.Fatalf("asked for %q: %v", ask, err)
		}
		if !card.Parked {
			t.Fatalf("asked for %q: the card is born on the shelf: %+v", ask, card)
		}
		if card.Zone != board.ZoneGray {
			t.Fatalf("asked for %q: zone = %q, want the planned zone", ask, card.Zone)
		}
	}
}

// A card sent to the backlog KEEPS ITS PLACE in the board's order (B11,
// amended with K14). The backlog reads in the Roadmap's order — one order,
// decided by the tree — so moving a parked card to the top of the board's
// order would move it to the top of its siblings on the Roadmap too: taking
// work out of hand must not rearrange the plan.
func TestParkingKeepsTheCardsPlace(t *testing.T) {
	f := backlogBoard()
	if err := New(f).SetBacklog(ctx, "acme", "plain", true); err != nil {
		t.Fatal(err)
	}
	if logged(f, "MoveCard plain") {
		t.Fatalf("parking moves nothing; calls were: %v", f.log)
	}
	// Taking a card OFF the shelf moves nothing: it is going into work, and
	// its place among the parked work is not the day board's business.
	g := backlogBoard()
	if err := New(g).SetBacklog(ctx, "acme", "plain", false); err != nil {
		t.Fatal(err)
	}
	if logged(g, "MoveCard plain after=") {
		t.Fatalf("unparking reorders nothing; calls were: %v", g.log)
	}
}

// A task leaves its backlog by being TAKEN INTO WORK (K1, K4): parked:false
// alone on a task with no date and no sprint would leave it in neither
// place, so it is refused and names the way out. On a task already in work
// (a direct writer can write both) it simply drops the flag.
func TestUnparkingWithoutWorkIsRefused(t *testing.T) {
	f := backlogBoard()
	svc := New(f)
	if err := svc.SetBacklog(ctx, "acme", "plain", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetBacklog(ctx, "acme", "plain", false); !errors.Is(err, ErrTakeIntoWork) {
		t.Fatalf("unpark with no work = %v, want ErrTakeIntoWork", err)
	}
	if c := f.get("plain"); !board.InBacklog(*c) {
		t.Fatalf("the refusal fires before the write: %+v", c)
	}
	today := board.TodayIso()
	g := newFake([]board.Card{
		{ItemID: "both", Team: "alpha", Parked: true, StartDate: today, Day: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	if err := New(g).SetBacklog(ctx, "acme", "both", false); err != nil {
		t.Fatal(err)
	}
	if c := g.get("both"); board.InBacklog(*c) {
		t.Fatalf("a parked card in work drops the flag: %+v", c)
	}
}

// Every door that TAKES A TASK INTO WORK unparks it (K4): a date set from
// the drawer or a drag onto the Team board, a sprint, a defer, and own with
// a person. It leaves the backlog at once — the backlog never shows work in
// hand — and the day boards draw it.
func TestEveryDoorThatTakesATaskIntoWorkUnparksIt(t *testing.T) {
	today := board.TodayIso()
	for _, tc := range []struct {
		door string
		take func(*Service) error
	}{
		{"SetDates", func(svc *Service) error { return svc.SetDates(ctx, "acme", "plain", today, today) }},
		{"SetStart", func(svc *Service) error { return svc.SetStart(ctx, "acme", "plain", today) }},
		{"SetDay", func(svc *Service) error { return svc.SetDay(ctx, "acme", "plain", today) }},
		{"SetSprintStart", func(svc *Service) error { return svc.SetSprintStart(ctx, "acme", "plain", today) }},
		{"Defer", func(svc *Service) error { return svc.Defer(ctx, "acme", "plain", 1) }},
		{"Own with a person", func(svc *Service) error { return svc.Own(ctx, "acme", "plain", "alpha", "kvaps") }},
	} {
		t.Run(tc.door, func(t *testing.T) {
			f := backlogBoard()
			svc := New(f)
			if err := svc.SetBacklog(ctx, "acme", "plain", true); err != nil {
				t.Fatal(err)
			}
			if err := tc.take(svc); err != nil {
				t.Fatal(err)
			}
			c := f.get("plain")
			if board.InBacklog(*c) {
				t.Fatalf("a task taken into work is still parked: %+v", c)
			}
			if !board.InWork(*c) {
				t.Fatalf("and it is in work: %+v", c)
			}
		})
	}
	// CLEARING a date is not taking anything into work: a parked card stays.
	f := backlogBoard()
	svc := New(f)
	if err := svc.SetBacklog(ctx, "acme", "plain", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDates(ctx, "acme", "plain", "", ""); err != nil {
		t.Fatal(err)
	}
	if c := f.get("plain"); !board.InBacklog(*c) {
		t.Fatalf("clearing dates unparked the card: %+v", c)
	}
}

// Handing a parked task to ANOTHER team keeps it parked (K5): the card's
// team chip is a change of team, and the card lands in that team's backlog — it joins no sprint on the way, as a team change on a card
// in work would.
func TestReteamingAParkedTaskKeepsItInTheBacklog(t *testing.T) {
	for _, door := range []string{"SetTeam", "Own with a team"} {
		f := backlogBoard()
		svc := New(f)
		if err := svc.SetBacklog(ctx, "acme", "plain", true); err != nil {
			t.Fatal(err)
		}
		var err error
		if door == "SetTeam" {
			err = svc.SetTeam(ctx, "acme", "plain", "beta", "")
		} else {
			err = svc.Own(ctx, "acme", "plain", "beta", "")
		}
		if err != nil {
			t.Fatalf("%s: %v", door, err)
		}
		c := f.get("plain")
		if c.Team != "beta" || !board.InBacklog(*c) || board.InWork(*c) {
			t.Fatalf("%s: want it in beta's backlog, out of work: %+v", door, c)
		}
	}
}

// Parking a card takes its REVIEW off the board too.
//
// A review is a question put to a person about work being done now. Parking
// the work withdraws the question: the reviewer should not be left holding a
// card for something nobody is doing. When the work comes back, whoever picks
// it up sends it to review again — a fresh question, to whoever is right then.
//
// It is the same cancellation that leaving the review stage performs, so the
// two doors behave alike: a review nobody has started is taken off the board,
// a review already WORKED ON is left alone (that work is the reviewer's), and
// the cancellation is recorded on the original where a person looks for it.
func TestParkingWithdrawsAnUntouchedReview(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 50,
			SprintStart: today, StartDate: today},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Assignees: []string{"lllamnyp"},
			SprintStart: today, StartDate: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	if err := New(f).SetBacklog(ctx, "acme", "orig", true); err != nil {
		t.Fatal(err)
	}
	if r := f.get("rev"); r != nil {
		t.Fatalf("the review goes with the work it was asking about: %+v", r)
	}
	if c := f.get("orig"); c == nil || !board.InBacklog(*c) {
		t.Fatalf("and the card itself is parked: %+v", c)
	}
}

// A review the reviewer has already put work into is never taken away by
// this — that work is theirs, and the same rule holds wherever a review is
// cancelled.
func TestParkingLeavesAReviewSomebodyHasStarted(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "orig", Team: "alpha", Stage: board.StageReview, Progress: 50,
			SprintStart: today, StartDate: today},
		{ItemID: "rev", Team: "alpha", ReviewOf: "orig", Progress: 40,
			Assignees: []string{"lllamnyp"}, SprintStart: today, StartDate: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	if err := New(f).SetBacklog(ctx, "acme", "orig", true); err != nil {
		t.Fatal(err)
	}
	if r := f.get("rev"); r == nil {
		t.Fatal("a review already worked on stays: that work is the reviewer's")
	}
}
