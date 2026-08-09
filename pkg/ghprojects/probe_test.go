package ghprojects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type probeResp struct {
	code int
	body string
}

// verdictOf collapses an access decision to what the board store acts on:
// admit (nil), a positive not-found/no-access, or some other upstream error.
func verdictOf(err error) string {
	switch {
	case err == nil:
		return "ADMIT"
	case errors.Is(err, ErrBoardNotFound):
		return "DENY(not-found)"
	default:
		return "DENY(err)"
	}
}

func fixtureServer(t *testing.T, resps []probeResp) *httptest.Server {
	t.Helper()
	i := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		r := resps[min(i, len(resps)-1)]
		i++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(r.code)
		fmt.Fprint(w, r.body)
	}))
}

// CheckBoardAccess is the cheap stand-in for the full board load in the cache
// authorization gate, so its verdict must never be MORE permissive than the
// load's: a token the full load would reject must be rejected by the probe on
// the same fixtures. This table runs identical GitHub responses through both
// paths and requires the verdicts to match — the lockstep this test pins is
// the entire security argument for admitting users by probe.
func TestCheckBoardAccessMatchesFullLoad(t *testing.T) {
	cases := []struct {
		name  string
		resps []probeResp
		want  string
	}{
		{"org resolves, project present (authorized)", []probeResp{
			{200, `{"data":{"organization":{"projectV2":{"id":"PVT_1"}}}}`}}, "ADMIT"},
		{"org resolves, projectV2 null + NOT_FOUND (no access or missing)", []probeResp{
			{200, `{"data":{"organization":{"projectV2":null}},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a ProjectV2"}]}`}}, "DENY(not-found)"},
		{"org null, user resolves with project (user-owned board)", []probeResp{
			{200, `{"data":{"organization":null},"errors":[{"type":"NOT_FOUND","message":"no org"}]}`},
			{200, `{"data":{"user":{"projectV2":{"id":"PVT_2"}}}}`}}, "ADMIT"},
		{"RATE_LIMITED on both", []probeResp{
			{200, `{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`}}, "DENY(err)"},
		{"HTTP 401 revoked token", []probeResp{
			{401, `{"message":"Bad credentials"}`}}, "DENY(err)"},
		{"HTTP 403 SAML or secondary rate limit", []probeResp{
			{403, `{"message":"Resource protected by organization SAML enforcement"}`}}, "DENY(err)"},
		{"INSUFFICIENT_SCOPES (token lacks read:project)", []probeResp{
			{200, `{"data":{"organization":null},"errors":[{"type":"INSUFFICIENT_SCOPES","message":"requires read:project"}]}`},
			{200, `{"data":{"user":null},"errors":[{"type":"INSUFFICIENT_SCOPES","message":"requires read:project"}]}`}}, "DENY(err)"},
		{"partial answer: project id present, FORBIDDEN alongside", []probeResp{
			{200, `{"data":{"organization":{"projectV2":{"id":"PVT_3"}}},"errors":[{"type":"FORBIDDEN","message":"partially forbidden"}]}`}}, "ADMIT"},
		{"empty 200 data", []probeResp{
			{200, `{"data":{}}`}}, "DENY(not-found)"},
		{"HTTP 502 upstream", []probeResp{
			{502, `bad gateway`}}, "DENY(err)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probeSrv := fixtureServer(t, tc.resps)
			defer probeSrv.Close()
			probeErr := New("tok", WithEndpoint(probeSrv.URL)).
				CheckBoardAccess(context.Background(), "acme", 1)

			loadSrv := fixtureServer(t, tc.resps)
			defer loadSrv.Close()
			_, loadErr := New("tok", WithEndpoint(loadSrv.URL)).
				LoadProjectBoard(context.Background(), "acme", 1)

			probeV, loadV := verdictOf(probeErr), verdictOf(loadErr)
			if probeV != tc.want {
				t.Fatalf("probe verdict = %s (%v), want %s", probeV, probeErr, tc.want)
			}
			if probeV != loadV {
				t.Fatalf("probe and full load disagree: probe=%s load=%s (%v / %v)", probeV, loadV, probeErr, loadErr)
			}
			if probeErr == nil && loadErr != nil {
				t.Fatalf("HOLE: probe admits where the full load denies (%v)", loadErr)
			}
		})
	}
}
