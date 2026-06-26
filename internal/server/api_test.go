package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGraphQL spins up a stub GitHub GraphQL endpoint and returns an aeman
// Server wired to it, plus a slice capturing the requests it received. The API
// token is stubbed via the apiTokens seam so tests never touch gh or OAuth.
func fakeGraphQL(t *testing.T, opts Options, respond func(query string, vars map[string]any) string) (*Server, *[]map[string]any) {
	t.Helper()
	var reqs []map[string]any
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		reqs = append(reqs, map[string]any{"query": body.Query, "vars": body.Variables})
		data := respond(body.Query, body.Variables)
		if data == "" {
			data = "{}"
		}
		_, _ = w.Write([]byte(`{"data":` + data + `}`))
	}))
	t.Cleanup(gh.Close)

	opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.graphqlEndpoint = gh.URL
	srv.apiTokens = func(*http.Request) (string, string, error) { return "test-token", "tester", nil }
	return srv, &reqs
}

const apiBoardJSON = `{"organization":{"projectV2":{
  "id":"PVT_1","number":7,"title":"Board","url":"https://example/7",
  "fields":{"nodes":[
    {"__typename":"ProjectV2SingleSelectField","id":"F_ZONE","name":"Zone","dataType":"SINGLE_SELECT","options":[
      {"id":"o_gray","name":"Planned","color":"GRAY"},{"id":"o_red","name":"Critical","color":"RED"}]},
    {"__typename":"ProjectV2Field","id":"F_PROG","name":"Progress","dataType":"NUMBER"}
  ]},
  "items":{"nodes":[
    {"id":"I_DRAFT","type":"DRAFT_ISSUE","createdAt":"2026-06-20T10:00:00Z",
     "content":{"__typename":"DraftIssue","id":"DI_1","title":"Draft","body":"","assignees":{"nodes":[]}},
     "fieldValues":{"nodes":[
       {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"o_red","name":"Critical","field":{"id":"F_ZONE","name":"Zone"}}
     ]}}
  ]}
}}}`

func apiRespond(query string, _ map[string]any) string {
	switch {
	case strings.Contains(query, "organization(login:") && strings.Contains(query, "projectV2(number:"):
		return apiBoardJSON
	case strings.Contains(query, "addProjectV2DraftIssue"):
		return `{"addProjectV2DraftIssue":{"projectItem":{"id":"I_NEW","content":{"id":"DI_NEW"}}}}`
	default:
		return "{}"
	}
}

func do(t *testing.T, srv *Server, method, target string, body string) *httptest.ResponseRecorder {
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

func TestAPIGetBoard(t *testing.T) {
	srv, _ := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodGet, "/api/v1/board?owner=acme&project=7", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var meta boardMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.ID != "PVT_1" || len(meta.Fields) != 2 {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestAPIListCards(t *testing.T) {
	srv, _ := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodGet, "/api/v1/cards?owner=acme&project=7", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Cards []map[string]any `json:"cards"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Cards) != 1 || out.Cards[0]["zone"] != "red" {
		t.Fatalf("cards = %+v", out.Cards)
	}
}

func TestAPIMissingBoardRef(t *testing.T) {
	srv, _ := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodGet, "/api/v1/cards", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAPILockBoardIgnoresQuery(t *testing.T) {
	srv, reqs := fakeGraphQL(t, Options{DefaultOwner: "acme", DefaultProject: 7, LockBoard: true}, apiRespond)
	rec := do(t, srv, http.MethodGet, "/api/v1/board?owner=evil&project=99", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, r := range *reqs {
		vars, _ := r["vars"].(map[string]any)
		if owner, ok := vars["owner"]; ok && owner != "acme" {
			t.Fatalf("locked board used owner %v", owner)
		}
	}
}

func TestAPICreateCard(t *testing.T) {
	srv, reqs := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=7", `{"title":"New","zone":"red"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	sawDraft := false
	for _, r := range *reqs {
		if q, _ := r["query"].(string); strings.Contains(q, "addProjectV2DraftIssue") {
			sawDraft = true
		}
	}
	if !sawDraft {
		t.Fatal("expected a draft-issue creation request")
	}
}

func TestAPIUpdateCard(t *testing.T) {
	srv, reqs := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/I_DRAFT?owner=acme&project=7", `{"progress":80}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	sawNumber := false
	for _, r := range *reqs {
		q, _ := r["query"].(string)
		vars, _ := r["vars"].(map[string]any)
		if strings.Contains(q, "value: { number: $value }") && vars["value"] == 80.0 {
			sawNumber = true
		}
	}
	if !sawNumber {
		t.Fatal("expected a number mutation with value 80")
	}
}

func TestAPIUpdateUnknownCardReturns404(t *testing.T) {
	srv, _ := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/NOPE?owner=acme&project=7", `{"progress":80}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAPIMoveCard(t *testing.T) {
	srv, reqs := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/I_DRAFT/move?owner=acme&project=7", `{"afterId":"I_OTHER"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	sawMove := false
	for _, r := range *reqs {
		q, _ := r["query"].(string)
		vars, _ := r["vars"].(map[string]any)
		if strings.Contains(q, "updateProjectV2ItemPosition") && vars["item"] == "I_DRAFT" && vars["after"] == "I_OTHER" {
			sawMove = true
		}
	}
	if !sawMove {
		t.Fatal("expected an updateProjectV2ItemPosition mutation with item=I_DRAFT after=I_OTHER")
	}
}

func TestAPIMoveCardToTop(t *testing.T) {
	srv, reqs := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/I_DRAFT/move?owner=acme&project=7", `{"afterId":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	sawMove := false
	for _, r := range *reqs {
		q, _ := r["query"].(string)
		vars, _ := r["vars"].(map[string]any)
		if strings.Contains(q, "updateProjectV2ItemPosition") && vars["item"] == "I_DRAFT" && vars["after"] == nil {
			sawMove = true
		}
	}
	if !sawMove {
		t.Fatal("expected a move mutation with a nil after (move to top)")
	}
}

func TestAPIAddNoteRequiresText(t *testing.T) {
	srv, _ := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/I_DRAFT/notes?owner=acme&project=7", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAPIInvalidJSON(t *testing.T) {
	srv, _ := fakeGraphQL(t, Options{}, apiRespond)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards?owner=acme&project=7", `{not json}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
