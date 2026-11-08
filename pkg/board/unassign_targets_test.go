package board

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// UNASSIGNED reaches exactly the open tasks in work under a row (K16) — what
// makes a plan item IN PROGRESS (H5). The fixture is shared with the client's
// mirror (web/src/strategy.test.ts reads the same file): the same cards must
// give the same tasks on both sides, a task on review, one in a past sprint,
// one in a sprint with no dates, one of another team among them; a subtask,
// a review card and finished or parked work never.
func TestUnassignTargetsMatchTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/unassign_targets.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Cards []struct {
			ItemID      string   `json:"itemId"`
			Type        string   `json:"type"`
			Parent      string   `json:"parent"`
			Team        string   `json:"team"`
			Parked      bool     `json:"parked"`
			StartDate   string   `json:"startDate"`
			Day         string   `json:"day"`
			SprintStart string   `json:"sprintStart"`
			Stage       string   `json:"stage"`
			Progress    int      `json:"progress"`
			ReviewOf    string   `json:"reviewOf"`
			Assignees   []string `json:"assignees"`
		} `json:"cards"`
		Targets map[string][]string `json:"targets"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	var cards []Card
	for _, c := range fx.Cards {
		cards = append(cards, Card{
			ItemID: c.ItemID, Type: c.Type, Parent: c.Parent, Team: c.Team, Parked: c.Parked,
			StartDate: c.StartDate, Day: c.Day, SprintStart: c.SprintStart,
			Stage: StageKey(c.Stage), Progress: c.Progress, ReviewOf: c.ReviewOf, Assignees: c.Assignees,
		})
	}
	ix := NewIndex(NewBoard(cards))
	for root, want := range fx.Targets {
		c, ok := ix.Find(root)
		if !ok {
			t.Fatalf("no card %s", root)
		}
		got := []string{}
		for _, x := range ix.UnassignTargets(c) {
			got = append(got, x.ItemID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("UnassignTargets(%s) = %v, want %v", root, got, want)
		}
	}
	// And the plan items read IN PROGRESS exactly while they have targets.
	for _, root := range []string{"g", "i", "e", "e2"} {
		c, _ := ix.Find(root)
		if st, _, _ := ix.HierarchyStatus(c); (st == HierarchyInProgress) != (len(fx.Targets[root]) > 0) {
			t.Errorf("%s reads %s with %d tasks in work", root, st, len(fx.Targets[root]))
		}
	}
}
