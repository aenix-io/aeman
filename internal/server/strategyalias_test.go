package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// THE STRATEGY BOARD ANSWERS TO ITS OLD NAME. Scripts and the stats pipeline
// still list /api/v1/views/roadmap/cards: the alias lists exactly what
// /views/strategy/cards lists, its watch opens, and a subscription made by
// the old name tracks the same membership.
func TestTheRoadmapAddressIsTheStrategyBoard(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "work", Title: "Receipts", Parent: "epic", Team: "alpha", SprintStart: today, StartDate: today},
		{ItemID: "loose", Title: "Somewhere else", Team: "alpha", Progress: 100},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	strategy := do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards?status=in-progress", "")
	roadmap := do(t, srv, http.MethodGet, "/api/v1/views/roadmap/cards?status=in-progress", "")
	if strategy.Code != http.StatusOK || roadmap.Code != http.StatusOK || strategy.Body.String() != roadmap.Body.String() {
		t.Fatalf("strategy %d %s\nroadmap %d %s", strategy.Code, strategy.Body, roadmap.Code, roadmap.Body)
	}

	ts := httptest.NewServer(srv.handler)
	defer ts.Close()
	for _, view := range []string{"strategy", "roadmap"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, resp, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/api/v1/views/"+view+"/watch", nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			cancel()
			t.Fatalf("the %s watch did not open: %v", view, err)
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
		cancel()
	}
}

// A subscription made by the old name is the Strategy board's: a task taken
// into work enters it with its plan item, exactly as on a strategy one.
func TestARoadmapSubscriptionTracksTheStrategyBoard(t *testing.T) {
	today := board.TodayIso()
	store := newBoardStore()
	b := board.NewBoardIn("acme", []board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Team: "alpha", Parked: true},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today}}
	e := seedEntry(store, "k", b)
	old, cancel := store.subscribe("k", "v1", &apiserver.Selector{View: "roadmap", Status: "in-progress"}, map[string]bool{"cards": true})
	defer cancel()
	now, cancel2 := store.subscribe("k", "v2", &apiserver.Selector{View: "strategy", Status: "in-progress"}, map[string]bool{"cards": true})
	defer cancel2()
	e.mu.Lock()
	c, _ := board.Find(e.board, "t")
	c.Parked, c.SprintStart, c.StartDate = false, today, today
	e.upsertCard(c)
	e.cardChanged("lead", c, "MODIFIED")
	e.mu.Unlock()
	settled(t, e)
	for name, sub := range map[string]*subscription{"roadmap": old, "strategy": now} {
		if got := took(t, sub, 2); got["epic"].Type != "ADDED" || got["t"].Type != "ADDED" {
			t.Fatalf("the %s subscription heard %v", name, got)
		}
	}
}
