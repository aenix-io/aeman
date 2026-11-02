package board

import (
	"reflect"
	"testing"
)

// A review card is a copy of the card it reviews, made for the asking, and
// the original answers for the work. Under an epic — a file written straight
// into the repository can put one there — it is not a leaf task: counting it
// put the epic's done/total off by one for every review in flight.
func TestAReviewCardIsNotALeafTask(t *testing.T) {
	t.Parallel()
	b := NewBoard([]Card{
		{ItemID: "epic", Title: "Billing", Type: TypeEpic},
		{ItemID: "task", Title: "the work", Parent: "epic", Team: "alpha", Progress: 40},
		{ItemID: "rev", Title: "review: the work", ReviewOf: "task", Parent: "epic", Team: "alpha", Progress: 10},
	})
	if got := ids(LeafTasks(b, "epic")); !reflect.DeepEqual(got, []string{"task"}) {
		t.Fatalf("LeafTasks(epic) = %v, want the task alone", got)
	}
	epic, _ := Find(b, "epic")
	if st, done, total := HierarchyStatus(b, epic); st != HierarchyInProgress || done != 0 || total != 1 {
		t.Fatalf("epic: %s %d/%d, want in-progress 0/1", st, done, total)
	}
}
