package boardservice

import (
	"errors"
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// docs/design/ownership.md: a planning card's owner and team, any card's
// customers, and the board's customer roster.

func ownershipSeed() *fakeBackend {
	f := newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "task", Title: "Receipts", Team: "alpha", Parent: "epic", Parked: true},
		{ItemID: "slot", Title: "BYOK", Scope: "Security", Project: "core"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}, "beta": {Current: board.TodayIso(), ItemID: "s2"}})
	f.b.Customers = []board.Customer{{ID: "hidora", Name: "Hidora"}, {ID: "acme", Name: "Acme"}}
	return f
}

// O9, O1, O2 — `own` on a planning card sets its owner and its team instead
// of being refused: a person needs no team there (they answer for the plan
// item, they do not do it), nobody clears the owner, and nothing about it
// puts the card on a day board — no dates, no sprint, no band.
func TestOwnOnAPlanningCardSetsItsOwnerAndTeam(t *testing.T) {
	f := ownershipSeed()
	svc := New(f)
	if err := svc.Own(ctx, "acme", "epic", "", "ann"); err != nil {
		t.Fatalf("a person alone on an epic = %v, want it taken", err)
	}
	if e := f.get("epic"); e.Owner != "ann" || e.Team != "" || len(e.Assignees) != 0 {
		t.Fatalf("the epic = owner %q team %q assignees %v", e.Owner, e.Team, e.Assignees)
	}
	if err := svc.Own(ctx, "acme", "epic", "beta", "ann"); err != nil {
		t.Fatal(err)
	}
	e := f.get("epic")
	if e.Owner != "ann" || e.Team != "beta" {
		t.Fatalf("the epic = owner %q team %q, want ann of beta", e.Owner, e.Team)
	}
	if board.InWork(*e) || e.SprintStart != "" || e.Zone != board.ZoneNone {
		t.Fatalf("owning an epic put it on a day board: %+v", e)
	}
	if err := svc.Own(ctx, "acme", "epic", "", ""); err != nil {
		t.Fatal(err)
	}
	if e := f.get("epic"); e.Owner != "" || e.Team != "" {
		t.Fatalf("nobody leaves owner %q team %q", e.Owner, e.Team)
	}
	// A task is answered as before: a person needs a team.
	if err := svc.Own(ctx, "acme", "task", "", "ann"); !errors.Is(err, ErrOwnerNeedsTeam) {
		t.Fatalf("a person alone on a task = %v, want ErrOwnerNeedsTeam", err)
	}
}

// O1 — an owner is a planning card's: a task's people are its assignees,
// and a project card has neither (H4).
func TestAnOwnerIsAPlanningCards(t *testing.T) {
	f := ownershipSeed()
	svc := New(f)
	if err := svc.SetOwner(ctx, "acme", "epic", "ann"); err != nil {
		t.Fatal(err)
	}
	if f.get("epic").Owner != "ann" {
		t.Fatal("the owner was not written")
	}
	for _, id := range []string{"task", "slot"} {
		if err := svc.SetOwner(ctx, "acme", id, "ann"); !errors.Is(err, ErrOwnerIsThePlans) {
			t.Fatalf("an owner on %s = %v, want ErrOwnerIsThePlans", id, err)
		}
	}
	// And a planning card's team is its own to set now (O1), placing nothing.
	if err := svc.SetTeam(ctx, "acme", "epic", "alpha", ""); err != nil {
		t.Fatalf("a team on an epic = %v", err)
	}
	if e := f.get("epic"); e.Team != "alpha" || e.SprintStart != "" {
		t.Fatalf("the epic = team %q sprint %q, want alpha and no sprint", e.Team, e.SprintStart)
	}
}

// O5, O6 — customers are the roster's, named by id; any card but a project
// card takes them; each is named once; an id the roster lacks is refused.
func TestACardNamesCustomersFromTheRoster(t *testing.T) {
	f := ownershipSeed()
	svc := New(f)
	if err := svc.SetCustomers(ctx, "acme", "task", []string{"hidora", "acme", "hidora"}); err != nil {
		t.Fatal(err)
	}
	if got := f.get("task").Customers; !reflect.DeepEqual(got, []string{"hidora", "acme"}) {
		t.Fatalf("the task's customers = %v", got)
	}
	if err := svc.SetCustomers(ctx, "acme", "epic", []string{"acme"}); err != nil {
		t.Fatalf("customers on an epic = %v", err)
	}
	if err := svc.SetCustomers(ctx, "acme", "slot", []string{"acme"}); !errors.Is(err, ErrProjectCard) {
		t.Fatalf("customers on a project card = %v, want ErrProjectCard", err)
	}
	if err := svc.SetCustomers(ctx, "acme", "task", []string{"nobody"}); !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("a customer the roster lacks = %v, want ErrCustomerNotFound", err)
	}
	if err := svc.SetCustomers(ctx, "acme", "task", nil); err != nil || len(f.get("task").Customers) != 0 {
		t.Fatalf("clearing them = %v, %v", err, f.get("task").Customers)
	}
}

