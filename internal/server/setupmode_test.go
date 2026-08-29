package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	forgepkg "github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// A board whose GitHub App is not installed on its repositories used to be
// a server that refused to start: the operator got a log line, and whoever
// opened the URL got nothing at all. The trouble is fixed with one click —
// installing the app — so the server comes up anyway and SHOWS the page
// that says so: the reason and the install button on "/", a 503 with the
// actionUrl on the API, "setup" in healthz. Installing the app (GitHub's
// post-install redirect hits /auth/setup) brings the board up without a
// restart.
func TestAMissingInstallationIsAPageNotARefusalToStart(t *testing.T) {
	// The remote needs an owner/repo shape: the app resolves its
	// installation by slug.
	url := "gittest://remotes/acme/" + strings.ReplaceAll(t.Name(), "/", "_") + ".git"
	gitTestRemotes[url] = memory.NewStorage()
	remote := gitstore.Remote{URL: url}
	seedGitRemote(t, remote)

	// The App API: no installation at first; flipping `installed` is the
	// person clicking Install on GitHub.
	var installed atomic.Bool
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app":
			_, _ = w.Write([]byte(`{"slug":"aenix-aeman","html_url":"https://github.com/apps/aenix-aeman"}`))
		case strings.HasSuffix(r.URL.Path, "/installation"):
			if !installed.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"id": 7}`))
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_x","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(appSrv.Close)
	app, err := forgepkg.NewGitHubAppAt(appSrv.URL, appSrv.Client(), "12345", testServerAppPEM(t))
	if err != nil {
		t.Fatal(err)
	}

	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git: &GitConfig{App: app,
			Repos:     []RepoSpec{{Name: "board", URL: remote.URL}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"}},
	})
	if err != nil {
		t.Fatalf("a missing installation must not refuse the start: %v", err)
	}
	srv.apiTokens = func(*http.Request) (string, string, error) { return "", "kvaps", nil }

	// The page says what is wrong and what to click.
	rec := doAs(t, srv, "kvaps", "GET", "/", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("setup page: %d, want 503 so nothing caches it as the app", rec.Code)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "https://github.com/apps/aenix-aeman/installations/new") || !strings.Contains(page, "board") {
		t.Fatalf("the page must carry the install link and name the repository: %s", page)
	}
	// The API answers with the machine-readable action, not a panic.
	rec = doAs(t, srv, "kvaps", "GET", "/api/v1/board", "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "installations/new") {
		t.Fatalf("api during setup: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/healthz", ""); !strings.Contains(rec.Body.String(), `"setup"`) {
		t.Fatalf("healthz must say setup: %s", rec.Body.String())
	}

	// The person installs the app; GitHub redirects to /auth/setup; the
	// board comes up without a restart.
	installed.Store(true)
	if rec := doAs(t, srv, "kvaps", "GET", "/auth/setup?setup_action=install", ""); rec.Code != http.StatusFound {
		t.Fatalf("setup redirect: %d", rec.Code)
	}
	srv.access = openAccess{domains: []string{"board"}}
	rec = doAs(t, srv, "kvaps", "GET", "/api/v1/board", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("after the install the board must serve: %d %s", rec.Code, rec.Body.String())
	}
}

// Any other startup trouble still stops the server: a page cannot fix a
// wrong token or an unreachable remote, and starting anyway would hide it.
func TestOtherStartupTroubleStillRefusesToStart(t *testing.T) {
	_, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git: &GitConfig{
			Repos:     []RepoSpec{{Name: "board", URL: "gittest://remotes/never-seeded-" + t.Name() + ".git"}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"}},
	})
	if err == nil {
		t.Fatal("an unreachable repository without an app must still refuse the start")
	}
}

var _ = context.Background

// After someone installs (or updates) the app, GitHub may start an OAuth
// authorization of its own and land on /auth/callback with an installation
// signature and no state of ours. That is the same event as /auth/setup —
// not a failed sign-in — and it kept greeting people who had just done the
// right thing with {"error":"invalid OAuth state"}. A real sign-in without
// a matching state is still refused: the CSRF check is only stepped around
// for a callback that GitHub itself marks as an installation redirect.
func TestGitHubsPostInstallRedirectIsNotAFailedSignIn(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	seedGitRemote(t, shared)
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &OAuthConfig{ClientID: "Iv23test", ClientSecret: "s", BaseURL: "https://board.example"},
		Git:    &GitConfig{Repos: []RepoSpec{{Name: "shared", URL: shared.URL}}, DataDir: t.TempDir(), Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.gitBE.git.pushDelay = 0

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}
	for _, target := range []string{
		"/auth/callback?code=xyz&installation_id=157446438&setup_action=install",
		"/auth/callback?installation_id=157446438&setup_action=update",
		"/auth/callback?code=xyz&setup_action=install&state=not-ours",
	} {
		if rec := get(target); rec.Code != http.StatusFound {
			t.Fatalf("%s: %d %s, want a redirect home", target, rec.Code, rec.Body.String())
		}
	}
	// An ordinary sign-in callback with a wrong state stays refused.
	if rec := get("/auth/callback?code=xyz&state=not-ours"); rec.Code != http.StatusBadRequest {
		t.Fatalf("a forged sign-in: %d, want 400", rec.Code)
	}
}
