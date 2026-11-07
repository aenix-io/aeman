package gitstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// THE PROJECT BOARD IS GONE, ITS FILES STAY. Project cards (a card file
// with `scope:`, or `epic:` from before schema 2) and the projects roster
// (projects/<id>/project.yaml, its columns and its deadlines) were the Project board's; the board was removed on
// 2026-11-07 and its files were left in the repository as they are — no
// migration, no schema bump. The reader skips them: they are not on the
// board, not listed, not counted under the plan item they were filed
// under, not reachable, and no write, move or maintenance pass ever
// touches them.

const (
	retiredProject = "projects/01P_PORTAL/project.yaml"
	retiredScope   = "projects/01P_PORTAL/scopes/01S_BUGS.yaml"
	retiredColumn  = "projects/01P_PORTAL/epics/01S_OLD.yaml"
	retiredLine    = "projects/01P_PORTAL/deadlines/01D_LINE.yaml"
	// Card files sit where CardPath puts them (the id's last two
	// characters), so a writer addressing the id reaches this very file.
	retiredCard = "cards/o/j/01CARDPROJ.md"
	retiredOld  = "cards/i/c/01CARDEPIC.md"
)

var retiredFiles = map[string]string{
	retiredProject: "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
	retiredScope:   "name: Bugs\nrank: a\ncreated: 2026-06-02T08:00:00Z\n",
	retiredColumn:  "name: Old\nrank: b\ncreated: 2026-06-02T08:00:00Z\n",
	retiredLine:    "week: 2026-09-07\ncreated: 2026-06-03T08:00:00Z\n",
	// A project card filed under the epic, and one written before schema 2,
	// when the column key was `epic`.
	retiredCard: "---\ntitle: Q2 plan\nproject: portal\nscope: Bugs\nparent: 01EPIC\nstart: 2026-09-07\nday: 2026-09-13\nrank: c\ncreated: 2026-08-20T09:00:00Z\n---\n\nKept as it is.\n",
	retiredOld:  "---\ntitle: old slot\nepic: Bugs\nrank: d\ncreated: 2026-08-20T09:00:00Z\n---\n",
}

func retiredRepo(t *testing.T) *Repo {
	t.Helper()
	return retiredRepoWith(t, nil)
}

func retiredRepoWith(t *testing.T, extra map[string]string) *Repo {
	t.Helper()
	files := map[string]string{
		BoardPath:              "schema: 3\ntitle: b\n",
		TeamPath("_"):          "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		TeamPath("01T_PORTAL"): "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		"cards/i/c/01EPIC.md":  "---\ntitle: Billing\ntype: epic\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
		"cards/s/k/01TASK.md":  "---\ntitle: Receipts\nteam: portal\nparent: 01EPIC\nparked: true\nrank: b\ncreated: 2026-08-20T09:00:00Z\n---\n",
		"cards/k/2/01TASK2.md": "---\ntitle: Invoices\nteam: portal\nparked: true\nrank: e\ncreated: 2026-08-20T09:00:00Z\n---\n",
	}
	for p, d := range retiredFiles {
		files[p] = d
	}
	for p, d := range extra {
		files[p] = d
	}
	return repoWith(t, files)
}

