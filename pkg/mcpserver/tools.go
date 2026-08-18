package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// boardRef is embedded in every tool input to select the target board.
type boardRef struct {
	Owner   string `json:"owner,omitempty" jsonschema:"GitHub org or user that owns the project; defaults to the server configuration"`
	Project int    `json:"project,omitempty" jsonschema:"GitHub Project number; defaults to the server configuration"`
}

// cardRef identifies a single card on a board.
type cardRef struct {
	boardRef
	UID string `json:"uid" jsonschema:"card uid (required)"`
}

// statusOutput acknowledges an action that leaves no single card to echo.
type statusOutput struct {
	Status string `json:"status"`
	UID    string `json:"uid,omitempty"`
}

// noteListOutput is the list_notes result: a card's notes as Note resources.
type noteListOutput struct {
	Kind  string           `json:"kind"`
	Items []apiserver.Note `json:"items"`
}

// findCard resolves a card on a loaded board by uid.
func findCard(b board.Board, uid string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ItemID == uid {
			return c, true
		}
	}
	return board.Card{}, false
}

// domainZone maps a semantic zone name (urgent, unplanned, planned,
// niceToHave) onto the domain key; "" passes through as "no zone".
func domainZone(name string) (board.ZoneKey, error) {
	if name == "" {
		return "", nil
	}
	z := apiserver.DomainZone(name)
	if z == "" {
		return "", fmt.Errorf("unknown zone %q (use urgent, unplanned, planned or niceToHave)", name)
	}
	return z, nil
}

// loadCard loads the board and resolves a card by uid.
func (h *server) loadCard(ctx context.Context, svc *boardservice.Service, owner string, project int, uid string) (board.Board, board.Card, error) {
	b, err := svc.Board(ctx, owner, project)
	if err != nil {
		return board.Board{}, board.Card{}, err
	}
	card, ok := findCard(b, uid)
	if !ok {
		return b, board.Card{}, fmt.Errorf("%w: %s", boardservice.ErrCardNotFound, uid)
	}
	return b, card, nil
}

// cardResource reloads the board and returns a card as its API resource — the
// standard mutation result, mirroring the way the UI re-renders what changed.
func (h *server) cardResource(ctx context.Context, svc *boardservice.Service, owner string, project int, uid string) (*mcp.CallToolResult, apiserver.Card, error) {
	b, card, err := h.loadCard(ctx, svc, owner, project, uid)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	return nil, apiserver.CardResource(b, card), nil
}

// --- Board / read ------------------------------------------------------------

func (h *server) getBoard(ctx context.Context, _ *mcp.CallToolRequest, in boardRef) (*mcp.CallToolResult, apiserver.BoardInfo, error) {
	svc, owner, project, err := h.ref(ctx, in)
	if err != nil {
		return nil, apiserver.BoardInfo{}, err
	}
	b, err := svc.Board(ctx, owner, project)
	if err != nil {
		return nil, apiserver.BoardInfo{}, err
	}
	return nil, apiserver.BoardResource(b), nil
}

// listCardsInput is a card LIST selector: an optional view plus plain field
// filters, mirroring GET /api/v1/cards.
type listCardsInput struct {
	boardRef
	View     string `json:"view,omitempty" jsonschema:"view to scope to: team, me or weekly; empty lists every card"`
	Team     string `json:"team,omitempty" jsonschema:"team key for the team/weekly views; on the me view a comma-separated set filters to those teams; empty is the no-team group / no filter"`
	Day      string `json:"day,omitempty" jsonschema:"viewed day as yyyy-mm-dd for the team/me views; defaults to today"`
	User     string `json:"user,omitempty" jsonschema:"GitHub login for the me view; empty is everyone"`
	Week     string `json:"week,omitempty" jsonschema:"plan week Monday as yyyy-mm-dd for the weekly view; defaults to the current week"`
	Stage    string `json:"stage,omitempty" jsonschema:"filter by stage: locked, review, recurrent or done"`
	Zone     string `json:"zone,omitempty" jsonschema:"filter by semantic zone: urgent, unplanned, planned or niceToHave"`
	Assignee string `json:"assignee,omitempty" jsonschema:"filter by assignee GitHub login"`
	Focus    bool   `json:"focus,omitempty" jsonschema:"keep only cards workable right now — drops done, on-review and locked; use this to show what can be picked up and worked on here and now"`
	Title    string `json:"title,omitempty" jsonschema:"case-insensitive substring filter on the card title — the cheap way to resolve a title someone mentioned to its uid: one call, a handful of rows"`
	Full     bool   `json:"full,omitempty" jsonschema:"include each card's full description in the listing. Default false: a listing is the board's row view — title, team, zone, assignees, progress, stage, dates, link refs — and card bodies come from get_card. Set only when genuinely reading many descriptions at once (bulk analysis), not to inspect one card"`
}

