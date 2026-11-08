package board

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// orderingCases is testdata/ordering_cases.json, shared with the client's
// mirror (web/src/queue.test.ts).
type orderingCases struct {
	BestGap []struct {
		Queue []string `json:"queue"`
		Epic  string   `json:"epic"`
		Want  int      `json:"want"`
	} `json:"bestGap"`
	Create []struct {
		Queue   []string `json:"queue"`
		Creates []string `json:"creates"`
		Want    []string `json:"want"`
	} `json:"create"`
	MoveEpic []struct {
		Queue     []string `json:"queue"`
		EpicOrder []string `json:"epicOrder"`
		Epic      string   `json:"epic"`
		Want      []string `json:"want"`
	} `json:"moveEpic"`
	ReorderInEpic []struct {
		Queue  []string `json:"queue"`
		Task   string   `json:"task"`
		After  string   `json:"after"`
		Before string   `json:"before"`
		Want   []string `json:"want"`
	} `json:"reorderInEpic"`
}

func loadOrderingCases(t *testing.T) orderingCases {
	t.Helper()
	raw, err := os.ReadFile("testdata/ordering_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var oc orderingCases
	if err := json.Unmarshal(raw, &oc); err != nil {
		t.Fatal(err)
	}
	return oc
}

// caseEpic is the epic a fixture id names: its first letter, or none for L/N.
func caseEpic(id string) string {
	if id == "" || id[0] == 'L' || id[0] == 'N' {
		return ""
	}
	return id[:1]
}

// caseBoard is a fixture's board: the epics in their order, then the queue
// of team T, every card keyed in that order.
func caseBoard(epicOrder, queue []string) Board {
	if len(epicOrder) == 0 {
		epicOrder = []string{"A", "B", "C", "X"}
	}
	q := &qboard{}
	n := 0
	key := func() string { n++; return fmt.Sprintf("k%03d", n) }
	for _, e := range epicOrder {
		q.epic(e, key())
	}
	for _, id := range queue {
		q.task(id, caseEpic(id), "T", key())
	}
	return q.board()
}

// THE ORDERING RULES ARE ONE RULE ON BOTH SIDES: the shared case table.
func TestTheOrderingRulesMatchTheSharedCases(t *testing.T) {
	oc := loadOrderingCases(t)
	order := EpicOrder{"A": 0, "B": 1, "C": 2, "X": 3}
	for _, c := range oc.BestGap {
		q := make([]Card, len(c.Queue))
		for i, id := range c.Queue {
			q[i] = Card{ItemID: id}
		}
		if got := BestGap(q, c.Epic, order, func(k Card) string { return caseEpic(k.ItemID) }); got != c.Want {
			t.Errorf("bestGap %v %s = %d, want %d", c.Queue, c.Epic, got, c.Want)
		}
	}
	for _, c := range oc.Create {
		b := caseBoard(nil, c.Queue)
		for _, id := range c.Creates {
			b = create(t, b, id, caseEpic(id), "T")
		}
		if got := queueOf(b, "T"); got != strings.Join(c.Want, " ") {
			t.Errorf("create %v into %v = %s, want %v", c.Creates, c.Queue, got, c.Want)
		}
	}
	for _, c := range oc.MoveEpic {
		b := caseBoard(nil, c.Queue)
		// The move: the epic right after its new predecessor in the order.
		after := ""
		for i, e := range c.EpicOrder {
			if e == c.Epic && i > 0 {
				after = c.EpicOrder[i-1]
			}
		}
		changes, err := MoveEpicPlan(b, c.Epic, after)
		b = mustApply(t, b, changes, err)
		if got := queueOf(b, "T"); got != strings.Join(c.Want, " ") {
			t.Errorf("move %s in %v = %s, want %v", c.Epic, c.Queue, got, c.Want)
		}
	}
	for _, c := range oc.ReorderInEpic {
		b := caseBoard(nil, c.Queue)
		changes, err := ReorderInEpicPlan(b, c.Task, c.After, c.Before)
		b = mustApply(t, b, changes, err)
		if got := queueOf(b, "T"); got != strings.Join(c.Want, " ") {
			t.Errorf("reorder %s in %v = %s, want %v", c.Task, c.Queue, got, c.Want)
		}
	}
}
