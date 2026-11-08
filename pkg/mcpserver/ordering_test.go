package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// backlogTitles is team T's backlog as an agent reads it, by title.
func backlogTitles(t *testing.T, cs *mcp.ClientSession) string {
	t.Helper()
	res := call(t, cs, "list_cards", map[string]any{"view": "backlog", "team": "T"})
	var out struct {
		Items []struct {
			Spec struct {
				Title string `json:"title"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &out); err != nil {
		t.Fatalf("listing: %v\n%s", err, textOf(res))
	}
	var titles []string
	for _, it := range out.Items {
		titles = append(titles, it.Spec.Title)
	}
	return strings.Join(titles, " ")
}

// uidOf is the uid a create answered with.
func uidOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var out struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &out); err != nil || out.Metadata.UID == "" {
		t.Fatalf("create answer: %v\n%s", err, textOf(res))
	}
	return out.Metadata.UID
}

// Q3 — THE MCP DOOR places a task made in an empty epic as every other door
// does: create_card on the backlog with a parent, and on the strategy view
// under the epic.
func TestMCPCreatingTasksInEmptyEpicsTakesTheirDefaultPlace(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "A", Title: "A", Type: board.TypeEpic, Team: "T", Rank: "a"},
		{ItemID: "B", Title: "B", Type: board.TypeEpic, Team: "T", Rank: "b"},
		{ItemID: "C", Title: "C", Type: board.TypeEpic, Team: "T", Rank: "c"},
		{ItemID: "X", Title: "X", Type: board.TypeEpic, Team: "T", Rank: "d"},
		{ItemID: "N1", Title: "N1", Team: "T", Parked: true, Rank: "m"},
		{ItemID: "X1", Title: "X1", Team: "T", Parked: true, Parent: "X", Rank: "n"},
	}, map[string]board.SprintState{"T": {Current: board.TodayIso(), ItemID: "sT"}})
	cs := connect(t, Config{Board: "acme"}, fake)
	for i, d := range []struct{ title, epic string }{{"B1", "B"}, {"A1", "A"}, {"C1", "C"}, {"A2", "A"}, {"B2", "B"}} {
		args := map[string]any{"title": d.title, "parent": d.epic, "view": "backlog", "team": "T"}
		if i%2 == 1 {
			args = map[string]any{"title": d.title, "parent": d.epic, "view": "strategy"}
		}
		call(t, cs, "create_card", args)
	}
	if got := backlogTitles(t, cs); got != "N1 A1 A2 B1 B2 C1 X1" {
		t.Fatalf("backlog = %s", got)
	}
}

// Q3, Q5, Q6 — THE AGENT SCENARIO: an agent makes epics, prioritises them
// with move_card BEFORE any task exists, then files a batch of tasks — each
// lands in its epic's area, creation order kept within an epic, with no
// move of a task by anybody. An epic moved later takes its tasks with it,
// and a task moved within its epic keeps to the epic's slots.
func TestAnAgentPrioritisesEmptyEpicsAndThenDecomposesThem(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "I", Title: "ТС2-7 · Kubernetes и S3", Type: board.TypeInitiative, Team: "T", Rank: "a"},
	}, map[string]board.SprintState{"T": {Current: board.TodayIso(), ItemID: "sT"}})
	cs := connect(t, Config{Board: "acme"}, fake)
	epic := map[string]string{}
	for _, name := range []string{"C", "A", "B"} {
		epic[name] = uidOf(t, call(t, cs, "create_card", map[string]any{"title": name, "type": "epic", "parent": "I", "view": "strategy"}))
	}
	// A first, then B after it: the order A < B < C, set while empty.
	call(t, cs, "move_card", map[string]any{"uid": epic["A"], "before": epic["C"]})
	call(t, cs, "move_card", map[string]any{"uid": epic["B"], "after": epic["A"]})
	for _, d := range []struct{ title, epic string }{{"C1", "C"}, {"B1", "B"}, {"A1", "A"}, {"A2", "A"}, {"B2", "B"}, {"C2", "C"}} {
		call(t, cs, "create_card", map[string]any{"title": d.title, "parent": epic[d.epic], "view": "strategy"})
	}
	if got := backlogTitles(t, cs); got != "A1 A2 B1 B2 C1 C2" {
		t.Fatalf("backlog = %s", got)
	}
	// C moved to the top: its tasks gathered ahead of everything.
	res := call(t, cs, "move_card", map[string]any{"uid": epic["C"], "before": epic["A"]})
	var moved struct {
		ActionID string `json:"actionId"`
		Moved    int    `json:"moved"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &moved); err != nil {
		t.Fatal(err)
	}
	if moved.Moved != 3 {
		t.Fatalf("move_card on an epic moved %d, want the epic and its 2 tasks (%s)", moved.Moved, textOf(res))
	}
	if got := backlogTitles(t, cs); got != "C1 C2 A1 A2 B1 B2" {
		t.Fatalf("backlog after C moved up = %s", got)
	}
	// Within B, on the Strategy board: B2 first, over B's own slots.
	call(t, cs, "reorder_in_epic", map[string]any{"uid": firstTitled(t, fake, "B2"), "before": firstTitled(t, fake, "B1")})
	if got := backlogTitles(t, cs); got != "C1 C2 A1 A2 B2 B1" {
		t.Fatalf("backlog after B2 moved within B = %s", got)
	}
}

func firstTitled(t *testing.T, f *boardservicetest.Backend, title string) string {
	t.Helper()
	for i := 1; i < 100; i++ {
		id := "new" + itoa(i)
		if c := f.Card(id); c != nil && c.Title == title {
			return id
		}
	}
	t.Fatalf("no card %q", title)
	return ""
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