func (h *server) listCards(ctx context.Context, _ *mcp.CallToolRequest, in listCardsInput) (*mcp.CallToolResult, apiserver.CardList, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.CardList{}, err
	}
	sel := apiserver.Selector{View: in.View, Team: in.Team, Day: in.Day, User: in.User, Week: in.Week, Assignee: in.Assignee, Focus: in.Focus}
	switch sel.View {
	case "", "all", "team", "me", "weekly":
	default:
		return nil, apiserver.CardList{}, fmt.Errorf("unknown view %q (use all, team, me or weekly)", sel.View)
	}
	// An unspecified view defaults to the caller's personal Me board (their own
	// cards); Team is the lead view and view=all is the whole board. "Who am I"
	// is resolved server-side, so a Me request needs no user (explicit wins).
	if sel.View == "" {
		sel.View = "me"
	}
	if sel.View == "me" && sel.User == "" && h.cfg.ResolveLogin != nil {
		if login, err := h.cfg.ResolveLogin(ctx); err == nil {
			sel.User = login
		}
	}
	// MCP inputs cannot distinguish absent from empty, so an empty stage/zone
	// means "not filtering" (unlike the HTTP query, where ?stage= filters).
	if in.Stage != "" {
		sel.Stage = &in.Stage
	}
	if in.Zone != "" {
		if _, err := domainZone(in.Zone); err != nil {
			return nil, apiserver.CardList{}, err
		}
		sel.Zone = &in.Zone
	}
	// The listing IS the board's row view; full=true opts a genuine bulk
	// reader into card bodies. Agents read a board the way people do — rows
	// first, then the detail pane (get_card) for the one card they act on.
	if in.Full {
		sel.Fields = "full"
	}
	b, err := svc.Board(ctx, owner, project)
	if err != nil {
		return nil, apiserver.CardList{}, err
	}
	list := apiserver.ListCards(b, sel)
	if in.Title != "" {
		needle := strings.ToLower(in.Title)
		kept := list.Items[:0]
		for _, c := range list.Items {
			if strings.Contains(strings.ToLower(c.Spec.Title), needle) {
				kept = append(kept, c)
			}
		}
		list.Items = kept
	}
	return nil, list, nil
}

func (h *server) getCard(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, in.UID)
}

// --- Create / update / delete ------------------------------------------------

// createCardInput describes a card to create, mirroring POST /api/v1/cards.
type createCardInput struct {
	boardRef
	Title    string `json:"title" jsonschema:"card title (required)"`
	Team     string `json:"team,omitempty" jsonschema:"team the card joins; empty is the no-team group. MUST be one of the board's EXISTING team keys — read them from get_board metadata.teams and map the user's wording onto an existing key, across languages and case ('маркетинг', 'the marketing team' -> existing 'marketing'). A value not in that list silently CREATES a new team with its own sprint pointer — a heavyweight, unusual action: only pass a new key when the user explicitly asks to create a new team"`
	Zone     string `json:"zone,omitempty" jsonschema:"semantic zone: urgent, unplanned, planned or niceToHave"`
	Assignee string `json:"assignee,omitempty" jsonschema:"GitHub login to assign"`
	Start    string `json:"start,omitempty" jsonschema:"scheduled day as yyyy-mm-dd; defaults to end, else today. A FUTURE day parks the card off the board until that day arrives — it is not shown in the current sprint meanwhile. Sprints are daily and created as they start, so no sprint covers a future day yet: the card deliberately joins NO sprint and the carry-over that reaches its day adopts it. That is the intended way to schedule work ahead; leave the sprint field alone"`
	End      string `json:"end,omitempty" jsonschema:"end/due day as yyyy-mm-dd; defaults to start, else today"`
	Sprint   string `json:"sprint,omitempty" jsonschema:"sprint start day the card joins; defaults to the team's current sprint"`
	Plan     string `json:"plan,omitempty" jsonschema:"weekly-plan band, wed or fri: creates a plan card with no dates instead of a day card"`
	Week     string `json:"week,omitempty" jsonschema:"plan week Monday as yyyy-mm-dd; defaults to the current week (plan cards only)"`
	ReviewOf string `json:"reviewOf,omitempty" jsonschema:"uid of the card this one reviews"`
	// StartNewSprint controls sprint membership: omit for auto (join the team's
	// running sprint, else start one today), true to force a new sprint today,
	// false to force-join the current sprint.
	StartNewSprint *bool `json:"startNewSprint,omitempty" jsonschema:"force a new sprint (true) or join the current one (false); omit for auto"`
}

