package ghprojects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// orgBoardJSON is a fixture board returned for organization project queries.
const orgBoardJSON = `{"organization":{"projectV2":{
  "id":"PVT_1","number":7,"title":"Board","url":"https://github.com/orgs/acme/projects/7",
  "fields":{"nodes":[
    {"__typename":"ProjectV2SingleSelectField","id":"F_ZONE","name":"Zone","dataType":"SINGLE_SELECT","options":[
       {"id":"o_gray","name":"Planned","color":"GRAY"},
       {"id":"o_red","name":"Critical","color":"RED"}]},
    {"__typename":"ProjectV2Field","id":"F_PROG","name":"Progress","dataType":"NUMBER"},
    {"__typename":"ProjectV2Field","id":"F_DAY","name":"Day","dataType":"DATE"},
    {"__typename":"ProjectV2SingleSelectField","id":"F_STATUS","name":"Status","dataType":"SINGLE_SELECT","options":[
       {"id":"s_todo","name":"Todo","color":"GRAY"},
       {"id":"s_doing","name":"In Progress","color":"YELLOW"}]}
  ]},
  "items":{"nodes":[
    {"id":"I_DRAFT","type":"DRAFT_ISSUE","createdAt":"2026-06-20T10:00:00Z",
     "content":{"__typename":"DraftIssue","id":"DI_1","title":"Draft card","body":"- [2026-06-21T09:00:00Z] first note","assignees":{"nodes":[]}},
     "fieldValues":{"nodes":[
        {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"o_red","name":"Critical","field":{"id":"F_ZONE","name":"Zone"}},
        {"__typename":"ProjectV2ItemFieldNumberValue","number":40,"field":{"id":"F_PROG","name":"Progress"}},
        {"__typename":"ProjectV2ItemFieldDateValue","date":"2026-06-26","field":{"id":"F_DAY","name":"Day"}}
     ]}},
    {"id":"I_ISSUE","type":"ISSUE","createdAt":"2026-06-19T10:00:00Z",
     "content":{"__typename":"Issue","id":"IS_1","number":12,"title":"Real issue","url":"https://github.com/acme/repo/issues/12","state":"OPEN","repository":{"nameWithOwner":"acme/repo"},"assignees":{"nodes":[{"login":"octocat"}]},"comments":{"nodes":[{"id":"C_1","body":"a comment","createdAt":"2026-06-19T11:00:00Z","author":{"login":"octocat"}}]}},
     "fieldValues":{"nodes":[
        {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"s_doing","name":"In Progress","field":{"id":"F_STATUS","name":"Status"}}
     ]}}
  ]}
}}}`

type recordedReq struct {
	query string
	vars  map[string]any
}

// fakeGitHub is a stub GraphQL endpoint that records requests and dispatches to
// a handler returning the data payload (raw JSON) for each operation.
type fakeGitHub struct {
	reqs    []recordedReq
	respond func(query string, vars map[string]any) string
}

func newClientWithFake(t *testing.T, respond func(query string, vars map[string]any) string) (*Client, *fakeGitHub) {
	t.Helper()
	fake := &fakeGitHub{respond: respond}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		fake.reqs = append(fake.reqs, recordedReq{query: body.Query, vars: body.Variables})
		data := fake.respond(body.Query, body.Variables)
		if data == "" {
			data = "{}"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":` + data + `}`))
	}))
	t.Cleanup(srv.Close)
	return New("test-token", WithEndpoint(srv.URL)), fake
}

// defaultRespond answers org project queries with the fixture and every mutation
// with an empty object.
func defaultRespond(query string, _ map[string]any) string {
	switch {
	case strings.Contains(query, "organization(login:") && strings.Contains(query, "projectV2(number:"):
		return orgBoardJSON
	case strings.Contains(query, "user(login: $login)"):
		return `{"user":{"id":"U_123"}}`
	case strings.Contains(query, "addProjectV2DraftIssue"):
		return `{"addProjectV2DraftIssue":{"projectItem":{"id":"I_NEW","content":{"id":"DI_NEW"}}}}`
	case strings.Contains(query, "node(id: $id)"):
		return `{"node":{"body":""}}`
	default:
		return "{}"
	}
}

