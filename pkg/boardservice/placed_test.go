package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// K1 IS AN INVARIANT, NOT A HOPE. Every door that can take a task's last
// placement — its last date, its sprint, the parent it rode — leaves the
// task PARKED in its team's backlog in the same request rather than on no
// board at all (an unplaced task is not Reachable). It is a plain park, the
// "back to the backlog" fallback (K3): the person, the band and the rank are
// left as they are — that is what SetBacklog's own emptying is for, and this
// is not that gesture.

// placedFixture is a team whose task "t" stands by one placement only, plus
// a parked parent "p" with a subtask "k" riding it.
func placedFixture(t0 board.Card) *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		t0,
		{ItemID: "p", Title: "parked parent", Team: "alpha", Parked: true, Assignees: []string{"bob"}},
		{ItemID: "k", Title: "its piece", Team: "alpha", Parent: "p"},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
}

// parkedAsIs asserts the task is parked and nothing else about it moved.
func parkedAsIs(t *testing.T, f *fakeBackend, id string, before board.Card) {
	t.Helper()
	c := f.get(id)
	if c == nil {
		t.Fatalf("%s is gone", id)
	}
	if !c.Parked {
		t.Fatalf("%s lost its last placement and was left unplaced, on no board: %+v", id, c)
	}
	if c.Zone != before.Zone || len(c.Assignees) != len(before.Assignees) || c.Team != before.Team {
		t.Fatalf("%s: the fallback park is a plain park — zone, person and team stay: %+v", id, c)
	}
}

func TestClearingATasksLastDateOrSprintParksIt(t *testing.T) {
	today := board.TodayIso()
	for _, tc := range []struct {
		door string
		card board.Card
		do   func(*Service) error
	}{
		{"SetDates to nothing", board.Card{StartDate: today, Day: today},
			func(s *Service) error { return s.SetDates(ctx, "acme", "t", "", "") }},
		{"SetDay to nothing on a card with only an end date", board.Card{Day: today},
			func(s *Service) error { return s.SetDay(ctx, "acme", "t", "") }},
		{"SetStart to nothing on a card with only a start", board.Card{StartDate: today},
			func(s *Service) error { return s.SetStart(ctx, "acme", "t", "") }},
		{"SetSprintStart to nothing on a sprint-only card", board.Card{SprintStart: today},
			func(s *Service) error { return s.SetSprintStart(ctx, "acme", "t", "") }},
	} {
		t.Run(tc.door, func(t *testing.T) {
			c := tc.card
			c.ItemID, c.Title, c.Team, c.Zone, c.Assignees = "t", "work", "alpha", board.ZoneRed, []string{"ann"}
			f := placedFixture(c)
			if err := tc.do(New(f)); err != nil {
				t.Fatal(err)
			}
			parkedAsIs(t, f, "t", c)
			if logged(f, "MoveCard t") {
				t.Fatalf("the fallback park moved the card's rank: %v", f.log)
			}
		})
	}
	// A card that still has another placement is in work, and stays so.
	f := placedFixture(board.Card{ItemID: "t", Title: "work", Team: "alpha", Day: today, SprintStart: today})
	if err := New(f).SetDay(ctx, "acme", "t", ""); err != nil {
		t.Fatal(err)
	}
	if c := f.get("t"); c.Parked {
		t.Fatalf("clearing the end date leaves the card in its sprint — in work, not parked: %+v", c)
	}
}

// A subtask of a parked task stands on no day (it rides its parent), so
// freeing it — pulling it out of the group, or deleting the parent — would
// leave it unplaced. It lands in its team's backlog instead.
func TestAFreedSubtaskWithNoPlaceIsParked(t *testing.T) {
	for _, door := range []string{"SetParent to nothing", "DeleteCard of the parent"} {
		t.Run(door, func(t *testing.T) {
			f := placedFixture(board.Card{ItemID: "t", Title: "unrelated", Team: "alpha", SprintStart: board.TodayIso()})
			svc := New(f)
			var err error
			if door == "SetParent to nothing" {
				err = svc.SetParent(ctx, "acme", "k", "")
			} else {
				err = svc.DeleteCard(ctx, "acme", "p")
			}
			if err != nil {
				t.Fatal(err)
			}
			c := f.get("k")
			if c == nil || c.Parent != "" || !c.Parked {
				t.Fatalf("the freed subtask is not in its backlog: %+v", c)
			}
		})
	}
}

