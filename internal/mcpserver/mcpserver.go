// Package mcpserver exposes aeman's board operations as a Model Context Protocol
// (MCP) server over stdio. Its tool set is a 1:1 projection of the /api/v1
// resource API (Board/Card/Note resources, actions, LIST selectors), calling
// the boardservice layer — the same logic behind the HTTP handlers — not
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

// mcpServer registers the tool set on a fresh MCP server. The tools are a 1:1
// projection of the /api/v1 resource API: reads return resources, mutations
// return the resulting Card (or a small status/report object).
func (h *server) mcpServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "aeman", Version: h.cfg.Version}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "get_board", Description: "Get the board identity and its team roster."}, h.getBoard)
	mcp.AddTool(s, &mcp.Tool{Name: "list_cards", Description: "List cards, optionally scoped to a view (team, me, weekly) and filtered by stage, semantic zone (urgent/unplanned/planned/niceToHave) or assignee."}, h.listCards)
	mcp.AddTool(s, &mcp.Tool{Name: "get_card", Description: "Get a single card by uid."}, h.getCard)
	mcp.AddTool(s, &mcp.Tool{Name: "create_card", Description: "Create a card that joins (or starts) its team's sprint — or a weekly-plan card when a plan band is given; zones are semantic (urgent/unplanned/planned/niceToHave)."}, h.createCard)
	mcp.AddTool(s, &mcp.Tool{Name: "update_card", Description: "Patch a card: only the provided fields change, an explicit empty string clears a field; zones are semantic (urgent/unplanned/planned/niceToHave)."}, h.updateCard)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_card", Description: "Delete a card for real, cascading to its linked review card."}, h.deleteCard)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_card", Description: "Smart-remove a card (the UI's x): demote it within its sprint or plan history, else delete it for real."}, h.removeCard)
	mcp.AddTool(s, &mcp.Tool{Name: "move_card", Description: "Reorder a card to sit after another; an empty after moves it to the top."}, h.moveCard)
	mcp.AddTool(s, &mcp.Tool{Name: "defer_card", Description: "Push a card's scheduled day N days ahead of today (or of its already-deferred slot)."}, h.deferCard)
	mcp.AddTool(s, &mcp.Tool{Name: "send_to_review", Description: "Create a linked review card for a reviewer and put the card on review; returns the review card."}, h.sendToReview)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_reviewer", Description: "Delete a card's linked review card."}, h.removeReviewer)
	mcp.AddTool(s, &mcp.Tool{Name: "take_into_plan", Description: "Take a weekly-plan card into work: assign an engineer and join the team's sprint."}, h.takeIntoPlan)
	mcp.AddTool(s, &mcp.Tool{Name: "release_from_plan", Description: "Release a card from the weekly plan (a pure plan card demotes to its previous week, else is deleted)."}, h.releaseFromPlan)
	mcp.AddTool(s, &mcp.Tool{Name: "carry_over", Description: "Advance a team's sprint to today and carry its unfinished cards forward (dryRun reports the counts)."}, h.carryOver)
	mcp.AddTool(s, &mcp.Tool{Name: "carry_week", Description: "Pull a team's unfinished plan cards from earlier weeks into the target week (dryRun reports the counts)."}, h.carryWeek)
	mcp.AddTool(s, &mcp.Tool{Name: "list_notes", Description: "List a card's work notes."}, h.listNotes)
	mcp.AddTool(s, &mcp.Tool{Name: "add_note", Description: "Append a work note to a card."}, h.addNote)
	mcp.AddTool(s, &mcp.Tool{Name: "edit_note", Description: "Rewrite one of a card's work notes."}, h.editNote)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_note", Description: "Delete one of a card's work notes."}, h.deleteNote)
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
