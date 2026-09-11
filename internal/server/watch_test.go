package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// wsAcceptOptions is strict same-origin in OAuth mode and additionally allows
// the Vite dev proxy's localhost origins in local-proxy mode.
func TestWSAcceptOptionsPerMode(t *testing.T) {
	oauth := &Server{auth: &authManager{}}
	if got := oauth.wsAcceptOptions(); len(got.OriginPatterns) != 0 || got.InsecureSkipVerify {
		t.Fatalf("oauth mode must be strict same-origin, got %+v", got)
	}
	local := &Server{}
	got := local.wsAcceptOptions()
	if got.InsecureSkipVerify {
		t.Fatal("origin verification must never be skipped")
	}
	if len(got.OriginPatterns) == 0 {
		t.Fatal("local mode must allow the localhost dev origins")
	}
}

// The Origin check actually rejects a cross-site handshake while allowing the
// localhost dev origin — a remote page cannot open the watch and read the board.
func TestWatchOriginEnforced(t *testing.T) {
	local := &Server{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, local.wsAcceptOptions())
		if err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer ts.Close()

	dial := func(origin string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c, resp, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):], &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": {origin}},
		})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			_ = c.Close(websocket.StatusNormalClosure, "")
		}
		return err
	}

	if err := dial("http://evil.example"); err == nil {
		t.Fatal("cross-site origin was accepted (board is exfiltratable)")
	}
	if err := dial("http://localhost:5173"); err != nil {
		t.Fatalf("localhost dev origin was rejected: %v", err)
	}
}

// THE WATCH IS DIALLED THROUGH THE WHOLE SERVER, not through the handler
// alone, because what breaks it lives in the middleware: the stale-read
// wrapper is not an http.Hijacker, so a watch that goes through it cannot be
// upgraded at all and answers 501. It skipped the watch by comparing the path
// against a literal — and when the BOARD moved into that path, every watch in
// the app broke, with nothing but a silent reconnect loop to say so. No test
// dialled the real route, so the whole suite stayed green.
func TestTheWatchUpgradesThroughTheMiddleware(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Title: "the work", Team: "alpha", Assignees: []string{"bob"},
			StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }
	ts := httptest.NewServer(srv.handler)
	defer ts.Close()

	for _, path := range []string{
		"/api/v1/views/me/watch?day=" + today,
		"/api/v1/views/all/watch",
		"/api/v1/views/triage/watch?team=alpha",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, resp, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+path, nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			code := 0
			if resp != nil {
				code = resp.StatusCode
			}
			cancel()
			t.Fatalf("%s did not upgrade (%d): %v", path, code, err)
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
		cancel()
	}
}
