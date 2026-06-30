package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-org/aeman/internal/board"
	"github.com/aenix-org/aeman/internal/boardservice"
)

// boardRef is embedded in every tool input to select the target board.
type boardRef struct {
	Owner   string `json:"owner,omitempty" jsonschema:"GitHub org or user that owns the project; defaults to the server configuration"`
	Project int    `json:"project,omitempty" jsonschema:"GitHub Project number; defaults to the server configuration"`
}

// itemInput identifies a single card on a board.
type itemInput struct {
	boardRef
	ItemID string `json:"itemId" jsonschema:"project item id of the card (required)"`
}

// boardMetaOutput is the get_board result: board identity, fields and sprint
// pointers (no cards).
type boardMetaOutput struct {
	ID           string                       `json:"id"`
	Number       int                          `json:"number"`
	Owner        string                       `json:"owner"`
	Fields       []board.ProjectField         `json:"fields"`
	SprintStates map[string]board.SprintState `json:"sprintStates"`
}

// cardsOutput is a list of cards returned by a view or a carry action.
type cardsOutput struct {
	Cards []board.Card `json:"cards"`
}

// statusOutput acknowledges an action that leaves no single card to echo.
type statusOutput struct {
	Status string `json:"status"`
	ItemID string `json:"itemId,omitempty"`
}

// cardResult reloads and returns the card resulting from an action.
func (h *server) cardResult(ctx context.Context, svc *boardservice.Service, owner string, project int, id string) (*mcp.CallToolResult, board.Card, error) {
	card, err := svc.Card(ctx, owner, project, id)
	if err != nil {
		return nil, board.Card{}, err
	}
	return nil, card, nil
}

func (h *server) getBoard(ctx context.Context, _ *mcp.CallToolRequest, in boardRef) (*mcp.CallToolResult, boardMetaOutput, error) {
	svc, owner, project, err := h.ref(ctx, in)
	if err != nil {
		return nil, boardMetaOutput{}, err
	}
	b, err := svc.Board(ctx, owner, project)
	if err != nil {
		return nil, boardMetaOutput{}, err
	}
	return nil, boardMetaOutput{ID: b.ID, Number: b.Number, Owner: b.Owner, Fields: b.Fields, SprintStates: b.SprintStates}, nil
}

// teamViewInput selects a team and day for the Team grid view.
type teamViewInput struct {
	boardRef
	Team string `json:"team,omitempty" jsonschema:"team key; empty is the no-team group"`
	Day  string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
}

func (h *server) teamView(ctx context.Context, _ *mcp.CallToolRequest, in teamViewInput) (*mcp.CallToolResult, cardsOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, cardsOutput{}, err
	}
	cards, err := svc.TeamView(ctx, owner, project, in.Team, in.Day)
	if err != nil {
		return nil, cardsOutput{}, err
	}
	return nil, cardsOutput{Cards: cards}, nil
}

// meViewInput selects a user and day for the personal day view.
type meViewInput struct {
	boardRef
	User string `json:"user,omitempty" jsonschema:"GitHub login; empty is everyone"`
	Day  string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
}

func (h *server) meView(ctx context.Context, _ *mcp.CallToolRequest, in meViewInput) (*mcp.CallToolResult, cardsOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, cardsOutput{}, err
	}
	cards, err := svc.MeView(ctx, owner, project, in.User, in.Day)
	if err != nil {
		return nil, cardsOutput{}, err
	}
	return nil, cardsOutput{Cards: cards}, nil
}

// weeklyPlanInput selects a team and week for the weekly plan.
type weeklyPlanInput struct {
	boardRef
	Team string `json:"team,omitempty" jsonschema:"team key; empty is the no-team group"`
	Week string `json:"week,omitempty" jsonschema:"week Monday as yyyy-mm-dd; defaults to the current week"`
}

