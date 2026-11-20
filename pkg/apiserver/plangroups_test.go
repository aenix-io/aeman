package apiserver

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

type planGroupsFixture struct {
	Cards []struct {
		UID       string       `json:"uid"`
		Zone      string       `json:"zone"`
		Assignees []string     `json:"assignees"`
		ReviewOf  string       `json:"reviewOf"`
		Subtask   bool         `json:"subtask"`
		Lineage   []LineageRef `json:"lineage"`
	} `json:"cards"`
	Me      []planGroupCase `json:"me"`
	Team    []planGroupCase `json:"team"`
	Backlog []planGroupCase `json:"backlog"`
}

type planGroupCase struct {
	Assignee *string  `json:"assignee,omitempty"`
	Zone     string   `json:"zone,omitempty"`
	Plan     string   `json:"plan"`
	Cards    []string `json:"cards"`
}

// THE PLANS READING IS ONE RULE ON BOTH SIDES (ADR 0002, S18, S19). What the
// Team, Me and Backlog boards draw in Plans, a listing asked with
// group=plans carries as its groups: per zone on Me, per person and zone on
// Team, runs of neighbouring cards on the Backlog. The fixture is shared
// with web/src/grouping.test.ts.
func TestPlanGroupsMatchTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../board/testdata/plan_groups.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx planGroupsFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	list := CardList{Kind: "CardList"}
	for _, c := range fx.Cards {
		var card Card
		card.Metadata.UID = c.UID
		card.Spec.Zone = c.Zone
		card.Spec.Assignees = c.Assignees
		card.Spec.ReviewOf = c.ReviewOf
		card.Status.Subtask = c.Subtask
		card.Status.Lineage = c.Lineage
		list.Items = append(list.Items, card)
	}
	for _, view := range []string{"me", "team", "backlog"} {
		want := map[string][]planGroupCase{"me": fx.Me, "team": fx.Team, "backlog": fx.Backlog}[view]
		got := []planGroupCase{}
		for _, g := range PlanGroups(list.Items, view) {
			c := planGroupCase{Assignee: g.Assignee, Zone: g.Zone, Cards: g.Cards}
			if g.Plan != nil {
				c.Plan = g.Plan.UID
			}
			got = append(got, c)
		}
		if !reflect.DeepEqual(got, want) {
			gj, _ := json.Marshal(got)
			wj, _ := json.Marshal(want)
			t.Errorf("%s groups =\n%s\nwant\n%s", view, gj, wj)
		}
	}
}

// Asked of a view that draws no groups (all, strategy), group=plans adds
// nothing: the Strategy board draws the tree itself.
func TestPlanGroupsOnlyOnTheBoardsThatDrawThem(t *testing.T) {
	items := []Card{{Metadata: CardMetadata{UID: "a"}}}
	for _, view := range []string{"all", "strategy", ""} {
		if g := PlanGroups(items, view); g != nil {
			t.Errorf("%q: groups = %+v, want none", view, g)
		}
	}
}

// under=<uid> keeps the cards standing under a plan item — or under a task,
// for its subtasks — at any depth: what that item's group shows on a board,
// asked of any view. A review card stands under what its task stands under,
// and under the task itself. The item is not under itself; on a listing that
// holds plan items (all, strategy) the ones below it are under it too.
func TestUnderKeepsTheCardsBelowAPlanItem(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "g", Type: board.TypeGoal},
		{ItemID: "i", Type: board.TypeInitiative, Parent: "g"},
		{ItemID: "e", Type: board.TypeEpic, Parent: "i"},
		{ItemID: "t1", Parent: "e", Team: "alpha", Parked: true},
		{ItemID: "s1", Parent: "t1", Team: "alpha"},
		{ItemID: "t2", Parent: "i", Team: "alpha", Parked: true},
		{ItemID: "t3", Team: "alpha", Parked: true},
		{ItemID: "r", ReviewOf: "t1", Team: "alpha", Parked: true},
	})
	under := func(view, uid string) []string {
		var out []string
		sel := Selector{View: view, Under: uid}
		if view == "backlog" {
			sel.Team = "alpha"
		}
		for _, c := range FilterCards(b, sel) {
			out = append(out, c.ItemID)
		}
		return out
	}
	for _, tc := range []struct {
		view, under string
		want        []string
	}{
		{"all", "i", []string{"e", "t1", "s1", "t2", "r"}},
		{"all", "e", []string{"t1", "s1", "r"}},
		{"all", "t1", []string{"s1", "r"}},
		{"all", "t3", nil},
		// The same filter on a board: the Backlog's cards under the
		// initiative, a task's subtask delivered with it as every view does.
		{"backlog", "i", []string{"t1", "s1", "t2", "r"}},
	} {
		if got := under(tc.view, tc.under); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("view=%s under=%s = %v, want %v", tc.view, tc.under, got, tc.want)
		}
	}
}
