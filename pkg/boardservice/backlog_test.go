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
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "core"},
		{ItemID: "ep", Title: board.EpicStateTitle, Epic: "Auth", Project: "core"},
		// An ordinary card, in the working area and scheduled for a week.
		{ItemID: "plain", Team: "alpha", Assignees: []string{"kvaps"},
			Week: board.MondayOf(today), SprintStart: today, StartDate: today, Day: today},
		// A Project-board slot and a process turn: neither is this board's to park.
		{ItemID: "slot", Team: "alpha", Project: "core", Epic: "Auth", StartDate: today, Day: today},
		{ItemID: "turn", Team: "alpha", Task: "t1", Week: board.MondayOf(today)},
		// A card of the NO-TEAM group, which has a shelf like any other team.
		{ItemID: "loose", Assignees: []string{"kvaps"}, Week: board.MondayOf(today)},
		// And one of a SECOND team.
		{ItemID: "other", Team: "beta", Week: board.MondayOf(today)},
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

// Parking takes the card out of the week and off the person: a card carrying
// an owner reads as somebody's work when it is not being worked on at all.
func TestParkingTakesTheCardOutOfTheWeekAndOffThePerson(t *testing.T) {
	f := backlogBoard()
	if err := New(f).SetBacklog(ctx, "acme", "plain", true); err != nil {
		t.Fatal(err)
	}
	c := f.get("plain")
	if c == nil || !board.InBacklog(*c) {
		t.Fatalf("the card is on the shelf: %+v", c)
	}
	if c.Week != "" {
		t.Fatalf("a card is on the shelf or in a week, never both: %+v", c)
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

// A card sent to the backlog lands at the TOP of it.
//
// A shelf nobody has sorted reads oldest-first, which answers "what has been
// waiting longest". That is the right answer for a shelf being worked THROUGH,
// and the wrong one for a card just put there: the thing somebody has this
// moment decided to keep is the thing they will look for, and it would go to
// the bottom of a hundred older ones. So parking places the card by hand — the
// same rank a card dragged to the top gets — and the ordering rule does the
// rest (BacklogOrder: by hand first, by age after).
func TestParkingPutsTheCardAtTheTopOfTheShelf(t *testing.T) {
	f := backlogBoard()
	if err := New(f).SetBacklog(ctx, "acme", "plain", true); err != nil {
		t.Fatal(err)
	}
	if !logged(f, "MoveCard plain after=") {
		t.Fatalf("parking asks for the top of the order; calls were: %v", f.log)
	}
	// Taking a card OFF the shelf moves nothing: it is going back to the strip
	// to be given a week, and its place among the parked work is not something
	// the strip has an opinion about.
	g := backlogBoard()
	if err := New(g).SetBacklog(ctx, "acme", "plain", false); err != nil {
		t.Fatal(err)
	}
	if logged(g, "MoveCard plain after=") {
		t.Fatalf("unparking reorders nothing; calls were: %v", g.log)
	}
}

// And taking it back off the shelf is the other half: the card returns to the
// strip, where somebody says when it is due.
func TestUnparkingReturnsTheCardToTheStrip(t *testing.T) {
	f := backlogBoard()
	svc := New(f)
	if err := svc.SetBacklog(ctx, "acme", "plain", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetBacklog(ctx, "acme", "plain", false); err != nil {
		t.Fatal(err)
	}
	if c := f.get("plain"); c == nil || board.InBacklog(*c) {
		t.Fatalf("the card comes off the shelf: %+v", c)
	}
}

// Every door that GIVES a card a week takes it off its shelf, not just the one
// that happens to be called SetWeek.
//
// This is the rule's second home, and it was missing: the Triage board places
// a card with Place — it has its own dates work to do — which wrote the week
// straight to the backend and left `parked` standing. The card then drew in
// its week AND in the drawer, which is the exact double-drawing the
// exclusivity exists to prevent, and it is what a person sees when they drag
// work out of the backlog and watch it stay there.
func TestEveryDoorThatGivesAWeekTakesTheCardOffItsShelf(t *testing.T) {
	week := board.MondayOf(board.TodayIso())
	for _, tc := range []struct {
		door string
		give func(*Service) error
	}{
		{"SetWeek", func(svc *Service) error { return svc.SetWeek(ctx, "acme", "plain", week) }},
		{"Place", func(svc *Service) error { return svc.Place(ctx, "acme", "plain", week) }},
	} {
		t.Run(tc.door, func(t *testing.T) {
			f := backlogBoard()
			svc := New(f)
			if err := svc.SetBacklog(ctx, "acme", "plain", true); err != nil {
				t.Fatal(err)
			}
			if err := tc.give(svc); err != nil {
				t.Fatal(err)
			}
			c := f.get("plain")
			if board.InBacklog(*c) {
				t.Fatalf("a card given a week is not still parked: %+v", c)
			}
			if c.Week != week {
				t.Fatalf("and it has the week it was given: %+v", c)
			}
		})
	}
}

// Grouping is the third door: a parent with no week of its own takes the
// subtask's, and a PARKED parent taking one would stand in that week while
// still sitting on the shelf.
func TestGroupingUnderAParkedParentTakesTheParentOffItsShelf(t *testing.T) {
	week := board.MondayOf(board.TodayIso())
	f := backlogBoard()
	svc := New(f)
	if err := svc.SetBacklog(ctx, "acme", "plain", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetWeek(ctx, "acme", "other", week); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetParent(ctx, "acme", "other", "plain"); err != nil {
		t.Fatal(err)
	}
	p := f.get("plain")
	if p.Week != "" && board.InBacklog(*p) {
		t.Fatalf("a parent given its subtask's week comes off its shelf: %+v", p)
	}
}

// A PROJECT card and a PROCESS TURN are not this board's to park. Their week
// is another board's to say — a slot's follows its dates, a turn's is its
// process's record of what that week was owed — so parking one would take it
// out of a plan this board does not own.
func TestProjectAndProcessWorkIsNotParked(t *testing.T) {
	for _, id := range []string{"slot", "turn"} {
		f := backlogBoard()
		err := New(f).SetBacklog(ctx, "acme", id, true)
		if !errors.Is(err, ErrNotYoursToPark) {
			t.Fatalf("%s parked = %v, want ErrNotYoursToPark", id, err)
		}
		if c := f.get(id); board.InBacklog(*c) {
			t.Fatalf("%s: the refusal fires before the write: %+v", id, c)
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
			Week: board.MondayOf(today), SprintStart: today, StartDate: today},
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
			Week: board.MondayOf(today), SprintStart: today, StartDate: today},
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
