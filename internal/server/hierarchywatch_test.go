package server

import (
	"encoding/json"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

// hierarchyWatchBoard is a goal → initiative → epic chain, one dated task of
// team alpha standing on its own (open: listed below the tree, H9), and one
// already finished (not listed — nothing left to plan on it).
func hierarchyWatchBoard(today string) board.Board {
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "goal", Title: "Reach Europe", Type: board.TypeGoal},
		{ItemID: "init", Title: "Portugal", Type: board.TypeInitiative, Parent: "goal"},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "init"},
		{ItemID: "c1", Title: "Invoices", Team: "alpha", StartDate: today, Day: today, SprintStart: today},
		{ItemID: "c2", Title: "Shipped", Team: "alpha", StartDate: today, Day: today, SprintStart: today,
			Progress: 100, DoneAt: today},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}}
	return b
}

// took reads n frames off a subscription into a map by uid, decoding the
// card each carries, so a burst's order — the view's — does not matter.
func took(t *testing.T, sub *subscription, n int) map[string]struct {
	Type string
	Card apiserver.Card
} {
	t.Helper()
	out := map[string]struct {
		Type string
		Card apiserver.Card
	}{}
	for i := 0; i < n; i++ {
		f := readFrame(t, sub)
		var c apiserver.Card
		if err := json.Unmarshal(f.Object, &c); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		out[c.Metadata.UID] = struct {
			Type string
			Card apiserver.Card
		}{f.Type, c}
	}
	noFrame(t, sub)
	return out
}

