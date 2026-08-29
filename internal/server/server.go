// Package server implements the aeman HTTP server: the embedded single-page
// application, the /api/v1 resource API and watch stream, the MCP transport,
// and the board store over the board's git repositories. The browser never
// holds a credential: identity is resolved server-side (the local gh login,
// or per-user OAuth sessions) and the push credential is the server's.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/ghcli"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/web"
)

// githubAPIBase is the forge's REST base: the visitor's repository
// permissions and the board members' avatars are asked there.
const githubAPIBase = "https://api.github.com"

// defaultAddr is used when Options.Addr is empty.
const defaultAddr = "127.0.0.1:8765"

// Options configures a Server.
type Options struct {
	// Addr is the listen address, e.g. "127.0.0.1:8765".
	Addr string
	// Version is reported to the frontend via /api/config.
	Version string
	// Logger receives structured logs; slog.Default() is used when nil.
	Logger *slog.Logger
	// Auth, when non-nil, enables OAuth multi-user mode: each visitor signs
	// in with the forge, and their own token decides which of the board's
	// repositories they may read and write.
	Auth *OAuthConfig
	// Git is the board's storage — its repositories (see gitmode.go). A
	// server without it serves no board; tests inject a service instead.
	Git *GitConfig
	// Forge is the code host behind the board — the identity provider,
	// the authority on repository access, the directory of names and
	// avatars, the git credential's dialect. GitHub when nil.
	Forge forge.Forge
	// CLI is the forge's command-line tool standing in for the signed-in
	// person on a single-user server (gh, glab); the gh CLI when nil. Unused
	// in OAuth mode.
	CLI forge.CLI
}

// Server is the aeman local HTTP server.
type Server struct {
	opts Options
	// build identifies the frontend bundle this binary carries — the hash of
	// its index.html. The SPA compares it against the one it started with and
	// offers a reload when a new build is being served, which is the only way
	// a long-open tab learns it is running yesterday's code.
	build string
	log   *slog.Logger
	// forge is the code host behind the board; cli its command-line tool
	// standing in for the signed-in person on a single-user server; people
	// the directory of names and avatars the forge fills.
	forge      forge.Forge
	cli        forge.CLI
	people     *people
	auth       *authManager
	httpClient *http.Client
	handler    http.Handler
	// store is the in-memory board cache and watch hub the /api/v1 watch and
	// snapshot read from; it also keeps API/MCP mutations fast.
	store *boardStore
	// gitBE is the store's backend in git mode — one shared backend over the
	// clone, not one per request; nil otherwise. gitCfg is its config.
	gitBE  *storeBackend
	gitCfg *GitConfig
	// visibleBE is gitBE as a visitor may use it — reads projected onto the
	// domains the request's rights can read, writes checked against them;
	// access decides those rights per visitor. Both nil outside git mode.
	visibleBE *visibleBackend
	access    domainAccess
	// setup, when non-nil, is the server waiting for its GitHub App to be
	// installed on a board repository — serving the page that says so
	// instead of the board. setupMu guards it and serialises retries.
	setupMu sync.Mutex
	setup   *setupState

	// apiTokens resolves the token (and login) for an /api/v1 request. It
	// defaults to tokenForRequest and is overridden in tests.
	apiTokens func(*http.Request) (token, login string, err error)
	// newService builds the board service for an /api/v1 request. It defaults
	// to boardservice.New over the visitor's view of the shared store and is
	// overridden in tests with a fake Backend.
	newService func(*http.Request) (*boardservice.Service, error)
}

// setupState is the server waiting for its GitHub App to be installed on a
// board repository: the reason, the install link, and what a retry needs.
type setupState struct {
	problem    string
	installURL string
	forge      forge.Forge
}

// initGit opens the board's repositories and wires the visitor-facing
// backend over them. Called at start, and again from /auth/setup when the
// start left the server waiting for the app to be installed.
func (s *Server) initGit(opts Options, f forge.Forge) error {
	if err := s.openGit(opts.Git); err != nil {
		return err
	}
	names := make([]string, 0, len(opts.Git.Repos))
	for _, r := range opts.Git.Repos {
		names = append(names, r.Name)
	}
	s.visibleBE = &visibleBackend{Backend: s.gitBE, primary: names[0], domains: names}
	if opts.Auth != nil {
		// Each visitor brings their own token: the forge says what it
		// may read and write, per domain.
		fa := newForgeAccess(f, s.httpClient, opts.Git.Repos, opts.Git.Token, s.people)
		fa.app = opts.Git.App
		fa.logger = s.log
		s.access = fa
	} else {
		s.access = openAccess{domains: names}
	}
	return nil
}

