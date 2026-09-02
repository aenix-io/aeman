package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func TestSetParentGroupsAndSyncsSprint(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", SprintStart: "2026-01-05", Progress: 0},
		{ItemID: "c", Team: "alpha", SprintStart: "2026-01-01", Progress: 40},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-05"}})
	if err := f2svc(f).SetParent(ctx, "acme", "c", "p"); err != nil {
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
	if err := f2svc(f).SetParent(ctx, "acme", "x", "c"); !errors.Is(err, ErrSubtaskDepth) {
		t.Fatalf("grouping under a subtask: err = %v", err)
	}
	// A card with subtasks cannot become one.
	if err := f2svc(f).SetParent(ctx, "acme", "p", "x"); !errors.Is(err, ErrSubtaskDepth) {
		t.Fatalf("grouping a parent: err = %v", err)
	}
	// Not under itself.
	if err := f2svc(f).SetParent(ctx, "acme", "x", "x"); !errors.Is(err, ErrSubtaskDepth) {
		t.Fatalf("grouping under itself: err = %v", err)
	}
}

func TestSetParentWeeklyCardHandsSlotToParent(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "w", Team: "alpha", Plan: board.PlanWed, Week: "2026-01-05"},
	}, nil)
	if err := f2svc(f).SetParent(ctx, "acme", "w", "p"); err != nil {
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
	if err := f2svc(f).SetParent(ctx, "acme", "c", ""); err != nil {
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
	if err := f2svc(f).SetStage(ctx, "acme", "p", board.StageDone); !errors.Is(err, ErrOpenSubtasks) {
		t.Fatalf("done with open subtasks: err = %v", err)
	}
	if err := f2svc(f).SetProgress(ctx, "acme", "p", 100); !errors.Is(err, ErrOpenSubtasks) {
		t.Fatalf("100%% with open subtasks: err = %v", err)
	}
	// Closing the last subtask unlocks the parent.
	if err := f2svc(f).SetStage(ctx, "acme", "c2", board.StageDone); err != nil {
		t.Fatal(err)
	}
	if err := f2svc(f).SetStage(ctx, "acme", "p", board.StageDone); err != nil {
		t.Fatalf("done after all subtasks closed: %v", err)
	}
}

func TestSubtaskProgressDrivesParent(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha"},
		{ItemID: "c1", Team: "alpha", Parent: "p", Progress: 0},
		{ItemID: "c2", Team: "alpha", Parent: "p", Progress: 0},
	}, nil)
	if err := f2svc(f).SetProgress(ctx, "acme", "c1", 100); err != nil {
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
		// A done subtask stays in the sprint it was finished in; the parent's
		// derived bar still counts it.
		{ItemID: "c1", Team: "alpha", Parent: "p", SprintStart: old, Stage: board.StageDone, Progress: 100},
		// An open subtask of ANOTHER team rides too - the parent's team drives.
		{ItemID: "c2", Team: "beta", Parent: "p", SprintStart: old, Progress: 20},
		// A subtask whose parent is NOT carried (done parent) stays put.
		{ItemID: "q", Team: "alpha", SprintStart: old, Stage: board.StageDone, Progress: 100},
		{ItemID: "c3", Team: "alpha", Parent: "q", SprintStart: old, Progress: 10},
	}, map[string]board.SprintState{"alpha": {Current: old}})
	rep, err := f2svc(f).CarryOver(ctx, "acme", "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 1 {
		t.Fatalf("Carried = %d, want 1 (the parent; followers not counted)", rep.Carried)
	}
	for _, id := range []string{"p", "c2"} {
		if f.get(id).SprintStart != today {
			t.Fatalf("%s not carried: %+v", id, f.get(id))
		}
	}
	// The done subtask stays behind in the closing sprint.
	if f.get("c1").SprintStart != old {
		t.Fatalf("done subtask carried: %+v", f.get("c1"))
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
	if err := f2svc(f).SetParent(ctx, "acme", "c2", "p2"); err != nil {
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
	if err := f2svc(f).SetParent(ctx, "acme", "c", "p"); err != nil {
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
	if err := f2svc(f).SetTeam(ctx, "acme", "p", "beta", ""); err != nil {
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
	if err := f2svc(f).SetTeam(ctx, "acme", "c", "gamma", ""); err != nil {
		t.Fatal(err)
	}
	if f.get("c").Team != "alpha" {
		t.Fatalf("subtask team = %q, want to stay the parent's alpha", f.get("c").Team)
	}
}

func TestGroupingUnfinishedCardReopensDoneParent(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", Stage: board.StageDone, Progress: 100},
		{ItemID: "done-child", Team: "alpha", Parent: "p", Progress: 100},
		{ItemID: "c", Team: "alpha", Progress: 40},
	}, nil)
	if err := f2svc(f).SetParent(ctx, "acme", "c", "p"); err != nil {
		t.Fatal(err)
	}
	// Done cannot stand on top of open subtasks: the parent reopened and its
	// bar derives again — (100 + 40) * 90 / 200 = 63.
	if f.get("p").Stage != board.StageNone {
		t.Fatalf("parent stage = %q, want cleared", f.get("p").Stage)
	}
	if f.get("p").Progress != 63 {
		t.Fatalf("parent progress = %d, want 63", f.get("p").Progress)
	}
}

func TestGroupingFinishedCardKeepsParentDone(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", Stage: board.StageDone, Progress: 100},
		{ItemID: "c", Team: "alpha", Stage: board.StageDone, Progress: 100},
	}, nil)
	if err := f2svc(f).SetParent(ctx, "acme", "c", "p"); err != nil {
		t.Fatal(err)
	}
	if f.get("p").Stage != board.StageDone || f.get("p").Progress != 100 {
		t.Fatalf("parent = stage %q progress %d, want done/100 untouched",
			f.get("p").Stage, f.get("p").Progress)
	}
}

