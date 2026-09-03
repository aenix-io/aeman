package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/server"
	"github.com/aenix-io/aeman/internal/tokenstore"
	"github.com/aenix-io/aeman/internal/tokenstore/tokenstoretest"
)

// The git-mode flags: repeatable --repo name=url, spans with weeks, a
// committer as "Name <email>", and environment fallbacks for every one of
// them — the compose file configures the server through the environment.

func TestRepoListParsesNameEqualsURL(t *testing.T) {
	var rl repoList
	if err := rl.Set("shared=https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	if err := rl.Set("closed=https://example.com/b.git"); err != nil {
		t.Fatal(err)
	}
	if len(rl) != 2 || rl[0].Name != "shared" || rl[0].URL != "https://example.com/a.git" || rl[1].Name != "closed" {
		t.Fatalf("repos = %+v", rl)
	}
	// A bare URL gets a name from its last path segment.
	if err := rl.Set("https://example.com/org/board.git"); err != nil {
		t.Fatal(err)
	}
	if rl[2].Name != "board" {
		t.Fatalf("bare url named %q, want board", rl[2].Name)
	}
	if err := rl.Set("=nourl"); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if err := rl.Set("dup=https://x/1.git"); err != nil {
		t.Fatal(err)
	}
	if err := rl.Set("dup=https://x/2.git"); err == nil {
		t.Fatal("a repeated name must be refused")
	}
	if rl.String() == "" {
		t.Fatal("String must render the list for --help")
	}
}

