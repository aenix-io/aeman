package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// tokenlessCLI is a forge.CLI on a machine with no credential anywhere.
// The tools it stands for answer that state with an error — `gh auth
// token` exits non-zero, and so does the chain the commands build — which
// is the contract this test exists to hold them to.
type tokenlessCLI struct{ err error }

func (c tokenlessCLI) Token(context.Context) (string, error) { return "", c.err }
func (c tokenlessCLI) Login(context.Context) (string, error) { return "", c.err }

// In the local mode `/api/config` decides "have we got a credential?" by
// asking the CLI for a token and looking only at the error. A CLI that
// answers "" with no error is therefore read as signed in: the board
// reports itself authenticated with an empty login, and the UI's "no
// token" banner — which hangs off tokenAvailable — never appears, so the
// person is shown a board that then fails on the forge's 401 with nothing
// explaining why. This pins the shape the CLI has to keep.
func TestLocalModeWithoutATokenIsNotAuthenticated(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		CLI:    tokenlessCLI{err: errors.New("no GitHub token; set GITHUB_TOKEN, run `aeman login`, or run `gh auth login`")},
		Git: &GitConfig{
			Repos:     []RepoSpec{{Name: "board", URL: remote.URL}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.gitBE.git.pushDelay = 0 // a timer firing after the test races TempDir's cleanup

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Mode           string `json:"mode"`
		Authenticated  bool   `json:"authenticated"`
		TokenAvailable bool   `json:"tokenAvailable"`
		Login          string `json:"login"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if got.Authenticated || got.TokenAvailable {
		t.Fatalf("authenticated = %v, tokenAvailable = %v; want both false with no credential anywhere", got.Authenticated, got.TokenAvailable)
	}
	if got.Login != "" {
		t.Fatalf("login = %q, want empty", got.Login)
	}
}
