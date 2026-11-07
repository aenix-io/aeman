package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Roadmap's status menu: TO DO marks parked work ready to be taken next
// (K15), UNASSIGNED takes work out of hand and back to its backlog (K16).
// Both answer for a task, and for a plan item they fan out to the tasks under
// it — at any depth — that they can mean something for.

func roadmapWorkBoard() *fakeBackend {
	today := board.TodayIso()
	return newFake([]board.Card{
		{ItemID: "g", Type: board.TypeGoal, Team: "alpha"},
		{ItemID: "e", Type: board.TypeEpic, Parent: "g"},
		// Parked, open: what TO DO is for.
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

func TestToDoOnATaskMarksItReady(t *testing.T) {
	f := roadmapWorkBoard()
	n, err := New(f).SetTodo(ctx, "acme", "p1", true)
	if err != nil || n != 1 {
		t.Fatalf("SetTodo = %d, %v; want 1 task marked", n, err)
	}
	if !f.get("p1").Todo {
		t.Fatal("p1 is TO DO")
	}
	// And back.
	if n, err := New(f).SetTodo(ctx, "acme", "p1", false); err != nil || n != 1 || f.get("p1").Todo {
		t.Fatalf("clearing = %d, %v, todo=%v", n, err, f.get("p1").Todo)
	}
}

func TestToDoOnAPlanItemMarksEveryOpenParkedTaskUnderIt(t *testing.T) {
	f := roadmapWorkBoard()
	n, err := New(f).SetTodo(ctx, "acme", "g", true)
	if err != nil {
		t.Fatal(err)
	}
	// p1 (under the epic, two levels down) and p2 (directly under the goal);
	// not the work in hand, not the finished one, not the card elsewhere.
	if n != 2 {
		t.Fatalf("marked %d, want 2", n)
	}
	for id, want := range map[string]bool{"p1": true, "p2": true, "w1": false, "w2": false, "done-parked": false, "other": false, "sub": false} {
		if got := f.get(id).Todo; got != want {
			t.Errorf("%s todo = %v, want %v", id, got, want)
		}
	}
	// Asked again it marks nothing: what is TO DO already stays so.
	if n, err := New(f).SetTodo(ctx, "acme", "g", true); err != nil || n != 0 {
		t.Fatalf("a second TO DO = %d, %v; want 0", n, err)
	}
}

func TestToDoIsRefusedWhereItMeansNothing(t *testing.T) {
	f := roadmapWorkBoard()
	for _, id := range []string{"w1", "done-parked", "sub"} {
		if _, err := New(f).SetTodo(ctx, "acme", id, true); !errors.Is(err, ErrNotParkedWork) {
			t.Errorf("TO DO on %s = %v, want ErrNotParkedWork", id, err)
		}
	}
	// A plan item with nothing parked under it is no error: nothing to mark.
	g := newFake([]board.Card{{ItemID: "lonely", Type: board.TypeEpic}}, nil)
	if n, err := New(g).SetTodo(ctx, "acme", "lonely", true); err != nil || n != 0 {
		t.Fatalf("TO DO on an empty epic = %d, %v", n, err)
	}
}

func TestTakingATaskIntoWorkClearsItsToDo(t *testing.T) {
	f := roadmapWorkBoard()
	svc := New(f)
	if _, err := svc.SetTodo(ctx, "acme", "p1", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetSprintStart(ctx, "acme", "p1", board.TodayIso()); err != nil {
		t.Fatal(err)
	}
	if c := f.get("p1"); c.Todo || c.Parked {
		t.Fatalf("taken into work: %+v; want neither parked nor TO DO", c)
	}
}

func TestFinishingATaskClearsItsToDo(t *testing.T) {
	f := roadmapWorkBoard()
	svc := New(f)
	if _, err := svc.SetTodo(ctx, "acme", "p2", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStage(ctx, "acme", "p2", board.StageDone); err != nil {
		t.Fatal(err)
	}
	if f.get("p2").Todo {
		t.Fatal("a finished task is not TO DO: reopened, it is not marked ready by a flag nobody saw")
	}
}

func TestUnassignedOnATaskParksIt(t *testing.T) {
	f := roadmapWorkBoard()
	n, err := New(f).UnassignWork(ctx, "acme", "w1")
	if err != nil || n != 1 {
		t.Fatalf("UnassignWork = %d, %v; want 1", n, err)
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
	n, err := svc.UnassignWork(ctx, "acme", "g")
	if err != nil || n != 2 {
		t.Fatalf("UnassignWork = %d, %v; want 2", n, err)
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
	if n, err := svc.UnassignWork(ctx, "acme", "g"); err != nil || n != 0 {
		t.Fatalf("a second UNASSIGNED = %d, %v; want 0", n, err)
	}
}

func TestUnassignedIsRefusedOnWorkNotInHand(t *testing.T) {
	f := roadmapWorkBoard()
	for _, id := range []string{"p1", "done-work"} {
		if _, err := New(f).UnassignWork(ctx, "acme", id); !errors.Is(err, ErrNotInWork) {
			t.Errorf("UNASSIGNED on %s = %v, want ErrNotInWork", id, err)
		}
	}
}
