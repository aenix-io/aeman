package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// O14 over HTTP: the roadmap listing takes `status` like its other
// filters, and a status the board does not have is a bad selector.
func TestTheRoadmapStatusFilterOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "work", Title: "Receipts", Parent: "epic", Team: "alpha", SprintStart: today, StartDate: today},
		{ItemID: "fin", Title: "Refunds", Parent: "epic", Team: "alpha", Progress: 100, StartDate: today},
		{ItemID: "docs", Title: "Docs", Type: board.TypeEpic},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodGet, "/api/v1/views/roadmap/cards?status=in-progress,backlog", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"uid":"work"`) || !strings.Contains(body, `"uid":"epic"`) ||
		strings.Contains(body, `"uid":"fin"`) || strings.Contains(body, `"uid":"docs"`) {
		t.Fatalf("hide done = %d %s", rec.Code, body)
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/views/roadmap/cards?status=done", "")
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, `"uid":"fin"`) || strings.Contains(body, `"uid":"work"`) {
		t.Fatalf("done = %d %s", rec.Code, body)
	}
	if rec := do(t, srv, http.MethodGet, "/api/v1/views/roadmap/cards?status=todo", ""); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalidSelector") {
		t.Fatalf("an unknown status = %d %s", rec.Code, rec.Body.String())
	}
}
