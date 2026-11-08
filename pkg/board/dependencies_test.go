package board

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"
)

// N1–N6 — a card waits for other cards (docs/design/dependencies.md).

func depBoard() Board {
	b := NewBoard([]Card{
		{ItemID: "goal", Type: TypeGoal, Customers: []string{"acme"}},
		{ItemID: "epic", Type: TypeEpic, Parent: "goal", DependsOn: []string{"infra"}},
		{ItemID: "task", Parent: "epic", Team: "a", Parked: true},
		{ItemID: "sub", Parent: "task", Team: "a"},
		{ItemID: "infra", Type: TypeEpic},
		{ItemID: "infra-t1", Parent: "infra", Team: "a", Progress: 100},
		{ItemID: "infra-t2", Parent: "infra", Team: "a", Parked: true},
		{ItemID: "db", Team: "a", Parked: true, Customers: []string{"zeta"}},
		{ItemID: "api", Team: "a", Parked: true, DependsOn: []string{"db", "gone"}},
		{ItemID: "done", Team: "a", Progress: 100},
		{ItemID: "waits-done", Team: "a", Parked: true, DependsOn: []string{"done"}},
		{ItemID: "empty", Type: TypeEpic},
		{ItemID: "waits-empty", Team: "a", Parked: true, DependsOn: []string{"empty"}},
		{ItemID: "review", ReviewOf: "task", Team: "a"},
	})
	b.Customers = []Customer{{ID: "acme"}, {ID: "zeta"}, {ID: "beta"}}
	return b
}

func blockerIDs(bs []Blocker) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Target.ItemID+"@"+b.Via.ItemID)
	}
	return out
}

// N3 — satisfied when the target is done; a plan item is done when its
// roll-up is; one with no tasks is not. The wait is inherited by every card
// under the card that names it, with the card that names it as `via`.
func TestACardIsLockedWhileWhatItWaitsForIsNotDone(t *testing.T) {
	ix := NewIndex(depBoard())
	for _, tc := range []struct {
		id   string
		want []string
	}{
		{"waits-done", nil},                            // its target is complete
		{"api", []string{"db@api"}},                    // db is open; "gone" is dangling (N6)
		{"waits-empty", []string{"empty@waits-empty"}}, // an epic with no tasks is not done
		{"epic", []string{"infra@epic"}},               // infra has an open task
		{"task", []string{"infra@epic"}},               // inherited from the epic
		{"sub", []string{"infra@epic"}},                // two levels down
		{"goal", nil},                                  // nothing above the epic waits
		{"db", nil},
	} {
		c, _ := ix.Find(tc.id)
		if got := blockerIDs(ix.Blockers(c)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s blockers = %v, want %v", tc.id, got, tc.want)
		}
		if got := ix.Locked(c); got != (len(tc.want) > 0) {
			t.Errorf("%s locked = %v", tc.id, got)
		}
	}
	// The last open task of the target finishing unlocks the whole subtree.
	b := depBoard()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "infra-t2" {
			b.Cards[i].Progress = 100
		}
	}
	ix = NewIndex(b)
	for _, id := range []string{"epic", "task", "sub"} {
		c, _ := ix.Find(id)
		if ix.Locked(c) {
			t.Errorf("%s still locked after its target was done", id)
		}
	}
}

// N3 — the lock is not a work status: placement is read as before.
func TestALockedTaskKeepsItsWorkStatus(t *testing.T) {
	ix := NewIndex(depBoard())
	c, _ := ix.Find("task")
	if !ix.Locked(c) || WorkStatus(c) != WorkStatusBacklog {
		t.Fatalf("locked %v, work status %q: a locked parked task is still backlog", ix.Locked(c), WorkStatus(c))
	}
}

