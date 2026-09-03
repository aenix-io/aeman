package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/nonet"
	"github.com/aenix-io/aeman/internal/server"
	"github.com/aenix-io/aeman/internal/tokenstore"
	"github.com/aenix-io/aeman/internal/tokenstore/tokenstoretest"
)

// The board's forge is read off the primary repository's host, and the flag
// or AEMAN_FORGE overrides it; a self-hosted GitLab on a host that does not
// say so is named by --gitlab-url. The config carries the forge so every
// command (serve, mcp, init) speaks the right dialect.
func TestGitConfigNamesTheForge(t *testing.T) {
	cases := []struct {
		args []string
		env  map[string]string
		want forge.Kind
	}{
		{[]string{"--repo", "https://github.com/acme/aeman-db.git"}, nil, forge.GitHub},
		{[]string{"--repo", "https://gitlab.com/kvaps/aeman-db.git"}, nil, forge.GitLab},
		{[]string{"--repo", "https://gitlab.example.org/team/board.git"}, nil, forge.GitLab},
		{[]string{"--repo", "https://git.example.org/team/board.git"}, nil, forge.GitHub},
		{[]string{"--repo", "https://git.example.org/team/board.git", "--gitlab-url", "https://git.example.org"}, nil, forge.GitLab},
		{[]string{"--repo", "https://git.example.org/team/board.git", "--forge", "gitlab"}, nil, forge.GitLab},
		{[]string{"--repo", "https://gitlab.com/kvaps/aeman-db.git", "--forge", "github"}, nil, forge.GitHub},
		{[]string{"--repo", "https://git.example.org/team/board.git"}, map[string]string{"AEMAN_FORGE": "gitlab"}, forge.GitLab},
		{[]string{"--repo", "https://git.example.org/team/board.git"}, map[string]string{"AEMAN_GITLAB_URL": "https://git.example.org"}, forge.GitLab},
	}
	for _, tc := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		gf := addGitFlags(fs, func(k string) string { return tc.env[k] })
		if err := fs.Parse(tc.args); err != nil {
			t.Fatal(err)
		}
		cfg, err := gf.config()
		if err != nil {
			t.Fatalf("%v %v: %v", tc.args, tc.env, err)
		}
		if cfg.Forge == nil || cfg.Forge.Kind() != tc.want {
			t.Errorf("%v %v: forge = %v, want %s", tc.args, tc.env, cfg.Forge, tc.want)
		}
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	gf := addGitFlags(fs, func(string) string { return "" })
	if err := fs.Parse([]string{"--repo", "https://gitlab.com/x/y.git", "--forge", "gitea"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gf.config(); err == nil || !strings.Contains(err.Error(), "forge") {
		t.Fatalf("an unknown forge must be refused: %v", err)
	}
}

type fakeCLI struct {
	token, login string
	err          error
	calls        int32
}

// The counter is atomic because this stands in a chain that a concurrent
// test drives from several goroutines; every reader of it is sequential.
func (f *fakeCLI) Token(context.Context) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.token, f.err
}
func (f *fakeCLI) Login(context.Context) (string, error) { return f.login, f.err }

// The token order per forge: the forge's own environment variables first
// (GITHUB_TOKEN/GH_TOKEN, GITLAB_TOKEN), then the CLI; an empty CLI answer
// names the login command of that forge's tool.
func TestResolveForgeTokenPrefersTheEnvironmentThenTheCLI(t *testing.T) {
	ctx := context.Background()
	// Both forges answer /user for any token, because what is under test
	// is which SOURCE the credential comes from, not whether a forge
	// accepts it. Pointed at the real hosts, the environment's token is
	// refused there and its source retired, and the test would pass or
	// fail on whether the machine can reach gitlab.com.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/user") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"login":"alice","username":"alice"}`)
	}))
	t.Cleanup(srv.Close)
	gl := forge.NewGitLab(srv.URL)
	gh := forge.NewGitHubAt(srv.URL)
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	// The order under test now lives in the chain, and the chain is what
	// production passes, so the test builds one rather than a bare fake:
	// asking a plain forge.CLI here would prove the order of a call site
	// that no longer exists.
	with := func(f forge.Forge, cli forge.CLI, e func(string) string) forge.CLI {
		log, _ := testLog()
		return &chain{log: log, forge: f, sources: []forge.CLI{newEnvCLI(f, e, srv.Client()), cli}}
	}

	cli := &fakeCLI{token: "cli-token"}
	if tok, err := resolveForgeToken(ctx, gl, with(gl, cli, env(map[string]string{"GITLAB_TOKEN": " env-token "}))); err != nil || tok != "env-token" || cli.calls != 0 {
		t.Fatalf("gitlab env: %q %v calls=%d", tok, err, cli.calls)
	}
	if tok, err := resolveForgeToken(ctx, gl, with(gl, cli, env(map[string]string{"GITHUB_TOKEN": "wrong-forge"}))); err != nil || tok != "cli-token" || cli.calls != 1 {
		t.Fatalf("gitlab ignores GITHUB_TOKEN and asks glab: %q %v calls=%d", tok, err, cli.calls)
	}
	if tok, err := resolveForgeToken(ctx, gh, with(gh, cli, env(map[string]string{"GH_TOKEN": "gh-env"}))); err != nil || tok != "gh-env" {
		t.Fatalf("github env: %q %v", tok, err)
	}
	empty := &fakeCLI{token: "  "}
	if _, err := resolveForgeToken(ctx, gl, with(gl, empty, env(nil))); err == nil || !strings.Contains(err.Error(), "glab auth login") || !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Fatalf("empty glab token: %v", err)
	}
	broken := &fakeCLI{err: errors.New("glab: not logged in")}
	if _, err := resolveForgeToken(ctx, gl, with(gl, broken, env(nil))); err == nil || !strings.Contains(err.Error(), "glab auth login") {
		t.Fatalf("a tool with nothing to give must still name the login commands: %v", err)
	}
}