func TestReopeningSubtaskReopensDoneParent(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", Stage: board.StageDone, Progress: 100},
		{ItemID: "c", Team: "alpha", Parent: "p", Progress: 100},
	}, nil)
	if err := f2svc(f).SetProgress(ctx, "acme", "c", 50); err != nil {
		t.Fatal(err)
	}
	if f.get("p").Stage != board.StageNone {
		t.Fatalf("parent stage = %q, want cleared", f.get("p").Stage)
	}
	// 50 * 90 / 100 = 45.
	if f.get("p").Progress != 45 {
		t.Fatalf("parent progress = %d, want 45", f.get("p").Progress)
	}
}

func TestDeleteParentReleasesSubtasks(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Title: "big", Team: "alpha"},
		{ItemID: "c1", Team: "alpha", Parent: "p"},
		{ItemID: "c2", Team: "alpha", Parent: "p"},
	}, nil)
	if err := f2svc(f).DeleteCard(ctx, "acme", "p"); err != nil {
		t.Fatal(err)
	}
	if f.get("p") != nil {
		t.Fatal("parent still on the board")
	}
	for _, id := range []string{"c1", "c2"} {
		c := f.get(id)
		if c == nil {
			t.Fatalf("%s was deleted with its parent", id)
		}
		if c.Parent != "" {
			t.Fatalf("%s still points at the deleted parent", id)
		}
	}
}

func TestParentSprintChangeDragsSubtasks(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", SprintStart: "2026-01-10"},
		{ItemID: "c", Team: "alpha", Parent: "p", SprintStart: "2026-01-10"},
	}, nil)
	if err := f2svc(f).SetSprintStart(ctx, "acme", "p", "2026-01-03"); err != nil {
		t.Fatal(err)
	}
	if f.get("c").SprintStart != "2026-01-03" {
		t.Fatalf("subtask sprint = %q, want dragged to 2026-01-03", f.get("c").SprintStart)
	}
}

// The × takes the whole group: a subtask is a piece of the card's work, not
// a card of its own, and the demote this replaced dragged them along too.
// An explicit DELETE of one card still frees its subtasks — two doors, two
// meanings: "this work is off the board" and "this card is a mistake".
func TestSmartRemoveTakesSubtasksWithIt(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", StartDate: "2026-01-10", SprintStart: "2026-01-10"},
		{ItemID: "c", Team: "alpha", Parent: "p", SprintStart: "2026-01-10"},
	}, map[string]board.SprintState{
		"alpha": {Current: "2026-01-10", Previous: "2026-01-03"},
	})
	if err := f2svc(f).Remove(ctx, "acme", "p", "grid"); err != nil {
		t.Fatal(err)
	}
	if f.get("p") != nil {
		t.Fatalf("parent = %+v, want gone", f.get("p"))
	}
	if f.get("c") != nil {
		t.Fatalf("subtask = %+v, want gone with its parent", f.get("c"))
	}
}

func TestDeleteParentReleasedChildInheritsAssignee(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "p", Title: "big", Team: "alpha", Assignees: []string{"bob"}},
		{ItemID: "c1", Team: "alpha", Parent: "p"},
		{ItemID: "c2", Team: "alpha", Parent: "p", Assignees: []string{"eve"}},
	}, nil)
	if err := f2svc(f).DeleteCard(ctx, "acme", "p"); err != nil {
		t.Fatal(err)
	}
	if got := f.get("c1").Assignees; len(got) != 1 || got[0] != "bob" {
		t.Fatalf("released c1 assignees = %v, want the parent's bob", got)
	}
	if got := f.get("c2").Assignees; len(got) != 1 || got[0] != "eve" {
		t.Fatalf("released c2 assignees = %v, want its own eve kept", got)
	}
}

