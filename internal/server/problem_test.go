package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json (%d %s)", ct, rec.Code, rec.Body.String())
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v (%s)", err, rec.Body.String())
	}
	return p
}

// An error is an RFC 9457 problem. The status says whose fault it was, code
// names the rule for a program, detail is the sentence a person reads.
func TestAPIErrorsAreProblems(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Team: "test"}}, map[string]board.SprintState{
		"test":   {Current: "2026-08-24", ItemID: "st1"},
		"portal": {Current: "2026-08-24", ItemID: "st2"},
	})
	srv := apiServer(t, Options{}, fake)
	for _, tc := range []struct {
		name, method, target, body string
		status                     int
		code                       string
	}{
		{"a card nobody has", http.MethodGet, "/api/v1/cards/nope", "", http.StatusNotFound, "cardNotFound"},
		{"a board nobody has", http.MethodGet, "/api/v1/views/nope/cards", "", http.StatusNotFound, "noSuchView"},
		{"a gesture the board does not draw", http.MethodPost, "/api/v1/views/backlog/cards/c1/actions/untriage", "",
			http.StatusNotFound, "gestureNotOffered"},
		// The body is refused in the parse, before the service is asked: 400.
		// The same route refusing a well-formed change is the service's 422.
		{"a body that does not parse", http.MethodPost, "/api/v1/teams/actions/rename", `{"team":`,
			http.StatusBadRequest, "invalidBody"},
		{"a rule that refused the change", http.MethodPost, "/api/v1/teams/actions/rename", `{"team":"test","to":"portal"}`,
			http.StatusUnprocessableEntity, "teamExists"},
		{"a zone there is no such", http.MethodPatch, "/api/v1/cards/c1", `{"zone":"purple"}`,
			http.StatusBadRequest, "unknownZone"},
		{"a size there is no such", http.MethodPatch, "/api/v1/cards/c1", `{"size":"huge"}`,
			http.StatusBadRequest, "unknownSize"},
		{"a stage there is no such", http.MethodPatch, "/api/v1/cards/c1", `{"stage":"refused"}`,
			http.StatusBadRequest, "unknownStage"},
		{"a day that is not one", http.MethodGet, "/api/v1/logs?day=tomorrow&uids=c1", "",
			http.StatusBadRequest, "invalidDay"},
		{"a capacity that was not sent", http.MethodPost, "/api/v1/teams/actions/capacity", `{"team":"test"}`,
			http.StatusBadRequest, "pointsRequired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, srv, tc.method, tc.target, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body.String())
			}
			p := decodeProblem(t, rec)
			if p.Code != tc.code {
				t.Errorf("code = %q, want %q", p.Code, tc.code)
			}
			if p.Type != "about:blank" || p.Title != http.StatusText(tc.status) || p.Status != tc.status {
				t.Errorf("problem = %+v, want about:blank / %q / %d", p, http.StatusText(tc.status), tc.status)
			}
			if p.Detail == "" {
				t.Error("detail is empty: a person is shown this sentence")
			}
		})
	}
}

