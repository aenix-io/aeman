package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// The BOARD an agent is standing on, as an argument — the same thing the HTTP
// door takes as a path segment (docs/design/view-scoped-api.md). It says what
// a create means, which gestures are on offer, and whether the card is one the
// board draws at all.

// boardStand is where a gesture is made from.
type boardStand struct {
	View string `json:"view,omitempty" jsonschema:"the BOARD you are acting from, when you are acting AS one: me (your own day board), team (the lead's grid — say team too), triage (the weeks), backlog (a team's parked work), project (the columns), personal (your own repository), or all when you have no board at all. Left out, nothing is assumed — you are not standing anywhere. Naming a board is how you ask to be held to it: a gesture that board does not draw, or one on a card it does not show, is then refused the way it would be refused of a person looking at that screen"`
	Team string `json:"team,omitempty" jsonschema:"the team whose board you are acting from (team and triage); the card's own team by default"`
	Day  string `json:"day,omitempty" jsonschema:"the day of the day board you are acting from; today by default"`
	User string `json:"user,omitempty" jsonschema:"whose me board you are acting from; yourself by default"`
}

// view is the board named, or the ESCAPE HATCH when the caller said nothing,
// and deliberately: an agent is not standing anywhere. It reaches a card by its uid — from a listing, from
// a title search — and making it work out which board draws that card would be
// friction with no safety in it, since the rules that matter (whose card it is,
// what the × may do to it) live in the service and answer every caller alike.
// Naming a board is how an agent ASKS to be held to one: say `view: me` and a
// card that is not on your board is refused, exactly as the SPA is refused.
func (s boardStand) view(fallback board.View) (board.View, error) {
	if s.View == "" {
		return fallback, nil
	}
	if !board.KnownView(s.View) {
		return "", fmt.Errorf("%w: %q", boardservice.ErrNoSuchView, s.View)
	}
	return board.View(s.View), nil
}

// gestureOn is the gate, and it asks exactly what the HTTP door asks: does
// this board draw the gesture, and does it draw this card. A person cannot
// press × on a card they cannot see; an agent that named a board should not be
// able to either, and one with no board to stand on says `view: all`.
func (h *server) gestureOn(ctx context.Context, svc *boardservice.Service, boardID string,
	stand boardStand, g boardservice.Gesture, uid string) error {
	view, err := stand.view(board.ViewAll)
	if err != nil {
		return err
	}
	if !boardservice.Offers(view, g) {
		return fmt.Errorf("the %s board has no %s — it is not a gesture it draws", view, g)
	}
	if view == board.ViewAll {
		return nil
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return err
	}
	sel := apiserver.Selector{Team: stand.Team, Day: stand.Day, User: stand.User}
	if sel.User == "" && (view == board.ViewMe || view == board.ViewPersonal) && h.cfg.ResolveLogin != nil {
		if login, err := h.cfg.ResolveLogin(ctx); err == nil {
			sel.User = login
		}
	}
	// The card's own team, but only where the LISTING names one (team,
	// triage, backlog): the Me, personal and Project boards list every team,
	// so filling it there would ask a stricter question than the board
	// answered — see internal/server/views.go, namesATeam.
	if sel.Team == "" && (view == board.ViewTeam || view == board.ViewTriage || view == board.ViewBacklog) {
		for _, c := range b.Cards {
			if c.ItemID == uid {
				sel.Team = c.Team
				break
			}
		}
	}
	// A board can be drawn from more than one listing — the Triage grid beside
	// its drawer, the Me day beside the personal column (board.Panes).
	for _, pane := range board.Panes(view) {
		paneSel := sel
		paneSel.View = string(pane)
		if apiserver.Drawn(b, paneSel, uid) {
			return nil
		}
	}
	return fmt.Errorf("that card is not on the %s board — act from the board that draws it, or say view: all", view)
}

// --- The gestures the boards draw ----------------------------------------------

type placeInput struct {
	cardRef
	boardStand
	Week string `json:"week" jsonschema:"the week's Monday as yyyy-mm-dd"`
}