// inSetup reports whether the server is still waiting for its app to be
// installed; the state itself is returned for the page and the API answer.
func (s *Server) inSetup() (*setupState, bool) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	return s.setup, s.setup != nil
}

// retrySetup tries to open the board again — the person says they have
// installed the app. One retry at a time; success ends the setup state.
func (s *Server) retrySetup() {
	s.setupMu.Lock()
	st := s.setup
	s.setupMu.Unlock()
	if st == nil {
		return
	}
	if err := s.initGit(s.opts, st.forge); err != nil {
		s.log.Warn("still waiting for the app installation", "err", err)
		var notInstalled *forge.AppNotInstalledError
		if errors.As(err, &notInstalled) {
			s.setupMu.Lock()
			s.setup = &setupState{problem: err.Error(), installURL: notInstalled.InstallURL, forge: st.forge}
			s.setupMu.Unlock()
		}
		return
	}
	s.setupMu.Lock()
	s.setup = nil
	s.setupMu.Unlock()
	s.log.Info("the app installation arrived; the board is up")
}

// New builds a Server from the given options.
func New(opts Options) (*Server, error) {
	if opts.Addr == "" {
		opts.Addr = defaultAddr
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	dist, err := web.DistFS()
	if err != nil {
		return nil, fmt.Errorf("load embedded frontend: %w", err)
	}

	// One forge behind everything: the sign-in, the access checks, the
	// people directory and the git credential agree on which code host the
	// board lives on. Named by the options, else by the git config, else
	// GitHub — the forge this code base grew up on.
	f := opts.Forge
	if f == nil && opts.Git != nil {
		f = opts.Git.Forge
	}
	if f == nil {
		f = forge.NewGitHub()
	}
	if opts.Git != nil && opts.Git.Forge == nil {
		opts.Git.Forge = f
	}
	cli := opts.CLI
	if cli == nil {
		cli = ghcli.NewTokenSource()
	}
	s := &Server{
		opts:       opts,
		build:      frontendBuild(dist),
		log:        opts.Logger,
		forge:      f,
		cli:        cli,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	s.apiTokens = s.tokenForRequest
	s.newService = s.defaultService
	// The directory of people is read with the server's own credential in
	// OAuth mode and with the CLI's on a single-user server.
	var peopleToken func(context.Context) string
	switch {
	case opts.Auth != nil && opts.Git != nil:
		if app := opts.Git.App; app != nil && len(opts.Git.Repos) > 0 {
			// App mode holds no static token: the directory asks with a
			// token minted for the primary repository.
			primary := opts.Git.Repos[0].URL
			peopleToken = func(ctx context.Context) string {
				tok, _ := app.Token(ctx, primary)
				return tok
			}
			break
		}
		tok := opts.Git.Token
		peopleToken = func(context.Context) string { return tok }
	case opts.Auth == nil:
		peopleToken = func(ctx context.Context) string {
			tok, _ := cli.Token(ctx)
			return tok
		}
	}
	s.people = newPeople(f, s.httpClient, peopleToken)
	s.store = newBoardStore()
	s.store.log = s.log
	s.store.member = s.people.member // the forge the identities come from
	if opts.Git != nil {
		if err := s.initGit(opts, f); err != nil {
			// One kind of startup trouble is fixed with a click — the
			// board's GitHub App missing from a repository — so the server
			// comes up anyway and SHOWS the page that says so, instead of a
			// log line for the operator and nothing for everyone else.
			// Installing the app (GitHub redirects to /auth/setup) brings
			// the board up without a restart. Anything else still refuses
			// to start: a page cannot fix a wrong token.
			var notInstalled *forge.AppNotInstalledError
			if !errors.As(err, &notInstalled) {
				return nil, err
			}
			s.setup = &setupState{problem: err.Error(), installURL: notInstalled.InstallURL, forge: f}
		}
	}
	if opts.Auth != nil {
		s.auth = newAuthManager(*opts.Auth, f, opts.Logger)
		s.auth.learned = s.people.learnUser
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", s.handleHealthz)
	// GitHub sends the person here after they install (or update) the
	// board's GitHub App, when the app's Setup URL points at
	// <base>/auth/setup: whatever kept their personal board from attaching
	// is forgotten and tried again at once, so installing the app fixes the
	// board without anyone hunting for a retry button. Registered in every
	// mode — the local one has personal boards too.
	mux.HandleFunc("/auth/setup", s.handleAppSetup)
	mux.HandleFunc("/api/config", s.handleConfig)
	if s.auth != nil {
		mux.HandleFunc("/auth/login", s.auth.handleLogin)
		mux.HandleFunc("/auth/callback", s.auth.handleCallback)
		mux.HandleFunc("/auth/logout", s.auth.handleLogout)
		// The OAuth authorization-server endpoints and the MCP transport are only
		// reachable in OAuth mode, where sessions double as MCP access tokens.
		s.registerOAuthServer(mux)
		s.registerMCP(mux)
	}
	s.registerAPI(mux)
	mux.Handle("/", spaHandler(dist))
	s.handler = logRequests(s.log, s.setupGate(clientIDMiddleware(s.csrfGuard(s.actorMiddleware(s.accessMiddleware(actionMiddleware(staleMiddleware(mux))))))))
	return s, nil
}

// setupGate serves the waiting-for-installation state: the page with the
// install button on every path, a 503 with the actionUrl on the API, while
// healthz and /auth/setup pass through (healthz says "setup" itself, and
// /auth/setup is how the wait ends).
func (s *Server) setupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st, waiting := s.inSetup()
		if !waiting || r.URL.Path == "/api/healthz" || r.URL.Path == "/auth/setup" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSONErrorAction(w, http.StatusServiceUnavailable, st.problem, st.installURL)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(setupPage(st)))
	})
}