func (h *server) createCard(ctx context.Context, _ *mcp.CallToolRequest, in createCardInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	zone, err := domainZone(in.Zone)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	card, err := svc.CreateCard(ctx, owner, project, boardservice.CreateCardArgs{
		Team:           in.Team,
		Zone:           zone,
		Title:          in.Title,
		Assignee:       in.Assignee,
		Day:            in.End,
		Start:          in.Start,
		SprintStart:    in.Sprint,
		Plan:           board.PlanBand(in.Plan),
		Week:           in.Week,
		ReviewOf:       in.ReviewOf,
		StartNewSprint: in.StartNewSprint,
	})
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, card.ItemID)
}

// updateCardInput is a card patch, mirroring PATCH /api/v1/cards/{uid}: only
// the provided fields change; an explicit empty string clears a field.
type updateCardInput struct {
	cardRef
	Title       *string `json:"title,omitempty" jsonschema:"new title"`
	Description *string `json:"description,omitempty" jsonschema:"the card's shared free-form body (what the whole team sees; live-syncs onto the linked review card) — the right place for review or handoff context, and for reference links: include related open PRs and issues in free form as FULL URLs or owner/repo#123 shorthands (encouraged — links are extracted from anywhere in the text, surfaced on the card, and GitHub refs resolve to live titles/states; read them back with list_links); empty clears it"`
	Team        *string `json:"team,omitempty" jsonschema:"team to move to (joins its current sprint); empty is the no-team group. MUST be an EXISTING team key from get_board metadata.teams — map the user's wording onto an existing key, across languages and case; an unknown value silently CREATES a new team (heavyweight, unusual) — only on an explicit request for a brand-new team"`
	Zone        *string `json:"zone,omitempty" jsonschema:"semantic zone: urgent, unplanned, planned or niceToHave; empty clears it"`
	Assignee    *string `json:"assignee,omitempty" jsonschema:"GitHub login; empty unassigns"`
	Progress    *int    `json:"progress,omitempty" jsonschema:"readiness percentage 0..100"`
	Stage       *string `json:"stage,omitempty" jsonschema:"locked, review, recurrent or done; empty clears it"`
	Recurrence  *string `json:"recurrence,omitempty" jsonschema:"reseed cycle of a recurrent card: empty = every sprint (default), week or month = reseeded by carry-over only once that interval has elapsed since the card's sprint"`
	Start       *string `json:"start,omitempty" jsonschema:"scheduled day as yyyy-mm-dd: the card joins the sprint active on that day. A FUTURE day parks it off the board until that day arrives (this is how you schedule work ahead, and how the +1 day / +1 week buttons work). Sprints are daily and created as they start, so no sprint covers a future day yet: the card is left with NO sprint while it waits and the carry-over that reaches its day adopts it — expected, not a mis-scheduled card, and setting sprint by hand would only drag it back onto today's board. Empty clears the dates"`
	End         *string `json:"end,omitempty" jsonschema:"end/due day as yyyy-mm-dd; empty clears it"`
	Sprint      *string `json:"sprint,omitempty" jsonschema:"sprint start day the card belongs to; empty clears it"`
	PlanBand    *string `json:"planBand,omitempty" jsonschema:"weekly-plan band, wed or fri; empty clears it"`
	PlanWeek    *string `json:"planWeek,omitempty" jsonschema:"plan week Monday as yyyy-mm-dd; empty clears it"`
	ReviewOf    *string `json:"reviewOf,omitempty" jsonschema:"uid of the card this one reviews; empty breaks the link"`
	Parent      *string `json:"parent,omitempty" jsonschema:"uid of the card to group this one under as a subtask (one level deep; empty ungroups it back to a standalone card); a subtask keeps its own description/notes/log, feeds the parent's derived progress and rides with it through carry-over"`
}

