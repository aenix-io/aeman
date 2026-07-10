package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-org/aeman/pkg/board"
)

func TestSetParentGroupsAndSyncsSprint(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", SprintStart: "2026-01-05", Progress: 0},
		{ItemID: "c", Team: "alpha", SprintStart: "2026-01-01", Progress: 40},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-05"}})
	if err := f2svc(f).SetParent(ctx, "acme", 1, "c", "p"); err != nil {
		t.Fatal(err)
	}
	if f.get("c").Parent != "p" {
		t.Fatalf("child = %+v", f.get("c"))
	}
	// The subtask joined its parent's sprint.
	if f.get("c").SprintStart != "2026-01-05" {
		t.Fatalf("subtask sprint = %q, want the parent's", f.get("c").SprintStart)
	}
	// The parent's progress derives from its children: 40 * 0.9 = 36.
	if f.get("p").Progress != 36 {
		t.Fatalf("parent progress = %d, want 36", f.get("p").Progress)
	}
}

func TestSetParentOneLevelOnly(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "c", Team: "alpha", Parent: "p"},
		{ItemID: "x", Team: "alpha"},
	}, nil)
	// A subtask cannot become a parent.
	if err := f2svc(f).SetParent(ctx, "acme", 1, "x", "c"); !errors.Is(err, ErrSubtaskDepth) {
		t.Fatalf("grouping under a subtask: err = %v", err)
	}
	// A card with subtasks cannot become one.
	if err := f2svc(f).SetParent(ctx, "acme", 1, "p", "x"); !errors.Is(err, ErrSubtaskDepth) {
		t.Fatalf("grouping a parent: err = %v", err)
	}
	// Not under itself.
	if err := f2svc(f).SetParent(ctx, "acme", 1, "x", "x"); !errors.Is(err, ErrSubtaskDepth) {
		t.Fatalf("grouping under itself: err = %v", err)
	}
}

func TestSetParentWeeklyCardHandsSlotToParent(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "w", Team: "alpha", Plan: board.PlanWed, Week: "2026-01-05"},
	}, nil)
	if err := f2svc(f).SetParent(ctx, "acme", 1, "w", "p"); err != nil {
		t.Fatal(err)
	}
	if f.get("p").Plan != board.PlanWed || f.get("p").Week != "2026-01-05" {
		t.Fatalf("parent did not take the weekly slot: %+v", f.get("p"))
	}
	if f.get("w").Plan != board.PlanNone || f.get("w").Week != "" {
		t.Fatalf("subtask kept its plan: %+v", f.get("w"))
	}
}

func TestSetParentClearUngroups(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "c", Team: "alpha", Parent: "p"},
	}, nil)
	if err := f2svc(f).SetParent(ctx, "acme", 1, "c", ""); err != nil {
		t.Fatal(err)
	}
	if f.get("c").Parent != "" {
		t.Fatalf("child not ungrouped: %+v", f.get("c"))
	}
}

func TestDoneGuardedByOpenSubtasks(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "c1", Team: "alpha", Parent: "p", Stage: board.StageDone, Progress: 100},
		{ItemID: "c2", Team: "alpha", Parent: "p", Progress: 50},
	}, nil)
	if err := f2svc(f).SetStage(ctx, "acme", 1, "p", board.StageDone); !errors.Is(err, ErrOpenSubtasks) {
		t.Fatalf("done with open subtasks: err = %v", err)
	}
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "p", 100); !errors.Is(err, ErrOpenSubtasks) {
		t.Fatalf("100%% with open subtasks: err = %v", err)
	}
	// Closing the last subtask unlocks the parent.
	if err := f2svc(f).SetStage(ctx, "acme", 1, "c2", board.StageDone); err != nil {
		t.Fatal(err)
	}
	if err := f2svc(f).SetStage(ctx, "acme", 1, "p", board.StageDone); err != nil {
		t.Fatalf("done after all subtasks closed: %v", err)
	}
}