func TestTheReaderSkipsTheProjectBoardsFiles(t *testing.T) {
	r := retiredRepo(t)
	s, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, c := range s.Cards {
		ids[c.ItemID] = true
	}
	if ids["01CARDPROJ"] || ids["01CARDEPIC"] || len(s.Cards) != 3 {
		t.Fatalf("the cards read = %v, want the epic and the two tasks alone", ids)
	}
	for _, p := range s.Unknown {
		if _, retired := retiredFiles[p]; retired {
			t.Fatalf("%s is listed as unknown, which health reports: it is a retired file, skipped quietly", p)
		}
	}
	for _, b := range s.Broken {
		t.Fatalf("a retired file read as broken: %v", b)
	}

	be := NewBackend(r, BackendOptions{})
	bd, err := be.LoadBoard(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	epic, _ := board.Find(bd, "01EPIC")
	if _, _, total := board.NewIndex(bd).HierarchyStatus(epic); total != 1 {
		t.Fatalf("the epic counts %d tasks, want the one task alone (not the project card filed under it)", total)
	}
	if _, ok := board.Find(bd, "01CARDPROJ"); ok {
		t.Fatal("a project card is on the board")
	}
}

// No write, delete, move or maintenance pass touches them: every retired
// file is byte-identical afterwards.
func TestNothingWritesTheProjectBoardsFiles(t *testing.T) {
	shared := retiredRepo(t)
	other := repoWith(t, map[string]string{
		"cards/e/r/01OTHER.md": "---\ntitle: Elsewhere\nteam: portal\nparked: true\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
		// The same project card in a second domain: a ghost of a move once,
		// never the sweep's to resolve now.
		retiredCard: retiredFiles[retiredCard],
	})
	clock := at("2026-08-28T09:00:00Z")
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "other", Repo: other}},
		BackendOptions{Now: func() time.Time { clock = clock.Add(time.Second); return clock }})
	ctx := ctxAs("kvaps")
	bd, err := mb.LoadBoard(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	task, _ := board.Find(bd, "01TASK")
	if err := mb.RenameCard(ctx, bd, task, "Receipts again"); err != nil {
		t.Fatal(err)
	}
	t2, _ := board.Find(bd, "01TASK2")
	if err := mb.DeleteCard(ctx, bd, t2); err != nil {
		t.Fatal(err)
	}
	epic, _ := board.Find(bd, "01EPIC")
	if err := mb.DeleteCard(ctx, bd, epic); err != nil {
		t.Fatal(err)
	}
	if _, err := mb.SweepGhosts(ctx, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	for p, want := range retiredFiles {
		got, err := shared.ReadFile(p)
		if err != nil || string(got) != want {
			t.Fatalf("%s after the writes = %q (%v), want it byte-identical", p, got, err)
		}
	}
	if got, err := other.ReadFile(retiredCard); err != nil || string(got) != retiredFiles[retiredCard] {
		t.Fatalf("the second domain's copy = %q (%v), want it untouched", got, err)
	}
}

// A writer handed a project card's id from outside the loaded board — an
// embedder, a stale client — is told the card is not there, and the file
// stays as it is: no delete, no rewrite, no reload.
func TestAProjectCardsIDReachesNoWrite(t *testing.T) {
	r := retiredRepo(t)
	be := NewBackend(r, BackendOptions{})
	ctx := ctxAs("kvaps")
	bd, err := be.LoadBoard(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	head := r.Head()
	for _, id := range []string{"01CARDPROJ", "01CARDEPIC"} {
		card := board.Card{ItemID: id, Title: "addressed from outside"}
		if err := be.DeleteCard(ctx, bd, card); !errors.Is(err, ErrCardNotFound) {
			t.Errorf("delete %s = %v, want ErrCardNotFound", id, err)
		}
		if err := be.RenameCard(ctx, bd, card, "renamed"); !errors.Is(err, ErrCardNotFound) {
			t.Errorf("rename %s = %v, want ErrCardNotFound", id, err)
		}
		if err := be.SetProgress(ctx, bd, card, 100); !errors.Is(err, ErrCardNotFound) {
			t.Errorf("progress on %s = %v, want ErrCardNotFound", id, err)
		}
	}
	got, err := be.LoadCards(ctx, bd, []string{"01CARDPROJ", "01CARDEPIC", "01TASK"})
	if err != nil || len(got) != 1 || got[0].ItemID != "01TASK" {
		t.Fatalf("LoadCards = %+v (%v), want the task alone", got, err)
	}
	if r.Head() != head {
		t.Fatal("a refused write made a commit")
	}
	for p, want := range retiredFiles {
		if data, err := r.ReadFile(p); err != nil || string(data) != want {
			t.Fatalf("%s = %q (%v), want it byte-identical", p, data, err)
		}
	}
}

// A task filed under a project card reads with no parent on a reload of
// that one card, as it does on a read of the whole board (dropDanglingParents):
// otherwise every edit of it brought the dead link back and hid the task as
// the subtask of a card no board draws. Across repositories too — the
// project card may sit in the other one.
func TestAReloadedCardDropsAParentThatIsAProjectCard(t *testing.T) {
	under := "---\ntitle: piece of the plan\nteam: portal\nparent: 01CARDPROJ\nparked: true\nrank: f\ncreated: 2026-08-20T09:00:00Z\n---\n"
	r := retiredRepoWith(t, map[string]string{"cards/c/e/01PIECE.md": under})
	got, err := NewBackend(r, BackendOptions{}).LoadCards(context.Background(), board.Board{}, []string{"01PIECE", "01TASK"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Parent != "" || got[1].Parent != "01EPIC" {
		t.Fatalf("reloaded = %+v, want the project-card parent dropped and the epic kept", got)
	}

	other := repoWith(t, map[string]string{BoardPath: "schema: 3\ntitle: o\n", "cards/c/e/01PIECE.md": under})
	shared := retiredRepo(t)
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "other", Repo: other}}, BackendOptions{})
	got, err = mb.LoadCards(context.Background(), board.Board{}, []string{"01PIECE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Parent != "" {
		t.Fatalf("across repositories = %+v, want the project-card parent dropped", got)
	}
}
