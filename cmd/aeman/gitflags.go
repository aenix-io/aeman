package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/server"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Git-mode configuration: repeatable --repo name=url with environment
// fallbacks for every flag, because the compose file configures the server
// through the environment. No repository configured means the server runs
// as before.

// repoList is the --repo flag: repeatable, "name=url" or a bare url.
type repoList []server.RepoSpec

func (r *repoList) String() string {
	parts := make([]string, 0, len(*r))
	for _, s := range *r {
		parts = append(parts, s.Name+"="+s.URL)
	}
	return strings.Join(parts, ",")
}

// Set adds one repository. A bare url is named after its last path segment.
func (r *repoList) Set(v string) error {
	name, url, ok := strings.Cut(v, "=")
	if !ok {
		url = v
		name = strings.TrimSuffix(path.Base(strings.TrimSuffix(url, "/")), ".git")
	}
	name, url = strings.TrimSpace(name), strings.TrimSpace(url)
	if name == "" || url == "" {
		return fmt.Errorf("--repo %q: want name=url", v)
	}
	for _, s := range *r {
		if s.Name == name {
			return fmt.Errorf("--repo %q: name %q used twice", v, name)
		}
	}
	*r = append(*r, server.RepoSpec{Name: name, URL: url})
	return nil
}

// parseSpan reads a duration with weeks and days on top of Go's units:
// "8w", "90d", "36h". Negative and unitless values are errors.
func parseSpan(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty duration")
	}
	unit := s[len(s)-1]
	if unit == 'w' || unit == 'd' {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("bad duration %q", s)
		}
		days := n
		if unit == 'w' {
			days *= 7
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("bad duration %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("bad duration %q: must be positive", s)
	}
	return d, nil
}

// parseCommitter reads "Name <email>".
func parseCommitter(s string) (gitstore.Identity, error) {
	name, rest, ok := strings.Cut(s, "<")
	email := strings.TrimSuffix(strings.TrimSpace(rest), ">")
	name = strings.TrimSpace(name)
	if !ok || name == "" || email == "" || strings.ContainsAny(email, "<> ") {
		return gitstore.Identity{}, fmt.Errorf("committer %q: want \"Name <email>\"", s)
	}
	return gitstore.Identity{Name: name, Email: email}, nil
}

// gitFlags holds the parsed values; config turns them into a GitConfig.
type gitFlags struct {
	repos                     repoList
	reposSet                  bool
	data, history, historyMax string
	syncInterval, unpushed    string
	committer, authorEmail    string
	forge, gitlabURL          string
	env                       func(string) string
}

// addGitFlags registers the git-mode flags on fs. env is the environment
// lookup (os.Getenv in production) used for fallbacks at config time.
func addGitFlags(fs *flag.FlagSet, env func(string) string) *gitFlags {
	g := &gitFlags{env: env}
	fs.Func("repo", "board repository as name=url (repeatable, primary first; env AEMAN_REPOS, comma-separated)", func(v string) error {
		g.reposSet = true
		return g.repos.Set(v)
	})
	fs.StringVar(&g.data, "data", "", "directory for clones and the session file (env AEMAN_DATA; default /data if it exists, else the user cache dir)")
	fs.StringVar(&g.history, "history", "", "history horizon to load in the background, e.g. 2w (env AEMAN_HISTORY; default 2w)")
	fs.StringVar(&g.historyMax, "history-max", "", "cap for on-demand history deepening (env AEMAN_HISTORY_MAX; default 1y)")
	fs.StringVar(&g.syncInterval, "sync-interval", "", "how often to fetch other replicas' commits (env AEMAN_SYNC_INTERVAL; default 15s)")
	fs.StringVar(&g.unpushed, "unpushed-warn", "", "age of the oldest unpushed commit that turns health degraded (env AEMAN_UNPUSHED_WARN; default 5m)")
	fs.StringVar(&g.committer, "committer", "", `committer identity "Name <email>" (env AEMAN_COMMITTER; default "aeman <aeman@localhost>")`)
	fs.StringVar(&g.authorEmail, "author-email", "", "author email template with {login} (env AEMAN_AUTHOR_EMAIL; default {login}@aeman)")
	fs.StringVar(&g.forge, "forge", "", "the code host the repositories live on: github or gitlab (env AEMAN_FORGE; default by the primary repository's host)")
	fs.StringVar(&g.gitlabURL, "gitlab-url", "", "base URL of a self-hosted GitLab, e.g. https://gitlab.example.org (env AEMAN_GITLAB_URL; default https://<primary repository host>)")
	return g
}