// A card created today has no earlier sprint to demote into, and with no
// band and no column it is nowhere else either: the × empties its only home
// and the subtasks ride along into the delete. In a band, the same parent is
// handed back to it instead — which is what this checks, so the cascade is
// not mistaken for the rule.
func TestSmartRemoveCreatedTodayHandsBackAParentThatHasABand(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "p", Team: "alpha", StartDate: today, SprintStart: today, Assignees: []string{"bob"},
			Plan: board.PlanFri, Week: board.MondayOf(today)},
		{ItemID: "c", Team: "alpha", Parent: "p", StartDate: today, SprintStart: today},
	}, map[string]board.SprintState{
		"alpha": {Current: today, Previous: "2026-01-03"},
	})
	if err := f2svc(f).Remove(ctx, "acme", "p", "grid"); err != nil {
		t.Fatal(err)
	}
	p := f.get("p")
	if p == nil {
		t.Fatalf("a created-today card is handed back, not deleted; log=%v", f.log)
	}
	if len(p.Assignees) != 0 || p.SprintStart != "" || p.Plan == board.PlanNone {
		t.Fatalf("it leaves the person and the sprint for the weekly plan: %+v", p)
	}
	// Nothing was destroyed, so the subtask stays nested under its parent
	// instead of being orphaned into a standalone card.
	c := f.get("c")
	if c == nil || c.Parent != "p" {
		t.Fatalf("the subtask rides along under its parent: %+v", c)
	}
}

func TestDoneOnRecurrentKeepsRecurrence(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "r", Team: "alpha", Stage: board.StageRecurrent, Progress: 40},
	}, nil)
	if err := f2svc(f).SetStage(ctx, "acme", "r", board.StageDone); err != nil {
		t.Fatal(err)
	}
	c := f.get("r")
	if c.Stage != board.StageRecurrent {
		t.Fatalf("stage = %q, want recurrent kept", c.Stage)
	}
	if c.Progress != 100 {
		t.Fatalf("progress = %d, want 100", c.Progress)
	}
}

// Grouping a weekly-plan card under a Project-board SLOT: the plan hand-off
// must not write the subtask's week onto the parent — a slot's week derives
// from its start date, the conflicting write is refused, and the refusal
// killed the whole grouping. A slot is on the Weekly panel by its span
// already; the subtask's plan simply clears.
func TestGroupPlanCardUnderASlot(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "slot", Title: "the slot", Epic: "E", Project: "P", Team: "t",
			StartDate: "2026-08-25", Week: "2026-08-24", Day: "2026-09-11"},
		{ItemID: "c1", Title: "plan card", Team: "t",
			Plan: board.PlanFri, Week: "2026-08-31"},
	}, nil)
	svc := New(fake)
	ctx := t.Context()
	if err := svc.SetParent(ctx, "o", "c1", "slot"); err != nil {
		t.Fatalf("grouping under a slot: %v", err)
	}
	b, _ := fake.LoadBoard(ctx, "o")
	var child, parent board.Card
	for _, c := range b.Cards {
		switch c.ItemID {
		case "c1":
			child = c
		case "slot":
			parent = c
		}
	}
	if child.Parent != "slot" {
		t.Fatalf("child parent = %q, want slot", child.Parent)
	}
	if child.Plan != board.PlanNone {
		t.Fatalf("the subtask kept its plan band %q", child.Plan)
	}
	if parent.Plan != board.PlanNone || parent.Week != "2026-08-24" {
		t.Fatalf("the slot was rewritten: band %q week %q", parent.Plan, parent.Week)
	}
}

// Pulling a subtask out of its parent must not make it ownerless. A subtask
// usually has no assignee of its own — it rides the parent's — so ungrouping
// dropped it into Unassigned and off every personal board: from the person
// who pulled it, the card simply vanished. deleteWithCascade already hands a
// released child the parent's person for exactly this reason; the plain
// ungroup now does the same.
func TestUngroupGivesTheParentsPerson(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: "the parent", Team: "portal", Assignees: []string{"androndo"}},
		{ItemID: "c1", Title: "smoke test", Team: "portal", Parent: "p1"},
	}, nil)
	svc := New(fake)
	ctx := t.Context()
	if err := svc.SetParent(ctx, "o", "c1", ""); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(ctx, "o")
	c, _ := findCard(b, "c1")
	if c.Parent != "" {
		t.Fatalf("still grouped under %q", c.Parent)
	}
	if len(c.Assignees) != 1 || c.Assignees[0] != "androndo" {
		t.Fatalf("assignees = %v, want the parent's person so the card stays on a board", c.Assignees)
	}
}

