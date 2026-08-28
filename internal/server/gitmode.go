package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Git mode: the board is a git repository. The server clones it under the
// data directory (shallow — the current state), serves it from the cache,
// commits every request, pushes in the background and deepens the history
// to the configured horizon. One repository is one board: the request carries no board address —
// the board is the one the server was started with.

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
	// HistoryMax caps on-demand deepening — a card's log cut by the horizon
	// fetches back to the card's creation, but never further than this.
	// Zero means no on-demand deepening.
	HistoryMax time.Duration
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

// openGit clones or reopens the primary repository and builds the store's
// backend over it. An unborn remote is refused with the command that fixes
// it: a server that invents a board on a typo in --repo is worse than one
// that stops.
func (s *Server) openGit(cfg *GitConfig) error {
	be, err := openGitStore(s.store, cfg, s.log)
	if err != nil {
		return err
	}
	s.gitBE = be
	s.gitCfg = cfg
	if cfg.History > 0 {
		for _, d := range be.git.domains {
			go s.deepenInBackground(d.Repo, d.remote, cfg.History)
		}
	}
	return nil
}

// GitBackend is a git-mode store without an HTTP server — what `aeman mcp
// --repo` runs on: its own clone, cache, queue and push.
type GitBackend struct {
	be    *storeBackend
	board string // the board's name: its primary repository
}

// OpenGitBackend clones or reopens the configured repository and returns a
// backend over it. log may be nil.
func OpenGitBackend(cfg *GitConfig, log *slog.Logger) (*GitBackend, error) {
	if log == nil {
		log = slog.Default()
	}
	store := newBoardStore()
	store.log = log
	be, err := openGitStore(store, cfg, log)
	if err != nil {
		return nil, err
	}
	return &GitBackend{be: be, board: cfg.Repos[0].Name}, nil
}

// Backend is the boardservice.Backend to build a service on.
func (g *GitBackend) Backend() boardservice.Backend { return g.be }

// Drain waits for the write queue and pushes — what a stdio MCP process
// does before it exits, so a client that closes the pipe right after a
// mutation loses nothing.
func (g *GitBackend) Drain(ctx context.Context) error {
	g.be.store.waitDrained(ctx)
	return g.be.syncNow(ctx, storeKey(g.board))
}

// openGitStore is the shared core: clone or reopen every domain under the
// data dir, check each has a board, build the store backend with its sync.
// Every configured repository must be initialised — a server that invents
// a domain on a typo in --repo is worse than one that stops.
func openGitStore(store *boardStore, cfg *GitConfig, log *slog.Logger) (*storeBackend, error) {
	if len(cfg.Repos) == 0 {
		return nil, errors.New("git mode needs at least one --repo")
	}
	opts := gitstore.Options{Committer: cfg.Committer}
	if opts.Committer.Name == "" {
		opts.Committer = gitstore.Identity{Name: "aeman", Email: "aeman@localhost"}
	}
	if cfg.AuthorEmail != "" {
		tpl := cfg.AuthorEmail
		opts.AuthorEmail = func(login string) string { return strings.ReplaceAll(tpl, "{login}", login) }
	}
	domains := make([]gitDomain, 0, len(cfg.Repos))
	seen := map[string]bool{}
	for _, spec := range cfg.Repos {
		if spec.Name == "" || seen[spec.Name] {
			return nil, fmt.Errorf("--repo needs a distinct name per repository (%q given twice or empty)", spec.Name)
		}
		seen[spec.Name] = true
		dir := filepath.Join(cfg.DataDir, "repos", spec.Name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("data dir: %w", err)
		}
		remote := gitstore.Remote{URL: spec.URL}
		if cfg.Token != "" {
			remote.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: cfg.Token}
		}
		repo, err := cloneOrOpen(dir, remote, opts, spec.URL)
		if err != nil {
			return nil, err
		}
		if !repo.Head().IsZero() {
			// G18: an older schema is brought up to date before anything is
			// served; a newer one is refused right here.
			migrated, err := gitstore.MigrateSchema(repo)
			if err != nil {
				return nil, fmt.Errorf("schema of %s: %w", spec.URL, err)
			}
			if migrated {
				log.Info("repository schema migrated", "repo", spec.Name, "schema", gitstore.SchemaVersion)
			}
		}
		if _, err := gitstore.Load(repo); err != nil {
			if errors.Is(err, gitstore.ErrEmptyRepository) {
				return nil, initHint(spec.URL)
			}
			return nil, fmt.Errorf("read %s: %w", spec.URL, err)
		}
		domains = append(domains, gitDomain{Domain: gitstore.Domain{Name: spec.Name, Repo: repo}, remote: remote})
	}
	if len(domains) > 1 {
		// Two repositories that already declare the same team, project or
		// process cannot be served as one board — the merge would fold two
		// different things into one. Refuse, naming both files.
		plain := make([]gitstore.Domain, 0, len(domains))
		for _, d := range domains {
			plain = append(plain, d.Domain)
		}
		snap, err := gitstore.LoadAll(plain)
		if err != nil {
			return nil, fmt.Errorf("read the board: %w", err)
		}
		if err := rosterCollision(snap); err != nil {
			return nil, err
		}
	}
	return newGitBackend(store, domains, gitOptions{PushDelay: 300 * time.Millisecond, SyncInterval: cfg.SyncInterval,
		MaintainEvery: 24 * time.Hour, HistoryMax: cfg.HistoryMax, Logger: log,
		// Issue/PR titles in card descriptions are read with the push
		// credential — the store has no per-visitor token any more.
		Links: newForgeLinks(githubAPIBase, &http.Client{Timeout: 10 * time.Second}, cfg.Token)}), nil
}

