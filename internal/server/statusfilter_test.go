package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// O14 over HTTP: the strategy listing takes `status` like its other
// filters, and a status the board does not have is a bad selector.
func TestTheStrategyStatusFilterOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "work", Title: "Receipts", Parent: "epic", Team: "alpha", SprintStart: today, StartDate: today},
		{ItemID: "fin", Title: "Refunds", Parent: "epic", Team: "alpha", Progress: 100, StartDate: today},
		{ItemID: "docs", Title: "Docs", Type: board.TypeEpic},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards?status=in-progress,backlog", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"uid":"work"`) || !strings.Contains(body, `"uid":"epic"`) ||
		strings.Contains(body, `"uid":"fin"`) || strings.Contains(body, `"uid":"docs"`) {
		t.Fatalf("hide done = %d %s", rec.Code, body)
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards?status=done", "")
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, `"uid":"fin"`) || strings.Contains(body, `"uid":"work"`) {
		t.Fatalf("done = %d %s", rec.Code, body)
	}
	// Hide done is every value but done: the empty epic is not done and
	// stays (no-items), the finished task goes.
	rec = do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards?status=backlog,in-progress,locked,no-items", "")
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, `"uid":"docs"`) ||
		!strings.Contains(body, `"uid":"work"`) || strings.Contains(body, `"uid":"fin"`) {
		t.Fatalf("hide done with locked and no-items = %d %s", rec.Code, body)
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards?status=no-items", "")
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, `"uid":"docs"`) ||
		strings.Contains(body, `"uid":"epic"`) {
		t.Fatalf("no-items = %d %s", rec.Code, body)
	}
	if rec := do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards?status=todo", ""); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalidSelector") {
		t.Fatalf("an unknown status = %d %s", rec.Code, rec.Body.String())
	}
}

// O14 over HTTP: an empty epic waiting for another is listed under Locked
// with status.locked, and under Hide done.
func TestTheLockedFilterOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "prod", Title: "Product", Type: board.TypeEpic},
		{ItemID: "work", Title: "Build", Parent: "prod", Team: "alpha", SprintStart: today, StartDate: today},
		{ItemID: "client", Title: "Acme rollout", Type: board.TypeEpic, DependsOn: []string{"prod"}},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }
	for _, q := range []string{"locked", "backlog,in-progress,locked,no-items"} {
		rec := do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards?status="+q, "")
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, `"uid":"client"`) || !strings.Contains(body, `"locked":true`) {
			t.Fatalf("status=%s = %d %s", q, rec.Code, body)
		}
		if q == "locked" && strings.Contains(body, `"metadata":{"uid":"prod"}`) {
			t.Fatalf("the epic waited for is not locked: %s", body)
		}
	}
}
