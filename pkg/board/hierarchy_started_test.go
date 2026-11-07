package board

import "testing"

// A LEAF HAS STARTED when it carries any progress, or stands in review or in
// locked, as well as when somebody has it or it is in progress (#287). A
// task held in review or locked is not "in progress" by the band, and one
// at a few percent may have nobody on it — yet each is plainly under way,
// and an epic over them read "to do". A task at 0% that nobody has, in no
// such stage, is still not started.
func TestALeafWithProgressOrHeldInReviewOrLockedHasStarted(t *testing.T) {
	for _, tc := range []struct {
		name string
		leaf Card
		want string
	}{
		{"a task at 60%, locked", Card{ItemID: "l", Parent: "e", Team: "a", Progress: 60, Stage: StageLocked}, HierarchyInProgress},
		{"a task at 90%, on review", Card{ItemID: "l", Parent: "e", Team: "a", Progress: 90, Stage: StageReview}, HierarchyInProgress},
		{"a task on review at 0%", Card{ItemID: "l", Parent: "e", Team: "a", Stage: StageReview}, HierarchyInProgress},
		{"a task at 5%, nobody's", Card{ItemID: "l", Parent: "e", Team: "a", Progress: 5}, HierarchyInProgress},
		{"a task at 0% that nobody has", Card{ItemID: "l", Parent: "e", Team: "a"}, HierarchyTodo},
		{"a refused task at 0%", Card{ItemID: "l", Parent: "e", Team: "a", Stage: StageRefuse}, HierarchyTodo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBoard([]Card{{ItemID: "e", Title: "Epic", Type: TypeEpic}, tc.leaf})
			epic, _ := Find(b, "e")
			if st, _, _ := HierarchyStatus(b, epic); st != tc.want {
				t.Fatalf("the epic = %q, want %q", st, tc.want)
			}
		})
	}
}
