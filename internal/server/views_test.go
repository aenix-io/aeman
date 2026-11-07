package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// The BOARD a caller is standing on is a path segment, and these are the three
// things it answers for: which board a gesture belongs to, whether the card is
// on it, and what a create means there (docs/design/view-scoped-api.md).

// A gesture asked of a board that does not draw it is a route that is not
// there — there is no "finished in the sprint before" on the Backlog, the
// Triage board's drop into a week is no route at all any more (K9), and the
// retired Project board is no board (H15).
func TestAGestureIsAnsweredOnlyByTheBoardThatDrawsIt(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Title: "the work", Team: "alpha", Assignees: []string{"bob"},
			StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	for _, target := range []string{
		"/api/v1/views/backlog/cards/c1/actions/finished-earlier",
		"/api/v1/views/team/cards/c1/actions/place",
		"/api/v1/views/team/cards/c1/actions/untriage",
		"/api/v1/views/triage/cards/c1/actions/remove",
		"/api/v1/views/project/cards/c1/actions/remove",
	} {
		if rec := do(t, srv, http.MethodPost, target, `{}`); rec.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d, want 404", target, rec.Code)
		}
	}
	// And on the board that does draw it, the same press lands.
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/team/cards/c1/actions/finished-earlier",
		`{}`); rec.Code == http.StatusNotFound {
		t.Fatalf("the Team board's own finished-earlier answered 404: %s", rec.Body.String())
	}
}

// OWN is the Roadmap's "whose is this", offered by every board that draws a
// task (H10). Over HTTP it goes through the
// gesture door like every press: the board is in the address, the card must
// be drawn there, and the answer's effect is the service's (own_test.go).
func TestOwnIsAnsweredByTheBoardsThatDrawATask(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "loose", Title: "Arrived"},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	if rec := do(t, srv, http.MethodPost, "/api/v1/views/project/cards/loose/actions/own",
		`{"team":"alpha"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("own on the retired Project board answered %d, want 404 (no such board)", rec.Code)
	}
	// A person without a team is the service's refusal, through the door.
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/roadmap/cards/loose/actions/own",
		`{"person":"ann"}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "ownerNeedsTeam") {
		t.Fatalf("a person with no team answered %d: %s", rec.Code, rec.Body.String())
	}
	// From the Roadmap, which lists the loose card below its tree (H9), a
	// person takes it home: team, person, today, the unplanned band.
	rec := do(t, srv, http.MethodPost, "/api/v1/views/roadmap/cards/loose/actions/own",
		`{"team":"alpha","person":"ann"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("own answered %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Spec struct {
			Team      string   `json:"team"`
			Assignees []string `json:"assignees"`
			Zone      string   `json:"zone"`
			Dates     struct {
				Start string `json:"start"`
			} `json:"dates"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Team != "alpha" || len(got.Spec.Assignees) != 1 || got.Spec.Assignees[0] != "ann" ||
		got.Spec.Dates.Start != today || got.Spec.Zone != "unplanned" {
		t.Fatalf("the card after own: %+v", got.Spec)
	}
}

