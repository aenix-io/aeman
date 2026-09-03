package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	forgepkg "github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/nonet"
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

// vanishingCLI is a chain whose elected source empties between the two
// questions the server asks: the first Token answers, the Login after it
// finds nothing there and moves on. A caller that asks separately gets one
// source's token and the next source's name.
type vanishingCLI struct{ asked int }

func (c *vanishingCLI) Token(context.Context) (string, error) {
	c.asked++
	if c.asked == 1 {
		return "tok-alice", nil
	}
	return "tok-bob", nil
}

// Login is the source that took over once alice's emptied — the wrong
// answer for a caller still holding alice's token.
func (c *vanishingCLI) Login(context.Context) (string, error) { return "bob", nil }

func (c *vanishingCLI) TokenAndLogin(context.Context) (string, string, error) {
	c.asked++
	return "tok-alice", "alice", nil
}

// The credential and the name on the work come from ONE source. The server
// used to ask the CLI twice, and a chain re-elects between the two calls
// when its winner empties — `aeman logout` in another terminal, a keychain
// window lapsing onto a removed item — so the request went on to push with
// one account's token under another account's login. Asking once closes
// that: a CLI that can answer both at once is asked that way.
func TestTheRequestsTokenAndLoginComeFromOneSource(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	cli := &vanishingCLI{}
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		CLI:    cli,
		Git: &GitConfig{
			Repos:     []RepoSpec{{Name: "board", URL: remote.URL}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.gitBE.git.pushDelay = 0

	tok, login, err := srv.tokenForRequest(httptest.NewRequest(http.MethodGet, "/api/v1/cards", nil))
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok-alice" || login != "alice" {
		t.Fatalf("token = %q, login = %q; want one source's pair, not tok-alice with bob", tok, login)
	}
}

// `/api/config` names a person on the page, so it asks the same way: the
// login it reports belongs to the token it found, not to whichever source
// answered second.
func TestConfigReportsTheLoginOfTheTokenItFound(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		CLI:    &vanishingCLI{},
		Git: &GitConfig{
			Repos:     []RepoSpec{{Name: "board", URL: remote.URL}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.gitBE.git.pushDelay = 0

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var got struct {
		Authenticated bool   `json:"authenticated"`
		Login         string `json:"login"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if !got.Authenticated || got.Login != "alice" {
		t.Fatalf("authenticated = %v, login = %q; want alice, the owner of the token found", got.Authenticated, got.Login)
	}
}

// unreachableForge is a chain whose token is fine and whose forge cannot
// be asked who it belongs to — a cold start behind a VPN that is not up.
type unreachableForge struct{}

func (unreachableForge) Token(context.Context) (string, error) { return "tok-alice", nil }
func (unreachableForge) Login(context.Context) (string, error) {
	return "", errors.New("ask GitHub whose token this is: dial tcp: i/o timeout")
}
func (c unreachableForge) TokenAndLogin(ctx context.Context) (string, string, error) {
	tok, _ := c.Token(ctx)
	login, err := c.Login(ctx)
	if err != nil {
		// What the real chain does: the token is the answer, an
		// unanswered owner is not a missing credential.
		return tok, "", nil //nolint:nilerr // mirrors chain.TokenAndLogin
	}
	return tok, login, nil
}

// A forge that cannot be reached is not a missing credential. The board
// has a token; only the question of whose it is went unanswered. Failing
// the pair would take every read down on a transient outage and tell the
// person to run `aeman login` for a token they already have — the banner
// lying in the other direction.
func TestAnUnreachableForgeIsNotAMissingToken(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		CLI:    unreachableForge{},
		Git: &GitConfig{
			Repos:     []RepoSpec{{Name: "board", URL: remote.URL}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.gitBE.git.pushDelay = 0

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfg struct {
		Authenticated  bool   `json:"authenticated"`
		TokenAvailable bool   `json:"tokenAvailable"`
		Login          string `json:"login"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if !cfg.TokenAvailable || !cfg.Authenticated {
		t.Fatalf("tokenAvailable = %v, authenticated = %v; the token is there, only its owner is unknown", cfg.TokenAvailable, cfg.Authenticated)
	}
	if cfg.Login != "" {
		t.Fatalf("login = %q, want empty — nobody answered for it", cfg.Login)
	}

	// And the board still reads.
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cards", nil))
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/cards = 401 %s; a forge outage must not take the board down", rec.Body.String())
	}
}

// rejectedToken is a chain whose stored credential the forge refuses.
type rejectedToken struct{}

func (rejectedToken) Token(context.Context) (string, error) { return "tok-revoked", nil }
func (rejectedToken) Login(context.Context) (string, error) { return "", forgepkg.ErrBadToken }
func (c rejectedToken) TokenAndLogin(context.Context) (string, string, error) {
	return "", "", forgepkg.ErrBadToken
}

// A refused token is not a working one. The page must say there is no
// usable credential rather than report a signed-in board that 401s on
// every read with nothing explaining it — the failure a stored token
// makes likely, because it outlives its validity without anyone looking.
func TestARejectedTokenIsNotAuthenticated(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		CLI:    rejectedToken{},
		Git: &GitConfig{
			Repos:     []RepoSpec{{Name: "board", URL: remote.URL}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.gitBE.git.pushDelay = 0

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfg struct {
		Authenticated  bool `json:"authenticated"`
		TokenAvailable bool `json:"tokenAvailable"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if cfg.Authenticated || cfg.TokenAvailable {
		t.Fatalf("authenticated = %v, tokenAvailable = %v; a refused token must show the banner", cfg.Authenticated, cfg.TokenAvailable)
	}
}

// This package's tests run with the network shut off: the default
// transport refuses anything but loopback, so a case that reaches for a
// real forge through it fails on the request rather than answering with
// whatever token the machine exports. Clients built from an httptest
// server carry their own transport and are not covered — the fakes here
// hand out the server rather than a client, unlike the ones in cmd/aeman
// and internal/tokenstore, which wrap what they hand over.
func TestMain(m *testing.M) {
	restore := nonet.Block()
	code := m.Run()
	restore()
	os.Exit(code)
}
