package board

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// backlogFixture is testdata/backlog_order.json: the board, what the server
// sends a client for each backlog card, and the order both sides read.
type backlogFixture struct {
	Cards []struct {
		ItemID      string   `json:"itemId"`
		Type        string   `json:"type"`
		Parent      string   `json:"parent"`
		Team        string   `json:"team"`
		Parked      bool     `json:"parked"`
		ReviewOf    string   `json:"reviewOf"`
		SprintStart string   `json:"sprintStart"`
		StartDate   string   `json:"startDate"`
		Stage       string   `json:"stage"`
		Progress    int      `json:"progress"`
		Assignees   []string `json:"assignees"`
	} `json:"cards"`
	Order   []string        `json:"order"`
	Subtask map[string]bool `json:"subtask"`
}

func (fx backlogFixture) board() Board {
	var cards []Card
	for _, c := range fx.Cards {
		cards = append(cards, Card{ItemID: c.ItemID, Type: c.Type, Parent: c.Parent, Team: c.Team,
			Parked: c.Parked, ReviewOf: c.ReviewOf,
			SprintStart: c.SprintStart, StartDate: c.StartDate, Stage: StageKey(c.Stage), Progress: c.Progress,
			Assignees: c.Assignees})
	}
	return NewBoard(cards)
}

// THE BACKLOG'S ORDER IS ONE RULE ON BOTH SIDES (K14, K10). The fixture is
// shared with the client's mirror (web/src/backlogboard.test.ts reads the
// same file): the reviews nobody has taken first in their tasks' Roadmap
// order, one of them of a task in work, one that carries a parent
// of its own (a direct writer's stray: it is placed by the task it reviews,
// never by that parent) and a review of a review (placed by the review it
// reviews) — the same cards must read in the same order.
func TestTheBacklogOrderMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/backlog_order.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx backlogFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	b := fx.board()
	ix := NewIndex(b)
	var shelf []Card
	for _, c := range b.Cards {
		if OnBacklog(c, ix.IsSubtask) {
			shelf = append(shelf, c)
		}
	}
	got := []string{}
	for _, c := range BacklogOrder(ix, shelf) {
		got = append(got, c.ItemID)
	}
	if !reflect.DeepEqual(got, fx.Order) {
		t.Fatalf("BacklogOrder = %v\nwant           %v", got, fx.Order)
	}
}

// WHICH CARD RIDES A PARENT TASK IS ONE ANSWER ON BOTH SIDES. A review card
// carries the reviewed task's chain on the wire, not a parent of its own, so
// the client cannot read a stray parent's type off its lineage: the server
// says (status.subtask), and a taken review whose stray parent names a plan
// item stays on the day boards on both sides (web/src/subtasks.test.ts
// reads the same fixture).
func TestWhoRidesAParentTaskMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/backlog_order.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx backlogFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	b := fx.board()
	for id, want := range fx.Subtask {
		c, ok := Find(b, id)
		if !ok {
			t.Fatalf("no card %s", id)
		}
		if got := IsSubtask(b, c); got != want {
			t.Errorf("IsSubtask(%s) = %v, the fixture says %v", id, got, want)
		}
	}
}
