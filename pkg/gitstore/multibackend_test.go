package gitstore

import (
	"context"
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

// twoDomains is a shared domain with the team "portal", and a closed domain
// with the team "secret"; each holds one card of its own team.
func twoDomains(t *testing.T) (*MultiBackend, *Repo, *Repo) {
	t.Helper()
	shared := repoWith(t, map[string]string{
		BoardPath:               "schema: 2\ntitle: b\n",
		TeamPath("_"):           "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		TeamPath("01T_PORTAL"):  "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: shared card\nteam: portal\nzone: yellow\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
	})
	closed := repoWith(t, map[string]string{
		TeamPath("01T_SECRET"):  "name: secret\nrank: c\ncreated: 2026-06-02T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		"cards/b/2/01CARDB2.md": "---\ntitle: closed card\nteam: secret\nrank: a\ncreated: 2026-08-21T09:00:00Z\n---\n",
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
	if len(b.Cards) != 2 || len(b.TeamOrder) != 3 {
		t.Fatalf("board = %d cards, %v teams", len(b.Cards), b.TeamOrder)
	}
	for _, c := range b.Cards {
		if c.Domain == "" {
			t.Fatalf("card %s has no domain", c.Title)
		}
	}
}

// G14 — a create lands where the rule says: a card of the closed team in
// the closed repository, a team card in the team's, a review card with its
// original, a subtask with its parent.
func TestMultiBackendCreateInheritsDomain(t *testing.T) {
	mb, shared, closed := twoDomains(t)
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x")

	inClosed, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "risk item", Team: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath(inClosed.ItemID)
	if _, err := closed.ReadFile(p); err != nil {
		t.Fatalf("closed-team card not in the closed repository: %v", err)
	}
	if _, err := shared.ReadFile(p); err == nil {
		t.Fatal("closed-team card leaked into the shared repository")
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

	// The review of a closed card may carry a shared team — the team rule
	// alone would leak it; it lands with the original.
	b, _ = mb.LoadBoard(ctx, "x")
	review, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "review: risk item", Team: "portal", ReviewOf: inClosed.ItemID, Assignee: "timur"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = CardPath(review.ItemID)
	if _, err := closed.ReadFile(p); err != nil {
		t.Fatalf("review card not with its original: %v", err)
	}
	sub, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "sub", Team: "portal", Parent: inClosed.ItemID})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = CardPath(sub.ItemID)
	if _, err := closed.ReadFile(p); err != nil {
		t.Fatalf("subtask with a team of its own not with its parent: %v", err)
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
	if err := mb.SetTeam(ctx, b, card, "secret"); err != nil {
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
	if !containsAll(string(data), "team: secret", "movedFrom: shared", "movedAt: ") {
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
	if !ok || moved.Domain != "closed" || moved.Team != "secret" || moved.MovedFrom != "shared" {
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
		BoardPath:               "schema: 2\ntitle: b\n",
		TeamPath("_"):           "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		TeamPath("01T_PORTAL"):  "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: the work\nteam: portal\nrank: a\n---\n",
		"cards/r/1/01CARDR1.md": "---\ntitle: review\nteam: portal\nreviewOf: 01CARDA1\nrank: b\n---\n",
		"cards/s/1/01CARDS1.md": "---\ntitle: subtask with a team of its own\nteam: portal\nparent: 01CARDA1\nrank: c\n---\n",
		"cards/s/2/01CARDS2.md": "---\ntitle: sub-subtask\nteam: portal\nparent: 01CARDS1\nrank: d\n---\n",
		"cards/o/1/01CARDO1.md": "---\ntitle: unrelated\nteam: portal\nrank: e\n---\n",
	})
	closed := repoWith(t, map[string]string{
		TeamPath("01T_SECRET"): "name: secret\nrank: c\ncreated: 2026-06-02T08:00:00Z\n",
	})
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}}, BackendOptions{Now: func() time.Time { return at("2026-08-28T09:00:00Z") }})
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x")
	work, _ := findByID(b, "01CARDA1")
	ctx, flush := WithScope(ctx, Action{Name: "update", ID: "01JB4KA0M2P4R6T8V0X2Z4B6N2", Cards: []string{"01CARDA1"}})
	if err := mb.SetTeam(ctx, b, work, "secret"); err != nil {
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
	// The subtask's own team is a shared one, yet it followed its parent.
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

// The domain a new team is declared in is the caller's
// choice, carried on the context (board.WithDomain) when the input does not
// name one: a service that builds the stub itself need not know about
// domains. Unknown domains are refused; cards never take one.
func TestMultiBackendCreateHonoursDomainFromContext(t *testing.T) {
	mb, shared, closed := twoDomains(t)
	ctx := board.WithDomain(ctxAs("kvaps"), "closed")
	b, _ := mb.LoadBoard(ctx, "x")
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
	if err := mb.SetSprintState(board.WithDomain(ctxAs("kvaps"), "nope"), b, "x", "2026-08-31", ""); err == nil {
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
	if err := mb.SetTeam(sctx, b, card, "secret"); err != nil {
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
	for _, want := range []string{"progress:30>60@timur", "team:portal>secret@kvaps", "progress:>30@kvaps"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("log lacks %q:\n%s", want, joined)
		}
	}
	// Newest first, and the old domain's entries come after the move.
	if !strings.HasPrefix(joined, "progress:30>60@timur") || strings.Index(joined, "team:portal>secret") > strings.Index(joined, "progress:>30@kvaps") {
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
		BoardPath:               "schema: 2\ntitle: b\n",
		TeamPath("01T_NEWER"):   "name: portal\nrank: a\ncreated: 2026-07-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: portal\nrank: a\n---\n",
	})
	closed := repoWith(t, map[string]string{
		TeamPath("01T_OLDER"):   "name: portal\nrank: z\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: secret\nrank: a\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
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
		BoardPath:               "schema: 2\ntitle: b\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: portal\nrank: a\n---\n",
	})
	closed := repoWith(t, map[string]string{
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: secret\nrank: a\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
	})
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}}, BackendOptions{})
	got, err := mb.LoadCards(context.Background(), board.Board{}, []string{"01CARDA1"})
	if err != nil || len(got) != 1 || got[0].Domain != "closed" || got[0].Team != "secret" {
		t.Fatalf("LoadCards = %+v (%v), want the closed copy once", got, err)
	}
}