// Spans accept weeks — "8w" is the horizon's natural unit — on top of Go's
// durations, and sprints as "4sprints"? No: sprints vary per team; weeks are
// the unit. Anything else is an error, not a silent default.
func TestParseSpan(t *testing.T) {
	cases := map[string]time.Duration{
		"8w":  8 * 7 * 24 * time.Hour,
		"1w":  7 * 24 * time.Hour,
		"36h": 36 * time.Hour,
		"15s": 15 * time.Second,
		"90d": 90 * 24 * time.Hour,
	}
	for in, want := range cases {
		got, err := parseSpan(in)
		if err != nil || got != want {
			t.Fatalf("parseSpan(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "eight weeks", "8", "-1w"} {
		if _, err := parseSpan(bad); err == nil {
			t.Fatalf("parseSpan(%q) must fail", bad)
		}
	}
}

func TestParseCommitter(t *testing.T) {
	id, err := parseCommitter("aeman <aeman@aenix.io>")
	if err != nil || id.Name != "aeman" || id.Email != "aeman@aenix.io" {
		t.Fatalf("%+v %v", id, err)
	}
	if _, err := parseCommitter("no email here"); err == nil {
		t.Fatal("a committer without <email> must be refused")
	}
}

// gitConfigFromFlags returns nil when no repository is configured — the
// server then runs as before — and otherwise a config with every default
// and every environment fallback applied.
func TestGitConfigFromFlagsAndEnv(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	env := map[string]string{}
	gf := addGitFlags(fs, func(k string) string { return env[k] })
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if cfg, err := gf.config(); err != nil || cfg != nil {
		t.Fatalf("no repos: cfg = %+v, err = %v; want nil, nil", cfg, err)
	}

	// Environment only, the way compose sets it.
	fs = flag.NewFlagSet("test", flag.ContinueOnError)
	env = map[string]string{
		"AEMAN_REPOS":         "shared=https://x/a.git,closed=https://x/b.git",
		"AEMAN_GIT_TOKEN":     "tok",
		"AEMAN_DATA":          "/data",
		"AEMAN_HISTORY":       "2w",
		"AEMAN_SYNC_INTERVAL": "30s",
		"AEMAN_UNPUSHED_WARN": "10m",
		"AEMAN_COMMITTER":     "bot <bot@x>",
		"AEMAN_AUTHOR_EMAIL":  "{login}@x.example",
	}
	gf = addGitFlags(fs, func(k string) string { return env[k] })
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := gf.config()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 2 || cfg.Repos[0].Name != "shared" || cfg.Repos[1].URL != "https://x/b.git" {
		t.Fatalf("repos = %+v", cfg.Repos)
	}
	if cfg.Token != "tok" || cfg.DataDir != "/data" || cfg.History != 14*24*time.Hour || cfg.SyncInterval != 30*time.Second || cfg.UnpushedWarn != 10*time.Minute {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Committer.Name != "bot" || cfg.Committer.Email != "bot@x" || cfg.AuthorEmail != "{login}@x.example" {
		t.Fatalf("identity = %+v %q", cfg.Committer, cfg.AuthorEmail)
	}

	// Flags win over the environment; defaults fill the rest.
	fs = flag.NewFlagSet("test", flag.ContinueOnError)
	env = map[string]string{"AEMAN_REPOS": "old=https://x/old.git", "AEMAN_HISTORY": "1w"}
	gf = addGitFlags(fs, func(k string) string { return env[k] })
	if err := fs.Parse([]string{"--repo", "new=https://x/new.git", "--history", "8w"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = gf.config()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].Name != "new" {
		t.Fatalf("flags must win: %+v", cfg.Repos)
	}
	if cfg.History != 8*7*24*time.Hour || cfg.SyncInterval != 15*time.Second || cfg.UnpushedWarn != 5*time.Minute || cfg.Committer.Name != "aeman" {
		t.Fatalf("defaults: %+v", cfg)
	}
	if cfg.DataDir == "" {
		t.Fatal("a data dir default is required")
	}
}

// With only the repository given, the background horizon defaults to two
// weeks: a sprint and the one before it are what the boards look back at, and
// a card's log deepens on demand past that (up to --history-max, a year).
func TestGitConfigDefaultsHorizonToTwoWeeks(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	gf := addGitFlags(fs, func(string) string { return "" })
	if err := fs.Parse([]string{"--repo", "board=https://x/board.git"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := gf.config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.History != 14*24*time.Hour {
		t.Fatalf("default --history = %v, want 2w", cfg.History)
	}
	if cfg.HistoryMax != 365*24*time.Hour || cfg.SyncInterval != 15*time.Second {
		t.Fatalf("other defaults moved: history-max %v, sync-interval %v", cfg.HistoryMax, cfg.SyncInterval)
	}
}

// Every repository of a board may carry its own credential: two boards in
// two organisations cannot share one narrowly-scoped token, and a token wide
// enough for both is wider than either needs. The variable is named after the
// domain — AEMAN_GIT_TOKEN_<NAME>, the name upper-cased with anything but a
// letter or a digit as an underscore — and AEMAN_GIT_TOKEN stands in for the
// domains that name none.
func TestGitConfigTakesAPerDomainToken(t *testing.T) {
	env := map[string]string{
		"AEMAN_GIT_TOKEN":           "default-token",
		"AEMAN_GIT_TOKEN_AEMAN_DB":  "org-token",
		"AEMAN_GIT_TOKEN_FOUNDERS":  "founders-token",
		"AEMAN_GIT_TOKEN_ODD_NAME_": "odd-token",
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	gf := addGitFlags(fs, func(k string) string { return env[k] })
	if err := fs.Parse([]string{
		"--repo", "aeman-db=https://github.com/aenix-org/aeman-db.git",
		"--repo", "founders=https://github.com/aenix-founders/aeman-db.git",
		"--repo", "odd.name!=https://github.com/acme/odd.git",
		"--repo", "plain=https://github.com/acme/plain.git",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := gf.config()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"aeman-db":  "org-token",
		"founders":  "founders-token",
		"odd.name!": "odd-token",
		"plain":     "default-token", // no variable of its own
	}
	for _, spec := range cfg.Repos {
		if spec.Token != want[spec.Name] {
			t.Errorf("%s token = %q, want %q", spec.Name, spec.Token, want[spec.Name])
		}
	}
	if cfg.Token != "default-token" {
		t.Fatalf("the shared default = %q", cfg.Token)
	}
}

// Without any AEMAN_GIT_TOKEN the specs carry nothing yet: fillGitToken asks
// the forge's CLI afterwards, and a remote that needs no credential (a file
// path, a public repository) works without one at all.
func TestGitConfigLeavesTokensEmptyWhenTheEnvironmentHasNone(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	gf := addGitFlags(fs, func(string) string { return "" })
	if err := fs.Parse([]string{"--repo", "board=https://x/board.git"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := gf.config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "" || cfg.Repos[0].Token != "" {
		t.Fatalf("tokens = %q / %q, want empty", cfg.Token, cfg.Repos[0].Token)
	}
}

// fillGitToken hands the forge CLI's token to every domain that has none of
// its own — the single-user server keeps working with no environment at all
// — and never overwrites a domain's own credential.
func TestFillGitTokenFillsOnlyTheDomainsWithoutOne(t *testing.T) {
	cfg := &server.GitConfig{
		Repos: []server.RepoSpec{
			{Name: "aeman-db", URL: "https://github.com/acme/a.git", Token: "own-token"},
			{Name: "founders", URL: "https://github.com/acme/b.git"},
		},
		Token: "default-token",
	}
	fillGitToken(context.Background(), cfg, &fakeCLI{token: "cli-token"})
	if cfg.Repos[0].Token != "own-token" || cfg.Repos[1].Token != "default-token" {
		t.Fatalf("tokens = %q / %q", cfg.Repos[0].Token, cfg.Repos[1].Token)
	}
}

// The push credential keeps the order the identity has: AEMAN_GIT_TOKEN
// first, then the keychain, then gh. A deployment that names a token in its
// environment is never quietly overridden by whatever a laptop stored
// months ago — with one set, neither later source is asked at all. And
// whichever source wins the token wins the login with it, which is the
// assertion that says the two orders are really one.
func TestFillGitTokenTakesTheKeychainBeforeTheCLIAndNeitherBeforeAEMANGitToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	ctx := context.Background()
	gh, client := fakeForge(t, map[string]string{"ghp_stored": "alice"})
	log, _ := testLog()
	repos := func() []server.RepoSpec {
		return []server.RepoSpec{{Name: "board", URL: "https://github.com/acme/board.git"}}
	}

	store := tokenstoretest.NewFake().Put("github.com", "ghp_stored")
	cli := &fakeCLI{token: "cli-token", login: "machine-user"}
	cfg := &server.GitConfig{Forge: gh, Repos: repos()}
	c := &chain{log: log, sources: []forge.CLI{tokenstore.NewCLI(store, gh, client), cli}}
	fillGitToken(ctx, cfg, c)
	if cfg.Token != "ghp_stored" || cfg.Repos[0].Token != "ghp_stored" {
		t.Fatalf("tokens = %q / %q, want the stored one", cfg.Token, cfg.Repos[0].Token)
	}
	if login, err := c.Login(ctx); err != nil || login != "alice" {
		t.Fatalf("Login = %q, %v; want the owner of the token that was used", login, err)
	}
	if cli.calls != 0 {
		t.Fatalf("gh was asked %d times while the keychain had a token, want 0", cli.calls)
	}

	store = tokenstoretest.NewFake().Put("github.com", "ghp_stored")
	cli = &fakeCLI{token: "cli-token"}
	cfg = &server.GitConfig{Forge: gh, Repos: repos(), Token: "env-token"}
	fillGitToken(ctx, cfg, &chain{log: log, sources: []forge.CLI{tokenstore.NewCLI(store, gh, client), cli}})
	if cfg.Token != "env-token" || cfg.Repos[0].Token != "env-token" {
		t.Fatalf("tokens = %q / %q, want AEMAN_GIT_TOKEN's", cfg.Token, cfg.Repos[0].Token)
	}
	if store.Gets() != 0 || cli.calls != 0 {
		t.Fatalf("the keychain was read %d times and gh asked %d, want 0 and 0", store.Gets(), cli.calls)
	}
}

// The forge's token variables decide the identity as well as the push
// credential, and the two must name one person. gh hands GH_TOKEN straight
// back, so before the keychain existed they agreed by accident; the
// environment is a source of the chain so they agree on purpose. Without
// that, a machine with GH_TOKEN exported and a token in its keychain
// pushes as one account and signs the commits with another's name.
func TestTheEnvironmentTokenAndItsOwnerAreOneAnswer(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "ghp_env_bot")
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"ghp_env_bot": "release-bot", "ghp_stored": "alice"})
	log, _ := testLog()
	store := tokenstoretest.NewFake().Put("github.com", "ghp_stored")
	cli := &fakeCLI{token: "cli-token", login: "machine-user"}

	c := &chain{log: log, sources: []forge.CLI{
		newEnvCLI(f, osEnv, client),
		tokenstore.NewCLI(store, f, client),
		cli,
	}}
	cfg := &server.GitConfig{Forge: f, Repos: []server.RepoSpec{{Name: "board", URL: "https://github.com/acme/board.git"}}}
	fillGitToken(ctx, cfg, c)

	if cfg.Token != "ghp_env_bot" {
		t.Fatalf("push credential = %q, want the environment's", cfg.Token)
	}
	if login, err := c.Login(ctx); err != nil || login != "release-bot" {
		t.Fatalf("Login = %q, %v; want the environment token's owner", login, err)
	}
	if store.Gets() != 0 || cli.calls != 0 {
		t.Fatalf("the keychain was read %d times and gh asked %d, want 0 and 0", store.Gets(), cli.calls)
	}

	// With nothing in the environment the chain carries on to the keychain,
	// and the identity follows it there.
	t.Setenv("GH_TOKEN", "")
	empty := &chain{log: log, sources: []forge.CLI{
		newEnvCLI(f, osEnv, client),
		tokenstore.NewCLI(tokenstoretest.NewFake().Put("github.com", "ghp_stored"), f, client),
		&fakeCLI{token: "cli-token", login: "machine-user"},
	}}
	if tok, err := empty.Token(ctx); err != nil || tok != "ghp_stored" {
		t.Fatalf("Token = %q, %v; want the stored one", tok, err)
	}
	if login, err := empty.Login(ctx); err != nil || login != "alice" {
		t.Fatalf("Login = %q, %v; want the stored token's owner", login, err)
	}
}

// The GitHub App credential replaces the PAT: an app id plus its private
// key (inline, base64, or a file) build the minting credential, and the
// static token stays empty. The key is validated here — a broken one stops
// the start, and a board on GitLab refuses the GitHub-only credential.
func TestGitConfigBuildsTheGitHubAppCredential(t *testing.T) {
	pemKey := testAppKeyPEM(t)

	parse := func(env map[string]string) (*server.GitConfig, error) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		gf := addGitFlags(fs, func(k string) string { return env[k] })
		if err := fs.Parse(nil); err != nil {
			t.Fatal(err)
		}
		return gf.config()
	}

	cfg, err := parse(map[string]string{
		"AEMAN_REPOS":          "board=https://github.com/acme/board.git",
		"AEMAN_GITHUB_APP_ID":  "12345",
		"AEMAN_GITHUB_APP_KEY": string(pemKey),
	})
	if err != nil || cfg.App == nil {
		t.Fatalf("inline key: err = %v", err)
	}
	if cfg.Token != "" {
		t.Fatalf("the static token must stay empty in app mode, got %q", cfg.Token)
	}

	// A .env file cannot hold a multiline PEM: base64 of the key works too.
	cfg, err = parse(map[string]string{
		"AEMAN_REPOS":          "board=https://github.com/acme/board.git",
		"AEMAN_GITHUB_APP_ID":  "12345",
		"AEMAN_GITHUB_APP_KEY": base64.StdEncoding.EncodeToString(pemKey),
	})
	if err != nil || cfg.App == nil {
		t.Fatalf("base64 key: err = %v", err)
	}

	// Or a file path.
	keyFile := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(keyFile, pemKey, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = parse(map[string]string{
		"AEMAN_REPOS":               "board=https://github.com/acme/board.git",
		"AEMAN_GITHUB_APP_ID":       "12345",
		"AEMAN_GITHUB_APP_KEY_FILE": keyFile,
	})
	if err != nil || cfg.App == nil {
		t.Fatalf("key file: err = %v", err)
	}

	// An id without a key is a configuration error, named at start.
	if _, err := parse(map[string]string{
		"AEMAN_REPOS":         "board=https://github.com/acme/board.git",
		"AEMAN_GITHUB_APP_ID": "12345",
	}); err == nil {
		t.Fatal("an app id without a key must be refused")
	}

	// The credential is GitHub's; a GitLab board cannot use it.
	if _, err := parse(map[string]string{
		"AEMAN_REPOS":          "board=https://gitlab.com/acme/board.git",
		"AEMAN_GITHUB_APP_ID":  "12345",
		"AEMAN_GITHUB_APP_KEY": string(pemKey),
	}); err == nil {
		t.Fatal("a GitLab board with a GitHub App credential must be refused")
	}
}

// testAppKeyPEM is a throwaway RSA key in the PKCS#1 PEM shape GitHub
// hands out for an app.
func testAppKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// AEMAN_GIT_TOKEN is trimmed like every other source. A value off a `.env`
// file or a heredoc carries a newline, and an untrimmed one counts as set:
// the chain is skipped and the whitespace reaches the git credential,
// which the forge answers with a 401 that names nothing. A value that is
// only whitespace is not a token at all, so the chain runs.
func TestTheSharedTokenIsTrimmed(t *testing.T) {
	cfgFor := func(tok string) *server.GitConfig {
		t.Helper()
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		gf := addGitFlags(fs, func(k string) string {
			if k == "AEMAN_GIT_TOKEN" {
				return tok
			}
			return ""
		})
		if err := fs.Parse([]string{"--repo", "board=https://github.com/acme/board.git"}); err != nil {
			t.Fatal(err)
		}
		cfg, err := gf.config()
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	if got := cfgFor("ghp_token\n"); got.Token != "ghp_token" || got.Repos[0].Token != "ghp_token" {
		t.Fatalf("tokens = %q / %q, want them trimmed", got.Token, got.Repos[0].Token)
	}
	if got := cfgFor("  ghp_token \t"); got.Token != "ghp_token" {
		t.Fatalf("token = %q, want it trimmed", got.Token)
	}
	if got := cfgFor(" \n\t "); got.Token != "" {
		t.Fatalf("token = %q; whitespace is not a credential, so the chain must run", got.Token)
	}
}

// The push credential goes through the same election-and-refusal path the
// identity does, so a stored token the forge has since revoked does not
// become the credential every push uses for the life of the process. That
// was the sharpest shape of the split: the board reads, the page names the
// right person, and every push 401s with nothing on screen to explain it,
// because fillGitToken had resolved the revoked one at start-up.
func TestFillGitTokenFallsThroughARevokedStoredToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	ctx := context.Background()
	f, client := fakeForge(t, map[string]string{"cli-token": "machine-user"})
	log, _ := testLog()
	store := tokenstoretest.NewFake().Put("github.com", "ghp_revoked")
	cli := &fakeCLI{token: "cli-token", login: "machine-user"}

	c := &chain{log: log, forge: f, sources: []forge.CLI{
		tokenstore.NewCLI(store, f, client), cli,
	}}
	cfg := &server.GitConfig{Forge: f, Repos: []server.RepoSpec{{Name: "board", URL: "https://github.com/acme/board.git"}}}
	fillGitToken(ctx, cfg, c)

	if cfg.Token != "cli-token" || cfg.Repos[0].Token != "cli-token" {
		t.Fatalf("push credential = %q / %q; want the source below the revoked one", cfg.Token, cfg.Repos[0].Token)
	}
	// And the identity agrees with it, which is the whole point.
	if login, err := c.Login(ctx); err != nil || login != "machine-user" {
		t.Fatalf("Login = %q, %v; the push credential and the name must be one account", login, err)
	}
}

// The push credential must not wait on the identity lookup. Resolving it
// used to read a variable and make no request at all; asking the chain
// instead means the forge is contacted here, and on a black-holed network
// — a VPN down, a captive portal, where packets are dropped rather than
// refused — that is the client's full timeout before the server listens.
// The credential does not depend on the answer, so the lookup gets a
// deadline of its own and the token arrives without it.
func TestFillGitTokenDoesNotWaitOnTheForgeForThePushCredential(t *testing.T) {
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(hang); srv.Close() })

	defer func(d time.Duration) { tokenLookupTimeout = d }(tokenLookupTimeout)
	tokenLookupTimeout = 50 * time.Millisecond

	f := forge.NewGitHubAt(srv.URL)
	log, _ := testLog()
	cli := &chain{log: log, forge: f, sources: []forge.CLI{
		newEnvCLI(f, func(k string) string {
			if k == "GITHUB_TOKEN" {
				return "env-token"
			}
			return ""
		}, guardedClient(srv)),
	}}
	cfg := &server.GitConfig{Forge: f, Repos: []server.RepoSpec{{Name: "board"}}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		fillGitToken(context.Background(), cfg, cli)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fillGitToken is still waiting on the forge")
	}
	if cfg.Token != "env-token" || cfg.Repos[0].Token != "env-token" {
		t.Fatalf("token = %q / %q, want the environment's despite the forge never answering", cfg.Token, cfg.Repos[0].Token)
	}
}

// Asking who the credential belongs to is bounded wherever it happens at
// start-up, not only where the credential itself is resolved. `aeman mcp`
// attaches the personal board before it serves stdio, and an MCP client
// gives up on a silent start: an unbounded lookup there puts the source's
// own 30-second ceiling on top of the credential lookup's, on exactly the
// network that made the first bound necessary.
func TestTheStartUpLoginIsBounded(t *testing.T) {
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(hang); srv.Close() })

	defer func(d time.Duration) { tokenLookupTimeout = d }(tokenLookupTimeout)
	tokenLookupTimeout = 50 * time.Millisecond

	f := forge.NewGitHubAt(srv.URL)
	log, _ := testLog()
	cli := &chain{log: log, forge: f, sources: []forge.CLI{
		newEnvCLI(f, func(string) string { return "env-token" }, guardedClient(srv)),
	}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// An unreachable forge is not a missing credential: the chain
		// answers with no name and no error, and attaching a personal
		// board is a no-op on an empty login. What must not happen is
		// waiting for it.
		if login, _ := boundedLogin(context.Background(), cli); login != "" {
			t.Errorf("login = %q, want none from a forge that never answered", login)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the start-up login is still waiting on the forge")
	}
}

// parseGitFlags is one round through the flag set: what a process is given
// on the command line and in its environment, resolved.
func parseGitFlags(t *testing.T, env map[string]string, args []string) *server.GitConfig {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	gf := addGitFlags(fs, func(k string) string { return env[k] })
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	cfg, err := gf.config()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// A service unit is a file, not a shell: it inherits none of the
// environment the install was typed in, so flagArgs has to bake the
// RESOLVED configuration into it. An install driven entirely by
// AEMAN_REPOS must still produce a unit that names the repositories.
func TestFlagArgsRoundTripThroughTheFlagSet(t *testing.T) {
	env := map[string]string{
		// A host that does NOT contain "gitlab": forge.Detect cannot reach
		// GitLab from the URL alone, so the rendered --forge/--gitlab-url
		// are the only thing carrying it into the unit.
		"AEMAN_REPOS":         "shared=https://code.example.org/a.git,closed=https://code.example.org/b.git",
		"AEMAN_DATA":          "/var/lib/aeman",
		"AEMAN_HISTORY":       "3w",
		"AEMAN_HISTORY_MAX":   "90d",
		"AEMAN_SYNC_INTERVAL": "30s",
		"AEMAN_UNPUSHED_WARN": "10m",
		"AEMAN_COMMITTER":     "bot <bot@example.org>",
		"AEMAN_AUTHOR_EMAIL":  "{login}@example.org",
		"AEMAN_FORGE":         "gitlab",
		"AEMAN_GITLAB_URL":    "https://code.example.org",
	}
	want := parseGitFlags(t, env, nil)

	// The unit's own run has none of that environment.
	got := parseGitFlags(t, map[string]string{}, flagArgs(want))

	if !reflect.DeepEqual(got.Repos, want.Repos) {
		t.Errorf("repos = %+v, want %+v", got.Repos, want.Repos)
	}
	if got.DataDir != want.DataDir {
		t.Errorf("data = %q, want %q", got.DataDir, want.DataDir)
	}
	if got.History != want.History || got.HistoryMax != want.HistoryMax ||
		got.SyncInterval != want.SyncInterval || got.UnpushedWarn != want.UnpushedWarn {
		t.Errorf("spans = %v %v %v %v, want %v %v %v %v",
			got.History, got.HistoryMax, got.SyncInterval, got.UnpushedWarn,
			want.History, want.HistoryMax, want.SyncInterval, want.UnpushedWarn)
	}
	if got.Committer != want.Committer || got.AuthorEmail != want.AuthorEmail {
		t.Errorf("identity = %+v %q, want %+v %q", got.Committer, got.AuthorEmail, want.Committer, want.AuthorEmail)
	}
	// The forge and the instance it points at survive too: a self-hosted
	// GitLab reached under a name that is not the repository's host would
	// otherwise silently become github.com in the unit.
	if got.Forge.Kind() != want.Forge.Kind() || got.Forge.AuthorizeURL() != want.Forge.AuthorizeURL() {
		t.Errorf("forge = %s %s, want %s %s",
			got.Forge.Kind(), got.Forge.AuthorizeURL(), want.Forge.Kind(), want.Forge.AuthorizeURL())
	}
}

// A unit does not run from the directory the install was typed in: launchd
// starts an agent in /, systemd a user service in the home directory. A
// relative --data carried across verbatim has the daemon open a different
// clone from the one the operator meant, or on launchd fail to make one at
// all and respawn forever.
// A credential can also arrive inside the repository URL, and a unit's
// command line is not private: /proc/<pid>/cmdline is world-readable on
// Linux and `systemctl --user cat` prints ExecStart in full. So it is
// dropped rather than rendered, and the install says which one it dropped.
func TestFlagArgsDropsACredentialInsideARepositoryURL(t *testing.T) {
	cfg := parseGitFlags(t, map[string]string{
		"AEMAN_REPOS": "board=https://x-access-token:ghp_inthisurl@github.com/acme/board.git",
	}, nil)
	rendered := strings.Join(flagArgs(cfg), " ")
	if strings.Contains(rendered, "ghp_inthisurl") || strings.Contains(rendered, "x-access-token") {
		t.Fatalf("flagArgs rendered the URL's credential: %s", rendered)
	}
	// The repository is still there, or the check above passes on nothing.
	if !strings.Contains(rendered, "board=https://github.com/acme/board.git") {
		t.Fatalf("flagArgs mangled the URL instead of stripping the credential: %s", rendered)
	}
	// And the daemon still clones with the credential — only the unit is
	// without it.
	if cfg.Repos[0].URL != "https://x-access-token:ghp_inthisurl@github.com/acme/board.git" {
		t.Fatalf("the running process lost its credential too: %q", cfg.Repos[0].URL)
	}
}

// An `ssh://` login must survive the strip, but a password sitting next to
// it is a secret like any other and the unit's command line is readable by
// other accounts. Dropping it costs nothing: go-git's ssh transport passes
// only the endpoint user to DefaultAuthBuilder and never reads a URL
// password.
func TestFlagArgsDropsAPasswordFromAnSSHRemote(t *testing.T) {
	const remote = "ssh://git:s3cret@github.com/acme/board.git"
	cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=" + remote}, nil)
	rendered := strings.Join(flagArgs(cfg), " ")
	if strings.Contains(rendered, "s3cret") {
		t.Fatalf("flagArgs rendered the ssh password: %s", rendered)
	}
	// The login is load-bearing, so the strip must not take it along.
	if !strings.Contains(rendered, "board=ssh://git@github.com/acme/board.git") {
		t.Fatalf("flagArgs dropped the ssh login with the password: %s", rendered)
	}
	// And the install has to name it, or a credential goes missing in
	// silence for whoever is reading the output.
	if !hasCredential(remote) {
		t.Error("the ssh password is not reported as dropped")
	}
	// The running process keeps it; only the unit file is without it.
	if cfg.Repos[0].URL != remote {
		t.Fatalf("the running process lost its password too: %q", cfg.Repos[0].URL)
	}
}

// Every URL form that hides no secret must reach the unit exactly as
// given, and none of them may be reported as a dropped credential. An
// `ssh://` userinfo with no password is the LOGIN, and go-git falls back to
// the local OS account when it is missing, so dropping it would have every
// fetch and push authenticate as the wrong user while the daemon reported
// itself healthy.
func TestFlagArgsKeepsEveryOtherURLFormIntact(t *testing.T) {
	for _, remote := range []string{
		// The login, not a secret: stripping it made go-git fall back to
		// the local OS account.
		"ssh://git@github.com/acme/board.git",
		// Does not parse as a URL at all, so it must survive untouched.
		"git@github.com:acme/board.git",
		// No userinfo to confuse anything — pinned because "safe by
		// construction" is the argument that was already wrong once here.
		"file:///srv/boards/board.git",
		"/srv/boards/board.git",
	} {
		cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=" + remote}, nil)
		rendered := strings.Join(flagArgs(cfg), " ")
		if !strings.Contains(rendered, "board="+remote) {
			t.Errorf("flagArgs changed %q:\n%s", remote, rendered)
		}
		// And the install must not call the login a credential.
		if hasCredential(remote) {
			t.Errorf("%q was read as carrying a credential", remote)
		}
	}
}

// A local remote given as a relative path hits the same trap --data does:
// the unit is not run from the directory the install was typed in.
func TestFlagArgsResolvesARelativeRepositoryPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "board.git"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=board.git"}, nil)
	rendered := strings.Join(flagArgs(cfg), " ")
	want := filepath.Join(dir, "board.git")
	if !strings.Contains(rendered, "board="+want) {
		t.Fatalf("flagArgs left the local path relative: %s", rendered)
	}

	// A URL keeps its shape, and a path that does not exist is not guessed at.
	for _, raw := range []string{"https://example.com/board.git", "ssh://git@example.com/board.git", "nope.git"} {
		cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=" + raw}, nil)
		if got := strings.Join(flagArgs(cfg), " "); !strings.Contains(got, "board="+raw) {
			t.Errorf("flagArgs changed %q: %s", raw, got)
		}
	}
}

func TestFlagArgsResolvesARelativeDataDir(t *testing.T) {
	cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=https://example.com/b.git"},
		[]string{"--data", filepath.Join(".", "board-data")})
	args := flagArgs(cfg)
	i := slices.Index(args, "--data")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("flagArgs rendered no --data: %v", args)
	}
	if !filepath.IsAbs(args[i+1]) {
		t.Fatalf("--data %q stayed relative", args[i+1])
	}
	if filepath.Base(args[i+1]) != "board-data" {
		t.Fatalf("--data %q is not the directory that was asked for", args[i+1])
	}
}

// The unit file sits in the user's home in plain text, readable by anything
// that runs as them. The daemon finds its credential the way `aeman mcp`
// does — from the environment or the forge's CLI — so no token is ever
// written into it.
func TestFlagArgsNeverRendersACredential(t *testing.T) {
	env := map[string]string{
		"AEMAN_REPOS":              "shared=https://x/a.git,closed=https://x/b.git",
		"AEMAN_GIT_TOKEN":          "ghp_sharedsecret",
		"AEMAN_GIT_TOKEN_CLOSED":   "ghp_closedsecret",
		"AEMAN_GITHUB_APP_KEY":     "ghp_appkeysecret",
		"AEMAN_GIT_TOKEN_NOTAREPO": "ghp_straysecret",
	}
	cfg := parseGitFlags(t, env, nil)
	rendered := strings.Join(flagArgs(cfg), " ")
	for _, secret := range []string{"ghp_sharedsecret", "ghp_closedsecret", "ghp_appkeysecret", "ghp_straysecret"} {
		if strings.Contains(rendered, secret) {
			t.Errorf("flagArgs rendered %s: %s", secret, rendered)
		}
	}
	// The repositories themselves still have to be there, or the check
	// above passes on an empty string.
	if !strings.Contains(rendered, "shared=https://x/a.git") || !strings.Contains(rendered, "closed=https://x/b.git") {
		t.Fatalf("flagArgs dropped the repositories: %s", rendered)
	}
}
