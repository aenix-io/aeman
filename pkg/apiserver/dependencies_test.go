package apiserver

import (
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func depWireBoard() board.Board {
	b := board.NewBoard([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, DependsOn: []string{"infra"}},
		{ItemID: "task", Title: "Invoices", Parent: "epic", Team: "a", Parked: true, DependsOn: []string{"db", "gone"}},
		{ItemID: "infra", Title: "Infra", Type: board.TypeEpic, Domain: "closed"},
		{ItemID: "infra-t", Title: "Racks", Parent: "infra", Team: "a", Parked: true, Domain: "closed"},
		{ItemID: "db", Title: "DB", Team: "a", Parked: true},
		{ItemID: "w", Title: "Waits", Team: "a", Parked: true, Customers: []string{"acme"}, DependsOn: []string{"db"}},
	})
	b.Customers = []board.Customer{{ID: "acme", Name: "Acme"}}
	return b
}

// N1, N3, N4, N5 on the wire: spec.dependsOn without the dangling id; the
// lock and its blockers with `via` for an inherited wait; what waits for a
// card; the customers it is for through that work, with where from.
func TestACardResourceSaysWhatItWaitsForAndWhatWaitsForIt(t *testing.T) {
	b := depWireBoard()
	find := func(id string) board.Card { c, _ := board.Find(b, id); return c }
	task := CardResource(b, find("task"))
	if !reflect.DeepEqual(task.Spec.DependsOn, []string{"db"}) {
		t.Fatalf("spec.dependsOn = %v, want db alone (gone is dangling)", task.Spec.DependsOn)
	}
	if !task.Status.Locked || len(task.Status.BlockedBy) != 2 || task.Status.HiddenBlockers != 0 {
		t.Fatalf("task status = locked %v blockedBy %+v hidden %d", task.Status.Locked, task.Status.BlockedBy, task.Status.HiddenBlockers)
	}
	if b0, b1 := task.Status.BlockedBy[0], task.Status.BlockedBy[1]; b0.UID != "db" || b0.Via != "" || b1.UID != "infra" || b1.Via != "epic" || b1.Title != "Infra" {
		t.Fatalf("blockedBy = %+v, want db (own), infra via epic", task.Status.BlockedBy)
	}
	db := CardResource(b, find("db"))
	if db.Status.Locked || len(db.Status.Blocks) != 2 || db.Status.Blocks[0].UID != "task" || db.Status.Blocks[1].UID != "w" {
		t.Fatalf("db = locked %v blocks %+v", db.Status.Locked, db.Status.Blocks)
	}
	if !reflect.DeepEqual(db.Status.EffectiveCustomers, []string{"acme"}) || len(db.Status.DependentCustomers) != 1 ||
		db.Status.DependentCustomers[0].Customer != "acme" || db.Status.DependentCustomers[0].From.UID != "w" {
		t.Fatalf("db customers = %v / %+v, want acme from w", db.Status.EffectiveCustomers, db.Status.DependentCustomers)
	}
	if w := CardResource(b, find("w")); len(w.Status.DependentCustomers) != 0 {
		t.Fatalf("a card's own customers are not dependent ones: %+v", w.Status.DependentCustomers)
	}
}

// N4 — on a visitor's visible board, a blocker they cannot read counts and
// is not named, and the hidden id is not in spec.dependsOn.
func TestAHiddenBlockerIsCountedOnTheVisitorsBoard(t *testing.T) {
	v := board.Visible(depWireBoard(), "", func(d string) bool { return d != "closed" })
	find := func(id string) board.Card { c, _ := board.Find(v, id); return c }
	task := CardResource(v, find("task"))
	if !task.Status.Locked || len(task.Status.BlockedBy) != 1 || task.Status.BlockedBy[0].UID != "db" || task.Status.HiddenBlockers != 1 {
		t.Fatalf("task = locked %v blockedBy %+v hidden %d", task.Status.Locked, task.Status.BlockedBy, task.Status.HiddenBlockers)
	}
	epic := CardResource(v, find("epic"))
	if len(epic.Spec.DependsOn) != 0 || !epic.Status.Locked || epic.Status.HiddenBlockers != 1 {
		t.Fatalf("epic = dependsOn %v locked %v hidden %d", epic.Spec.DependsOn, epic.Status.Locked, epic.Status.HiddenBlockers)
	}
}

// N5 — the customer filter reads the inherited customers too.
func TestTheCustomerFilterFindsAWaitedForCard(t *testing.T) {
	b := depWireBoard()
	got := []string{}
	for _, c := range FilterCards(b, Selector{View: "backlog", Customer: "acme"}) {
		got = append(got, c.ItemID)
	}
	if !reflect.DeepEqual(got, []string{"db", "w"}) && !reflect.DeepEqual(got, []string{"w", "db"}) {
		t.Fatalf("backlog for acme = %v, want db and w", got)
	}
}