func (h *server) updateCard(ctx context.Context, _ *mcp.CallToolRequest, in updateCardInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	_, card, err := h.loadCard(ctx, svc, owner, project, in.UID)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := h.applyCardPatch(ctx, svc, owner, project, card, in); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, in.UID)
}

// applyCardPatch runs the provided spec edits through the service methods, in
// a fixed order: plain fields first, then plan, then dates (an explicit sprint
// wins over the one the calendar derives from start).
func (h *server) applyCardPatch(ctx context.Context, svc *boardservice.Service, owner string, project int, card board.Card, in updateCardInput) error {
	if in.Title != nil {
		if err := svc.Rename(ctx, owner, project, in.UID, *in.Title); err != nil {
			return err
		}
	}
	if in.Description != nil {
		if err := svc.SetDescription(ctx, owner, project, in.UID, *in.Description); err != nil {
			return err
		}
	}
	if in.Team != nil {
		if err := svc.SetTeam(ctx, owner, project, in.UID, *in.Team, ""); err != nil {
			return err
		}
	}
	if in.Zone != nil {
		zone, err := domainZone(*in.Zone)
		if err != nil {
			return err
		}
		if err := svc.SetZone(ctx, owner, project, in.UID, zone); err != nil {
			return err
		}
	}
	if in.Assignee != nil {
		if err := svc.SetAssignee(ctx, owner, project, in.UID, *in.Assignee); err != nil {
			return err
		}
	}
	if in.Progress != nil {
		if err := svc.SetProgress(ctx, owner, project, in.UID, *in.Progress); err != nil {
			return err
		}
	}
	if in.Stage != nil {
		if err := svc.SetStage(ctx, owner, project, in.UID, board.StageKey(*in.Stage)); err != nil {
			return err
		}
	}
	if in.Recurrence != nil {
		if err := svc.SetRecurrence(ctx, owner, project, in.UID, *in.Recurrence); err != nil {
			return err
		}
	}
	if in.Parent != nil {
		if err := svc.SetParent(ctx, owner, project, in.UID, *in.Parent); err != nil {
			return err
		}
	}
	if in.ReviewOf != nil {
		if err := svc.SetReviewOf(ctx, owner, project, in.UID, *in.ReviewOf); err != nil {
			return err
		}
	}
	if in.PlanBand != nil {
		if err := svc.SetPlan(ctx, owner, project, in.UID, board.PlanBand(*in.PlanBand)); err != nil {
			return err
		}
	}
	if in.PlanWeek != nil {
		if err := svc.SetWeek(ctx, owner, project, in.UID, *in.PlanWeek); err != nil {
			return err
		}
	}
	return h.applyDatePatch(ctx, svc, owner, project, card, in)
}

// applyDatePatch applies the date part of a card patch: a start relocates the
// card with the calendar semantics (end kept unless also provided), an end
// alone moves the due day, and an explicit sprint overrides the membership.
func (h *server) applyDatePatch(ctx context.Context, svc *boardservice.Service, owner string, project int, card board.Card, in updateCardInput) error {
	switch {
	case in.Start != nil:
		end := card.Day
		if in.End != nil {
			end = *in.End
		}
		if err := svc.SetDates(ctx, owner, project, in.UID, *in.Start, end); err != nil {
			return err
		}
	case in.End != nil:
		if err := svc.SetDay(ctx, owner, project, in.UID, *in.End); err != nil {
			return err
		}
	}
	if in.Sprint != nil {
		return svc.SetSprintStart(ctx, owner, project, in.UID, *in.Sprint)
	}
	return nil
}