// In the OAuth mode the server needs a credential for EVERY repository of
// the board — it pushes them and asks the forge who may read them with its
// own — but a board whose domains each carry their own needs no shared one.
// The error names the domains that lack one and the variables that would
// give them one.
func TestMissingTokensNamesTheDomainsWithoutACredential(t *testing.T) {
	all := &server.GitConfig{Repos: []server.RepoSpec{
		{Name: "aeman-db", Token: "a"}, {Name: "founders", Token: "b"},
	}}
	if got := missingTokens(all); len(got) != 0 {
		t.Fatalf("every domain has a token; missing = %v", got)
	}
	some := &server.GitConfig{Repos: []server.RepoSpec{
		{Name: "aeman-db", Token: "a"}, {Name: "founders"}, {Name: "odd.name"},
	}}
	got := missingTokens(some)
	if len(got) != 2 || got[0] != "founders (AEMAN_GIT_TOKEN_FOUNDERS)" || got[1] != "odd.name (AEMAN_GIT_TOKEN_ODD_NAME)" {
		t.Fatalf("missing = %v; want the two domains with the variables that would fill them", got)
	}
	none := &server.GitConfig{Repos: []server.RepoSpec{{Name: "aeman-db"}}}
	if got := missingTokens(none); len(got) != 1 {
		t.Fatalf("missing = %v", got)
	}
}

// One OAuth application per board, and it must belong to the board's forge.
func TestOAuthPairMustMatchTheForge(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	gl := forge.NewGitLab("https://gitlab.com")
	gh := forge.NewGitHub()
	if id, sec, err := oauthPair(gl, env(map[string]string{"AEMAN_GITLAB_CLIENT_ID": "i", "AEMAN_GITLAB_CLIENT_SECRET": "s"})); err != nil || id != "i" || sec != "s" {
		t.Fatalf("gitlab pair: %q %q %v", id, sec, err)
	}
	if _, _, err := oauthPair(gh, env(map[string]string{"AEMAN_GITLAB_CLIENT_ID": "i", "AEMAN_GITLAB_CLIENT_SECRET": "s"})); err == nil {
		t.Fatal("a GitLab application on a GitHub board must be refused")
	}
	if _, _, err := oauthPair(gl, env(map[string]string{"AEMAN_GITHUB_CLIENT_ID": "i", "AEMAN_GITHUB_CLIENT_SECRET": "s"})); err == nil {
		t.Fatal("a GitHub application on a GitLab board must be refused")
	}
	if _, _, err := oauthPair(gl, env(map[string]string{"AEMAN_GITHUB_CLIENT_ID": "i", "AEMAN_GITHUB_CLIENT_SECRET": "s",
		"AEMAN_GITLAB_CLIENT_ID": "i", "AEMAN_GITLAB_CLIENT_SECRET": "s"})); err == nil {
		t.Fatal("both pairs must be refused")
	}
	if id, _, err := oauthPair(gh, env(nil)); err != nil || id != "" {
		t.Fatalf("no pair is local mode, not an error: %q %v", id, err)
	}
	if cliFor(gl, nil, nil, nil) == nil || cliFor(gh, nil, nil, nil) == nil {
		t.Fatal("every forge has a CLI")
	}
}

