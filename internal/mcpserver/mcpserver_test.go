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

// call invokes a tool and fails the test on a transport or tool error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s error: %s", name, textOf(res))
	}
	return res
}

// callErr invokes a tool and requires a tool error, returning its text.
func callErr(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s: expected a tool error, got %s", name, textOf(res))
	}
	return textOf(res)
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
		"get_board", "list_cards", "get_card", "create_card", "update_card",
		"delete_card", "remove_card", "move_card", "defer_card", "in_progress",
		"send_to_review", "remove_reviewer", "take_into_plan", "release_from_plan",
		"carry_over", "carry_week",
		"list_links", "list_notes", "add_note", "edit_note", "delete_note",
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

func TestMCPGetBoard(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{
		"alpha": {Current: "2026-07-01"},
	})
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "get_board", nil)
	if !strings.Contains(textOf(res), `"alpha"`) {
		t.Fatalf("board missing team roster: %s", textOf(res))
	}
}

func TestMCPListCardsTeamView(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", StartDate: today, SprintStart: today},
		{ItemID: "c2", Team: "beta", StartDate: today, SprintStart: today},
	}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "list_cards", map[string]any{"view": "team", "team": "alpha"})
	if !strings.Contains(textOf(res), "c1") || strings.Contains(textOf(res), "c2") {
		t.Fatalf("team view should hold exactly c1: %s", textOf(res))
	}
}

func TestMCPListCardsZoneFilterIsSemantic(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Zone: board.ZoneRed},
		{ItemID: "c2", Zone: board.ZoneGray},
	}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "list_cards", map[string]any{"zone": "urgent"})
	if !strings.Contains(textOf(res), "c1") || strings.Contains(textOf(res), "c2") {
		t.Fatalf("zone=urgent should hold exactly c1: %s", textOf(res))
	}
	if msg := callErr(t, cs, "list_cards", map[string]any{"zone": "red"}); !strings.Contains(msg, "unknown zone") {
		t.Fatalf("colour zones must be rejected: %s", msg)
	}
	if msg := callErr(t, cs, "list_cards", map[string]any{"view": "nope"}); !strings.Contains(msg, "unknown view") {
		t.Fatalf("unknown views must be rejected: %s", msg)
	}
}

func TestMCPGetCard(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Title: "x", Zone: board.ZoneRed}}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "get_card", map[string]any{"uid": "c1"})
	if !strings.Contains(textOf(res), `"urgent"`) {
		t.Fatalf("card zone should be semantic: %s", textOf(res))
	}
	if msg := callErr(t, cs, "get_card", map[string]any{"uid": "ghost"}); !strings.Contains(msg, "card not found") {
		t.Fatalf("missing card should error: %s", msg)
	}
}

func TestMCPCreateCard(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "create_card", map[string]any{"team": "alpha", "title": "Hello", "zone": "urgent"})
	if len(fake.Creates()) != 1 || fake.Creates()[0].Title != "Hello" {
		t.Fatalf("creates = %+v", fake.Creates())
	}
	if fake.Creates()[0].Zone != board.ZoneRed {
		t.Fatalf("semantic zone should map to the domain key, got %q", fake.Creates()[0].Zone)
	}
	if !strings.Contains(textOf(res), `"Card"`) {
		t.Fatalf("create should return the Card resource: %s", textOf(res))
	}
}

func TestMCPUpdateCardPatchesOnlyProvidedFields(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Title: "x", Assignees: []string{"kvaps"}}}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "update_card", map[string]any{"uid": "c1", "progress": 40})
	if !fake.Saw("SetProgress c1 40") {
		t.Fatalf("progress not applied")
	}
	for _, untouched := range []string{"RenameCard", "SetAssignee", "SetTeam", "SetZone", "SetStage", "SetStart", "SetDay"} {
		if fake.Count(untouched) != 0 {
			t.Fatalf("%s must not run on a progress-only patch", untouched)
		}
	}
	if !strings.Contains(textOf(res), `"progress":40`) {
		t.Fatalf("update should return the patched Card: %s", textOf(res))
	}
}

func TestMCPUpdateCardEmptyStringClears(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Title: "x", Assignees: []string{"kvaps"}}}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	call(t, cs, "update_card", map[string]any{"uid": "c1", "title": "y", "assignee": ""})
	if !fake.Saw("RenameCard c1") || fake.Card("c1").Title != "y" {
		t.Fatalf("rename not applied: %+v", fake.Card("c1"))
	}
	if !fake.Saw("SetAssignee c1 ") || len(fake.Card("c1").Assignees) != 0 {
		t.Fatalf("empty assignee should unassign: %+v", fake.Card("c1"))
	}
}

