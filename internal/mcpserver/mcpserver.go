// Package mcpserver exposes aeman's GitHub Projects v2 operations as a Model
// Context Protocol (MCP) server over stdio, sharing the ghprojects client with
// the HTTP API.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
}

// Serve runs an MCP server over stdio until ctx is cancelled or the client
// disconnects.
func Serve(ctx context.Context, s *mcp.Server) error {
	return s.Run(ctx, &mcp.StdioTransport{})
}

// server holds the configuration shared by all tool handlers.
type server struct {
	cfg Config
}

// New builds an MCP server with the aeman tool set.
func New(cfg Config) *mcp.Server {
	h := &server{cfg: cfg}
	s := mcp.NewServer(&mcp.Implementation{Name: "aeman", Version: cfg.Version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_board",
		Description: "Get the project board identity and its field metadata (columns, single-select options).",
	}, h.getBoard)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_cards",
		Description: "List all cards on the board with their zone, assignees, progress, status, day, notes and field values.",
	}, h.listCards)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_card",
		Description: "Create a draft-issue card on the board, optionally setting zone, assignee, day, status, team and progress.",
	}, h.createCard)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_card",
		Description: "Update a card: any of title, zone, progress, day, assignee, status, team, or arbitrary board fields.",
	}, h.updateCard)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_card",
		Description: "Reorder a card to sit after another card; an empty afterId moves it to the top of the board.",
	}, h.moveCard)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_card",
		Description: "Delete a card (project item) from the board.",
	}, h.deleteCard)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_note",
		Description: "Add a work note to a card: an issue comment, or a dated line in a draft issue's body.",
	}, h.addNote)

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

// client builds a ghprojects client with a freshly resolved token.
func (h *server) client(ctx context.Context) (*ghprojects.Client, error) {
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
	return ghprojects.New(tok, opts...), nil
}

// board resolves the reference, builds a client and loads the board.
func (h *server) board(ctx context.Context, owner string, project int) (*ghprojects.Client, *ghprojects.Board, error) {
	o, p, err := h.resolve(owner, project)
	if err != nil {
		return nil, nil, err
	}
	client, err := h.client(ctx)
	if err != nil {
		return nil, nil, err
	}
	board, err := client.LoadProjectBoard(ctx, o, p)
	if err != nil {
		return nil, nil, err
	}
	return client, board, nil
}