// pick returns the flag value, else the environment's, else the default.
func (g *gitFlags) pick(flagVal, envKey, def string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := strings.TrimSpace(g.env(envKey)); v != "" {
		return v
	}
	return def
}

// resolvedRepos is the board's repositories: the --repo flags, else
// AEMAN_REPOS as a comma-separated list of the same shape. Empty is a
// legitimate answer — `aeman login` runs before there is a board.
func (g *gitFlags) resolvedRepos() (repoList, error) {
	if g.reposSet {
		return g.repos, nil
	}
	var repos repoList
	for _, part := range strings.Split(g.env("AEMAN_REPOS"), ",") {
		if part = strings.TrimSpace(part); part != "" {
			if err := repos.Set(part); err != nil {
				return nil, fmt.Errorf("AEMAN_REPOS: %w", err)
			}
		}
	}
	return repos, nil
}

// forgeTarget is the forge a login is for and the keychain account it goes
// under. The account is the forge's own host, so `aeman login` writes the
// item `aeman serve` and `aeman mcp` read: every command asks the same
// forge value Detect built from the same flags. With no repository
// configured it is GitHub at github.com — but a GitLab asked for by name
// still needs a host, which Detect refuses to guess.
func (g *gitFlags) forgeTarget() (forge.Forge, string, error) {
	repos, err := g.resolvedRepos()
	if err != nil {
		return nil, "", err
	}
	var primary string
	if len(repos) > 0 {
		primary = repos[0].URL
	}
	f, err := forge.Detect(primary, forge.Kind(g.pick(g.forge, "AEMAN_FORGE", "")), g.pick(g.gitlabURL, "AEMAN_GITLAB_URL", ""))
	if err != nil {
		return nil, "", fmt.Errorf("--forge: %w", err)
	}
	return f, f.Host(), nil
}

// config builds the GitConfig, or nil when no repository is configured.
func (g *gitFlags) config() (*server.GitConfig, error) {
	repos, err := g.resolvedRepos()
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, nil //nolint:nilnil // no git mode configured is not an error
	}
	// Trimmed like every other token source: a trailing newline off a
	// `.env` file or a heredoc would otherwise count as "set", skip the
	// chain, and go into the git credential as whitespace — a 401 on
	// push with nothing on screen pointing at the cause.
	cfg := &server.GitConfig{Repos: repos, Token: strings.TrimSpace(g.env("AEMAN_GIT_TOKEN"))}
	// A board may span two organisations, and one token narrow enough for
	// either cannot reach both: every domain may name its own credential,
	// falling back to the shared one. A domain still without one here is
	// filled by fillGitToken from the credential chain.
	for i := range cfg.Repos {
		if tok := strings.TrimSpace(g.env(tokenEnvFor(cfg.Repos[i].Name))); tok != "" {
			cfg.Repos[i].Token = tok
			continue
		}
		cfg.Repos[i].Token = cfg.Token
	}
	if cfg.Forge, err = forge.Detect(repos[0].URL, forge.Kind(g.pick(g.forge, "AEMAN_FORGE", "")), g.pick(g.gitlabURL, "AEMAN_GITLAB_URL", "")); err != nil {
		return nil, fmt.Errorf("--forge: %w", err)
	}
	if cfg.History, err = parseSpan(g.pick(g.history, "AEMAN_HISTORY", "2w")); err != nil {
		return nil, fmt.Errorf("--history: %w", err)
	}
	if cfg.HistoryMax, err = parseSpan(g.pick(g.historyMax, "AEMAN_HISTORY_MAX", "365d")); err != nil {
		return nil, fmt.Errorf("--history-max: %w", err)
	}
	if cfg.SyncInterval, err = parseSpan(g.pick(g.syncInterval, "AEMAN_SYNC_INTERVAL", "15s")); err != nil {
		return nil, fmt.Errorf("--sync-interval: %w", err)
	}
	if cfg.UnpushedWarn, err = parseSpan(g.pick(g.unpushed, "AEMAN_UNPUSHED_WARN", "5m")); err != nil {
		return nil, fmt.Errorf("--unpushed-warn: %w", err)
	}
	if cfg.Committer, err = parseCommitter(g.pick(g.committer, "AEMAN_COMMITTER", "aeman <aeman@localhost>")); err != nil {
		return nil, err
	}
	cfg.AuthorEmail = g.pick(g.authorEmail, "AEMAN_AUTHOR_EMAIL", "")
	cfg.DataDir = g.pick(g.data, "AEMAN_DATA", defaultDataDir())
	if cfg.App, err = g.githubApp(cfg.Forge); err != nil {
		return nil, err
	}
	return cfg, nil
}

