package gitstore

import (
	"context"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// MultiBackend is the Backend over several domains: every read merges them,
// every write goes to the domain the card belongs to (G14), and a write that
// changes where a card belongs is a move — create in the new domain, then
// delete in the old, same id, one action id (G22).

var _ boardservice.Backend = (*MultiBackend)(nil)

// twoDomains is a shared domain with the team "portal" and the project
// "portal", and a closed domain with the project "secret" and its column.
func twoDomains(t *testing.T) (*MultiBackend, *Repo, *Repo) {
	t.Helper()
	shared := repoWith(t, map[string]string{
		BoardPath:                         "schema: 1\ntitle: b\n",
		TeamPath("_"):                     "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		TeamPath("01T_PORTAL"):            "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		ProjectPath("01P_PORTAL"):         "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		EpicPath("01P_PORTAL", "01E_BUG"): "name: Bugs\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md":           "---\ntitle: shared card\nteam: portal\nzone: yellow\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
	})
	closed := repoWith(t, map[string]string{
		ProjectPath("01P_SECRET"):          "name: secret\nrank: b\ncreated: 2026-06-02T08:00:00Z\n",
		EpicPath("01P_SECRET", "01E_RISK"): "name: Risk\nrank: a\ncreated: 2026-06-02T08:00:00Z\n",
		"cards/b/2/01CARDB2.md":            "---\ntitle: closed card\nteam: portal\nproject: secret\nepic: Risk\nrank: a\ncreated: 2026-08-21T09:00:00Z\n---\n",
	})
	clock := at("2026-08-28T09:00:00Z")
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}},
		BackendOptions{Now: func() time.Time { clock = clock.Add(time.Second); return clock }})
	return mb, shared, closed
}

func TestMultiBackendLoadBoardMergesDomains(t *testing.T) {
	mb, _, _ := twoDomains(t)
	b, err := mb.LoadBoard(context.Background(), "x", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Cards) != 2 || len(b.Projects) != 2 || len(b.Epics) != 2 || len(b.TeamOrder) != 2 {
		t.Fatalf("board = %d cards, %v projects, %d epics, %v teams", len(b.Cards), b.Projects, len(b.Epics), b.TeamOrder)
	}
	for _, c := range b.Cards {
		if c.Domain == "" {
			t.Fatalf("card %s has no domain", c.Title)
		}
	}
}

// G14 — a create lands where the rule says: a closed-project card in the
// closed repository, a team card in the team's, a review card with its
// original, a subtask with its parent.
func TestMultiBackendCreateInheritsDomain(t *testing.T) {
	mb, shared, closed := twoDomains(t)
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x", 1)

	inClosed, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "risk item", Team: "portal", Project: "secret", Epic: "Risk"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath(inClosed.ItemID)
	if _, err := closed.ReadFile(p); err != nil {
		t.Fatalf("closed-project card not in the closed repository: %v", err)
	}
	if _, err := shared.ReadFile(p); err == nil {
		t.Fatal("closed-project card leaked into the shared repository")
	}
	if inClosed.Domain != "closed" {
		t.Fatalf("returned card domain = %q", inClosed.Domain)
	}

	inShared, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "team item", Team: "portal"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = CardPath(inShared.ItemID)
	if _, err := shared.ReadFile(p); err != nil {
		t.Fatalf("team card not in the team's repository: %v", err)
	}

	// The review of a closed card carries the original's team and no
	// project — the team rule alone would leak it; it lands with the
	// original.
	b, _ = mb.LoadBoard(ctx, "x", 1)
	review, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "review: risk item", Team: "portal", ReviewOf: inClosed.ItemID, Assignee: "timur"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = CardPath(review.ItemID)
	if _, err := closed.ReadFile(p); err != nil {
		t.Fatalf("review card not with its original: %v", err)
	}
	sub, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "sub", Team: "portal", Parent: inClosed.ItemID, Project: "portal", Epic: "Bugs"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = CardPath(sub.ItemID)
	if _, err := closed.ReadFile(p); err != nil {
		t.Fatalf("subtask with its own column not with its parent: %v", err)
	}
}

