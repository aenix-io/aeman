package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aenix-org/aeman/pkg/apiserver"
	"github.com/aenix-org/aeman/pkg/board"
	"github.com/aenix-org/aeman/pkg/boardservice"
	"github.com/aenix-org/aeman/pkg/boardservice/boardservicetest"
)

// apiServer wires an aeman Server whose /api/v1 board service is backed by the
// in-memory fake, so the handlers exercise the real boardservice logic without
// touching GitHub or the token sources.
func apiServer(t *testing.T, opts Options, fake *boardservicetest.Backend) *Server {
	t.Helper()
	opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.newService = func(*http.Request) (*boardservice.Service, error) {
		return boardservice.New(fake), nil
	}
	return srv
}

func do(t *testing.T, srv *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, r)
	return rec
}

func decodeCard(t *testing.T, rec *httptest.ResponseRecorder) apiserver.Card {
	t.Helper()
	var c apiserver.Card
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("decode card: %v (%s)", err, rec.Body.String())
	}
	return c
}

func decodeList(t *testing.T, rec *httptest.ResponseRecorder) apiserver.CardList {
	t.Helper()
	var l apiserver.CardList
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatalf("decode list: %v (%s)", err, rec.Body.String())
	}
	return l
}

func TestAPIBoard(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "st1"}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/board?owner=acme&project=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var info apiserver.BoardInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Kind != "Board" || len(info.Metadata.Teams) != 1 || info.Metadata.Teams[0] != "alpha" {
		t.Fatalf("board = %+v", info)
	}
}

func TestAPIListCardsTeamView(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: today, SprintStart: today},
		{ItemID: "c2", Team: "beta", StartDate: today, SprintStart: today},
	}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/cards?owner=acme&project=1&view=team&team=alpha", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	list := decodeList(t, rec)
	if list.Kind != "CardList" || len(list.Items) != 1 || list.Items[0].Metadata.UID != "c1" {
		t.Fatalf("list = %+v", list)
	}
}

func TestAPIListCardsMeView(t *testing.T) {
	today := board.TodayIso()
	// Me shows a card whose sprintStart equals the sprint active on the viewed day
	// and whose startDate has arrived, so the no-team group needs a sprint anchor.
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Assignees: []string{"bob"}, StartDate: today, SprintStart: today},
	}, map[string]board.SprintState{"": {Current: today}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/cards?owner=acme&project=1&view=me&user=bob", "")
	list := decodeList(t, rec)
	if len(list.Items) != 1 || list.Items[0].Metadata.UID != "c1" {
		t.Fatalf("me view = %+v", list.Items)
	}
}

func TestAPIListCardsWeeklyCarriesProgress(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "p1", Team: "alpha", Plan: board.PlanWed, Week: "2026-06-15", Progress: 40},
		{ItemID: "p2", Team: "alpha", Plan: board.PlanFri, Week: "2026-06-15", Stage: board.StageRecurrent, Progress: 100},
	}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/cards?owner=acme&project=1&view=weekly&team=alpha&week=2026-06-15", "")
	list := decodeList(t, rec)
	if len(list.Items) != 2 || list.Weekly == nil || list.Weekly.Progress != 40 {
		t.Fatalf("weekly = %+v %+v (recurrent excluded from the bar)", list.Items, list.Weekly)
	}
}

func TestAPICreateCard(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=1",
		`{"title":"New task","team":"alpha","zone":"urgent","assignees":["kvaps"],"dates":{"start":"2026-06-21"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	c := decodeCard(t, rec)
	if c.Spec.Title != "New task" || c.Spec.Zone != "urgent" {
		t.Fatalf("card = %+v", c.Spec)
	}
	// One-day range: end defaults to start; the card joins the current sprint.
	if c.Spec.Dates.Start != "2026-06-21" || c.Spec.Dates.End != "2026-06-21" || c.Spec.Dates.Sprint != "2026-06-20" {
		t.Fatalf("dates = %+v", c.Spec.Dates)
	}
}

func TestAPICreateRequiresTitle(t *testing.T) {
	srv := apiServer(t, Options{}, boardservicetest.New(nil, nil))
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=1", `{"team":"alpha"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAPIPatchProgressDoneIsDerived(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Progress: 40}}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/c1?owner=acme&project=1", `{"progress":100}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	c := decodeCard(t, rec)
	if c.Spec.Progress != 100 || c.Spec.Stage != "" || !c.Status.Complete {
		t.Fatalf("card = %+v %+v (done is derived: 100%% + no stage)", c.Spec, c.Status)
	}
}

