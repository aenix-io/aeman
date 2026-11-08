package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// N7 over HTTP (ADR 0002): PATCH spec.dependsOn writes the whole list; the
// card reads back locked with its blocker; a cycle and a bad target are
// refused by name.
func TestDependenciesOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "db", Title: "DB", Team: "alpha", Parked: true},
		{ItemID: "api", Title: "API", Team: "alpha", Parked: true},
		{ItemID: "rev", Title: "review: DB", ReviewOf: "db", Team: "alpha"},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/api", `{"dependsOn":["db"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body.String())
	}
	var c apiserver.Card
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	if len(c.Spec.DependsOn) != 1 || !c.Status.Locked || len(c.Status.BlockedBy) != 1 || c.Status.BlockedBy[0].UID != "db" {
		t.Fatalf("the card = %+v / %+v", c.Spec.DependsOn, c.Status)
	}
	for body, code := range map[string]string{
		`{"dependsOn":["api"]}`:  "dependencyCycle",
		`{"dependsOn":["rev"]}`:  "dependencyTarget",
		`{"dependsOn":["nope"]}`: "dependencyTarget",
	} {
		if rec := do(t, srv, http.MethodPatch, "/api/v1/cards/db", body); rec.Code != http.StatusUnprocessableEntity ||
			!strings.Contains(rec.Body.String(), code) {
			t.Fatalf("%s = %d %s, want 422 %s", body, rec.Code, rec.Body.String(), code)
		}
	}
	if rec := do(t, srv, http.MethodPatch, "/api/v1/cards/api", `{"dependsOn":[]}`); rec.Code != http.StatusOK || len(fake.Card("api").DependsOn) != 0 {
		t.Fatalf("clear = %d %v", rec.Code, fake.Card("api").DependsOn)
	}
}

// N4 — a visitor's list is the readable part: writing it keeps a dependency
// on a card they cannot read (their board does not hold it, so their list
// cannot name it), and a write they may not make is refused.
func TestAVisitorsDependencyWriteKeepsWhatTheyCannotSee(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "api", Title: "API", Team: "alpha", Parked: true, DependsOn: []string{"secret", "db"}, Domain: "shared"},
		{ItemID: "db", Title: "DB", Team: "alpha", Parked: true, Domain: "shared"},
		{ItemID: "auth", Title: "Auth", Team: "alpha", Parked: true, Domain: "shared"},
		{ItemID: "secret", Title: "Secret", Team: "alpha", Parked: true, Domain: "closed"},
	}, nil)
	v := &visibleBackend{Backend: fake, primary: "shared", domains: []string{"shared", "closed"}}
	ctx := withRights(t.Context(), rightsOn([]string{"shared"}, []string{"shared"}))
	whole, _ := fake.LoadBoard(ctx, "acme")
	api, _ := board.Find(whole, "api")
	if err := v.SetDependsOn(ctx, whole, api, []string{"auth"}); err != nil {
		t.Fatal(err)
	}
	if got := fake.Card("api").DependsOn; len(got) != 2 || got[0] != "auth" || got[1] != "secret" {
		t.Fatalf("dependsOn = %v, want auth (the visitor's) and secret (kept)", got)
	}
	readOnly := withRights(t.Context(), rightsOn([]string{"shared"}, nil))
	if err := v.SetDependsOn(readOnly, whole, api, nil); err == nil {
		t.Fatal("a visitor who may not write the card wrote its dependencies")
	}
}
