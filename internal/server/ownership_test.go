package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// docs/design/ownership.md over HTTP (ADR 0002): the customer roster's own
// routes, a card's owner and customers in the PATCH and the create, `own`
// on a planning card, and the roadmap's filters.
func TestOwnershipOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "task", Title: "Receipts", Parent: "epic", Team: "alpha", Parked: true},
		{ItemID: "other", Title: "Docs", Type: board.TypeEpic},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodPost, "/api/v1/customers", `{"name":"Hidora"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add a customer = %d %s", rec.Code, rec.Body.String())
	}
	var cu struct{ ID, Name string }
	_ = json.Unmarshal(rec.Body.Bytes(), &cu)
	if cu.ID == "" || cu.Name != "Hidora" {
		t.Fatalf("the customer = %+v", cu)
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/customers", `{"name":"hidora"}`); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "customerExists") {
		t.Fatalf("a name twice = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/customers/actions/rename", `{"customer":"`+cu.ID+`","to":"Hidora SA"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("rename = %d %s", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, http.MethodPatch, "/api/v1/cards/epic", `{"owner":"ann","customers":["`+cu.ID+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch the epic = %d %s", rec.Code, rec.Body.String())
	}
	if e := fake.Card("epic"); e.Owner != "ann" || len(e.Customers) != 1 {
		t.Fatalf("the epic = %+v", e)
	}
	if rec := do(t, srv, http.MethodPatch, "/api/v1/cards/task", `{"owner":"ann"}`); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "ownerIsThePlans") {
		t.Fatalf("an owner on a task = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/roadmap/cards/other/actions/own", `{"team":"alpha","person":"ann"}`); rec.Code != http.StatusOK {
		t.Fatalf("own a planning card = %d %s", rec.Code, rec.Body.String())
	}
	if o := fake.Card("other"); o.Owner != "ann" || o.Team != "alpha" {
		t.Fatalf("the owned epic = %+v", o)
	}

	rec = do(t, srv, http.MethodGet, "/api/v1/views/roadmap/cards?customer="+cu.ID, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"uid":"task"`) || strings.Contains(rec.Body.String(), `"uid":"other"`) {
		t.Fatalf("the roadmap for Hidora = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodGet, "/api/v1/board", ""); !strings.Contains(rec.Body.String(), `"name":"Hidora SA"`) {
		t.Fatalf("the board's roster: %s", rec.Body.String())
	}

	if rec := do(t, srv, http.MethodPost, "/api/v1/customers/actions/delete", `{"customer":"`+cu.ID+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
	if e := fake.Card("epic"); len(e.Customers) != 0 {
		t.Fatalf("the delete did not reach the epic: %v", e.Customers)
	}
}
