package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const boardJSON = `{"organization":{"projectV2":{
  "id":"PVT_1","number":7,"title":"Board","url":"https://example/7",
  "fields":{"nodes":[
    {"__typename":"ProjectV2SingleSelectField","id":"F_ZONE","name":"Zone","dataType":"SINGLE_SELECT","options":[
      {"id":"o_red","name":"Critical","color":"RED"}]}
  ]},
  "items":{"nodes":[
    {"id":"I_DRAFT","type":"DRAFT_ISSUE","createdAt":"2026-06-20T10:00:00Z",
     "content":{"__typename":"DraftIssue","id":"DI_1","title":"Draft","body":"","assignees":{"nodes":[]}},
     "fieldValues":{"nodes":[
       {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"o_red","name":"Critical","field":{"id":"F_ZONE","name":"Zone"}}
     ]}}
  ]}
}}}`

func fakeGitHub(t *testing.T) (string, *[]string) {
	t.Helper()
	var queries []string
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		queries = append(queries, body.Query)
		data := "{}"
		switch {
		case strings.Contains(body.Query, "organization(login:") && strings.Contains(body.Query, "projectV2(number:"):
			data = boardJSON
		case strings.Contains(body.Query, "addProjectV2DraftIssue"):
			data = `{"addProjectV2DraftIssue":{"projectItem":{"id":"I_NEW","content":{"id":"DI_NEW"}}}}`
		}
		_, _ = w.Write([]byte(`{"data":` + data + `}`))
	}))
	t.Cleanup(gh.Close)
	return gh.URL, &queries
}

// connect builds an in-memory MCP client session against an aeman MCP server.
func connect(t *testing.T, cfg Config) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := New(cfg)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func staticToken(string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return "test-token", nil }
}

func TestMCPListsTools(t *testing.T) {
	url, _ := fakeGitHub(t)
	cs := connect(t, Config{Owner: "acme", Project: 7, Endpoint: url, ResolveToken: staticToken("")})
	names := map[string]bool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		names[tool.Name] = true
	}
	for _, want := range []string{"get_board", "list_cards", "create_card", "update_card", "move_card", "delete_card", "add_note"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestMCPListCards(t *testing.T) {
	url, _ := fakeGitHub(t)
	cs := connect(t, Config{Owner: "acme", Project: 7, Endpoint: url, ResolveToken: staticToken("")})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_cards"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(res))
	}
	if !strings.Contains(textOf(res), "I_DRAFT") {
		t.Fatalf("result missing card: %s", textOf(res))
	}
}

func TestMCPCreateCard(t *testing.T) {
	url, queries := fakeGitHub(t)
	cs := connect(t, Config{Owner: "acme", Project: 7, Endpoint: url, ResolveToken: staticToken("")})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "create_card",
		Arguments: map[string]any{"title": "Hello"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(res))
	}
	created := false
	for _, q := range *queries {
		if strings.Contains(q, "addProjectV2DraftIssue") {
			created = true
		}
	}
	if !created {
		t.Fatal("expected a draft creation request")
	}
}

func TestMCPMissingBoardConfig(t *testing.T) {
	url, _ := fakeGitHub(t)
	cs := connect(t, Config{Endpoint: url, ResolveToken: staticToken("")})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_cards"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError || !strings.Contains(textOf(res), "owner and project are required") {
		t.Fatalf("expected board-required error, got isError=%v text=%s", res.IsError, textOf(res))
	}
}

func textOf(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}