// G22 — maintenance removes a torn move's ghost, but only once the
// destination has landed; a duplicate that is not a move (neither copy says
// movedFrom) is a maintainer's problem and is never touched.
func TestMultiBackendSweepGhosts(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:               "schema: 2\ntitle: b\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: portal\nrank: a\n---\n",
		"cards/d/1/01CARDD1.md": "---\ntitle: dup\nteam: portal\nrank: b\n---\n",
	})
	closed := repoWith(t, map[string]string{
		"cards/a/1/01CARDA1.md": "---\ntitle: moving\nteam: secret\nrank: a\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
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
	if err := mb.SetSprintState(ctx, b, "infra", "2026-08-31", ""); err != nil {
		t.Fatal(err)
	}
	if err := mb.SetSprintState(board.WithDomain(ctx, "closed"), b, "vault", "2026-08-31", ""); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadAll(mb.domains)
	for _, tm := range s.Teams {
		switch {
		case tm.Name == "infra" && tm.Domain != "shared":
			t.Fatalf("a team without a domain choice landed in %q, want the primary", tm.Domain)
		case tm.Name == "vault" && tm.Domain != "closed":
			t.Fatalf("a team with a domain choice landed in %q, want it", tm.Domain)
		}
	}
	if err := mb.SetSprintState(board.WithDomain(ctx, "nope"), b, "x", "2026-08-31", ""); err == nil {
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
	// The pointer of a team is written where the team is declared.
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

// The grid's × on a subtask standing in its PARENT's repository, against
// the real store: what is left is answered like any other card — deleted, the working area having been its last home. The service
// tests prove the rule; this proves the whole gesture survives the move,
// which is the part a fake cannot show: the file is left in NEITHER
// repository, and no half-moved copy stays behind.
func TestTheGridRemoveOnASubtaskAcrossRepositories(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:              "schema: 2\ntitle: b\n",
		TeamPath("01T_PORTAL"): "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-17\n",
	})
	closed := repoWith(t, map[string]string{
		TeamPath("01T_SECRET"): "name: secret\nrank: b\ncreated: 2026-06-02T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-17\n",
		// The child carries a team of the OTHER repository: out of its
		// group, its own team places it there.
		"cards/t/1/01PARENT1.md":  "---\ntitle: closed work\nteam: secret\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
		"cards/0/1/01KID00001.md": "---\ntitle: the child\nteam: portal\nparent: 01PARENT1\nstart: 2026-08-20\nsprint: 2026-08-24\nrank: b\ncreated: 2026-08-20T09:00:00Z\n---\n",
	})
	clock := at("2026-08-28T09:00:00Z")
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}},
		BackendOptions{Now: func() time.Time { clock = clock.Add(time.Second); return clock }})
	ctx, flush := WithScope(ctxAs("kvaps"), Action{Name: "remove", ID: "01JB4KA0M2P4R6T8V0X2Z4B6N2", Cards: []string{"01KID00001"}})
	if err := boardservice.New(mb).Remove(ctx, "acme", "01KID00001", board.ViewAll, boardservice.RemoveAuto); err != nil {
		t.Fatalf("the × must complete: %v", err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath("01KID00001")
	if _, err := shared.ReadFile(p); err == nil {
		t.Fatal("the × empties the card's last home: no file where it was moved to")
	}
	if _, err := closed.ReadFile(p); err == nil {
		t.Fatal("and none left behind in the parent's repository either")
	}
	b, err := mb.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if kid, ok := findByID(b, "01KID00001"); ok {
		t.Fatalf("the card is gone from the board: %+v", kid)
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

// THE TOP OF THE BOARD IS THE TOP OF EVERY DOMAIN. Moving a card to the top
// (afterID "") — a review nobody has taken being made, a card parked at the
// top of its backlog — puts it before every card of the merged board, not
// only before the cards of its own repository: the board's order is one
// list across the domains (K10's "top of the board's order").
func TestMultiBackendTopIsTheTopOfTheMergedBoard(t *testing.T) {
	mb, _, _ := twoDomains(t)
	ctx := ctxAs("kvaps")
	b, err := mb.LoadBoard(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	var card board.Card
	for _, c := range b.Cards {
		if c.ItemID == "01CARDB2" {
			card = c
		}
	}
	if err := mb.MoveCard(ctx, b, card, ""); err != nil {
		t.Fatal(err)
	}
	b, _ = mb.LoadBoard(ctx, "x")
	ranks := map[string]string{}
	for _, c := range b.Cards {
		ranks[c.ItemID] = c.Rank
	}
	if ranks["01CARDB2"] >= ranks["01CARDA1"] {
		t.Fatalf("the closed card ranks %q, the shared one %q: the top is not the board's", ranks["01CARDB2"], ranks["01CARDA1"])
	}
}

// The customer roster lives in the primary and is the board's whatever the
// domains: a customer added through the MultiBackend is a file of the
// primary, read on the merged board, and a delete takes it off a card of a
// closed domain too.
func TestMultiBackendKeepsTheCustomerRosterInThePrimary(t *testing.T) {
	mb, shared, closed := twoDomains(t)
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x")
	cu, err := mb.AddCustomer(ctx, b, "Hidora")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shared.ReadFile(CustomerPath(cu.ID)); err != nil {
		t.Fatalf("the customer is not in the primary: %v", err)
	}
	if _, err := closed.ReadFile(CustomerPath(cu.ID)); err == nil {
		t.Fatal("the customer leaked into the closed repository")
	}
	b, _ = mb.LoadBoard(ctx, "x")
	if len(b.Customers) != 1 || b.Customers[0].Name != "Hidora" {
		t.Fatalf("the merged board's roster = %+v", b.Customers)
	}
	var card board.Card
	for _, c := range b.Cards {
		if c.ItemID == "01CARDB2" {
			card = c
		}
	}
	if err := mb.SetCustomers(ctx, b, card, []string{cu.ID}); err != nil {
		t.Fatal(err)
	}
	b, _ = mb.LoadBoard(ctx, "x")
	if err := mb.DeleteCustomer(ctx, b, cu.ID); err != nil {
		t.Fatal(err)
	}
	b, _ = mb.LoadBoard(ctx, "x")
	for _, c := range b.Cards {
		if len(c.Customers) != 0 {
			t.Fatalf("%s still names %v", c.ItemID, c.Customers)
		}
	}
	if len(b.Customers) != 0 {
		t.Fatalf("the roster still holds %+v", b.Customers)
	}
}