func TestAPIPatchStageDoneFillsProgress(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Progress: 40}}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/c1?owner=acme&project=1", `{"stage":"done"}`)
	c := decodeCard(t, rec)
	if c.Spec.Stage != "" || c.Spec.Progress != 100 {
		t.Fatalf("card = %+v (picking done clears the stage and fills 100)", c.Spec)
	}
}

func TestAPIPatchZoneSemanticNames(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1"}}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/c1?owner=acme&project=1", `{"zone":"unplanned"}`)
	if c := decodeCard(t, rec); c.Spec.Zone != "unplanned" {
		t.Fatalf("card = %+v", c.Spec)
	}
	rec = do(t, srv, http.MethodPatch, "/api/v1/cards/c1?owner=acme&project=1", `{"zone":"red"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("colour names are not part of the API: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAPIPatchDatesRunsCalendarRule(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: "2026-06-20", SprintStart: "2026-06-20", Day: "2026-06-20"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-06-20", Previous: "2026-06-13", ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/c1?owner=acme&project=1",
		`{"dates":{"start":"2026-06-14","end":"2026-06-16"}}`)
	c := decodeCard(t, rec)
	// 06-14 is inside the previous sprint [06-13, 06-20): the card joins it.
	if c.Spec.Dates.Start != "2026-06-14" || c.Spec.Dates.Sprint != "2026-06-13" || c.Spec.Dates.End != "2026-06-16" {
		t.Fatalf("dates = %+v", c.Spec.Dates)
	}
}

func TestAPIDeferAction(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: "2000-01-01", SprintStart: "2000-01-01", CreatedAt: "2000-01-01T10:00:00Z"},
	}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/c1/actions/defer?owner=acme&project=1", `{"days":1}`)
	c := decodeCard(t, rec)
	want := board.AddDays(board.TodayIso(), 1)
	if c.Spec.Dates.Start != want || c.Spec.Dates.Sprint != "2000-01-01" {
		t.Fatalf("dates = %+v, want start %s with history kept", c.Spec.Dates, want)
	}
}

func TestAPIRemoveActionDemotes(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: "2026-06-20", SprintStart: "2026-06-20"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-06-20", Previous: "2026-06-13", ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/c1/actions/remove?owner=acme&project=1", `{"from":"grid"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/cards/c1?owner=acme&project=1", "")
	if c := decodeCard(t, rec); c.Spec.Dates.Sprint != "2026-06-13" {
		t.Fatalf("first remove demotes: %+v", c.Spec.Dates)
	}
}

func TestAPIDeleteCascadesToReview(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha"},
		{ItemID: "r1", Team: "alpha", ReviewOf: "c1"},
	}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodDelete, "/api/v1/cards/c1?owner=acme&project=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/cards/r1?owner=acme&project=1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the linked review card must cascade: %d", rec.Code)
	}
}

func TestAPISendToReviewCreatesLinkedCard(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", Title: "Work", Progress: 50},
	}, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/c1/actions/send-to-review?owner=acme&project=1",
		`{"reviewer":"lllamnyp","day":"2026-06-21"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	review := decodeCard(t, rec)
	if review.Spec.ReviewOf != "c1" || review.Spec.Assignees[0] != "lllamnyp" {
		t.Fatalf("review card = %+v", review.Spec)
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/cards/c1?owner=acme&project=1", "")
	if c := decodeCard(t, rec); c.Spec.Stage != "review" || c.Status.ReviewedBy != "lllamnyp" {
		t.Fatalf("original = %+v %+v", c.Spec, c.Status)
	}
}

func TestAPICarryOverDryRun(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", SprintStart: "2026-01-01"},
	}, map[string]board.SprintState{"alpha": {Current: "2026-01-01", ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/sprints/actions/carry-over?owner=acme&project=1",
		`{"team":"alpha","dryRun":true}`)
	var rep boardservice.CarryReport
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 1 {
		t.Fatalf("report = %+v", rep)
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/cards/c1?owner=acme&project=1", "")
	if c := decodeCard(t, rec); c.Spec.Dates.Sprint != "2026-01-01" {
		t.Fatalf("dry run must not move the card: %+v", c.Spec.Dates)
	}
}

func TestAPISprints(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", Previous: "2026-06-13", ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/sprints?owner=acme&project=1", "")
	var list struct {
		Kind  string             `json:"kind"`
		Items []apiserver.Sprint `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Kind != "SprintList" || len(list.Items) != 1 || list.Items[0].Spec.Current != "2026-06-20" {
		t.Fatalf("sprints = %+v", list)
	}
	rec = do(t, srv, http.MethodPatch, "/api/v1/sprints?owner=acme&project=1",
		`{"team":"alpha","current":"2026-06-27","previous":"2026-06-20"}`)
	var sp apiserver.Sprint
	if err := json.Unmarshal(rec.Body.Bytes(), &sp); err != nil {
		t.Fatal(err)
	}
	if sp.Spec.Current != "2026-06-27" {
		t.Fatalf("sprint = %+v", sp)
	}
}

