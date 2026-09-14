package server

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// An MCP tool call stamps one action on the context and holds the write queue
// for its length, so a fan-out tool's per-card writes join a single commit
// instead of one commit each (finding: MCP commit flooding). A non-tool method
// is left alone.
func TestMCPActionMiddlewareGroupsAToolCall(t *testing.T) {
	be := &storeBackend{}
	mw := stampMCPAction(be)

	var id string
	var heldDuring bool
	h := mw(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		a := actionFrom(ctx)
		id = a.ID
		heldDuring = be.actionHeld(a.ID)
		return nil, nil
	})
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "rename_epic"}}
	if _, err := h(context.Background(), "tools/call", req); err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("no action id was stamped for a tool call")
	}
	if !heldDuring {
		t.Fatal("the action was not held while the tool ran")
	}
	if be.actionHeld(id) {
		t.Fatal("the action was not released after the tool ran")
	}

	// A non-tool method carries no action.
	stamped := false
	h2 := mw(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		stamped = actionFrom(ctx).ID != ""
		return nil, nil
	})
	if _, err := h2(context.Background(), "tools/list", nil); err != nil {
		t.Fatal(err)
	}
	if stamped {
		t.Fatal("a non-tool method must not stamp an action")
	}
}
