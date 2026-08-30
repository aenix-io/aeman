package gitstore

import (
	"context"
	"errors"
	"strings"
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
	b, err := mb.LoadBoard(context.Background(), "x")
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
	b, _ := mb.LoadBoard(ctx, "x")

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
	b, _ = mb.LoadBoard(ctx, "x")
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
	b, _ := mb.LoadBoard(ctx, "x")
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
	got, _ := mb.LoadBoard(ctx, "x")
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
	b, _ := mb.LoadBoard(ctx, "x")
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
	got, _ := mb.LoadBoard(ctx, "x")
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
	b, _ := mb.LoadBoard(ctx, "x")
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
	got, _ := mb.LoadBoard(ctx, "x")
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

// The domain a new team, project or process is declared in is the caller's
// choice, carried on the context (board.WithDomain) when the input does not
// name one: a service that builds the stub itself need not know about
// domains. Unknown domains are refused; cards never take one.
func TestMultiBackendCreateHonoursDomainFromContext(t *testing.T) {
	mb, shared, closed := twoDomains(t)
	ctx := board.WithDomain(ctxAs("kvaps"), "closed")
	b, _ := mb.LoadBoard(ctx, "x")
	proj, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProjectStateTitle, Project: "vault"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ReadFile(ProjectPath(proj.ItemID)); err != nil {
		t.Fatalf("project not declared in the chosen domain: %v", err)
	}
	proc, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProcessStateTitle, Process: "audit"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ReadFile(ProcessPath(proc.ItemID)); err != nil {
		t.Fatalf("process not declared in the chosen domain: %v", err)
	}
	if err := mb.SetSprintState(ctx, b, "ops", "2026-08-31", ""); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadAll(mb.domains)
	for _, tm := range s.Teams {
		if tm.Name == "ops" && tm.Domain != "closed" {
			t.Fatalf("new team declared in %q, want the chosen domain", tm.Domain)
		}
	}
	// A card ignores the choice: its domain follows the rule.
	card, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "team item", Team: "portal"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath(card.ItemID)
	if _, err := shared.ReadFile(p); err != nil {
		t.Fatalf("a card must follow the rule, not the context choice: %v", err)
	}
	// An existing team's pointer stays where the team is, whatever the context says.
	if err := mb.SetSprintState(ctx, b, "portal", "2026-08-31", "2026-08-24"); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ReadFile(TeamPath("01T_PORTAL")); err == nil {
		t.Fatal("an existing team must not be re-declared in the chosen domain")
	}
	if _, err := mb.CreateCard(board.WithDomain(ctxAs("kvaps"), "nope"), b, board.CreateInput{Title: board.ProjectStateTitle, Project: "x"}); err == nil {
		t.Fatal("an unknown domain must be refused")
	}
}

// The card's log is its commits — in the domain it lives in and, following
// Aeman-Moved-From, in the domain it came from (G22): one continuous feed,
// newest first, each field change an event with the commit's actor and time.
func TestMultiBackendCardLogFollowsMoveIntoOldDomain(t *testing.T) {
	mb, _, _ := twoDomains(t)
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x")
	card, _ := findByID(b, "01CARDA1")
	if err := mb.SetProgress(ctx, b, card, 30); err != nil {
		t.Fatal(err)
	}
	sctx, flush := WithScope(ctx, Action{Name: "update", ID: "01JB4KA0M2P4R6T8V0X2Z4B6N3", Cards: []string{card.ItemID}})
	if err := mb.SetProject(sctx, b, card, "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	b, _ = mb.LoadBoard(ctx, "x")
	moved, _ := findByID(b, card.ItemID)
	if err := mb.SetProgress(board.WithActor(context.Background(), "timur"), b, moved, 60); err != nil {
		t.Fatal(err)
	}
	events, truncated, err := mb.CardLog(ctx, b, card.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated.IsZero() {
		t.Fatalf("full clones report a horizon: %v", truncated)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind+":"+e.From+">"+e.To+"@"+e.Actor)
	}
	joined := strings.Join(kinds, " ")
	for _, want := range []string{"progress:30>60@timur", "project:>secret@kvaps", "progress:>30@kvaps"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("log lacks %q:\n%s", want, joined)
		}
	}
	// Newest first, and the old domain's entries come after the move.
	if !strings.HasPrefix(joined, "progress:30>60@timur") || strings.Index(joined, "project:>secret") > strings.Index(joined, "progress:>30@kvaps") {
		t.Fatalf("order:\n%s", joined)
	}
	for _, e := range events {
		if e.At == "" || e.ID == "" {
			t.Fatalf("event without time or id: %+v", e)
		}
	}
}

// Health learns what the merge had to resolve: duplicate roster names (G13)
// and torn-move ghosts (G22), as of the last load.
func TestMultiBackendIssuesReportAliasesAndGhosts(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:               "schema: 1\ntitle: b\n",
		TeamPath("01T_NEWER"):   "name: portal\nrank: a\ncreated: 2026-07-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: portal\nrank: a\n---\n",
	})
	closed := repoWith(t, map[string]string{
		TeamPath("01T_OLDER"):   "name: portal\nrank: z\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nproject: secret\nrank: a\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
	})
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}}, BackendOptions{})
	if aliases, ghosts := mb.Issues(); aliases != nil || ghosts != nil {
		t.Fatalf("issues before any load = %v / %v, want none yet", aliases, ghosts)
	}
	if _, err := mb.LoadBoard(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	aliases, ghosts := mb.Issues()
	if len(aliases) != 1 || aliases[0].Kind != "team" || aliases[0].Name != "portal" || aliases[0].Domain != "shared" {
		t.Fatalf("aliases = %+v", aliases)
	}
	if len(ghosts) != 1 || ghosts[0].ID != "01CARDA1" || ghosts[0].Domain != "shared" || ghosts[0].Current != "closed" {
		t.Fatalf("ghosts = %+v", ghosts)
	}
}

