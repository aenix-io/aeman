package boardservice

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// emptyEpicsBoard is the brief's main scenario: epics A < B < C prioritised
// while EMPTY, each carried out by team T; T's queue already holds a neutral
// task N1 and a task X1 of a later epic X. Every card has a key, as a store
// gives one.
func emptyEpicsBoard() *fakeBackend {
	f := newFake([]board.Card{
		{ItemID: "A", Title: "A", Type: board.TypeEpic, Team: "T", Rank: "a"},
		{ItemID: "B", Title: "B", Type: board.TypeEpic, Team: "T", Rank: "b"},
		{ItemID: "C", Title: "C", Type: board.TypeEpic, Team: "T", Rank: "c"},
		{ItemID: "X", Title: "X", Type: board.TypeEpic, Team: "T", Rank: "d"},
		{ItemID: "N1", Title: "N1", Team: "T", Parked: true, Rank: "m"},
		{ItemID: "X1", Title: "X1", Team: "T", Parked: true, Parent: "X", Rank: "n"},
	}, map[string]board.SprintState{"T": {Current: board.TodayIso(), ItemID: "sT"}})
	return f
}

// queueNames is team T's backlog in the order it reads, by title.
func queueNames(t *testing.T, s *Service) string {
	t.Helper()
	b, err := s.backend.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	board.SortCards(b.Cards)
	var out []string
	for _, c := range board.NewIndex(b).Queue("T") {
		out = append(out, c.Title)
	}
	return strings.Join(out, " ")
}

// The decomposition, in the order the engineers type it.
var decomposition = []struct{ title, epic string }{
	{"B1", "B"}, {"A1", "A"}, {"C1", "C"}, {"A2", "A"}, {"B2", "B"},
}

// What every door must produce: each task in its epic's area, in creation
// order, the epics in their order; N1 (neutral) and X1 (a later epic) where
// they stood.
const decomposed = "N1 A1 A2 B1 B2 C1 X1"

// Q3 — CREATING TASKS IN EMPTY EPICS, EVERY SERVICE DOOR. A task made under
// an epic lands after that epic's last parked task, or — the first task of
// an empty epic — where the epic order puts it; nobody orders anything by
// hand. The Strategy board's "+" under an epic (no team, no day: it stands
// under the epic's team), the Backlog's create with a parent, and the raw
// door all give the same queue. The MCP door and the agent scenario are
// pkg/mcpserver's; the direct git writer pkg/gitstore's.
func TestCreatingTasksInEmptyEpicsThroughEveryDoor(t *testing.T) {
	doors := map[string]func(s *Service, title, epic string) error{
		"strategy": func(s *Service, title, epic string) error {
			_, err := s.CreateInView(ctx, "acme", board.ViewStrategy, CreateCardArgs{Title: title, Parent: epic})
			return err
		},
		"backlog": func(s *Service, title, epic string) error {
			_, err := s.CreateInView(ctx, "acme", board.ViewBacklog, CreateCardArgs{Title: title, Team: "T", Parent: epic})
			return err
		},
		"all (REST's raw door)": func(s *Service, title, epic string) error {
			_, err := s.CreateInView(ctx, "acme", board.ViewAll, CreateCardArgs{Title: title, Parked: true, Parent: epic})
			return err
		},
	}
	for name, door := range doors {
		t.Run(name, func(t *testing.T) {
			s := New(emptyEpicsBoard())
			for _, d := range decomposition {
				if err := door(s, d.title, d.epic); err != nil {
					t.Fatal(err)
				}
			}
			if got := queueNames(t, s); got != decomposed {
				t.Fatalf("queue = %s, want %s", got, decomposed)
			}
		})
	}
}