// fakeForge answers /user with the login a token belongs to and 401 for a
// token it does not know, so a chain's Login can be checked without a real
// GitHub.
func fakeForge(t *testing.T, logins map[string]string) (forge.Forge, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login, ok := logins[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if r.URL.Path != "/user" || !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"login":"`+login+`"}`)
	}))
	t.Cleanup(srv.Close)
	return forge.NewGitHubAt(srv.URL), srv.Client()
}

// testLog is a debug-level logger over a buffer, so a test can assert on
// what the chain said about a source it skipped.
func testLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// The keychain comes before the forge's CLI: a machine that ran `aeman
// login` never shells out to gh at all, which is the point — an MCP client
// starts the process with no environment and no terminal.
func TestChainPrefersKeychainOverCLI(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"ghp_stored": "alice"})
	store := tokenstoretest.NewFake().Put("github.com", "ghp_stored")
	cli := &fakeCLI{token: "cli-token"}
	log, _ := testLog()

	c := &chain{sources: []forge.CLI{tokenstore.NewCLI(store, f, client), cli}, log: log}
	if tok, err := c.Token(ctx); err != nil || tok != "ghp_stored" {
		t.Fatalf("Token = %q, %v; want the stored one", tok, err)
	}
	if cli.calls != 0 {
		t.Fatalf("gh was asked %d times while the keychain had a token, want 0", cli.calls)
	}

	// And cliFor is what puts it there: the same store, reached through the
	// chain the commands actually build.
	if tok, err := cliFor(f, store, func(string) string { return "" }, log).Token(ctx); err != nil || tok != "ghp_stored" {
		t.Fatalf("cliFor(...).Token = %q, %v; want the stored token", tok, err)
	}
}

// A store with nothing in it, and a store that failed for any other
// reason, are the same answer to the chain: nothing here, ask gh. None of
// them warns — they are the normal state of a machine that never ran
// `aeman login`, and on macOS the store cannot tell them apart anyway,
// since everything but a missing item arrives as whatever
// /usr/bin/security printed.
func TestChainFallsThroughWhenTheStoreHasNothingToGive(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, nil)
	for _, storeErr := range []error{
		tokenstore.ErrNotFound,
		errors.New("keychain: darwin cli: security find-generic-password: exit status 36"),
		errors.New("keychain: darwin cli: security find-generic-password: exit status 51"),
	} {
		store := tokenstoretest.NewFake()
		store.Fail(storeErr)
		cli := &fakeCLI{token: "cli-token"}
		log, buf := testLog()

		c := &chain{log: log, forge: forge.NewGitHub(), sources: []forge.CLI{tokenstore.NewCLI(store, f, client), cli}}
		if tok, err := c.Token(ctx); err != nil || tok != "cli-token" {
			t.Fatalf("%v: Token = %q, %v; want gh's", storeErr, tok, err)
		}
		if strings.Contains(buf.String(), "level=WARN") {
			t.Errorf("%v: a store with nothing to give must not warn: %s", storeErr, buf.String())
		}
	}
}

// The login comes from whoever supplied the token. A stored bot token and a
// gh signed in as a person are two different people, and the one the
// commits are attributed to must be the one the push is made with.
func TestChainLoginFollowsTheTokenSource(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"ghp_bot": "aeman-bot"})
	log, _ := testLog()

	stored := &chain{log: log, sources: []forge.CLI{
		tokenstore.NewCLI(tokenstoretest.NewFake().Put("github.com", "ghp_bot"), f, client),
		&fakeCLI{token: "cli-token", login: "machine-user"},
	}}
	if _, err := stored.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if login, err := stored.Login(ctx); err != nil || login != "aeman-bot" {
		t.Fatalf("Login = %q, %v; want the stored token's owner", login, err)
	}

	empty := &chain{log: log, sources: []forge.CLI{
		tokenstore.NewCLI(tokenstoretest.NewFake(), f, client),
		&fakeCLI{token: "cli-token", login: "machine-user"},
	}}
	if _, err := empty.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if login, err := empty.Login(ctx); err != nil || login != "machine-user" {
		t.Fatalf("Login = %q, %v; want gh's", login, err)
	}
}

// `aeman mcp` asks for the login before anything asks for a token, so the
// election cannot wait for the first Token call: Login runs it itself, and
// the source it elects is the one that answers every later Token.
func TestChainLoginElectsTheSourceWhenTokenWasNotAskedFirst(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"ghp_bot": "aeman-bot"})
	cli := &fakeCLI{token: "cli-token", login: "machine-user"}
	log, _ := testLog()

	c := &chain{log: log, sources: []forge.CLI{
		tokenstore.NewCLI(tokenstoretest.NewFake().Put("github.com", "ghp_bot"), f, client), cli,
	}}
	if login, err := c.Login(ctx); err != nil || login != "aeman-bot" {
		t.Fatalf("Login = %q, %v; want the stored token's owner", login, err)
	}
	if tok, err := c.Token(ctx); err != nil || tok != "ghp_bot" {
		t.Fatalf("Token after Login = %q, %v; want the elected source's", tok, err)
	}
	if cli.calls != 0 {
		t.Fatalf("gh was asked %d times, want 0", cli.calls)
	}
}

