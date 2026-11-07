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
	// above it change their standing — no items → in progress (it is in work).
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
		if got[id].Type != "MODIFIED" || h == nil || h.Status != board.HierarchyInProgress || h.Total != 1 {
			t.Fatalf("%s after the attach = %s %+v, want MODIFIED in-progress 0/1 (the task is in work)", id, got[id].Type, h)
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

// A REVIEW CARD UPDATES LIVE WITH ITS TASK. It carries what it shows of the
// card it reviews — status.reviewed's title and people, and the task's chain
// — and no write ever touches it when the task changes, so the task's change
// re-sends its review card(s) in the same fan-out: renamed, the review
// arrives MODIFIED with the new title; handed to somebody else, with the new
// people. A change that moves none of that sends nothing extra.
func TestATasksReviewCardHearsWhatItShowsOfTheTask(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "t", Title: "Receipts", Team: "alpha", Assignees: []string{"ann"}, Stage: board.StageReview,
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "r", Title: "review: Receipts", Team: "alpha", ReviewOf: "t", Parked: true},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}}
	e := seedEntry(store, "k", b)
	backlog := apiserver.Selector{View: "all", Team: "alpha"}
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	scoped, cancel2 := store.subscribe("k", "", &backlog, map[string]bool{"cards": true})
	defer cancel2()
	change := func(edit func(*board.Card)) {
		e.mu.Lock()
		c, _ := board.Find(e.board, "t")
		edit(&c)
		e.upsertCard(c)
		e.cardChanged("", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}

	change(func(c *board.Card) { c.Title = "Receipts, v2" })
	for _, sub := range []*subscription{raw, scoped} {
		got := took(t, sub, 2)
		r := got["r"]
		if r.Type != "MODIFIED" || r.Card.Status.Reviewed == nil || r.Card.Status.Reviewed.Title != "Receipts, v2" {
			t.Fatalf("after the rename the review card = %+v, want MODIFIED reviewing «Receipts, v2»", r)
		}
	}

	change(func(c *board.Card) { c.Assignees = []string{"bob"} })
	for _, sub := range []*subscription{raw, scoped} {
		got := took(t, sub, 2)
		if rv := got["r"].Card.Status.Reviewed; rv == nil || len(rv.Assignees) != 1 || rv.Assignees[0] != "bob" {
			t.Fatalf("after the hand-over the review card shows %+v, want bob", rv)
		}
	}

	// Progress is not what a review card shows: the task alone.
	change(func(c *board.Card) { c.Progress = 70 })
	if got := took(t, raw, 1); got["t"].Type != "MODIFIED" {
		t.Fatalf("the task's own frame: %+v", got)
	}
}

// A PLAN ITEM'S TEAM AND CUSTOMERS REACH THE TASKS DIRECTLY UNDER IT. Their
// effective team and customers (ownership.md, O3, O4) are computed from the
// plan item and no write touches them, so naming a customer on an epic
// re-sends the tasks under it with their new effective customers — as a
// rename re-sends their lineage.
func TestAPlanItemsCustomersReachTheTasksUnderIt(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Team: "alpha", Parked: true},
	})
	b.Customers = []board.Customer{{ID: "cu1", Name: "Hidora"}}
	e := seedEntry(store, "k", b)
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	e.mu.Lock()
	c, _ := board.Find(e.board, "epic")
	c.Customers = []string{"cu1"}
	c.Team = "beta"
	e.upsertCard(c)
	e.cardChanged("", c, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	got := took(t, raw, 2)
	task := got["t"].Card
	if got["t"].Type != "MODIFIED" || len(task.Status.EffectiveCustomers) != 1 || task.Status.EffectiveTeam != "alpha" {
		t.Fatalf("the task under the epic = %+v, want MODIFIED with Hidora and its own team", task.Status)
	}
}