// Q3, Q10 — the Backlog "+" places EXACTLY where it was pressed, and an
// explicit place wins over the default; with inheritParent the gap's epic
// is the task's.
func TestTheBacklogPlusPlacesExactlyWhereItWasPressed(t *testing.T) {
	s := New(emptyEpicsBoard())
	// Typed right after N1 with the inheritance asked for: N1 is neutral,
	// so the task is neutral too, and stands right there.
	if _, err := s.CreateInView(ctx, "acme", board.ViewBacklog,
		CreateCardArgs{Title: "N2", Team: "T", After: "N1", InheritParent: true}); err != nil {
		t.Fatal(err)
	}
	if got := queueNames(t, s); got != "N1 N2 X1" {
		t.Fatalf("queue = %s", got)
	}
}

// Q5 — the epic move is the same operation from every board: one Move on an
// epic gathers its tasks (the Strategy board's drag and the Backlog's Epics
// row both send it).
func TestMovingAnEpicThroughTheServiceGathersItsTasks(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "A", Title: "A", Type: board.TypeEpic, Rank: "a"},
		{ItemID: "B", Title: "B", Type: board.TypeEpic, Rank: "b"},
		{ItemID: "A1", Title: "A1", Team: "T", Parked: true, Parent: "A", Rank: "c"},
		{ItemID: "B2", Title: "B2", Team: "T", Parked: true, Parent: "B", Rank: "d"},
		{ItemID: "A2", Title: "A2", Team: "T", Parked: true, Parent: "A", Rank: "e"},
		{ItemID: "B1", Title: "B1", Team: "T", Parked: true, Parent: "B", Rank: "f"},
	}, nil)
	s := New(f)
	res, err := s.Move(ctx, "acme", "B", board.Spot{Before: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved != 3 {
		t.Fatalf("moved %d, want the epic and its two tasks", res.Moved)
	}
	if got := queueNames(t, s); got != "B2 B1 A1 A2" {
		t.Fatalf("queue = %s", got)
	}
	// And within the epic on the Strategy board: B1 first, over B's slots.
	if _, err := s.ReorderInEpic(ctx, "acme", "B1", "", "B2"); err != nil {
		t.Fatal(err)
	}
	if got := queueNames(t, s); got != "B1 B2 A1 A2" {
		t.Fatalf("queue = %s", got)
	}
	if _, err := s.ReorderInEpic(ctx, "acme", "N", "", ""); err == nil {
		t.Fatal("a card that is not on the board was reordered")
	}
}

// Q9 — a parked task handed to another team lands after its epic's last
// task in that team's queue; Q8 — a task back from work keeps its key only
// inside its epic's area.
func TestAHandOverAndAReturnArePlacedByTheQueue(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "A", Title: "A", Type: board.TypeEpic, Rank: "a"},
		{ItemID: "B", Title: "B", Type: board.TypeEpic, Rank: "b"},
		{ItemID: "A9", Title: "A9", Team: "T", Parked: true, Parent: "A", Rank: "c"},
		{ItemID: "A1", Title: "A1", Team: "U", Parked: true, Parent: "A", Rank: "d"},
		{ItemID: "B1", Title: "B1", Team: "U", Parked: true, Parent: "B", Rank: "e"},
		{ItemID: "wB", Title: "wB", Team: "U", Parent: "B", Rank: "0a", StartDate: board.TodayIso(), Day: board.TodayIso()},
	}, map[string]board.SprintState{"T": {ItemID: "sT"}, "U": {ItemID: "sU"}})
	s := New(f)
	if err := s.SetTeam(ctx, "acme", "A9", "U", ""); err != nil {
		t.Fatal(err)
	}
	if got := teamQueue(t, s, "U"); got != "A1 A9 B1" {
		t.Fatalf("U after the hand-over = %s", got)
	}
	// wB was first of all by key; back from work it goes after B's last.
	if err := s.SetBacklog(ctx, "acme", "wB", true); err != nil {
		t.Fatal(err)
	}
	if got := teamQueue(t, s, "U"); got != "A1 A9 B1 wB" {
		t.Fatalf("U after the return = %s", got)
	}
}

func teamQueue(t *testing.T, s *Service, team string) string {
	t.Helper()
	b, err := s.backend.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	board.SortCards(b.Cards)
	var out []string
	for _, c := range board.NewIndex(b).Queue(team) {
		out = append(out, c.Title)
	}
	return strings.Join(out, " ")
}
