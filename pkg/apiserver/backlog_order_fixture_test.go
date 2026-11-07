package apiserver

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The client reads a backlog card's place off what the server sends it —
// status.lineage and status.effectivePriority — so the shared backlog-order
// fixture (pkg/board/testdata/backlog_order.json) states that wire input,
// and this holds it to CardResource's own answer. A REVIEW card's lineage is
// the planning chain of the card it reviews and nothing of its own (K10): a
// stray parent of its own is not on it, which is how the Go order and the
// client's agree on where it stands.
func TestTheBacklogFixtureWireIsTheServersAnswer(t *testing.T) {
	raw, err := os.ReadFile("../board/testdata/backlog_order.json")
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
			Priority    string   `json:"priority"`
			ReviewOf    string   `json:"reviewOf"`
			SprintStart string   `json:"sprintStart"`
			StartDate   string   `json:"startDate"`
			Stage       string   `json:"stage"`
			Progress    int      `json:"progress"`
			Assignees   []string `json:"assignees"`
		} `json:"cards"`
		Wire map[string]struct {
			Lineage           []string `json:"lineage"`
			EffectivePriority string   `json:"effectivePriority"`
			Subtask           bool     `json:"subtask"`
		} `json:"wire"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	var cards []board.Card
	for _, c := range fx.Cards {
		cards = append(cards, board.Card{ItemID: c.ItemID, Type: c.Type, Parent: c.Parent, Team: c.Team,
			Parked: c.Parked, Priority: board.PriorityKey(c.Priority), ReviewOf: c.ReviewOf,
			SprintStart: c.SprintStart, StartDate: c.StartDate, Stage: board.StageKey(c.Stage), Progress: c.Progress,
			Assignees: c.Assignees})
	}
	b := board.NewBoard(cards)
	for id, want := range fx.Wire {
		c, ok := board.Find(b, id)
		if !ok {
			t.Fatalf("no card %s", id)
		}
		st := CardResource(b, c).Status
		got := []string{}
		for _, l := range st.Lineage {
			got = append(got, l.Type+":"+l.UID)
		}
		if want.Lineage == nil {
			want.Lineage = []string{}
		}
		if !reflect.DeepEqual(got, want.Lineage) || st.EffectivePriority != want.EffectivePriority || st.Subtask != want.Subtask {
			t.Errorf("%s on the wire: lineage %v priority %q subtask %v, the fixture says %v %q %v",
				id, got, st.EffectivePriority, st.Subtask, want.Lineage, want.EffectivePriority, want.Subtask)
		}
	}
}
