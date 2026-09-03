package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Git mode end to end: a server configured with --repo serves the board
// from its clone, a write becomes a commit, the drain pushes it, health
// shows what is unpushed, and an unborn remote is refused with the hint.

func gitModeServer(t *testing.T, remote gitstore.Remote) *Server {
	t.Helper()
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git: &GitConfig{
			Repos:     []RepoSpec{{Name: "board", URL: remote.URL}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	releaseDataDir(t, srv)
	// Identity without a GitHub token: git mode needs none for the API.
	srv.apiTokens = func(*http.Request) (string, string, error) { return "", "tester", nil }
	srv.gitBE.git.pushDelay = 0 // tests push by hand; a timer firing after the test races TempDir's cleanup
	return srv
}

// G59 — the board NAMES its repository, one or many. Every stamp the
// payload carries is a domain name (the store stamps the primary's entries
// too), so a client that reads "no domains" as "the primary is the empty
// string" ends up with two names for one repository — and every rule that
// asks "the same repository?" answers no. On a single-repository board
// that hid the "+" in the no-project bucket, hid "No team", and made the
// grid's × on a subtask look like a delete to the client while the server
// was ungrouping it.
func TestTheBoardNamesItsRepositoryEvenWhenThereIsOnlyOne(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv := gitModeServer(t, remote)
	var got struct {
		Metadata struct {
			Domains []struct {
				Name     string `json:"name"`
				Writable bool   `json:"writable"`
			} `json:"domains"`
			TeamDomains map[string]string `json:"teamDomains"`
		} `json:"metadata"`
	}
	rec := do(t, srv, http.MethodGet, "/api/v1/board", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Metadata.Domains) != 1 || got.Metadata.Domains[0].Name != "board" {
		t.Fatalf("the board names its own repository: %+v", got.Metadata.Domains)
	}
	// And what the roster is stamped with is that same name, in the same
	// namespace the client compares in.
	for team, domain := range got.Metadata.TeamDomains {
		if domain != "board" {
			t.Fatalf("team %q is stamped %q, not the board's own name", team, domain)
		}
	}
}

func TestGitModeServesTheConfiguredBoard(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv := gitModeServer(t, remote)

	// The board comes from the clone; owner/board in the query are ignored —
	// there is exactly one board, the configured repository.
	rec := do(t, srv, http.MethodGet, "/api/v1/board", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /board: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"portal"`) {
		t.Fatalf("board body lacks the seeded team: %s", rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/cards?view=all", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"one"`) || !strings.Contains(rec.Body.String(), `"two"`) {
		t.Fatalf("GET /cards: %d %s", rec.Code, rec.Body.String())
	}

	// A create answers at once with the final id, and its commit follows.
	rec = do(t, srv, http.MethodPost, "/api/v1/cards", `{"title":"three","team":"portal","zone":"planned","dates":{"start":"2026-08-27"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /cards: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Metadata struct{ UID string } `json:"metadata"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || len(created.Metadata.UID) != 26 {
		t.Fatalf("created uid = %q (%v)", created.Metadata.UID, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	p, _ := gitstore.CardPath(created.Metadata.UID)
	if _, err := srv.gitBE.git.primary().ReadFile(p); err != nil {
		t.Fatalf("created card not committed locally: %v", err)
	}

	// Health names the unpushed age until the drain pushes.
	rec = do(t, srv, http.MethodGet, "/api/healthz", "")
	if !strings.Contains(rec.Body.String(), `"unpushedAgeSeconds"`) {
		t.Fatalf("healthz = %s", rec.Body.String())
	}
	if err := srv.drainAndPush(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	other, err := gitstore.Clone(context.Background(), filesystem.NewStorage(osfs.New(t.TempDir()), cache.NewObjectLRUDefault()), remote, gitstore.Options{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ReadFile(p); err != nil {
		t.Fatalf("the pushed card is not on the remote: %v", err)
	}
	rec = do(t, srv, http.MethodGet, "/api/healthz", "")
	if !strings.Contains(rec.Body.String(), `"unpushedAgeSeconds":0`) {
		t.Fatalf("healthz after push = %s", rec.Body.String())
	}
}

// A repository that was never initialised is refused at start with the
// command that fixes it — not served as an empty board.
func TestGitModeRefusesUnbornRemote(t *testing.T) {
	remote := gitRemote(t) // registered, never pushed to
	_, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git:    &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: t.TempDir()},
	})
	if err == nil || !strings.Contains(err.Error(), "aeman init") {
		t.Fatalf("err = %v, want a refusal naming aeman init", err)
	}
}

// The claim is taken before the directory is touched, so every failure
// after that point has to hand it back: a start that refused once would
// otherwise leave the directory locked until the process died, and the
// second attempt would blame a process that is this very one.
func TestAFailedStartReleasesTheDataDir(t *testing.T) {
	remote := gitRemote(t) // registered, never seeded: the start refuses
	dir := t.TempDir()
	if _, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git:    &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir},
	}); err == nil {
		t.Fatal("an unborn remote must refuse the start")
	}
	l, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("the refused start kept the claim: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}

// A second start over the same data directory reopens the clone instead of
// cloning again, and picks up what the remote gained meanwhile.
func TestGitModeReopensTheClone(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	mk := func() *Server {
		srv, err := New(Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Git:    &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir},
		})
		if err != nil {
			t.Fatal(err)
		}
		srv.apiTokens = func(*http.Request) (string, string, error) { return "", "tester", nil }
		t.Cleanup(func() { _ = srv.Close() })
		return srv
	}
	first := mk()
	rec := do(t, first, http.MethodPost, "/api/v1/cards", `{"title":"offline","team":"portal","zone":"planned","dates":{"start":"2026-08-27"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first.store.waitDrained(ctx)
	// No push: the process "dies" — and dying releases its claim on the
	// data directory, which is what lets the second one open it at all.
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second := mk()
	rec = do(t, second, http.MethodGet, "/api/v1/cards?view=all", "")
	if !strings.Contains(rec.Body.String(), `"offline"`) {
		t.Fatalf("the unpushed card did not survive the restart: %s", rec.Body.String())
	}
	if n, _ := second.gitBE.git.primary().Unpushed(); n != 1 {
		t.Fatalf("unpushed after reopen = %d, want 1", n)
	}
}

// aeman mcp --repo owns its own store: OpenGitBackend hands out the backend
// without an HTTP server, and Drain pushes what a session left behind.
func TestOpenGitBackendStandalone(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	gb, err := OpenGitBackend(&GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gb.Close() })
	ctx := withAction(context.Background(), "01JB4KA0M2P4R6T8V0X2Z4B6M1", "progress")
	bd, err := gb.Backend().LoadBoard(ctx, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Cards) != 2 {
		t.Fatalf("cards = %d", len(bd.Cards))
	}
	if err := gb.Backend().SetProgress(ctx, bd, cardByTitle(bd, "one"), 60); err != nil {
		t.Fatal(err)
	}
	if err := gb.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	other, err := gitstore.Clone(context.Background(), filesystem.NewStorage(osfs.New(t.TempDir()), cache.NewObjectLRUDefault()), remote, gitstore.Options{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := gitstore.CardPath(cardByTitle(bd, "one").ItemID)
	if data, _ := other.ReadFile(p); !strings.Contains(string(data), "progress: 60") {
		t.Fatalf("the drained write is not on the remote:\n%s", data)
	}
}

// The daemon's whole health signal is this number, and this is the only
// place the delegation to the store is exercised: /healthz's own test
// supplies its own health function, so nothing there would notice this
// reading the wrong board. It must not rest on unpushedAge ignoring the
// key it is given.
func TestOpenGitBackendReportsItsUnpushedAge(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	gb, err := OpenGitBackend(&GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: t.TempDir()},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gb.Close() })

	if age := gb.UnpushedAge(); age != 0 {
		t.Fatalf("a fresh clone has nothing waiting, got %v", age)
	}

	ctx := withAction(context.Background(), "01JB4KA0M2P4R6T8V0X2Z4B6M1", "progress")
	bd, err := gb.Backend().LoadBoard(ctx, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if err := gb.Backend().SetProgress(ctx, bd, cardByTitle(bd, "one"), 60); err != nil {
		t.Fatal(err)
	}
	// The write has to become a commit before it counts as unpushed.
	wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gb.be.store.waitDrained(wait)
	if age := gb.UnpushedAge(); age <= 0 {
		t.Fatalf("a committed, unpushed write is not reported: %v", age)
	}

	if err := gb.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if age := gb.UnpushedAge(); age != 0 {
		t.Fatalf("after the push nothing is waiting, got %v", age)
	}
}

// The rejection replaced an index-out-of-range panic on cfg.Repos[0].
func TestOpenGitBackendRefusesNoRepository(t *testing.T) {
	_, err := OpenGitBackend(&GitConfig{DataDir: t.TempDir()},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("a backend with no repository was opened")
	}
	if !strings.Contains(err.Error(), "--repo") {
		t.Fatalf("the refusal does not name the flag: %v", err)
	}
}

// G62 — one process owns a board's clones. A second `aeman mcp` on the same
// --data is refused at start, naming the directory, rather than left to
// race the first one's fetch and push; and the refusal lasts exactly as
// long as the first process holds the directory.
func TestOpenGitBackendRefusesASecondProcessOnTheSameData(t *testing.T) {
	// What this pins is the refusal, not the grace a start gives a
	// predecessor that is leaving: this holder is not going anywhere.
	restore := dataLockWait
	dataLockWait = 50 * time.Millisecond
	t.Cleanup(func() { dataLockWait = restore })
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	open := func() (*GitBackend, error) {
		return OpenGitBackend(&GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir},
			slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	first, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(); err == nil {
		t.Fatal("a second backend opened the data directory the first one holds")
	} else if !strings.Contains(err.Error(), dir) {
		t.Fatalf("the refusal does not name the directory: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, err := open()
	if err != nil {
		t.Fatalf("the directory is still claimed after Close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// The claim does not care which subcommand took it: `aeman serve` and
// `aeman mcp` are one process each, and the second of them is told to talk
// to the first over HTTP instead of opening the clones a second time.
func TestServeAndMCPCannotShareOneDataDir(t *testing.T) {
	// What this pins is the refusal, not the grace a start gives a
	// predecessor that is leaving: this holder is not going anywhere.
	restore := dataLockWait
	dataLockWait = 50 * time.Millisecond
	t.Cleanup(func() { dataLockWait = restore })
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(Options{
		Logger: log,
		Git:    &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	mcpCfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}
	if _, err := OpenGitBackend(mcpCfg, log); err == nil {
		t.Fatal("aeman mcp opened the data directory the server holds")
	} else if !strings.Contains(err.Error(), "aeman mcp --listen") {
		t.Fatalf("the refusal does not point at the shared daemon: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	gb, err := OpenGitBackend(mcpCfg, log)
	if err != nil {
		t.Fatalf("the server's Close did not release the directory: %v", err)
	}
	if err := gb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// The request's action name comes from its route.
func TestActionNameFromRoute(t *testing.T) {
	cases := map[[2]string]string{
		{http.MethodPost, "/api/v1/cards"}:                            "create",
		{http.MethodPatch, "/api/v1/cards/01J"}:                       "update",
		{http.MethodDelete, "/api/v1/cards/01J"}:                      "delete",
		{http.MethodPost, "/api/v1/cards/01J/actions/send-to-review"}: "send-to-review",
		{http.MethodPost, "/api/v1/cards/01J/notes"}:                  "note",
		{http.MethodPatch, "/api/v1/cards/01J/notes/01K"}:             "note-edit",
		{http.MethodDelete, "/api/v1/cards/01J/notes/01K"}:            "note-delete",
		{http.MethodPost, "/api/v1/sprints/actions/carry-over"}:       "carry-over",
		{http.MethodPost, "/api/v1/epics"}:                            "add-epic",
		{http.MethodPost, "/api/v1/epics/actions/rename"}:             "rename",
		{http.MethodPost, "/api/v1/projects"}:                         "add-project",
		{http.MethodPost, "/api/v1/processes/tasks"}:                  "add-task",
		{http.MethodGet, "/api/v1/cards"}:                             "",
	}
	for in, want := range cases {
		if got := actionName(in[0], in[1]); got != want {
			t.Fatalf("actionName(%s %s) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

// Two repositories that already declare the same team (or project, or
// process) cannot be served as one board: the store would merge them into
// one and the cards of two different teams would mix. The server refuses to
// start and names both files, so a maintainer renames one and starts again.
func TestGitModeRefusesCollidingRosterNames(t *testing.T) {
	shared, closed := gitRemoteN(t, "shared"), gitRemoteN(t, "closed")
	seedGitRemote(t, shared) // team portal at teams/01JB4TEAM.yaml
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: closed\n")},
		{Path: gitstore.TeamPath("01JB4TEAMX"), Data: []byte("name: portal\nrank: a\ncreated: 2026-07-01T08:00:00Z\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), closed); err != nil {
		t.Fatal(err)
	}
	_, err = New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git: &GitConfig{Repos: []RepoSpec{{Name: "shared", URL: shared.URL}, {Name: "closed", URL: closed.URL}},
			DataDir: t.TempDir()},
	})
	if err == nil {
		t.Fatal("two domains declaring team portal must not be served as one board")
	}
	for _, want := range []string{`team "portal"`, "shared/teams/01JB4TEAM.yaml", "closed/teams/01JB4TEAMX.yaml", "rename"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q lacks %q", err.Error(), want)
		}
	}
}
