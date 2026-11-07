package boardservice

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Roadmap's badge menu: a card's own PRIORITY (K15), and UNASSIGNED,
// which takes work out of hand and back to its backlog (K16) — for a task,
// or every task in work under a plan item, at any depth.

func roadmapWorkBoard() *fakeBackend {
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

// The menu sets the card's OWN priority — a task's or a plan item's — and
// "No priority" clears it; anything else is refused before it is written.
func TestThePriorityIsSetOnTheCardItself(t *testing.T) {
	f := roadmapWorkBoard()
	svc := New(f)
	if err := svc.SetPriority(ctx, "acme", "e", "P1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPriority(ctx, "acme", "w1", "P3"); err != nil {
		t.Fatal(err)
	}
	if f.get("e").Priority != board.PriorityP1 || f.get("w1").Priority != board.PriorityP3 {
		t.Fatalf("e %q, w1 %q", f.get("e").Priority, f.get("w1").Priority)
	}
	// Set on the epic, it is written on the epic alone: what the tasks under
	// it count under is derived, never written down on them.
	if f.get("p1").Priority != board.PriorityNone || f.count("SetPriority p1") != 0 {
		t.Fatalf("the epic's priority was written on a task under it: %+v", f.get("p1"))
	}
	if err := svc.SetPriority(ctx, "acme", "e", ""); err != nil || f.get("e").Priority != board.PriorityNone {
		t.Fatalf("No priority clears it: %v, %q", err, f.get("e").Priority)
	}
	for _, bad := range []string{"P5", "p1", "P-1", "high"} {
		if err := svc.SetPriority(ctx, "acme", "w1", bad); !errors.Is(err, ErrBadPriority) {
			t.Errorf("SetPriority %q = %v, want ErrBadPriority", bad, err)
		}
	}
	if f.get("w1").Priority != board.PriorityP3 {
		t.Fatalf("a refused value changed nothing: %q", f.get("w1").Priority)
	}
}

// Nothing clears a priority on its own (K15): taking the task into work,
// finishing it and reopening it all keep it.
func TestThePriorityOutlivesTakingFinishingAndReopening(t *testing.T) {
	f := roadmapWorkBoard()
	f.get("p1").Priority = board.PriorityP2
	svc := New(f)
	if err := svc.SetSprintStart(ctx, "acme", "p1", board.TodayIso()); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStage(ctx, "acme", "p1", board.StageDone); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reopen(ctx, "acme", "p1"); err != nil {
		t.Fatal(err)
	}
	if got := f.get("p1").Priority; got != board.PriorityP2 || f.count("SetPriority") != 0 {
		t.Fatalf("priority after take, finish, reopen = %q (%d writes), want P2 untouched", got, f.count("SetPriority"))
	}
}

func TestUnassignedOnATaskParksIt(t *testing.T) {
	f := roadmapWorkBoard()
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
	f := roadmapWorkBoard()
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
	f := roadmapWorkBoard()
	for _, id := range []string{"p1", "done-work"} {
		if _, err := New(f).UnassignWork(ctx, "acme", id, nil); !errors.Is(err, ErrNotInWork) {
			t.Errorf("UNASSIGNED on %s = %v, want ErrNotInWork", id, err)
		}
	}
}

// The confirm counted what the reader could SEE: with a Roadmap filter on,
// the plan item's tasks on screen are fewer than the board holds. The
// gesture names them, and the server acts on exactly those — each checked
// against what the gesture may reach — and nothing else (M1).
func TestTheStatusMenuActsOnExactlyTheTasksItWasGiven(t *testing.T) {
	f := roadmapWorkBoard()
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
	f := roadmapWorkBoard()
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
	f := roadmapWorkBoard()
	f.unwritable = map[string]bool{"w2": true}
	_, err := New(f).UnassignWork(ctx, "acme", "g", nil)
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("UnassignWork over an unwritable task = %v, want ErrForbidden naming 1 of 2", err)
	}
	if f.count("SetBacklog") != 0 || f.count("SetAssignee") != 0 {
		t.Fatalf("nothing is written: %v", f.log)
	}
}

// UNASSIGNED parks each task where it stands — the plan's order is not
// rearranged by taking work out of hand — and reads the board once for the
// whole gesture, however many tasks it moves (M3).
func TestUnassignedMovesNoTaskAndReadsTheBoardOnce(t *testing.T) {
	f := roadmapWorkBoard()
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
