package main

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/aenix-io/aeman/internal/server"
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
	fillGitToken(context.Background(), cfg)
	if cfg.Repos[0].Token != "own-token" || cfg.Repos[1].Token != "default-token" {
		t.Fatalf("tokens = %q / %q", cfg.Repos[0].Token, cfg.Repos[1].Token)
	}
}
