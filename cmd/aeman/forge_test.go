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
	"strings"
	"testing"

	"github.com/aenix-io/aeman/internal/forge"
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
	calls        int
}

func (f *fakeCLI) Token(context.Context) (string, error) { f.calls++; return f.token, f.err }
func (f *fakeCLI) Login(context.Context) (string, error) { return f.login, f.err }

// The token order per forge: the forge's own environment variables first
// (GITHUB_TOKEN/GH_TOKEN, GITLAB_TOKEN), then the CLI; an empty CLI answer
// names the login command of that forge's tool.
func TestResolveForgeTokenPrefersTheEnvironmentThenTheCLI(t *testing.T) {
	ctx := context.Background()
	gl := forge.NewGitLab("https://gitlab.com")
	gh := forge.NewGitHub()
	cli := &fakeCLI{token: "cli-token"}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	if tok, err := resolveForgeToken(ctx, gl, cli, env(map[string]string{"GITLAB_TOKEN": " env-token "})); err != nil || tok != "env-token" || cli.calls != 0 {
		t.Fatalf("gitlab env: %q %v calls=%d", tok, err, cli.calls)
	}
	if tok, err := resolveForgeToken(ctx, gl, cli, env(map[string]string{"GITHUB_TOKEN": "wrong-forge"})); err != nil || tok != "cli-token" || cli.calls != 1 {
		t.Fatalf("gitlab ignores GITHUB_TOKEN and asks glab: %q %v calls=%d", tok, err, cli.calls)
	}
	if tok, err := resolveForgeToken(ctx, gh, cli, env(map[string]string{"GH_TOKEN": "gh-env"})); err != nil || tok != "gh-env" {
		t.Fatalf("github env: %q %v", tok, err)
	}
	empty := &fakeCLI{token: "  "}
	if _, err := resolveForgeToken(ctx, gl, empty, env(nil)); err == nil || !strings.Contains(err.Error(), "glab auth login") || !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Fatalf("empty glab token: %v", err)
	}
	broken := &fakeCLI{err: errors.New("glab: not logged in")}
	if _, err := resolveForgeToken(ctx, gl, broken, env(nil)); !errors.Is(err, broken.err) {
		t.Fatalf("cli error must surface: %v", err)
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
	if tok, err := cliFor(forge.NewGitHub(), store, func(string) string { return "" }, log).Token(ctx); err != nil || tok != "ghp_stored" {
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
	env := func(string) string { return "" }

	for _, cli := range []*fakeCLI{
		{err: errors.New(`read token from gh: exec: "gh": executable file not found in $PATH`)},
		{err: errors.New("gh returned an empty token; run `gh auth login`")},
		{token: "  "},
	} {
		log, buf := testLog()
		c := &chain{log: log, sources: []forge.CLI{
			tokenstore.NewCLI(tokenstoretest.NewFake(), f, client), cli,
		}}
		_, err := resolveForgeToken(ctx, gh, c, env)
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
	store := tokenstoretest.NewFake().Put("github.com", "ghp_for_migrate")

	tok, err := resolveGitHubToken(context.Background(), store, log)
	if err != nil || tok != "ghp_for_migrate" {
		t.Fatalf("resolveGitHubToken = %q, %v; want the github.com item", tok, err)
	}
	if store.Gets() != 1 {
		t.Fatalf("the store was read %d times, want 1 — a miss here would fall through to gh", store.Gets())
	}
}