// With no token in any source the person is told how to get one. That
// message only survives if no source's own complaint displaces it, and on
// a machine without gh there is always a complaint: the real tool answers
// a missing binary or a missing login with an error, never with an empty
// string, so the fakes here do the same. What must reach the person is the
// message naming the commands, not exec's "file not found".
func TestChainWithoutATokenAnywhereNamesTheLoginCommand(t *testing.T) {
	ctx := context.Background()
	gh := forge.NewGitHub()
	f, client := fakeForge(t, nil)

	for _, cli := range []*fakeCLI{
		{err: errors.New(`read token from gh: exec: "gh": executable file not found in $PATH`)},
		{err: errors.New("gh returned an empty token; run `gh auth login`")},
		{token: "  "},
	} {
		log, buf := testLog()
		c := &chain{log: log, forge: gh, sources: []forge.CLI{
			tokenstore.NewCLI(tokenstoretest.NewFake(), f, client), cli,
		}}
		_, err := resolveForgeToken(ctx, gh, c)
		if err == nil || !strings.Contains(err.Error(), "aeman login") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
			t.Fatalf("%v: error = %v; want the one naming the login commands", cli.err, err)
		}
		if strings.Contains(buf.String(), "level=WARN") {
			t.Errorf("%v: a tool that has no token to give must not warn: %s", cli.err, buf.String())
		}
	}

	// And it is an ERROR, not an empty string with no error. The server
	// asks the chain directly to decide whether it has a credential at all,
	// so a silent empty answer would read there as "signed in" and hide the
	// banner that says what is missing.
	log, _ := testLog()
	empty := &chain{log: log, forge: gh, sources: []forge.CLI{
		newEnvCLI(gh, func(string) string { return "" }, client),
		tokenstore.NewCLI(tokenstoretest.NewFake(), f, client),
		&fakeCLI{err: errors.New(`exec: "gh": executable file not found in $PATH`)},
	}}
	tok, err := empty.Token(ctx)
	if err == nil {
		t.Fatalf("Token with nothing anywhere = %q, nil; want an error", tok)
	}
	if !strings.Contains(err.Error(), "aeman login") {
		t.Fatalf("Token error = %v; want the message naming the login commands", err)
	}
}

// `aeman login` and `aeman serve` must agree on which item they are
// talking about, and they do so by construction: both read the account off
// the forge value Detect built from the same flags, so there is no second
// parse of a URL to drift from the first. The case that used to drift is
// the last one — a repository URL with no host in it, plus a GitLab named
// on its own.
func TestLoginAndServeAgreeOnTheKeychainAccount(t *testing.T) {
	for _, args := range [][]string{
		{"--repo", "https://github.com/acme/board.git"},
		{"--repo", "https://gitlab.example.org/a/b.git"},
		{"--repo", "git@host.example:a/b.git"},
		{"--gitlab-url", "https://gitlab.example.org"},
		{"--repo", "board=/srv/board.git", "--gitlab-url", "https://gitlab.example.org"},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		gf := addGitFlags(fs, func(string) string { return "" })
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		_, account, err := gf.forgeTarget()
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		cfg, err := gf.config()
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if cfg == nil {
			continue // no repository: only login has an opinion, and it is the one above
		}
		if served := cfg.Forge.Host(); served != account {
			t.Errorf("%v: login writes %q, serve reads %q", args, account, served)
		}
	}
}

// The environment's source bounds its forge call for the reason the
// keychain's does: Login holds a lock while it asks, and the default HTTP
// client has no timeout at all.
func TestEnvCLIWithoutAClientBoundsTheForgeCall(t *testing.T) {
	c := newEnvCLI(forge.NewGitHub(), func(string) string { return "" }, nil)
	if c.client == http.DefaultClient {
		t.Fatal("http.DefaultClient has no timeout; a wedged forge would pin the lock")
	}
	if c.client.Timeout <= 0 {
		t.Fatalf("client timeout = %v, want a bound", c.client.Timeout)
	}
}

