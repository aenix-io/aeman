package apiserver

import (
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// O13 on the listings: a task with no team of its own stands under its
// plan item's team — in that team's backlog, under that team in the
// view=all/me team set (the Team and Backlog watches), in its grid — and
// leaves the no-team group. Its own team still wins; with no team above it,
// it is the no-team group's. The resource still carries its OWN team: what
// a client edits.
func TestATaskIsListedUnderItsEffectiveTeam(t *testing.T) {
	today := board.TodayIso()
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Parked: true},
		{ItemID: "own", Title: "Own team", Parent: "epic", Team: "ops", Parked: true},
		{ItemID: "plan", Title: "Docs", Type: board.TypeEpic},
		{ItemID: "loose", Title: "Teamless", Parent: "plan", Parked: true},
		{ItemID: "w", Title: "In work", Parent: "epic", Assignees: []string{"ann"}, StartDate: today, Day: today, SprintStart: today},
	})
	b.SprintStates = map[string]board.SprintState{"portal": {Current: today}}
	ids := func(sel Selector) []string {
		var out []string
		for _, c := range FilterCards(b, sel) {
			out = append(out, c.ItemID)
		}
		return out
	}
	if got := ids(Selector{View: "backlog", Team: "portal"}); !reflect.DeepEqual(got, []string{"t"}) {
		t.Fatalf("portal's backlog = %v, want the inherited task", got)
	}
	if got := ids(Selector{View: "backlog", Team: ","}); !reflect.DeepEqual(got, []string{"loose"}) {
		t.Fatalf("the no-team backlog = %v, want only the task with no team above it", got)
	}
	if got := ids(Selector{View: "backlog", Team: "ops"}); !reflect.DeepEqual(got, []string{"own"}) {
		t.Fatalf("ops's backlog = %v, want the task whose own team wins", got)
	}
	if got := ids(Selector{View: "all", Team: "portal"}); !reflect.DeepEqual(got, []string{"t", "w"}) {
		t.Fatalf("view=all team=portal = %v", got)
	}
	if got := ids(Selector{View: "team", Team: "portal", Day: today}); !reflect.DeepEqual(got, []string{"w"}) {
		t.Fatalf("portal's grid = %v", got)
	}
	if !Drawn(b, Selector{View: "backlog", Team: "portal"}, "t") || Drawn(b, Selector{View: "backlog", Team: ","}, "t") {
		t.Fatal("the gesture gate does not agree with the listing")
	}
	c, _ := board.Find(b, "t")
	if res := CardResource(b, c); res.Spec.Team != "" || res.Status.EffectiveTeam != "portal" {
		t.Fatalf("t's resource = team %q effective %q, want its own (none) and portal", res.Spec.Team, res.Status.EffectiveTeam)
	}
}

// The team comes from what the reader may read: the listing is built on
// their visible board, where a task's chain stops at an ancestor they cannot
// read, so that ancestor's team places nothing for them. (A planning card
// lives in the primary, which a reader always reads; an ancestor they cannot
// read is a card in another repository, and a card whose parent the reader
// does not hold reads as a subtask of it — on no list of its own, as before.
// Either way it is never listed under the hidden ancestor's team.)
func TestAnUnreadableAncestorPlacesNothing(t *testing.T) {
	b := board.NewBoardIn("shared", []board.Card{
		{ItemID: "epic", Title: "Secret", Type: board.TypeEpic, Team: "portal", Domain: "closed"},
		{ItemID: "t", Title: "Open task", Parent: "epic", Parked: true},
	})
	visible := board.Visible(b, "shared", func(d string) bool { return d == "shared" })
	if c, _ := board.Find(visible, "t"); board.PlacedTeam(visible, c) != "" {
		t.Fatalf("the reader places the task under %q, a team only the hidden epic names", board.PlacedTeam(visible, c))
	}
	if len(FilterCards(visible, Selector{View: "backlog", Team: "portal"})) != 0 {
		t.Fatal("portal's backlog for the reader shows a task through an epic they cannot read")
	}
}