func (h *server) weeklyPlan(ctx context.Context, _ *mcp.CallToolRequest, in weeklyPlanInput) (*mcp.CallToolResult, board.WeeklyBands, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.WeeklyBands{}, err
	}
	bands, err := svc.WeeklyPlan(ctx, owner, project, in.Team, in.Week)
	if err != nil {
		return nil, board.WeeklyBands{}, err
	}
	return nil, bands, nil
}

// createCardInput describes a card to create.
type createCardInput struct {
	boardRef
	Team     string `json:"team,omitempty" jsonschema:"team the card joins; empty is the no-team group"`
	Zone     string `json:"zone,omitempty" jsonschema:"Ford zone: gray, green, yellow or red"`
	Title    string `json:"title" jsonschema:"card title (required)"`
	Assignee string `json:"assignee,omitempty" jsonschema:"GitHub login to assign"`
	Day      string `json:"day,omitempty" jsonschema:"create day as yyyy-mm-dd; defaults to today"`
	// StartNewSprint controls sprint membership: omit for auto (join the team's
	// running sprint, else start one today), true to force a new sprint today,
	// false to force-join the current sprint.
	StartNewSprint *bool `json:"startNewSprint,omitempty" jsonschema:"force a new sprint (true) or join the current one (false); omit for auto"`
}

func (h *server) createCard(ctx context.Context, _ *mcp.CallToolRequest, in createCardInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	card, err := svc.CreateCard(ctx, owner, project, boardservice.CreateCardArgs{
		Team:           in.Team,
		Zone:           board.ZoneKey(in.Zone),
		Title:          in.Title,
		Assignee:       in.Assignee,
		Day:            in.Day,
		StartNewSprint: in.StartNewSprint,
	})
	if err != nil {
		return nil, board.Card{}, err
	}
	return nil, card, nil
}

// teamInput names a team for a sprint-wide action.
type teamInput struct {
	boardRef
	Team string `json:"team,omitempty" jsonschema:"team key; empty is the no-team group"`
}

func (h *server) carryOver(ctx context.Context, _ *mcp.CallToolRequest, in teamInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.CarryOver(ctx, owner, project, in.Team); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok"}, nil
}

// carryWeekInput names a team and target week for the weekly carry.
type carryWeekInput struct {
	boardRef
	Team string `json:"team,omitempty" jsonschema:"team key; empty is the no-team group"`
	Week string `json:"week,omitempty" jsonschema:"target week Monday as yyyy-mm-dd; defaults to the current week"`
}

func (h *server) carryWeek(ctx context.Context, _ *mcp.CallToolRequest, in carryWeekInput) (*mcp.CallToolResult, cardsOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, cardsOutput{}, err
	}
	carried, err := svc.CarryWeek(ctx, owner, project, in.Team, in.Week)
	if err != nil {
		return nil, cardsOutput{}, err
	}
	return nil, cardsOutput{Cards: carried}, nil
}

// setStageInput moves a card to a stage.
type setStageInput struct {
	itemInput
	Stage string `json:"stage,omitempty" jsonschema:"locked, review, done, or empty to clear"`
}

func (h *server) setStage(ctx context.Context, _ *mcp.CallToolRequest, in setStageInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.SetStage(ctx, owner, project, in.ItemID, board.StageKey(in.Stage)); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}

func (h *server) setInProgress(ctx context.Context, _ *mcp.CallToolRequest, in itemInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.SetInProgress(ctx, owner, project, in.ItemID); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}

// setProgressInput sets a card's readiness percentage.
type setProgressInput struct {
	itemInput
	Progress int `json:"progress" jsonschema:"readiness percentage 0..100"`
}

func (h *server) setProgress(ctx context.Context, _ *mcp.CallToolRequest, in setProgressInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.SetProgress(ctx, owner, project, in.ItemID, in.Progress); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}

// sendToReviewInput sends a card to review for a reviewer.
type sendToReviewInput struct {
	itemInput
	Reviewer string `json:"reviewer,omitempty" jsonschema:"GitHub login of the reviewer"`
	Day      string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
}

