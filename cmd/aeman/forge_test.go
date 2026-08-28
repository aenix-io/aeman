package main

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/internal/forge"
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
	if cliFor(gl, "https://gitlab.example.org/t/b.git") == nil || cliFor(gh, "https://github.com/a/b.git") == nil {
		t.Fatal("every forge has a CLI")
	}
}
