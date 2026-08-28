package gitstore

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A board is an ordered list of domains — repositories — and a visitor's
// board is the union of the ones they can read. LoadAll merges the domains'
// snapshots: rosters are fragments merged on read with their rank keys,
// duplicate names resolve to the oldest declaration, every card and roster
// entry knows its domain, and a card caught mid-move shows once.

func repoWith(t *testing.T, files map[string]string) *Repo {
	t.Helper()
	r := newRepo(t)
	var ws []FileWrite
	for p, d := range files {
		ws = append(ws, FileWrite{Path: p, Data: []byte(d)})
	}
	if _, err := r.Commit(Action{Name: "import", Summary: "seed"}, ws); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLoadAllMergesRostersInRankOrder(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:                 "schema: 1\ntitle: the board\n",
		TeamPath("_"):             "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		TeamPath("01T_PORTAL"):    "name: portal\nrank: c\ncreated: 2026-06-01T08:00:00Z\n",
		ProjectPath("01P_PORTAL"): "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md":   "---\ntitle: shared one\nteam: portal\nrank: b\n---\n",
	})
	closed := repoWith(t, map[string]string{
		BoardPath:                       "schema: 1\ntitle: IGNORED — only the primary names the board\n",
		TeamPath("01T_OPS"):             "name: ops\nrank: b\ncreated: 2026-06-02T08:00:00Z\n",
		ProjectPath("01P_SECRET"):       "name: secret\nrank: b\ncreated: 2026-06-02T08:00:00Z\n",
		EpicPath("01P_SECRET", "01E_X"): "name: X\nrank: a\ncreated: 2026-06-02T08:00:00Z\n",
		"cards/a/2/01CARDA2.md":         "---\ntitle: closed one\nproject: secret\nepic: X\nrank: a\n---\n",
	})
	s, err := LoadAll([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Board.Title != "the board" {
		t.Fatalf("board title = %q, want the primary's", s.Board.Title)
	}
	if got := teamNames(s); got != ",ops,portal" {
		t.Fatalf("teams = %q, want the two domains interleaved by rank", got)
	}
	if s.Teams[1].Domain != "closed" || s.Teams[2].Domain != "shared" || s.Teams[0].Domain != "shared" {
		t.Fatalf("team domains = %s %s %s", s.Teams[0].Domain, s.Teams[1].Domain, s.Teams[2].Domain)
	}
	if len(s.Projects) != 2 || s.Projects[0].Name != "portal" || s.Projects[1].Name != "secret" || s.Projects[1].Domain != "closed" || len(s.Projects[1].Epics) != 1 {
		t.Fatalf("projects = %+v", s.Projects)
	}
	if len(s.Cards) != 2 || s.Cards[0].Title != "closed one" || s.Cards[0].Domain != "closed" || s.Cards[1].Domain != "shared" {
		t.Fatalf("cards = %+v", s.Cards)
	}
}

func teamNames(s Snapshot) string {
	names := make([]string, 0, len(s.Teams))
	for _, tm := range s.Teams {
		names = append(names, tm.Name)
	}
	return strings.Join(names, ",")
}

// G13 — two fragments declaring the same team: the OLDEST declaration is the
// team (its rank, its sprint pointer, its id); the other is an alias whose
// cards still count. Health learns about it; nothing is renamed or dropped.
func TestLoadAllDuplicateNamesResolveToOldest(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:               "schema: 1\ntitle: b\n",
		TeamPath("01T_NEWER"):   "name: portal\nrank: a\ncreated: 2026-07-01T08:00:00Z\nsprint:\n  current: 2026-08-31\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: in shared\nteam: portal\nrank: a\n---\n",
	})
	closed := repoWith(t, map[string]string{
		TeamPath("01T_OLDER"):   "name: portal\nrank: z\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		"cards/a/2/01CARDA2.md": "---\ntitle: in closed\nteam: portal\nrank: b\n---\n",
	})
	s, err := LoadAll([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Teams) != 1 || s.Teams[0].ID != "01T_OLDER" || s.Teams[0].Rank != "z" || s.Teams[0].Sprint.Current != "2026-08-24" || s.Teams[0].Domain != "closed" {
		t.Fatalf("teams = %+v, want the older declaration only", s.Teams)
	}
	if len(s.Aliases) != 1 || s.Aliases[0].Kind != "team" || s.Aliases[0].Name != "portal" || s.Aliases[0].Domain != "shared" || s.Aliases[0].ID != "01T_NEWER" || s.Aliases[0].Winner != "01T_OLDER" {
		t.Fatalf("aliases = %+v", s.Aliases)
	}
	if len(s.Cards) != 2 {
		t.Fatalf("cards = %d, both fragments' cards must count", len(s.Cards))
	}
}