// A winner that stops answering is not a winner any more. `aeman logout`
// empties the store a running server elected, and the chain has to move on
// rather than report "no token" for as long as the process lives —
// remembering a source exists to keep the token and the login on ONE
// source, not to stop looking. An empty answer triggers that as much as an
// error does, which is the same guard the election itself applies: the
// environment's source is the one that can hand back "" with no error at
// all.
func TestChainReElectsWhenTheWinnerHasNothingLeft(t *testing.T) {
	ctx := context.Background()
	gh := forge.NewGitHub()

	for _, tc := range []struct {
		name  string
		empty func(*fakeCLI)
	}{
		{"the token is gone", func(c *fakeCLI) { c.token = "" }},
		{"the source now fails", func(c *fakeCLI) { c.token, c.err = "", tokenstore.ErrNotFound }},
	} {
		log, _ := testLog()
		first := &fakeCLI{token: "first-token", login: "first-user"}
		second := &fakeCLI{token: "second-token", login: "second-user"}
		c := &chain{log: log, forge: gh, sources: []forge.CLI{first, second}}

		if tok, err := c.Token(ctx); err != nil || tok != "first-token" {
			t.Fatalf("%s: Token = %q, %v; want the first source's", tc.name, tok, err)
		}
		if login, err := c.Login(ctx); err != nil || login != "first-user" {
			t.Fatalf("%s: Login = %q, %v; want the first source's", tc.name, login, err)
		}

		tc.empty(first)

		if tok, err := c.Token(ctx); err != nil || tok != "second-token" {
			t.Fatalf("%s: Token after the winner emptied = %q, %v; want the next source's", tc.name, tok, err)
		}
		// And the login moves with it, or the push and the name on the
		// commits come from two different accounts.
		if login, err := c.Login(ctx); err != nil || login != "second-user" {
			t.Fatalf("%s: Login after the winner emptied = %q, %v; want the next source's", tc.name, login, err)
		}
	}
}

// `aeman migrate` reads a Projects v2 board, which is always on GitHub
// whatever forge the destination repository lives on, so its credential is
// looked up under github.com and not under the board's host. That pin was
// prose until now: a store holding only a github.com item answers it, and
// answers it once.
func TestMigrateResolvesItsTokenFromTheGithubComAccount(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	log, _ := testLog()
	// A fake forge: asking for a token now asks who it belongs to, and a
	// test must not reach api.github.com or the machine's own gh for that.
	f, _ := fakeForge(t, map[string]string{"ghp_for_migrate": "alice"})
	store := tokenstoretest.NewFake().Put("github.com", "ghp_for_migrate")

	tok, err := resolveGitHubToken(context.Background(), f, store, log)
	if err != nil || tok != "ghp_for_migrate" {
		t.Fatalf("resolveGitHubToken = %q, %v; want the github.com item", tok, err)
	}
	if store.Gets() != 1 {
		t.Fatalf("the store was read %d times, want 1 — a miss here would fall through to gh", store.Gets())
	}
}

// The environment's source answers for its token the way the keychain's
// does: a forge that rejects it says so, and an environment holding
// nothing names no one rather than naming the wrong person.
func TestEnvCLIAnswersForItsOwnToken(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"ghp_good": "alice"})

	good := newEnvCLI(f, func(string) string { return "ghp_good" }, client)
	if login, err := good.Login(ctx); err != nil || login != "alice" {
		t.Fatalf("Login = %q, %v; want the token's owner", login, err)
	}

	revoked := newEnvCLI(f, func(string) string { return "ghp_revoked" }, client)
	if _, err := revoked.Login(ctx); !errors.Is(err, forge.ErrBadToken) {
		t.Fatalf("Login with a rejected token = %v, want forge.ErrBadToken", err)
	}
	if _, err := revoked.Login(ctx); !errors.Is(err, forge.ErrBadToken) {
		t.Fatalf("a failed login must not be cached: %v", err)
	}

	empty := newEnvCLI(f, func(string) string { return "" }, client)
	if tok, err := empty.Token(ctx); err != nil || tok != "" {
		t.Fatalf("Token = %q, %v; an empty environment is nothing to report, not a failure", tok, err)
	}
	if _, err := empty.Login(ctx); err == nil {
		t.Fatal("an empty environment must name no one rather than answer")
	}
}

// A forge that cannot be reached is not a missing credential. The pair's
// promise is that the token and the name come from ONE source, not that
// both succeed: when the elected source holds a token and the forge
// cannot say whose it is, the token is handed over with no name. Failing
// the pair instead would take every read down on a transient outage and
// tell the person to run `aeman login` for a token they already have.
func TestTheChainKeepsTheTokenWhenTheForgeCannotBeAsked(t *testing.T) {
	ctx := context.Background()
	// A forge whose /user never answers: nothing is listening on the port.
	dead := forge.NewGitHubAt("http://127.0.0.1:1")
	log, _ := testLog()
	store := tokenstoretest.NewFake().Put("github.com", "ghp_stored")

	c := &chain{log: log, forge: dead, sources: []forge.CLI{
		tokenstore.NewCLI(store, dead, &http.Client{Timeout: time.Second}),
	}}
	tok, login, err := c.TokenAndLogin(ctx)
	if err != nil {
		t.Fatalf("TokenAndLogin = %q, %q, %v; the token is there, only its owner is unknown", tok, login, err)
	}
	if tok != "ghp_stored" {
		t.Fatalf("token = %q, want the stored one", tok)
	}
	if login != "" {
		t.Fatalf("login = %q, want empty — nobody answered for it", login)
	}
}

