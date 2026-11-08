package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// docs/design/ownership.md over HTTP (ADR 0002): the customer roster's own
// routes, a card's owner and customers in the PATCH and the create, `own`
// on a planning card, and the strategy's filters.
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
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/strategy/cards/other/actions/own", `{"team":"alpha","person":"ann"}`); rec.Code != http.StatusOK {
		t.Fatalf("own a planning card = %d %s", rec.Code, rec.Body.String())
	}
	if o := fake.Card("other"); o.Owner != "ann" || o.Team != "alpha" {
		t.Fatalf("the owned epic = %+v", o)
	}
	// One half comes off alone through the update (the owner picker's "no
	// person" and an unpicked team), the other half staying.
	if rec := do(t, srv, http.MethodPatch, "/api/v1/cards/other", `{"team":""}`); rec.Code != http.StatusOK {
		t.Fatalf("take the epic's team off = %d %s", rec.Code, rec.Body.String())
	}
	if o := fake.Card("other"); o.Owner != "ann" || o.Team != "" {
		t.Fatalf("the epic with its team off = %+v", o)
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/strategy/cards/other/actions/own", `{"team":"alpha"}`); rec.Code != http.StatusOK {
		t.Fatalf("a team alone on a planning card = %d %s", rec.Code, rec.Body.String())
	}
	if o := fake.Card("other"); o.Owner != "ann" || o.Team != "alpha" {
		t.Fatalf("a team alone took the owner off: %+v", o)
	}

	rec = do(t, srv, http.MethodGet, "/api/v1/views/strategy/cards?customer="+cu.ID, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"uid":"task"`) || strings.Contains(rec.Body.String(), `"uid":"other"`) {
		t.Fatalf("the strategy for Hidora = %d %s", rec.Code, rec.Body.String())
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

// O5 — a customer's delete takes it off every card that names it, in every
// repository, with the server's credential. So a visitor who may write the
// roster (the primary) but not every repository holding such a card would
// be writing where they may not: the delete is refused (403), the roster
// and the cards untouched. Where they may write every one, it goes through.
func TestDeletingACustomerNeedsEveryRepositoryItReaches(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "x", Domain: "closed", Title: "in closed"},
		{ItemID: "y", Domain: "shared", Title: "in shared"},
	}, nil)
	hidora, _ := fake.AddCustomer(context.Background(), board.Board{}, "Hidora")
	acme, _ := fake.AddCustomer(context.Background(), board.Board{}, "Acme")
	fake.Card("x").Customers = []string{hidora.ID}
	fake.Card("y").Customers = []string{acme.ID}
	v := &visibleBackend{Backend: fake, primary: "shared", domains: []string{"shared", "closed"}}
	// Reads the closed repository, writes the shared one only.
	ctx := withRights(context.Background(), rightsOn([]string{"shared", "closed"}, []string{"shared"}))
	bd, err := v.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteCustomer(ctx, bd, hidora.ID); !errors.Is(err, boardservice.ErrForbidden) {
		t.Fatalf("deleting a customer a closed card names = %v, want ErrForbidden", err)
	}
	if c := fake.Card("x"); len(c.Customers) != 1 {
		t.Fatalf("a refused delete reached the card: %v", c.Customers)
	}
	if err := v.DeleteCustomer(ctx, bd, acme.ID); err != nil {
		t.Fatalf("deleting a customer only writable cards name = %v", err)
	}
}
