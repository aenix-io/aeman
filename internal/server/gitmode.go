package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Git mode: the board is a git repository. The server clones it under the
// data directory (shallow — the current state), serves it from the cache,
// commits every request, pushes in the background and deepens the history
// to the configured horizon. One repository is one board; the request's
// owner/board parameters are ignored, as under --lock-board.

// RepoSpec names one domain: a repository and its label.
type RepoSpec struct {
	Name string
	URL  string
}

// GitConfig enables git mode.
type GitConfig struct {
	// Repos are the board's domains, primary first. Only the primary is
	// served for now.
	Repos []RepoSpec
	// Token is the push/fetch credential (HTTPS basic auth); empty means an
	// unauthenticated transport (local file remotes, tests).
	Token string
	// DataDir holds the clones (<DataDir>/repos/<name>).
	DataDir string
	// History is the background deepening horizon; zero disables it.
	History time.Duration
	// SyncInterval is the fetch cadence; zero disables the ticker.
	SyncInterval time.Duration
	// UnpushedWarn is the age of the oldest unpushed commit that turns
	// health red; zero means 5 minutes.
	UnpushedWarn time.Duration
	Committer    gitstore.Identity
	// AuthorEmail is the author email template ("{login}" substituted);
	// empty means <login>@aeman.
	AuthorEmail string
}

// gitBoardKey is the store key of the single configured board.
const gitBoardOwner = "git"

// openGit clones or reopens the primary repository and builds the store's
// backend over it. An unborn remote is refused with the command that fixes
// it: a server that invents a board on a typo in --repo is worse than one
// that stops.
func (s *Server) openGit(cfg *GitConfig) error {
	if len(cfg.Repos) == 0 {
		return errors.New("git mode needs at least one --repo")
	}
	primary := cfg.Repos[0]
	dir := filepath.Join(cfg.DataDir, "repos", primary.Name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	storer := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	remote := gitstore.Remote{URL: primary.URL}
	if cfg.Token != "" {
		remote.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: cfg.Token}
	}
	opts := gitstore.Options{Committer: cfg.Committer}
	if opts.Committer.Name == "" {
		opts.Committer = gitstore.Identity{Name: "aeman", Email: "aeman@localhost"}
	}
	if cfg.AuthorEmail != "" {
		tpl := cfg.AuthorEmail
		opts.AuthorEmail = func(login string) string { return strings.ReplaceAll(tpl, "{login}", login) }
	}

	var repo *gitstore.Repo
	if existing := gitstore.Open(storer, opts); !existing.Head().IsZero() {
		// A clone from an earlier run: its unpushed commits are still there.
		repo = existing
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		r, err := gitstore.Clone(ctx, storer, remote, opts, 1)
		if err != nil && !errors.Is(err, gitstore.ErrEmptyRepository) && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
			// Not every server speaks shallow (GitHub, GitLab and Gitea do;
			// a plain daemon may not): fall back to the whole history rather
			// than refuse the board. The failed attempt left an unborn
			// repository behind; start the directory over — it holds nothing
			// yet — and a real failure fails again here.
			if gitstore.Open(storer, opts).Head().IsZero() {
				if err := os.RemoveAll(dir); err != nil {
					return fmt.Errorf("reset clone dir: %w", err)
				}
				if err := os.MkdirAll(dir, 0o750); err != nil {
					return fmt.Errorf("data dir: %w", err)
				}
			}
			storer = filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
			r, err = gitstore.Clone(ctx, storer, remote, opts, 0)
		}
		if err != nil {
			if errors.Is(err, gitstore.ErrEmptyRepository) || errors.Is(err, transport.ErrEmptyRemoteRepository) {
				return fmt.Errorf("repository %s has no board yet — run `aeman init --repo %s` first", primary.URL, primary.URL)
			}
			return fmt.Errorf("clone %s: %w", primary.URL, err)
		}
		repo = r
	}
	if _, err := gitstore.Load(repo); err != nil {
		if errors.Is(err, gitstore.ErrEmptyRepository) {
			return fmt.Errorf("repository %s has no board yet — run `aeman init --repo %s` first", primary.URL, primary.URL)
		}
		return fmt.Errorf("read %s: %w", primary.URL, err)
	}
	s.gitBE = newGitBackend(s.store, repo, remote, gitOptions{PushDelay: 300 * time.Millisecond, SyncInterval: cfg.SyncInterval, Logger: s.log})
	s.gitCfg = cfg
	if cfg.History > 0 {
		go s.deepenInBackground(repo, remote, cfg.History)
	}
	return nil
}

// deepenInBackground brings the history to the horizon after start-up, so
// the cold start stays a depth-1 clone and the log fills in behind it.
func (s *Server) deepenInBackground(repo *gitstore.Repo, remote gitstore.Remote, horizon time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := repo.DeepenSince(ctx, remote, time.Now().Add(-horizon)); err != nil {
		s.log.Warn("history deepen failed", "err", err)
		return
	}
	s.log.Info("history deepened", "horizon", horizon)
}

// gitBoard is the (owner, number) the single configured board is served as.
func (s *Server) gitBoard() (string, int) {
	return gitBoardOwner, 1
}

// drainAndPush is the shutdown path in git mode: wait for the queue, then
// push whatever is unpushed — one push, not N mutations.
func (s *Server) drainAndPush(ctx context.Context) error {
	s.store.waitDrained(ctx)
	if s.gitBE == nil {
		return nil
	}
	owner, number := s.gitBoard()
	return s.gitBE.syncNow(ctx, storeKey(owner, number))
}

// actionName is what a request is, for the commit it becomes: the name
// after /actions/, or the kind of write the method and route imply. Reads
// have none.
func actionName(method, path string) string {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return ""
	}
	if i := strings.Index(path, "/actions/"); i >= 0 {
		return strings.Trim(path[i+len("/actions/"):], "/")
	}
	rest := strings.TrimPrefix(path, "/api/v1/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	verb := map[string]string{http.MethodPost: "create", http.MethodPatch: "update", http.MethodDelete: "delete"}[method]
	if verb == "" {
		verb = "write"
	}
	switch {
	case len(parts) >= 3 && parts[0] == "cards" && parts[2] == "notes":
		return map[string]string{"create": "note", "update": "note-edit", "delete": "note-delete"}[verb]
	case parts[0] == "cards":
		return verb
	case parts[0] == "processes" && len(parts) >= 2 && parts[1] == "tasks":
		return map[string]string{"create": "add-task", "update": "update-task", "delete": "delete-task"}[verb]
	case verb == "create":
		return "add-" + strings.TrimSuffix(parts[0], "s")
	}
	return verb + "-" + strings.TrimSuffix(parts[0], "s")
}

// actionMiddleware stamps a mutating /api/v1 request with its action: a
// fresh id and the route's name. In git mode every write the request makes
// joins that one commit.
func actionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1") {
			if name := actionName(r.Method, r.URL.Path); name != "" {
				r = r.WithContext(withAction(r.Context(), gitstore.NewID(time.Now()), name))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// unpushedWarn is the health threshold.
func (s *Server) unpushedWarn() time.Duration {
	if s.gitCfg != nil && s.gitCfg.UnpushedWarn > 0 {
		return s.gitCfg.UnpushedWarn
	}
	return 5 * time.Minute
}