func TestMCPUpdateCardDates(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", StartDate: "2026-07-01", Day: "2026-07-05"}}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	// start alone: calendar semantics, the current end is kept.
	call(t, cs, "update_card", map[string]any{"uid": "c1", "start": "2026-07-02"})
	if !fake.Saw("SetStart c1 2026-07-02") || !fake.Saw("SetDay c1 2026-07-05") || !fake.Saw("SetSprintStart c1 2026-07-02") {
		t.Fatalf("start-only patch should relocate keeping the end")
	}
	// end alone: only the due day moves.
	call(t, cs, "update_card", map[string]any{"uid": "c1", "end": "2026-07-09"})
	if !fake.Saw("SetDay c1 2026-07-09") || fake.Count("SetStart") != 1 {
		t.Fatalf("end-only patch must not touch start")
	}
	// sprint alone: only the membership moves.
	call(t, cs, "update_card", map[string]any{"uid": "c1", "sprint": "2026-06-30"})
	if !fake.Saw("SetSprintStart c1 2026-06-30") || fake.Count("SetStart") != 1 || fake.Count("SetDay") != 2 {
		t.Fatalf("sprint-only patch must not touch the calendar dates")
	}
}

func TestMCPUpdateCardReviewOf(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1"}, {ItemID: "c2"}}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	call(t, cs, "update_card", map[string]any{"uid": "c2", "reviewOf": "c1"})
	if !fake.Saw("SetReviewOf c2 c1") || fake.Card("c2").ReviewOf != "c1" {
		t.Fatalf("reviewOf not applied: %+v", fake.Card("c2"))
	}
}

func TestMCPDeleteCardCascades(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "orig", Title: "x"},
		{ItemID: "rev", ReviewOf: "orig"},
	}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	call(t, cs, "delete_card", map[string]any{"uid": "orig"})
	if fake.Card("orig") != nil || fake.Card("rev") != nil {
		t.Fatalf("both cards should be gone")
	}
	if fake.Count("DeleteCard") != 2 {
		t.Fatalf("want 2 deletes, got %d", fake.Count("DeleteCard"))
	}
}

func TestMCPRemoveCard(t *testing.T) {
	// A card outside any current sprint has nothing to demote to: real delete.
	fake := boardservicetest.New([]board.Card{{ItemID: "c1"}}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	call(t, cs, "remove_card", map[string]any{"uid": "c1"})
	if fake.Card("c1") != nil {
		t.Fatalf("card should be deleted")
	}
	if msg := callErr(t, cs, "remove_card", map[string]any{"uid": "c1", "from": "nowhere"}); !strings.Contains(msg, "unknown from") {
		t.Fatalf("unknown from must be rejected: %s", msg)
	}
}

func TestMCPSendToReview(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "orig", Title: "x", Team: "alpha", StartDate: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today}})
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "send_to_review", map[string]any{"uid": "orig", "reviewer": "bob"})
	if len(fake.Creates()) != 1 || fake.Creates()[0].ReviewOf != "orig" || fake.Creates()[0].Assignee != "bob" {
		t.Fatalf("creates = %+v", fake.Creates())
	}
	if !strings.Contains(textOf(res), "review: x") {
		t.Fatalf("should return the review card: %s", textOf(res))
	}
	if fake.Card("orig").Stage != board.StageReview {
		t.Fatalf("original should be on review, got %q", fake.Card("orig").Stage)
	}
}

func TestMCPCarryOverDryRun(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", SprintStart: "2026-06-25", Progress: 50},
	}, map[string]board.SprintState{"alpha": {Current: "2026-06-25"}})
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "carry_over", map[string]any{"team": "alpha", "dryRun": true})
	if !strings.Contains(textOf(res), `"carried":1`) {
		t.Fatalf("dry run should report the count: %s", textOf(res))
	}
	if fake.Count("SetSprintState") != 0 || fake.Count("SetSprintStart") != 0 {
		t.Fatalf("dry run must not write")
	}
}

func TestMCPNotes(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Notes: []board.Note{{ID: "n1", Body: "hello", Source: "comment"}}},
	}, nil)
	cs := connect(t, Config{Owner: "acme", Project: 1}, fake)
	res := call(t, cs, "list_notes", map[string]any{"uid": "c1"})
	if !strings.Contains(textOf(res), "n1") || !strings.Contains(textOf(res), "hello") {
		t.Fatalf("notes missing: %s", textOf(res))
	}
	call(t, cs, "add_note", map[string]any{"uid": "c1", "text": "yo"})
	if !fake.Saw("AddNote c1 yo") {
		t.Fatalf("note not added")
	}
	call(t, cs, "edit_note", map[string]any{"uid": "c1", "noteId": "n1", "text": "hi"})
	if !fake.Saw("EditNote c1 n1 hi") {
		t.Fatalf("note not edited")
	}
	call(t, cs, "delete_note", map[string]any{"uid": "c1", "noteId": "n1"})
	if !fake.Saw("DeleteNote c1 n1") {
		t.Fatalf("note not deleted")
	}
	if msg := callErr(t, cs, "edit_note", map[string]any{"uid": "c1", "noteId": "ghost", "text": "x"}); !strings.Contains(msg, "note not found") {
		t.Fatalf("missing note should error: %s", msg)
	}
}

func TestMCPMissingBoardConfig(t *testing.T) {
	cs := connect(t, Config{}, boardservicetest.New(nil, nil))
	if msg := callErr(t, cs, "list_cards", nil); !strings.Contains(msg, "owner and project are required") {
		t.Fatalf("expected board-required error, got %s", msg)
	}
}
