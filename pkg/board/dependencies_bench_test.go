package board

import (
	"fmt"
	"testing"
)

// benchBoard is a board the size of production: ten goals, fifty
// initiatives, two hundred and forty epics, about three thousand tasks under
// them and some loose, a review now and then, in two repositories. deps
// dependencies are laid between plan items and tasks (0 for none, 1 for the
// one goal→initiative case, more for a dense board).
func benchBoard(deps int) Board {
	var cards []Card
	add := func(c Card) { cards = append(cards, c) }
	for g := range 10 {
		add(Card{ItemID: fmt.Sprintf("g%d", g), Type: TypeGoal, Customers: []string{"acme"}})
	}
	for i := range 50 {
		add(Card{ItemID: fmt.Sprintf("i%d", i), Type: TypeInitiative, Parent: fmt.Sprintf("g%d", i%10)})
	}
	for e := range 240 {
		dom := "shared"
		if e%7 == 0 {
			dom = "closed"
		}
		add(Card{ItemID: fmt.Sprintf("e%d", e), Type: TypeEpic, Parent: fmt.Sprintf("i%d", e%50), Domain: dom})
	}
	for t := range 2800 {
		c := Card{ItemID: fmt.Sprintf("t%d", t), Team: "alpha", Parent: fmt.Sprintf("e%d", t%240), Domain: "shared"}
		if t%3 == 0 {
			c.Progress = 100
		}
		add(c)
		if t%40 == 0 {
			add(Card{ItemID: fmt.Sprintf("r%d", t), Team: "alpha", ReviewOf: c.ItemID, Domain: "shared"})
		}
	}
	for l := range 200 {
		add(Card{ItemID: fmt.Sprintf("l%d", l), Team: "alpha", Domain: "shared"})
	}
	for d := range deps {
		switch d % 3 {
		case 0: // a goal waits for an initiative of another goal
			cards[d%10].DependsOn = append(cards[d%10].DependsOn, fmt.Sprintf("i%d", (d+1)%50))
		case 1: // an epic waits for an epic
			cards[60+d%240].DependsOn = append(cards[60+d%240].DependsOn, fmt.Sprintf("e%d", (d*7+3)%240))
		default: // a task waits for a task
			cards[300+d%2800].DependsOn = append(cards[300+d%2800].DependsOn, fmt.Sprintf("t%d", (d*13+5)%2800))
		}
	}
	b := NewBoardIn("shared", cards)
	b.Customers = []Customer{{ID: "acme", Name: "Acme"}}
	return b
}

// What a listing asks of every card: its blockers and its customers, on one
// index.
func benchListing(b *testing.B, deps int) {
	bd := benchBoard(deps)
	b.ResetTimer()
	for range b.N {
		ix := NewIndex(bd)
		for _, c := range bd.Cards {
			_ = ix.Blockers(c)
			_ = ix.EffectiveCustomers(c)
			_ = ix.DependentCustomers(c)
		}
	}
}

func BenchmarkDependencyListingNone(b *testing.B)   { benchListing(b, 0) }
func BenchmarkDependencyListingOne(b *testing.B)    { benchListing(b, 1) }
func BenchmarkDependencyListingDense(b *testing.B)  { benchListing(b, 300) }
func BenchmarkVisibleWithNoDependency(b *testing.B) { benchVisible(b, 0) }
func BenchmarkVisibleWithOneDependency(b *testing.B) {
	benchVisible(b, 1)
}
func BenchmarkVisibleWithDenseDependencies(b *testing.B) { benchVisible(b, 300) }

// A restricted visitor's board, as every request of theirs builds it.
func benchVisible(b *testing.B, deps int) {
	bd := benchBoard(deps)
	b.ResetTimer()
	for range b.N {
		_ = Visible(bd, "shared", func(d string) bool { return d == "shared" })
	}
}
