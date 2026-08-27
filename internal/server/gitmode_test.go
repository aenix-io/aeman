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
	// Identity without a GitHub token: git mode needs none for the API.
	srv.apiTokens = func(*http.Request) (string, string, error) { return "", "tester", nil }
	return srv
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
	if _, err := srv.gitBE.git.repo.ReadFile(p); err != nil {
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
	// No push: the process "dies".
	second := mk()
	rec = do(t, second, http.MethodGet, "/api/v1/cards?view=all", "")
	if !strings.Contains(rec.Body.String(), `"offline"`) {
		t.Fatalf("the unpushed card did not survive the restart: %s", rec.Body.String())
	}
	if n, _ := second.gitBE.git.repo.Unpushed(); n != 1 {
		t.Fatalf("unpushed after reopen = %d, want 1", n)
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