func (h *server) deleteCard(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteCard(ctx, owner, project, in.UID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted", UID: in.UID}, nil
}

// removeCardInput is the smart ×: the backend decides between demote, release
// and real delete based on where the card sits.
type removeCardInput struct {
	cardRef
	From string `json:"from,omitempty" jsonschema:"where the remove was pressed: grid (default) or plan"`
}

func (h *server) removeCard(ctx context.Context, _ *mcp.CallToolRequest, in removeCardInput) (*mcp.CallToolResult, statusOutput, error) {
	switch in.From {
	case "", "grid", "plan":
	default:
		return nil, statusOutput{}, fmt.Errorf("unknown from %q (use grid or plan)", in.From)
	}
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.Remove(ctx, owner, project, in.UID, in.From); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "removed", UID: in.UID}, nil
}

// --- Card actions --------------------------------------------------------------

// moveCardInput reorders a card on the board.
type moveCardInput struct {
	cardRef
	After  string `json:"after,omitempty" jsonschema:"uid to position after; empty moves the card to the top"`
	Before string `json:"before,omitempty" jsonschema:"uid to position right before (the server resolves the true anchor); wins over after"`
}

func (h *server) moveCard(ctx context.Context, _ *mcp.CallToolRequest, in moveCardInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	move := func() error {
		if in.Before != "" {
			return svc.MoveCardBefore(ctx, owner, project, in.UID, in.Before)
		}
		return svc.MoveCard(ctx, owner, project, in.UID, in.After)
	}
	if err := move(); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, in.UID)
}

// deferCardInput pushes a card's scheduled day ahead.
type deferCardInput struct {
	cardRef
	Days int `json:"days" jsonschema:"days to push ahead of today (or of the already-deferred slot): 1 for a day, 7 for a week (required)"`
}

func (h *server) deferCard(ctx context.Context, _ *mcp.CallToolRequest, in deferCardInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.Defer(ctx, owner, project, in.UID, in.Days); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, in.UID)
}

// setInProgress moves a card to the implicit In Progress status: the stage is
// cleared and the progress nudged into the [10, 90] band at the edges.
func (h *server) setInProgress(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.SetInProgress(ctx, owner, project, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, in.UID)
}

// sendToReviewInput sends a card to review for a reviewer.
type sendToReviewInput struct {
	cardRef
	Reviewer string `json:"reviewer" jsonschema:"GitHub login of the reviewer (required)"`
	Day      string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
}

func (h *server) sendToReview(ctx context.Context, _ *mcp.CallToolRequest, in sendToReviewInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	review, err := svc.SendToReview(ctx, owner, project, in.UID, in.Reviewer, in.Day, "")
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, review.ItemID)
}

func (h *server) removeReviewer(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.RemoveReviewer(ctx, owner, project, in.UID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", UID: in.UID}, nil
}

// takeIntoPlanInput takes a weekly-plan card into work.
type takeIntoPlanInput struct {
	cardRef
	Engineer string `json:"engineer" jsonschema:"GitHub login to assign (required)"`
	Zone     string `json:"zone,omitempty" jsonschema:"semantic zone: urgent, unplanned, planned or niceToHave; empty keeps the card's own zone"`
	Day      string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
}

func (h *server) takeIntoPlan(ctx context.Context, _ *mcp.CallToolRequest, in takeIntoPlanInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	zone, err := domainZone(in.Zone)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.TakeIntoPlan(ctx, owner, project, in.UID, in.Engineer, zone, in.Day); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, in.UID)
}

func (h *server) releaseFromPlan(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.ReleaseFromPlan(ctx, owner, project, in.UID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "released", UID: in.UID}, nil
}

// --- Sprint actions ----------------------------------------------------------

// carryOverInput names a team for the sprint carry-over.
type carryOverInput struct {
	boardRef
	Team   string `json:"team,omitempty" jsonschema:"EXISTING team key from get_board metadata.teams; empty is the no-team group. Carrying an unknown team quietly bootstraps a sprint pointer for it — never invent a key here"`
	DryRun bool   `json:"dryRun,omitempty" jsonschema:"report the would-be counts without changing anything"`
}

