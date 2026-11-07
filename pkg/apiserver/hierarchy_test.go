package apiserver

import (
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func hierarchyBoard() board.Board {
	today := board.TodayIso()
	b := board.NewBoard([]board.Card{
		{ItemID: "goal", Title: "Reach Europe", Type: board.TypeGoal},
		{ItemID: "init", Title: "Portugal", Type: board.TypeInitiative, Parent: "goal"},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "init"},
		{ItemID: "t1", Title: "Invoices", Parent: "epic", Team: "alpha", Assignees: []string{"dev"},
			StartDate: today, Day: today, SprintStart: today, Progress: 40},
		{ItemID: "t1a", Title: "Invoice PDF", Parent: "t1", Team: "alpha", Progress: 100},
		{ItemID: "loose", Title: "Loose", Team: "alpha", Assignees: []string{"dev"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "loose-sub", Title: "Loose piece", Parent: "loose", Team: "alpha"},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today}}
	return b
}

// The roadmap view is the hierarchy AND the work that is not in it yet: every
// planning card and every card with one above it — a task, its subtask —
// and, below them, every OPEN task that has no planning card
// above it: the work in hand on the Team and Me boards, the cards parked on a
// shelf, the strip. Those are what a lead drags INTO the tree, so the tree's
// board lists them (H9). What stays out: a task already finished outside the
// hierarchy (nothing to plan) and a subtask whose parent task is out.
func TestTheRoadmapListsTheHierarchy(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	today := board.TodayIso()
	b.Cards = append(b.Cards,
		// Finished outside the hierarchy: not on the roadmap.
		board.Card{ItemID: "done-loose", Title: "Shipped", Team: "alpha", Assignees: []string{"dev"},
			StartDate: today, Day: today, SprintStart: today, Progress: 100, DoneAt: today},
		// Its subtask follows it out.
		board.Card{ItemID: "done-sub", Title: "Shipped piece", Parent: "done-loose", Team: "alpha"},
		// Parked on a shelf: the backlog is on the roadmap.
		board.Card{ItemID: "parked", Title: "Someday", Team: "alpha", Parked: true},
		// In the strip — nobody has said when — still work to plan.
		board.Card{ItemID: "strip", Title: "Arrived"},
	)
	got := []string{}
	for _, c := range FilterCards(b, Selector{View: "roadmap"}) {
		got = append(got, c.ItemID)
	}
	want := []string{"goal", "init", "epic", "t1", "t1a", "loose", "loose-sub", "parked", "strip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("roadmap = %v, want %v", got, want)
	}
	// And the day boards never list a planning card, while the task under
	// the epic stands on them like any task.
	for _, view := range []string{"team", "me"} {
		sel := Selector{View: view, Team: "alpha", User: "dev", Day: board.TodayIso()}
		for _, c := range FilterCards(b, sel) {
			if board.IsPlanning(c) {
				t.Errorf("%s lists the planning card %s", view, c.ItemID)
			}
		}
		if !Drawn(b, sel, "t1") {
			t.Errorf("%s does not draw the task under the epic", view)
		}
	}
	// The gate agrees with the listing: a gesture on the roadmap reaches a
	// planning card, a task under one, and the loose open work below the
	// tree — and not what the listing leaves out.
	for _, id := range []string{"epic", "t1", "loose", "parked", "strip"} {
		if !Drawn(b, Selector{View: "roadmap"}, id) {
			t.Errorf("the roadmap draws %s", id)
		}
	}
	for _, id := range []string{"done-loose", "done-sub"} {
		if Drawn(b, Selector{View: "roadmap"}, id) {
			t.Errorf("%s is not on the roadmap", id)
		}
	}
}

// A card resource carries its breadcrumb — every ancestor, root first — and
// a planning card its derived standing (H5).
func TestACardResourceCarriesItsLineageAndHierarchy(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	find := func(id string) board.Card {
		c, _ := board.Find(b, id)
		return c
	}
	sub := CardResource(b, find("t1a"))
	if sub.Spec.Type != "" || sub.Status.Hierarchy != nil {
		t.Fatalf("a task has no type and no hierarchy status: %+v", sub)
	}
	crumbs := []string{}
	for _, l := range sub.Status.Lineage {
		crumbs = append(crumbs, l.Type+":"+l.Title)
	}
	if want := []string{"goal:Reach Europe", "initiative:Portugal", "epic:Billing", ":Invoices"}; !reflect.DeepEqual(crumbs, want) {
		t.Fatalf("lineage = %v, want %v", crumbs, want)
	}
	goal := CardResource(b, find("goal"))
	if goal.Spec.Type != board.TypeGoal || len(goal.Status.Lineage) != 0 {
		t.Fatalf("goal = %+v", goal.Spec)
	}
	// t1 at 40: in progress, 0 of 1 (the subtask t1a is t1's own).
	if h := goal.Status.Hierarchy; h == nil || h.Status != board.HierarchyInProgress || h.Done != 0 || h.Total != 1 {
		t.Fatalf("hierarchy = %+v, want in-progress 0/1", h)
	}
	if CardResource(b, find("loose")).Status.Lineage != nil {
		t.Fatal("a root has no lineage")
	}
}
