package apiserver

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The Strategy board answers to its old name: `roadmap` in a query, in a
// path segment or in a selector an embedder built by hand reads as
// `strategy`, lists the same cards and is named `strategy` back.
func TestTheRoadmapSelectorReadsAsTheStrategyBoard(t *testing.T) {
	t.Parallel()
	sel, err := ParseSelector(url.Values{"view": {"roadmap"}})
	if err != nil || sel.View != "strategy" {
		t.Fatalf("view=roadmap parsed as %q, %v; want strategy", sel.View, err)
	}
	sel, err = ParseViewSelector("roadmap", url.Values{"status": {"backlog"}})
	if err != nil || sel.View != "strategy" || sel.Status != "backlog" {
		t.Fatalf("the roadmap segment parsed as %+v, %v; want strategy", sel, err)
	}
	b := board.NewBoard([]board.Card{
		{ItemID: "g", Title: "Goal", Type: board.TypeGoal},
		{ItemID: "t", Title: "Task", Parent: "g", Team: "alpha", Parked: true},
		{ItemID: "x", Title: "Elsewhere", Team: "alpha", Progress: 100},
	})
	ids := func(view string) []string {
		var out []string
		for _, c := range FilterCards(b, Selector{View: view}) {
			out = append(out, c.ItemID)
		}
		return out
	}
	if got, want := ids("roadmap"), ids("strategy"); !reflect.DeepEqual(got, want) || len(got) != 2 {
		t.Fatalf("roadmap lists %v, strategy %v; want the same goal and task", got, want)
	}
	if (Selector{View: "roadmap", Team: "alpha"}).Mirrored() || (Selector{View: "strategy", Team: "alpha"}).Mirrored() {
		t.Fatal("the alias and the board disagree on who works out membership")
	}
}