// N4 — blockers the reader cannot read count but are not named: a hidden
// target, or a wait inherited through a hidden ancestor.
func TestABlockerTheReaderCannotReadIsCountedNotNamed(t *testing.T) {
	ix := NewIndex(depBoard())
	task, _ := ix.Find("task")
	shown, hidden := ix.BlockersAs(task, func(c Card) bool { return c.ItemID != "infra" })
	if len(shown) != 0 || hidden != 1 {
		t.Fatalf("hidden target: shown %v hidden %d", blockerIDs(shown), hidden)
	}
	shown, hidden = ix.BlockersAs(task, func(c Card) bool { return c.ItemID != "epic" })
	if len(shown) != 0 || hidden != 1 {
		t.Fatalf("hidden via: shown %v hidden %d", blockerIDs(shown), hidden)
	}
	shown, hidden = ix.BlockersAs(task, func(Card) bool { return true })
	if len(shown) != 1 || hidden != 0 {
		t.Fatalf("all readable: shown %v hidden %d", blockerIDs(shown), hidden)
	}
	// The projection records the whole board's count for what it keeps.
	b := depBoard()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "infra" || b.Cards[i].ItemID == "infra-t1" || b.Cards[i].ItemID == "infra-t2" {
			b.Cards[i].Domain = "closed"
		}
	}
	v := Visible(b, "", func(d string) bool { return d != "closed" })
	if v.BlockerTotals["task"] != 1 || v.BlockerTotals["epic"] != 1 || v.BlockerTotals["db"] != 0 {
		t.Fatalf("blocker totals = %v", v.BlockerTotals)
	}
	if all := Visible(b, "", func(string) bool { return true }); all.BlockerTotals != nil {
		t.Fatal("a projection that hides nothing records nothing")
	}
}

// N4 — the cards that wait for this one, directly.
func TestACardKnowsWhatWaitsForIt(t *testing.T) {
	ix := NewIndex(depBoard())
	db, _ := ix.Find("db")
	var got []string
	for _, c := range ix.Blocks(db) {
		got = append(got, c.ItemID)
	}
	if !reflect.DeepEqual(got, []string{"api"}) {
		t.Fatalf("db blocks %v", got)
	}
}

// N5 — a target is for the customers of the work that waits for it,
// transitively, each card once; they flow down the tree; the ones that
// arrive only that way name the nearest waiting card.
func TestATargetIsForTheCustomersOfTheWorkThatWaitsForIt(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "a", Team: "x", Customers: []string{"acme"}, DependsOn: []string{"b"}},
		{ItemID: "b", Team: "x", DependsOn: []string{"c"}},
		{ItemID: "c", Team: "x", Customers: []string{"beta"}},
		{ItemID: "plan", Type: TypeEpic},
		{ItemID: "plan-t", Parent: "plan", Team: "x"},
		{ItemID: "w", Team: "x", Customers: []string{"zeta"}, DependsOn: []string{"plan"}},
		// A cycle a direct writer left: each passes the other's, and it ends.
		{ItemID: "p", Team: "x", Customers: []string{"acme"}, DependsOn: []string{"q"}},
		{ItemID: "q", Team: "x", Customers: []string{"beta"}, DependsOn: []string{"p"}},
	})
	b.Customers = []Customer{{ID: "acme"}, {ID: "zeta"}, {ID: "beta"}}
	ix := NewIndex(b)
	for id, want := range map[string][]string{
		"a":      {"acme"},
		"b":      {"acme"},
		"c":      {"beta", "acme"},
		"plan":   {"zeta"},
		"plan-t": {"zeta"},
		"p":      {"acme", "beta"},
		"q":      {"beta", "acme"},
	} {
		c, _ := ix.Find(id)
		if got := ix.EffectiveCustomers(c); !reflect.DeepEqual(got, want) {
			t.Errorf("%s customers = %v, want %v", id, got, want)
		}
	}
	c, _ := ix.Find("c")
	from := ix.DependentCustomers(c)
	if len(from) != 1 || from[0].Customer != "acme" || from[0].From.ItemID != "b" {
		t.Fatalf("c's dependent customers = %+v, want acme from b", from)
	}
	pt, _ := ix.Find("plan-t")
	if from := ix.DependentCustomers(pt); len(from) != 1 || from[0].From.ItemID != "w" {
		t.Fatalf("plan-t's dependent customers = %+v, want zeta from w", from)
	}
}

