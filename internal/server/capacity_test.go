package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// A lead sets a person's capacity through the roster endpoint and gets the
// board back with the number beside the person — derived until then, theirs
// from now on. A number that could not be a week is 422; a body that says
// nothing is 400.
func TestAPIPatchPersonSetsTheCapacityTheBoardMeasuresAgainst(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", Assignees: []string{"tym83"}, Size: board.SizeL, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)

	rec := do(t, srv, http.MethodPatch, "/api/v1/people/tym83", `{"capacity":40}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var info apiserver.BoardInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	var m apiserver.Member
	for _, x := range info.Metadata.Members {
		if x.Login == "tym83" {
			m = x
		}
	}
	if m.Capacity != 40 || m.CapacityDerived || m.Load != 4 {
		t.Fatalf("member = %+v, want capacity 40 (set), load 4", m)
	}

	if rec := do(t, srv, http.MethodPatch, "/api/v1/people/tym83", `{"capacity":-5}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a negative capacity answers 422, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPatch, "/api/v1/people/tym83", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a body with no capacity answers 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
