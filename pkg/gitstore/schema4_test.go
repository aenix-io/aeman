package gitstore

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
)

// schema3Domains is a board of two repositories at schema 3 whose backlog
// order (the Strategy tree, K14) is NOT the key order, interleaved across
// the repositories: goal g2 before g1 by key, tasks keyed against the tree.
func schema3Domains(t *testing.T) []Domain {
	t.Helper()
	card := func(id, body string) (string, string) {
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return p, "---\n" + body + "created: 2026-08-20T09:00:00Z\n---\n"
	}
	primary := map[string]string{
		BoardPath:              "schema: 3\ntitle: b\n",
		TeamPath("01T_PORTAL"): "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
	}
	closed := map[string]string{
		BoardPath:              "schema: 3\ntitle: c\n",
		TeamPath("01T_SECRET"): "name: secret\nrank: c\ncreated: 2026-06-01T08:00:00Z\n",
	}
	for _, c := range []struct{ id, body string }{
		{"01G2000000000000000000000A", "title: g2\ntype: goal\nrank: a\n"},
		{"01G1000000000000000000000A", "title: g1\ntype: goal\nrank: b\n"},
		{"01E1000000000000000000000A", "title: e1\ntype: epic\nparent: 01G1000000000000000000000A\nrank: c\n"},
		{"01E2000000000000000000000A", "title: e2\ntype: epic\nparent: 01G2000000000000000000000A\nrank: d\n"},
		{"01TE1A00000000000000000000", "title: e1 a\nteam: portal\nparked: true\nparent: 01E1000000000000000000000A\nrank: e\n"},
		{"01TE2A00000000000000000000", "title: e2 a\nteam: portal\nparked: true\nparent: 01E2000000000000000000000A\nrank: f\n"},
		{"01LOOSE0000000000000000000", "title: loose\nteam: portal\nparked: true\nrank: 0\n"},
	} {
		p, d := card(c.id, c.body)
		primary[p] = d
	}
	for _, c := range []struct{ id, body string }{
		{"01SE1B00000000000000000000", "title: secret e1\nteam: secret\nparked: true\nparent: 01E1000000000000000000000A\nrank: a5\n"},
		{"01SE2B00000000000000000000", "title: secret e2\nteam: secret\nparked: true\nparent: 01E2000000000000000000000A\nrank: g\n"},
	} {
		p, d := card(c.id, c.body)
		closed[p] = d
	}
	return []Domain{{Name: "shared", Repo: repoWith(t, primary)}, {Name: "closed", Repo: repoWith(t, closed)}}
}