// N1 — what may be a target.
func TestADependencyTargetIsAnyCardButItselfAReviewOrItsOwnLine(t *testing.T) {
	ix := NewIndex(depBoard())
	for _, tc := range []struct {
		card, target string
		ok           bool
	}{
		{"api", "done", true},
		{"task", "db", true},
		{"epic", "empty", true},
		{"api", "api", false},     // itself
		{"api", "review", false},  // a review card
		{"review", "db", false},   // a review card waits for nothing
		{"task", "goal", false},   // an ancestor
		{"goal", "sub", false},    // a descendant
		{"api", "nowhere", false}, // not on the board
	} {
		c, _ := ix.Find(tc.card)
		if c.ItemID == "" {
			c = Card{ItemID: tc.card}
		}
		if got := ix.DependencyTargetOK(c, tc.target); got != tc.ok {
			t.Errorf("%s → %s ok = %v, want %v", tc.card, tc.target, got, tc.ok)
		}
	}
}

// N2 — a dependency that would close a cycle: direct, transitive, through an
// inherited wait and through a plan item's roll-up.
func TestADependencyThatClosesACycleIsFound(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "a", Team: "x", DependsOn: []string{"b"}},
		{ItemID: "b", Team: "x", DependsOn: []string{"c"}},
		{ItemID: "c", Team: "x"},
		{ItemID: "e", Type: TypeEpic},
		{ItemID: "e-t", Parent: "e", Team: "x", DependsOn: []string{"z"}},
		{ItemID: "z", Team: "x"},
		{ItemID: "plan", Type: TypeEpic, DependsOn: []string{"y"}},
		{ItemID: "plan-t", Parent: "plan", Team: "x"},
		{ItemID: "y", Team: "x"},
	})
	ix := NewIndex(b)
	for _, tc := range []struct {
		card, target string
		cycle        bool
	}{
		{"b", "a", true},      // a→b, b→a
		{"c", "a", true},      // a→b→c, c→a
		{"z", "e", true},      // e's task waits for z: z→e closes it through the roll-up
		{"y", "plan-t", true}, // plan-t inherits plan→y: y→plan-t closes it
		{"c", "z", false},
		{"a", "c", false}, // a already reaches c; adding it again is no cycle
	} {
		c, _ := ix.Find(tc.card)
		if got := ix.WouldCycle(c, tc.target); got != tc.cycle {
			t.Errorf("%s → %s cycle = %v, want %v", tc.card, tc.target, got, tc.cycle)
		}
	}
}

// N5 — what a reader works out: a card they cannot read passes nothing on.
func TestADependentTheReaderCannotReadPassesNoCustomer(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "a", Team: "x", Customers: []string{"acme"}, DependsOn: []string{"c"}},
		{ItemID: "h", Team: "x", Customers: []string{"zeta"}, DependsOn: []string{"c"}},
		{ItemID: "c", Team: "x"},
	})
	b.Customers = []Customer{{ID: "acme"}, {ID: "zeta"}}
	ix := NewIndex(b)
	c, _ := ix.Find("c")
	got, from := ix.EffectiveCustomersAs(c, func(x Card) bool { return x.ItemID != "h" })
	if !reflect.DeepEqual(got, []string{"acme"}) || len(from) != 1 || from[0].From.ItemID != "a" {
		t.Fatalf("as the reader: %v %+v, want acme from a alone", got, from)
	}
	if all := ix.EffectiveCustomers(c); !reflect.DeepEqual(all, []string{"acme", "zeta"}) {
		t.Fatalf("on the whole board: %v", all)
	}
}

// N2 — a cycle closed by inheritance alone: T already waits for X, and X
// sits under epic E. E waiting for T would hand T's wait down to X, which T
// waits for — both locked for good. Reaching any card under c is reaching c.
func TestACycleThroughACardUnderTheWaitingPlanItemIsFound(t *testing.T) {
	ix := NewIndex(NewBoard([]Card{
		{ItemID: "E", Type: TypeEpic},
		{ItemID: "X", Parent: "E", Team: "x"},
		{ItemID: "T", Team: "x", DependsOn: []string{"X"}},
		{ItemID: "free", Team: "x"},
	}))
	e, _ := ix.Find("E")
	if !ix.WouldCycle(e, "T") {
		t.Fatal("E → T is accepted, and X under E would wait for T, which waits for X")
	}
	if ix.WouldCycle(e, "free") {
		t.Fatal("a card that reaches nothing under E closes no cycle")
	}
}

