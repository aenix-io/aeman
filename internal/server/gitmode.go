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
	"github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/aenix-io/aeman/internal/forge"
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
	// Token is this repository's own credential — a board may span two
	// organisations, and one token narrow enough for either cannot reach
	// both. Empty falls back to GitConfig.Token.
	Token string
}

// token is the credential this repository is read and written with.
func (s RepoSpec) token(fallback string) string {
	if s.Token != "" {
		return s.Token
	}
	return fallback
}

// GitConfig enables git mode.
type GitConfig struct {
	// Repos are the board's domains, primary first. Only the primary is
	// served for now.
	Repos []RepoSpec
	// Token is the push/fetch credential (HTTPS basic auth) for the
	// repositories that name none of their own; empty means an
	// unauthenticated transport (local file remotes, tests).
	Token string
	// App mints the server credential per repository from a GitHub App
	// installation instead of a static token: nothing to issue by hand,
	// nothing that quietly expires in a .env file. A repository that names
	// its own Token keeps it; the App covers the rest. GitHub only.
	App *forge.GitHubApp
	// Forge is the code host the repositories live on: it says how the
	// token travels over HTTPS (the basic-auth username differs per forge).
	// GitHub when nil.
	Forge forge.Forge
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
	lock  *dataLock
}

// OpenGitBackend clones or reopens the configured repository and returns a
// backend over it. log may be nil.
func OpenGitBackend(cfg *GitConfig, log *slog.Logger) (*GitBackend, error) {
	if log == nil {
		log = slog.Default()
	}
	if len(cfg.Repos) == 0 {
		return nil, errors.New("git mode needs at least one --repo")
	}
	lock, err := lockDataDirWaiting(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	store := newBoardStore()
	store.log = log
	be, err := openGitStore(store, cfg, log)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	return &GitBackend{be: be, board: cfg.Repos[0].Name, lock: lock}, nil
}

// Backend is the boardservice.Backend to build a service on.
func (g *GitBackend) Backend() boardservice.Backend { return g.be }

// AttachPersonal attaches the login's personal repository, if the primary
// links one, using token to clone and push — what `aeman mcp` does for the
// local user at start, the way the HTTP server does for each visitor.
func (g *GitBackend) AttachPersonal(ctx context.Context, login, token string) error {
	return g.be.attachPersonal(ctx, login, token)
}

// Drain waits for the write queue and pushes — what a stdio MCP process
// does before it exits, so a client that closes the pipe right after a
// mutation loses nothing.
// It reports a queue that did not empty as an ERROR rather than pushing on
// in silence: what is left never became a commit and does not survive this
// process, so a caller that logs the error is the only thing standing
// between a lost change and nobody knowing.
func (g *GitBackend) Drain(ctx context.Context) error {
	if left := g.be.store.waitDrained(ctx); left > 0 {
		return fmt.Errorf("%w: %d change(s) never became commits and are lost with this process",
			errQueueNotDrained, left)
	}
	return g.be.syncNowWaiting(ctx, storeKey(g.board))
}

// errQueueNotDrained is a drain that ran out of time with writes still in the
// queue. It is not a push failure — unpushed COMMITS sit in the clone and the
// next start sends them — but a loss: these changes never became commits at
// all, and nothing outlives the process holding them.
var errQueueNotDrained = errors.New("the write queue did not drain")

// UnpushedAge is how long the oldest commit this process has failed to push
// has been waiting; zero when everything has landed. A stdio process lived
// one editor session and reported a failed push on the client's stderr; a
// daemon runs unattended for weeks, so this is what tells one that is
// working apart from one whose credential died a week ago.
func (g *GitBackend) UnpushedAge() time.Duration { return g.be.unpushedAge(storeKey(g.board)) }

// Close releases this process's claim on the data directory, so the next
// `aeman serve` or `aeman mcp` may open the clones. It does not stop the
// background sync, so it belongs on a process's way out rather than
// mid-life.
func (g *GitBackend) Close() error { return g.lock.Close() }

// openGitStore is the shared core: clone or reopen every domain under the
// data dir, check each has a board, build the store backend with its sync.
// Every configured repository must be initialised — a server that invents
// a domain on a typo in --repo is worse than one that stops.
func openGitStore(store *boardStore, cfg *GitConfig, log *slog.Logger) (*storeBackend, error) {
	f := cfg.Forge
	if f == nil {
		f = forge.NewGitHub()
	}
	// Issue/PR titles are still resolved against GitHub only; another
	// forge's token has no business there, so the lookups go
	// unauthenticated. The primary's credential does the asking — the
	// links point anywhere, and a per-domain token is not more entitled.
	linksToken := ""
	if f.Kind() == forge.GitHub && len(cfg.Repos) > 0 {
		linksToken = cfg.Repos[0].token(cfg.Token)
	}
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
		if tok := spec.token(cfg.Token); tok != "" {
			remote.Auth = f.GitAuth(tok)
		} else if cfg.App != nil {
			// Mint once before the clone: a repository the app is not
			// installed on must fail as itself (AppNotInstalledError — the
			// one startup trouble a page with a button fixes), not as a
			// bare 401 from the git transport.
			mintCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := cfg.App.Token(mintCtx, spec.URL)
			cancel()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", spec.Name, err)
			}
			// The installation token is asked for on every request, so one
			// renewed between two pushes is picked up without re-wiring.
			remote.Auth = cfg.App.GitAuthFor(spec.URL)
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
	links := newForgeLinks(githubAPIBase, &http.Client{Timeout: 10 * time.Second}, linksToken)
	if linksToken == "" && cfg.App != nil && f.Kind() == forge.GitHub && len(cfg.Repos) > 0 {
		app, primary := cfg.App, cfg.Repos[0].URL
		links.tokenFn = func(ctx context.Context) string {
			tok, err := app.Token(ctx, primary)
			if err != nil {
				log.Warn("app token for links", "err", err)
			}
			return tok
		}
	}
	return newGitBackend(store, domains, gitOptions{PushDelay: 300 * time.Millisecond, SyncInterval: cfg.SyncInterval,
		MaintainEvery: 24 * time.Hour, HistoryMax: cfg.HistoryMax, Logger: log,
		DataDir: cfg.DataDir, RepoOpts: opts, Forge: f,
		// Issue/PR titles in card descriptions are read with the push
		// credential — the store has no per-visitor token any more.
		Links: links}), nil
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

// errUnbornRemote is a repository with no commits at all: the server refuses
// to invent a board in it (initHint names the command that does), while a
// personal repository a person just created is given one on the spot.
var errUnbornRemote = errors.New("repository has no board yet")

func initHint(url string) error {
	return fmt.Errorf("%w: run `aeman init --repo %s` first (%s)", errUnbornRemote, url, url)
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
//
// A queue that did not empty is SAID OUT LOUD. What is left at this point
// never became a commit, so it is not waiting in the clone for the next start
// — it is in this process's memory and goes with it, though the person who
// made those changes was told they were saved. Nothing else reports it
// either: /healthz counts commits in the clone, so a write that never became
// one is invisible there. A line in the log at the moment it happens is the
// only trace such a loss can leave.
func (s *Server) drainAndPush(ctx context.Context) error {
	if left := s.store.waitDrained(ctx); left > 0 {
		s.log.Error("the write queue did not drain before shutdown; changes that never became commits are lost",
			"unsaved", left)
	}
	if s.gitBE == nil {
		return nil
	}
	boardID := s.gitBoard()
	return s.gitBE.syncNowWaiting(ctx, storeKey(boardID))
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