// AND THE WHOLE SUBTREE, WHEN IT IS THE TEAM OR THE CUSTOMERS. A goal's
// customers are every card's below it, through every level (O4), so naming
// one re-sends the initiative, the epic, the task, its subtask and the task's
// review card — each carries the new effective customers. A task's own
// customers reach its subtask and its review card the same way. A title
// change still reaches the direct tasks alone (their lineage), as before.
func TestAnOwnershipChangeReachesTheWholeSubtree(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "goal", Title: "Grow", Type: board.TypeGoal},
		{ItemID: "init", Title: "Portugal", Type: board.TypeInitiative, Parent: "goal"},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "init"},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Team: "alpha", Parked: true},
		{ItemID: "sub", Title: "Receipt PDF", Parent: "t", Team: "alpha"},
		{ItemID: "r", Title: "review: Receipts", ReviewOf: "t", Team: "alpha", Parked: true},
	})
	b.Customers = []board.Customer{{ID: "cu1", Name: "Hidora"}, {ID: "cu2", Name: "Acme"}}
	e := seedEntry(store, "k", b)
	raw, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	change := func(id string, edit func(*board.Card)) {
		e.mu.Lock()
		c, _ := board.Find(e.board, id)
		edit(&c)
		e.upsertCard(c)
		e.cardChanged("", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}

	change("goal", func(c *board.Card) { c.Customers = []string{"cu1"} })
	got := took(t, raw, 6)
	for _, id := range []string{"init", "epic", "t", "sub", "r"} {
		if cs := got[id].Card.Status.EffectiveCustomers; len(cs) != 1 || cs[0] != "cu1" {
			t.Fatalf("%s after the goal's customer = %+v, want Hidora", id, got[id])
		}
	}

	// The task, the three plan items above it (their standing), its subtask
	// and its review card.
	change("t", func(c *board.Card) { c.Customers = []string{"cu2"} })
	got = took(t, raw, 6)
	for _, id := range []string{"sub", "r"} {
		if cs := got[id].Card.Status.EffectiveCustomers; len(cs) != 2 {
			t.Fatalf("%s after the task's customer = %v, want Hidora and Acme", id, cs)
		}
	}

	// A title is shown by the direct tasks' lineage alone.
	change("goal", func(c *board.Card) { c.Title = "Grow more" })
	got = took(t, raw, 1)
	if _, ok := got["goal"]; !ok || len(got) != 1 {
		t.Fatalf("a goal's rename re-sent %v", got)
	}
}

// A FILTERED ROADMAP HAS NO CLIENT MIRROR, SO ITS AUTHOR IS TOLD TOO. The
// team, customer and owner filters are the server's alone (O7, O8): the
// board draws what it was sent. An author's own ADDED and DELETED used to be
// held back — their tab was assumed to have placed the card itself — while
// the ancestors that came along only for that card were sent as DELETED, so
// the author's Roadmap kept the card with no chain above it. On a filtered
// Roadmap the author is sent the membership changes like everyone else; only
// the MODIFIED echo of their own write is spared.
func TestAFilteredRoadmapTellsItsAuthorWhatLeftAndEntered(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "goal", Title: "Grow", Type: board.TypeGoal},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "goal"},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Team: "alpha", Assignees: []string{"ann"}},
		{ItemID: "loose", Title: "Docs", Team: "alpha", Assignees: []string{"ann"}},
	})
	e := seedEntry(store, "k", b)
	sel := &apiserver.Selector{View: "roadmap", Owner: "ann"}
	author, cancel := store.subscribe("k", "me", sel, map[string]bool{"cards": true})
	defer cancel()
	other, cancel2 := store.subscribe("k", "other", sel, map[string]bool{"cards": true})
	defer cancel2()
	assign := func(who string) {
		e.mu.Lock()
		c, _ := board.Find(e.board, "t")
		c.Assignees = []string{who}
		e.upsertCard(c)
		e.cardChanged("me", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}

	// t leaves the filter, and with it the goal and the epic, which were
	// drawn only as its chain.
	assign("bob")
	for name, sub := range map[string]*subscription{"author": author, "other": other} {
		got := took(t, sub, 3)
		for _, id := range []string{"goal", "epic", "t"} {
			if got[id].Type != "DELETED" {
				t.Fatalf("%s: %s = %q, want DELETED (%v)", name, id, got[id].Type, got)
			}
		}
	}
	// And it enters again, with its chain.
	assign("ann")
	for name, sub := range map[string]*subscription{"author": author, "other": other} {
		got := took(t, sub, 3)
		for _, id := range []string{"goal", "epic", "t"} {
			if got[id].Type != "ADDED" {
				t.Fatalf("%s: %s = %q, want ADDED (%v)", name, id, got[id].Type, got)
			}
		}
	}
}