// setupPage is the one screen a fresh deployment shows until its app is
// installed: what is wrong, the button that fixes it, and a refresh loop so
// the board appears on its own once the installation lands.
func setupPage(st *setupState) string {
	return `<!doctype html>
<meta charset="utf-8">
<meta http-equiv="refresh" content="7">
<title>aeman — one step left</title>
<style>
  body { font: 16px/1.5 -apple-system, sans-serif; max-width: 40rem; margin: 4rem auto; padding: 0 1rem; color: #24292f; }
  .btn { display: inline-block; padding: .6rem 1.4rem; border-radius: 6px; background: #1f883d; color: #fff; text-decoration: none; font-weight: 600; }
  code { background: #f2f2f2; padding: .1em .3em; border-radius: 3px; overflow-wrap: anywhere; }
</style>
<h1>One step left</h1>
<p>This board stores itself in git repositories, and its GitHub App cannot reach one of them yet:</p>
<p><code>` + html.EscapeString(st.problem) + `</code></p>
<p><a class="btn" href="` + html.EscapeString(st.installURL) + `">Install the GitHub App</a></p>
<p>Pick the organisation, select the board&#39;s repositories, install. This page checks again every few seconds and becomes the board on its own.</p>
`
}

// csrfGuard rejects cross-site state-changing requests to the token-bearing
// /api/ surface. In local-proxy mode a request is authenticated purely by
// reaching the server, so a browser on another site could otherwise drive
// mutations with the injected gh token; browsers attach an Origin header to
// every cross-site POST (even "simple" text/plain ones, which skip preflight),
// so an Origin mismatch is the tell. A request with no Origin is a non-browser
// client (curl, scripts), which carries no ambient credentials to abuse.
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStateChanging(r.Method) && strings.HasPrefix(r.URL.Path, "/api/") {
			if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin, r) {
				writeJSONError(w, http.StatusForbidden, "cross-site request blocked")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isStateChanging reports whether the method mutates server state.
func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// originAllowed reports whether a request's Origin may drive a mutation:
// same-origin always, plus localhost (the Vite dev proxy) in local-proxy mode.
func (s *Server) originAllowed(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Host == r.Host {
		return true
	}
	if s.auth == nil {
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	}
	return false
}

// URL returns the address the server can be reached on in a browser.
func (s *Server) URL() string {
	host := s.opts.Addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	host = strings.Replace(host, "0.0.0.0", "127.0.0.1", 1)
	return "http://" + host
}

// Run starts the server and blocks until ctx is cancelled or it fails.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.opts.Addr, err)
	}

	httpServer := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("aeman is listening", "url", s.URL())
		errCh <- httpServer.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		// Flush the write-behind queue (in-flight op included) on its own
		// generous budget first — a restart must not silently drop changes
		// users already saw applied. The container's stop grace period has to
		// exceed drain + shutdown.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err := s.drainAndPush(drainCtx); err != nil {
			// The commits are on disk for the next start; say so and go.
			s.log.Warn("final push failed; unpushed commits stay in the clone", "err", err)
		}
		drainCancel()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// actorMiddleware stamps the acting user's login onto the request context, so
