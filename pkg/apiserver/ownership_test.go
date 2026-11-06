package apiserver

import (
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// docs/design/ownership.md on the wire and in the Roadmap's listing.

func ownershipBoard() board.Board {
	b := board.NewBoard([]board.Card{
		{ItemID: "goal", Title: "Grow", Type: board.TypeGoal, Owner: "gwen", Customers: []string{"hidora"}},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "goal", Team: "portal", Owner: "ann"},
		{ItemID: "t1", Title: "Receipts", Parent: "epic", Parked: true},
		{ItemID: "t2", Title: "Invoices", Parent: "epic", Team: "cozystack", Customers: []string{"acme"}, Parked: true},
		{ItemID: "other", Title: "Docs", Type: board.TypeEpic, Team: "docs"},
		{ItemID: "loose", Title: "Loose", Team: "ops", Parked: true},
	})
	b.Customers = []board.Customer{{ID: "hidora", Name: "Hidora"}, {ID: "acme", Name: "Acme"}}
	return b
}

// O1, O3, O4 on the resource: spec.owner and spec.customers as written,
// status.effectiveTeam and status.effectiveCustomers as computed.
func TestTheResourceCarriesOwnershipAsWrittenAndAsComputed(t *testing.T) {
	b := ownershipBoard()
	res := func(id string) Card {
		c, _ := board.Find(b, id)
		return CardResource(b, c)
	}
	if e := res("epic"); e.Spec.Owner != "ann" || e.Spec.Team != "portal" || !reflect.DeepEqual(e.Status.EffectiveCustomers, []string{"hidora"}) {
		t.Fatalf("the epic = %+v / %+v", e.Spec, e.Status)
	}
	t1 := res("t1")
	if t1.Spec.Team != "" || t1.Status.EffectiveTeam != "portal" || !reflect.DeepEqual(t1.Status.EffectiveCustomers, []string{"hidora"}) {
		t.Fatalf("t1 = team %q effective %q customers %v", t1.Spec.Team, t1.Status.EffectiveTeam, t1.Status.EffectiveCustomers)
	}
	t2 := res("t2")
	if t2.Status.EffectiveTeam != "cozystack" || !reflect.DeepEqual(t2.Status.EffectiveCustomers, []string{"hidora", "acme"}) ||
		!reflect.DeepEqual(t2.Spec.Customers, []string{"acme"}) {
		t.Fatalf("t2 = %+v / %+v", t2.Spec, t2.Status)
	}
	info := BoardResource(b)
	if len(info.Metadata.Customers) != 2 || info.Metadata.Customers[0].Name != "Hidora" {
		t.Fatalf("the board's customer roster = %+v", info.Metadata.Customers)
	}
}

// O5 — an id the roster no longer holds reaches no reader: not in the
// card's own customers, not in its effective ones, and the customer filter
// cannot match it.
func TestADanglingCustomerReachesNoReader(t *testing.T) {
	b := ownershipBoard()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "t2" {
			b.Cards[i].Customers = []string{"gone", "acme"}
		}
	}
	c, _ := board.Find(b, "t2")
	res := CardResource(b, c)
	if !reflect.DeepEqual(res.Spec.Customers, []string{"acme"}) || !reflect.DeepEqual(res.Status.EffectiveCustomers, []string{"hidora", "acme"}) {
		t.Fatalf("t2 = customers %v effective %v, want the dangling id nowhere", res.Spec.Customers, res.Status.EffectiveCustomers)
	}
	if got := roadmapIDs(b, Selector{Customer: "gone"}); len(got) != 0 {
		t.Fatalf("customer=gone = %v, want nothing", got)
	}
}

func roadmapIDs(b board.Board, sel Selector) []string {
	sel.View = "roadmap"
	var out []string
	for _, c := range FilterCards(b, sel) {
		out = append(out, c.ItemID)
	}
	return out
}

