package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
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

// toolDescription is what an AGENT is told about a tool: the description the
// server advertises over the wire, read the way a client reads it.
func toolDescription(t *testing.T, name string) string {
	t.Helper()
	cs := connect(t, Config{}, boardservicetest.New(nil, nil))
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == name {
			return tool.Description
		}
	}
	t.Fatalf("no tool named %q", name)
	return ""
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

// toolSurface is everything an AGENT is told about a tool: its description
// plus the description of every argument, which is where most of the rules
// are actually written (update_card alone has twenty).
func toolSurface(t *testing.T, name string) string {
	t.Helper()
	cs := connect(t, Config{}, boardservicetest.New(nil, nil))
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != name {
			continue
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", name, err)
		}
		return tool.Description + " " + string(schema)
	}
	t.Fatalf("no tool named %q", name)
	return ""
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
	cs := connect(t, Config{Board: "acme"}, boardservicetest.New(nil, nil))
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
		"send_to_review", "remove_reviewer", "carry_over",
		"list_links", "list_log", "list_notes", "add_note", "edit_note", "delete_note",
		"add_epic", "delete_epic", "set_epic_project", "rename_epic",
		"add_project", "delete_project", "reorder_projects", "rename_project", "rename_team",
		"add_deadline", "delete_deadline", "move_deadline",
		"list_processes", "add_process", "delete_process", "rename_process",
		"set_process_project", "set_process_paused",
		"reorder_processes", "reorder_process_tasks", "reopen_card",
		"add_process_task", "update_process_task", "delete_process_task",
		"mirror_card", "unmirror_card", "remove_from_project",
		"set_capacity", "set_team_capacity",
		// The gestures and reads the boards have and the tool set did not.
		"place_card", "untriage_card", "finished_earlier", "list_sprints",
		"list_day_logs", "reorder_teams", "reorder_epics", "delete_team",
		"set_sprint_state",
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
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
	res := call(t, cs, "list_cards", map[string]any{"view": "all", "zone": "urgent"})
	if !strings.Contains(textOf(res), "c1") || strings.Contains(textOf(res), "c2") {
		t.Fatalf("zone=urgent should hold exactly c1: %s", textOf(res))
	}
	if msg := callErr(t, cs, "list_cards", map[string]any{"zone": "red"}); !strings.Contains(msg, "unknown zone") {
		t.Fatalf("colour zones must be rejected: %s", msg)
	}
	// A board nobody has, said as such — the same sentinel every door answers
	// with, so an agent reads one message wherever it asked.
	if msg := callErr(t, cs, "list_cards", map[string]any{"view": "nope"}); !strings.Contains(msg, "no such board") {
		t.Fatalf("a board nobody has must be rejected: %s", msg)
	}
}

func TestMCPGetCard(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Title: "x", Zone: board.ZoneRed}}, nil)
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
	// Typed into the team's grid: urgent is the plan speaking, and the Me
	// board adds as unplanned and nothing else (boardservice.addsUnplanned).
	res := call(t, cs, "create_card", map[string]any{"view": "team", "team": "alpha", "title": "Hello", "zone": "urgent"})
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
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
	call(t, cs, "delete_card", map[string]any{"uid": "orig"})
	if fake.Card("orig") != nil || fake.Card("rev") != nil {
		t.Fatalf("both cards should be gone")
	}
	if fake.Count("DeleteCard") != 2 {
		t.Fatalf("want 2 deletes, got %d", fake.Count("DeleteCard"))
	}
}

func TestMCPRemoveCard(t *testing.T) {
	// remove_card empties the working area. A card that is also scheduled for
	// a WEEK is left in that week — whatever it carries; deleting
	// deliberately is delete_card's job.
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Progress: 40, Week: "2026-08-24",
		SprintStart: "2026-08-28", StartDate: "2026-08-28"}}, nil)
	cs := connect(t, Config{Board: "acme"}, fake)
	call(t, cs, "remove_card", map[string]any{"uid": "c1"})
	c := fake.Card("c1")
	if c == nil {
		t.Fatalf("a card scheduled for a week must not be deleted by remove_card")
	}
	if c.Week != "2026-08-24" || c.SprintStart != "" {
		t.Fatalf("it keeps its week and leaves the working area: %+v", c)
	}
	// A card that is nowhere else — no week, no column — was only in the
	// working area, and removing it from there is deletion; what it carries
	// is the caller's to ask about first.
	fake2 := boardservicetest.New([]board.Card{{ItemID: "c2", Progress: 40}}, nil)
	cs2 := connect(t, Config{Board: "acme"}, fake2)
	call(t, cs2, "remove_card", map[string]any{"uid": "c2"})
	if fake2.Card("c2") != nil {
		t.Fatal("a card with nowhere else to be is deleted by remove_card")
	}
}

