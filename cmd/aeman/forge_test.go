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
	if cliFor(gl, "https://gitlab.example.org/t/b.git", nil, nil) == nil || cliFor(gh, "https://github.com/a/b.git", nil, nil) == nil {
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
	store := tokenstore.NewFake().Put("github.com", "ghp_stored")
	cli := &fakeCLI{token: "cli-token"}
	log, _ := testLog()

	c := &chain{sources: []forge.CLI{tokenstore.NewCLI(store, f, "github.com", client), cli}, log: log}
	if tok, err := c.Token(ctx); err != nil || tok != "ghp_stored" {
		t.Fatalf("Token = %q, %v; want the stored one", tok, err)
	}
	if cli.calls != 0 {
		t.Fatalf("gh was asked %d times while the keychain had a token, want 0", cli.calls)
	}

	// And cliFor is what puts it there: the same store, reached through the
	// chain the commands actually build.
	if tok, err := cliFor(forge.NewGitHub(), "https://github.com/acme/board.git", store, log).Token(ctx); err != nil || tok != "ghp_stored" {
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
	} {
		store := tokenstore.NewFake()
		store.Err = storeErr
		cli := &fakeCLI{token: "cli-token"}
		log, buf := testLog()

		c := &chain{log: log, sources: []forge.CLI{tokenstore.NewCLI(store, f, "github.com", client), cli}}
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
		tokenstore.NewCLI(tokenstore.NewFake().Put("github.com", "ghp_bot"), f, "github.com", client),
		&fakeCLI{token: "cli-token", login: "machine-user"},
	}}
	if _, err := stored.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if login, err := stored.Login(ctx); err != nil || login != "aeman-bot" {
		t.Fatalf("Login = %q, %v; want the stored token's owner", login, err)
	}

	empty := &chain{log: log, sources: []forge.CLI{
		tokenstore.NewCLI(tokenstore.NewFake(), f, "github.com", client),
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
		tokenstore.NewCLI(tokenstore.NewFake().Put("github.com", "ghp_bot"), f, "github.com", client), cli,
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

// With no token in any source the person needs to be told how to get one,
// not handed the keychain's internal "item not found": an absent store says
// nothing, so the message is still the one that names the login command. A
// source that fails for any other reason does surface its own error.
func TestChainWithoutATokenAnywhereNamesTheLoginCommand(t *testing.T) {
	ctx := context.Background()
	gh := forge.NewGitHub()
	f, client := fakeForge(t, nil)
	log, _ := testLog()
	env := func(string) string { return "" }

	silent := &chain{log: log, sources: []forge.CLI{
		tokenstore.NewCLI(tokenstore.NewFake(), f, "github.com", client), &fakeCLI{token: "  "},
	}}
	_, err := resolveForgeToken(ctx, gh, silent, env)
	if err == nil || !strings.Contains(err.Error(), "gh auth login") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error = %v; want the one naming the login command", err)
	}

	// A gh that is not signed in says so with an error, and that error
	// must not displace this message either: it names the commands, and
	// exec's "file not found" does not.
	broken := &chain{log: log, sources: []forge.CLI{
		tokenstore.NewCLI(tokenstore.NewFake(), f, "github.com", client),
		&fakeCLI{err: errors.New("gh returned an empty token; run `gh auth login`")},
	}}
	if _, err := resolveForgeToken(ctx, gh, broken, env); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error = %v; want the one naming the login command", err)
	}
}

// The keychain account is the forge instance the board lives on, so one
// machine holds a token per forge and `aeman login` writes the item the
// other commands read. It is the repository's host, else a self-hosted
// GitLab named on its own, else github.com.
func TestKeychainAccountIsTheForgeHost(t *testing.T) {
	cases := []struct {
		repoURL, gitlabURL, want string
	}{
		{"https://github.com/acme/board.git", "", "github.com"},
		{"https://gitlab.example.org/a/b.git", "", "gitlab.example.org"},
		{"git@host.example:a/b.git", "", "host.example"},
		{"acme/board", "", "github.com"},
		{"", "https://gitlab.example.org", "gitlab.example.org"},
		{"", "", "github.com"},
	}
	for _, tc := range cases {
		if got := forgeHost(tc.repoURL, tc.gitlabURL); got != tc.want {
			t.Errorf("forgeHost(%q, %q) = %q, want %q", tc.repoURL, tc.gitlabURL, got, tc.want)
		}
	}
}
