// Package server implements the local HTTP server that serves the embedded
// single-page application and proxies GitHub API requests, injecting a token
// obtained from the local gh CLI so the browser never has to hold credentials.
package server

import (
	"context"
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

	"github.com/aenix-org/aeman/internal/ghcli"
	"github.com/aenix-org/aeman/web"
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
	// Version is reported to the frontend via /api/config.
	Version string
	// Logger receives structured logs; slog.Default() is used when nil.
	Logger *slog.Logger
}

// Server is the aeman local HTTP server.
type Server struct {
	opts    Options
	log     *slog.Logger
	tokens  *ghcli.TokenSource
	handler http.Handler
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
		opts:   opts,
		log:    opts.Logger,
		tokens: ghcli.NewTokenSource(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", s.handleHealthz)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.Handle("/api/github/", s.githubProxy())
	mux.Handle("/", spaHandler(dist))
	s.handler = logRequests(s.log, mux)
	return s, nil
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
		tok, err := s.tokens.Token(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "GitHub token unavailable: "+err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), tokenCtxKey{}, tok)
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
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
	DefaultOwner   string `json:"defaultOwner,omitempty"`
	DefaultProject int    `json:"defaultProject,omitempty"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	resp := configResponse{
		Mode:           "local-proxy",
		Version:        s.opts.Version,
		DefaultOwner:   s.opts.DefaultOwner,
		DefaultProject: s.opts.DefaultProject,
	}
	if _, err := s.tokens.Token(r.Context()); err == nil {
		resp.TokenAvailable = true
		if login, err := ghcli.Login(r.Context()); err == nil {
			resp.Login = login
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

// spaHandler serves files from the embedded frontend, falling back to
// index.html for client-side routes.
func spaHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(root, clean); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "frontend is not built yet; run `make frontend`", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
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