// rosterCollision is the start-up refusal for a name two domains declare:
// one line per collision, both files named, so the fix is a rename away.
func rosterCollision(s gitstore.Snapshot) error {
	if len(s.Aliases) == 0 {
		return nil
	}
	domainOf := map[string]string{}
	for _, t := range s.Teams {
		domainOf[t.ID] = t.Domain
	}
	for _, p := range s.Projects {
		domainOf[p.ID] = p.Domain
	}
	for _, p := range s.Processes {
		domainOf[p.ID] = p.Domain
	}
	pathOf := func(kind, id string) string {
		switch kind {
		case "team":
			return gitstore.TeamPath(id)
		case "project":
			return gitstore.ProjectPath(id)
		default:
			return gitstore.ProcessPath(id)
		}
	}
	var b strings.Builder
	b.WriteString("the repositories declare the same name twice; rename one of each pair and start again:")
	for _, a := range s.Aliases {
		fmt.Fprintf(&b, "\n  %s %q: %s/%s and %s/%s", a.Kind, a.Name, domainOf[a.Winner], pathOf(a.Kind, a.Winner), a.Domain, pathOf(a.Kind, a.ID))
	}
	return errors.New(b.String())
}

func initHint(url string) error {
	return fmt.Errorf("repository %s has no board yet — run `aeman init --repo %s` first", url, url)
}

// cloneOrOpen reopens an existing clone (its unpushed commits included) or
// makes a shallow one; a server that does not speak shallow gets a full
// clone instead of a refusal.
func cloneOrOpen(dir string, remote gitstore.Remote, opts gitstore.Options, url string) (*gitstore.Repo, error) {
	storer := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	if existing := gitstore.Open(storer, opts); !existing.Head().IsZero() {
		return existing, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, err := gitstore.Clone(ctx, storer, remote, opts, 1)
	if err != nil && !errors.Is(err, gitstore.ErrEmptyRepository) && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		// Not every server speaks shallow (GitHub, GitLab and Gitea do; a
		// plain daemon may not). The failed attempt left an unborn
		// repository behind; start the directory over — it holds nothing
		// yet — and a real failure fails again here.
		if gitstore.Open(storer, opts).Head().IsZero() {
			if err := os.RemoveAll(dir); err != nil {
				return nil, fmt.Errorf("reset clone dir: %w", err)
			}
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, fmt.Errorf("data dir: %w", err)
			}
		}
		storer = filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
		r, err = gitstore.Clone(ctx, storer, remote, opts, 0)
	}
	if err != nil {
		if errors.Is(err, gitstore.ErrEmptyRepository) || errors.Is(err, transport.ErrEmptyRemoteRepository) {
			return nil, initHint(url)
		}
		return nil, fmt.Errorf("clone %s: %w", url, err)
	}
	return r, nil
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

// gitBoard is the name the single configured board is served as: its
// primary repository's name ("board" when no repository is configured — a
// test server with an injected service).
func (s *Server) gitBoard() string {
	if s.gitCfg != nil && len(s.gitCfg.Repos) > 0 {
		return s.gitCfg.Repos[0].Name
	}
	return "board"
}

// drainAndPush is the shutdown path in git mode: wait for the queue, then
// push whatever is unpushed — one push, not N mutations.
func (s *Server) drainAndPush(ctx context.Context) error {
	s.store.waitDrained(ctx)
	if s.gitBE == nil {
		return nil
	}
	boardID := s.gitBoard()
	return s.gitBE.syncNow(ctx, storeKey(boardID))
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
