package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aenix-org/aeman/internal/board"
	"github.com/aenix-org/aeman/internal/boardservice"
	"github.com/aenix-org/aeman/internal/boardservice/boardservicetest"
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

func TestAPIBoardMeta(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20"}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/board?owner=acme&project=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var meta boardMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.Owner != "acme" || meta.SprintStates["alpha"].Current != "2026-06-20" {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestAPITeamView(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: today, SprintStart: today},
		{ItemID: "c2", Team: "beta", StartDate: today, SprintStart: today},
	}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/team?owner=acme&project=1&team=alpha", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out cardsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Cards) != 1 || out.Cards[0].ItemID != "c1" {
		t.Fatalf("cards = %+v", out.Cards)
	}
}

func TestAPIMeView(t *testing.T) {
	today := board.TodayIso()
	// Me shows a card whose sprintStart equals the sprint active on the viewed day
	// and whose startDate has arrived, so the no-team group needs a sprint anchor.
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Assignees: []string{"bob"}, StartDate: today, SprintStart: today},
	}, map[string]board.SprintState{"": {Current: today}})
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/me?owner=acme&project=1&user=bob", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out cardsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Cards) != 1 || out.Cards[0].ItemID != "c1" {
		t.Fatalf("cards = %+v", out.Cards)
	}
}

func TestAPIWeeklyView(t *testing.T) {
	week := "2026-06-22"
	fake := boardservicetest.New([]board.Card{
		{ItemID: "a1", Plan: board.PlanWed, Week: week, Team: "alpha"},
		{ItemID: "a2", Plan: board.PlanFri, Week: week, Team: "alpha"},
	}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/weekly?owner=acme&project=1&team=alpha&week="+week, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var bands board.WeeklyBands
	_ = json.Unmarshal(rec.Body.Bytes(), &bands)
	if len(bands.Wed) != 1 || len(bands.Fri) != 1 {
		t.Fatalf("bands = %+v", bands)
	}
}

func TestAPICreateCard(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=1",
		`{"team":"alpha","zone":"red","title":"New","assignee":"bob"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var card board.Card
	_ = json.Unmarshal(rec.Body.Bytes(), &card)
	if card.Title != "New" || card.Team != "alpha" {
		t.Fatalf("card = %+v", card)
	}
	if len(fake.Creates()) != 1 || fake.Creates()[0].Zone != board.ZoneRed {
		t.Fatalf("creates = %+v", fake.Creates())
	}
}

func TestAPICreateRequiresTitle(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=1", `{"team":"alpha"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAPISetProgressDoneLink(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Progress: 50}}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/c1/progress?owner=acme&project=1", `{"progress":100}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var card board.Card
	_ = json.Unmarshal(rec.Body.Bytes(), &card)
	if card.Progress != 100 || card.Stage != board.StageDone {
		t.Fatalf("100%% should auto-set done: %+v", card)
	}
}

func TestAPISetStage(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Progress: 100}}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/c1/stage?owner=acme&project=1", `{"stage":"review"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var card board.Card
	_ = json.Unmarshal(rec.Body.Bytes(), &card)
	if card.Stage != board.StageReview || card.Progress != 90 {
		t.Fatalf("review should knock a full card to 90: %+v", card)
	}
}

func TestAPIDeleteCascadesToReview(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "orig", Title: "x"},
		{ItemID: "rev", ReviewOf: "orig"},
	}, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodDelete, "/api/v1/cards/orig?owner=acme&project=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.Card("orig") != nil || fake.Card("rev") != nil {
		t.Fatalf("both cards should be gone; orig=%v rev=%v", fake.Card("orig"), fake.Card("rev"))
	}
	if fake.Count("DeleteCard") != 2 {
		t.Fatalf("expected two deletes, got %d", fake.Count("DeleteCard"))
	}
}

func TestAPIUnknownCardReturns404(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/nope/progress?owner=acme&project=1", `{"progress":50}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAPIMissingBoardRef(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/team", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAPILockBoardIgnoresQuery(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: "x"}})
	srv := apiServer(t, Options{DefaultOwner: "acme", DefaultProject: 7, LockBoard: true}, fake)
	rec := do(t, srv, http.MethodGet, "/api/v1/board?owner=evil&project=99", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("locked board should resolve from defaults: status = %d", rec.Code)
	}
}

func TestAPIInvalidJSON(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	srv := apiServer(t, Options{}, fake)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=1", `{not json}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
