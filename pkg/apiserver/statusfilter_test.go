package apiserver

import (
	"net/url"
	"reflect"
	"slices"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// O14 — the Roadmap's STATUS filter, over each task's work status
// (board.WorkStatus): a task matches by its own, a subtask rides its parent
// task, a review card follows the task it reviews, and a plan item matches
// when any task under it does — a plan item with no tasks matches no status
// at all. Every match comes with its ancestors; several statuses are OR, and
// the filter is AND with team, customer and owner.
func statusBoard() board.Board {
	const day = "2026-11-02"
	b := board.NewBoard([]board.Card{
		{ItemID: "goal", Title: "Grow", Type: board.TypeGoal},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "goal", Team: "portal"},
		{ItemID: "work", Title: "Receipts", Parent: "epic", SprintStart: day, StartDate: day},
		{ItemID: "kid", Title: "Receipts PDF", Parent: "work", Parked: true},
		// A review filed under the epic by a direct writer (the Roadmap lists
		// it then): parked itself, it follows the task it reviews.
		{ItemID: "rev", Title: "review: Receipts", ReviewOf: "work", Parent: "epic", Assignees: []string{"bob"}, Parked: true},
		{ItemID: "shelf", Title: "Invoices", Parent: "epic", Team: "cozystack", Parked: true},
		{ItemID: "fin", Title: "Refunds", Parent: "epic", Progress: 100, StartDate: day},
		{ItemID: "empty", Title: "Docs", Type: board.TypeEpic, Parent: "goal"},
		{ItemID: "loose", Title: "Loose", Team: "ops", SprintStart: day},
	})
	return b
}

func TestTheRoadmapStatusFilter(t *testing.T) {
	t.Parallel()
	b := statusBoard()
	for _, tc := range []struct {
		status string
		want   []string
	}{
		// No filter: the listing as it was.
		{"", []string{"goal", "epic", "work", "kid", "rev", "shelf", "fin", "empty", "loose"}},
		// The work in hand, with its ancestors: the subtask rides its task
		// (parked itself), the review follows the task it reviews.
		{"in-progress", []string{"goal", "epic", "work", "kid", "rev", "loose"}},
		{"backlog", []string{"goal", "epic", "shelf"}},
		{"done", []string{"goal", "epic", "fin"}},
		// In progress and Backlog alone: everything but the finished task —
		// and the plan item with no tasks, which neither names.
		{"in-progress,backlog", []string{"goal", "epic", "work", "kid", "rev", "shelf", "loose"}},
		// Hide done is every value but done: the empty epic is not done, so
		// it stays (no-items).
		{"backlog,in-progress,locked,no-items", []string{"goal", "epic", "work", "kid", "rev", "shelf", "empty", "loose"}},
		{"no-items", []string{"goal", "empty"}},
		// Nothing on this board waits for anything.
		{"locked", nil},
		{"in-progress,backlog,done", []string{"goal", "epic", "work", "kid", "rev", "shelf", "fin", "loose"}},
	} {
		if got := roadmapIDs(b, Selector{Status: tc.status}); !slices.Equal(got, tc.want) {
			t.Errorf("status=%q = %v, want %v", tc.status, got, tc.want)
		}
	}
	// AND with the team filter: the epic's own team draws the plan, the task
	// must be in progress.
	if got := roadmapIDs(b, Selector{Status: "backlog", Team: "cozystack"}); !reflect.DeepEqual(got, []string{"goal", "epic", "shelf"}) {
		t.Errorf("backlog of cozystack = %v", got)
	}
	if got := roadmapIDs(b, Selector{Status: "done", Team: "ops"}); len(got) != 0 {
		t.Errorf("nothing of ops is done, got %v", got)
	}
	// A filter is the server's alone: the client cannot mirror membership.
	if (Selector{View: "roadmap", Status: "done"}).Mirrored() {
		t.Error("a status-filtered roadmap is mirrored by the client")
	}
}

func TestTheStatusSelectorIsReadAndChecked(t *testing.T) {
	t.Parallel()
	sel, err := ParseSelector(url.Values{"view": {"roadmap"}, "status": {"in-progress,backlog"}})
	if err != nil || sel.Status != "in-progress,backlog" {
		t.Fatalf("parsed %+v, %v", sel, err)
	}
	if _, err := ParseSelector(url.Values{"view": {"roadmap"}, "status": {"in-progress,todo"}}); err == nil {
		t.Fatal("an unknown status was accepted")
	}
	if _, err := ParseSelector(url.Values{"view": {"roadmap"}, "status": {""}}); err != nil {
		t.Fatalf("an empty status is no filter: %v", err)
	}
	// locked and no-items are values of the filter too, the whole of Hide
	// done among them.
	if _, err := ParseSelector(url.Values{"view": {"roadmap"}, "status": {"backlog,in-progress,locked,no-items"}}); err != nil {
		t.Fatalf("Hide done was refused: %v", err)
	}
	for _, bad := range []string{"no items", "blocked", "review"} {
		if _, err := ParseSelector(url.Values{"view": {"roadmap"}, "status": {bad}}); err == nil {
			t.Errorf("status %q was accepted", bad)
		}
	}
}

