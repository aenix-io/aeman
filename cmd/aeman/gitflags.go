package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	fs.StringVar(&g.history, "history", "", "history horizon to load in the background, e.g. 8w (env AEMAN_HISTORY; default 8w)")
	fs.StringVar(&g.historyMax, "history-max", "", "cap for on-demand history deepening (env AEMAN_HISTORY_MAX; default 1y)")
	fs.StringVar(&g.syncInterval, "sync-interval", "", "how often to fetch other replicas' commits (env AEMAN_SYNC_INTERVAL; default 15s)")
	fs.StringVar(&g.unpushed, "unpushed-warn", "", "age of the oldest unpushed commit that turns health degraded (env AEMAN_UNPUSHED_WARN; default 5m)")
	fs.StringVar(&g.committer, "committer", "", `committer identity "Name <email>" (env AEMAN_COMMITTER; default "aeman <aeman@localhost>")`)
	fs.StringVar(&g.authorEmail, "author-email", "", "author email template with {login} (env AEMAN_AUTHOR_EMAIL; default {login}@aeman)")
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

// config builds the GitConfig, or nil when no repository is configured.
func (g *gitFlags) config() (*server.GitConfig, error) {
	repos := g.repos
	if !g.reposSet {
		repos = nil
		for _, part := range strings.Split(g.env("AEMAN_REPOS"), ",") {
			if part = strings.TrimSpace(part); part != "" {
				if err := repos.Set(part); err != nil {
					return nil, fmt.Errorf("AEMAN_REPOS: %w", err)
				}
			}
		}
	}
	if len(repos) == 0 {
		return nil, nil //nolint:nilnil // no git mode configured is not an error
	}
	cfg := &server.GitConfig{Repos: repos, Token: g.env("AEMAN_GIT_TOKEN")}
	var err error
	if cfg.History, err = parseSpan(g.pick(g.history, "AEMAN_HISTORY", "8w")); err != nil {
		return nil, fmt.Errorf("--history: %w", err)
	}
	if _, err = parseSpan(g.pick(g.historyMax, "AEMAN_HISTORY_MAX", "365d")); err != nil {
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
	return cfg, nil
}

// fillGitToken supplies the push/fetch credential when AEMAN_GIT_TOKEN is
// unset: the local gh CLI's token, the same source the GitHub board uses in
// local mode. Best-effort — a remote that needs no credential (a file path,
// a public repository) works without it.
func fillGitToken(ctx context.Context, cfg *server.GitConfig) {
	if cfg == nil || cfg.Token != "" {
		return
	}
	if tok, err := resolveGitHubToken(ctx); err == nil {
		cfg.Token = tok
	}
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