// An unfiltered Roadmap keeps sparing its author: the board places a card
// it moved by the same rules (roadmap.ts), so an echo would only make it
// jump.
func TestAnUnfilteredRoadmapSparesItsAuthor(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "t", Title: "Receipts", Team: "alpha", Assignees: []string{"ann"}},
	})
	e := seedEntry(store, "k", b)
	author, cancel := store.subscribe("k", "me", &apiserver.Selector{View: "roadmap"}, map[string]bool{"cards": true})
	defer cancel()
	e.mu.Lock()
	c, _ := board.Find(e.board, "t")
	c.Parent = "epic"
	e.upsertCard(c)
	e.cardChanged("me", c, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	got := took(t, author, 1)
	if got["t"].Type != "" || got["epic"].Type != "MODIFIED" {
		t.Fatalf("the author was sent %v, want the epic's new standing alone", got)
	}
}

// O4, O11 — the Team board and the Backlog filter by effective customers on
// what they hold, so a change to an ancestor's customers must reach the
// tasks on them: the subtree re-send (subtreeOwning) reaches a scoped watch
// as MODIFIED frames for the descendants its view holds, carrying the new
// effective customers. Not only the unscoped stream.
func TestAnAncestorsCustomersReachAScopedWatch(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Customers: []string{"cu1"}},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Team: "alpha", Assignees: []string{"ann"},
			StartDate: today, Day: today, SprintStart: today},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}}
	b.Customers = []board.Customer{{ID: "cu1", Name: "Hidora"}}
	e := seedEntry(store, "k", b)
	me, cancel := store.subscribe("k", "", &apiserver.Selector{View: "all", Team: "alpha"}, map[string]bool{"cards": true})
	defer cancel()
	e.mu.Lock()
	c, _ := board.Find(e.board, "epic")
	c.Customers = nil
	e.upsertCard(c)
	e.cardChanged("other", c, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	got := took(t, me, 1)
	if got["t"].Type != "MODIFIED" || len(got["t"].Card.Status.EffectiveCustomers) != 0 {
		t.Fatalf("the Team board's watch heard %+v, want the task MODIFIED with no customer", got)
	}
}

// O11 — the Team board's and the Backlog's watch narrowed by customer: a
// card that gains a chosen customer enters it as ADDED, one that loses it
// leaves as DELETED — for every other watcher. Its author is spared both:
// these boards apply the same rule to what they hold (customerPasses), so
// their own change has already placed the card.
func TestACustomerFilteredWatchSeesCardsEnterAndLeave(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "t", Title: "Receipts", Team: "alpha", Parked: true},
	})
	b.Customers = []board.Customer{{ID: "cu1", Name: "Hidora"}}
	e := seedEntry(store, "k", b)
	sel := &apiserver.Selector{View: "all", Team: "alpha", Customer: "cu1"}
	viewer, cancel := store.subscribe("k", "viewer", sel, map[string]bool{"cards": true})
	defer cancel()
	author, cancel2 := store.subscribe("k", "me", sel, map[string]bool{"cards": true})
	defer cancel2()
	set := func(ids []string) {
		e.mu.Lock()
		c, _ := board.Find(e.board, "t")
		c.Customers = ids
		e.upsertCard(c)
		e.cardChanged("me", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}
	set([]string{"cu1"})
	if got := took(t, viewer, 1); got["t"].Type != "ADDED" {
		t.Fatalf("gaining Hidora: the viewer heard %v, want ADDED", got)
	}
	noFrame(t, author)
	set(nil)
	if got := took(t, viewer, 1); got["t"].Type != "DELETED" {
		t.Fatalf("losing Hidora: the viewer heard %v, want DELETED", got)
	}
	noFrame(t, author)
}

