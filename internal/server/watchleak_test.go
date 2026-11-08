package server

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

// A PLAN ITEM's counts in a frame are what its watcher's LISTING says: the
// listing is built on their visible board, where the tasks of a repository
// they cannot read are absent, so status.hierarchy counts only the tasks
// they can read. The frame used to count the whole board — an epic of the
// primary over tasks in a closed repository read "backlog, 0 of 1" in the
// list and "in progress, 0 of 3" on the stream, which told the watcher how
// much work, and how far along, sat where they cannot look.
func TestAPlanItemsFrameCountsOnlyTheTasksItsWatcherMayRead(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "open", Title: "Open task", Parent: "epic", Team: "alpha", Parked: true},
		{ItemID: "c1", Title: "Secret one", Parent: "epic", Team: "alpha", Domain: "closed", SprintStart: "2026-11-02", StartDate: "2026-11-02"},
		{ItemID: "c2", Title: "Secret two", Parent: "epic", Team: "alpha", Domain: "closed", SprintStart: "2026-11-02", StartDate: "2026-11-02"},
		// A sub-epic the watcher cannot read: nothing under it counts either.
		{ItemID: "sub", Title: "Secret epic", Type: board.TypeEpic, Parent: "epic", Domain: "closed"},
		{ItemID: "under", Title: "Under the secret epic", Parent: "sub", Team: "alpha", Progress: 100},
	})
	e := seedEntry(store, "k", b)
	rights := rightsOn([]string{"shared"}, nil)
	narrow, cancel := store.subscribeAs("k", "bob", nil, map[string]bool{"cards": true}, rights)
	defer cancel()
	wide, cancel2 := store.subscribeAs("k", "lead", nil, map[string]bool{"cards": true}, rightsOn([]string{"shared", "closed"}, nil))
	defer cancel2()
	e.mu.Lock()
	c, _ := board.Find(e.board, "epic")
	c.Title += "!"
	e.upsertCard(c)
	e.cardChanged("", c, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)

	e.mu.Lock()
	visible := board.Visible(e.board, rights.primary, rights.readable)
	whole := e.board
	e.mu.Unlock()
	ep, _ := board.Find(visible, "epic")
	want := apiserver.CardResource(visible, ep).Status.Hierarchy
	got := cardFrames(narrow)["epic"].Status.Hierarchy
	if got == nil || want == nil || *got != *want {
		t.Fatalf("the frame counts %+v, the listing %+v", got, want)
	}
	if got.Total != 1 || got.Status != board.HierarchyBacklog {
		t.Fatalf("the frame counts %+v, want the one readable task, in the backlog", got)
	}
	// A watcher who reads everything is told the whole count.
	full, _ := board.Find(whole, "epic")
	if w, all := cardFrames(wide)["epic"].Status.Hierarchy, apiserver.CardResource(whole, full).Status.Hierarchy; w == nil || *w != *all {
		t.Fatalf("the full frame counts %+v, want %+v", w, all)
	}
}

// A REVIEW card's team comes from its task, and the task's from its own
// ancestors (O13). When the task is readable but its PARENT TASK is not, the
// review's frame must not inherit through that parent — the listing on the
// watcher's visible board ends the chain there.
func TestAReviewsFrameDoesNotInheritThroughItsTasksUnreadableParent(t *testing.T) {
	store := newBoardStore()
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "secret", Title: "Secret parent", Team: "ops", Domain: "closed", Parked: true},
		{ItemID: "t", Title: "Open piece", Parent: "secret", Stage: board.StageReview, SprintStart: "2026-11-02"},
		{ItemID: "r", Title: "review: Open piece", ReviewOf: "t", Parked: true},
	})
	e := seedEntry(store, "k", b)
	rights := rightsOn([]string{"shared"}, nil)
	narrow, cancel := store.subscribeAs("k", "bob", nil, map[string]bool{"cards": true}, rights)
	defer cancel()
	e.mu.Lock()
	c, _ := board.Find(e.board, "r")
	c.Title += "!"
	e.upsertCard(c)
	e.cardChanged("", c, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)

	st := cardFrames(narrow)["r"].Status
	if st.EffectiveTeam != "" {
		t.Fatalf("the review inherits team %q through a parent its watcher cannot read", st.EffectiveTeam)
	}
	e.mu.Lock()
	visible := board.Visible(e.board, rights.primary, rights.readable)
	e.mu.Unlock()
	rv, _ := board.Find(visible, "r")
	if listed := apiserver.CardResource(visible, rv).Status; listed.EffectiveTeam != st.EffectiveTeam {
		t.Fatalf("the frame says %q, the listing %q", st.EffectiveTeam, listed.EffectiveTeam)
	}
}