// A subtask that has its OWN assignee keeps them: the pull-out must not hand
// someone else's work to the parent's owner.
func TestUngroupKeepsItsOwnPerson(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: "the parent", Team: "portal", Assignees: []string{"androndo"}},
		{ItemID: "c1", Title: "smoke test", Team: "portal", Parent: "p1", Assignees: []string{"kitsunoff"}},
	}, nil)
	svc := New(fake)
	if err := svc.SetParent(t.Context(), "o", "c1", ""); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	c, _ := findCard(b, "c1")
	if len(c.Assignees) != 1 || c.Assignees[0] != "kitsunoff" {
		t.Fatalf("assignees = %v, want its own person untouched", c.Assignees)
	}
}

// A subtask always belongs to its parent's PERSON, the way it always belongs
// to the parent's team. Three doors lead in, and all three must agree, or a
// family drifts apart and someone else's card lands on your personal board:
// the Me view admits a card when you own one of its subtasks, so a single
// stray child drags the parent and all its siblings onto a board they are not
// part of.
func TestSubtaskOwnerFollowsTheParent(t *testing.T) {
	t.Run("grouping hands the child the parent's person", func(t *testing.T) {
		fake := newFake([]board.Card{
			{ItemID: "p1", Title: "parent", Team: "portal", Assignees: []string{"IvanStukov"}},
			{ItemID: "c1", Title: "child", Team: "portal", Assignees: []string{"krakazyabra"}},
		}, nil)
		svc := New(fake)
		if err := svc.SetParent(t.Context(), "o", "c1", "p1"); err != nil {
			t.Fatal(err)
		}
		b, _ := fake.LoadBoard(t.Context(), "o")
		c, _ := findCard(b, "c1")
		if len(c.Assignees) != 1 || c.Assignees[0] != "IvanStukov" {
			t.Fatalf("assignees = %v, want the parent's person", c.Assignees)
		}
	})

	t.Run("a direct change on a subtask follows the parent instead of drifting", func(t *testing.T) {
		fake := newFake([]board.Card{
			{ItemID: "p1", Title: "parent", Team: "portal", Assignees: []string{"IvanStukov"}},
			{ItemID: "c1", Title: "child", Team: "portal", Parent: "p1", Assignees: []string{"IvanStukov"}},
		}, nil)
		svc := New(fake)
		if err := svc.SetAssignee(t.Context(), "o", "c1", "krakazyabra"); err != nil {
			t.Fatal(err)
		}
		b, _ := fake.LoadBoard(t.Context(), "o")
		c, _ := findCard(b, "c1")
		if len(c.Assignees) != 1 || c.Assignees[0] != "IvanStukov" {
			t.Fatalf("assignees = %v, want it snapped back to the parent's person", c.Assignees)
		}
	})

	t.Run("re-assigning the parent cascades to every subtask", func(t *testing.T) {
		fake := newFake([]board.Card{
			{ItemID: "p1", Title: "parent", Team: "portal", Assignees: []string{"IvanStukov"}},
			{ItemID: "c1", Title: "one", Team: "portal", Parent: "p1", Assignees: []string{"IvanStukov"}},
			{ItemID: "c2", Title: "two", Team: "portal", Parent: "p1", Assignees: []string{"IvanStukov"}},
		}, nil)
		svc := New(fake)
		if err := svc.SetAssignee(t.Context(), "o", "p1", "krakazyabra"); err != nil {
			t.Fatal(err)
		}
		b, _ := fake.LoadBoard(t.Context(), "o")
		for _, id := range []string{"p1", "c1", "c2"} {
			c, _ := findCard(b, id)
			if len(c.Assignees) != 1 || c.Assignees[0] != "krakazyabra" {
				t.Fatalf("%s assignees = %v, want the whole family on krakazyabra", id, c.Assignees)
			}
		}
	})

	t.Run("unassigning the parent unassigns the family", func(t *testing.T) {
		fake := newFake([]board.Card{
			{ItemID: "p1", Title: "parent", Team: "portal", Assignees: []string{"IvanStukov"}},
			{ItemID: "c1", Title: "one", Team: "portal", Parent: "p1", Assignees: []string{"IvanStukov"}},
		}, nil)
		svc := New(fake)
		if err := svc.SetAssignee(t.Context(), "o", "p1", ""); err != nil {
			t.Fatal(err)
		}
		b, _ := fake.LoadBoard(t.Context(), "o")
		c, _ := findCard(b, "c1")
		if len(c.Assignees) != 0 {
			t.Fatalf("child assignees = %v, want none", c.Assignees)
		}
	})
}