func TestMCPSendToReview(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "orig", Title: "x", Team: "alpha", StartDate: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today}})
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
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
	cs := connect(t, Config{Board: "acme"}, fake)
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
	if msg := callErr(t, cs, "list_cards", nil); !strings.Contains(msg, "board is required") {
		t.Fatalf("expected board-required error, got %s", msg)
	}
}

func TestMCPListDefaultsToMe(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "mine", Team: "alpha", Assignees: []string{"bob"}, Progress: 40, SprintStart: today},
		{ItemID: "theirs", Team: "alpha", Assignees: []string{"carol"}, Progress: 40, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	cs := connect(t, Config{Board: "acme", ResolveLogin: func(context.Context) (string, error) { return "bob", nil }}, fake)
	// No view → the caller's own Me board.
	res := call(t, cs, "list_cards", map[string]any{})
	if !strings.Contains(textOf(res), "mine") || strings.Contains(textOf(res), "theirs") {
		t.Fatalf("default list must be the caller's Me board: %s", textOf(res))
	}
	// view=all lists the whole board.
	all := call(t, cs, "list_cards", map[string]any{"view": "all"})
	if !strings.Contains(textOf(all), "mine") || !strings.Contains(textOf(all), "theirs") {
		t.Fatalf("view=all must list the whole board: %s", textOf(all))
	}
}

// list_cards mirrors the board's row view: light rows by default (no bodies),
// a title substring resolves a mentioned card to its uid in one call, and
// full=true remains for genuine bulk reads. get_card stays the detail pane.
func TestMCPListCardsRowsAndTitleFilter(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Team: "alpha", Title: "Fix DRBD split-brain", Description: "long body https://github.com/acme/repo/pull/7"},
		{ItemID: "c2", Team: "alpha", Title: "Renew TLS certificates", Description: "another body"},
	}, nil)
	cs := connect(t, Config{Board: "acme"}, fake)

	rows := textOf(call(t, cs, "list_cards", map[string]any{"view": "all"}))
	if strings.Contains(rows, "long body") || strings.Contains(rows, "another body") {
		t.Fatalf("rows must not carry bodies: %s", rows)
	}
	if !strings.Contains(rows, `"links"`) || !strings.Contains(rows, `"pull"`) {
		t.Fatalf("rows must carry the derived link refs: %s", rows)
	}

	filtered := textOf(call(t, cs, "list_cards", map[string]any{"view": "all", "title": "drbd"}))
	if !strings.Contains(filtered, "c1") || strings.Contains(filtered, "c2") {
		t.Fatalf("title filter should keep exactly c1: %s", filtered)
	}

	full := textOf(call(t, cs, "list_cards", map[string]any{"view": "all", "full": true}))
	if !strings.Contains(full, "long body") {
		t.Fatalf("full=true must carry bodies: %s", full)
	}

	card := textOf(call(t, cs, "get_card", map[string]any{"uid": "c1"}))
	if !strings.Contains(card, "long body") {
		t.Fatalf("get_card is the detail pane, body missing: %s", card)
	}
}