// THE ROADMAP UPDATES LIVE. A planning card's standing is derived from the
// tasks under it (H5) and never written, so no write ever touches the epic
// when a task under it changes — and the Roadmap watcher, which draws the
// epic with its counts, heard nothing. Every card change now reaches the
// planning cards above it: the chain it LEFT and the chain it JOINED, as
// MODIFIED frames carrying the fresh counts, to everyone who may read them
// — the originator included, since the server alone knows the new standing.
func TestAChangeUnderAPlanningCardReachesItsAncestorsOnTheRoadmap(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	e := seedEntry(store, "k", hierarchyWatchBoard(today))
	sel := apiserver.Selector{View: "roadmap"}
	roadmap, cancel := store.subscribe("k", "", &sel, map[string]bool{"cards": true})
	defer cancel()
	origin, cancel2 := store.subscribe("k", "me", &sel, map[string]bool{"cards": true})
	defer cancel2()
	raw, cancel3 := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel3()
	// The open loose task is on the Roadmap from the start — below the tree,
	// where it is dragged in from (H9); the finished one is not.
	if !roadmap.members["epic"] || !roadmap.members["c1"] || roadmap.members["c2"] {
		t.Fatalf("seeded roadmap members = %v", roadmap.members)
	}
	find := func(id string) board.Card {
		c, _ := board.Find(e.board, id)
		return c
	}

	// The task is filed under the epic by "me": it moves INTO the tree (it was
	// listed already, so the frame is a change, not an arrival), and the three
	// above it change their standing — no items → to do.
	e.mu.Lock()
	c1 := find("c1")
	c1.Parent = "epic"
	e.upsertCard(c1)
	e.cardChanged("me", c1, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	got := took(t, roadmap, 4)
	if got["c1"].Type != "MODIFIED" {
		t.Fatalf("the task's own frame: %+v", got["c1"])
	}
	for _, id := range []string{"goal", "init", "epic"} {
		h := got[id].Card.Status.Hierarchy
		if got[id].Type != "MODIFIED" || h == nil || h.Status != board.HierarchyTodo || h.Total != 1 {
			t.Fatalf("%s after the attach = %s %+v, want MODIFIED todo 0/1", id, got[id].Type, h)
		}
	}
	// The originator holds its own change already, but not the standing the
	// server derived: the ancestors reach it, the task's own frame does not.
	mine := took(t, origin, 3)
	if _, echoed := mine["c1"]; echoed || mine["epic"].Type != "MODIFIED" {
		t.Fatalf("the originator got %+v", mine)
	}
	// The raw stream hears the verb and the ancestors alike.
	all := took(t, raw, 4)
	if all["c1"].Type != "MODIFIED" || all["goal"].Type != "MODIFIED" {
		t.Fatalf("the raw stream got %+v", all)
	}

	// Somebody takes the task: in progress, all the way up.
	e.mu.Lock()
	c1 = find("c1")
	c1.Assignees = []string{"ann"}
	e.upsertCard(c1)
	e.cardChanged("", c1, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	got = took(t, roadmap, 4)
	if got["c1"].Type != "MODIFIED" || got["goal"].Card.Status.Hierarchy.Status != board.HierarchyInProgress {
		t.Fatalf("after the assignment: %+v / %+v", got["c1"].Type, got["goal"].Card.Status.Hierarchy)
	}

	// Re-filed straight under the goal: the chain it LEFT (epic, initiative)
	// hears it as much as the one it joined, or the epic would keep a task
	// it no longer has.
	e.mu.Lock()
	c1 = find("c1")
	c1.Parent = "goal"
	e.upsertCard(c1)
	e.cardChanged("", c1, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	got = took(t, roadmap, 4)
	if h := got["epic"].Card.Status.Hierarchy; got["epic"].Type != "MODIFIED" || h.Status != board.HierarchyNoItems || h.Total != 0 {
		t.Fatalf("the epic the task left: %s %+v, want MODIFIED no-items 0/0", got["epic"].Type, h)
	}
	if h := got["goal"].Card.Status.Hierarchy; h.Status != board.HierarchyInProgress || h.Total != 1 {
		t.Fatalf("the goal the task joined: %+v, want in-progress 0/1", h)
	}

	// Deleted: gone from the Roadmap, and the goal has no items again.
	e.mu.Lock()
	gone := find("c1")
	e.removeCard("c1")
	e.cardChanged("", gone, "DELETED")
	e.mu.Unlock()
	settled(t, e)
	got = took(t, roadmap, 2)
	if got["c1"].Type != "DELETED" {
		t.Fatalf("the task did not leave the Roadmap: %+v", got["c1"])
	}
	if h := got["goal"].Card.Status.Hierarchy; got["goal"].Type != "MODIFIED" || h.Status != board.HierarchyNoItems {
		t.Fatalf("the goal after the delete: %s %+v, want MODIFIED no-items", got["goal"].Type, h)
	}

	// A card the Roadmap did NOT list — the finished task — ENTERS it when it
	// is filed under the epic: an arrival for the watcher, and the chain above
	// counts it at once (one done of one: done).
	e.mu.Lock()
	c2 := find("c2")
	c2.Parent = "epic"
	e.upsertCard(c2)
	e.cardChanged("", c2, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	got = took(t, roadmap, 4)
	if got["c2"].Type != "ADDED" {
		t.Fatalf("the finished task did not enter the Roadmap: %+v", got["c2"])
	}
	if h := got["goal"].Card.Status.Hierarchy; got["goal"].Type != "MODIFIED" || h.Status != board.HierarchyDone || h.Done != 1 || h.Total != 1 {
		t.Fatalf("the goal after the finished task joined: %s %+v, want MODIFIED done 1/1", got["goal"].Type, h)
	}
}

// A TASK CARRIES ITS PLANNING CARD'S TITLE AND END (status.lineage), and the
// Backlog draws both — the tag and the due date (K2, K6). The Backlog watches
// `view=all&team=<set>`, which never holds the epic itself, so a retitled or
// re-dated epic reached the Roadmap and not the backlog cards under it, which
// went on showing the old name and the old due date. A planning card whose
// title or dates change therefore re-sends its descendant TASKS, to every
// board whose filter holds them; a change that moves neither (a description)
// sends them nothing.
func TestARetitledOrRedatedPlanningCardReachesTheTasksUnderIt(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	b := hierarchyWatchBoard(today)
	b.Cards = append(b.Cards, board.Card{ItemID: "p1", Title: "Receipts", Team: "alpha", Parent: "epic", Parked: true})
	e := seedEntry(store, "k", b)
	sel := apiserver.Selector{View: "all", Team: "alpha"}
	backlog, cancel := store.subscribe("k", "", &sel, map[string]bool{"cards": true})
	defer cancel()
	find := func(id string) board.Card {
		c, _ := board.Find(e.board, id)
		return c
	}
	change := func(edit func(*board.Card)) {
		e.mu.Lock()
		epic := find("epic")
		edit(&epic)
		e.upsertCard(epic)
		e.cardChanged("", epic, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}

	change(func(c *board.Card) { c.Title = "Payments" })
	got := took(t, backlog, 1)
	lin := got["p1"].Card.Status.Lineage
	if got["p1"].Type != "MODIFIED" || len(lin) == 0 || lin[len(lin)-1].Title != "Payments" {
		t.Fatalf("after the retitle the task under the epic = %+v", got["p1"])
	}

	change(func(c *board.Card) { c.StartDate, c.Day = today, "2026-12-31" })
	got = took(t, backlog, 1)
	lin = got["p1"].Card.Status.Lineage
	if len(lin) == 0 || lin[len(lin)-1].End != "2026-12-31" {
		t.Fatalf("after the re-date the task under the epic = %+v", got["p1"])
	}

	change(func(c *board.Card) { c.Description = "why we bill" })
	noFrame(t, backlog)
}

// Only the NEAREST planning card is shown on a task (K2: the tag and the due
// date are its parent's), so a change re-sends the DIRECT task children of
// the planning card that changed and nothing deeper: a goal retitled sends
// nothing to the tasks under its epics, and an epic retitled sends exactly
// its own tasks — not a frame per task of the whole subtree, which on a big
// tree overflowed a watcher's buffer and dropped the frames after it.
func TestOnlyTheDirectTasksOfARetitledPlanningCardAreResent(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	b := hierarchyWatchBoard(today)
	b.Cards = append(b.Cards,
		board.Card{ItemID: "p1", Title: "Receipts", Team: "alpha", Parent: "epic", Parked: true},
		board.Card{ItemID: "p2", Title: "Refunds", Team: "alpha", Parent: "epic", Parked: true},
		board.Card{ItemID: "p3", Title: "Under the goal", Team: "alpha", Parent: "goal", Parked: true},
		board.Card{ItemID: "k1", Title: "a piece", Team: "alpha", Parent: "p1"},
	)
	e := seedEntry(store, "k", b)
	sel := apiserver.Selector{View: "all", Team: "alpha"}
	backlog, cancel := store.subscribe("k", "", &sel, map[string]bool{"cards": true})
	defer cancel()
	retitle := func(id, title string) {
		e.mu.Lock()
		c, _ := board.Find(e.board, id)
		c.Title = title
		e.upsertCard(c)
		e.cardChanged("", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}

	retitle("goal", "Reach the world")
	got := took(t, backlog, 1)
	if _, ok := got["p3"]; !ok {
		t.Fatalf("the goal's own task was not re-sent: %v", got)
	}

	retitle("epic", "Payments")
	got = took(t, backlog, 2)
	for _, id := range []string{"p1", "p2"} {
		if got[id].Type != "MODIFIED" {
			t.Fatalf("the epic's task %s was not re-sent: %v", id, got)
		}
	}
}
