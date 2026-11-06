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
		{ItemID: "t1", Title: "Receipts", Parent: "epic", Assignees: []string{"bob"}},
		{ItemID: "t2", Title: "Invoices", Parent: "epic", Team: "cozystack", Customers: []string{"acme"}, Parked: true},
		{ItemID: "other", Title: "Docs", Type: board.TypeEpic, Team: "docs"},
		{ItemID: "loose", Title: "Loose", Team: "ops", Assignees: []string{"ann"}},
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

// O8 — the Owner filter is the Roadmap's one person filter: a person in it
// matches a plan item whose OWN owner they are (never inherited) and a task
// or subtask they are assigned to, each drawn with its ancestors. Several
// people are OR.
func TestTheRoadmapOwnerFilter(t *testing.T) {
	b := ownershipBoard()
	// ann owns the epic and is on the loose task; the tasks under her epic
	// are not hers, so they are not drawn.
	if got := roadmapIDs(b, Selector{Owner: "ann"}); !reflect.DeepEqual(got, []string{"goal", "epic", "loose"}) {
		t.Fatalf("owner=ann = %v", got)
	}
	// gwen owns the goal alone: the epic below is not hers by inheritance.
	if got := roadmapIDs(b, Selector{Owner: "gwen"}); !reflect.DeepEqual(got, []string{"goal"}) {
		t.Fatalf("owner=gwen = %v", got)
	}
	// bob is on t1: the task, with the epic and the goal above it.
	if got := roadmapIDs(b, Selector{Owner: "bob"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t1"}) {
		t.Fatalf("owner=bob = %v", got)
	}
	if got := roadmapIDs(b, Selector{Owner: "ann,bob"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t1", "loose"}) {
		t.Fatalf("owner=ann,bob = %v", got)
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

// The combination, each filter judged on its own and AND across them. ann
// owns the epic, whose own team is portal; only t2 under it is cozystack's
// (and Acme's). The epic passes team (by t2) and owner (by itself), so it is
// listed; t2 is not ann's, so it is not. bob's t1 is portal's: under
// owner=bob AND team=portal it is drawn with its chain.
func TestTheRoadmapFiltersCombineOwnerWithTheWorkBelow(t *testing.T) {
	b := ownershipBoard()
	if got := roadmapIDs(b, Selector{Owner: "ann", Team: "cozystack"}); !reflect.DeepEqual(got, []string{"goal", "epic"}) {
		t.Fatalf("owner=ann AND team=cozystack = %v", got)
	}
	if got := roadmapIDs(b, Selector{Owner: "ann", Customer: "acme"}); !reflect.DeepEqual(got, []string{"goal", "epic"}) {
		t.Fatalf("owner=ann AND customer=acme = %v", got)
	}
	if got := roadmapIDs(b, Selector{Owner: "bob", Team: "portal"}); !reflect.DeepEqual(got, []string{"goal", "epic", "t1"}) {
		t.Fatalf("owner=bob AND team=portal = %v", got)
	}
	if got := roadmapIDs(b, Selector{Owner: "bob", Customer: "acme"}); !reflect.DeepEqual(got, []string{"goal", "epic"}) {
		t.Fatalf("owner=bob AND customer=acme = %v", got)
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

// O7 — the no-team group is a team like any other in the Roadmap's Team
// filter: the empty member of the set, written alone as "," (a set holding
// only the empty key, as on every other board). Choosing real teams leaves
// the no-team work out; choosing No team keeps what has no effective team.
func TestTheRoadmapTeamFilterHasNoTeamAsATeam(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "t1", Title: "Receipts", Parent: "epic", Parked: true},
		{ItemID: "plan", Title: "Docs", Type: board.TypeEpic},
		{ItemID: "t2", Title: "Guides", Parent: "plan", Parked: true},
		{ItemID: "loose", Title: "Loose", Parked: true},
	})
	if got := roadmapIDs(b, Selector{Team: "portal"}); !reflect.DeepEqual(got, []string{"epic", "t1"}) {
		t.Fatalf("team=portal = %v, want no no-team work", got)
	}
	if got := roadmapIDs(b, Selector{Team: ","}); !reflect.DeepEqual(got, []string{"plan", "t2", "loose"}) {
		t.Fatalf("team=, (No team) = %v", got)
	}
	if got := roadmapIDs(b, Selector{Team: "portal,"}); !reflect.DeepEqual(got, []string{"epic", "t1", "plan", "t2", "loose"}) {
		t.Fatalf("team=portal, = %v", got)
	}
	if got := roadmapIDs(b, Selector{Team: ""}); len(got) != 5 {
		t.Fatalf("no team filter = %v, want everything", got)
	}
}

// O11 — the day boards' Customer filter: the Team board's and the Backlog's
// listing and watch (view=team, backlog, all) and the Me list keep a task
// whose EFFECTIVE customers (its own plus every ancestor's, O4) include one
// of those chosen. Several are OR; with the team set, AND.
func TestTheDayBoardsFilterByEffectiveCustomer(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Customers: []string{"cu1"}},
		{ItemID: "t1", Title: "Receipts", Parent: "epic", Team: "alpha", Parked: true},
		{ItemID: "t2", Title: "Invoices", Team: "alpha", Customers: []string{"cu2"}, Parked: true},
		{ItemID: "t3", Title: "Docs", Team: "alpha", Parked: true},
		{ItemID: "t4", Title: "Ops", Team: "beta", Customers: []string{"cu1"}, Parked: true},
	})
	b.Customers = []board.Customer{{ID: "cu1", Name: "Hidora"}, {ID: "cu2", Name: "Acme"}}
	ids := func(sel Selector) []string {
		var out []string
		for _, c := range FilterCards(b, sel) {
			out = append(out, c.ItemID)
		}
		return out
	}
	if got := ids(Selector{View: "backlog", Team: "alpha", Customer: "cu1"}); !reflect.DeepEqual(got, []string{"t1"}) {
		t.Fatalf("backlog alpha for Hidora = %v", got)
	}
	if got := ids(Selector{View: "backlog", Team: "alpha,beta", Customer: "cu1,cu2"}); !reflect.DeepEqual(got, []string{"t1", "t2", "t4"}) {
		t.Fatalf("backlog alpha,beta for Hidora or Acme = %v", got)
	}
	if got := ids(Selector{View: "all", Team: "alpha", Customer: "cu2"}); !reflect.DeepEqual(got, []string{"t2"}) {
		t.Fatalf("all alpha for Acme = %v", got)
	}
	if got := ids(Selector{View: "backlog", Team: "alpha"}); !reflect.DeepEqual(got, []string{"t1", "t2", "t3"}) {
		t.Fatalf("backlog alpha with no customer filter = %v", got)
	}
}

// And the no-team group alone is the set ",": on the Backlog it is the
// no-team group's backlog, not every team's.
func TestABacklogOfTheNoTeamGroupAloneIsItsOwn(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "a", Title: "A", Team: "alpha", Parked: true},
		{ItemID: "n", Title: "N", Parked: true},
	})
	var got []string
	for _, c := range FilterCards(b, Selector{View: "backlog", Team: ","}) {
		got = append(got, c.ItemID)
	}
	if !reflect.DeepEqual(got, []string{"n"}) {
		t.Fatalf("backlog team=, = %v, want the no-team group's alone", got)
	}
}