func TestLoadBoardMapsCards(t *testing.T) {
	client, _ := newClientWithFake(t, defaultRespond)
	board, err := client.LoadBoard(context.Background(), "acme", 7)
	if err != nil {
		t.Fatalf("LoadBoard: %v", err)
	}
	if board.ID != "PVT_1" || len(board.Fields) != 4 || len(board.Cards) != 2 {
		t.Fatalf("board = %+v", board)
	}

	draft := board.cardByItemID("I_DRAFT")
	if draft == nil || !draft.IsDraft {
		t.Fatalf("draft card missing or not a draft: %+v", draft)
	}
	if draft.Zone != ZoneRed {
		t.Errorf("draft zone = %q, want red", draft.Zone)
	}
	if draft.Progress == nil || *draft.Progress != 40 {
		t.Errorf("draft progress = %v, want 40", draft.Progress)
	}
	if draft.Day != "2026-06-26" {
		t.Errorf("draft day = %q", draft.Day)
	}
	if len(draft.Notes) != 1 || draft.Notes[0].Source != "draft" || draft.Notes[0].Body != "first note" {
		t.Errorf("draft notes = %+v", draft.Notes)
	}
	if draft.Fields["Zone"] != "Critical" {
		t.Errorf("draft Fields[Zone] = %q, want Critical", draft.Fields["Zone"])
	}

	issue := board.cardByItemID("I_ISSUE")
	if issue == nil || issue.IsDraft {
		t.Fatalf("issue card missing or marked draft: %+v", issue)
	}
	if len(issue.Assignees) != 1 || issue.Assignees[0] != "octocat" {
		t.Errorf("issue assignees = %v", issue.Assignees)
	}
	if issue.Status != "In Progress" {
		t.Errorf("issue status = %q", issue.Status)
	}
	if len(issue.Notes) != 1 || issue.Notes[0].Source != "comment" {
		t.Errorf("issue notes = %+v", issue.Notes)
	}
}

func TestUpdateCardSetsZoneProgressDay(t *testing.T) {
	client, fake := newClientWithFake(t, defaultRespond)
	board, err := client.LoadBoard(context.Background(), "acme", 7)
	if err != nil {
		t.Fatalf("LoadBoard: %v", err)
	}
	zone := ZoneGray
	day := "2026-07-01"
	prog := 75.0
	err = client.UpdateCard(context.Background(), board, "I_DRAFT", UpdateCardInput{
		Zone:     &zone,
		Progress: &prog,
		Day:      &day,
	})
	if err != nil {
		t.Fatalf("UpdateCard: %v", err)
	}

	var sawSingleSelect, sawNumber, sawDate bool
	for _, r := range fake.reqs {
		switch {
		case strings.Contains(r.query, "singleSelectOptionId") && r.vars["option"] == "o_gray":
			sawSingleSelect = true
		case strings.Contains(r.query, "value: { number: $value }") && r.vars["value"] == 75.0:
			sawNumber = true
		case strings.Contains(r.query, "value: { date: $value }") && r.vars["value"] == "2026-07-01":
			sawDate = true
		}
	}
	if !sawSingleSelect || !sawNumber || !sawDate {
		t.Fatalf("missing mutation: select=%v number=%v date=%v", sawSingleSelect, sawNumber, sawDate)
	}
}

func TestUpdateCardClearsDayWithEmptyString(t *testing.T) {
	client, fake := newClientWithFake(t, defaultRespond)
	board, _ := client.LoadBoard(context.Background(), "acme", 7)
	empty := ""
	if err := client.UpdateCard(context.Background(), board, "I_DRAFT", UpdateCardInput{Day: &empty}); err != nil {
		t.Fatalf("UpdateCard: %v", err)
	}
	cleared := false
	for _, r := range fake.reqs {
		if strings.Contains(r.query, "clearProjectV2ItemFieldValue") && r.vars["field"] == "F_DAY" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected clearProjectV2ItemFieldValue for Day")
	}
}

