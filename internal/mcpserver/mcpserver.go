// Package mcpserver exposes aeman's board operations as a Model Context Protocol
// (MCP) server over stdio. Its tools mirror the UI's views and actions by
// calling the boardservice layer (the same logic the web frontend runs), not by
// proxying GitHub directly.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-org/aeman/internal/boardservice"
	"github.com/aenix-org/aeman/internal/ghprojects"
)

// Config configures the MCP server.
type Config struct {
	// Owner is the default GitHub org/user.
	Owner string
	// Project is the default GitHub Project number.
	Project int
	// Lock pins owner/project, ignoring per-tool overrides.
	Lock bool
	// Version is reported to MCP clients.
	Version string
	// ResolveToken returns a GitHub token for the current call.
	ResolveToken func(ctx context.Context) (string, error)
	// Endpoint overrides the GraphQL endpoint (used in tests).
	Endpoint string
	// HTTPClient overrides the HTTP client (used in tests).
	HTTPClient ghprojects.Doer
	// WrapBackend, when set, wraps the production backend — the HTTP server uses
	// it to route MCP mutations through its shared board store, so they update
	// the cache and reach watch clients like every other write.
	WrapBackend func(boardservice.Backend) boardservice.Backend
}

// Serve runs an MCP server over stdio until ctx is cancelled or the client
// disconnects.
func Serve(ctx context.Context, s *mcp.Server) error {
	return s.Run(ctx, &mcp.StdioTransport{})
}

// server holds the configuration shared by all tool handlers.
type server struct {
	cfg Config
	// newBackend builds the board backend for a call. It defaults to a
	// ghprojects client over a freshly resolved token and is overridden in tests
	// with a fake Backend.
	newBackend func(ctx context.Context) (boardservice.Backend, error)
}

// New builds an MCP server with the aeman tool set.
func New(cfg Config) *mcp.Server {
	h := &server{cfg: cfg}
	h.newBackend = h.defaultBackend
	return h.mcpServer()
}

// mcpServer registers the tool set on a fresh MCP server. The tools mirror the
// /api/v1 operation set: every name maps to one boardservice method.
func (h *server) mcpServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "aeman", Version: h.cfg.Version}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "get_board", Description: "Get the board identity, field metadata and the per-team sprint pointers."}, h.getBoard)
	mcp.AddTool(s, &mcp.Tool{Name: "team_view", Description: "List the Team board's cards for a team on a day (day defaults to today)."}, h.teamView)
	mcp.AddTool(s, &mcp.Tool{Name: "me_view", Description: "List a person's day-board cards (user defaults to everyone, day to today)."}, h.meView)
	mcp.AddTool(s, &mcp.Tool{Name: "weekly_plan", Description: "Get a team's weekly-plan cards for a week, split into the wed/fri bands."}, h.weeklyPlan)
	mcp.AddTool(s, &mcp.Tool{Name: "create_card", Description: "Create a card that joins (or starts) its team's sprint."}, h.createCard)
	mcp.AddTool(s, &mcp.Tool{Name: "carry_over", Description: "Advance a team's sprint to today and carry its unfinished cards forward."}, h.carryOver)
	mcp.AddTool(s, &mcp.Tool{Name: "carry_week", Description: "Pull a team's unfinished plan cards from earlier weeks into the target week."}, h.carryWeek)
	mcp.AddTool(s, &mcp.Tool{Name: "set_stage", Description: "Set a card's stage: locked, review, recurrent, done, or empty to clear it."}, h.setStage)
	mcp.AddTool(s, &mcp.Tool{Name: "set_in_progress", Description: "Move a card to the implicit In Progress status (clears the stage)."}, h.setInProgress)
	mcp.AddTool(s, &mcp.Tool{Name: "set_progress", Description: "Set a card's readiness percentage (0..100), running the done auto-link."}, h.setProgress)
	mcp.AddTool(s, &mcp.Tool{Name: "send_to_review", Description: "Create a linked review card for a reviewer and put the card on review."}, h.sendToReview)
	mcp.AddTool(s, &mcp.Tool{Name: "reassign_reviewer", Description: "Point a card's review at another reviewer (sends to review if it has none)."}, h.reassignReviewer)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_reviewer", Description: "Delete a card's linked review card."}, h.removeReviewer)
	mcp.AddTool(s, &mcp.Tool{Name: "set_assignee", Description: "Set or clear a card's assignee."}, h.setAssignee)
	mcp.AddTool(s, &mcp.Tool{Name: "set_team", Description: "Move a card to a team and join that team's current sprint."}, h.setTeam)
	mcp.AddTool(s, &mcp.Tool{Name: "take_into_plan", Description: "Take a weekly-plan card into work: assign it and join the team's sprint."}, h.takeIntoPlan)
	mcp.AddTool(s, &mcp.Tool{Name: "release_from_plan", Description: "Release a card from the weekly plan."}, h.releaseFromPlan)
	mcp.AddTool(s, &mcp.Tool{Name: "move_card", Description: "Reorder a card to sit after another; an empty afterId moves it to the top."}, h.moveCard)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_card", Description: "Delete a card, cascading to its linked review card."}, h.deleteCard)
	mcp.AddTool(s, &mcp.Tool{Name: "add_note", Description: "Append a work note to a card."}, h.addNote)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_card", Description: "Change a card's title."}, h.renameCard)
	return s
}

// resolve picks the effective owner/project, honouring the lock and defaults.
func (h *server) resolve(owner string, project int) (string, int, error) {
	o, p := h.cfg.Owner, h.cfg.Project
	if !h.cfg.Lock {
		if owner != "" {
			o = owner
		}
		if project != 0 {
			p = project
		}
	}
	if o == "" || p == 0 {
		return "", 0, fmt.Errorf("owner and project are required (pass them or configure server defaults)")
	}
	return o, p, nil
}

// defaultBackend builds a ghprojects client with a freshly resolved token. It is
// the production newBackend (*ghprojects.Client satisfies boardservice.Backend).
func (h *server) defaultBackend(ctx context.Context) (boardservice.Backend, error) {
	tok, err := h.cfg.ResolveToken(ctx)
	if err != nil {
		return nil, err
	}
	opts := []ghprojects.Option{}
	if h.cfg.HTTPClient != nil {
		opts = append(opts, ghprojects.WithHTTPClient(h.cfg.HTTPClient))
	}
	if h.cfg.Endpoint != "" {
		opts = append(opts, ghprojects.WithEndpoint(h.cfg.Endpoint))
	}
	var backend boardservice.Backend = ghprojects.New(tok, opts...)
	if h.cfg.WrapBackend != nil {
		backend = h.cfg.WrapBackend(backend)
	}
	return backend, nil
}

// ref resolves the board reference and builds the board service for a call.
func (h *server) ref(ctx context.Context, in boardRef) (svc *boardservice.Service, owner string, project int, err error) {
	owner, project, err = h.resolve(in.Owner, in.Project)
	if err != nil {
		return nil, "", 0, err
	}
	backend, err := h.newBackend(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	return boardservice.New(backend), owner, project, nil
}
