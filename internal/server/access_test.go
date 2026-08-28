package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// G17/G25 — who sees what, who writes where. A visitor's board is the union
// of the domains they can read: an unreadable domain is absent from the
// snapshot and from the watch stream; an unreadable primary is no board at
// all. A write needs write access to the domain it targets — both domains
// of a move — whatever the server's own credential can do.

// fakeAccess answers rights per login; an unknown login is refused.
type fakeAccess struct{ byLogin map[string]*domainRights }

func (f fakeAccess) canPush(context.Context, string, string) (bool, error) { return true, nil }

func (f fakeAccess) rights(_ context.Context, _, login string) (*domainRights, error) {
	r, ok := f.byLogin[login]
	if !ok {
		return nil, errors.New("unknown visitor")
	}
	return r, nil
}

// readers are the known logins whose rights read the domain.
func (f fakeAccess) readers(_ context.Context, domain string, logins []string) ([]string, error) {
	var out []string
	for _, l := range logins {
		if r, ok := f.byLogin[l]; ok && r.canRead(domain) {
			out = append(out, l)
		}
	}
	return out, nil
}

func rightsOn(read, write []string) *domainRights {
	r := &domainRights{primary: "shared", read: map[string]bool{}, write: map[string]bool{}}
	for _, d := range read {
		r.read[d] = true
	}
	for _, d := range write {
		r.write[d] = true
	}
	return r
}

// gitModeServerOver is a server over several remotes (shared, closed) whose
// visitors are named by the X-Login header and whose rights come from the
// fake.
func gitModeServerOver(t *testing.T, access fakeAccess, remotes ...gitstore.Remote) *Server {
	t.Helper()
	names := []string{"shared", "closed"}
	specs := make([]RepoSpec, 0, len(remotes))
	for i, r := range remotes {
		specs = append(specs, RepoSpec{Name: names[i], URL: r.URL})
	}
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git:    &GitConfig{Repos: specs, DataDir: t.TempDir(), Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.apiTokens = func(r *http.Request) (string, string, error) {
		login := r.Header.Get("X-Login")
		if login == "" {
			login = "tester"
		}
		return "", login, nil
	}
	srv.access = access
	srv.gitBE.git.pushDelay = 0 // tests push by hand; a timer firing after the test races TempDir's cleanup
	return srv
}

func doAs(t *testing.T, srv *Server, login, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("X-Login", login)
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, r)
	return rec
}

// cardUID finds a card's uid by title through the API, as a visitor who can
// see it.
func cardUID(t *testing.T, srv *Server, login, title string) string {
	t.Helper()
	rec := doAs(t, srv, login, http.MethodGet, "/api/v1/cards?view=all", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /cards as %s: %d %s", login, rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			Metadata struct{ UID string }   `json:"metadata"`
			Spec     struct{ Title string } `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, it := range list.Items {
		if it.Spec.Title == title {
			return it.Metadata.UID
		}
	}
	t.Fatalf("card %q not visible to %s", title, login)
	return ""
}

func twoDomainServer(t *testing.T) *Server {
	t.Helper()
	shared, closed := gitRemoteN(t, "shared"), gitRemoteN(t, "closed")
	seedGitRemote(t, shared)
	seedClosedRemote(t, closed)
	return gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{
		"alice": rightsOn([]string{"shared"}, []string{"shared"}),                     // shared only
		"bob":   rightsOn([]string{"shared", "closed"}, []string{"shared", "closed"}), // everything
		"carol": rightsOn([]string{"closed"}, []string{"closed"}),                     // not the primary
		"dave":  rightsOn([]string{"shared", "closed"}, []string{"shared"}),           // reads all, writes shared
	}}, shared, closed)
}

// G17 — an unreadable domain is absent: no project, no column, no card.
func TestUnreadableDomainAbsent(t *testing.T) {
	srv := twoDomainServer(t)
	rec := doAs(t, srv, "alice", http.MethodGet, "/api/v1/board", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"secret"`) || !strings.Contains(rec.Body.String(), `"portal"`) {
		t.Fatalf("alice's board: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAs(t, srv, "alice", http.MethodGet, "/api/v1/cards?view=all", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "three-closed") || !strings.Contains(rec.Body.String(), `"one"`) {
		t.Fatalf("alice's cards: %d %s", rec.Code, rec.Body.String())
	}
	// The closed card is not fetchable by id either.
	uid := cardUID(t, srv, "bob", "three-closed")
	if rec := doAs(t, srv, "alice", http.MethodGet, "/api/v1/cards/"+uid, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("alice GET closed card: %d, want 404", rec.Code)
	}
	rec = doAs(t, srv, "bob", http.MethodGet, "/api/v1/board", "")
	if !strings.Contains(rec.Body.String(), `"secret"`) {
		t.Fatalf("bob's board lacks the closed project: %s", rec.Body.String())
	}
}

// G17 — no primary, no board.
func TestUnreadablePrimaryIs403(t *testing.T) {
	srv := twoDomainServer(t)
	for _, path := range []string{"/api/v1/board", "/api/v1/cards?view=all"} {
		if rec := doAs(t, srv, "carol", http.MethodGet, path, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("carol GET %s: %d %s, want 403", path, rec.Code, rec.Body.String())
		}
	}
	if rec := doAs(t, srv, "nobody", http.MethodGet, "/api/v1/board", ""); rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown visitor: %d, want a refusal", rec.Code)
	}
}

