package boardservice

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Q10 — THE BACKLOG "+" BETWEEN AN EPIC'S TASKS MAKES A TASK OF THAT EPIC. A
// create placed by an anchor and asked to inherit (`inheritParent`) takes the
// epic of its context in the team's backlog: the same epic above and below
// → that epic; a boundary → the card above's (none when it has none); the
// top or bottom of the list → the only neighbour's. Without the flag — the
// default, so an agent never files tasks under an epic silently — nothing is
// inherited, and an explicit parent always wins.
func TestABacklogPlusInheritsTheEpicOfItsContext(t *testing.T) {
	seed := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "A", Title: "epic A", Type: board.TypeEpic},
			{ItemID: "B", Title: "epic B", Type: board.TypeEpic},
			{ItemID: "a1", Title: "a1", Team: "alpha", Parked: true, Parent: "A"},
			{ItemID: "a2", Title: "a2", Team: "alpha", Parked: true, Parent: "A"},
			{ItemID: "n1", Title: "n1", Team: "alpha", Parked: true},
			{ItemID: "b1", Title: "b1", Team: "alpha", Parked: true, Parent: "B"},
		}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
	}
	parentOf := func(t *testing.T, args CreateCardArgs) string {
		t.Helper()
		f := seed()
		args.Title, args.Team = "new", "alpha"
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog, args)
		if err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.get(c.ItemID).Parent
	}
	for _, tc := range []struct {
		name string
		args CreateCardArgs
		want string
	}{
		{"between two tasks of one epic", CreateCardArgs{After: "a1", InheritParent: true}, "A"},
		{"at a boundary, the card above's", CreateCardArgs{After: "a2", InheritParent: true}, "A"},
		{"at a boundary below a neutral task, none", CreateCardArgs{After: "n1", InheritParent: true}, ""},
		{"at the top, the card below's", CreateCardArgs{Before: "a1", InheritParent: true}, "A"},
		{"at the bottom, the card above's", CreateCardArgs{After: "b1", InheritParent: true}, "B"},
		{"without the flag nothing is inherited", CreateCardArgs{After: "a1"}, ""},
		{"an explicit parent wins", CreateCardArgs{After: "a1", Parent: "B", InheritParent: true}, "B"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parentOf(t, tc.args); got != tc.want {
				t.Fatalf("parent = %q, want %q", got, tc.want)
			}
		})
	}
}

// Q10 — a review nobody has taken is no neighbour: a "+" pressed right
// below one (anchored on it) takes the epic of the next task instead.
func TestABacklogPlusSkipsAReviewNobodyHasTaken(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "A", Title: "epic A", Type: board.TypeEpic},
		{ItemID: "r", Title: "review: a1", Team: "alpha", Parked: true, ReviewOf: "a1"},
		{ItemID: "a1", Title: "a1", Team: "alpha", Parked: true, Parent: "A"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
	c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewBacklog,
		CreateCardArgs{Title: "new", Team: "alpha", After: "r", InheritParent: true})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.get(c.ItemID).Parent; got != "A" {
		t.Fatalf("parent = %q, want the epic of the task below the review", got)
	}
}