// A source that answers the pair with neither a token nor a refusal has
// nothing to give, and the chain must say so with an error rather than
// hand back an empty token and nil — the shape the server reads as signed
// in, banner suppressed, board blank.
type emptyPair struct{}

func (emptyPair) Token(context.Context) (string, error) { return "tok", nil }
func (emptyPair) Login(context.Context) (string, error) { return "", errors.New("forge unreachable") }
func (emptyPair) TokenAndLogin(context.Context) (string, string, error) {
	return "", "", errors.New("forge unreachable")
}

// silentPair wins the election with a token and then hands the pair back
// a login with no credential under it, saying nothing about why. That is
// a source emptying between the two calls — `aeman logout` in another
// terminal — and it is the only way the shape reaches pairOnce at all,
// since electExcept refuses to elect a source whose Token is empty.
type silentPair struct{}

func (silentPair) Token(context.Context) (string, error) { return "tok", nil }
func (silentPair) Login(context.Context) (string, error) { return "someone", nil }
func (silentPair) TokenAndLogin(context.Context) (string, string, error) {
	return "", "someone", nil
}

func TestAnEmptyPairIsAnErrorNotASilentSuccess(t *testing.T) {
	// Both halves: a source that reports why it has nothing, and one that
	// reports nothing at all. The second is the dangerous one — ("", nil)
	// is the shape Token forbids, and the server reads it as signed in,
	// so the banner that would name the missing credential never shows
	// and the board 401s instead.
	for _, src := range []forge.CLI{emptyPair{}, silentPair{}} {
		log, _ := testLog()
		c := &chain{log: log, forge: forge.NewGitHub(), sources: []forge.CLI{src}}
		tok, login, err := c.TokenAndLogin(context.Background())
		if err == nil {
			t.Fatalf("%T: TokenAndLogin = %q, %q, nil; an empty token with no error is the one shape forbidden", src, tok, login)
		}
		if tok != "" {
			t.Fatalf("%T: token = %q, want empty alongside the error", src, tok)
		}
	}
}

// A source whose token the forge REFUSES has nothing left to give, which
// ends its reign like an empty answer does — and the chain asks the next
// one. Without this, a stale GITHUB_TOKEN in front of a working keychain
// or gh hides both: `serve` reports itself unauthenticated, the board
// never loads, and the banner names the two sources that are below the
// one that already won.
func TestARefusedTokenEndsTheReignAndTheNextSourceAnswers(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"ghp_good": "alice"})
	log, _ := testLog()

	c := &chain{log: log, forge: f, sources: []forge.CLI{
		// A stale variable the forge no longer accepts.
		newEnvCLI(f, func(string) string { return "ghp_stale" }, client),
		tokenstore.NewCLI(tokenstoretest.NewFake().Put("github.com", "ghp_good"), f, client),
	}}
	tok, login, err := c.TokenAndLogin(ctx)
	if err != nil {
		t.Fatalf("TokenAndLogin = %q, %q, %v; the keychain below holds a good token", tok, login, err)
	}
	if tok != "ghp_good" || login != "alice" {
		t.Fatalf("token = %q, login = %q; want the next source's pair", tok, login)
	}
}

// With nothing left below it, the refusal is what the person is told —
// not "no token anywhere", which would send them to add one they have.
func TestARefusedTokenWithNothingBelowItSurfaces(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"ghp_good": "alice"})
	log, _ := testLog()

	c := &chain{log: log, forge: f, sources: []forge.CLI{
		tokenstore.NewCLI(tokenstoretest.NewFake().Put("github.com", "ghp_revoked"), f, client),
	}}
	if _, _, err := c.TokenAndLogin(ctx); !errors.Is(err, forge.ErrBadToken) {
		t.Fatalf("TokenAndLogin = %v, want forge.ErrBadToken", err)
	}
}

// A forge that REFUSES the token is the opposite case and must not be
// softened into it. The credential is dead, and answering "here it is,
// nobody knows whose" leaves the board reporting itself signed in, no
// banner, and a 401 on every read with nothing on the page saying why.
func TestTheChainSurfacesARejectedToken(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"ghp_good": "alice"})
	log, _ := testLog()
	store := tokenstoretest.NewFake().Put("github.com", "ghp_revoked")

	c := &chain{log: log, forge: f, sources: []forge.CLI{tokenstore.NewCLI(store, f, client)}}
	tok, login, err := c.TokenAndLogin(ctx)
	if !errors.Is(err, forge.ErrBadToken) {
		t.Fatalf("TokenAndLogin = %q, %q, %v; want forge.ErrBadToken", tok, login, err)
	}
}