// O7 — the team filter matches a task by its effective team, and a planning
// card by itself or any task under it; every match comes with its
// ancestors. An epic of portal holding a cozystack task shows under
// cozystack with that task only.
func TestTheRoadmapTeamFilter(t *testing.T) {
	b := ownershipBoard()
	if got := roadmapIDs(b, Selector{Team: "cozystack"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t2"}) {
		t.Fatalf("team=cozystack = %v", got)
	}
	if got := roadmapIDs(b, Selector{Team: "portal"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t1"}) {
		t.Fatalf("team=portal = %v", got)
	}
	// Several values are OR.
	if got := roadmapIDs(b, Selector{Team: "docs,ops"}); !reflect.DeepEqual(got, []string{"other", "loose"}) {
		t.Fatalf("team=docs,ops = %v", got)
	}
	// And the gate agrees with the listing.
	if Drawn(b, Selector{View: "roadmap", Team: "cozystack"}, "t1") {
		t.Fatal("the gate draws t1 under cozystack")
	}
}

// O7 — the customer filter, over effective customers.
func TestTheRoadmapCustomerFilter(t *testing.T) {
	b := ownershipBoard()
	if got := roadmapIDs(b, Selector{Customer: "acme"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t2"}) {
		t.Fatalf("customer=acme = %v", got)
	}
	if got := roadmapIDs(b, Selector{Customer: "hidora"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t1", "t2"}) {
		t.Fatalf("customer=hidora = %v", got)
	}
}

// O8 — the owner filter matches a planning card by its OWN owner only (never
// inherited, and no task has one); the work under a matching plan item is
// listed with it as far as the other filters let it — here there are none.
// Each filter is judged on its own and they are AND: a planning card passes
// team or customer by itself or any task under it, and owner by its own.
func TestTheRoadmapOwnerFilter(t *testing.T) {
	b := ownershipBoard()
	if got := roadmapIDs(b, Selector{Owner: "ann"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t1", "t2"}) {
		t.Fatalf("owner=ann = %v", got)
	}
	// gwen owns the goal: the goal and the work under it; the epic comes as
	// the ancestor of that work, not because gwen owns it.
	if got := roadmapIDs(b, Selector{Owner: "gwen"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t1", "t2"}) {
		t.Fatalf("owner=gwen = %v", got)
	}
	if got := roadmapIDs(b, Selector{Owner: "nobody"}); len(got) != 0 {
		t.Fatalf("owner=nobody = %v", got)
	}
	if got := roadmapIDs(b, Selector{Owner: "ann", Team: "docs"}); len(got) != 0 {
		t.Fatalf("owner=ann AND team=docs = %v, want nothing", got)
	}
	sel, err := ParseViewSelector("roadmap", map[string][]string{"owner": {"ann"}, "customer": {"acme,hidora"}, "team": {"portal"}})
	if err != nil || sel.Owner != "ann" || sel.Customer != "acme,hidora" || sel.Team != "portal" {
		t.Fatalf("parsed %+v %v", sel, err)
	}
}

// The combination: ann owns the epic, whose own team is portal; only a task
// under it is cozystack's (or Acme's). The epic passes team (by that task)
// and owner (by itself), so it is listed — with that task alone.
func TestTheRoadmapFiltersCombineOwnerWithTheWorkBelow(t *testing.T) {
	b := ownershipBoard()
	if got := roadmapIDs(b, Selector{Owner: "ann", Team: "cozystack"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t2"}) {
		t.Fatalf("owner=ann AND team=cozystack = %v", got)
	}
	if got := roadmapIDs(b, Selector{Owner: "ann", Customer: "acme"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t2"}) {
		t.Fatalf("owner=ann AND customer=acme = %v", got)
	}
	if got := roadmapIDs(b, Selector{Team: "cozystack", Customer: "hidora"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t2"}) {
		t.Fatalf("team=cozystack AND customer=hidora = %v", got)
	}
}

// With no filter the Roadmap is listed as it always was.
func TestTheUnfilteredRoadmapIsUnchanged(t *testing.T) {
	b := ownershipBoard()
	want := []string{"goal", "epic", "t1", "t2", "other", "loose"}
	if got := roadmapIDs(b, Selector{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("the unfiltered roadmap = %v, want %v", got, want)
	}
}

// H3, O1 — a planning card keeps a team now, and that team must not put it
// on a listing the team selector narrows: the Team and Backlog watches
// (view=all&team=…) and MCP list_cards view=all team=… held tasks before,
// and still hold tasks only. Without a team the all view lists every card,
// as before.
func TestATeamListingHoldsNoPlanningCard(t *testing.T) {
	b := ownershipBoard()
	for _, view := range []string{"all", "", "me"} {
		for _, c := range FilterCards(b, Selector{View: view, Team: "portal,docs", User: "ann", Day: board.TodayIso()}) {
			if board.IsPlanning(c) {
				t.Errorf("view=%q team=portal,docs lists the %s %s", view, c.Type, c.ItemID)
			}
		}
	}
	ids := map[string]bool{}
	for _, c := range FilterCards(b, Selector{View: "all"}) {
		ids[c.ItemID] = true
	}
	if !ids["epic"] || !ids["goal"] {
		t.Fatalf("view=all with no team = %v, want the planning cards too", ids)
	}
}