// hideDone is the toolbar's one press: every status filter value but done.
const hideDone = "backlog,in-progress,locked,no-items"

// O14 — Locked and No items, and what Hide done keeps. The client epics wait
// for the product epic (N3) and have no tasks yet: neither is done, so Hide
// done keeps them — it used to name only In progress and Backlog, which a
// plan item with no tasks never matches, and they vanished. A plan item
// answers Locked and No items BY ITSELF (its own lock, its own roll-up); a
// task answers Locked by its lock, the inherited one included. Done wins: a
// finished card that still waits is done, not locked.
func dependencyBoard() board.Board {
	const day = "2026-11-02"
	return board.NewBoard([]board.Card{
		{ItemID: "ops", Title: "Operations", Type: board.TypeGoal},
		{ItemID: "prod", Title: "Product", Type: board.TypeEpic, Parent: "ops", Team: "cozystack"},
		{ItemID: "pwork", Title: "Build it", Parent: "prod", SprintStart: day},
		// Empty and waiting for the product epic.
		{ItemID: "client", Title: "Acme rollout", Type: board.TypeEpic, Parent: "ops", Team: "portal", DependsOn: []string{"prod"}},
		// Empty, waiting for nothing.
		{ItemID: "empty", Title: "Docs", Type: board.TypeEpic, Parent: "ops"},
		// Every task finished.
		{ItemID: "shipped", Title: "Shipped", Type: board.TypeEpic, Parent: "ops"},
		{ItemID: "sdone", Title: "Shipped task", Parent: "shipped", Progress: 100, StartDate: day},
		// Waiting, with a parked task under it that inherits the wait.
		{ItemID: "held", Title: "Held", Type: board.TypeEpic, Parent: "ops", Team: "cozystack", DependsOn: []string{"prod"}},
		{ItemID: "hwork", Title: "Held task", Parent: "held", Parked: true},
		// Waiting, yet every task finished: done, not locked.
		{ItemID: "closed", Title: "Closed", Type: board.TypeEpic, Parent: "ops", DependsOn: []string{"prod"}},
		{ItemID: "cdone", Title: "Closed task", Parent: "closed", Progress: 100, StartDate: day},
		// A loose task in hand that waits itself.
		{ItemID: "wait", Title: "Waits", SprintStart: day, DependsOn: []string{"pwork"}},
	})
}

func TestTheRoadmapStatusFilterKeepsWhatIsNotDone(t *testing.T) {
	t.Parallel()
	b := dependencyBoard()
	for _, tc := range []struct {
		name string
		sel  Selector
		want []string
	}{
		{"hide done keeps the empty and the waiting epics, drops the finished ones",
			Selector{Status: hideDone}, []string{"ops", "prod", "pwork", "client", "empty", "held", "hwork", "wait"}},
		{"locked: the waiting epics by themselves, the task by its inherited wait, the loose task by its own",
			Selector{Status: "locked"}, []string{"ops", "client", "held", "hwork", "wait"}},
		{"no-items: the plan items with nothing under them, waiting or not",
			Selector{Status: "no-items"}, []string{"ops", "client", "empty"}},
		{"done: the finished epics and their tasks, the waiting one included",
			Selector{Status: "done"}, []string{"ops", "shipped", "sdone", "closed", "cdone"}},
		{"locked or no-items is the union",
			Selector{Status: "locked,no-items"}, []string{"ops", "client", "empty", "held", "hwork", "wait"}},
		// A plan item matching by itself still needs its own fields to pass
		// the other filters: the waiting portal epic, not cozystack's.
		{"locked of portal", Selector{Status: "locked", Team: "portal"}, []string{"ops", "client"}},
		{"locked of cozystack", Selector{Status: "locked", Team: "cozystack"}, []string{"ops", "held", "hwork"}},
		{"no-items of cozystack", Selector{Status: "no-items", Team: "cozystack"}, nil},
	} {
		if got := roadmapIDs(b, tc.sel); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	// The lock lifts when the product epic is done: the client epic is no
	// longer locked, and still has no items.
	for i := range b.Cards {
		if b.Cards[i].ItemID == "pwork" {
			b.Cards[i].Progress = 100
		}
	}
	if got := roadmapIDs(b, Selector{Status: "locked"}); got != nil {
		t.Errorf("nothing waits once the product is done, got %v", got)
	}
	if got := roadmapIDs(b, Selector{Status: "no-items"}); !slices.Equal(got, []string{"ops", "client", "empty"}) {
		t.Errorf("no-items after the product shipped: %v", got)
	}
}

// O14 — a SUBTASK is filtered by its parent task, its own lock included: a
// subtask that waits for something itself (a direct writer's dependsOn) is
// LOCKED on its badge, yet the Locked filter goes by the parent, which waits
// for nothing, and leaves it out — and keeps it where the parent matches.
// It rides its parent on the Roadmap; it is not a filter's match of its own.
func TestALockedSubtaskFollowsItsUnlockedParentTask(t *testing.T) {
	t.Parallel()
	const day = "2026-11-02"
	b := board.NewBoard([]board.Card{
		{ItemID: "goal", Title: "Grow", Type: board.TypeGoal},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "goal"},
		{ItemID: "work", Title: "Receipts", Parent: "epic", SprintStart: day, StartDate: day},
		{ItemID: "kid", Title: "Receipts PDF", Parent: "work", SprintStart: day, DependsOn: []string{"other"}},
		{ItemID: "other", Title: "Fonts", Parent: "epic", Parked: true},
	})
	ix := board.NewIndex(b)
	kid, _ := ix.Find("kid")
	if !CardResource(b, kid).Status.Locked {
		t.Fatal("the subtask's own wait does not lock its badge")
	}
	if got := roadmapIDs(b, Selector{Status: "locked"}); slices.Contains(got, "kid") {
		t.Errorf("locked = %v: the subtask goes by its parent, which waits for nothing", got)
	}
	if got := roadmapIDs(b, Selector{Status: "in-progress"}); !slices.Contains(got, "kid") {
		t.Errorf("in-progress = %v: the subtask rides its parent task in", got)
	}
}