// A setter writes the card's own domain and touches nothing elsewhere.
func TestMultiBackendSetterWritesOwnDomain(t *testing.T) {
	mb, shared, closed := twoDomains(t)
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x", 1)
	sharedHead, closedHead := shared.Head(), closed.Head()
	var closedCard board.Card
	for _, c := range b.Cards {
		if c.Title == "closed card" {
			closedCard = c
		}
	}
	if err := mb.SetProgress(ctx, b, closedCard, 60); err != nil {
		t.Fatal(err)
	}
	if shared.Head() != sharedHead {
		t.Fatal("a write to a closed card touched the shared repository")
	}
	if closed.Head() == closedHead {
		t.Fatal("the closed repository did not change")
	}
	got, _ := mb.LoadBoard(ctx, "x", 1)
	c, _ := findByID(got, closedCard.ItemID)
	if c.Progress != 60 || c.Domain != "closed" {
		t.Fatalf("card after write = %+v", c)
	}
}

// G22 — re-filing a card so that it belongs elsewhere is a move: the new
// copy is written first, with movedFrom, then the old one is deleted; the
// id stays; both commits share the action id.
func TestMultiBackendRefilingIsAMove(t *testing.T) {
	mb, shared, closed := twoDomains(t)
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x", 1)
	var card board.Card
	for _, c := range b.Cards {
		if c.Title == "shared card" {
			card = c
		}
	}
	ctx, flush := WithScope(ctx, Action{Name: "update", ID: "01JB4KA0M2P4R6T8V0X2Z4B6N1", Cards: []string{card.ItemID}})
	if err := mb.SetProject(ctx, b, card, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := mb.SetEpic(ctx, b, card, "Risk"); err != nil {
		t.Fatal(err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath(card.ItemID)
	data, err := closed.ReadFile(p)
	if err != nil {
		t.Fatalf("moved card not in the closed repository: %v", err)
	}
	if !containsAll(string(data), "project: secret", "epic: Risk", "movedFrom: shared", "movedAt: ") {
		t.Fatalf("moved copy:\n%s", data)
	}
	if _, err := shared.ReadFile(p); err == nil {
		t.Fatal("the old copy is still in the shared repository")
	}
	// Both sides committed under one action id; the destination first.
	cc, _ := closed.CommitObject(closed.Head())
	sc, _ := shared.CommitObject(shared.Head())
	if ParseTrailers(cc.Message).ActionID != "01JB4KA0M2P4R6T8V0X2Z4B6N1" || ParseTrailers(sc.Message).ActionID != "01JB4KA0M2P4R6T8V0X2Z4B6N1" {
		t.Fatalf("action ids: closed %q shared %q", ParseTrailers(cc.Message).ActionID, ParseTrailers(sc.Message).ActionID)
	}
	if cards := ParseTrailers(sc.Message).Cards; len(cards) != 1 || cards[0] != card.ItemID {
		t.Fatalf("delete commit names %v", cards)
	}
	// The create says where the card came from, the delete where it went —
	// the log walker follows the first into the old domain.
	if got := ParseTrailers(cc.Message).Extra["Aeman-Moved-From"]; got != "shared" {
		t.Fatalf("create commit Aeman-Moved-From = %q, want shared", got)
	}
	if got := ParseTrailers(sc.Message).Extra["Aeman-Moved-To"]; got != "closed" {
		t.Fatalf("delete commit Aeman-Moved-To = %q, want closed", got)
	}
	if cc.Committer.When.After(sc.Committer.When) {
		t.Fatalf("delete (%v) committed before create (%v)", sc.Committer.When, cc.Committer.When)
	}
	got, _ := mb.LoadBoard(ctx, "x", 1)
	moved, ok := findByID(got, card.ItemID)
	if !ok || moved.Domain != "closed" || moved.Project != "secret" || moved.MovedFrom != "shared" {
		t.Fatalf("board after move = %+v (found %v)", moved, ok)
	}
	if n := len(got.Cards); n != 2 {
		t.Fatalf("cards after move = %d, want 2 (no duplicate)", n)
	}
}

// G14 — a move cascades to what the linked-card rules tie to the card: its
// review card and its subtasks (and theirs) move with it, in the same
// action, each with movedFrom; a card linked to something else stays.
func TestMultiBackendMoveCascadesToLinkedCards(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:                         "schema: 1\ntitle: b\n",
		TeamPath("_"):                     "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		TeamPath("01T_PORTAL"):            "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
		ProjectPath("01P_PORTAL"):         "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		EpicPath("01P_PORTAL", "01E_BUG"): "name: Bugs\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md":           "---\ntitle: the work\nteam: portal\nrank: a\n---\n",
		"cards/r/1/01CARDR1.md":           "---\ntitle: review\nteam: portal\nreviewOf: 01CARDA1\nrank: b\n---\n",
		"cards/s/1/01CARDS1.md":           "---\ntitle: subtask with own column\nteam: portal\nproject: portal\nepic: Bugs\nparent: 01CARDA1\nrank: c\n---\n",
		"cards/s/2/01CARDS2.md":           "---\ntitle: sub-subtask\nteam: portal\nparent: 01CARDS1\nrank: d\n---\n",
		"cards/o/1/01CARDO1.md":           "---\ntitle: unrelated\nteam: portal\nrank: e\n---\n",
	})
	closed := repoWith(t, map[string]string{
		ProjectPath("01P_SECRET"): "name: secret\nrank: b\ncreated: 2026-06-02T08:00:00Z\n",
	})
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}}, BackendOptions{Now: func() time.Time { return at("2026-08-28T09:00:00Z") }})
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x", 1)
	work, _ := findByID(b, "01CARDA1")
	ctx, flush := WithScope(ctx, Action{Name: "update", ID: "01JB4KA0M2P4R6T8V0X2Z4B6N2", Cards: []string{"01CARDA1"}})
	if err := mb.SetProject(ctx, b, work, "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"01CARDA1", "01CARDR1", "01CARDS1", "01CARDS2"} {
		p, _ := CardPath(id)
		data, err := closed.ReadFile(p)
		if err != nil {
			t.Fatalf("%s did not follow into the closed repository: %v", id, err)
		}
		if !containsAll(string(data), "movedFrom: shared") {
			t.Fatalf("%s moved copy lacks movedFrom:\n%s", id, data)
		}
		if _, err := shared.ReadFile(p); err == nil {
			t.Fatalf("%s still in the shared repository", id)
		}
	}
	// The subtask's own column is a shared project, yet it followed its parent.
	if p, _ := CardPath("01CARDO1"); !fileExists(shared, p) {
		t.Fatal("an unrelated card was moved")
	}
	cc, _ := closed.CommitObject(closed.Head())
	sc, _ := shared.CommitObject(shared.Head())
	if len(ParseTrailers(cc.Message).Cards) != 4 || len(ParseTrailers(sc.Message).Cards) != 4 {
		t.Fatalf("commits name %v / %v, want all four cards in both", ParseTrailers(cc.Message).Cards, ParseTrailers(sc.Message).Cards)
	}
	if ParseTrailers(cc.Message).ActionID != ParseTrailers(sc.Message).ActionID {
		t.Fatal("cascade split across action ids")
	}
	got, _ := mb.LoadBoard(ctx, "x", 1)
	if n := len(got.Cards); n != 5 {
		t.Fatalf("cards after cascade = %d, want 5", n)
	}
	for _, id := range []string{"01CARDR1", "01CARDS1", "01CARDS2"} {
		c, _ := findByID(got, id)
		if c.Domain != "closed" {
			t.Fatalf("%s domain = %q after cascade", id, c.Domain)
		}
	}
}

