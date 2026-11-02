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
		{ItemID: "p1", Title: "Legal", Parent: "init", Scope: "Compliance", Project: "eu"},
		{ItemID: "loose", Title: "Loose", Team: "alpha", Assignees: []string{"dev"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "loose-sub", Title: "Loose piece", Parent: "loose", Team: "alpha"},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today}}
	return b
}

// The roadmap view is the hierarchy: every planning card and every card with
// one above it — a task, its subtask, a project card — and nothing else. A
// task with no planning card above it is the day boards' (H3, H7).
func TestTheRoadmapListsTheHierarchy(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	got := []string{}
	for _, c := range FilterCards(b, Selector{View: "roadmap"}) {
		got = append(got, c.ItemID)
	}
	want := []string{"goal", "init", "epic", "t1", "t1a", "p1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("roadmap = %v, want %v", got, want)
	}
	// And the day boards never list a planning card, while the task under
	// the epic stands on them like any task.
	for _, view := range []string{"team", "me", "triage"} {
		sel := Selector{View: view, Team: "alpha", User: "dev", Day: board.TodayIso(), From: board.MondayOf(board.TodayIso()), Weeks: 1}
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
	// planning card and a task under one, not a loose task.
	if !Drawn(b, Selector{View: "roadmap"}, "epic") || !Drawn(b, Selector{View: "roadmap"}, "t1") {
		t.Error("the roadmap draws the epic and the task under it")
	}
	if Drawn(b, Selector{View: "roadmap"}, "loose") {
		t.Error("a task with no planning card above it is not on the roadmap")
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
	// t1 at 40, p1 at 0: in progress, 0 of 2 (the subtask t1a is t1's own).
	if h := goal.Status.Hierarchy; h == nil || h.Status != board.HierarchyInProgress || h.Done != 0 || h.Total != 2 {
		t.Fatalf("hierarchy = %+v, want in-progress 0/2", h)
	}
	if CardResource(b, find("loose")).Status.Lineage != nil {
		t.Fatal("a root has no lineage")
	}
}