// githubApp builds the GitHub App credential when AEMAN_GITHUB_APP_ID is
// set: the private key comes inline (PEM, or base64 of it — a .env file
// cannot hold a multiline value) or from a file. The App replaces the
// static token; a repository that names its own token keeps it.
func (g *gitFlags) githubApp(f forge.Forge) (*forge.GitHubApp, error) {
	id := strings.TrimSpace(g.env("AEMAN_GITHUB_APP_ID"))
	if id == "" {
		return nil, nil //nolint:nilnil // no app configured is not an error
	}
	if f.Kind() != forge.GitHub {
		return nil, fmt.Errorf("AEMAN_GITHUB_APP_ID is set, but the board lives on %s — the GitHub App credential works on GitHub only", f.Label())
	}
	raw := strings.TrimSpace(g.env("AEMAN_GITHUB_APP_KEY"))
	if raw != "" && !strings.Contains(raw, "BEGIN") {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("AEMAN_GITHUB_APP_KEY is neither PEM nor base64 of it: %w", err)
		}
		raw = string(decoded)
	}
	if raw == "" {
		path := strings.TrimSpace(g.env("AEMAN_GITHUB_APP_KEY_FILE"))
		if path == "" {
			return nil, errors.New("AEMAN_GITHUB_APP_ID needs the app's private key: AEMAN_GITHUB_APP_KEY (PEM or its base64) or AEMAN_GITHUB_APP_KEY_FILE")
		}
		data, err := os.ReadFile(path) //nolint:gosec // the operator's own key path
		if err != nil {
			return nil, fmt.Errorf("AEMAN_GITHUB_APP_KEY_FILE: %w", err)
		}
		raw = string(data)
	}
	return forge.NewGitHubApp(id, []byte(raw))
}

// tokenLookupTimeout bounds the start-up credential lookup. A variable so
// a test need not wait it out.
var tokenLookupTimeout = 5 * time.Second

// boundedLogin asks who the elected credential belongs to under that same
// bound. Every start-up caller wants the name and can start without it, so
// none of them may wait on the forge for the source's own ceiling.
func boundedLogin(ctx context.Context, cli forge.CLI) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, tokenLookupTimeout)
	defer cancel()
	return cli.Login(ctx)
}

// fillGitToken supplies the push/fetch credential when AEMAN_GIT_TOKEN is
// unset: the forge's token variables, then cli — the OS keychain and the
// forge's own tool, the same value the identity is read from, so the push
// and the name on the commits come from one credential. Best-effort — a
// remote that needs no credential (a file path, a public repository) works
// without it.
func fillGitToken(ctx context.Context, cfg *server.GitConfig, cli forge.CLI) {
	if cfg == nil {
		return
	}
	// With a GitHub App the server credential is minted, not found: nothing
	// below the environment is asked for it.
	if cfg.Token == "" && cfg.App == nil && !allDomainsHaveTokens(cfg) {
		// Bounded separately from the caller's context: the chain asks the
		// forge whose token this is, and the answer is not needed here —
		// the credential is. A dropped-packet network (a VPN down, a
		// captive portal) would otherwise hold the server before it
		// listens for as long as the forge client allows.
		lookup, cancel := context.WithTimeout(ctx, tokenLookupTimeout)
		defer cancel()
		if tok, err := resolveForgeToken(lookup, cfg.Forge, cli); err == nil {
			cfg.Token = tok
		}
	}
	// Every domain ends up with a credential of its own: its variable, else
	// the shared one. From here nothing has to remember the fallback.
	for i := range cfg.Repos {
		if cfg.Repos[i].Token == "" {
			cfg.Repos[i].Token = cfg.Token
		}
	}
}

// allDomainsHaveTokens reports whether every repository already names its
// own credential — then the shared one is not needed and the chain is
// not asked for it.
func allDomainsHaveTokens(cfg *server.GitConfig) bool {
	for _, r := range cfg.Repos {
		if r.Token == "" {
			return false
		}
	}
	return len(cfg.Repos) > 0
}

// tokenEnvFor is the environment variable a domain's credential is read
// from: AEMAN_GIT_TOKEN_<NAME>, the name upper-cased with anything but a
// letter or a digit as an underscore.
func tokenEnvFor(domain string) string {
	var b strings.Builder
	b.WriteString("AEMAN_GIT_TOKEN_")
	for _, r := range strings.ToUpper(domain) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// defaultDataDir is /data when the container's volume is there, else a
// per-user cache directory.
func defaultDataDir() string {
	if info, err := os.Stat("/data"); err == nil && info.IsDir() {
		return "/data"
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "aeman")
	}
	return filepath.Join(os.TempDir(), "aeman")
}
