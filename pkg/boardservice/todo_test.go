package boardservice

import (
	"errors"
	"slices"
	"strings"
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
	n, err := New(f).SetTodo(ctx, "acme", "p1", true, nil)
	if err != nil || len(n) != 1 {
		t.Fatalf("SetTodo = %v, %v; want 1 task marked", n, err)
	}
	if !f.get("p1").Todo {
		t.Fatal("p1 is TO DO")
	}
	// And back.
	if n, err := New(f).SetTodo(ctx, "acme", "p1", false, nil); err != nil || len(n) != 1 || f.get("p1").Todo {
		t.Fatalf("clearing = %v, %v, todo=%v", n, err, f.get("p1").Todo)
	}
}

func TestToDoOnAPlanItemMarksEveryOpenParkedTaskUnderIt(t *testing.T) {
	f := roadmapWorkBoard()
	n, err := New(f).SetTodo(ctx, "acme", "g", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	// p1 (under the epic, two levels down) and p2 (directly under the goal);
	// not the work in hand, not the finished one, not the card elsewhere.
	if !slices.Equal(n, []string{"p1", "p2"}) {
		t.Fatalf("marked %v, want p1 and p2", n)
	}
	for id, want := range map[string]bool{"p1": true, "p2": true, "w1": false, "w2": false, "done-parked": false, "other": false, "sub": false} {
		if got := f.get(id).Todo; got != want {
			t.Errorf("%s todo = %v, want %v", id, got, want)
		}
	}
	// Asked again it marks nothing: what is TO DO already stays so.
	if n, err := New(f).SetTodo(ctx, "acme", "g", true, nil); err != nil || len(n) != 0 {
		t.Fatalf("a second TO DO = %v, %v; want 0", n, err)
	}
}

func TestToDoIsRefusedWhereItMeansNothing(t *testing.T) {
	f := roadmapWorkBoard()
	for _, id := range []string{"w1", "done-parked", "sub"} {
		if _, err := New(f).SetTodo(ctx, "acme", id, true, nil); !errors.Is(err, ErrNotParkedWork) {
			t.Errorf("TO DO on %s = %v, want ErrNotParkedWork", id, err)
		}
	}
	// A plan item with nothing parked under it is no error: nothing to mark.
	g := newFake([]board.Card{{ItemID: "lonely", Type: board.TypeEpic}}, nil)
	if n, err := New(g).SetTodo(ctx, "acme", "lonely", true, nil); err != nil || len(n) != 0 {
		t.Fatalf("TO DO on an empty epic = %v, %v", n, err)
	}
}

func TestTakingATaskIntoWorkClearsItsToDo(t *testing.T) {
	f := roadmapWorkBoard()
	svc := New(f)
	if _, err := svc.SetTodo(ctx, "acme", "p1", true, nil); err != nil {
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
	if _, err := svc.SetTodo(ctx, "acme", "p2", true, nil); err != nil {
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
	if n, err := New(f).SetTodo(ctx, "acme", "g", true, []string{"p2"}); err != nil || len(n) != 1 || !f.get("p2").Todo || f.get("p1").Todo {
		t.Fatalf("SetTodo(g, [p2]) = %v, %v; p1 %v p2 %v", n, err, f.get("p1").Todo, f.get("p2").Todo)
	}
}

// A uid the gesture cannot reach from the row — not under it, not of the
// kind it reaches — refuses the whole request before anything is written.
func TestTheStatusMenuRefusesATaskItCannotReach(t *testing.T) {
	f := roadmapWorkBoard()
	if _, err := New(f).UnassignWork(ctx, "acme", "g", []string{"w1", "p1"}); !errors.Is(err, ErrNotATarget) {
		t.Fatalf("UnassignWork with a parked uid = %v, want ErrNotATarget", err)
	}
	if _, err := New(f).SetTodo(ctx, "acme", "e", true, []string{"p2"}); !errors.Is(err, ErrNotATarget) {
		t.Fatalf("SetTodo naming a task outside the epic = %v, want ErrNotATarget", err)
	}
	if f.count("SetBacklog") != 0 || f.count("SetTodo") != 0 {
		t.Fatalf("nothing is written: %v", f.log)
	}
}

// A task the actor may not write refuses the WHOLE gesture before anything
// is written, saying how many and which: half a plan taken out of work is
// worse than none (M2).
func TestTheStatusMenuWritesNothingWhenOneTaskIsNotTheActorsToWrite(t *testing.T) {
	f := roadmapWorkBoard()
	f.unwritable = map[string]bool{"w2": true, "p2": true}
	_, err := New(f).UnassignWork(ctx, "acme", "g", nil)
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("UnassignWork over an unwritable task = %v, want ErrForbidden naming 1 of 2", err)
	}
	if _, err := New(f).SetTodo(ctx, "acme", "g", true, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("SetTodo over an unwritable task = %v, want ErrForbidden", err)
	}
	if f.count("SetBacklog") != 0 || f.count("SetTodo") != 0 || f.count("SetAssignee") != 0 {
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

// Finishing clears TO DO with what the write already holds — no second read
// of the board — by its stage or by its progress. Subtasks never finish
// their parent: the derived bar stops at 90 and the last step is a person's
// (board.DerivedProgress), so a parent's TO DO goes when that step is taken.
func TestFinishingClearsToDoWithoutReReadingTheBoard(t *testing.T) {
	f := roadmapWorkBoard()
	f.get("p2").Todo = true
	if err := New(f).SetStage(ctx, "acme", "p2", board.StageDone); err != nil {
		t.Fatal(err)
	}
	if f.get("p2").Todo || f.count("LoadBoard") != 1 {
		t.Fatalf("todo=%v, board read %d times", f.get("p2").Todo, f.count("LoadBoard"))
	}
	g := roadmapWorkBoard()
	g.get("p1").Todo = true
	if err := New(g).SetProgress(ctx, "acme", "p1", 100); err != nil {
		t.Fatal(err)
	}
	if g.get("p1").Todo || g.count("LoadBoard") != 1 {
		t.Fatalf("finished by its bar: todo=%v, board read %d times", g.get("p1").Todo, g.count("LoadBoard"))
	}
	h := newFake([]board.Card{
		{ItemID: "e", Type: board.TypeEpic},
		{ItemID: "parent", Parent: "e", Parked: true, Todo: true},
		{ItemID: "piece", Parent: "parent", Parked: true, Progress: 50},
	}, nil)
	if err := New(h).SetProgress(ctx, "acme", "piece", 100); err != nil {
		t.Fatal(err)
	}
	if !h.get("parent").Todo {
		t.Fatalf("a parent its subtasks brought to 90 is not finished, and keeps TO DO: %+v", h.get("parent"))
	}
}

// A task grouped under a TASK becomes a subtask: it rides its parent and is
// in no backlog, so it is not TO DO either.
func TestBecomingASubtaskClearsToDo(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "host", Team: "alpha", Parked: true},
		{ItemID: "ready", Team: "alpha", Parked: true, Todo: true},
	}, nil)
	if err := New(f).SetParent(ctx, "acme", "ready", "host"); err != nil {
		t.Fatal(err)
	}
	if f.get("ready").Todo {
		t.Fatalf("a subtask is not TO DO: %+v", f.get("ready"))
	}
}
