package board

import (
	"reflect"
	"testing"
)

// The Index answers exactly what the one-card helpers answer, card for card
// — subtasks, planning cards, a dangling parent, a review card, the counts
// — because a listing builds one and asks it of every card, and a watcher
// of the Strategy board must read the same standing a single GET reads.
func TestIndexAgreesWithTheLinearHelpers(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	b.Cards = append(b.Cards,
		// A review of the task under the epic, with a reviewer on it.
		Card{ItemID: "t1r", Title: "review: Invoices", ReviewOf: "t1", Parent: "epic", Team: "alpha", Assignees: []string{"rev"}},
		// In the strip, and parked: the triage question has both answers.
		Card{ItemID: "strip", Title: "Arrived"},
		Card{ItemID: "shelf", Title: "Someday", Team: "alpha", Parked: true},
	)
	ix := NewIndex(b)
	for _, c := range b.Cards {
		if got, want := ix.IsSubtask(c), IsSubtask(b, c); got != want {
			t.Errorf("%s: IsSubtask index=%v linear=%v", c.ItemID, got, want)
		}
		if got, want := ix.Lineage(c), Lineage(b, c); !reflect.DeepEqual(ids(got), ids(want)) {
			t.Errorf("%s: Lineage index=%v linear=%v", c.ItemID, ids(got), ids(want))
		}
		if got, want := ix.HasPlanningAncestor(c), HasPlanningAncestor(b, c); got != want {
			t.Errorf("%s: HasPlanningAncestor index=%v linear=%v", c.ItemID, got, want)
		}
		if got, want := ix.ChildrenOf(c.ItemID), ChildrenOf(b, c.ItemID); !reflect.DeepEqual(ids(got), ids(want)) {
			t.Errorf("%s: ChildrenOf index=%v linear=%v", c.ItemID, ids(got), ids(want))
		}
		if got, want := ix.LeafTasks(c.ItemID), LeafTasks(b, c.ItemID); !reflect.DeepEqual(ids(got), ids(want)) {
			t.Errorf("%s: LeafTasks index=%v linear=%v", c.ItemID, ids(got), ids(want))
		}
		s1, d1, n1 := ix.HierarchyStatus(c)
		s2, d2, n2 := HierarchyStatus(b, c)
		if s1 != s2 || d1 != d2 || n1 != n2 {
			t.Errorf("%s: HierarchyStatus index=%s %d/%d linear=%s %d/%d", c.ItemID, s1, d1, n1, s2, d2, n2)
		}
		if got, want := ix.OnBacklog(c), OnBacklog(c, func(c Card) bool { return IsSubtask(b, c) }); got != want {
			t.Errorf("%s: OnBacklog index=%v linear=%v", c.ItemID, got, want)
		}
		p1, ok1 := ix.ParentOf(c)
		p2, ok2 := ParentOf(b, c)
		if ok1 != ok2 || p1.ItemID != p2.ItemID {
			t.Errorf("%s: ParentOf index=%v,%s linear=%v,%s", c.ItemID, ok1, p1.ItemID, ok2, p2.ItemID)
		}
	}
	// A card not on the board, and the dangling parent itself.
	if _, ok := ix.Find("gone"); ok {
		t.Error("a dangling parent is not found")
	}
	// The loader dropped t4's dangling parent: a root, in the index as in the
	// linear helpers.
	if c, _ := ix.Find("t4"); c.Parent != "" || ix.IsSubtask(c) {
		t.Error("a card whose parent the board does not hold reads as a root")
	}
	// The reviewer, as the resource's own walk finds it.
	if got := ix.ReviewedBy(Card{ItemID: "t1"}); got != "rev" {
		t.Errorf("ReviewedBy(t1) = %q, want rev", got)
	}
	if got := ix.ReviewedBy(Card{ItemID: "t2"}); got != "" {
		t.Errorf("ReviewedBy(t2) = %q, want none", got)
	}
}
