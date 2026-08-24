// Package server implements the local HTTP server that serves the embedded
// single-page application and proxies GitHub API requests, injecting a token
// obtained from the local gh CLI so the browser never has to hold credentials.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/aenix-io/aeman/internal/ghcli"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/web"
)

// githubAPIBase is the upstream the proxy forwards GitHub requests to.
const githubAPIBase = "https://api.github.com"

// defaultAddr is used when Options.Addr is empty.
const defaultAddr = "127.0.0.1:8765"

// Options configures a Server.
type Options struct {
	// Addr is the listen address, e.g. "127.0.0.1:8765".
	Addr string
	// DefaultOwner is the GitHub org/user pre-selected in the UI (optional).
	DefaultOwner string
	// DefaultProject is the project number pre-selected in the UI (0 = none).
	DefaultProject int
	// LockBoard pins the UI to DefaultOwner/DefaultProject and hides the board
	// picker. Users without access to that board (their own token can't read it)
	// just see an access-denied screen. The /api/v1 API honours it too, ignoring
	// client-supplied owner/project.
	LockBoard bool
	// Version is reported to the frontend via /api/config.
	Version string
	// Logger receives structured logs; slog.Default() is used when nil.
	Logger *slog.Logger
	// Auth, when non-nil, enables GitHub OAuth multi-user mode (each visitor
	// signs in and the proxy uses their own token).
	Auth *OAuthConfig
}

// Server is the aeman local HTTP server.
type Server struct {
	opts Options
	// build identifies the frontend bundle this binary carries — the hash of
	// its index.html. The SPA compares it against the one it started with and
	// offers a reload when a new build is being served, which is the only way
	// a long-open tab learns it is running yesterday's code.
	build      string
	log        *slog.Logger
	tokens     *ghcli.TokenSource
	auth       *authManager
	httpClient *http.Client
	handler    http.Handler
	// store is the in-memory board cache and watch hub the /api/v1 watch and
	// snapshot read from; it also keeps API/MCP mutations fast.
	store *boardStore

	// apiTokens resolves the token (and login) for an /api/v1 request. It
	// defaults to tokenForRequest — the same resolution the /api/github proxy
	// uses — and is overridden in tests.
	apiTokens func(*http.Request) (token, login string, err error)
	// graphqlEndpoint overrides the GitHub GraphQL endpoint (used in tests).
	graphqlEndpoint string
	// newService builds the board service for an /api/v1 request. It defaults to
	// boardservice.New over the per-request ghprojects client (apiClient resolves
	// the OAuth-session or gh-CLI token) and is overridden in tests with a fake
	// Backend.
	newService func(*http.Request) (*boardservice.Service, error)
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

	s := &Server{
		opts:       opts,
		build:      frontendBuild(dist),
		log:        opts.Logger,
		tokens:     ghcli.NewTokenSource(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	s.apiTokens = s.tokenForRequest
	s.newService = s.defaultService
	s.store = newBoardStore()
	s.store.log = s.log
	if opts.Auth != nil {
		s.auth = newAuthManager(*opts.Auth, opts.Logger)
		// The warm roster lives next to the session file: restarts bring the
		// cache back up instead of leaving a cold-load pit (startupWarm).
		if opts.Auth.SessionFile != "" {
			s.store.warmFile = opts.Auth.SessionFile + ".boards"
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", s.handleHealthz)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.Handle("/api/github/", s.githubProxy())
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
	s.handler = logRequests(s.log, clientIDMiddleware(s.csrfGuard(s.actorMiddleware(staleMiddleware(mux)))))
	return s, nil
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

	// Bring the cache up in the background: the boards that were warm before
	// the restart, or the default board in local mode. Nothing waits on it —
	// requests that arrive first simply ride the same detached loads.
	go s.startupWarm()

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
		s.store.waitDrained(drainCtx)
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

type tokenCtxKey struct{}

// githubProxy returns a reverse proxy to the GitHub API that injects the gh
// token. It short-circuits with 401 when no token is available.
func (s *Server) githubProxy() http.Handler {
	target, _ := url.Parse(githubAPIBase)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/api/github")
			pr.Out.URL.RawPath = ""
			if pr.Out.URL.Path == "" {
				pr.Out.URL.Path = "/"
			}
			pr.Out.Host = target.Host
			if tok, ok := pr.In.Context().Value(tokenCtxKey{}).(string); ok {
				pr.Out.Header.Set("Authorization", "Bearer "+tok)
			}
			if pr.Out.Header.Get("Accept") == "" {
				pr.Out.Header.Set("Accept", "application/vnd.github+json")
			}
			pr.Out.Header.Set("User-Agent", "aeman")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.log.Error("github proxy error", "err", err)
			writeJSONError(w, http.StatusBadGateway, "github proxy error: "+err.Error())
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, _, err := s.tokenForRequest(r)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), tokenCtxKey{}, tok)
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tokenForRequest resolves the GitHub token (and login) for a request: from the
// signed-in session in OAuth mode, or from the local gh CLI otherwise.
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
	tok, err := s.tokens.Token(r.Context())
	if err != nil {
		return "", "", err
	}
	login, _ = ghcli.Login(r.Context())
	return tok, login, nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	DefaultOwner   string `json:"defaultOwner,omitempty"`
	DefaultProject int    `json:"defaultProject,omitempty"`
	LockBoard      bool   `json:"lockBoard"`
	// Tz is the board's day time zone (IANA name): the SPA computes "today"
	// in it so every user sees the same board day.
	Tz string `json:"tz"`
	// Build identifies the served frontend bundle; a running SPA whose own
	// build differs is looking at stale code and says so.
	Build string `json:"build,omitempty"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	resp := configResponse{
		Version:        s.opts.Version,
		Build:          s.build,
		DefaultOwner:   s.opts.DefaultOwner,
		DefaultProject: s.opts.DefaultProject,
		LockBoard:      s.opts.LockBoard,
		Tz:             board.LocationName(),
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
		if _, err := s.tokens.Token(r.Context()); err == nil {
			resp.Authenticated = true
			resp.TokenAvailable = true
			if login, err := ghcli.Login(r.Context()); err == nil {
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
		log.Debug("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
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