// A sprint-only task grouped under a PARKED parent rides that parent out of
// its sprint; ungrouped again it has nothing left, and is parked.
func TestATaskUngroupedFromAParkedParentIsParked(t *testing.T) {
	today := board.TodayIso()
	f := placedFixture(board.Card{ItemID: "t", Title: "sprint only", Team: "alpha", SprintStart: today})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "t", "p"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("t"); c.SprintStart != "" || c.Parked {
		t.Fatalf("grouped, it rides its parked parent — out of the sprint, not parked itself: %+v", c)
	}
	if err := svc.SetParent(ctx, "acme", "t", ""); err != nil {
		t.Fatal(err)
	}
	if c := f.get("t"); !c.Parked {
		t.Fatalf("ungrouped with no place of its own, it is parked: %+v", c)
	}
}

// A subtask moved out of its group to an EPIC is a task of its own under the
// plan (H1) — and one with no date and no sprint is parked.
func TestASubtaskFiledUnderAnEpicWithNoPlaceIsParked(t *testing.T) {
	f := placedFixture(board.Card{ItemID: "e", Title: "epic", Type: board.TypeEpic})
	if err := New(f).SetParent(ctx, "acme", "k", "e"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("k"); c.Parent != "e" || !c.Parked {
		t.Fatalf("a task under an epic with no place of its own is parked: %+v", c)
	}
}

// Reopening a finished task that never had a date or a sprint makes it OPEN
// work with no place — it is parked, not lost.
func TestReopeningAnUnplacedFinishedTaskParksIt(t *testing.T) {
	for _, tc := range []struct {
		door string
		do   func(*Service) error
	}{
		{"SetProgress below 100", func(s *Service) error { return s.SetProgress(ctx, "acme", "t", 40) }},
		{"Reopen with a recorded jump", func(s *Service) error { return s.Reopen(ctx, "acme", "t") }},
		{"SetInProgress", func(s *Service) error { return s.SetInProgress(ctx, "acme", "t") }},
		{"SetStage to locked", func(s *Service) error { return s.SetStage(ctx, "acme", "t", board.StageLocked) }},
	} {
		t.Run(tc.door, func(t *testing.T) {
			f := placedFixture(board.Card{ItemID: "t", Title: "done long ago", Team: "alpha", Progress: 100, DoneFrom: 30})
			if err := tc.do(New(f)); err != nil {
				t.Fatal(err)
			}
			if c := f.get("t"); !c.Parked {
				t.Fatalf("reopened with no place, it is parked: %+v", c)
			}
		})
	}
}

// A SUBTASK rides its parent: grouping a parked task under a task takes it
// out of its backlog (it is drawn where its parent is), and a backlog create
// naming a TASK as parent is refused — the backlog holds tasks of their own,
// and a subtask parked and riding a parent's sprint would be in two places.
func TestASubtaskIsNeverParked(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "w", Title: "in work", Team: "alpha", SprintStart: today, StartDate: today},
		{ItemID: "q", Title: "parked", Team: "alpha", Parked: true},
		{ItemID: "e", Title: "epic", Type: board.TypeEpic},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "q", "w"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("q"); c.Parked || c.SprintStart != today {
		t.Fatalf("grouped under a task it rides that task, unparked: %+v", c)
	}
	if _, err := svc.CreateInView(ctx, "acme", board.ViewBacklog,
		CreateCardArgs{Title: "x", Team: "alpha", Parent: "w"}); !errors.Is(err, ErrNotOnThisBoard) {
		t.Fatalf("a backlog create under a task = %v, want ErrNotOnThisBoard", err)
	}
	if f.count("CreateCard") != 0 {
		t.Fatal("the refusal comes before the write")
	}
	// Under a planning card it is a task of its own, and parked as asked.
	c, err := svc.CreateInView(ctx, "acme", board.ViewBacklog, CreateCardArgs{Title: "y", Team: "alpha", Parent: "e"})
	if err != nil || !c.Parked || c.Parent != "e" {
		t.Fatalf("a backlog create under an epic = %+v, %v", c, err)
	}
}
