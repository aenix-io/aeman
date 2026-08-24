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

// boardRef is embedded in every tool input to select the target board. It is
// addressed by owner+board: "project" is aeman's own planning entity (a group
// of epic columns on the Project board), not the GitHub board.
type boardRef struct {
	Owner string `json:"owner,omitempty" jsonschema:"GitHub org or user that owns the board; defaults to the server configuration"`
	Board int    `json:"board,omitempty" jsonschema:"GitHub Project number of the board; defaults to the server configuration. Not to be confused with an aeman project, which groups epic columns"`
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
	View     string `json:"view,omitempty" jsonschema:"view to scope to: team, me, weekly or project; empty lists every card"`
	Team     string `json:"team,omitempty" jsonschema:"team key for the team/weekly views; on the me view a comma-separated set filters to those teams; empty is the no-team group / no filter"`
	Day      string `json:"day,omitempty" jsonschema:"viewed day as yyyy-mm-dd for the team/me views; defaults to today"`
	User     string `json:"user,omitempty" jsonschema:"GitHub login for the me view; empty is everyone"`
	Week     string `json:"week,omitempty" jsonschema:"plan week Monday as yyyy-mm-dd for the weekly view; defaults to the current week"`
	Project  string `json:"project,omitempty" jsonschema:"aeman project to scope the project view to — one project's epic columns; empty is every project. Read the roster from get_board metadata.projects"`
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
	sel := apiserver.Selector{View: in.View, Team: in.Team, Day: in.Day, User: in.User, Week: in.Week,
		Project: in.Project, Assignee: in.Assignee, Focus: in.Focus}
	switch sel.View {
	case "", "all", "team", "me", "weekly", "project":
	default:
		return nil, apiserver.CardList{}, fmt.Errorf("unknown view %q (use all, team, me, weekly or project)", sel.View)
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
	Week     string `json:"week,omitempty" jsonschema:"WEEKLY-PLAN cards only (those created with plan=wed|fri): the plan week's Monday as yyyy-mm-dd, defaulting to the current week. A card filed under an epic ignores it — the slot's row is the week of its start date"`
	Epic     string `json:"epic,omitempty" jsonschema:"Project-board column to file the card under, together with project. MUST be an EXISTING column — read them from get_board metadata.epics; add_epic creates one when the user explicitly asks. The card's week is its row; start/end dates may span several weeks"`
	Project  string `json:"project,omitempty" jsonschema:"the project half of the column named by epic (columns are the (project, epic) pair — epic names repeat across projects)"`
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
		Epic:           in.Epic,
		Project:        in.Project,
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
	Epic        *string `json:"epic,omitempty" jsonschema:"Project-board column to file the card under; empty clears it. MUST be an EXISTING column from get_board metadata.epics — and columns are identified by the (project, epic) pair, so pass project too unless the card is already in the right project"`
	Project     *string `json:"project,omitempty" jsonschema:"the project half of the card's column (see epic). Epic names repeat across projects, so filing a card into another project's column needs both"`
	PlanBand    *string `json:"planBand,omitempty" jsonschema:"weekly-plan band, wed or fri; empty clears it"`
	PlanWeek    *string `json:"planWeek,omitempty" jsonschema:"WEEKLY-PLAN cards only: the plan week's Monday as yyyy-mm-dd, empty clears it. A Project-board slot (a card with an epic) refuses it — its week IS its start date's week, so move the dates instead and the row follows"`
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
	if in.Epic != nil || in.Project != nil {
		// A column is the (project, epic) pair. Naming only the epic keeps the
		// card's current project, which is what filing inside one project
		// means; crossing projects needs both.
		epic := card.Epic
		if in.Epic != nil {
			epic = *in.Epic
		}
		if err := svc.SetEpic(ctx, owner, project, in.UID, epic, in.Project); err != nil {
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

// epicInput names one Project-board column, and the project that owns it.
type epicInput struct {
	boardRef
	Name    string `json:"name" jsonschema:"the epic's name — the Project-board column header"`
	Project string `json:"project,omitempty" jsonschema:"the project the column belongs to (required: epic names repeat across projects, so the pair identifies a column). On set_epic_project this is the TARGET project, and empty detaches the column from every project"`
	From    string `json:"from,omitempty" jsonschema:"set_epic_project only: the project the column is in today"`
	To      string `json:"to,omitempty" jsonschema:"rename_epic only: the column's new name"`
}

func (h *server) addEpic(ctx context.Context, _ *mcp.CallToolRequest, in epicInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.AddEpic(ctx, owner, boardNum, in.Name, in.Project); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "added"}, nil
}

// setEpicProject moves a column to another project (empty detaches it).
func (h *server) setEpicProject(ctx context.Context, _ *mcp.CallToolRequest, in epicInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.SetEpicProject(ctx, owner, boardNum, in.From, in.Name, in.Project); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "updated"}, nil
}

// renameEpic renames a column in place, cards and all.
func (h *server) renameEpic(ctx context.Context, _ *mcp.CallToolRequest, in epicInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.RenameEpic(ctx, owner, boardNum, in.Project, in.Name, in.To); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "renamed"}, nil
}

// --- Processes ----------------------------------------------------------------

type listProcessesInput struct {
	boardRef
	Project string `json:"project,omitempty" jsonschema:"scope to one project's processes; empty is every project"`
}

func (h *server) listProcesses(ctx context.Context, _ *mcp.CallToolRequest, in listProcessesInput) (*mcp.CallToolResult, apiserver.ProcessList, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.ProcessList{}, err
	}
	b, err := svc.Board(ctx, owner, boardNum)
	if err != nil {
		return nil, apiserver.ProcessList{}, err
	}
	return nil, apiserver.ProcessesResource(b, in.Project), nil
}

