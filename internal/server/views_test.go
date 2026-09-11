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
		Zone      string   `json:"zone"`
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
		`{"title":"mine","team":"alpha"}`))
	if len(mine.Spec.Assignees) != 1 || mine.Spec.Assignees[0] != "bob" {
		t.Fatalf("the Me board filed it on %v, want the caller", mine.Spec.Assignees)
	}
	// And in the band it adds in: work that came up today is unplanned, and
	// the Me board files it nowhere else.
	if mine.Spec.Zone != "unplanned" {
		t.Fatalf("the Me board filed it as %q, want unplanned", mine.Spec.Zone)
	}
	if rec := do(t, newSrv(), http.MethodPost, "/api/v1/views/me/cards",
		`{"title":"planning","team":"alpha","zone":"planned"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("planning on the Me board answered %d", rec.Code)
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

// A SUBTASK rides on its parent in the Me listing, and the gate must ask the
// listing's own question or it refuses what the board draws: the Me board
// lists every team, so judging the press on the CARD's team drops a parent of
// another team out of the listing and the child with it. The boards whose
// listing does name a team keep the fill.
func TestTheGateAsksTheListingsOwnQuestionAboutTeams(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "parent", Title: "the work", Team: "alpha", Assignees: []string{"bob"},
			StartDate: today, Day: today, SprintStart: today},
		// A mismatch no gesture produces — grouping forces the parent's team
		// — but `aeman migrate` and a direct git write both can, and the gate
		// must not be the thing that refuses what the board is drawing.
		{ItemID: "step", Title: "a step", Team: "beta", Parent: "parent", Assignees: []string{"bob"},
			StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}, "beta": {Current: today, ItemID: "s2"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	if rec := do(t, srv, http.MethodPost, "/api/v1/views/me/cards/step/actions/remove",
		`{"intent":"off-board"}`); rec.Code == http.StatusNotFound {
		t.Fatalf("the × on a subtask the Me board draws answered 404: %s", rec.Body.String())
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

// WHICH CARD A ROUTE ADDRESSES is read in two places that both changed under
// the boards: the as-of guard judges the write by that card, and the echo
// suppression scopes a tab's own change to it. Both used to cut the path at a
// literal prefix, so a gesture made through a board named neither — the guard
// let a write to a settled day through, and the ×'s own change echoed back at
// the tab that made it, undoing the optimistic state on screen.
func TestTheCardARouteAddressesIsFoundThroughItsBoardToo(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{
		"/api/v1/cards/c1":                                    "c1",
		"/api/v1/cards/c1/notes":                              "c1",
		"/api/v1/views/team/cards/c1/actions/remove":          "c1",
		"/api/v1/views/triage/cards/c2/actions/place":         "c2",
		"/api/v1/views/all/cards/c3/actions/finished-earlier": "c3",
		// A collection addresses no card, and neither does anything else.
		"/api/v1/views/team/cards": "",
		"/api/v1/sprints":          "",
		"/api/v1/board":            "",
	} {
		if got := cardOfPath(path); got != want {
			t.Errorf("cardOfPath(%q) = %q, want %q", path, got, want)
		}
	}
}