func TestUpdateCardUnknownItem(t *testing.T) {
	client, _ := newClientWithFake(t, defaultRespond)
	board, _ := client.LoadBoard(context.Background(), "acme", 7)
	title := "x"
	err := client.UpdateCard(context.Background(), board, "NOPE", UpdateCardInput{Title: &title})
	if err == nil || !strings.Contains(err.Error(), "card not found") {
		t.Fatalf("err = %v, want card not found", err)
	}
}

func TestCreateCardAppliesZone(t *testing.T) {
	client, fake := newClientWithFake(t, defaultRespond)
	board, _ := client.LoadBoard(context.Background(), "acme", 7)
	card, err := client.CreateCard(context.Background(), board, CreateCardInput{
		Title:    "New work",
		Zone:     ZoneRed,
		Assignee: "octocat",
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if card == nil {
		t.Fatal("nil card")
	}
	var sawDraft, sawAssigneeResolve, sawZone bool
	for _, r := range fake.reqs {
		switch {
		case strings.Contains(r.query, "addProjectV2DraftIssue"):
			sawDraft = true
			if r.vars["title"] != "New work" {
				t.Errorf("draft title var = %v", r.vars["title"])
			}
		case strings.Contains(r.query, "user(login: $login)"):
			sawAssigneeResolve = true
		case strings.Contains(r.query, "singleSelectOptionId") && r.vars["item"] == "I_NEW" && r.vars["option"] == "o_red":
			sawZone = true
		}
	}
	if !sawDraft || !sawAssigneeResolve || !sawZone {
		t.Fatalf("create flow incomplete: draft=%v assignee=%v zone=%v", sawDraft, sawAssigneeResolve, sawZone)
	}
}

func TestAddNoteOnIssueAddsComment(t *testing.T) {
	client, fake := newClientWithFake(t, defaultRespond)
	board, _ := client.LoadBoard(context.Background(), "acme", 7)
	if err := client.AddNote(context.Background(), board, "I_ISSUE", "ship it"); err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	commented := false
	for _, r := range fake.reqs {
		if strings.Contains(r.query, "addComment") && r.vars["body"] == "ship it" && r.vars["subject"] == "IS_1" {
			commented = true
		}
	}
	if !commented {
		t.Fatal("expected addComment mutation")
	}
}

func TestAddNoteOnDraftAppendsBody(t *testing.T) {
	client, fake := newClientWithFake(t, defaultRespond)
	board, _ := client.LoadBoard(context.Background(), "acme", 7)
	if err := client.AddNote(context.Background(), board, "I_DRAFT", "draft note"); err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	updated := false
	for _, r := range fake.reqs {
		if strings.Contains(r.query, "updateProjectV2DraftIssue") && strings.Contains(r.query, "body: $body") {
			if body, ok := r.vars["body"].(string); ok && strings.Contains(body, "draft note") {
				updated = true
			}
		}
	}
	if !updated {
		t.Fatal("expected draft body update containing the note")
	}
}

func TestMoveAndDeleteCard(t *testing.T) {
	client, fake := newClientWithFake(t, defaultRespond)
	board, _ := client.LoadBoard(context.Background(), "acme", 7)
	after := "I_ISSUE"
	if err := client.MoveCard(context.Background(), board, "I_DRAFT", &after); err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	if err := client.DeleteCard(context.Background(), board, "I_DRAFT"); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}
	var moved, deleted bool
	for _, r := range fake.reqs {
		if strings.Contains(r.query, "updateProjectV2ItemPosition") && r.vars["after"] == "I_ISSUE" {
			moved = true
		}
		if strings.Contains(r.query, "deleteProjectV2Item") && r.vars["item"] == "I_DRAFT" {
			deleted = true
		}
	}
	if !moved || !deleted {
		t.Fatalf("moved=%v deleted=%v", moved, deleted)
	}
}

func TestLoadBoardNotFound(t *testing.T) {
	client, _ := newClientWithFake(t, func(query string, _ map[string]any) string {
		// Both org and user queries return null projects.
		if strings.Contains(query, "organization(login:") {
			return `{"organization":null}`
		}
		return `{"user":null}`
	})
	_, err := client.LoadBoard(context.Background(), "ghost", 99)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want not found", err)
	}
}