// N4 — Visible records each kept card's blocker count on the WHOLE board,
// and a board on which nothing waits records none (and pays nothing).
func TestVisibleRecordsWholeBoardBlockerCountsOnlyWhenSomethingWaits(t *testing.T) {
	b := NewBoardIn("shared", []Card{
		{ItemID: "E", Title: "Epic", Type: TypeEpic, Domain: "shared"},
		{ItemID: "done", Parent: "E", Team: "x", Progress: 100, Domain: "shared"},
		{ItemID: "open", Parent: "E", Team: "x", Domain: "closed"},
		{ItemID: "W", Team: "x", DependsOn: []string{"E", "S"}, Domain: "shared"},
		{ItemID: "S", Team: "x", Domain: "closed"},
	})
	can := func(d string) bool { return d == "shared" }
	if got := Visible(b, "shared", can).BlockerTotals["W"]; got != 2 {
		t.Fatalf("W's whole-board blockers = %d, want 2 (the epic still open on the whole board, and S)", got)
	}
	quiet := NewBoardIn("shared", []Card{{ItemID: "a", Domain: "closed"}, {ItemID: "b", Domain: "shared"}})
	if Visible(quiet, "shared", can).BlockerTotals != nil {
		t.Fatal("a board with no dependency records blocker counts")
	}
}

// The customer walk keeps each card's answer, so a dense board works each
// one out once — but a cycle a direct writer left must not leave a partial
// answer behind: whichever card is asked first, every card in the cycle
// answers whole.
func TestACustomerCycleAnswersTheSameWhicheverCardIsAskedFirst(t *testing.T) {
	cards := []Card{
		{ItemID: "a", Title: "A", Team: "t", Customers: []string{"ca"}, DependsOn: []string{"b"}},
		{ItemID: "b", Title: "B", Team: "t", Customers: []string{"cb"}, DependsOn: []string{"c"}},
		{ItemID: "c", Title: "C", Team: "t", Customers: []string{"cc"}, DependsOn: []string{"a"}},
	}
	mk := func() *Index {
		b := NewBoard(cards)
		b.Customers = []Customer{{ID: "ca"}, {ID: "cb"}, {ID: "cc"}}
		return NewIndex(b)
	}
	want := map[string]int{}
	ix := mk()
	for _, id := range []string{"a", "b", "c"} {
		c, _ := ix.Find(id)
		want[id] = len(ix.EffectiveCustomers(c))
	}
	for _, order := range [][]string{{"c", "b", "a"}, {"b", "a", "c"}} {
		ix := mk()
		for _, id := range order {
			c, _ := ix.Find(id)
			if got := len(ix.EffectiveCustomers(c)); got != want[id] || got != 3 {
				t.Fatalf("asked %v: %s has %d customers, want 3 (and %d asked first)", order, id, got, want[id])
			}
		}
	}
}

// A CLIQUE a direct writer left — every card waiting on every other — is
// worked out in linear time, not by trying every order of the cycle: the
// watch counts it under the board's lock, and a dozen cards once took
// seconds. Each answer is the whole clique's customers, the same through the
// listing's door and the watch's.
func TestACustomerCliqueIsWorkedOutFast(t *testing.T) {
	const k = 12
	var cards []Card
	var roster []Customer
	for i := range k {
		id := fmt.Sprintf("c%02d", i)
		var deps []string
		for j := range k {
			if j != i {
				deps = append(deps, fmt.Sprintf("c%02d", j))
			}
		}
		cards = append(cards, Card{ItemID: id, Title: id, Team: "t", Customers: []string{"x" + id}, DependsOn: deps})
		roster = append(roster, Customer{ID: "x" + id})
	}
	b := NewBoard(cards)
	b.Customers = roster
	ix := NewIndex(b)
	start := time.Now()
	for _, c := range b.Cards {
		got := ix.EffectiveCustomers(c)
		if len(got) != k {
			t.Fatalf("%s has %d customers, want the whole clique's %d", c.ItemID, len(got), k)
		}
		as, _ := ix.EffectiveCustomersAs(c, func(Card) bool { return true })
		if !slices.Equal(sortedCopy(got), sortedCopy(as)) {
			t.Fatalf("%s: listing %v, watch %v", c.ItemID, got, as)
		}
	}
	if took := time.Since(start); took > 50*time.Millisecond {
		t.Fatalf("a %d-card clique took %v", k, took)
	}
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
