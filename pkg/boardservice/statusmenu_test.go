package boardservice

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Strategy board's badge menu: UNASSIGNED, which takes work out of hand and
// back to its backlog (K16) — for a task, or every task in work under a plan
// item, at any depth.

func strategyWorkBoard() *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		{ItemID: "g", Type: board.TypeGoal, Team: "alpha"},
		{ItemID: "e", Type: board.TypeEpic, Parent: "g"},
		// Parked, open: waiting in a backlog.
		{ItemID: "p1", Parent: "e", Parked: true},
		{ItemID: "p2", Parent: "g", Parked: true},
		// In work: what UNASSIGNED is for.
		{ItemID: "w1", Parent: "e", Team: "alpha", Assignees: []string{"kvaps"}, SprintStart: today, StartDate: today, Day: today},
		{ItemID: "w2", Parent: "g", Team: "alpha", Assignees: []string{"bob"}, SprintStart: today, Day: today},
		// Finished: neither touches it, wherever it stands.
		{ItemID: "done-parked", Parent: "e", Parked: true, Progress: 100},
		{ItemID: "done-work", Parent: "e", SprintStart: today, Day: today, Progress: 100},
		// A subtask rides its parent: neither is its to answer.
		{ItemID: "sub", Parent: "w1", SprintStart: today},
		// Not under the goal at all.
		{ItemID: "other", Parked: true},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
}

func TestUnassignedOnATaskParksIt(t *testing.T) {
	f := strategyWorkBoard()
	n, err := New(f).UnassignWork(ctx, "acme", "w1", nil)
	if err != nil || len(n) != 1 {
		t.Fatalf("UnassignWork = %v, %v; want 1", n, err)
	}
	c := f.get("w1")
	if !c.Parked || len(c.Assignees) != 0 || c.SprintStart != "" || c.Day != "" || c.StartDate != "" {
		t.Fatalf("w1 is back in its backlog, nobody's, undated: %+v", c)
	}
}

func TestUnassignedOnAPlanItemParksEveryOpenTaskInWorkUnderIt(t *testing.T) {
	f := strategyWorkBoard()
	svc := New(f)
	g, _ := board.Find(f.b, "g")
	if got := len(board.NewIndex(f.b).UnassignTargets(g)); got != 2 {
		t.Fatalf("the confirm counts %d tasks, want 2 (w1, w2)", got)
	}
	n, err := svc.UnassignWork(ctx, "acme", "g", nil)
	if err != nil || !slices.Equal(n, []string{"w1", "w2"}) {
		t.Fatalf("UnassignWork = %v, %v; want 2", n, err)
	}
	for _, id := range []string{"w1", "w2"} {
		if c := f.get(id); !c.Parked || len(c.Assignees) != 0 {
			t.Errorf("%s back in its backlog: %+v", id, c)
		}
	}
	// Finished work is untouched, and so is what was parked already.
	if c := f.get("done-work"); c.Parked || c.SprintStart == "" {
		t.Errorf("finished work stays where it is: %+v", c)
	}
	if n, err := svc.UnassignWork(ctx, "acme", "g", nil); err != nil || len(n) != 0 {
		t.Fatalf("a second UNASSIGNED = %v, %v; want 0", n, err)
	}
}

func TestUnassignedIsRefusedOnWorkNotInHand(t *testing.T) {
	f := strategyWorkBoard()
	for _, id := range []string{"p1", "done-work"} {
		if _, err := New(f).UnassignWork(ctx, "acme", id, nil); !errors.Is(err, ErrNotInWork) {
			t.Errorf("UNASSIGNED on %s = %v, want ErrNotInWork", id, err)
		}
	}
}