// A TOOL DESCRIPTION IS A PROMISE TO AN AGENT, and an agent has no other way
// to learn the rules: it cannot read the board's code or try a gesture to see
// what happens. So a description that names a refusal must name one the
// server actually makes.
//
// create_card promised a 403 for filing work for YOURSELF in a planned zone.
// The service stopped refusing that on 2026-09-03 (S11): the Team and Triage
// grids send the same create, so the guard refused the boards whose whole
// purpose is planning, and it protected nothing anyway — the same card could
// be made unplanned and moved with a zone patch. The description was left
// behind, so an agent creating an urgent card for its own user read its own
// success as a hole in the server and reported it as one.
//
// This test does not read the prose; it holds the two ends together. If the
// refusal comes back, TestAPersonMayPlanTheirOwnWork fails and the sentence
// can return with it.
func TestCreateCardDescribesNoRefusalTheServerDoesNotMake(t *testing.T) {
	desc := toolDescription(t, "create_card")
	for _, gone := range []string{"403", "unplanned work", "refused (403)"} {
		if strings.Contains(desc, gone) {
			t.Errorf("create_card still promises %q; the service has not refused that since S11", gone)
		}
	}
	// remove_card's own refusal IS still made (ErrNotYoursToRemove), so its
	// description keeps saying so — the check above must not be read as "no
	// tool may mention a refusal".
	if !strings.Contains(toolDescription(t, "remove_card"), "403") {
		t.Error("remove_card stopped naming the refusal the service still makes")
	}
}

// Four more descriptions that told an agent something the service does not do.
// An agent cannot try a gesture to see what happens — the description IS the
// rule for it — so each of these sent it into a wrong move, and the worst of
// them destroys a card.
func TestDescriptionsMatchWhatTheServiceDoes(t *testing.T) {
	// update_process_task said the running iteration is "left exactly as it
	// is". Content is; ROUTING is not: a team or assignee change deletes an
	// untouched turn and spawns a fresh one for the new owner
	// (routeOpenIterations, pinned by TestReassigningATurnReplacesTheUntouchedCard).
	task := toolDescription(t, "update_process_task")
	if strings.Contains(task, "left exactly as it is") {
		t.Error("update_process_task still promises the running turn is untouched; reassigning deletes it")
	}
	for _, want := range []string{"ROUTING", "deleted"} {
		if !strings.Contains(task, want) {
			t.Errorf("update_process_task should say what reassigning does to the live turn (%q)", want)
		}
	}

	// A deadline belongs to a PROJECT: one line per project per week, so two
	// projects due the same week are two lines (board.FindDeadline matches on
	// both fields; TestDeadlines pins it).
	for _, tool := range []string{"add_deadline", "move_deadline"} {
		d := toolDescription(t, tool)
		if strings.Contains(d, "A week holds at most one line") ||
			strings.Contains(d, "two deadlines on one date are one deadline") {
			t.Errorf("%s still describes deadlines as one-per-week; they are one per (project, week)", tool)
		}
		if !strings.Contains(d, "project") {
			t.Errorf("%s must say a deadline belongs to a project", tool)
		}
	}

	// The three kinds of roster name do NOT share a namespace: nameFree
	// switches into three separate maps, so a team may carry a project's name.
	// What is shared is the repositories.
	if team := toolDescription(t, "rename_team"); strings.Contains(team, "are one namespace") {
		t.Error("rename_team still claims teams, projects and processes share a namespace; they do not")
	}

	// And a team name nothing declares does not fail: it declares a team (G39),
	// so a typo leaves a ghost on the board. The description has to say so,
	// because an agent types the name rather than picking it from a list.
	if task := toolDescription(t, "add_process_task"); strings.Contains(task, "MUST be an existing team key") {
		t.Error("add_process_task still promises a validation nothing performs")
	}

	// The rules that used to live in the browser are the service's now, so an
	// agent meets them too — and a description that does not name them turns
	// a rule into a surprise. Each is read off the ARGUMENT it belongs to:
	// update_card is one tool with twenty of them.
	update := toolSurface(t, "update_card")
	for _, want := range []struct{ rule, needle string }{
		// A process turn moves only inside its own occurrence (G66).
		{"a turn's week is bounded by its cycle", "occurrence"},
		// The shelf refuses a review card and a subtask (G66).
		{"the shelf refuses a card with no place of its own", "a place of its own to be parked out of"},
		// And a review card takes neither review nor recurrent.
		{"a review card cannot be reviewed or made recurrent", "review of a review"},
	} {
		if !strings.Contains(update, want.needle) {
			t.Errorf("update_card does not say %s (%q)", want.rule, want.needle)
		}
	}
}

