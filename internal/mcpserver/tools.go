package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-org/aeman/internal/ghprojects"
)

// boardRef is embedded in every tool input to select the target board.
type boardRef struct {
	Owner   string `json:"owner,omitempty" jsonschema:"GitHub org or user that owns the project; defaults to the server configuration"`
	Project int    `json:"project,omitempty" jsonschema:"GitHub Project number; defaults to the server configuration"`
}

// boardMetaOutput is the get_board result (no cards).
type boardMetaOutput struct {
	ID     string                    `json:"id"`
	Number int                       `json:"number"`
	Title  string                    `json:"title"`
	URL    string                    `json:"url"`
	Owner  string                    `json:"owner"`
	Fields []ghprojects.ProjectField `json:"fields"`
}

func (h *server) getBoard(ctx context.Context, _ *mcp.CallToolRequest, in boardRef) (*mcp.CallToolResult, boardMetaOutput, error) {
	_, board, err := h.board(ctx, in.Owner, in.Project)
	if err != nil {
		return nil, boardMetaOutput{}, err
	}
	return nil, boardMetaOutput{
		ID:     board.ID,
		Number: board.Number,
		Title:  board.Title,
		URL:    board.URL,
		Owner:  board.Owner,
		Fields: board.Fields,
	}, nil
}

// cardsOutput is the list_cards result.
type cardsOutput struct {
	Cards []ghprojects.Card `json:"cards"`
}

func (h *server) listCards(ctx context.Context, _ *mcp.CallToolRequest, in boardRef) (*mcp.CallToolResult, cardsOutput, error) {
	_, board, err := h.board(ctx, in.Owner, in.Project)
	if err != nil {
		return nil, cardsOutput{}, err
	}
	return nil, cardsOutput{Cards: board.Cards}, nil
}

// createCardInput describes a card to create.
type createCardInput struct {
	boardRef
	Title    string   `json:"title" jsonschema:"card title (required)"`
	Zone     string   `json:"zone,omitempty" jsonschema:"Ford zone: gray, green, yellow or red"`
	Assignee string   `json:"assignee,omitempty" jsonschema:"GitHub login to assign"`
	Day      string   `json:"day,omitempty" jsonschema:"planned day as yyyy-mm-dd"`
	Status   string   `json:"status,omitempty" jsonschema:"status/stage single-select option name"`
	Team     string   `json:"team,omitempty" jsonschema:"team field value"`
	Progress *float64 `json:"progress,omitempty" jsonschema:"readiness percentage 0..100"`
	// StartNewSprint controls sprint membership for a team card: omit for auto
	// (join the team's running sprint, else start a new one today), true to force
	// a new sprint today, false to force-join the current sprint. When engaged,
	// startDate = sprintStart and day defaults to today.
	StartNewSprint *bool `json:"startNewSprint,omitempty" jsonschema:"force a new sprint (true) or join the current one (false); omit for auto"`
}

func (h *server) createCard(ctx context.Context, _ *mcp.CallToolRequest, in createCardInput) (*mcp.CallToolResult, ghprojects.Card, error) {
	client, board, err := h.board(ctx, in.Owner, in.Project)
	if err != nil {
		return nil, ghprojects.Card{}, err
	}
	card, err := client.CreateProjectCard(ctx, board, ghprojects.CreateCardInput{
		Title:          in.Title,
		Zone:           ghprojects.ZoneKey(in.Zone),
		Assignee:       in.Assignee,
		Day:            in.Day,
		Status:         in.Status,
		Team:           in.Team,
		Progress:       in.Progress,
		StartNewSprint: in.StartNewSprint,
	})
	if err != nil {
		return nil, ghprojects.Card{}, err
	}
	return nil, *card, nil
}

// updateCardInput describes a partial card update.
type updateCardInput struct {
	boardRef
	ItemID   string   `json:"itemId" jsonschema:"project item id of the card (required)"`
	Title    *string  `json:"title,omitempty" jsonschema:"new title"`
	Zone     *string  `json:"zone,omitempty" jsonschema:"Ford zone (gray/green/yellow/red); empty string clears it"`
	Progress *float64 `json:"progress,omitempty" jsonschema:"readiness percentage 0..100"`
	Day      *string  `json:"day,omitempty" jsonschema:"planned day yyyy-mm-dd; empty string clears it"`
	Assignee *string  `json:"assignee,omitempty" jsonschema:"GitHub login; empty string clears it"`
	Status   *string  `json:"status,omitempty" jsonschema:"status/stage option name; empty string clears it"`
	Team     *string  `json:"team,omitempty" jsonschema:"team value; empty string clears it"`
}

// statusOutput is a simple acknowledgement.
type statusOutput struct {
	Status string `json:"status"`
	ItemID string `json:"itemId,omitempty"`
}

func (h *server) updateCard(ctx context.Context, _ *mcp.CallToolRequest, in updateCardInput) (*mcp.CallToolResult, statusOutput, error) {
	client, board, err := h.board(ctx, in.Owner, in.Project)
	if err != nil {
		return nil, statusOutput{}, err
	}
	upd := ghprojects.UpdateCardInput{
		Title:    in.Title,
		Progress: in.Progress,
		Day:      in.Day,
		Assignee: in.Assignee,
		Status:   in.Status,
		Team:     in.Team,
	}
	if in.Zone != nil {
		z := ghprojects.ZoneKey(*in.Zone)
		upd.Zone = &z
	}
	if err := client.UpdateCard(ctx, board, in.ItemID, upd); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", ItemID: in.ItemID}, nil
}

// moveCardInput reorders a card.
type moveCardInput struct {
	boardRef
	ItemID  string `json:"itemId" jsonschema:"project item id to move (required)"`
	AfterID string `json:"afterId,omitempty" jsonschema:"item id to position after; empty moves to the top"`
}

func (h *server) moveCard(ctx context.Context, _ *mcp.CallToolRequest, in moveCardInput) (*mcp.CallToolResult, statusOutput, error) {
	client, board, err := h.board(ctx, in.Owner, in.Project)
	if err != nil {
		return nil, statusOutput{}, err
	}
	var after *string
	if in.AfterID != "" {
		after = &in.AfterID
	}
	if err := client.MoveProjectCard(ctx, board, in.ItemID, after); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", ItemID: in.ItemID}, nil
}

// itemInput identifies a single card.
type itemInput struct {
	boardRef
	ItemID string `json:"itemId" jsonschema:"project item id (required)"`
}

func (h *server) deleteCard(ctx context.Context, _ *mcp.CallToolRequest, in itemInput) (*mcp.CallToolResult, statusOutput, error) {
	client, board, err := h.board(ctx, in.Owner, in.Project)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := client.DeleteProjectCard(ctx, board, in.ItemID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", ItemID: in.ItemID}, nil
}

// addNoteInput attaches a note to a card.
type addNoteInput struct {
	boardRef
	ItemID string `json:"itemId" jsonschema:"project item id (required)"`
	Text   string `json:"text" jsonschema:"note text (required)"`
}

func (h *server) addNote(ctx context.Context, _ *mcp.CallToolRequest, in addNoteInput) (*mcp.CallToolResult, statusOutput, error) {
	client, board, err := h.board(ctx, in.Owner, in.Project)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := client.AddProjectNote(ctx, board, in.ItemID, in.Text); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", ItemID: in.ItemID}, nil
}