// The confirm counted what the reader could SEE: with a Strategy filter on,
// the plan item's tasks on screen are fewer than the board holds. The
// gesture names them, and the server acts on exactly those — each checked
// against what the gesture may reach — and nothing else (M1).
func TestTheStatusMenuActsOnExactlyTheTasksItWasGiven(t *testing.T) {
	f := strategyWorkBoard()
	n, err := New(f).UnassignWork(ctx, "acme", "g", []string{"w1"})
	if err != nil || len(n) != 1 {
		t.Fatalf("UnassignWork(g, [w1]) = %v, %v; want 1", n, err)
	}
	if !f.get("w1").Parked || f.get("w2").Parked {
		t.Fatalf("only the named task moves: w1 %+v, w2 %+v", f.get("w1"), f.get("w2"))
	}
}

// A uid the gesture cannot reach from the row — not under it, not of the
// kind it reaches — refuses the whole request before anything is written.
func TestTheStatusMenuRefusesATaskItCannotReach(t *testing.T) {
	f := strategyWorkBoard()
	if _, err := New(f).UnassignWork(ctx, "acme", "g", []string{"w1", "p1"}); !errors.Is(err, ErrNotATarget) {
		t.Fatalf("UnassignWork with a parked uid = %v, want ErrNotATarget", err)
	}
	if _, err := New(f).UnassignWork(ctx, "acme", "e", []string{"w2"}); !errors.Is(err, ErrNotATarget) {
		t.Fatalf("UnassignWork naming a task outside the epic = %v, want ErrNotATarget", err)
	}
	if f.count("SetBacklog") != 0 {
		t.Fatalf("nothing is written: %v", f.log)
	}
}

// A task the actor may not write refuses the WHOLE gesture before anything
// is written, saying how many and which: half a plan taken out of work is
// worse than none (M2).
func TestTheStatusMenuWritesNothingWhenOneTaskIsNotTheActorsToWrite(t *testing.T) {
	f := strategyWorkBoard()
	f.unwritable = map[string]bool{"w2": true}
	_, err := New(f).UnassignWork(ctx, "acme", "g", nil)
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("UnassignWork over an unwritable task = %v, want ErrForbidden naming 1 of 2", err)
	}
	if f.count("SetBacklog") != 0 || f.count("SetAssignee") != 0 {
		t.Fatalf("nothing is written: %v", f.log)
	}
}

// UNASSIGNED parks each task without a move of its own — the plan's order is
// not rearranged by taking work out of hand; its queue place is the return
// rule's (Q8, an ordering action, not a move) — and reads the board once for
// the whole gesture, however many tasks it moves (M3).
func TestUnassignedMovesNoTaskAndReadsTheBoardOnce(t *testing.T) {
	f := strategyWorkBoard()
	if _, err := New(f).UnassignWork(ctx, "acme", "g", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.count("MoveCard"); got != 0 {
		t.Fatalf("MoveCard called %d times; parking keeps a task's place", got)
	}
	if got := f.count("LoadBoard"); got != 1 {
		t.Fatalf("the board was read %d times, want once", got)
	}
}

// The confirm asks the SERVER what UNASSIGNED would take (a dry run): the
// reader's Strategy may be filtered, and only the whole board says what makes
// the plan item IN PROGRESS. The dry run names those tasks and writes
// nothing; the gesture then takes exactly them.
func TestUnassignedDryRunNamesTheTasksAndWritesNothing(t *testing.T) {
	f := strategyWorkBoard()
	got, err := New(f).UnassignPreview(ctx, "acme", "g")
	if err != nil || !slices.Equal(got, []string{"w1", "w2"}) {
		t.Fatalf("UnassignPreview(g) = %v, %v; want w1, w2", got, err)
	}
	if f.count("SetBacklog") != 0 || f.get("w1").Parked {
		t.Fatalf("a dry run writes nothing: %v", f.log)
	}
	if _, err := New(f).UnassignPreview(ctx, "acme", "p1"); !errors.Is(err, ErrNotInWork) {
		t.Fatalf("a dry run on parked work = %v, want ErrNotInWork", err)
	}
}
