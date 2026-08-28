package gitstore

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
)

// Load reads the whole tree at the branch tip into one snapshot: every
// card, the roster, the board file — sorted by rank, ids from paths, unknown
// paths listed rather than guessed at.

func seedBoard(t *testing.T, r *Repo) {
	t.Helper()
	files := map[string]string{
		BoardPath:                                 "schema: 1\ntitle: aeman board\n",
		TeamPath("_"):                             "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		TeamPath("01JB4TEAM"):                     "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-21\n",
		ProjectPath("01JB4PROJ"):                  "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		EpicPath("01JB4PROJ", "01JB4EPIC"):        "name: Bugs\nrank: a\ncreated: 2026-06-02T08:00:00Z\n",
		DeadlinePath("01JB4PROJ", "01JB4DL"):      "week: 2026-09-07\ncreated: 2026-06-03T08:00:00Z\n",
		ProcessPath("01JB4PROC"):                  "name: Invoicing\nproject: portal\nrank: a\ncreated: 2026-06-04T08:00:00Z\n",
		TaskPath("01JB4PROC", "01JB4TASK"):        "---\ntitle: Invoice ACME\nteam: portal\nrecurrence: month\nrank: a\ncreated: 2026-06-04T08:00:00Z\n---\n\nSend the invoice.\n",
		"cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md": "---\ntitle: first\nteam: portal\nzone: yellow\nprogress: 40\nrank: b\ncreated: 2026-08-26T09:14:03Z\n---\n\nDetails.\n",
		"cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md": "---\ntitle: second\nteam: portal\nrank: a\ncreated: 2026-08-26T09:15:03Z\n---\n",
		"README.md":                               "not ours\n",
	}
	var writes []FileWrite
	for p, d := range files {
		writes = append(writes, FileWrite{Path: p, Data: []byte(d)})
	}
	if _, err := r.Commit(Action{Name: "import", Summary: "seed"}, writes); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsTheWholeTree(t *testing.T) {
	r := newRepo(t)
	seedBoard(t, r)
	s, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	if s.Board.Title != "aeman board" || s.Board.Schema != 1 {
		t.Fatalf("board = %+v", s.Board)
	}
	// Cards come back in rank order, ids from their paths, bodies parsed.
	if len(s.Cards) != 2 {
		t.Fatalf("cards = %d", len(s.Cards))
	}
	if s.Cards[0].ItemID != "01JB4K2E7QZMX3R8V0N5T9WYC2" || s.Cards[1].ItemID != "01JB4K2E7QZMX3R8V0N5T9WYC1" {
		t.Fatalf("card order = %s, %s (want rank a before rank b)", s.Cards[0].ItemID, s.Cards[1].ItemID)
	}
	if s.Cards[1].Progress != 40 || s.Cards[1].Zone != board.ZoneYellow || s.Cards[1].Description != "Details." {
		t.Fatalf("card fields lost: %+v", s.Cards[1])
	}
	// Teams: the no-team group first by rank, portal with its pointer.
	if len(s.Teams) != 2 || s.Teams[0].ID != "_" || s.Teams[1].Name != "portal" || s.Teams[1].Sprint.Current != "2026-08-24" {
		t.Fatalf("teams = %+v", s.Teams)
	}
	// Projects carry their epics and deadlines.
	if len(s.Projects) != 1 || s.Projects[0].ID != "01JB4PROJ" || s.Projects[0].Name != "portal" {
		t.Fatalf("projects = %+v", s.Projects)
	}
	if len(s.Projects[0].Epics) != 1 || s.Projects[0].Epics[0].Name != "Bugs" || len(s.Projects[0].Deadlines) != 1 || s.Projects[0].Deadlines[0].Week != "2026-09-07" {
		t.Fatalf("project children = %+v", s.Projects[0])
	}
	// Processes carry their tasks, which are card files.
	if len(s.Processes) != 1 || s.Processes[0].Name != "Invoicing" || len(s.Processes[0].Tasks) != 1 {
		t.Fatalf("processes = %+v", s.Processes)
	}
	if task := s.Processes[0].Tasks[0]; task.ID != "01JB4TASK" || task.Card.Title != "Invoice ACME" || task.Card.Recurrence != "month" || task.Card.Description != "Send the invoice." {
		t.Fatalf("task = %+v", task)
	}
	// What is not ours is named, not silently dropped or misread.
	if len(s.Unknown) != 1 || s.Unknown[0] != "README.md" {
		t.Fatalf("unknown = %v", s.Unknown)
	}
}

// Equal ranks tie-break by id, so two writers minting the same rank still
// produce one deterministic order everywhere.
func TestLoadTieBreaksByID(t *testing.T) {
	r := newRepo(t)
	_, err := r.Commit(Action{Name: "import", Summary: "seed"}, []FileWrite{
		{Path: "cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md", Data: []byte("---\ntitle: two\nrank: m\n---\n")},
		{Path: "cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md", Data: []byte("---\ntitle: one\nrank: m\n---\n")},
		{Path: "cards/c/3/01JB4K2E7QZMX3R8V0N5T9WYC3.md", Data: []byte("---\ntitle: three\n---\n")}, // no rank at all
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{s.Cards[0].ItemID, s.Cards[1].ItemID, s.Cards[2].ItemID}
	// An empty rank sorts before everything (it is the smallest string);
	// equal ranks fall back to the id.
	want := []string{"01JB4K2E7QZMX3R8V0N5T9WYC3", "01JB4K2E7QZMX3R8V0N5T9WYC1", "01JB4K2E7QZMX3R8V0N5T9WYC2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// A newer schema is refused at load — the server must not misread a
// repository written by its successor.
func TestLoadRefusesNewerSchema(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Commit(Action{Name: "import", Summary: "seed"}, []FileWrite{{Path: BoardPath, Data: []byte("schema: 99\n")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(r); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("err = %v, want ErrSchemaNewer", err)
	}
}

// An unborn branch is a distinct, named condition: `serve` turns it into
// "run aeman init", not into an empty board.
func TestLoadEmptyRepositoryIsNamed(t *testing.T) {
	r, err := Init(memory.NewStorage(), Options{Committer: serverID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(r); !errors.Is(err, ErrEmptyRepository) {
		t.Fatalf("err = %v, want ErrEmptyRepository", err)
	}
}

// A file that is ours by path but broken inside is reported with its path,
// not swallowed and not fatal for the rest of the board.
func TestLoadReportsBrokenFile(t *testing.T) {
	r := newRepo(t)
	_, err := r.Commit(Action{Name: "import", Summary: "seed"}, []FileWrite{
		{Path: "cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md", Data: []byte("---\ntitle: fine\n---\n")},
		{Path: "cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md", Data: []byte("no front matter here\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Load(r)
	if err != nil {
		t.Fatalf("a broken card must not fail the load: %v", err)
	}
	if len(s.Cards) != 1 || s.Cards[0].Title != "fine" {
		t.Fatalf("cards = %+v", s.Cards)
	}
	if len(s.Broken) != 1 || s.Broken[0].Path != "cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md" || s.Broken[0].Err == nil {
		t.Fatalf("broken = %+v", s.Broken)
	}
}