type processInput struct {
	boardRef
	Name    string `json:"name" jsonschema:"the process's name"`
	Project string `json:"project,omitempty" jsonschema:"add_process: the project the process is part of (from get_board metadata.projects)"`
	To      string `json:"to,omitempty" jsonschema:"rename_process only: the new name"`
}

func (h *server) addProcess(ctx context.Context, _ *mcp.CallToolRequest, in processInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.AddProcess(ctx, owner, boardNum, in.Name, in.Project); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "added"}, nil
}

func (h *server) deleteProcess(ctx context.Context, _ *mcp.CallToolRequest, in processInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteProcess(ctx, owner, boardNum, in.Name); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted"}, nil
}

func (h *server) renameProcess(ctx context.Context, _ *mcp.CallToolRequest, in processInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.RenameProcess(ctx, owner, boardNum, in.Name, in.To); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "renamed"}, nil
}

type setProcessProjectInput struct {
	boardRef
	Process string `json:"process" jsonschema:"the process to move (required)"`
	Project string `json:"project" jsonschema:"the project it should belong to, from get_board metadata.projects; empty moves it to the no-project bucket"`
}

func (h *server) setProcessProject(ctx context.Context, _ *mcp.CallToolRequest, in setProcessProjectInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.SetProcessProject(ctx, owner, boardNum, in.Process, in.Project); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "moved"}, nil
}

type pauseProcessInput struct {
	boardRef
	Process string `json:"process" jsonschema:"the process to pause or resume (required)"`
	Paused  bool   `json:"paused" jsonschema:"true stops it filing iterations, false starts it again"`
}

func (h *server) setProcessPaused(ctx context.Context, _ *mcp.CallToolRequest, in pauseProcessInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.SetProcessPaused(ctx, owner, boardNum, in.Process, in.Paused); err != nil {
		return nil, statusOutput{}, err
	}
	status := "resumed"
	if in.Paused {
		status = "paused"
	}
	return nil, statusOutput{Status: status}, nil
}

// reopenCard undoes a done mark, restoring the pre-done progress from the
// card's own activity log (fallback: the In Progress nudge).
func (h *server) reopenCard(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.Reopen(ctx, owner, project, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, owner, project, in.UID)
}

type reorderProcessesInput struct {
	boardRef
	Processes []string `json:"processes" jsonschema:"every process name in the desired order (from list_processes)"`
}

func (h *server) reorderProcesses(ctx context.Context, _ *mcp.CallToolRequest, in reorderProcessesInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.ReorderProcesses(ctx, owner, boardNum, in.Processes); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "reordered"}, nil
}

type reorderProcessTasksInput struct {
	boardRef
	Process string   `json:"process" jsonschema:"the process whose task order this is (required)"`
	UIDs    []string `json:"uids" jsonschema:"the task uids in the desired order. A uid belonging to another process is ADOPTED into this one at its position — that is how moving a task between processes lands"`
}

func (h *server) reorderProcessTasks(ctx context.Context, _ *mcp.CallToolRequest, in reorderProcessTasksInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.ReorderProcessTasks(ctx, owner, boardNum, in.Process, in.UIDs); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "reordered"}, nil
}

type addTaskInput struct {
	boardRef
	Process     string `json:"process" jsonschema:"the process this task belongs to (required)"`
	Title       string `json:"title" jsonschema:"the title every iteration will be created with (required)"`
	Description string `json:"description,omitempty" jsonschema:"the body every iteration will be created with"`
	Recurrence  string `json:"recurrence" jsonschema:"the cycle: week, 2weeks, month or quarter (required). Counted on the calendar from start, not from when the last iteration closed"`
	Start       string `json:"start,omitempty" jsonschema:"the calendar anchor the cycle is counted from, yyyy-mm-dd; defaults to today"`
	Team        string `json:"team,omitempty" jsonschema:"the team whose weekly plan the iterations land in — MUST be an existing team key from get_board metadata.teams"`
	Assignee    string `json:"assignee,omitempty" jsonschema:"the standing owner (GitHub login); every iteration is assigned to them, the team lead may reassign"`
	Accumulate  bool   `json:"accumulate,omitempty" jsonschema:"spawn the next iteration even while the previous one is still open, so unpaid months pile up as separate cards. Default false: an open iteration simply goes overdue and the next one waits"`
}