// G25 — a write needs write access to the domain it targets; a move needs
// both. A read-only collaborator sees the board and cannot change it.
func TestWriteNeedsWriteAccessToTheDomain(t *testing.T) {
	srv := twoDomainServer(t)
	closedUID := cardUID(t, srv, "dave", "three-closed")
	sharedUID := cardUID(t, srv, "dave", "one")
	if rec := doAs(t, srv, "dave", http.MethodPatch, "/api/v1/cards/"+closedUID, `{"progress":50}`); rec.Code != http.StatusForbidden {
		t.Fatalf("dave PATCH closed card: %d %s, want 403", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "dave", http.MethodPatch, "/api/v1/cards/"+sharedUID, `{"progress":50}`); rec.Code != http.StatusOK {
		t.Fatalf("dave PATCH shared card: %d %s", rec.Code, rec.Body.String())
	}
	// Filing the shared card under the closed project moves it into a domain
	// dave cannot write: refused, and the card stays where it was.
	if rec := doAs(t, srv, "dave", http.MethodPatch, "/api/v1/cards/"+sharedUID, `{"project":"secret","epic":"Risk"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("dave moves into closed: %d %s, want 403", rec.Code, rec.Body.String())
	}
	rec := doAs(t, srv, "dave", http.MethodGet, "/api/v1/cards/"+sharedUID, "")
	if strings.Contains(rec.Body.String(), `"secret"`) {
		t.Fatalf("refused move changed the card: %s", rec.Body.String())
	}
	// A note, a delete, a roster write on the closed side: all refused.
	if rec := doAs(t, srv, "dave", http.MethodPost, "/api/v1/cards/"+closedUID+"/notes", `{"text":"hi"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("dave notes a closed card: %d", rec.Code)
	}
	if rec := doAs(t, srv, "dave", http.MethodDelete, "/api/v1/cards/"+closedUID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("dave deletes a closed card: %d", rec.Code)
	}
	if rec := doAs(t, srv, "dave", http.MethodPost, "/api/v1/epics", `{"project":"secret","name":"Later"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("dave adds a closed column: %d %s", rec.Code, rec.Body.String())
	}
	// bob may do all of it.
	if rec := doAs(t, srv, "bob", http.MethodPatch, "/api/v1/cards/"+sharedUID, `{"project":"secret","epic":"Risk"}`); rec.Code != http.StatusOK {
		t.Fatalf("bob moves into closed: %d %s", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	// Alice, who cannot read closed, no longer sees the moved card.
	if rec := doAs(t, srv, "alice", http.MethodGet, "/api/v1/cards/"+sharedUID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("alice still sees the card moved into closed: %d", rec.Code)
	}
}

// Teams, projects and processes are declared in the domain the caller picks
// — a body field, refused unless the visitor can write there, unknown names
// refused outright; without a choice they land in the primary.
func TestCreateRosterEntryInChosenDomain(t *testing.T) {
	srv := twoDomainServer(t)
	if rec := doAs(t, srv, "dave", http.MethodPost, "/api/v1/projects", `{"name":"vault","domain":"closed"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("dave declares a closed project: %d %s, want 403", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "bob", http.MethodPost, "/api/v1/projects", `{"name":"vault","domain":"nope"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown domain: %d %s, want 400", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "bob", http.MethodPost, "/api/v1/projects", `{"name":"vault","domain":"closed"}`); rec.Code != http.StatusCreated {
		t.Fatalf("bob declares a closed project: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "bob", http.MethodPost, "/api/v1/processes", `{"name":"audit","domain":"closed"}`); rec.Code != http.StatusCreated {
		t.Fatalf("bob declares a closed process: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "bob", http.MethodPatch, "/api/v1/sprints", `{"team":"ops","current":"2026-08-31","domain":"closed"}`); rec.Code != http.StatusOK {
		t.Fatalf("bob declares a closed team: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "alice", http.MethodPost, "/api/v1/projects", `{"name":"open"}`); rec.Code != http.StatusCreated {
		t.Fatalf("alice declares a primary project: %d %s", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	// Alice sees the primary's project, not the closed ones.
	rec := doAs(t, srv, "alice", http.MethodGet, "/api/v1/board", "")
	if body := rec.Body.String(); !strings.Contains(body, `"open"`) || strings.Contains(body, `"vault"`) || strings.Contains(body, `"audit"`) || strings.Contains(body, `"ops"`) {
		t.Fatalf("alice's board: %s", body)
	}
	rec = doAs(t, srv, "bob", http.MethodGet, "/api/v1/board", "")
	if body := rec.Body.String(); !strings.Contains(body, `"vault"`) || !strings.Contains(body, `"audit"`) || !strings.Contains(body, `"ops"`) {
		t.Fatalf("bob's board: %s", body)
	}
}

// G16 — GET /board names the visitor's domains: which they may write, and
// who can read each one, so the reviewer picker offers only logins that can
// read the card's domain.
func TestBoardListsDomainsWithMembers(t *testing.T) {
	srv := twoDomainServer(t)
	// Assignees make members: kvaps on the shared card, timur on the closed.
	sharedUID := cardUID(t, srv, "bob", "one")
	closedUID := cardUID(t, srv, "bob", "three-closed")
	for uid, who := range map[string]string{sharedUID: "alice", closedUID: "bob"} {
		if rec := doAs(t, srv, "bob", http.MethodPatch, "/api/v1/cards/"+uid, `{"assignees":["`+who+`"]}`); rec.Code != http.StatusOK {
			t.Fatalf("assign %s: %d %s", who, rec.Code, rec.Body.String())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx) // the commits must land before TempDir is removed
	type domainInfo struct {
		Name     string   `json:"name"`
		Writable bool     `json:"writable"`
		Members  []string `json:"members"`
	}
	var got struct {
		Metadata struct {
			Domains []domainInfo `json:"domains"`
			Members []struct {
				Login     string `json:"login"`
				AvatarURL string `json:"avatarUrl"`
			} `json:"members"`
		} `json:"metadata"`
	}
	rec := doAs(t, srv, "dave", http.MethodGet, "/api/v1/board", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// Members carry the forge's avatar, so the SPA assembles no URL itself.
	if len(got.Metadata.Members) != 2 || got.Metadata.Members[0].Login != "alice" ||
		got.Metadata.Members[0].AvatarURL != "https://avatars.githubusercontent.com/alice?size=48" {
		t.Fatalf("members = %+v", got.Metadata.Members)
	}
	if len(got.Metadata.Domains) != 2 || got.Metadata.Domains[0].Name != "shared" || !got.Metadata.Domains[0].Writable || got.Metadata.Domains[1].Name != "closed" || got.Metadata.Domains[1].Writable {
		t.Fatalf("dave's domains = %+v", got.Metadata.Domains)
	}
	// Members are the board's assignees; per domain, those who can read it:
	// alice cannot read closed, bob can read both.
	if m := got.Metadata.Domains[0].Members; strings.Join(m, ",") != "alice,bob" {
		t.Fatalf("shared members = %v", m)
	}
	if m := got.Metadata.Domains[1].Members; strings.Join(m, ",") != "bob" {
		t.Fatalf("closed members = %v", m)
	}
	// Alice's board names only what she can read.
	rec = doAs(t, srv, "alice", http.MethodGet, "/api/v1/board", "")
	got.Metadata.Domains = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Metadata.Domains) != 1 || got.Metadata.Domains[0].Name != "shared" {
		t.Fatalf("alice's domains = %+v", got.Metadata.Domains)
	}
}

// G17 — the watch stream is filtered like the snapshot: a change in a domain
// the subscriber cannot read never reaches its socket; a change it can read
// does.
func TestWatchFilteredByDomain(t *testing.T) {
	srv := twoDomainServer(t)
	closedUID := cardUID(t, srv, "bob", "three-closed")
	sharedUID := cardUID(t, srv, "bob", "one")
	key := storeKey(srv.gitBoard())
	resources := map[string]bool{"cards": true, "sprints": true, "ordering": true}
	aliceSub, cancelA := srv.store.subscribeAs(key, "alice-tab", nil, resources, rightsOn([]string{"shared"}, nil))
	defer cancelA()
	bobSub, cancelB := srv.store.subscribeAs(key, "bob-tab", nil, resources, rightsOn([]string{"shared", "closed"}, nil))
	defer cancelB()

	if rec := doAs(t, srv, "bob", http.MethodPatch, "/api/v1/cards/"+closedUID, `{"progress":60}`); rec.Code != http.StatusOK {
		t.Fatalf("bob PATCH closed: %d %s", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	if titles := cardTitles(drainFrames(t, aliceSub), "MODIFIED"); len(titles) != 0 {
		t.Fatalf("alice received card frames for a closed-domain change: %v", titles)
	}
	if titles := cardTitles(drainFrames(t, bobSub), "MODIFIED"); len(titles) != 1 || titles[0] != "three-closed" {
		t.Fatalf("bob's frames for the closed change = %v, want three-closed", titles)
	}
	if rec := doAs(t, srv, "bob", http.MethodPatch, "/api/v1/cards/"+sharedUID, `{"progress":60}`); rec.Code != http.StatusOK {
		t.Fatalf("bob PATCH shared: %d %s", rec.Code, rec.Body.String())
	}
	srv.store.waitDrained(ctx)
	if titles := cardTitles(drainFrames(t, aliceSub), "MODIFIED"); len(titles) != 1 || titles[0] != "one" {
		t.Fatalf("alice's frames for a shared change = %v, want one", titles)
	}
}