func fileExists(r *Repo, p string) bool {
	_, err := r.ReadFile(p)
	return err == nil
}

// G22 — maintenance removes a torn move's ghost, but only once the
// destination has landed; a duplicate that is not a move (neither copy says
// movedFrom) is a maintainer's problem and is never touched.
func TestMultiBackendSweepGhosts(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:               "schema: 1\ntitle: b\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: portal\nrank: a\n---\n",
		"cards/d/1/01CARDD1.md": "---\ntitle: dup\nteam: portal\nrank: b\n---\n",
	})
	closed := repoWith(t, map[string]string{
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nproject: secret\nrank: a\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
		"cards/d/1/01CARDD1.md": "---\ntitle: dup\nteam: portal\nrank: b\n---\n",
	})
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}}, BackendOptions{Now: func() time.Time { return at("2026-08-28T11:00:00Z") }})
	ctx := context.Background()
	before := shared.Head()
	n, err := mb.SweepGhosts(ctx, func(string) bool { return false })
	if err != nil || n != 0 || shared.Head() != before {
		t.Fatalf("destination not landed: swept %d (%v), head moved %v", n, err, shared.Head() != before)
	}
	n, err = mb.SweepGhosts(ctx, func(d string) bool { return d == "closed" })
	if err != nil || n != 1 {
		t.Fatalf("swept %d (%v), want 1", n, err)
	}
	if p, _ := CardPath("01CARDA1"); fileExists(shared, p) {
		t.Fatal("ghost still in the shared repository")
	}
	if p, _ := CardPath("01CARDD1"); !fileExists(shared, p) || !fileExists(closed, p) {
		t.Fatal("a plain duplicate was swept")
	}
	c, _ := shared.CommitObject(shared.Head())
	if tr := ParseTrailers(c.Message); tr.Action != "maintenance" || len(tr.Cards) != 1 || tr.Cards[0] != "01CARDA1" {
		t.Fatalf("sweep commit trailers = %+v", tr)
	}
	// Idempotent: nothing left to sweep.
	if n, _ := mb.SweepGhosts(ctx, func(string) bool { return true }); n != 0 {
		t.Fatalf("second sweep removed %d", n)
	}
}