// The no-team group belongs to the primary: a `_` fragment in another
// domain — what `aeman init` writes for every repository — is ignored, not
// an alias, however old it is.
func TestLoadAllNoTeamFragmentOutsidePrimaryIgnored(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:     "schema: 1\ntitle: b\n",
		TeamPath("_"): "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
	})
	closed := repoWith(t, map[string]string{
		BoardPath:     "schema: 1\ntitle: closed\n",
		TeamPath("_"): "rank: a\ncreated: 2026-05-01T08:00:00Z\n",
	})
	s, err := LoadAll([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Teams) != 1 || s.Teams[0].Name != "" || s.Teams[0].Domain != "shared" {
		t.Fatalf("teams = %+v, want the primary's no-team group only", s.Teams)
	}
	if len(s.Aliases) != 0 {
		t.Fatalf("aliases = %+v, want none for a `_` fragment", s.Aliases)
	}
}

// An alias project's columns and deadline lines show as the winner's.
func TestLoadAllAliasProjectMergesColumns(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:                       "schema: 1\ntitle: b\n",
		ProjectPath("01P_OLD"):          "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		EpicPath("01P_OLD", "01E_BUGS"): "name: Bugs\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
	})
	closed := repoWith(t, map[string]string{
		ProjectPath("01P_NEW"):           "name: portal\nrank: b\ncreated: 2026-07-01T08:00:00Z\n",
		EpicPath("01P_NEW", "01E_DOCS"):  "name: Docs\nrank: b\ncreated: 2026-07-01T08:00:00Z\n",
		DeadlinePath("01P_NEW", "01D_1"): "week: 2026-09-07\ncreated: 2026-07-01T08:00:00Z\n",
	})
	s, err := LoadAll([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Projects) != 1 || s.Projects[0].ID != "01P_OLD" {
		t.Fatalf("projects = %+v", s.Projects)
	}
	p := s.Projects[0]
	if len(p.Epics) != 2 || p.Epics[0].Name != "Bugs" || p.Epics[1].Name != "Docs" || p.Epics[1].Domain != "closed" {
		t.Fatalf("epics = %+v", p.Epics)
	}
	if len(p.Deadlines) != 1 || p.Deadlines[0].Domain != "closed" {
		t.Fatalf("deadlines = %+v", p.Deadlines)
	}
}

// G22 — a card present in two domains mid-move shows once: the copy whose
// movedFrom names the other domain is current, the other is a ghost.
func TestLoadAllHidesTornMoveGhost(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:               "schema: 1\ntitle: b\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: portal\nrank: a\n---\n",
	})
	closed := repoWith(t, map[string]string{
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nproject: secret\nrank: a\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
	})
	s, err := LoadAll([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Cards) != 1 || s.Cards[0].Domain != "closed" || s.Cards[0].Project != "secret" {
		t.Fatalf("cards = %+v, want the moved copy only", s.Cards)
	}
	if len(s.Ghosts) != 1 || s.Ghosts[0].Domain != "shared" || s.Ghosts[0].ID != "01CARDA1" || s.Ghosts[0].Current != "closed" {
		t.Fatalf("ghosts = %+v, want the shared copy, current in closed", s.Ghosts)
	}
}

// A card in two domains where neither copy says it moved is a duplicate,
// not a torn move: the primary side is served, the other is reported as a
// ghost with no current domain — maintenance must leave it alone.
func TestLoadAllPlainDuplicateIsNotAMove(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:               "schema: 1\ntitle: b\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: dup\nteam: portal\nrank: a\n---\n",
	})
	closed := repoWith(t, map[string]string{
		"cards/a/1/01CARDA1.md": "---\ntitle: dup\nproject: secret\nrank: a\n---\n",
	})
	s, err := LoadAll([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Cards) != 1 || s.Cards[0].Domain != "shared" {
		t.Fatalf("cards = %+v, want the primary's copy", s.Cards)
	}
	if len(s.Ghosts) != 1 || s.Ghosts[0].Domain != "closed" || s.Ghosts[0].Current != "" {
		t.Fatalf("ghosts = %+v, want the closed copy with no current domain", s.Ghosts)
	}
}

// The primary is required — it names the board — and LoadAll over one
// domain is Load.
func TestLoadAllPrimaryRequired(t *testing.T) {
	if _, err := LoadAll(nil); err == nil {
		t.Fatal("no domains must be an error")
	}
	r := newRepo(t)
	seedBoard(t, r)
	one, err := LoadAll([]Domain{{Name: "board", Repo: r}})
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := Load(r)
	if len(one.Cards) != len(plain.Cards) || len(one.Teams) != len(plain.Teams) || one.Cards[0].Domain != "board" {
		t.Fatalf("single-domain LoadAll differs from Load: %d/%d cards", len(one.Cards), len(plain.Cards))
	}
	_ = board.Card{}
}
