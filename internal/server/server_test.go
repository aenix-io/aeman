package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := New(Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
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