// O5 — an id the roster no longer holds does not lock the card: a delete
// that raced an edit, or a replayed write, can leave one behind. Only an id
// NEW to the card is checked against the roster; a dangling one already on
// it is dropped when the card is written, and the edit goes through.
// Duplicates are named once.
func TestADanglingCustomerDoesNotLockTheCard(t *testing.T) {
	f := ownershipSeed()
	f.get("task").Customers = []string{"hidora", "gone"}
	svc := New(f)
	if err := svc.SetCustomers(ctx, "acme", "task", []string{"hidora", "gone", "acme", "acme"}); err != nil {
		t.Fatalf("an edit that keeps the dangling id = %v, want it taken", err)
	}
	if got := f.get("task").Customers; !reflect.DeepEqual(got, []string{"hidora", "acme"}) {
		t.Fatalf("the task's customers = %v, want the dangling id dropped and acme once", got)
	}
	f.get("task").Customers = []string{"gone"}
	if err := svc.SetCustomers(ctx, "acme", "task", []string{"gone"}); err != nil {
		t.Fatalf("re-sending the card's own dangling id = %v", err)
	}
	if got := f.get("task").Customers; len(got) != 0 {
		t.Fatalf("the task's customers = %v, want the dangling id cleaned away", got)
	}
	// A NEW id the roster lacks is still refused.
	if err := svc.SetCustomers(ctx, "acme", "task", []string{"hidora", "nobody"}); !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("a new id the roster lacks = %v, want ErrCustomerNotFound", err)
	}
}

// O5 — the roster: add, rename, reorder, delete. A name is said once
// (case aside); a delete reaches every card that names the customer.
func TestTheCustomerRosterActions(t *testing.T) {
	f := ownershipSeed()
	svc := New(f)
	if _, err := svc.AddCustomer(ctx, "acme", "  "); !errors.Is(err, ErrCustomerName) {
		t.Fatalf("an empty name = %v, want ErrCustomerName", err)
	}
	if _, err := svc.AddCustomer(ctx, "acme", "hidora"); !errors.Is(err, ErrCustomerExists) {
		t.Fatalf("a name the roster has = %v, want ErrCustomerExists", err)
	}
	zeta, err := svc.AddCustomer(ctx, "acme", "Zeta")
	if err != nil || zeta.Name != "Zeta" {
		t.Fatalf("add = %+v, %v", zeta, err)
	}
	if err := svc.RenameCustomer(ctx, "acme", zeta.ID, "Acme"); !errors.Is(err, ErrCustomerExists) {
		t.Fatalf("a rename onto another's name = %v, want ErrCustomerExists", err)
	}
	if err := svc.RenameCustomer(ctx, "acme", "nobody", "X"); !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("renaming a customer the roster lacks = %v", err)
	}
	if err := svc.RenameCustomer(ctx, "acme", zeta.ID, "Zeta GmbH"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReorderCustomers(ctx, "acme", []string{zeta.ID, "acme", "hidora"}); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range f.b.Customers {
		order = append(order, c.ID)
	}
	if !reflect.DeepEqual(order, []string{zeta.ID, "acme", "hidora"}) {
		t.Fatalf("the roster order = %v", order)
	}
	_ = svc.SetCustomers(ctx, "acme", "task", []string{"hidora"})
	if err := svc.DeleteCustomer(ctx, "acme", "hidora"); err != nil {
		t.Fatal(err)
	}
	if len(f.get("task").Customers) != 0 {
		t.Fatal("the delete did not reach the card")
	}
	if err := svc.DeleteCustomer(ctx, "acme", "hidora"); !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("deleting it twice = %v", err)
	}
}

// And a planning card is born with its owner, team and customers when the
// Roadmap's create says them.
func TestAPlanningCardIsBornWithItsOwnership(t *testing.T) {
	f := ownershipSeed()
	c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewRoadmap,
		CreateCardArgs{Title: "Grow", Type: board.TypeGoal, Team: "alpha", Owner: "ann", Customers: []string{"acme"}})
	if err != nil {
		t.Fatal(err)
	}
	got := f.get(c.ItemID)
	if got.Team != "alpha" || got.Owner != "ann" || !reflect.DeepEqual(got.Customers, []string{"acme"}) {
		t.Fatalf("the goal = %+v", got)
	}
}