// switchableCLI is a source whose token can be taken away from another
// goroutine, with no cache in the way — which the keychain source has, so
// using that one here would never reach the fall-through this is about.
type switchableCLI struct {
	mu         sync.Mutex
	token      string
	login      string
	asked      int
	emptyAfter int
}

// Token empties the source once it has been asked emptyAfter times, so
// the fall-through happens at a fixed point in the run rather than after
// a sleep — a wall-clock trigger races the goroutines and made this pass
// only under the race detector, which slows them down enough.
func (c *switchableCLI) Token(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked++
	if c.emptyAfter > 0 && c.asked > c.emptyAfter {
		c.token, c.login = "", ""
	}
	return c.token, nil
}

func (c *switchableCLI) Login(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		return "", errors.New("nothing to name")
	}
	return c.login, nil
}

// The pairing is what the chain exists for, and the server asks for it on
// every request — so it is asked concurrently, and a re-election racing a
// read is exactly the window that would hand back one source's token with
// another's name. Every answer must be a matching pair, whichever source
// won by the time it was taken, and the fall-through must really happen:
// the first source is emptied under the readers the way `aeman logout`
// empties one under a running server.
func TestTheTokenAndTheLoginStayTogetherUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	log, _ := testLog()
	first := &switchableCLI{token: "first-token", login: "first-user", emptyAfter: 30}
	second := &fakeCLI{token: "second-token", login: "second-user"}
	c := &chain{log: log, forge: forge.NewGitHub(), sources: []forge.CLI{first, second}}
	owners := map[string]string{"first-token": "first-user", "second-token": "second-user"}

	var wg sync.WaitGroup
	var pairs atomic.Int32
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				tok, login, err := c.TokenAndLogin(ctx)
				if err != nil {
					continue // a moment with no source is allowed; a mismatched pair is not
				}
				want, known := owners[tok]
				if !known {
					t.Errorf("token %q came from nowhere", tok)
					return
				}
				// An empty login is the documented answer for "the owner
				// could not be established" — a source emptied between the
				// two reads, the same shape as an unreachable forge. What
				// must never happen is a login that belongs to a DIFFERENT
				// token than the one handed back.
				if login != "" && login != want {
					t.Errorf("token %q came back with login %q, want %q — the pair is from two sources", tok, login, want)
					return
				}
				if login != "" {
					pairs.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	if pairs.Load() == 0 {
		t.Fatal("no pair was taken at all; the test proved nothing")
	}
	// The fall-through really happened, or this only tested one source.
	if atomic.LoadInt32(&second.calls) == 0 {
		t.Fatal("the second source was never reached: the re-election this is about did not occur")
	}
}

// `aeman mcp` only ever asks for a login — its personal-board attach, its
// ResolveLogin and its middleware all call Login and never Token. So the
// refusal fall-through has to live there too, or a stored token that has
// expired leaves that process with no identity for its whole life: no
// personal board, the default list not scoped to the person, and every
// commit unattributed, with a working gh sitting right underneath.
func TestLoginAloneAlsoFallsThroughARefusedToken(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"cli-token": "machine-user"})
	log, _ := testLog()

	c := &chain{log: log, forge: f, sources: []forge.CLI{
		tokenstore.NewCLI(tokenstoretest.NewFake().Put("github.com", "ghp_revoked"), f, client),
		&fakeCLI{token: "cli-token", login: "machine-user"},
	}}
	for i := range 3 {
		login, err := c.Login(ctx)
		if err != nil || login != "machine-user" {
			t.Fatalf("Login call %d = %q, %v; want the source below the revoked one", i+1, login, err)
		}
	}
}