// LoadCards resolves a torn move like LoadAll does: the moved copy, not the
// first domain's.
func TestMultiBackendLoadCardsPrefersMovedCopy(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:               "schema: 1\ntitle: b\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: portal\nrank: a\n---\n",
	})
	closed := repoWith(t, map[string]string{
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nproject: secret\nrank: a\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
	})
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}}, BackendOptions{})
	got, err := mb.LoadCards(context.Background(), board.Board{}, []string{"01CARDA1"})
	if err != nil || len(got) != 1 || got[0].Domain != "closed" || got[0].Project != "secret" {
		t.Fatalf("LoadCards = %+v (%v), want the closed copy once", got, err)
	}
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
	b, _ := mb.LoadBoard(ctx, "x")
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
	b, _ = mb.LoadBoard(ctx, "x")
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

// A column's STUB never changes repository: refile hands a stub straight
// back to the backend that holds it (isStub). So a column cannot follow a
// project into another repository — the service refuses that move rather
// than produce a column declared in one repository and owned by a project
// of another, with every card in it stranded (G57).
func TestAnEpicStubStaysInItsRepositoryWhenItsProjectChanges(t *testing.T) {
	mb, _, _ := twoDomains(t)
	ctx := context.Background()
	b, err := mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	// The primary repository's column, moved to the CLOSED repository's
	// project: the write must land where the stub already is.
	var stub board.Card
	for _, e := range b.Epics {
		if e.Project == "portal" {
			stub = board.Card{ItemID: e.ItemID, Title: board.EpicStateTitle, Epic: e.Name, Project: e.Project}
			break
		}
	}
	if stub.ItemID == "" {
		t.Fatalf("the fixture must declare a column of portal: %+v", b.Epics)
	}
	was := b.Domains[stub.ItemID]
	// The write goes to the backend that HOLDS the stub, so the target
	// project is looked for there — and a project of the other repository
	// is simply not there. The store cannot perform this move at all.
	if err := mb.SetProject(ctx, b, stub, "secret"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("a stub is written where it already lives: %v", err)
	}
	after, err := mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Domains[stub.ItemID]; got != was {
		t.Fatalf("and it stays there: %q → %q", was, got)
	}
	// Which is why boardservice refuses the move up front, with a sentence
	// about repositories instead of a confusing "project not found".
	if d := board.ProjectDomain(after, "secret"); d == was {
		t.Fatalf("the fixture's two projects must live apart: %q", d)
	}
}