// WHAT THE BOARDS CAN DO, AN AGENT CAN DO (ADR 0002). An audit of the live
// tool set against the SPA found ten gestures a person makes with a mouse and
// no agent could make at all — and the pattern was not oversight but drift:
// each was added to a board and never to the tool set beside it.
func TestTheToolSetHoldsWhatTheBoardsDo(t *testing.T) {
	cs := connect(t, Config{Board: "acme"}, boardservicetest.New(nil, nil))
	have := map[string]bool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		have[tool.Name] = true
	}
	for _, name := range []string{
		// The day boards' own answers.
		"finished_earlier",
		// The Triage board's, beside place (mirror_card's neighbour on the
		// grid): a card pulled back out of every week into the strip.
		"untriage_card", "place_card",
		// The roster gestures the manage dialogs make.
		"reorder_teams", "reorder_epics", "delete_team", "set_sprint_state",
		// What a day board reads in one request instead of a log per card.
		"list_day_logs",
		// And when each team's sprint began, which every date rule is
		// reckoned against and no tool answered.
		"list_sprints",
	} {
		if !have[name] {
			t.Errorf("the boards do %s and no tool does", name)
		}
	}
}

// The BOARD is an argument here as it is a path segment over HTTP: an agent
// says which board it is standing on, and the server holds it to it.
func TestAnAgentSaysWhichBoardItIsStandingOn(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "mine", Title: "mine", Team: "alpha", Assignees: []string{"kvaps"},
			Week: board.MondayOf(today), StartDate: today, Day: today, SprintStart: today},
		{ItemID: "theirs", Title: "theirs", Team: "alpha", Assignees: []string{"carol"},
			Week: board.MondayOf(today), StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	cs := connect(t, Config{Board: "acme", ResolveLogin: func(context.Context) (string, error) {
		return "kvaps", nil
	}}, fake)

	// Standing on my own board, somebody else's card is not mine to remove.
	if msg := callErr(t, cs, "remove_card", map[string]any{
		"uid": "theirs", "view": "me", "intent": "unassign",
	}); !strings.Contains(msg, "not on the me board") {
		t.Fatalf("removing another person's card from my board = %q", msg)
	}
	// From the board that draws it, the same press lands.
	call(t, cs, "remove_card", map[string]any{"uid": "theirs", "view": "team", "team": "alpha", "intent": "unassign"})
}

// A card typed into a board is that board's kind of card, over MCP too.
func TestAnAgentCreatesIntoABoard(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	cs := connect(t, Config{Board: "acme", ResolveLogin: func(context.Context) (string, error) {
		return "kvaps", nil
	}}, fake)

	call(t, cs, "create_card", map[string]any{
		"view": "backlog", "title": "someday", "team": "alpha", "zone": "planned",
	})
	b, err := fake.LoadBoard(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	var parked, found bool
	for _, c := range b.Cards {
		if c.Title == "someday" {
			found, parked = true, c.Parked
		}
	}
	if !found || !parked {
		t.Fatalf("a card typed into the drawer: found=%v parked=%v", found, parked)
	}
	// Naming no board files the card on the person the agent is acting for,
	// not in the team's Unassigned column: an engineer who asks for a card
	// means one of their own, and the listing defaults the same way.
	call(t, cs, "create_card", map[string]any{"title": "mine", "team": "alpha"})
	b, err = fake.LoadBoard(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b.Cards {
		if c.Title == "mine" {
			if len(c.Assignees) != 1 || c.Assignees[0] != "kvaps" {
				t.Fatalf("a card created with no board named = %v, want the caller", c.Assignees)
			}
		}
	}

	// And a board refuses the fields it does not own, by name. A card filed
	// into a week AHEAD stands on no day: it waits for its Monday (B1). (The
	// row that is NOW is the exception — that card carries today's days.)
	if msg := callErr(t, cs, "create_card", map[string]any{
		"view": "triage", "title": "later", "team": "alpha", "zone": "planned",
		"week": board.AddDays(board.MondayOf(today), 7), "start": today,
	}); !strings.Contains(msg, "does not take") {
		t.Fatalf("a dated card typed into a week ahead = %q", msg)
	}
}
