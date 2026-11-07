package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The board's address is the document's: /?view=…&card=… is served the
// app, as the root always is, so a deep link opens the board.
func TestADeepLinkIsServedTheApp(t *testing.T) {
	h := spaHandler(fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>aeman</title>")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?view=roadmap&card=01ABC&day=2026-11-06", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(rec.Body.String(), "aeman") {
		t.Fatalf("a deep link answered %d (cache %q): %s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
}