// A person cannot press × on a card they cannot see. An agent standing on the
// Me board could: it named a board and then acted on somebody else's card, and
// nothing asked whether that board draws it.
func TestAGestureIsRefusedForACardTheBoardDoesNotDraw(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		// Unassign always has the backlog to leave them in (K3), so the test
		// is about the BOARD and nothing else.
		{ItemID: "mine", Title: "mine", Team: "alpha", Assignees: []string{"bob"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "theirs", Title: "theirs", Team: "alpha", Assignees: []string{"carol"},
			StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	if rec := do(t, srv, http.MethodPost, "/api/v1/views/me/cards/theirs/actions/remove",
		`{"intent":"unassign"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("somebody else's card removed from MY board answered %d, want 404", rec.Code)
	}
	// The lead's grid draws it, and so does the escape hatch.
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/team/cards/theirs/actions/remove",
		`{"intent":"unassign"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("the team board's × answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/all/cards/mine/actions/remove",
		`{"intent":"unassign"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("view=all answered %d: %s", rec.Code, rec.Body.String())
	}
}

// createdCard is the part of the answer the add-boxes differ by.
type createdCard struct {
	Metadata struct {
		UID string `json:"uid"`
	} `json:"metadata"`
	Spec struct {
		Assignees []string `json:"assignees"`
		Zone      string   `json:"zone"`
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
	shelved := created(t, do(t, newSrv(), http.MethodPost, "/api/v1/views/backlog/cards",
		`{"title":"someday","team":"alpha","zone":"planned"}`))
	if !shelved.Spec.Parked || shelved.Spec.Dates.Start != "" || shelved.Spec.Dates.Sprint != "" {
		t.Fatalf("a backlog create = %+v, want the backlog alone", shelved.Spec)
	}
	// The Triage board is retired (K9): a create into it is a board nobody
	// has.
	if rec := do(t, newSrv(), http.MethodPost, "/api/v1/views/triage/cards",
		`{"title":"later","team":"alpha"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("a create into the retired Triage board answered %d, want 404", rec.Code)
	}
	// And a board refuses what it does not own, 422, naming the field.
	rec := do(t, newSrv(), http.MethodPost, "/api/v1/views/me/cards",
		`{"title":"x","team":"alpha","zone":"planned","type":"epic"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a type named on the Me board answered %d", rec.Code)
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
	// Every row, not the first one that matches: a client reads this in place
	// of being told the surface, so the whole map is the contract.
	got := map[string][]string{}
	for _, v := range out.Items {
		got[v.Name] = v.Gestures
	}
	want := map[string][]string{
		"me":      {"remove", "finished-earlier", "own"},
		"team":    {"remove", "finished-earlier", "own"},
		"backlog": {"remove", "own", "take"},
		"roadmap": {"remove", "own"},
		"all":     {"remove", "finished-earlier", "own", "take"},
	}
	if len(got) != len(want) {
		t.Fatalf("the catalog lists %v", got)
	}
	for name, gestures := range want {
		if strings.Join(got[name], ",") != strings.Join(gestures, ",") {
			t.Errorf("the %s board draws %v, want %v", name, got[name], gestures)
		}
	}
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
		"/api/v1/views/backlog/cards/c2/actions/own":          "c2",
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

// THE DOOR HANDS THE SERVICE THE BOARD. The narrow × of the Me board is a
// service rule with its own tests, and the plumbing that carries the board to
// it had none: hardcoding board.ViewAll in either handler left every suite
// green. This is the wire, tested as a wire — the same card, refused from the
// board whose × is narrow and taken from the one whose × is wide.
func TestTheDoorHandsTheServiceTheBoardItWasPressedOn(t *testing.T) {
	today := board.TodayIso()
	seed := func() *boardservicetest.Backend {
		return boardservicetest.New([]board.Card{
			// Mine, and the plan has taken it up: the Me board draws no × on
			// it (it is no longer only mine), the team's grid does.
			{ItemID: "planned", Title: "in the plan", Team: "alpha", Author: "bob",
				Assignees: []string{"bob"}, Zone: board.ZoneGray,
				StartDate: today, Day: today, SprintStart: today},
		}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	}
	srv := func(fake *boardservicetest.Backend) *Server {
		s := apiServer(t, Options{}, fake)
		s.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }
		return s
	}

	if rec := do(t, srv(seed()), http.MethodPost, "/api/v1/views/me/cards/planned/actions/remove",
		`{"intent":"unassign"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("the Me board's × on planned work answered %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv(seed()), http.MethodPost, "/api/v1/views/team/cards/planned/actions/remove?team=alpha",
		`{"intent":"unassign"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("the team grid's × on the same card answered %d: %s", rec.Code, rec.Body.String())
	}
}

// The Backlog is a STANDING board of its own (K5, K9): its gestures — the ×
// and own, the owner picker that takes a task into work — are judged against
// the backlog listing, so a parked task is drawn there and a gesture on it
// lands; one on a task in work is refused, since the Backlog does not draw it.
func TestTheBacklogIsABoardItsGesturesStandOn(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "shelved", Title: "someday", Team: "alpha", Parked: true},
		{ItemID: "taken", Title: "in work", Team: "alpha", StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	if rec := do(t, srv, http.MethodPost, "/api/v1/views/backlog/cards/shelved/actions/own?team=alpha",
		`{"team":"alpha","person":"bob"}`); rec.Code != http.StatusOK {
		t.Fatalf("own from the Backlog answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := fake.Card("shelved"); got == nil || got.Parked || got.StartDate != today {
		t.Fatalf("own with a person takes the task into work (K4): %+v", got)
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/backlog/cards/taken/actions/remove?team=alpha",
		`{"intent":"off-board"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("the Backlog's × on a task in work answered %d, want 404 — it does not draw it", rec.Code)
	}
}

// A TASK HAS NO WEEK (K7): `week` is not a field of the create or the patch
// any more, so the API takes nothing from it — the card a create answers with
// carries no week, and a patch naming one changes nothing about the card.
func TestTheAPITakesNoWeekForATask(t *testing.T) {
	today := board.TodayIso()
	week := board.MondayOf(today)
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Title: "in work", Team: "alpha", StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodPost, "/api/v1/views/team/cards", `{"title":"x","team":"alpha","week":"`+week+`"}`)
	if rec.Code == http.StatusCreated && strings.Contains(rec.Body.String(), `"week"`) {
		t.Fatalf("a create took a week: %s", rec.Body.String())
	}
	rec = do(t, srv, http.MethodPatch, "/api/v1/cards/c1", `{"week":"`+week+`"}`)
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), `"week"`) {
		t.Fatalf("a patch took a week: %s", rec.Body.String())
	}
	if got := fake.Card("c1"); got == nil || got.StartDate != today || got.Day != today {
		t.Fatalf("a patch naming a week changed the card: %+v", got)
	}
}

// TAKE over HTTP (K11): the Backlog's own gesture, answered through the
// gesture door — the caller is the one the card is given to, on the card's
// own team, and it leaves the backlog; a task already in work is 422
// `alreadyInWork`, and another board has no take at all.
func TestTakeIsTheBacklogsGestureOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "p", Title: "parked", Team: "alpha", Parked: true},
		{ItemID: "w", Title: "in work", Team: "alpha", Assignees: []string{"ann"}, StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodPost, "/api/v1/views/backlog/cards/p/actions/take?team=alpha", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("take answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := fake.Card("p"); got == nil || got.Parked || len(got.Assignees) != 1 || got.Assignees[0] != "bob" || got.Team != "alpha" {
		t.Fatalf("taken by the caller on its own team, out of the backlog: %+v", got)
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/all/cards/w/actions/take", ""); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "alreadyInWork") {
		t.Fatalf("take on work in hand = %d %s, want 422 alreadyInWork", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/team/cards/w/actions/take?team=alpha", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("take on the Team board answered %d, want 404 — it is the backlog's", rec.Code)
	}
}

// The insert "+" over HTTP is ONE create naming its place: the card is born
// after the card above the gap (or before the one below it), in that
// request — on the Backlog and in a Team or Me cell. Two places, or a place
// on a board that is not a ranked list, are 422 by name.
func TestABacklogCreateTakesItsPlaceOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "a", Title: "first", Team: "alpha", Parked: true},
		{ItemID: "b", Title: "second", Team: "alpha", Parked: true},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodPost, "/api/v1/views/backlog/cards?team=alpha", `{"title":"between","team":"alpha","after":"a"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a placed create answered %d: %s", rec.Code, rec.Body.String())
	}
	var made createdCard
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	if !fake.Saw("MoveCard " + made.Metadata.UID + " after=a") {
		t.Fatal("the card was not placed after «first» in the create")
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/backlog/cards?team=alpha",
		`{"title":"x","team":"alpha","after":"a","before":"b"}`); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "twoPlaces") {
		t.Fatalf("two places = %d %s, want 422 twoPlaces", rec.Code, rec.Body.String())
	}
	// A Team board cell takes a place too, keeping the cell's person and
	// band; the Roadmap places by parent and refuses one by name.
	rec = do(t, srv, http.MethodPost, "/api/v1/views/team/cards?team=alpha",
		`{"title":"in a cell","team":"alpha","zone":"planned","assignees":["ann"],"before":"b"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a placed Team create answered %d: %s", rec.Code, rec.Body.String())
	}
	var cell createdCard
	if err := json.Unmarshal(rec.Body.Bytes(), &cell); err != nil {
		t.Fatal(err)
	}
	if cell.Spec.Zone != "planned" || len(cell.Spec.Assignees) != 1 || cell.Spec.Assignees[0] != "ann" ||
		!fake.Saw("MoveCard "+cell.Metadata.UID+" after=a") {
		t.Fatalf("the cell's card = %+v, want ann's, planned, right before «second»", cell.Spec)
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/views/roadmap/cards",
		`{"title":"x","type":"epic","after":"a"}`); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "notOnThisBoard") {
		t.Fatalf("a place on the Roadmap = %d %s, want 422 notOnThisBoard", rec.Code, rec.Body.String())
	}
}

// The Roadmap's status menu over HTTP (K15, K16): TO DO and UNASSIGNED are
// card actions answering how many tasks they changed — on a plan item, every
// task under it they reach — and each refuses by name a task it means
// nothing for.
func TestToDoAndUnassignedOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "e", Title: "epic", Type: board.TypeEpic, Team: "alpha"},
		{ItemID: "p1", Title: "parked one", Parent: "e", Parked: true},
		{ItemID: "p2", Title: "parked two", Parent: "e", Parked: true},
		{ItemID: "w", Title: "in work", Parent: "e", Team: "alpha", Assignees: []string{"ann"}, StartDate: today, Day: today, SprintStart: today},
		{ItemID: "done", Title: "finished", Parent: "e", Team: "alpha", SprintStart: today, Day: today, Progress: 100},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodPost, "/api/v1/cards/e/actions/todo", `{"todo":true}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"changed":2`) {
		t.Fatalf("TO DO on the epic = %d %s, want 200 with 2 changed", rec.Code, rec.Body.String())
	}
	if !fake.Card("p1").Todo || !fake.Card("p2").Todo || fake.Card("w").Todo {
		t.Fatal("the parked tasks are TO DO, the work in hand is not")
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/cards/w/actions/todo", `{"todo":true}`); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "notParkedWork") {
		t.Fatalf("TO DO on work in hand = %d %s, want 422 notParkedWork", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, http.MethodPost, "/api/v1/cards/e/actions/unassign-work", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"changed":1`) {
		t.Fatalf("UNASSIGNED on the epic = %d %s, want 200 with 1 changed", rec.Code, rec.Body.String())
	}
	if got := fake.Card("w"); !got.Parked || len(got.Assignees) != 0 {
		t.Fatalf("the work in hand is back in its backlog: %+v", got)
	}
	if got := fake.Card("done"); got.Parked {
		t.Fatalf("finished work is untouched: %+v", got)
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/cards/p1/actions/unassign-work", ""); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "notInWork") {
		t.Fatalf("UNASSIGNED on parked work = %d %s, want 422 notInWork", rec.Code, rec.Body.String())
	}
}

// With a Roadmap filter on the reader sees — and the confirm counts — fewer
// tasks than the board holds. The request names them, and the server acts on
// exactly those (M1); a uid the gesture cannot reach from the card is 422
// notATarget and nothing changes.
func TestTheStatusMenuActsOnTheTasksTheReaderCountedOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "e", Title: "epic", Type: board.TypeEpic, Team: "alpha"},
		{ItemID: "shown", Title: "shown", Parent: "e", Team: "alpha", Assignees: []string{"ann"}, SprintStart: today, Day: today},
		{ItemID: "hidden", Title: "filtered out", Parent: "e", Team: "beta", Assignees: []string{"bob"}, SprintStart: today, Day: today},
		{ItemID: "p", Title: "parked", Parent: "e", Parked: true},
		{ItemID: "q", Title: "parked too", Parent: "e", Parked: true},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}, "beta": {Current: today, ItemID: "s2"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodPost, "/api/v1/cards/e/actions/unassign-work", `{"uids":["shown"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"changed":1`) {
		t.Fatalf("UNASSIGNED on the counted task = %d %s", rec.Code, rec.Body.String())
	}
	if !fake.Card("shown").Parked || fake.Card("hidden").Parked {
		t.Fatalf("only the counted task leaves work: shown %+v, hidden %+v", fake.Card("shown"), fake.Card("hidden"))
	}
	rec = do(t, srv, http.MethodPost, "/api/v1/cards/e/actions/todo", `{"todo":true,"uids":["q"]}`)
	if rec.Code != http.StatusOK || !fake.Card("q").Todo || fake.Card("p").Todo {
		t.Fatalf("TO DO on the counted task = %d %s; p %v q %v", rec.Code, rec.Body.String(), fake.Card("p").Todo, fake.Card("q").Todo)
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/cards/e/actions/unassign-work", `{"uids":["hidden","p"]}`); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "notATarget") || fake.Card("hidden").Parked {
		t.Fatalf("a uid the gesture cannot reach = %d %s, want 422 notATarget and nothing changed", rec.Code, rec.Body.String())
	}
}