// A route nothing serves answers as the API, not as the page behind it: the
// SPA's catch-all stands under every unmatched path and used to hand a caller
// asking for JSON a 200 and index.html. A known path asked with a method it
// does not have lands in the same answer, and for the same reason: the mux
// answers 405 only when no pattern takes the METHOD, and the SPA's pattern
// carries none, so it takes every method and a wrong verb reached the page
// too. Both read "no such route" now; neither ever read "wrong verb".
func TestAnUnknownRouteIsAProblem(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Team: "test"}}, nil)
	srv := apiServer(t, Options{}, fake)
	for _, tc := range []struct{ name, method, target string }{
		{"a path nothing serves", http.MethodGet, "/api/v1/nope"},
		{"an action the card has no door for", http.MethodPost, "/api/v1/cards/c1/actions/nope"},
		{"a card asked with a method it does not answer", http.MethodPut, "/api/v1/cards/c1"},
		{"a collection asked with one", http.MethodDelete, "/api/v1/sprints"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, srv, tc.method, tc.target, "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
			}
			p := decodeProblem(t, rec)
			if p.Code != "unknownRoute" {
				t.Fatalf("code = %q, want unknownRoute", p.Code)
			}
			if !strings.Contains(p.Detail, tc.method) || !strings.Contains(p.Detail, tc.target) {
				t.Errorf("detail = %q; it must say which request found nothing", p.Detail)
			}
		})
	}
	// The catch-all is registered without a method and stands last, so the
	// three routes that are not resources keep their own answers.
	for _, tc := range []struct {
		name, target string
		status       int
	}{
		{"the index", "/api/v1", http.StatusOK},
		{"the document", "/api/v1/openapi.json", http.StatusOK},
		// The handshake fails against a recorder, which is the point: the
		// watch handler ran rather than the catch-all.
		{"the watch", "/api/v1/views/me/watch", http.StatusUpgradeRequired},
	} {
		t.Run(tc.name+" is untouched", func(t *testing.T) {
			if rec := do(t, srv, http.MethodGet, tc.target, ""); rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

// ServeMux canonicalises a path before matching it and normally answers the
// first request with a redirect and an HTML body. The JSON surface owns that
// answer too: an unclean path is not silently changed into another operation,
// and a client can decode the same problem shape it gets for every other path
// no route serves. Config and health live outside v1 but inside the same JSON
// surface, so they carry the guarantee as well.
func TestAnUncleanAPIPathIsAProblem(t *testing.T) {
	srv := apiServer(t, Options{}, boardservicetest.New(nil, nil))
	for _, target := range []string{
		"/api/v1/cards//notes",
		"/api/v1/cards/x/../notes",
		"/api/v1/cards/./notes",
		"/api//config",
		"/api/config/../healthz",
		"/api/x/../healthz",
	} {
		t.Run(target, func(t *testing.T) {
			rec := do(t, srv, http.MethodGet, target, "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
			}
			if location := rec.Header().Get("Location"); location != "" {
				t.Errorf("Location = %q, want no redirect", location)
			}
			p := decodeProblem(t, rec)
			if p.Code != "unknownRoute" {
				t.Fatalf("code = %q, want unknownRoute", p.Code)
			}
			if !strings.Contains(p.Detail, target) {
				t.Errorf("detail = %q; it must retain the path that was refused", p.Detail)
			}
		})
	}
}

// A path too long to echo is clipped, and the clip lands on a rune boundary.
// Cutting by bytes splits a multi-byte rune, and the half that survives is
// not text — it reaches the reader as U+FFFD. Testing that the detail is
// VALID utf-8 does not catch it: the server's own json.Marshal replaces the
// broken byte with the replacement rune, so the answer is well-formed either
// way and only the character itself tells the two apart. The 9-byte prefix is
// what puts byte 120 INSIDE a rune — after the 8-byte "/api/v1/" the cut
// falls between two and a byte cut looks correct there.
func TestALongPathIsClippedWithoutBreakingARune(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Team: "test"}}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/x"+strings.Repeat("я", 80), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if p := decodeProblem(t, rec); strings.ContainsRune(p.Detail, utf8.RuneError) {
		t.Errorf("detail carries a broken rune: %q", p.Detail)
	}

	// The METHOD is the caller's bytes too — the token grammar bounds its
	// length no more than the path's — so clipping one of the two leaves the
	// sentence as long as whichever was left.
	rec = do(t, srv, strings.Repeat("X", 5000), "/api/v1/nope", "")
	if p := decodeProblem(t, rec); len(p.Detail) > 300 {
		t.Errorf("detail is %d bytes: the method is echoed unclipped", len(p.Detail))
	}
}

// What no table row answers is not a rule refusing a change: it is the forge
// failing, and a caller may retry it.
func TestUnclassifiedErrorIsAGatewayFailure(t *testing.T) {
	p := problemFor(errors.New("dial tcp: i/o timeout"))
	if p.Status != http.StatusBadGateway || p.Code != "upstreamFailed" {
		t.Fatalf("problem = %+v, want 502 upstreamFailed", p)
	}
	if p.Detail != "dial tcp: i/o timeout" {
		t.Fatalf("detail = %q, want the error's own text", p.Detail)
	}
}

// A cross-site write is refused before /api/v1 is reached, and answers in the
// same shape as the handlers behind it.
func TestCrossSiteRefusalIsAProblem(t *testing.T) {
	srv := apiServer(t, Options{}, boardservicetest.New(nil, nil))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/presence", nil)
	r.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if p := decodeProblem(t, rec); p.Code != "crossSiteBlocked" {
		t.Fatalf("code = %q, want crossSiteBlocked", p.Code)
	}
}