func (h *server) carryOver(ctx context.Context, _ *mcp.CallToolRequest, in carryOverInput) (*mcp.CallToolResult, boardservice.CarryReport, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, boardservice.CarryReport{}, err
	}
	rep, err := svc.CarryOver(ctx, owner, project, in.Team, in.DryRun)
	if err != nil {
		return nil, boardservice.CarryReport{}, err
	}
	return nil, rep, nil
}

// carryWeekInput names a team and target week for the weekly carry.
type carryWeekInput struct {
	boardRef
	Team   string `json:"team,omitempty" jsonschema:"EXISTING team key from get_board metadata.teams; empty is the no-team group. Carrying an unknown team quietly bootstraps a sprint pointer for it — never invent a key here"`
	Week   string `json:"week,omitempty" jsonschema:"target week Monday as yyyy-mm-dd; defaults to the current week"`
	DryRun bool   `json:"dryRun,omitempty" jsonschema:"report the would-be counts without changing anything"`
}

func (h *server) carryWeek(ctx context.Context, _ *mcp.CallToolRequest, in carryWeekInput) (*mcp.CallToolResult, boardservice.CarryReport, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, boardservice.CarryReport{}, err
	}
	rep, err := svc.CarryWeek(ctx, owner, project, in.Team, in.Week, in.DryRun)
	if err != nil {
		return nil, boardservice.CarryReport{}, err
	}
	return nil, rep, nil
}

// --- Notes ---------------------------------------------------------------------

// linkListOutput is the list_links result envelope.
type linkListOutput struct {
	Kind  string       `json:"kind"`
	Items []board.Link `json:"items"`
}

// listLog returns a card's unified activity feed: recorded events and work
// notes merged chronologically.
func (h *server) listLog(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, apiserver.LogList, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.LogList{}, err
	}
	_, card, err := h.loadCard(ctx, svc, owner, project, in.UID)
	if err != nil {
		return nil, apiserver.LogList{}, err
	}
	return nil, apiserver.CardLog(card), nil
}

// listLinks returns the URLs found in a card's description: GitHub issue/PR
// references first (resolved to their titles when possible), plain links after.
func (h *server) listLinks(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, linkListOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, linkListOutput{}, err
	}
	links, err := svc.CardLinks(ctx, owner, project, in.UID)
	if err != nil {
		return nil, linkListOutput{}, err
	}
	if links == nil {
		links = []board.Link{}
	}
	return nil, linkListOutput{Kind: "LinkList", Items: links}, nil
}

func (h *server) listNotes(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, noteListOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, noteListOutput{}, err
	}
	_, card, err := h.loadCard(ctx, svc, owner, project, in.UID)
	if err != nil {
		return nil, noteListOutput{}, err
	}
	return nil, noteListOutput{Kind: "NoteList", Items: apiserver.NoteResources(card)}, nil
}

// addNoteInput attaches a note to a card.
type addNoteInput struct {
	cardRef
	Text string `json:"text" jsonschema:"work-log line, private to the assignee's Me-view day panel — not team-visible context (use update_card description for that) (required)"`
}

func (h *server) addNote(ctx context.Context, _ *mcp.CallToolRequest, in addNoteInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.AddNote(ctx, owner, project, in.UID, in.Text); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", UID: in.UID}, nil
}

// editNoteInput rewrites one of a card's notes.
type editNoteInput struct {
	cardRef
	NoteID string `json:"noteId" jsonschema:"note id from list_notes (required)"`
	Text   string `json:"text" jsonschema:"new note text (required)"`
}

func (h *server) editNote(ctx context.Context, _ *mcp.CallToolRequest, in editNoteInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.EditNote(ctx, owner, project, in.UID, in.NoteID, in.Text); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", UID: in.UID}, nil
}

// deleteNoteInput removes one of a card's notes.
type deleteNoteInput struct {
	cardRef
	NoteID string `json:"noteId" jsonschema:"note id from list_notes (required)"`
}

func (h *server) deleteNote(ctx context.Context, _ *mcp.CallToolRequest, in deleteNoteInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteNote(ctx, owner, project, in.UID, in.NoteID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted", UID: in.UID}, nil
}
