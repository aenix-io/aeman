package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// The BOARD a caller is standing on is a path segment, and these are the three
// things it answers for: which board a gesture belongs to, whether the card is
// on it, and what a create means there (docs/design/view-scoped-api.md).

// A gesture asked of a board that does not draw it is a route that is not
// there — there is no week to drop into on the Me board, and no "finished in
// the sprint before" on the Triage grid.
func TestAGestureIsAnsweredOnlyByTheBoardThatDrawsIt(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Title: "the work", Team: "alpha", Assignees: []string{"bob"},
			StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	week := board.MondayOf(today)
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/me/cards/c1/actions/place",
		`{"week":"`+week+`"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("a drop into a week on the Me board answered %d, want 404", rec.Code)
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/triage/cards/c1/actions/finished-earlier",
		`{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("finished-earlier on the Triage board answered %d, want 404", rec.Code)
	}
	// And on the board that does draw it, the same press lands.
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/triage/cards/c1/actions/place",
		`{"week":"`+week+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("the Triage board's own drop answered %d: %s", rec.Code, rec.Body.String())
	}
}

// A person cannot press × on a card they cannot see. An agent standing on the
// Me board could: it named a board and then acted on somebody else's card, and
// nothing asked whether that board draws it.
func TestAGestureIsRefusedForACardTheBoardDoesNotDraw(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		// Both carry the week they were scheduled in, so the × has somewhere
		// to leave them and the test is about the BOARD and nothing else.
		{ItemID: "mine", Title: "mine", Team: "alpha", Assignees: []string{"bob"},
			Week: board.MondayOf(today), StartDate: today, Day: today, SprintStart: today},
		{ItemID: "theirs", Title: "theirs", Team: "alpha", Assignees: []string{"carol"},
			Week: board.MondayOf(today), StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	if rec := do(t, srv, http.MethodPost, "/api/v1/views/me/cards/theirs/actions/remove",
		`{"intent":"unassign"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("somebody else's card removed from MY board answered %d, want 404", rec.Code)
	}
	// The lead's grid draws it, and so does the escape hatch.
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/team/cards/theirs/actions/remove",
		`{"intent":"unassign"}`); rec.Code != http.StatusOK {
		t.Fatalf("the team board's × answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/all/cards/mine/actions/remove",
		`{"intent":"unassign"}`); rec.Code != http.StatusOK {
		t.Fatalf("view=all answered %d: %s", rec.Code, rec.Body.String())
	}
}

// createdCard is the part of the answer the add-boxes differ by.
type createdCard struct {
	Spec struct {
		Assignees []string `json:"assignees"`
		Week      string   `json:"week"`
		Parked    bool     `json:"parked"`
		Dates     struct {
			Start  string `json:"start"`
			Sprint string `json:"sprint"`
		} `json:"dates"`
	} `json:"spec"`
}

// What a create MEANS is the board's to say, and the four add-boxes differ.
func TestACreateMeansWhatTheBoardMeansByIt(t *testing.T) {
	today := board.TodayIso()
	newSrv := func() *Server {
		fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
		srv := apiServer(t, Options{}, fake)
		srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }
		return srv
	}
	created := func(t *testing.T, rec *httptest.ResponseRecorder) createdCard {
		t.Helper()
		var out createdCard
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	mine := created(t, do(t, newSrv(), http.MethodPost, "/api/v1/views/me/cards",
		`{"title":"mine","team":"alpha","zone":"planned"}`))
	if len(mine.Spec.Assignees) != 1 || mine.Spec.Assignees[0] != "bob" {
		t.Fatalf("the Me board filed it on %v, want the caller", mine.Spec.Assignees)
	}
	theirs := created(t, do(t, newSrv(), http.MethodPost, "/api/v1/views/team/cards",
		`{"title":"theirs","team":"alpha","zone":"planned"}`))
	if len(theirs.Spec.Assignees) != 0 {
		t.Fatalf("the Team grid filed it on %v, want the Unassigned column", theirs.Spec.Assignees)
	}
	week := board.MondayOf(today)
	later := created(t, do(t, newSrv(), http.MethodPost, "/api/v1/views/triage/cards",
		`{"title":"later","team":"alpha","zone":"planned","week":"`+week+`"}`))
	if later.Spec.Week != week || later.Spec.Dates.Start != "" || later.Spec.Dates.Sprint != "" {
		t.Fatalf("a Triage create = %+v, want a week and no day", later.Spec)
	}
	shelved := created(t, do(t, newSrv(), http.MethodPost, "/api/v1/views/backlog/cards",
		`{"title":"someday","team":"alpha","zone":"planned"}`))
	if !shelved.Spec.Parked || shelved.Spec.Week != "" {
		t.Fatalf("a drawer create = %+v, want the shelf alone", shelved.Spec)
	}
	// And a board refuses what it does not own, 422, naming the field.
	rec := do(t, newSrv(), http.MethodPost, "/api/v1/views/me/cards",
		`{"title":"x","team":"alpha","zone":"planned","epic":"Auth"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a column named on the Me board answered %d", rec.Code)
	}
}

// The catalog, so a client reads the surface instead of being told it.
func TestTheCatalogNamesTheBoardsAndTheirGestures(t *testing.T) {
	srv := apiServer(t, Options{}, boardservicetest.New(nil, nil))
	rec := do(t, srv, http.MethodGet, "/api/v1/views", "")
	var out struct {
		Kind  string `json:"kind"`
		Items []struct {
			Name     string   `json:"name"`
			Gestures []string `json:"gestures"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	if out.Kind != "ViewList" || len(out.Items) != len(board.Views()) {
		t.Fatalf("catalog = %+v", out)
	}
	for _, v := range out.Items {
		if v.Name == string(board.ViewTriage) {
			if len(v.Gestures) != 3 {
				t.Fatalf("the Triage board draws %v", v.Gestures)
			}
			return
		}
	}
	t.Fatal("the Triage board is missing from the catalog")
}