// Roster entries are declared in the primary unless the create names a
// domain; a team declared in the closed domain keeps its sprint pointer
// there.
func TestMultiBackendRosterDomainChoice(t *testing.T) {
	mb, shared, closed := twoDomains(t)
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x", 1)
	proj, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProjectStateTitle, Project: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shared.ReadFile(ProjectPath(proj.ItemID)); err != nil {
		t.Fatalf("project without a domain choice must land in the primary: %v", err)
	}
	proj2, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProjectStateTitle, Project: "vault", Domain: "closed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ReadFile(ProjectPath(proj2.ItemID)); err != nil {
		t.Fatalf("project with a domain choice must land there: %v", err)
	}
	if _, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProjectStateTitle, Project: "x", Domain: "nope"}); err == nil {
		t.Fatal("an unknown domain must be refused")
	}
	// A team in the closed domain, and its pointer written there.
	if err := mb.SetSprintState(ctx, b, "ops", "2026-08-31", ""); err != nil {
		t.Fatal(err)
	}
	b, _ = mb.LoadBoard(ctx, "x", 1)
	if _, ok := b.SprintStates["ops"]; !ok {
		t.Fatal("new team missing")
	}
	// A card of the closed project whose team lives in shared is still a
	// closed card (project before team) — and the team's pointer is read for
	// it all the same: the server reads every domain.
	if err := mb.SetSprintState(ctx, b, "portal", "2026-08-31", "2026-08-24"); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ReadFile(TeamPath("01T_PORTAL")); err == nil {
		t.Fatal("portal's pointer must be written in shared, where the team is declared")
	}
	data, _ := shared.ReadFile(TeamPath("01T_PORTAL"))
	if !containsAll(string(data), "current: 2026-08-31", "previous: 2026-08-24") {
		t.Fatalf("team file:\n%s", data)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains1(s, p) {
			return false
		}
	}
	return true
}

func contains1(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