// O13 — a task with no team of its own stands under its plan item's team, so
// when the plan item's team changes the task MOVES between teams' lists: it
// leaves the old team's Team-board and Backlog watch as DELETED and enters
// the new team's as ADDED — though no write touched the task.
func TestAPlanItemsTeamMovesTheTasksBelowBetweenWatches(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Parked: true},
		{ItemID: "own", Title: "Own team", Parent: "epic", Team: "ops", Parked: true},
	})
	e := seedEntry(store, "k", b)
	portal, cancel := store.subscribe("k", "viewer", &apiserver.Selector{View: "all", Team: "portal"}, map[string]bool{"cards": true})
	defer cancel()
	docs, cancel2 := store.subscribe("k", "viewer2", &apiserver.Selector{View: "all", Team: "docs"}, map[string]bool{"cards": true})
	defer cancel2()
	e.mu.Lock()
	c, _ := board.Find(e.board, "epic")
	c.Team = "docs"
	e.upsertCard(c)
	e.cardChanged("lead", c, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	if got := took(t, portal, 1); got["t"].Type != "DELETED" {
		t.Fatalf("portal's watch heard %v, want the task DELETED", got)
	}
	if got := took(t, docs, 1); got["t"].Type != "ADDED" || got["t"].Card.Status.EffectiveTeam != "docs" {
		t.Fatalf("docs's watch heard %v, want the task ADDED under docs", got)
	}
}

// And the Team board's day grid and the Backlog's own view: a plan item's
// team change and a task filed under another plan item move the task — its
// subtask riding it on the grid — between the teams' watches.
func TestPlacementMovesTasksBetweenTheGridAndBacklogWatches(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "docsEpic", Title: "Docs", Type: board.TypeEpic, Team: "docs"},
		{ItemID: "w", Title: "In work", Parent: "epic", Assignees: []string{"ann"}, StartDate: today, Day: today, SprintStart: today},
		{ItemID: "ws", Title: "Its subtask", Parent: "w", Assignees: []string{"ann"}, StartDate: today, Day: today, SprintStart: today},
		{ItemID: "p", Title: "Parked", Parent: "epic", Parked: true},
	})
	b.SprintStates = map[string]board.SprintState{"portal": {Current: today}, "docs": {Current: today}}
	e := seedEntry(store, "k", b)
	grid := func(team string) *subscription {
		s, cancel := store.subscribe("k", "", &apiserver.Selector{View: "team", Team: team, Day: today}, map[string]bool{"cards": true})
		t.Cleanup(cancel)
		return s
	}
	backlog := func(team string) *subscription {
		s, cancel := store.subscribe("k", "", &apiserver.Selector{View: "backlog", Team: team}, map[string]bool{"cards": true})
		t.Cleanup(cancel)
		return s
	}
	portalGrid, docsGrid, portalBacklog, docsBacklog := grid("portal"), grid("docs"), backlog("portal"), backlog("docs")
	change := func(id string, edit func(*board.Card)) {
		e.mu.Lock()
		c, _ := board.Find(e.board, id)
		edit(&c)
		e.upsertCard(c)
		e.cardChanged("lead", c, "MODIFIED")
		e.mu.Unlock()
		settled(t, e)
	}
	change("epic", func(c *board.Card) { c.Team = "docs" })
	if got := took(t, portalGrid, 2); got["w"].Type != "DELETED" || got["ws"].Type != "DELETED" {
		t.Fatalf("portal's grid heard %v, want the task and its subtask DELETED", got)
	}
	if got := took(t, docsGrid, 2); got["w"].Type != "ADDED" || got["ws"].Type != "ADDED" {
		t.Fatalf("docs's grid heard %v, want the task and its subtask ADDED", got)
	}
	if got := took(t, portalBacklog, 1); got["p"].Type != "DELETED" {
		t.Fatalf("portal's backlog heard %v", got)
	}
	if got := took(t, docsBacklog, 1); got["p"].Type != "ADDED" {
		t.Fatalf("docs's backlog heard %v", got)
	}
	// Taken out from under the epic, the parked task leaves docs's backlog.
	change("p", func(c *board.Card) { c.Parent = "" })
	if got := took(t, docsBacklog, 1); got["p"].Type != "DELETED" {
		t.Fatalf("unfiled: docs's backlog heard %v", got)
	}
}

