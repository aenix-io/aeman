package board

import (
	"reflect"
	"testing"
)

// docs/design/ownership.md: who answers for a plan item (its OWNER, one
// person, planning cards only), which team carries it out (its TEAM), and
// which customers it is for (CUSTOMERS, ids from the board's roster).

// O1 — a planning card may carry one owner and one team, and the reading
// keeps both; a person on it as an ASSIGNEE is still not read (H3: nobody's
// work is on a planning card). A project card carries neither an owner nor
// customers (H4), and a task carries no owner — its people are assignees.
func TestAPlanningCardKeepsItsOwnerAndTeam(t *testing.T) {
	epic := SettleCard(Card{ItemID: "e", Type: TypeEpic, Team: "portal", Owner: "ann",
		Assignees: []string{"bob"}, Customers: []string{"c1"}})
	if epic.Team != "portal" || epic.Owner != "ann" || len(epic.Assignees) != 0 ||
		!reflect.DeepEqual(epic.Customers, []string{"c1"}) {
		t.Fatalf("the epic read = %+v, want team portal, owner ann, customer c1, nobody assigned", epic)
	}
	slot := SettleCard(Card{ItemID: "s", Scope: "S", Project: "P", Owner: "ann", Customers: []string{"c1"}})
	if slot.Owner != "" || len(slot.Customers) != 0 {
		t.Fatalf("a project card read with %q / %v, want neither an owner nor customers", slot.Owner, slot.Customers)
	}
	task := SettleCard(Card{ItemID: "t", Team: "portal", Owner: "ann", Customers: []string{"c1"}})
	if task.Owner != "" || !reflect.DeepEqual(task.Customers, []string{"c1"}) {
		t.Fatalf("a task read = %+v, want no owner and its customer", task)
	}
}

func ownershipBoard() *Index {
	b := NewBoard([]Card{
		{ItemID: "g", Type: TypeGoal, Team: "portal", Owner: "gwen", Customers: []string{"hidora"}},
		{ItemID: "i", Type: TypeInitiative, Parent: "g"},
		{ItemID: "e", Type: TypeEpic, Parent: "i", Customers: []string{"acme"}},
		{ItemID: "t", Parent: "e", Customers: []string{"acme", "zeta"}},
		{ItemID: "own", Parent: "e", Team: "cozystack"},
		{ItemID: "sub", Parent: "own"},
		{ItemID: "r", ReviewOf: "t", Team: "portal", Parked: true},
		{ItemID: "loose", Team: "ops"},
	})
	b.Customers = []Customer{{ID: "hidora", Name: "Hidora"}, {ID: "acme", Name: "Acme"}, {ID: "zeta", Name: "Zeta"}}
	return NewIndex(b)
}

// O3 — the effective team is the card's own, else the nearest ancestor's
// that has one; computed, never written. A task's own team always wins over
// an inherited one, and a subtask rides its task's.
func TestTheEffectiveTeamIsTheNearestOne(t *testing.T) {
	ix := ownershipBoard()
	for id, want := range map[string]string{
		"g": "portal", "i": "portal", "e": "portal", "t": "portal",
		"own": "cozystack", "sub": "cozystack", "loose": "ops", "r": "portal",
	} {
		c, _ := ix.Find(id)
		if got := ix.EffectiveTeam(c); got != want {
			t.Errorf("%s effective team = %q, want %q", id, got, want)
		}
	}
}

// O4 — the effective customers are the card's own plus every ancestor's:
// a lower card adds to them, never overwrites; each named once, the
// ancestors' first. O6 — a review card shows its task's.
func TestTheEffectiveCustomersAreTheUnion(t *testing.T) {
	ix := ownershipBoard()
	for id, want := range map[string][]string{
		"g":     {"hidora"},
		"i":     {"hidora"},
		"e":     {"hidora", "acme"},
		"t":     {"hidora", "acme", "zeta"},
		"own":   {"hidora", "acme"},
		"sub":   {"hidora", "acme"},
		"r":     {"hidora", "acme", "zeta"},
		"loose": nil,
	} {
		c, _ := ix.Find(id)
		if got := ix.EffectiveCustomers(c); !reflect.DeepEqual(got, want) {
			t.Errorf("%s effective customers = %v, want %v", id, got, want)
		}
	}
}

// O2 — an owner is never inherited: the initiative under gwen's goal has
// none of its own, and nothing on a day board comes from it.
func TestAnOwnerIsNotInherited(t *testing.T) {
	ix := ownershipBoard()
	i, _ := ix.Find("i")
	if i.Owner != "" {
		t.Fatalf("the initiative has owner %q, want none", i.Owner)
	}
	b := NewBoard(ix.cards)
	for _, c := range MeView(b, "gwen", TodayIso()) {
		t.Fatalf("gwen's Me board holds %s: an owner puts nothing on a day board", c.ItemID)
	}
}

// O5 — a customer id the roster no longer holds is nobody's: a delete that
// raced an edit, or a replayed write the other side's delete came before,
// can leave a card naming one, and it must not lock the card nor reach a
// reader. The effective customers skip it, at every level.
func TestADanglingCustomerIsNoOnesEffective(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "e", Type: TypeEpic, Customers: []string{"gone", "acme"}},
		{ItemID: "t", Parent: "e", Customers: []string{"gone"}},
		{ItemID: "r", ReviewOf: "t", Team: "portal", Parked: true},
	})
	b.Customers = []Customer{{ID: "acme", Name: "Acme"}}
	ix := NewIndex(b)
	for _, id := range []string{"e", "t", "r"} {
		c, _ := ix.Find(id)
		if got := ix.EffectiveCustomers(c); !reflect.DeepEqual(got, []string{"acme"}) {
			t.Errorf("%s effective customers = %v, want only Acme", id, got)
		}
	}
	// And a board with no roster at all names nobody.
	b.Customers = nil
	ix = NewIndex(b)
	if c, _ := ix.Find("t"); ix.EffectiveCustomers(c) != nil {
		t.Errorf("with no roster the task's customers = %v, want none", ix.EffectiveCustomers(c))
	}
}

// H4, O3 — a project card has no team, and so no effective one: a stray
// `team:` an old file still carries is not read as the team it counts
// under, the way its customers are none (O6).
func TestAProjectCardHasNoEffectiveTeam(t *testing.T) {
	ix := NewIndex(NewBoard([]Card{{ItemID: "slot", Scope: "Security", Project: "core", Team: "ops"}}))
	c, _ := ix.Find("slot")
	if got := ix.EffectiveTeam(c); got != "" {
		t.Fatalf("a project card's effective team = %q, want none", got)
	}
}
