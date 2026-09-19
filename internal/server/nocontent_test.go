package server

import (
	"net/http"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// A write that leaves nothing to show answers 204 with no body — whichever
// family it is in, and whether it used to say 200 or 201.
func TestNothingToReturnIs204(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "test"},
		{ItemID: "c2", Team: "test"},
	}, map[string]board.SprintState{"test": {Current: "2026-08-24", ItemID: "st1"}})
	srv := apiServer(t, Options{}, fake)
	for _, tc := range []struct{ name, method, target, body string }{
		{"a project added", http.MethodPost, "/api/v1/projects", `{"name":"engineering"}`},
		{"an epic added", http.MethodPost, "/api/v1/epics", `{"name":"Launch","project":"engineering"}`},
		{"an epic renamed", http.MethodPost, "/api/v1/epics/actions/rename", `{"project":"engineering","epic":"Launch","to":"Ship"}`},
		{"a deadline added", http.MethodPost, "/api/v1/deadlines", `{"week":"2026-08-24","project":"engineering"}`},
		{"a process added", http.MethodPost, "/api/v1/processes", `{"name":"audit","project":"engineering"}`},
		{"a team renamed", http.MethodPost, "/api/v1/teams/actions/rename", `{"team":"test","to":"platform"}`},
		{"a team's capacity set", http.MethodPost, "/api/v1/teams/actions/capacity", `{"team":"platform","points":40}`},
		{"a card moved", http.MethodPost, "/api/v1/cards/c1/actions/move", `{"after":"c2"}`},
		{"presence told", http.MethodPost, "/api/v1/presence", `{"card":"c1"}`},
		{"a card deleted", http.MethodDelete, "/api/v1/cards/c1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, srv, tc.method, tc.target, tc.body)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body.String())
			}
			if rec.Body.Len() != 0 {
				t.Fatalf("body = %q, want none", rec.Body.String())
			}
		})
	}
}