// A mirror the service accepts must survive the next LOAD — the store is
// the only place where the primary's name is known, so a board assembled
// without it judges a stamped column against an unstamped placement and
// drops exactly the placements it had just written. Both halves of the
// rule the branch introduced, over the real MultiBackend rather than a
// fixture that models the primary as "".
func TestAMirrorWrittenThroughTheStoreSurvivesTheNextLoad(t *testing.T) {
	// Two columns in the SHARED repository — a project's and the no-project
	// bucket, which is a lawful mirror home (G15) — and one in the closed
	// repository, which is not.
	shared := repoWith(t, map[string]string{
		BoardPath:                         "schema: 1\ntitle: b\n",
		TeamPath("01T_PORTAL"):            "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		ProjectPath("01P_PORTAL"):         "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		EpicPath("01P_PORTAL", "01E_BUG"): "name: Bugs\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		ProjectPath("_"):                  "rank: b\ncreated: 2026-06-01T08:00:00Z\n",
		EpicPath("_", "01E_CHORE"):        "name: Chores\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
	})
	closed := repoWith(t, map[string]string{
		ProjectPath("01P_SECRET"):          "name: secret\nrank: c\ncreated: 2026-06-02T08:00:00Z\n",
		EpicPath("01P_SECRET", "01E_RISK"): "name: Risk\nrank: a\ncreated: 2026-06-02T08:00:00Z\n",
	})
	clock := at("2026-08-28T09:00:00Z")
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}},
		BackendOptions{Now: func() time.Time { clock = clock.Add(time.Second); return clock }})
	ctx := ctxAs("kvaps")
	b, err := mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if b.Primary != "shared" {
		t.Fatalf("the board must name its primary repository: %q", b.Primary)
	}
	card, err := mb.CreateCard(ctx, b, board.CreateInput{
		Title: "shared work", Team: "portal", Project: "portal", Epic: "Bugs"})
	if err != nil {
		t.Fatal(err)
	}
	if b, err = mb.LoadBoard(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	// The bucket column of the same repository: one file, two columns.
	if err := mb.SetMirrors(ctx, b, card, []board.Placement{{Epic: "Chores"}}); err != nil {
		t.Fatal(err)
	}
	after, err := mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := findByID(after, card.ItemID)
	if !ok {
		t.Fatal("the card must come back from the store")
	}
	if len(stored.Mirrors) != 1 || stored.Mirrors[0] != (board.Placement{Epic: "Chores"}) {
		t.Fatalf("the mirror must survive the load: %+v", stored.Mirrors)
	}
	// And the column of ANOTHER repository must not: the file that holds
	// the card is not in it, so the placement names a column its readers
	// may not have (G15). The store drops it on the way back, whatever a
	// direct writer put in the file.
	if err := mb.SetMirrors(ctx, b, stored, []board.Placement{{Project: "secret", Epic: "Risk"}}); err != nil {
		t.Fatal(err)
	}
	// It IS on disk — the store writes what it is told, and a direct writer
	// could have put it there just the same. The drop is on the way back.
	cp, _ := CardPath(card.ItemID)
	data, err := shared.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Risk") {
		t.Fatalf("the placement was not written at all:\n%s", data)
	}
	after, err = mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	stored, ok = findByID(after, card.ItemID)
	if !ok {
		t.Fatal("the card is still there")
	}
	if len(stored.Mirrors) != 0 {
		t.Fatalf("a cross-repository placement is not one this card can have: %+v", stored.Mirrors)
	}
}