func (h *server) sendToReview(ctx context.Context, _ *mcp.CallToolRequest, in sendToReviewInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	review, err := svc.SendToReview(ctx, owner, project, in.ItemID, in.Reviewer, in.Day)
	if err != nil {
		return nil, board.Card{}, err
	}
	return nil, review, nil
}

// reassignReviewerInput points a card's review at another reviewer.
type reassignReviewerInput struct {
	itemInput
	Reviewer string `json:"reviewer,omitempty" jsonschema:"GitHub login of the new reviewer"`
	Day      string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
}

func (h *server) reassignReviewer(ctx context.Context, _ *mcp.CallToolRequest, in reassignReviewerInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.ReassignReviewer(ctx, owner, project, in.ItemID, in.Reviewer, in.Day); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}

func (h *server) removeReviewer(ctx context.Context, _ *mcp.CallToolRequest, in itemInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.RemoveReviewer(ctx, owner, project, in.ItemID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", ItemID: in.ItemID}, nil
}

// setAssigneeInput sets or clears a card's assignee.
type setAssigneeInput struct {
	itemInput
	Login string `json:"login,omitempty" jsonschema:"GitHub login; empty unassigns"`
}

func (h *server) setAssignee(ctx context.Context, _ *mcp.CallToolRequest, in setAssigneeInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.SetAssignee(ctx, owner, project, in.ItemID, in.Login); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}

// setTeamInput moves a card to a team.
type setTeamInput struct {
	itemInput
	Team string `json:"team,omitempty" jsonschema:"team key; empty is the no-team group"`
	Day  string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
}

func (h *server) setTeam(ctx context.Context, _ *mcp.CallToolRequest, in setTeamInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.SetTeam(ctx, owner, project, in.ItemID, in.Team, in.Day); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}

// takeIntoPlanInput takes a weekly-plan card into work.
type takeIntoPlanInput struct {
	itemInput
	Engineer string `json:"engineer,omitempty" jsonschema:"GitHub login to assign; empty unassigns"`
	Zone     string `json:"zone,omitempty" jsonschema:"Ford zone; empty keeps the card's own zone"`
	Day      string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
}

func (h *server) takeIntoPlan(ctx context.Context, _ *mcp.CallToolRequest, in takeIntoPlanInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.TakeIntoPlan(ctx, owner, project, in.ItemID, in.Engineer, board.ZoneKey(in.Zone), in.Day); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}

func (h *server) releaseFromPlan(ctx context.Context, _ *mcp.CallToolRequest, in itemInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.ReleaseFromPlan(ctx, owner, project, in.ItemID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", ItemID: in.ItemID}, nil
}

// moveCardInput reorders a card.
type moveCardInput struct {
	itemInput
	AfterID string `json:"afterId,omitempty" jsonschema:"item id to position after; empty moves to the top"`
}

func (h *server) moveCard(ctx context.Context, _ *mcp.CallToolRequest, in moveCardInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.MoveCard(ctx, owner, project, in.ItemID, in.AfterID); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}

func (h *server) deleteCard(ctx context.Context, _ *mcp.CallToolRequest, in itemInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteCard(ctx, owner, project, in.ItemID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", ItemID: in.ItemID}, nil
}

// addNoteInput attaches a note to a card.
type addNoteInput struct {
	itemInput
	Text string `json:"text" jsonschema:"note text (required)"`
}

func (h *server) addNote(ctx context.Context, _ *mcp.CallToolRequest, in addNoteInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.AddNote(ctx, owner, project, in.ItemID, in.Text); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", ItemID: in.ItemID}, nil
}

// renameCardInput changes a card's title.
type renameCardInput struct {
	itemInput
	Title string `json:"title" jsonschema:"new title (required)"`
}

func (h *server) renameCard(ctx context.Context, _ *mcp.CallToolRequest, in renameCardInput) (*mcp.CallToolResult, board.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, board.Card{}, err
	}
	if err := svc.Rename(ctx, owner, project, in.ItemID, in.Title); err != nil {
		return nil, board.Card{}, err
	}
	return h.cardResult(ctx, svc, owner, project, in.ItemID)
}
