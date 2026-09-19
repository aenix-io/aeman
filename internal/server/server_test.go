package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aenix-io/aeman/internal/nonet"
)

// guardedClient covers the transport httptest owns, which never consults
// the default transport replaced by nonet.Block in TestMain.
func guardedClient(srv *httptest.Server) *http.Client {
	c := srv.Client()
	c.Transport = nonet.Guard(c.Transport)
	return c
}

func TestServerTestClientsStayOnLoopback(t *testing.T) {
	const external = "http://192.0.2.1/user"
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, external, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer local.Close()
	for name, client := range map[string]*http.Client{
		"default":  newTestServer(t).httpClient,
		"httptest": guardedClient(local),
	} {
		t.Run(name, func(t *testing.T) {
			client.Timeout = time.Second
			for _, target := range []string{local.URL, strings.Replace(local.URL, "127.0.0.1", "localhost", 1)} {
				resp, err := client.Get(target)
				if err != nil {
					t.Fatalf("loopback request: %v", err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					t.Fatalf("loopback status = %d, want 204", resp.StatusCode)
				}
			}
			for _, target := range []string{external, local.URL + "/redirect"} {
				resp, err := client.Get(target)
				if resp != nil {
					_ = resp.Body.Close()
				}
				if err == nil || !strings.Contains(err.Error(), "tried to reach 192.0.2.1") {
					t.Fatalf("%s: error = %v; want the guard's refusal", target, err)
				}
			}
		})
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := New(Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.handler = conforms(t, srv.handler)
	return srv
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %q, want status ok", rec.Body.String())
	}
}

func TestSPAHandlerServesAndFallsBack(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>aeman</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h := spaHandler(fsys)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", rec.Code, http.StatusOK)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ford/today", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("fallback status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "aeman") {
		t.Fatalf("fallback body = %q, want index.html", rec.Body.String())
	}
}

func TestURLNormalisesHost(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8765": "http://127.0.0.1:8765",
		":8765":          "http://127.0.0.1:8765",
		"0.0.0.0:9000":   "http://127.0.0.1:9000",
	}
	for addr, want := range cases {
		s := &Server{opts: Options{Addr: addr}}
		if got := s.URL(); got != want {
			t.Errorf("URL(%q) = %q, want %q", addr, got, want)
		}
	}
}

// csrfGuard blocks a cross-site state-changing request to /api (a browser
// attaches Origin to every cross-site POST), while allowing same-origin,
// no-Origin (non-browser) callers, and safe methods.
func TestCSRFGuard(t *testing.T) {
	local := &Server{}                     // local-proxy mode (auth nil)
	oauth := &Server{auth: &authManager{}} // OAuth mode

	req := func(method, origin, host string) *http.Request {
		r := httptest.NewRequest(method, "/api/v1/views/team/cards", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	run := func(s *Server, r *http.Request) int {
		rec := httptest.NewRecorder()
		s.csrfGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(rec, r)
		return rec.Code
	}

	if run(local, req(http.MethodPost, "http://evil.example", "127.0.0.1:8765")) != http.StatusForbidden {
		t.Fatal("cross-site POST must be blocked")
	}
	if run(local, req(http.MethodPost, "http://127.0.0.1:8765", "127.0.0.1:8765")) != http.StatusOK {
		t.Fatal("same-origin POST must pass")
	}
	if run(local, req(http.MethodPost, "http://localhost:5173", "127.0.0.1:8765")) != http.StatusOK {
		t.Fatal("localhost dev origin must pass in local mode")
	}
	if run(oauth, req(http.MethodPost, "http://localhost:5173", "aeman.test")) != http.StatusForbidden {
		t.Fatal("OAuth mode must be strict same-origin")
	}
	if run(local, req(http.MethodPost, "", "127.0.0.1:8765")) != http.StatusOK {
		t.Fatal("no-Origin (non-browser) POST must pass")
	}
	if run(local, req(http.MethodGet, "http://evil.example", "127.0.0.1:8765")) != http.StatusOK {
		t.Fatal("safe methods must not be guarded")
	}
}