func TestAPIOrdering(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1"}, {ItemID: "c2"}}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/ordering?owner=acme&project=1", "")
	var o apiserver.Ordering
	if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if o.Kind != "Ordering" || len(o.Spec.UIDs) != 2 {
		t.Fatalf("ordering = %+v", o)
	}
}

func TestAPINotes(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", ContentID: "D1", IsDraft: true}}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/c1/notes?owner=acme&project=1", `{"text":"hello"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Kind  string           `json:"kind"`
		Items []apiserver.Note `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Kind != "NoteList" || len(list.Items) != 1 || list.Items[0].Spec.Text != "hello" {
		t.Fatalf("notes = %+v", list)
	}
}

func TestAPIUnknownCardReturns404(t *testing.T) {
	srv := apiServer(t, Options{}, boardservicetest.New(nil, nil))
	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/nope?owner=acme&project=1", `{"progress":50}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPIMissingBoardRef(t *testing.T) {
	srv := apiServer(t, Options{}, boardservicetest.New(nil, nil))
	rec := do(t, srv, http.MethodGet, "/api/v1/cards", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAPILockBoardIgnoresQuery(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: "x", ItemID: "s1"}})
	srv := apiServer(t, Options{DefaultOwner: "acme", DefaultProject: 7, LockBoard: true}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/board?owner=evil&project=99", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("locked board should resolve from defaults: status = %d", rec.Code)
	}
}

func TestAPIIndex(t *testing.T) {
	srv := apiServer(t, Options{Version: "test-1.2.3"}, boardservicetest.New(nil, nil))
	// The catalog is public metadata: it must not resolve a token or board.
	srv.newService = func(*http.Request) (*boardservice.Service, error) {
		t.Fatal("index must not build a board service")
		return nil, nil
	}
	rec := do(t, srv, http.MethodGet, "/api/v1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var idx apiIndex
	if err := json.Unmarshal(rec.Body.Bytes(), &idx); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if idx.Name != "aeman" || idx.Version != "test-1.2.3" || idx.MCP != "/mcp" || len(idx.Endpoints) == 0 {
		t.Fatalf("index = %+v", idx)
	}
	for _, ep := range idx.Endpoints {
		if ep.Path == "/api/v1/snapshot" {
			t.Fatal("the old snapshot endpoint must be gone from the catalog")
		}
	}
}

func TestAPIInvalidJSON(t *testing.T) {
	srv := apiServer(t, Options{}, boardservicetest.New(nil, nil))
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=1", `{not json}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAPICardLinks(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{
		ItemID:      "c1",
		Description: "https://example.com/spec and https://github.com/acme/repo/issues/3",
	}}, nil)
	fake.SetRefs(map[string]board.Link{
		"https://github.com/acme/repo/issues/3": {
			URL: "https://github.com/acme/repo/issues/3", Kind: "issue",
			Owner: "acme", Repo: "repo", Number: 3, Title: "Broken build", State: "closed"},
	})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/cards/c1/links?owner=acme&project=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Kind  string       `json:"kind"`
		Items []board.Link `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Kind != "LinkList" || len(list.Items) != 2 {
		t.Fatalf("links = %+v", list)
	}
	if list.Items[0].Title != "Broken build" || list.Items[1].Kind != "link" {
		t.Fatalf("order/resolution wrong: %+v", list.Items)
	}
}

func TestAPICreateCardFromGitHubURL(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	fake.SetRefs(map[string]board.Link{
		"https://github.com/acme/repo/pull/7": {
			URL: "https://github.com/acme/repo/pull/7", Kind: "pull",
			Owner: "acme", Repo: "repo", Number: 7, Title: "feat: warp drive", State: "open"},
	})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=1",
		`{"title":"https://github.com/acme/repo/pull/7","team":"alpha"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	c := decodeCard(t, rec)
	if c.Spec.Title != "feat: warp drive" || c.Spec.Description != "https://github.com/acme/repo/pull/7" {
		t.Fatalf("card = %+v", c.Spec)
	}
}