func (h *server) addProcessTask(ctx context.Context, _ *mcp.CallToolRequest, in addTaskInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	tpl, err := svc.AddProcessTask(ctx, owner, boardNum, in.Process, boardservice.TaskArgs{
		Title: in.Title, Description: in.Description, Recurrence: in.Recurrence,
		Start: in.Start, Team: in.Team, Assignee: in.Assignee, Accumulate: in.Accumulate,
	})
	if err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "added", UID: tpl.ItemID}, nil
}

type updateTaskInput struct {
	boardRef
	UID         string  `json:"uid" jsonschema:"the task's uid, from list_processes (required)"`
	Title       *string `json:"title,omitempty" jsonschema:"new title for the NEXT iterations"`
	Description *string `json:"description,omitempty" jsonschema:"new body for the NEXT iterations; empty clears"`
	Recurrence  *string `json:"recurrence,omitempty" jsonschema:"new cycle: week, 2weeks, month or quarter"`
	Start       *string `json:"start,omitempty" jsonschema:"new calendar anchor, yyyy-mm-dd"`
	Team        *string `json:"team,omitempty" jsonschema:"new team (an existing team key)"`
	Assignee    *string `json:"assignee,omitempty" jsonschema:"new standing owner; empty clears"`
	Accumulate  *bool   `json:"accumulate,omitempty" jsonschema:"whether the next iteration spawns while the previous is open"`
}

func (h *server) updateProcessTask(ctx context.Context, _ *mcp.CallToolRequest, in updateTaskInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	err = svc.UpdateProcessTask(ctx, owner, boardNum, in.UID, boardservice.TaskPatch{
		Title: in.Title, Description: in.Description, Recurrence: in.Recurrence,
		Start: in.Start, Team: in.Team, Assignee: in.Assignee, Accumulate: in.Accumulate,
	})
	if err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "updated", UID: in.UID}, nil
}

type taskRef struct {
	boardRef
	UID string `json:"uid" jsonschema:"the task's uid, from list_processes (required)"`
}

func (h *server) deleteProcessTask(ctx context.Context, _ *mcp.CallToolRequest, in taskRef) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteProcessTask(ctx, owner, boardNum, in.UID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted", UID: in.UID}, nil
}

// deadlineInput names a week of the Project board (and, for a move, the week
// the line is dragged to).
type deadlineInput struct {
	boardRef
	Week    string `json:"week" jsonschema:"the deadline's week as yyyy-mm-dd; any day resolves to its Monday, since the line sits on a week row"`
	Project string `json:"project,omitempty" jsonschema:"whose deadline it is — a deadline belongs to a project and takes its colour. Empty means a line belonging to no project"`
	To      string `json:"to,omitempty" jsonschema:"move_deadline only: the week to drag the line to"`
}

func (h *server) addDeadline(ctx context.Context, _ *mcp.CallToolRequest, in deadlineInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.AddDeadline(ctx, owner, boardNum, in.Week, in.Project); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "added"}, nil
}

func (h *server) deleteDeadline(ctx context.Context, _ *mcp.CallToolRequest, in deadlineInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteDeadline(ctx, owner, boardNum, in.Week, in.Project); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted"}, nil
}

func (h *server) moveDeadline(ctx context.Context, _ *mcp.CallToolRequest, in deadlineInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.MoveDeadline(ctx, owner, boardNum, in.Project, in.Week, in.To); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "moved"}, nil
}

// projectInput names one project of the Project board.
type projectInput struct {
	boardRef
	Name string `json:"name" jsonschema:"the project's name — the chip on the Project board"`
	To   string `json:"to,omitempty" jsonschema:"rename_project only: the project's new name"`
}

// renameProject renames a project in place, columns and cards along with it.
func (h *server) renameProject(ctx context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.RenameProject(ctx, owner, boardNum, in.Name, in.To); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "renamed"}, nil
}

func (h *server) addProject(ctx context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.AddProject(ctx, owner, boardNum, in.Name); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "added"}, nil
}

func (h *server) deleteProject(ctx context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteProject(ctx, owner, boardNum, in.Name); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted"}, nil
}

// reorderProjectsInput carries the whole chip order, top to bottom.
type reorderProjectsInput struct {
	boardRef
	Projects []string `json:"projects" jsonschema:"every project name in the order the chips should appear"`
}

func (h *server) reorderProjects(ctx context.Context, _ *mcp.CallToolRequest, in reorderProjectsInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, boardNum, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.ReorderProjects(ctx, owner, boardNum, in.Projects); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "reordered"}, nil
}

func (h *server) deleteEpic(ctx context.Context, _ *mcp.CallToolRequest, in epicInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, owner, project, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteEpic(ctx, owner, project, in.Name, in.Project); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted"}, nil
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