func (h *server) placeCard(ctx context.Context, _ *mcp.CallToolRequest, in placeInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := h.gestureOn(ctx, svc, boardID, in.boardStand, boardservice.GesturePlace, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.Place(ctx, boardID, in.UID, in.Week); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

type standOnCard struct {
	cardRef
	boardStand
}

func (h *server) untriageCard(ctx context.Context, _ *mcp.CallToolRequest, in standOnCard) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := h.gestureOn(ctx, svc, boardID, in.boardStand, boardservice.GestureUntriage, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.Untriage(ctx, boardID, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

func (h *server) finishedEarlier(ctx context.Context, _ *mcp.CallToolRequest, in standOnCard) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := h.gestureOn(ctx, svc, boardID, in.boardStand, boardservice.GestureFinishedEarlier, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.FinishedEarlier(ctx, boardID, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

// --- The roster gestures the manage dialogs make -------------------------------

type reorderTeamsInput struct {
	boardRef
	Teams []string `json:"teams" jsonschema:"every team key in the order they are to stand in, the no-team group as an empty string"`
}

func (h *server) reorderTeams(ctx context.Context, _ *mcp.CallToolRequest, in reorderTeamsInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.ReorderTeams(ctx, boardID, in.Teams); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "reordered"}, nil
}

type reorderEpicsInput struct {
	boardRef
	Project string   `json:"project,omitempty" jsonschema:"the project whose columns these are; empty is the no-project bucket"`
	Epics   []string `json:"epics" jsonschema:"that project's column names in the order they are to stand in"`
}

func (h *server) reorderEpics(ctx context.Context, _ *mcp.CallToolRequest, in reorderEpicsInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.ReorderEpics(ctx, boardID, in.Project, in.Epics); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "reordered"}, nil
}

// deleteTeamInput names the team whose pointer goes.
type deleteTeamInput struct {
	boardRef
	Team string `json:"team" jsonschema:"the team key; empty is the no-team group, which cannot be deleted"`
}

func (h *server) deleteTeam(ctx context.Context, _ *mcp.CallToolRequest, in deleteTeamInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteTeam(ctx, boardID, in.Team); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted"}, nil
}

type sprintStateInput struct {
	boardRef
	Team     string `json:"team,omitempty" jsonschema:"the team whose pointer this is; empty is the no-team group"`
	Current  string `json:"current" jsonschema:"the day the team's current sprint began, yyyy-mm-dd"`
	Previous string `json:"previous,omitempty" jsonschema:"the day the one before it began; empty when there was none"`
}

func (h *server) setSprintState(ctx context.Context, _ *mcp.CallToolRequest, in sprintStateInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.SetSprintState(ctx, boardID, in.Team, in.Current, in.Previous); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "set"}, nil
}

// --- What the boards READ that no tool answered --------------------------------

func (h *server) listSprints(ctx context.Context, _ *mcp.CallToolRequest, in boardRef) (*mcp.CallToolResult, sprintList, error) {
	svc, boardID, err := h.ref(ctx, in)
	if err != nil {
		return nil, sprintList{}, err
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return nil, sprintList{}, err
	}
	return nil, sprintList{Kind: "SprintList", Items: apiserver.SprintResources(b)}, nil
}

// sprintList is the answer of list_sprints: every team's pointer, the shape
// GET /api/v1/sprints serves.
type sprintList struct {
	Kind  string             `json:"kind"`
	Items []apiserver.Sprint `json:"items"`
}

type dayLogsInput struct {
	boardRef
	UIDs []string `json:"uids" jsonschema:"the cards to read, at most 200 — the ones the board draws that day"`
	Day  string   `json:"day,omitempty" jsonschema:"the board day as yyyy-mm-dd; today by default"`
}

func (h *server) listDayLogs(ctx context.Context, _ *mcp.CallToolRequest, in dayLogsInput) (*mcp.CallToolResult, apiserver.DayLogList, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.DayLogList{}, err
	}
	if len(in.UIDs) > 200 {
		return nil, apiserver.DayLogList{}, fmt.Errorf("at most 200 cards at once, not %d", len(in.UIDs))
	}
	day := in.Day
	if day == "" {
		day = board.TodayIso()
	}
	logs, err := svc.DayLogs(ctx, boardID, in.UIDs, day)
	if err != nil {
		return nil, apiserver.DayLogList{}, err
	}
	per := make(map[string]apiserver.DayEntries, len(logs))
	for uid, l := range logs {
		per[uid] = apiserver.DayEntries{Notes: l.Notes, Events: l.Events}
	}
	return nil, apiserver.DayLogsFrom(day, per), nil
}
