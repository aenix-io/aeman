package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