// The owner of the environment's token is asked for ONCE, through either
// entry point. The chain prefers the pair, so a guard living only in
// Login is a guard production never reaches: every request would then put
// a /user call on the wire, under this source's lock, where the keychain's
// peer makes one for the life of the token. Counting the calls is the
// only assertion that catches it — asserting the login's value passes
// either way, which is why it went unnoticed.
func TestEnvCLIAsksTheForgeOncePerToken(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		_, _ = io.WriteString(w, `{"login":"alice"}`)
	}))
	t.Cleanup(srv.Close)
	f := forge.NewGitHubAt(srv.URL)

	c := newEnvCLI(f, func(string) string { return "ghp_env" }, srv.Client())
	for range 5 {
		if _, login, err := c.TokenAndLogin(ctx); err != nil || login != "alice" {
			t.Fatalf("TokenAndLogin = %q, %v", login, err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times for five pairs, want 1", n)
	}

	// And the other entry point shares the same answer.
	if login, err := c.Login(ctx); err != nil || login != "alice" {
		t.Fatalf("Login = %q, %v", login, err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times, want 1: both entry points share one guard", n)
	}
}

// Every test in this package runs with the network shut off, so a case
// that builds a forge against the real host fails on the dial instead of
// reaching it. Grepping for the constructor was the other option and it
// flags ten harmless uses — a real forge is how a test says "GitHub's
// variable names" — while missing the one shape that matters, a fake-
// looking forge whose base is real.
func TestMain(m *testing.M) {
	restore := nonet.Block()
	// The commands build the real keychain, and logout deletes from it
	// without asking; a test that calls an entry point would do that to
	// whoever is running `go test`. Every test here injects a fake
	// instead, and this makes the other path impossible rather than
	// discouraged.
	openStore = func(*slog.Logger) tokenstore.Store {
		panic("a test reached the real OS keychain: pass a tokenstoretest.Fake instead of calling a command entry point")
	}
	code := m.Run()
	restore()
	os.Exit(code)
}

// The push credential and the name on the commits come from ONE source,
// including when the first source's token is dead. A revoked GITHUB_TOKEN
// in front of a good keychain item used to push with the revoked one —
// resolveForgeToken read the variables itself, ahead of the chain, so the
// forge never got asked whether that token was still any good — while the
// identity came from the keychain below it. The commits then carried a
// name the push did not belong to.
func TestARevokedEnvironmentTokenDoesNotBecomeThePushCredential(t *testing.T) {
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"good-keychain": "alice"})
	store := tokenstoretest.NewFake()
	if err := store.Set("github.com", "good-keychain"); err != nil {
		t.Fatal(err)
	}
	log, _ := testLog()
	c := &chain{log: log, forge: f, sources: []forge.CLI{
		newEnvCLI(f, func(k string) string {
			if k == "GITHUB_TOKEN" {
				return "revoked"
			}
			return ""
		}, client),
		tokenstore.NewCLI(store, f, client),
	}}

	tok, err := resolveForgeToken(ctx, f, c)
	if err != nil || tok != "good-keychain" {
		t.Fatalf("resolveForgeToken = %q, %v; want the keychain's token once the forge refuses the environment's", tok, err)
	}
	login, err := c.Login(ctx)
	if err != nil || login != "alice" {
		t.Fatalf("Login = %q, %v; want the owner of the token that will do the pushing", login, err)
	}
}

// A source that empties between the election and the pair call hands off
// to the next one rather than taking the whole request down. The window
// is real: the keychain source re-reads its token every five minutes, so
// a `aeman logout` in another terminal lands between the two reads, and
// the request in that gap would otherwise be told to set a credential the
// machine already has one of, banner and all.
func TestAnEmptiedWinnerHandsOffInsteadOfEndingTheChain(t *testing.T) {
	log, _ := testLog()
	c := &chain{log: log, forge: forge.NewGitHub(), sources: []forge.CLI{
		silentPair{}, &fakeCLI{token: "below", login: "bob"},
	}}
	tok, login, err := c.TokenAndLogin(context.Background())
	if err != nil || tok != "below" || login != "bob" {
		t.Fatalf("TokenAndLogin = %q, %q, %v; want the source below the emptied one", tok, login, err)
	}
}

// emptyingPair wins the election and then answers the pair with neither a
// token nor a refusal — a store that stopped answering between the two
// reads.
type emptyingPair struct{ err error }

func (emptyingPair) Token(context.Context) (string, error) { return "tok", nil }
func (p emptyingPair) Login(context.Context) (string, error) {
	return "", p.err
}
func (p emptyingPair) TokenAndLogin(context.Context) (string, string, error) {
	return "", "", p.err
}

// Only a REFUSED token is worth reporting in place of the error that names
// the login commands. A source that merely stopped answering has said
// nothing the person can act on — handing them the store's own complaint
// instead tells them what broke and not what to do about it, and the
// commands are the whole point of that error.
func TestAnEmptiedWinnerDoesNotReplaceTheNoTokenError(t *testing.T) {
	log, buf := testLog()
	boom := errors.New("keychain: darwin cli: security find-generic-password: exit status 36")
	c := &chain{log: log, forge: forge.NewGitHub(), sources: []forge.CLI{emptyingPair{err: boom}}}

	_, _, err := c.TokenAndLogin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "aeman login") {
		t.Fatalf("error = %v; want the one naming the login commands", err)
	}
	// The reason still has to be findable, or a store that quietly stops
	// answering leaves nothing behind to debug with — not even at debug
	// level, which is where the election records the same class.
	if !strings.Contains(buf.String(), "exit status 36") {
		t.Fatalf("the store's own complaint went nowhere: %s", buf.String())
	}
}
