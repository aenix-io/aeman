package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// THE STRATEGY BOARD'S OLD NAME IS NO BOARD. Renamed from roadmap on
// 2026-11-08: /api/v1/views/roadmap/cards and its watch answer as any board
// nobody has does — 404 noSuchView — while /views/strategy/ lists and
// watches.
func TestTheOldRoadmapAddressIsNoBoard(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "work", Title: "Receipts", Parent: "epic", Team: "alpha", SprintStart: today, StartDate: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	if w := do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "work") {
		t.Fatalf("strategy: %d %s", w.Code, w.Body)
	}
	w := do(t, srv, http.MethodGet, "/api/v1/views/roadmap/cards", "")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "noSuchView") {
		t.Fatalf("roadmap: %d %s, want 404 noSuchView", w.Code, w.Body)
	}

	ts := httptest.NewServer(srv.handler)
	defer ts.Close()
	for view, opens := range map[string]bool{"strategy": true, "roadmap": false} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, resp, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/api/v1/views/"+view+"/watch", nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if opens != (err == nil) {
			cancel()
			t.Fatalf("the %s watch: err %v, want open=%v", view, err, opens)
		}
		if err == nil {
			_ = c.Close(websocket.StatusNormalClosure, "")
		} else if resp == nil || resp.StatusCode != http.StatusNotFound {
			cancel()
			t.Fatalf("the %s watch was refused with %v, want 404", view, resp)
		}
		cancel()
	}
}