// boardservice mutations can attribute the activity events they record. Only
// /api/v1 mutations read it; resolution is the same session/token lookup the
// handlers already use, so this adds no extra round-trips.
func (s *Server) actorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1") {
			if _, login, err := s.apiTokens(r); err == nil && login != "" {
				r = r.WithContext(boardservice.WithActor(r.Context(), login))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) tokenForRequest(r *http.Request) (token, login string, err error) {
	if s.auth != nil {
		sess, ok := s.auth.session(r)
		if !ok {
			return "", "", errors.New("not signed in")
		}
		return sess.token, sess.login, nil
	}
	tok, err := s.cli.Token(r.Context())
	if err != nil {
		return "", "", err
	}
	login, _ = s.cli.Login(r.Context())
	return tok, login, nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	if st, waiting := s.inSetup(); waiting {
		writeJSON(w, http.StatusOK, map[string]string{"status": "setup", "error": st.problem, "actionUrl": st.installURL})
		return
	}
	resp := map[string]any{"status": "ok"}
	if s.gitBE != nil {
		// A push that cannot land must not be discovered a week later: the
		// oldest unpushed commit's age is here, and past the threshold the
		// status says so.
		boardID := s.gitBoard()
		age := s.gitBE.unpushedAge(storeKey(boardID))
		resp["unpushedAgeSeconds"] = int(age.Seconds())
		if age > s.unpushedWarn() {
			resp["status"] = "degraded"
		}
		// How long ago the cache was last known to be the remote — a full
		// read or a fetch that found nothing new. A number that keeps
		// growing means the sync is not running, and the next visitor after
		// a break will pay for a blocking re-read.
		resp["cacheAgeSeconds"] = int(s.gitBE.store.entry(storeKey(boardID)).age().Seconds())
		// What the merge of the domains had to resolve: duplicate roster
		// names (a maintainer merges them by hand) and torn-move ghosts
		// (maintenance removes them once the destination has landed).
		aliases, ghosts := s.gitBE.git.mb.Issues()
		if len(aliases) > 0 {
			list := make([]map[string]string, 0, len(aliases))
			for _, a := range aliases {
				list = append(list, map[string]string{"kind": a.Kind, "name": a.Name, "domain": a.Domain, "id": a.ID, "winner": a.Winner})
			}
			resp["aliases"] = list
		}
		if len(ghosts) > 0 {
			list := make([]map[string]string, 0, len(ghosts))
			for _, g := range ghosts {
				list = append(list, map[string]string{"id": g.ID, "domain": g.Domain, "current": g.Current})
			}
			resp["ghosts"] = list
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// configResponse is returned by /api/config to bootstrap the frontend.
type configResponse struct {
	Mode           string `json:"mode"`
	Version        string `json:"version"`
	Login          string `json:"login,omitempty"`
	TokenAvailable bool   `json:"tokenAvailable"`
	Authenticated  bool   `json:"authenticated"`
	AuthURL        string `json:"authUrl,omitempty"`
	LogoutURL      string `json:"logoutUrl,omitempty"`
	// Tz is the board's day time zone (IANA name): the SPA computes "today"
	// in it so every user sees the same board day.
	Tz string `json:"tz"`
	// Build identifies the served frontend bundle; a running SPA whose own
	// build differs is looking at stale code and says so.
	Build string `json:"build,omitempty"`
	// Forge names the code host behind the board (github, gitlab);
	// ForgeLabel is its display name for the sign-in copy; ForgeHost the
	// host the board's repositories live on; CLI the tool a single-user
	// server reads the identity from (gh, glab).
	Forge      string `json:"forge"`
	ForgeLabel string `json:"forgeLabel"`
	ForgeHost  string `json:"forgeHost,omitempty"`
	CLI        string `json:"cli"`
}

// forgeHost is the host the board's repositories live on — the primary's,
// else the forge's public one.
func (s *Server) forgeHost() string {
	if s.gitCfg != nil && len(s.gitCfg.Repos) > 0 {
		if h := forge.HostOf(s.gitCfg.Repos[0].URL); h != "" {
			return h
		}
	}
	if s.forge.Kind() == forge.GitLab {
		return "gitlab.com"
	}
	return "github.com"
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	resp := configResponse{
		Version:    s.opts.Version,
		Build:      s.build,
		Tz:         board.LocationName(),
		Forge:      string(s.forge.Kind()),
		ForgeLabel: s.forge.Label(),
		ForgeHost:  s.forgeHost(),
		CLI:        cliName(s.forge),
	}
	if s.auth != nil {
		resp.Mode = "oauth"
		resp.AuthURL = "/auth/login"
		resp.LogoutURL = "/auth/logout"
		if sess, ok := s.auth.session(r); ok {
			resp.Authenticated = true
			resp.TokenAvailable = true
			resp.Login = sess.login
		}
	} else {
		resp.Mode = "local-proxy"
		if _, err := s.cli.Token(r.Context()); err == nil {
			resp.Authenticated = true
			resp.TokenAvailable = true
			if login, err := s.cli.Login(r.Context()); err == nil {
				resp.Login = login
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeJSONErrorAction is a refusal with something to click: actionUrl is
// the page that fixes the trouble (installing the board's GitHub App), for
// the UI to render as a button rather than a URL buried in prose.
func writeJSONErrorAction(w http.ResponseWriter, status int, msg, actionURL string) {
	if actionURL == "" {
		writeJSONError(w, status, msg)
		return
	}
	writeJSON(w, status, map[string]string{"error": msg, "actionUrl": actionURL})
}

// frontendBuild fingerprints the embedded bundle: index.html names the hashed
// asset files, so its own hash changes with every build.
func frontendBuild(root fs.FS) string {
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(index)
	return hex.EncodeToString(sum[:8])
}

// spaHandler serves files from the embedded frontend, falling back to
// index.html for client-side routes.
//
// Caching matters here: the bundle's filename carries a content hash, so it can
// be cached forever, but the document that POINTS at it must not be — an
// embedded file system reports no modification time, leaving the browser
// without a validator and free to reuse index.html on its own judgement. That
// is how a reloaded tab keeps running yesterday's build after a deploy.
func spaHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(root, clean); err == nil {
			if clean == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "frontend is not built yet; run `make frontend`", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

// logRequests logs each request with method, path and duration.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		dur := time.Since(start)
		switch {
		// A slow request is the fact that explains every "aeman is down"
		// report; at Debug they were invisible on production. The watch
		// socket is exempt — it is SUPPOSED to stay open for hours.
		case dur > 5*time.Second && !strings.HasPrefix(r.URL.Path, "/api/v1/watch"):
			log.Warn("slow request", "method", r.Method, "path", r.URL.Path, "dur", dur)
		case dur > time.Second && !strings.HasPrefix(r.URL.Path, "/api/v1/watch"):
			log.Info("slow request", "method", r.Method, "path", r.URL.Path, "dur", dur)
		default:
			log.Debug("request", "method", r.Method, "path", r.URL.Path, "dur", dur)
		}
	})
}

// clientIDMiddleware stashes the caller's self-assigned client id (the
// X-Aeman-Client header) on the request context. The board store uses it to
// avoid echoing a client's own changes back over its watch stream.
func clientIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("X-Aeman-Client"); id != "" {
			r = r.WithContext(withClientID(r.Context(), id))
			// The card the request ADDRESSES (the {uid} of /cards/{uid}/...)
			// scopes the echo suppression: only that card's change is the
			// author's own optimistic state; everything else a request
			// touches — batch fan-outs, cascades — echoes even to them.
			if rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/cards/"); ok && rest != "" {
				uid := rest
				if i := strings.IndexByte(uid, '/'); i >= 0 {
					uid = uid[:i]
				}
				if uid != "" {
					r = r.WithContext(withTargetItem(r.Context(), uid))
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
