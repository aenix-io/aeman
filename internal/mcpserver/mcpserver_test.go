package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-org/aeman/internal/board"
	"github.com/aenix-org/aeman/internal/boardservice"
	"github.com/aenix-org/aeman/internal/boardservice/boardservicetest"
)

// connect builds an in-memory MCP client session against an aeman MCP server
// whose board backend is the given fake, so the tools exercise the real
// boardservice logic without touching GitHub.
func connect(t *testing.T, cfg Config, backend boardservice.Backend) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	h := &server{cfg: cfg}
	h.newBackend = func(context.Context) (boardservice.Backend, error) { return backend, nil }
	srv := h.mcpServer()

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

func textOf(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestMCPListsTools(t *testing.T) {
	cs := connect(t, Config{Owner: "acme", Project: 1}, boardservicetest.New(nil, nil))
	names := map[string]bool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		names[tool.Name] = true
	}
	want := []string{
		"get_board", "team_view", "me_view", "weekly_plan", "create_card",
		"carry_over", "carry_week", "set_stage", "set_in_progress", "set_progress",
		"send_to_review", "reassign_reviewer", "remove_reviewer", "set_assignee",
		"set_team", "take_into_plan", "release_from_plan", "move_card",
		"delete_card", "add_note", "rename_card",
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("missing tool %q", w)
		}
	}
	if len(names) != len(want) {
		t.Errorf("tool count = %d, want %d (%v)", len(names), len(want), names)
	}
}

func TestMCPTeamView(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: today, SprintStart: today},
	}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "team_view",
		Arguments: map[string]any{"team": "alpha"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(res))
	}
	if !strings.Contains(textOf(res), "c1") {
		t.Fatalf("result missing card: %s", textOf(res))
	}
}

func TestMCPCreateCard(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "create_card",
		Arguments: map[string]any{"team": "alpha", "title": "Hello"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(res))
	}
	if len(fake.Creates()) != 1 || fake.Creates()[0].Title != "Hello" {
		t.Fatalf("creates = %+v", fake.Creates())
	}
}

func TestMCPDeleteCardCascades(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "orig", Title: "x"},
		{ItemID: "rev", ReviewOf: "orig"},
	}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "delete_card",
		Arguments: map[string]any{"itemId": "orig"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(res))
	}
	if fake.Card("orig") != nil || fake.Card("rev") != nil {
		t.Fatalf("both cards should be gone")
	}
	if fake.Count("DeleteCard") != 2 {
		t.Fatalf("want 2 deletes, got %d", fake.Count("DeleteCard"))
	}
}

func TestMCPMissingBoardConfig(t *testing.T) {
	cs := connect(t, Config{}, boardservicetest.New(nil, nil))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "team_view"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError || !strings.Contains(textOf(res), "owner and project are required") {
		t.Fatalf("expected board-required error, got isError=%v text=%s", res.IsError, textOf(res))
	}
}