// A DELETED CARD IS NEVER LEFT ON ITS AUTHOR'S BOARD. Deleting a plan item
// is one action of several writes: its children move up first, and only then
// does the card go. The children's move names the plan item as the chain
// they left, so a fan-out that runs between the writes — the coalescing
// timer firing while the delete is still being written — sends the author a
// MODIFIED for the plan item, which is still there. The DELETED that follows
// was then swallowed as the author's own echo, and their board drew the
// plan item again: "the epic comes back, I have to delete it twice". A card
// gone from the board is told as DELETED to everyone, its author included —
// a DELETED for a card a board already took off changes nothing.
func TestADeletedPlanItemIsToldDeletedToItsAuthorLast(t *testing.T) {
	for _, withChildren := range []bool{true, false} {
		store := newBoardStore()
		cards := []board.Card{
			{ItemID: "goal", Title: "Grow", Type: board.TypeGoal},
			{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "goal"},
		}
		if withChildren {
			cards = append(cards, board.Card{ItemID: "t", Title: "Receipts", Parent: "epic", Team: "alpha", Parked: true})
		}
		e := seedEntry(store, "k", board.NewBoardIn("acme", cards))
		author, cancel := store.subscribe("k", "me", &apiserver.Selector{View: "roadmap"}, map[string]bool{"cards": true})
		if withChildren {
			// The child moves up to the goal: a write the request did not
			// address, so it is nobody's echo.
			e.mu.Lock()
			c, _ := board.Find(e.board, "t")
			c.Parent = "goal"
			e.upsertCard(c)
			e.cardChanged("", c, "MODIFIED")
			e.mu.Unlock()
			// The coalesced fan-out fires while the delete is being written.
			e.flushMembers()
		}
		// The plan item goes, by the author's own request.
		e.mu.Lock()
		epic, _ := board.Find(e.board, "epic")
		e.removeCard("epic")
		e.cardChanged("me", epic, "DELETED")
		e.mu.Unlock()
		settled(t, e)
		var last string
		for {
			select {
			case data := <-author.ch:
				var f struct {
					Type   string `json:"type"`
					Object struct {
						Metadata struct {
							UID string `json:"uid"`
						} `json:"metadata"`
					} `json:"object"`
				}
				_ = json.Unmarshal(data, &f)
				if f.Object.Metadata.UID == "epic" {
					last = f.Type
				}
				continue
			default:
			}
			break
		}
		cancel()
		if withChildren && last != "DELETED" {
			t.Fatalf("with children: the author's last frame for the deleted epic = %q, want DELETED", last)
		}
		if !withChildren && last != "DELETED" {
			t.Fatalf("no children: the author's last frame for the deleted epic = %q, want DELETED", last)
		}
	}
}

// And the raw stream (a watch with no selector) tells the author too: a
// DELETED is never swallowed as an echo, whatever stream carries it.
func TestTheRawStreamTellsTheAuthorADeletedCard(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", board.NewBoardIn("acme", []board.Card{
		{ItemID: "t", Title: "Receipts", Team: "alpha", Parked: true},
	}))
	author, cancel := store.subscribe("k", "me", nil, map[string]bool{"cards": true})
	defer cancel()
	e.mu.Lock()
	c, _ := board.Find(e.board, "t")
	e.removeCard("t")
	e.cardChanged("me", c, "DELETED")
	e.mu.Unlock()
	if got := took(t, author, 1); got["t"].Type != "DELETED" {
		t.Fatalf("the author's raw stream heard %v, want the card DELETED", got)
	}
}
