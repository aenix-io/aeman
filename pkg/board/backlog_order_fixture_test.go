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
		ItemID      string `json:"itemId"`
		Type        string `json:"type"`
		Parent      string `json:"parent"`
		Team        string `json:"team"`
		Parked      bool   `json:"parked"`
		Priority    string `json:"priority"`
		ReviewOf    string `json:"reviewOf"`
		SprintStart string `json:"sprintStart"`
		StartDate   string `json:"startDate"`
		Stage       string `json:"stage"`
		Progress    int    `json:"progress"`
	} `json:"cards"`
	Order []string `json:"order"`
}

func (fx backlogFixture) board() Board {
	var cards []Card
	for _, c := range fx.Cards {
		cards = append(cards, Card{ItemID: c.ItemID, Type: c.Type, Parent: c.Parent, Team: c.Team,
			Parked: c.Parked, Priority: PriorityKey(c.Priority), ReviewOf: c.ReviewOf,
			SprintStart: c.SprintStart, StartDate: c.StartDate, Stage: StageKey(c.Stage), Progress: c.Progress})
	}
	return NewBoard(cards)
}

// THE BACKLOG'S ORDER IS ONE RULE ON BOTH SIDES (K14, K15, K10). The fixture
// is shared with the client's mirror (web/src/backlogboard.test.ts reads the
// same file): priorities own and inherited, a review nobody has taken
// leading its task's group, one of a task in work, one that carries a parent
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