func TestSubtaskProgressDrivesParent(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "c1", Team: "alpha", Parent: "p", Progress: 0},
		{ItemID: "c2", Team: "alpha", Parent: "p", Progress: 0},
	}, nil)
	if err := f2svc(f).SetProgress(ctx, "acme", 1, "c1", 100); err != nil {
		t.Fatal(err)
	}
	// c1 at 100 (complete), c2 at 0: (100+0)/2 * 0.9 = 45.
	if f.get("p").Progress != 45 {
		t.Fatalf("parent progress = %d, want 45", f.get("p").Progress)
	}
}

func TestCarryOverDragsSubtasksWithParent(t *testing.T) {
	old := "2026-01-01"
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", SprintStart: old},
		// A done subtask still rides with its parent.
		{ItemID: "c1", Team: "alpha", Parent: "p", SprintStart: old, Stage: board.StageDone, Progress: 100},
		// A subtask of ANOTHER team rides too - the parent's team drives.
		{ItemID: "c2", Team: "beta", Parent: "p", SprintStart: old, Progress: 20},
		// A subtask whose parent is NOT carried (done parent) stays put.
		{ItemID: "q", Team: "alpha", SprintStart: old, Stage: board.StageDone, Progress: 100},
		{ItemID: "c3", Team: "alpha", Parent: "q", SprintStart: old, Progress: 10},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", 1, "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 1 {
		t.Fatalf("Carried = %d, want 1 (the parent; followers not counted)", rep.Carried)
	}
	for _, id := range []string{"p", "c1", "c2"} {
		if f.get(id).SprintStart != today {
			t.Fatalf("%s not carried: %+v", id, f.get(id))
		}
	}
	// The hanging subtask (its own team, done parent) stays.
	if f.get("c3").SprintStart != old {
		t.Fatalf("hanging subtask moved: %+v", f.get("c3"))
	}
}

func TestSetParentMoveBetweenParentsResyncsBoth(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p1", Team: "alpha", Progress: 54},
		{ItemID: "p2", Team: "alpha", Progress: 0},
		{ItemID: "c1", Team: "alpha", Parent: "p1", Progress: 40},
		{ItemID: "c2", Team: "alpha", Parent: "p1", Progress: 80},
	}, nil)
	if err := f2svc(f).SetParent(ctx, "acme", 1, "c2", "p2"); err != nil {
		t.Fatal(err)
	}
	if f.get("c2").Parent != "p2" {
		t.Fatalf("child = %+v", f.get("c2"))
	}
	// The old parent re-derives from its remaining child: 40 * 0.9 = 36.
	if f.get("p1").Progress != 36 {
		t.Fatalf("old parent progress = %d, want 36", f.get("p1").Progress)
	}
	// The new parent derives from its adopted child: 80 * 0.9 = 72.
	if f.get("p2").Progress != 72 {
		t.Fatalf("new parent progress = %d, want 72", f.get("p2").Progress)
	}
}

func TestSetParentAdoptsParentTeam(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "c", Team: "beta", Progress: 40},
	}, nil)
	if err := f2svc(f).SetParent(ctx, "acme", 1, "c", "p"); err != nil {
		t.Fatal(err)
	}
	if f.get("c").Team != "alpha" {
		t.Fatalf("subtask team = %q, want the parent's alpha", f.get("c").Team)
	}
}

func TestSetTeamCascadesToSubtasks(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "c", Team: "alpha", Parent: "p"},
	}, map[string]board.SprintState{"beta": {Current: "2026-01-05"}})
	if err := f2svc(f).SetTeam(ctx, "acme", 1, "p", "beta", ""); err != nil {
		t.Fatal(err)
	}
	if f.get("p").Team != "beta" || f.get("c").Team != "beta" {
		t.Fatalf("teams = %q/%q, want beta/beta", f.get("p").Team, f.get("c").Team)
	}
	if f.get("c").SprintStart != f.get("p").SprintStart {
		t.Fatalf("subtask sprint %q != parent sprint %q",
			f.get("c").SprintStart, f.get("p").SprintStart)
	}
}

func TestSetTeamOnSubtaskFollowsParent(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "c", Team: "alpha", Parent: "p"},
	}, nil)
	if err := f2svc(f).SetTeam(ctx, "acme", 1, "c", "gamma", ""); err != nil {
		t.Fatal(err)
	}
	if f.get("c").Team != "alpha" {
		t.Fatalf("subtask team = %q, want to stay the parent's alpha", f.get("c").Team)
	}
}