func mergedBoard(t *testing.T, ds []Domain) board.Board {
	t.Helper()
	b, err := NewMultiBackend(ds, BackendOptions{}).LoadBoard(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// backlogIDs is a board's whole backlog (every team mixed) in the order it
// reads: the new rule on a schema-4 board, schema 3's on an older one.
func backlogIDs(b board.Board, schema3 bool) string {
	cards := append([]board.Card(nil), b.Cards...)
	if schema3 {
		cards = schema3Sorted(cards)
	}
	ix := board.NewIndex(board.Board{Cards: cards})
	var listed []board.Card
	for _, c := range cards {
		if ix.OnBacklog(c) {
			listed = append(listed, c)
		}
	}
	order := board.BacklogOrder
	if schema3 {
		order = board.Schema3BacklogOrder
	}
	var ids []string
	for _, c := range order(ix, listed) {
		ids = append(ids, c.ItemID)
	}
	return strings.Join(ids, " ")
}

// Q14 — SCHEMA 4 KEEPS EVERY ORDER, ONE COMMIT PER REPOSITORY, ONCE. The
// merged backlog reads as schema 3 read it (a mix of two repositories); a
// dry run writes nothing; the bump is one commit in each repository, marker
// and keys together; a second run writes nothing.
func TestMigrateOrderingKeepsTheBacklogInOneCommitPerRepository(t *testing.T) {
	ds := schema3Domains(t)
	want := backlogIDs(mergedBoard(t, ds), true)
	heads := []string{ds[0].Repo.Head().String(), ds[1].Repo.Head().String()}
	dry, err := MigrateOrdering(ds, true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.Migrated || dry.Total() == 0 || ds[0].Repo.Head().String() != heads[0] {
		t.Fatalf("dry run = %+v, heads moved: %v", dry, ds[0].Repo.Head().String() != heads[0])
	}
	report, err := MigrateOrdering(ds, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total() != dry.Total() {
		t.Fatalf("the run re-keyed %d files, the dry run said %d", report.Total(), dry.Total())
	}
	for i, d := range ds {
		if n := len(commitsBetweenHash(t, d.Repo, heads[i])); n != 1 {
			t.Fatalf("%s: %d commits, want 1", d.Name, n)
		}
		s, _ := Load(d.Repo)
		if s.Board.Schema != 4 {
			t.Fatalf("%s: schema %d", d.Name, s.Board.Schema)
		}
	}
	if got := backlogIDs(mergedBoard(t, ds), false); got != want {
		t.Fatalf("backlog\n was %s\n now %s", want, got)
	}
	again, err := MigrateOrdering(ds, false)
	if err != nil || again.Migrated {
		t.Fatalf("a second run = %+v, %v; want nothing", again, err)
	}
}

// Q14 — an INTERRUPTED run resumes to the same keys: the first repository
// bumped, the second not; the plan is computed from the pre-bump trees, so
// the resumed run writes the second exactly as an uninterrupted one would.
func TestMigrateOrderingResumesToTheSameKeys(t *testing.T) {
	whole := schema3Domains(t)
	if _, err := MigrateOrdering(whole, false); err != nil {
		t.Fatal(err)
	}
	// The interruption: the primary bumped by a run that planned over BOTH
	// repositories, and the process gone before the second.
	torn := schema3Domains(t)
	report, err := MigrateOrdering(torn, true)
	if err != nil {
		t.Fatal(err)
	}
	pre := mergedBoard(t, torn)
	changes, _, _ := board.Schema4Plan(pre)
	var primary []board.RankChange
	for _, ch := range changes {
		if c, ok := board.Find(pre, ch.ID); ok && c.Domain == "shared" {
			primary = append(primary, ch)
		}
	}
	if err := commitSchema4(torn[0], primary, runOf(torn)); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateOrdering(torn, false); err != nil {
		t.Fatal(err)
	}
	if report.Total() == 0 {
		t.Fatal("the fixture must need re-keying")
	}
	a, b := mergedBoard(t, whole), mergedBoard(t, torn)
	for _, c := range a.Cards {
		k, _ := board.Find(b, c.ItemID)
		if k.Rank != c.Rank {
			t.Fatalf("%s: %q uninterrupted, %q resumed", c.ItemID, c.Rank, k.Rank)
		}
	}
}

func commitsBetweenHash(t *testing.T, r *Repo, since string) []string {
	t.Helper()
	var out []string
	_ = r.Walk(r.Head(), func(c *object.Commit) (bool, error) {
		if c.Hash.String() == since {
			return false, nil
		}
		out = append(out, c.Hash.String())
		return true, nil
	})
	return out
}

// Q14 — THE REAL BOARD. Opt-in (AEMAN_ORDER_FIXTURE="aeman-db=/path/a.git,
// founders=/path/b.git", the primary first): each bare repository is cloned
// into memory — the copy is never written — brought to schema 3, and
// migrated. Every team's backlog and all teams mixed read as before; every
// order of Schema4Orders holds; ids and links are unchanged; a second run
// writes nothing. It prints the files re-keyed and the Team cells changed.
func TestSchema4PreservesEveryOrderOnARealBoard(t *testing.T) {
	spec := os.Getenv("AEMAN_ORDER_FIXTURE")
	if spec == "" {
		t.Skip("AEMAN_ORDER_FIXTURE names a copy of a real board's repositories")
	}
	var ds []Domain
	for _, part := range strings.Split(spec, ",") {
		name, path, _ := strings.Cut(part, "=")
		st := memory.NewStorage()
		if _, err := git.Clone(st, nil, &git.CloneOptions{URL: path, ReferenceName: "refs/heads/main", SingleBranch: true}); err != nil {
			t.Fatalf("clone %s: %v", path, err)
		}
		r := Open(st, Options{Committer: Identity{Name: "aeman", Email: "aeman@test"}})
		if _, err := MigrateSchema(r); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ds = append(ds, Domain{Name: name, Repo: r})
	}
	before := mergedBoard(t, ds)
	old := schema3Sorted(nonState(before.Cards))
	oldIx := board.NewIndex(board.Board{Cards: old, Customers: before.Customers})
	chains := board.Schema4Orders(oldIx, old)
	teams := map[string]bool{"": true}
	for _, c := range old {
		teams[oldIx.PlacedTeam(c)] = true
	}
	start := time.Now()
	report, err := MigrateOrdering(ds, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("migrated in %v\n%s", time.Since(start), report)
	after := mergedBoard(t, ds)
	pos := map[string]int{}
	for i, c := range after.Cards {
		pos[c.ItemID] = i
	}
	for _, chain := range chains {
		for i := 1; i < len(chain); i++ {
			if pos[chain[i-1]] > pos[chain[i]] {
				t.Fatalf("order lost: %s after %s", chain[i-1], chain[i])
			}
		}
	}
	newIx := board.NewIndex(after)
	for team := range teams {
		was := teamBacklog(oldIx, old, team, board.Schema3BacklogOrder)
		now := teamBacklog(newIx, after.Cards, team, board.BacklogOrder)
		if was != now {
			t.Fatalf("team %q backlog changed\n was %s\n now %s", team, was, now)
		}
	}
	if len(after.Cards) != len(before.Cards) {
		t.Fatalf("%d cards before, %d after", len(before.Cards), len(after.Cards))
	}
	for _, c := range before.Cards {
		k, ok := board.Find(after, c.ItemID)
		if !ok || k.Parent != c.Parent || k.ReviewOf != c.ReviewOf || strings.Join(k.DependsOn, ",") != strings.Join(c.DependsOn, ",") {
			t.Fatalf("%s: a link changed", c.ItemID)
		}
	}
	if again, err := MigrateOrdering(ds, false); err != nil || again.Migrated {
		t.Fatalf("a second run = %+v, %v", again, err)
	}
}

func nonState(cards []board.Card) []board.Card {
	var out []board.Card
	for _, c := range cards {
		if !board.IsStateTitle(c.Title) {
			out = append(out, c)
		}
	}
	return out
}

func teamBacklog(ix *board.Index, cards []board.Card, team string, order func(*board.Index, []board.Card) []board.Card) string {
	var listed []board.Card
	for _, c := range cards {
		if ix.OnBacklog(c) && ix.PlacedTeam(c) == team {
			listed = append(listed, c)
		}
	}
	var ids []string
	for _, c := range order(ix, listed) {
		ids = append(ids, c.ItemID)
	}
	return strings.Join(ids, " ")
}

// Q14 — A REPOSITORY THAT JOINS A BOARD ALREADY AT SCHEMA 4 is no resume of
// that board's migration: its cards are not re-planned by the old tree
// rules. Its parked tasks take their default place in the board as it
// stands (Q3 — after their epic's last parked task in the team's queue),
// their own order kept, and the board's own repository is not written.
func TestARepositoryJoiningASchema4BoardGetsTheQueueRules(t *testing.T) {
	card := func(files map[string]string, id, body string) {
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		files[p] = "---\n" + body + "created: 2026-08-20T09:00:00Z\n---\n"
	}
	primary := map[string]string{
		BoardPath:              "schema: 3\ntitle: b\n",
		TeamPath("01T_PORTAL"): "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
	}
	card(primary, "01EA000000000000000000000A", "title: A\ntype: epic\nrank: a\n")
	card(primary, "01EB000000000000000000000A", "title: B\ntype: epic\nrank: b\n")
	card(primary, "01A1000000000000000000000A", "title: A1\nteam: portal\nparked: true\nparent: 01EA000000000000000000000A\nrank: c\n")
	card(primary, "01B1000000000000000000000A", "title: B1\nteam: portal\nparked: true\nparent: 01EB000000000000000000000A\nrank: d\n")
	ds := []Domain{{Name: "shared", Repo: repoWith(t, primary)}}
	if _, err := MigrateOrdering(ds, false); err != nil {
		t.Fatal(err)
	}
	joining := map[string]string{BoardPath: "schema: 3\ntitle: c\n"}
	card(joining, "01B2000000000000000000000A", "title: B2\nteam: portal\nparked: true\nparent: 01EB000000000000000000000A\nrank: 1\n")
	card(joining, "01A2000000000000000000000A", "title: A2\nteam: portal\nparked: true\nparent: 01EA000000000000000000000A\nrank: 2\n")
	ds = append(ds, Domain{Name: "joined", Repo: repoWith(t, joining)})
	head := ds[0].Repo.Head()
	if _, err := MigrateOrdering(ds, false); err != nil {
		t.Fatal(err)
	}
	if ds[0].Repo.Head() != head {
		t.Fatal("the join wrote the board's own repository")
	}
	if s, _ := Load(ds[1].Repo); s.Board.Schema != 4 {
		t.Fatalf("the joining repository is at schema %d", s.Board.Schema)
	}
	b := mergedBoard(t, ds)
	var titles []string
	for _, c := range board.NewIndex(b).Queue("portal") {
		titles = append(titles, c.Title)
	}
	if got := strings.Join(titles, " "); got != "A1 A2 B1 B2" {
		t.Fatalf("portal's queue after the join = %s", got)
	}
	if again, err := MigrateOrdering(ds, false); err != nil || again.Migrated {
		t.Fatalf("a second run = %+v, %v", again, err)
	}
}