// O14 with N4 — Locked agrees with status.locked on a visitor's board: a
// card waiting for one the visitor cannot read is locked to them (the
// resource counts the blocker as hidden), and the filter finds it.
func TestTheLockedFilterCountsABlockerTheVisitorCannotRead(t *testing.T) {
	t.Parallel()
	whole := board.NewBoard([]board.Card{
		{ItemID: "secret", Title: "Internal", Type: board.TypeEpic, Domain: "private"},
		{ItemID: "sw", Title: "Internal work", Parent: "secret", Domain: "private", SprintStart: "2026-11-02"},
		{ItemID: "client", Title: "Acme rollout", Type: board.TypeEpic, DependsOn: []string{"secret"}},
	})
	v := board.Visible(whole, "main", func(d string) bool { return d == "main" })
	if got := roadmapIDs(v, Selector{Status: "locked"}); !slices.Equal(got, []string{"client"}) {
		t.Fatalf("locked on the visitor's board = %v, want [client]", got)
	}
	ix := board.NewIndex(v)
	c, _ := ix.Find("client")
	if !CardResource(v, c).Status.Locked {
		t.Fatal("the resource does not call the client epic locked")
	}
}

// O14 — the AND across filters is checked on ONE task. A plan item passes by
// a task under it that passes every task-level filter itself — where the
// plan item's own field may stand in for a filter that reads plan items too
// (its team, its customers, its owner); the status is always the task's. It
// used to take the team from one task and the status (or the customer) from
// another, and drew an epic with nothing under it that matched.
func TestTheRoadmapFiltersAreAndedOnOneTask(t *testing.T) {
	t.Parallel()
	const day = "2026-11-02"
	b := board.NewBoard([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Owner: "zed"},
		{ItemID: "a", Title: "A", Parent: "epic", Team: "portal", Assignees: []string{"ann"},
			Customers: []string{"acme"}, Progress: 100, StartDate: day},
		{ItemID: "b", Title: "B", Parent: "epic", Team: "ops", Assignees: []string{"carl"}, SprintStart: day},
		{ItemID: "c", Title: "C", Parent: "epic", Team: "ops", Assignees: []string{"carl"}, Parked: true},
	})
	b.Customers = []board.Customer{{ID: "acme", Name: "Acme"}, {ID: "zeta", Name: "Zeta"}}
	for _, tc := range []struct {
		name string
		sel  Selector
		want []string
	}{
		// portal's task is done, the task in progress is ops'.
		{"team and status", Selector{Team: "portal", Status: "in-progress"}, nil},
		// ann's task is done, the backlog task is carl's.
		{"owner and status", Selector{Owner: "ann", Status: "backlog"}, nil},
		// The same rule mends the older pair: acme's task is portal's,
		// ops' tasks are for nobody.
		{"team and customer", Selector{Team: "ops", Customer: "acme"}, nil},
		// One task passing everything draws the plan and itself.
		{"one task passes both", Selector{Team: "ops", Status: "backlog"}, []string{"epic", "c"}},
		// The plan item's own owner stands in for the owner filter; the
		// status is still a task's.
		{"own owner, a task's status", Selector{Owner: "zed", Status: "in-progress"}, []string{"epic"}},
		{"own owner, a task done or parked", Selector{Owner: "zed", Status: "done,backlog"}, []string{"epic"}},
	} {
		var got []string
		got = append(got, roadmapIDs(b, tc.sel)...)
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