// The grid's × on a subtask standing in a column of its PARENT's
// repository, against the real store: the pull-out moves the card's FILE
// to the repository its own team names, the column cannot come with it and
// is repaired away, and what is left is answered like any other columnless
// card — demoted, not left alive in no column, no sprint and no plan. The
// service tests prove the rule; this proves it survives the move, which is
// the part a fake cannot show.
func TestTheGridRemoveOnASubtaskAcrossRepositories(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:              "schema: 1\ntitle: b\n",
		TeamPath("01T_PORTAL"): "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-17\n",
	})
	closed := repoWith(t, map[string]string{
		ProjectPath("01P_SECRET"):          "name: secret\nrank: a\ncreated: 2026-06-02T08:00:00Z\n",
		EpicPath("01P_SECRET", "01E_RISK"): "name: Risk\nrank: a\ncreated: 2026-06-02T08:00:00Z\n",
		// The no-project bucket of the CLOSED repository: a column that
		// places no card by a project, so a card standing in it is placed
		// by its own team — in the other repository.
		ProjectPath("_"):           "rank: b\ncreated: 2026-06-02T08:00:00Z\n",
		EpicPath("_", "01E_CHORE"): "name: Chores\nrank: a\ncreated: 2026-06-02T08:00:00Z\n",
		"cards/t/1/01PARENT1.md":   "---\ntitle: closed work\nteam: portal\nproject: secret\nepic: Risk\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
		"cards/0/1/01KID00001.md":  "---\ntitle: the child\nteam: portal\nparent: 01PARENT1\nepic: Chores\nstart: 2026-08-20\nsprint: 2026-08-24\nrank: b\ncreated: 2026-08-20T09:00:00Z\n---\n",
	})
	clock := at("2026-08-28T09:00:00Z")
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}},
		BackendOptions{Now: func() time.Time { clock = clock.Add(time.Second); return clock }})
	ctx, flush := WithScope(ctxAs("kvaps"), Action{Name: "remove", ID: "01JB4KA0M2P4R6T8V0X2Z4B6N2", Cards: []string{"01KID00001"}})
	if err := boardservice.New(mb).Remove(ctx, "acme", "01KID00001", "grid"); err != nil {
		t.Fatalf("the × must complete: %v", err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath("01KID00001")
	if _, err := shared.ReadFile(p); err != nil {
		t.Fatalf("the file follows the card out of the group: %v", err)
	}
	if _, err := closed.ReadFile(p); err == nil {
		t.Fatal("and does not stay behind in the parent's repository")
	}
	b, err := mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	kid, ok := findByID(b, "01KID00001")
	if !ok {
		t.Fatal("the card is kept: portal has a previous sprint to demote into")
	}
	if kid.Parent != "" || kid.Epic != "" {
		t.Fatalf("out of the group, and out of a column its repository does not hold: %+v", kid)
	}
	if kid.SprintStart != "2026-08-17" {
		t.Fatalf("demoted like any other columnless card, not left nowhere: %+v", kid)
	}
}

// A create that names a PARENT groups the card it has just written, inside
// the one scope the whole request commits in. The card's file is staged
// there and a bare store cannot see it until the scope flushes, so a
// re-read at that moment fails — and the create's own undo then deletes
// the card the caller asked for. The server never saw it (its cache
// answers the load); an embedder, whom pkg/boardservice is published for,
// sees nothing else.
func TestCreatingASubtaskInsideOneScope(t *testing.T) {
	mb, shared, _ := twoDomains(t)
	ctx, flush := WithScope(ctxAs("kvaps"), Action{Name: "create", ID: "01JB4KA0M2P4R6T8V0X2Z4B6N3"})
	svc := boardservice.New(mb)
	b, err := svc.Board(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	var parent board.Card
	for _, c := range b.Cards {
		if c.Title == "shared card" {
			parent = c
		}
	}
	if parent.ItemID == "" {
		t.Fatal("the fixture's card is the parent")
	}
	kid, err := svc.CreateCard(ctx, "acme", boardservice.CreateCardArgs{
		Title: "child", Team: "portal", Parent: parent.ItemID,
	})
	if err != nil {
		t.Fatalf("a create that groups must not fail on its own staged write: %v", err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	after, err := mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := findByID(after, kid.ItemID)
	if !ok {
		t.Fatal("the card the caller was handed must exist")
	}
	if got.Parent != parent.ItemID {
		t.Fatalf("and be grouped under the parent it named: %+v", got)
	}
	p, _ := CardPath(kid.ItemID)
	if _, err := shared.ReadFile(p); err != nil {
		t.Fatalf("with its file where the parent's is: %v", err)
	}
}

// Re-assigning a process task inside ONE scope moves the turn that is
// already running: "who does it" has to take effect on the card in front
// of the person, not only on next month's. The routing used to re-read the
// task it had just written — staged, so a bare store answered with the OLD
// owner, every open turn matched, and the whole re-route silently did
// nothing for an embedder.
func TestReRoutingATasksTurnInsideOneScope(t *testing.T) {
	mb, _, _ := twoDomains(t)
	svc := boardservice.New(mb)
	week := board.MondayOf(board.TodayIso())
	// The process and the task are requests of their own, as they are in
	// the product — one commit each.
	plain := ctxAs("kvaps")
	if err := svc.AddProcess(plain, "acme", "Invoicing", ""); err != nil {
		t.Fatal(err)
	}
	task, err := svc.AddProcessTask(plain, "acme", "Invoicing", boardservice.TaskArgs{
		Title: "Invoice ACME", Recurrence: "week", Start: week, Team: "portal", Assignee: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The edit is the request under test, in the scope its commit is made
	// in: the routing that follows it reads the task this scope has just
	// written.
	ctx, flush := WithScope(plain, Action{Name: "process", ID: "01JB4KA0M2P4R6T8V0X2Z4B6N4"})
	// The task is due this week, so it filed its turn at once — on alice.
	// Not asserted here: inside the scope that write is staged, and a plain
	// load reads the repository as it was. The end state says both things.
	bob := "bob"
	if err := svc.UpdateProcessTask(ctx, "acme", task.ItemID,
		boardservice.TaskPatch{Assignee: &bob}); err != nil {
		t.Fatal(err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	after, err := mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	its := board.Iterations(after, task.ItemID)
	if len(its) != 1 {
		// 0 means the task's create never filed its turn (the spawn read a
		// task that was not there yet); 2 means the old one was left behind.
		t.Fatalf("the task filed one turn and it was re-routed, not doubled: %d", len(its))
	}
	if len(its[0].Assignees) != 1 || its[0].Assignees[0] != "bob" {
		t.Fatalf("the running turn follows the task's new owner: %+v", its[0].Assignees)
	}
}
