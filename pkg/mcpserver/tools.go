package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// boardRef is embedded in every tool input to select the target board — the
// name of its primary repository.
type boardRef struct {
	Board string `json:"board,omitempty" jsonschema:"the board's name (its primary repository); defaults to the server configuration and is ignored when the server pins its board"`
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
func (h *server) loadCard(ctx context.Context, svc *boardservice.Service, boardID string, uid string) (board.Board, board.Card, error) {
	b, err := svc.Board(ctx, boardID)
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
func (h *server) cardResource(ctx context.Context, svc *boardservice.Service, boardID string, uid string) (*mcp.CallToolResult, apiserver.Card, error) {
	b, card, err := h.loadCard(ctx, svc, boardID, uid)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	return nil, apiserver.CardResource(b, card), nil
}

// --- Board / read ------------------------------------------------------------

func (h *server) getBoard(ctx context.Context, _ *mcp.CallToolRequest, in boardRef) (*mcp.CallToolResult, apiserver.BoardInfo, error) {
	svc, boardID, err := h.ref(ctx, in)
	if err != nil {
		return nil, apiserver.BoardInfo{}, err
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return nil, apiserver.BoardInfo{}, err
	}
	return nil, apiserver.BoardResource(b), nil
}

// listCardsInput is a card LIST selector: an optional view plus plain field
// filters, mirroring GET /api/v1/views/{view}/cards.
type listCardsInput struct {
	boardRef
	View     string `json:"view,omitempty" jsonschema:"the board to list: team, me, backlog or strategy — or all for every card (roadmap, the Strategy board's old name, is still accepted). Empty is your own Me board, which is where everyone works. backlog lists each team's PARKED open tasks — accepted and not taken into work — in the order the backlog is read (the reviews nobody has taken first, then the Strategy board's order — tasks under a plan item by their place in the tree, loose tasks after); they are on no day board until somebody gives them a day, a sprint or a person"`
	Team     string `json:"team,omitempty" jsonschema:"team key for the team/backlog views (a comma-separated set lists several). A task is placed by the team it STANDS UNDER: its own, else the nearest ancestor's (a task with no team under an epic of portal is portal's); on the me and all views a comma-separated set filters to those teams' TASKS (a goal, an initiative or an epic is never listed under a team there); on the strategy view a comma-separated set keeps a task whose EFFECTIVE team (its own, else the nearest ancestor's) is one of them and a planning card that has that team or such a task under it, each with its ancestors. A set is comma-separated and the no-team group is its empty member: 'alpha,' is alpha and the no-team group, ',' the no-team group alone. Left empty it means, per view: on team, the no-team group's grid; on backlog, me, all and strategy, no team filter (every team's)"`
	Day      string `json:"day,omitempty" jsonschema:"viewed day as yyyy-mm-dd for the team/me views; defaults to today"`
	User     string `json:"user,omitempty" jsonschema:"GitHub login for the me view; empty is everyone"`
	Stage    string `json:"stage,omitempty" jsonschema:"filter by stage: locked, review, recurrent, refuse or done"`
	Zone     string `json:"zone,omitempty" jsonschema:"filter by semantic zone: urgent, unplanned, planned or niceToHave"`
	Assignee string `json:"assignee,omitempty" jsonschema:"filter by assignee GitHub login"`
	Focus    bool   `json:"focus,omitempty" jsonschema:"keep only cards workable right now — drops done, on-review and locked; use this to show what can be picked up and worked on here and now"`
	Title    string `json:"title,omitempty" jsonschema:"case-insensitive substring filter on the card title — the cheap way to resolve a title someone mentioned to its uid: one call, a handful of rows"`
	Full     bool   `json:"full,omitempty" jsonschema:"include each card's full description in the listing. Default false: a listing is the board's row view — title, team, zone, assignees, progress, stage, dates, link refs — and card bodies come from get_card. Set only when genuinely reading many descriptions at once (bulk analysis), not to inspect one card"`
	Reviews  bool   `json:"reviews,omitempty" jsonschema:"include each card's linked REVIEW card beside it (the me and team views). A review is somebody's work too — it counts in the reviewer's load — and without this it is on no listing you asked for"`
	Customer string `json:"customer,omitempty" jsonschema:"a comma-separated set of customer ids (get_board metadata.customers), matched against a card's EFFECTIVE customers (its own plus every ancestor's). On the strategy view: a task whose effective customers include one, a planning card that does or has such a task under it, each listed with its ancestors. On the team, backlog, all and me views: the cards whose effective customers include one, AND the team set"`
	Owner    string `json:"owner,omitempty" jsonschema:"on the strategy view, a comma-separated set of logins — the Strategy board's one person filter: the goals, initiatives and epics whose OWN owner is one of them (never inherited) and the tasks and subtasks one of them is assigned to, each with its ancestors — the plan items one answers for and the work one does. team narrows the Strategy board by EFFECTIVE team (own, else the nearest ancestor's) the same way; different filters are AND"`
	Status   string `json:"status,omitempty" jsonschema:"on the strategy view, a comma-separated set of statuses — in-progress, backlog, done, locked, no-items — to list only that work: a task by its own work status (done when complete, backlog when parked, in-progress when in work — a date or a sprint, review/locked/refused stages included; a task nobody has placed reads as backlog) and by locked while a dependency holds it (status.locked, own or inherited) and it is not done; a subtask by its parent task's, a review card by the task it reviews; a goal, an initiative or an epic by itself for locked (status.locked, its roll-up not done) and no-items (nothing under it), and otherwise when any task under it matches; each with its ancestors. in-progress shows the work in hand; in-progress,backlog,locked,no-items (every value but done) hides exactly the finished work. Several values are OR; with team, customer and owner, AND"`
	Snapshot bool   `json:"snapshot,omitempty" jsonschema:"read a PAST day as it stood rather than today's cards filtered by that day: every field is the day's own, so a card finished since reads unfinished and one created since is absent. Needs day set to a past day; the board's history answers it, and a day it no longer reaches is an error rather than a wrong answer"`
}

func (h *server) listCards(ctx context.Context, _ *mcp.CallToolRequest, in listCardsInput) (*mcp.CallToolResult, apiserver.CardList, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.CardList{}, err
	}
	sel := apiserver.Selector{View: in.View, Team: in.Team, Day: in.Day, User: in.User,
		Assignee: in.Assignee, Focus: in.Focus,
		IncludeReviews: in.Reviews, Customer: in.Customer, Owner: in.Owner, Status: in.Status}
	if err := apiserver.CheckStatus(sel.Status); err != nil {
		return nil, apiserver.CardList{}, err
	}
	if sel.View != "" && !board.KnownView(sel.View) {
		return nil, apiserver.CardList{}, fmt.Errorf("%w: %q (use all, team, me, backlog or strategy)",
			boardservice.ErrNoSuchView, sel.View)
	}
	// An unspecified view defaults to the caller's own Me board (their own
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
	// A past day can be read AS IT STOOD. The day is built by the board
	// service, which is what the HTTP API reads it through too: an agent and
	// a person asking about the same day must not get different boards.
	day := ""
	if in.Snapshot && board.HasRecords(sel.View) {
		day = sel.Day
	}
	b, records, at, err := svc.BoardOfDay(ctx, boardID, day)
	if err != nil {
		return nil, apiserver.CardList{}, err
	}
	asOf := ""
	if !at.IsZero() {
		// A record gives back what the × took off that day, for the teams it
		// is a record of, and marks the cards that are one.
		asOf = at.Format(time.RFC3339)
		sel.LeftOn, sel.RecordCards = sel.Day, records
	}
	list := apiserver.ListCards(b, sel)
	apiserver.MarkRecords(&list, records, asOf)
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
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

// --- Create / update / delete ------------------------------------------------

// createCardInput describes a card to create, mirroring POST /api/v1/cards —
// plus a description, which that door does not take yet.
type createCardInput struct {
	boardRef
	Title       string `json:"title" jsonschema:"card title (required)"`
	Team        string `json:"team,omitempty" jsonschema:"team the card joins; empty is the no-team group. MUST be one of the board's EXISTING team keys — read them from get_board metadata.teams and map the user's wording onto an existing key, across languages and case ('маркетинг', 'the marketing team' -> existing 'marketing'). A value not in that list silently CREATES a new team with its own sprint pointer — a heavyweight, unusual action: only pass a new key when the user explicitly asks to create a new team"`
	Zone        string `json:"zone,omitempty" jsonschema:"semantic zone: urgent, unplanned, planned or niceToHave. The Me board (the default) adds work as UNPLANNED and refuses the other three: something that came up today is unplanned by definition, and the other bands are the plan. To file work in a band of the plan — an urgent card, planned work for the week — use view=team and name the assignee (that is what a person does on the team's grid, including for themselves); a subtask takes its parent's band whatever board it is typed on"`
	Size        string `json:"size,omitempty" jsonschema:"what the card weighs: S (up to ~2h), M (half a day to a day), L (2–5 days) or XL (a week or more); summed as 1/2/4/8 points against each person's weekly capacity. Leave empty when you do not know — an unsized card is honest, a guessed one is not"`
	Assignee    string `json:"assignee,omitempty" jsonschema:"GitHub login to assign"`
	Start       string `json:"start,omitempty" jsonschema:"scheduled day as yyyy-mm-dd; defaults to end, else today. A FUTURE day parks the card off the board until that day arrives — it is not shown in the current sprint meanwhile. Sprints are daily and created as they start, so no sprint covers a future day yet: the card deliberately joins NO sprint and the carry-over that reaches its day adopts it. That is the intended way to schedule work ahead; leave the sprint field alone"`
	End         string `json:"end,omitempty" jsonschema:"end/due day as yyyy-mm-dd; defaults to start, else today"`
	Sprint      string `json:"sprint,omitempty" jsonschema:"sprint start day the card joins; defaults to the team's current sprint"`
	Type        string `json:"type,omitempty" jsonschema:"makes a PLANNING card — goal, initiative or epic — the levels above the day boards (view=strategy, where it is required; every other board refuses it). A planning card takes a title, dates (its bar on the Strategy board) and a parent of a HIGHER level (initiative under goal; epic under initiative or goal), and may take a team (the team that carries it out), an owner (the one person who answers for it) and customers — no assignee (the people are on the tasks under it; it lives in the primary repository), no band, no size, and none of a task's day-board state: no sprint, backlog or review. Its status is derived from the tasks under it and never written. Leave empty for a task"`
	ReviewOf    string `json:"reviewOf,omitempty" jsonschema:"uid of the card this one reviews"`
	Description string `json:"description,omitempty" jsonschema:"the card's shared free-form body, at create instead of a separate update_card call (what the whole team sees) — and the right place for reference links: include related open PRs and issues in free form as FULL URLs or owner/repo#123 shorthands (links are extracted from anywhere in the text, surfaced on the card, and GitHub refs resolve to live titles/states; read them back with list_links). When the title is a bare GitHub issue/PR URL, that link is kept: appended to an explicit body that does not already name it. With reviewOf the body is SHARED with the card being reviewed, so a create that would overwrite a body standing there is refused — the review is made without one, or both are changed with update_card"`
	// StartNewSprint controls sprint membership: omit for auto (join the team's
	// running sprint, else start one today), true to force a new sprint today,
	// false to force-join the current sprint.
	StartNewSprint *bool `json:"startNewSprint,omitempty" jsonschema:"force a new sprint (true) or join the current one (false); omit for auto"`
	// View is the BOARD the card is typed into, which is what makes it the
	// kind of card it is; each board refuses the fields it does not own.
	View      string   `json:"view,omitempty" jsonschema:"the BOARD you are typing the card into, which is what it MEANS: me (the DEFAULT — the card is yours, scheduled for the day, in your team's sprint; it stands on your own board and in your column of the team's grid), team (the lead's grid, where PLANNING is done: the card lands in the Unassigned column unless you name an assignee, and any band is allowed — this is the board for work in the plan, whether it is for somebody else or for the person you are acting for, and the only one that takes urgent/planned/niceToHave), backlog (parked in the team's backlog, last: accepted and not taken into work, on no day), strategy (a PLANNING card — a goal, an initiative or an epic: pass type, and a parent of a higher level if it has one — or a TASK under one: no type, pass parent naming a goal, an initiative or an epic; born with no team, no day and no sprint, it is parked in the no-team group's backlog and stands on the Strategy board until somebody takes it into work). A board refuses what it does not own — a parked card typed into a day, a type named on the Me board — and says which field it was"`
	Owner     string   `json:"owner,omitempty" jsonschema:"on the strategy view, for a goal, an initiative or an epic: the person who answers for it (not inherited, puts nothing on a day board); refused on a task"`
	Customers []string `json:"customers,omitempty" jsonschema:"customer ids from get_board metadata.customers the new card is for"`
	After     string   `json:"after,omitempty" jsonschema:"on the backlog, team and me views: uid of the card the new one is placed right AFTER, in the same create — how a person inserts a card between two in a backlog or in a Team/Me cell (the cell's person and band are the create's own fields). A list is read top-down. One of after and before, or neither (the card lands last)"`
	Before    string   `json:"before,omitempty" jsonschema:"on the backlog, team and me views: uid of the card the new one is placed right BEFORE — the top of a list is before its first card. One of after and before, or neither"`
	Parent    string   `json:"parent,omitempty" jsonschema:"uid of the card this one sits under — the ONE link of the hierarchy, and what it means is the parent's type. A TASK parent groups this card as a subtask (one level deep; a subtask rides its parent and is never parked, so view=backlog refuses a task parent). A goal, an initiative or an epic as parent files the card in the hierarchy: a task may sit under any of the three, an epic under an initiative or a goal, an initiative under a goal — the parent must be of a higher level (parentLevel) and in the card's own repository (crossDomain) — and nothing else about the card changes"`
}

func (h *server) createCard(ctx context.Context, _ *mcp.CallToolRequest, in createCardInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	zone, err := domainZone(in.Zone)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	size, ok := board.ParseSize(in.Size)
	if !ok {
		return nil, apiserver.Card{}, fmt.Errorf("%w: %q (use S, M, L, XL or empty)", boardservice.ErrUnknownSize, in.Size)
	}
	// The body is validated BEFORE anything is created: refused after, the
	// card would already exist and a retry would leave a twin.
	desc := board.StripEventLines(in.Description)
	if utf8.RuneCountInString(desc) > boardservice.MaxDescriptionLen {
		return nil, apiserver.Card{}, boardservice.ErrDescriptionTooLong
	}
	// A create-by-URL title files its link as the body (withLinkDescription);
	// an explicit body takes the field over, and the link joins it — it is the
	// card's only trace of the item the card came from. Settled here, before
	// the review guard, so that guard judges the body that will be stored.
	// Dropped rather than truncated when the pair would break the cap: the
	// body is what the caller asked to store, whole.
	if ref, ok := board.ParseGitHubRef(strings.TrimSpace(in.Title)); ok && desc != "" && !mentionsLink(desc, ref) &&
		utf8.RuneCountInString(desc)+utf8.RuneCountInString(ref.URL)+2 <= boardservice.MaxDescriptionLen {
		desc += "\n\n" + ref.URL
	}
	// WHO the agent is acting for, when the transport has not already said:
	// the Me board files the card on them, and the activity is theirs. Both
	// transports stamp the actor themselves (stdio's login middleware, the
	// HTTP session's UserID); this is the same seam the listing uses to
	// answer "my board", so the two cannot disagree.
	if board.ActorFrom(ctx) == "" && h.cfg.ResolveLogin != nil {
		if login, err := h.cfg.ResolveLogin(ctx); err == nil && login != "" {
			ctx = boardservice.WithActor(ctx, login)
		}
	}
	// A review card's body is SHARED with the card it reviews (L4), so a body
	// passed here lands on that card as well — and what stands there is not
	// this create's to overwrite. The caller named the review, not the work
	// being reviewed, and that card may carry notes nobody meant to lose.
	// Read once the actor is known, so the read is logged as theirs.
	if in.ReviewOf != "" && desc != "" {
		_, reviewed, lerr := h.loadCard(ctx, svc, boardID, in.ReviewOf)
		if lerr != nil {
			// Closed rather than open: a guard over somebody's notes that
			// cannot read them refuses the write, it does not wave it past.
			return nil, apiserver.Card{}, lerr
		}
		if body := reviewed.Description; body != "" && body != desc {
			return nil, apiserver.Card{}, fmt.Errorf(
				"card %s already has a description, and a review card shares the body of the card it reviews — creating this review with one would overwrite it: create the review without a description, or change the shared body with update_card",
				in.ReviewOf)
		}
	}
	// No board named means the caller's OWN: an agent is acting for somebody,
	// and a card filed for them stands on their board and in their column of
	// the team's grid. The team's own add-box (an unassigned card in the
	// Unassigned column) is a different thing and is asked for by name — the
	// listing defaults the same way, to the caller's Me board.
	view, err := (boardStand{View: in.View}).view(board.ViewMe)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	card, err := svc.CreateInView(ctx, boardID, view, boardservice.CreateCardArgs{
		Team:           in.Team,
		Zone:           zone,
		Size:           size,
		Title:          in.Title,
		Assignee:       in.Assignee,
		Day:            in.End,
		Start:          in.Start,
		SprintStart:    in.Sprint,
		ReviewOf:       in.ReviewOf,
		StartNewSprint: in.StartNewSprint,
		Parent:         in.Parent,
		Type:           in.Type,
		After:          in.After,
		Before:         in.Before,
		Owner:          in.Owner,
		Customers:      in.Customers,
	})
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if desc != "" {
		if err := svc.SetDescription(ctx, boardID, card.ItemID, desc); err != nil {
			// The card EXISTS. A bare error reads as "nothing was created" and
			// the retry is a second create — so name the card that is already
			// there and the door that still writes its body.
			return nil, apiserver.Card{}, fmt.Errorf(
				"card %s was created, but its description was not written (%w) — set it with update_card, not another create_card",
				card.ItemID, err)
		}
	}
	return h.cardResource(ctx, svc, boardID, card.ItemID)
}

// mentionsLink reports whether a body already names the item a create-by-URL
// title points at. Matched by the item, never by URL text: the same issue is
// written owner/repo#7 and .../issues/7, a pull request answers to both
// /pull/7 and /issues/7, an owner and a repository answer to any case, and a
// plain substring test additionally reads .../issues/1 as present in a body
// that only mentions .../issues/13.
func mentionsLink(description string, ref board.Link) bool {
	for _, l := range board.ExtractLinks(description) {
		if l.IsGitHubRef() && l.Number == ref.Number &&
			strings.EqualFold(l.Owner, ref.Owner) && strings.EqualFold(l.Repo, ref.Repo) {
			return true
		}
	}
	return false
}

// updateCardInput is a card patch, mirroring PATCH /api/v1/cards/{uid}: only
// the provided fields change; an explicit empty string clears a field.
type updateCardInput struct {
	cardRef
	Title       *string   `json:"title,omitempty" jsonschema:"new title"`
	Description *string   `json:"description,omitempty" jsonschema:"the card's shared free-form body (what the whole team sees; live-syncs onto the linked review card) — the right place for review or handoff context, and for reference links: include related open PRs and issues in free form as FULL URLs or owner/repo#123 shorthands (encouraged — links are extracted from anywhere in the text, surfaced on the card, and GitHub refs resolve to live titles/states; read them back with list_links); empty clears it"`
	Owner       *string   `json:"owner,omitempty" jsonschema:"on a goal, an initiative or an epic: the ONE person who answers for it — breaks it down, triages what lands under it, keeps its status honest. Not inherited by the levels below and it puts nothing on anybody's day boards. Refused on a task (whose people are its assignees); empty takes it off"`
	Customers   *[]string `json:"customers,omitempty" jsonschema:"customer IDS from get_board metadata.customers that this card itself is for, replacing what it named; an empty list takes them all off. The cards below inherit them (status.effectiveCustomers is a card's own plus every ancestor's). An id the roster lacks is refused — add_customer first when the user names a new customer"`
	DependsOn   *[]string `json:"dependsOn,omitempty" jsonschema:"uids of the cards this card WAITS FOR, the whole list, replacing what it named; an empty list clears it. While one of them — or one a card above it waits for — is not done (a task complete, a goal/initiative/epic whose tasks are all complete), the card is LOCKED: status.locked, with status.blockedBy naming the blockers (via = the ancestor an inherited wait comes from) and status.hiddenBlockers counting ones you cannot read. The lock is derived, never written, and does not stop the card being planned or taken. The cards waited for are for this card's customers too (their status.dependentCustomers). A target must be another card on the board, not a review card, not this card's ancestor or descendant (dependencyTarget), and must not close a cycle (dependencyCycle)"`
	Team        *string   `json:"team,omitempty" jsonschema:"team to move to (joins its current sprint); empty is the no-team group. On a goal, an initiative or an epic it is the team that carries the plan item out: it joins no sprint, places nothing, and the cards below with no team of their own take it as their effective team; empty takes it off. MUST be an EXISTING team key from get_board metadata.teams — map the user's wording onto an existing key, across languages and case; an unknown value silently CREATES a new team (heavyweight, unusual) — only on an explicit request for a brand-new team"`
	Zone        *string   `json:"zone,omitempty" jsonschema:"semantic zone: urgent, unplanned, planned or niceToHave; empty clears it"`
	Size        *string   `json:"size,omitempty" jsonschema:"what the card weighs: S (up to ~2h, one action in one place), M (half a day to a day, one deliverable in one component), L (2–5 days, several parts or people) or XL (a week or more, epic-shaped); empty clears it. The board sums sizes as 1/2/4/8 points against each person's weekly capacity, so size the work the card asks for, consistently — not the topic's importance"`
	Assignee    *string   `json:"assignee,omitempty" jsonschema:"GitHub login; empty unassigns"`
	Progress    *int      `json:"progress,omitempty" jsonschema:"readiness percentage 0..100"`
	Stage       *string   `json:"stage,omitempty" jsonschema:"locked, review, recurrent, refuse or done; empty clears it. REVIEW on a task with no review card makes one with NOBODY on it, parked in the task's team's backlog (the 'needs a reviewer' item) — to name the reviewer use send_to_review, or own_card with a person on that review card. REFUSE is the answer of the person the card is on — 'I am not doing this' — and only they may set it (403 otherwise); it leaves the card on the board for their lead to answer. Clearing it is the answer, and anyone may. A REVIEW card itself takes neither review nor recurrent: a review of a review is a chain nothing draws, and the asking is one-off by nature."`
	Recurrence  *string   `json:"recurrence,omitempty" jsonschema:"reseed cycle of a recurrent card: empty = every sprint (default), week or month = reseeded by carry-over only once that interval has elapsed since the card's sprint"`
	Start       *string   `json:"start,omitempty" jsonschema:"scheduled day as yyyy-mm-dd: the card joins the sprint active on that day. A FUTURE day parks it off the board until that day arrives (this is how you schedule work ahead, and how the +1 day / +1 week buttons work). Sprints are daily and created as they start, so no sprint covers a future day yet: the card is left with NO sprint while it waits and the carry-over that reaches its day adopts it — expected, not a mis-scheduled card, and setting sprint by hand would only drag it back onto today's board. Empty clears the dates"`
	End         *string   `json:"end,omitempty" jsonschema:"end/due day as yyyy-mm-dd; empty clears it"`
	Sprint      *string   `json:"sprint,omitempty" jsonschema:"sprint start day the card belongs to; empty clears it"`
	Parked      *bool     `json:"parked,omitempty" jsonschema:"true parks the task in its TEAM's backlog: the ranked list of work the team has accepted and not taken into work. Every team has one and nothing declares it. Parking clears the person, the sprint and the dates, takes the card's subtasks off the board with it, withdraws any review of it and puts it at the top of the backlog. A task leaves its backlog by being TAKEN INTO WORK — a start date, a sprint, or own_card with a person — and each of those unparks it on its own; false alone on a parked task with no date and no sprint is refused. A REVIEW card and a SUBTASK are refused: neither has a place of its own to be parked in. A task has no week"`
	ReviewOf    *string   `json:"reviewOf,omitempty" jsonschema:"uid of the card this one reviews; empty breaks the link"`
	Parent      *string   `json:"parent,omitempty" jsonschema:"uid of the card this one sits under; empty takes it out. Under a TASK it is a subtask (one level deep): it keeps its own description/notes/log, feeds the parent's derived progress and rides with it — team, person, sprint — through carry-over. Under a goal, an initiative or an epic it is filed in the hierarchy and NOTHING else changes; the parent must be of a higher level than the card (an epic may not sit under an epic) and in the card's own repository"`
}

func (h *server) updateCard(ctx context.Context, _ *mcp.CallToolRequest, in updateCardInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	_, card, err := h.loadCard(ctx, svc, boardID, in.UID)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := h.applyCardPatch(ctx, svc, boardID, card, in); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

// applyCardPatch runs the provided spec edits through the service methods, in
// a fixed order: plain fields first, then plan, then dates (an explicit sprint
// wins over the one the calendar derives from start).
func (h *server) applyCardPatch(ctx context.Context, svc *boardservice.Service, boardID string, card board.Card, in updateCardInput) error {
	if in.Title != nil {
		if err := svc.Rename(ctx, boardID, in.UID, *in.Title); err != nil {
			return err
		}
	}
	if in.Description != nil {
		if err := svc.SetDescription(ctx, boardID, in.UID, *in.Description); err != nil {
			return err
		}
	}
	if in.Team != nil {
		if err := svc.SetTeam(ctx, boardID, in.UID, *in.Team, ""); err != nil {
			return err
		}
	}
	if in.Owner != nil {
		if err := svc.SetOwner(ctx, boardID, in.UID, *in.Owner); err != nil {
			return err
		}
	}
	if in.Customers != nil {
		if err := svc.SetCustomers(ctx, boardID, in.UID, *in.Customers); err != nil {
			return err
		}
	}
	if in.DependsOn != nil {
		if err := svc.SetDependsOn(ctx, boardID, in.UID, *in.DependsOn); err != nil {
			return err
		}
	}
	if in.Zone != nil {
		zone, err := domainZone(*in.Zone)
		if err != nil {
			return err
		}
		if err := svc.SetZone(ctx, boardID, in.UID, zone); err != nil {
			return err
		}
	}
	if in.Size != nil {
		size, ok := board.ParseSize(*in.Size)
		if !ok {
			return fmt.Errorf("%w: %q (use S, M, L, XL or empty)", boardservice.ErrUnknownSize, *in.Size)
		}
		if err := svc.SetSize(ctx, boardID, in.UID, size); err != nil {
			return err
		}
	}
	if in.Assignee != nil {
		if err := svc.SetAssignee(ctx, boardID, in.UID, *in.Assignee); err != nil {
			return err
		}
	}
	if in.Progress != nil {
		if err := svc.SetProgress(ctx, boardID, in.UID, *in.Progress); err != nil {
			return err
		}
	}
	if in.Stage != nil {
		if err := svc.SetStage(ctx, boardID, in.UID, board.StageKey(*in.Stage)); err != nil {
			return err
		}
	}
	if in.Recurrence != nil {
		if err := svc.SetRecurrence(ctx, boardID, in.UID, *in.Recurrence); err != nil {
			return err
		}
	}
	if in.Parent != nil {
		if err := svc.SetParent(ctx, boardID, in.UID, *in.Parent); err != nil {
			return err
		}
	}
	if in.ReviewOf != nil {
		if err := svc.SetReviewOf(ctx, boardID, in.UID, *in.ReviewOf); err != nil {
			return err
		}
	}
	if in.Parked != nil {
		if err := svc.SetBacklog(ctx, boardID, in.UID, *in.Parked); err != nil {
			return err
		}
	}
	return h.applyDatePatch(ctx, svc, boardID, card, in)
}

// applyDatePatch applies the date part of a card patch: a start relocates the
// card with the calendar semantics (end kept unless also provided), an end
// alone moves the due day, and an explicit sprint overrides the membership.
func (h *server) applyDatePatch(ctx context.Context, svc *boardservice.Service, boardID string, card board.Card, in updateCardInput) error {
	switch {
	case in.Start != nil:
		end := card.Day
		if in.End != nil {
			end = *in.End
		}
		if err := svc.SetDates(ctx, boardID, in.UID, *in.Start, end); err != nil {
			return err
		}
	case in.End != nil:
		if err := svc.SetDay(ctx, boardID, in.UID, *in.End); err != nil {
			return err
		}
	}
	if in.Sprint != nil {
		return svc.SetSprintStart(ctx, boardID, in.UID, *in.Sprint)
	}
	return nil
}

// reopenCard undoes a done mark, restoring the pre-done progress from the
// card's own activity log (fallback: the In Progress nudge).
func (h *server) reopenCard(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.Reopen(ctx, boardID, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

// teamInput names one team of the board and, for a rename, its new name.
type teamInput struct {
	boardRef
	Team string `json:"team" jsonschema:"the team's current name (its chip on the boards)"`
	To   string `json:"to" jsonschema:"the team's new name; a name another team has — in any of the board's repositories — is refused"`
}

// renameTeam renames a team in place, its cards and process tasks along
// with it.
func (h *server) renameTeam(ctx context.Context, _ *mcp.CallToolRequest, in teamInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.RenameTeam(ctx, boardID, in.Team, in.To); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "renamed"}, nil
}

func (h *server) deleteCard(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteCard(ctx, boardID, in.UID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted", UID: in.UID}, nil
}

// removeCardInput is the smart ×. Omitting the intent leaves the gesture to
// decide, as it always did; naming one is how the UI passes on the answer the
// person gave its dialog.
type removeCardInput struct {
	cardRef
	boardStand
	Intent string `json:"intent,omitempty" jsonschema:"what the x is to do: unassign (take the card out of work and put it back in its team's backlog — parked, no person, no day, no sprint) or off-board (take it away, with the subtasks that were pieces of it). Omit to let the gesture decide, which is what it did before there was a choice. On a REVIEW card unassign is back to backlog — the reviewer taken off, the review waiting at the top of its task's team backlog with nobody on it, the task still on review — and off-board or no intent withdraws the review: the task goes back to In progress and the review card goes"`
}

func (h *server) removeCard(ctx context.Context, _ *mcp.CallToolRequest, in removeCardInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	intent := boardservice.RemoveIntent(in.Intent)
	switch intent {
	case boardservice.RemoveAuto, boardservice.Unassign, boardservice.OffBoard:
	default:
		return nil, statusOutput{}, fmt.Errorf("unknown intent %q (use unassign, off-board, or leave it out)", in.Intent)
	}
	// The × is a BOARD's gesture: which one it was made from decides whether
	// this card is yours to remove at all (views.go), and the service is told
	// the same board — the Me board's × is the narrow one.
	view, err := (boardStand{View: in.View}).view(board.ViewAll)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := h.gestureOn(ctx, svc, boardID, in.boardStand, boardservice.GestureRemove, in.UID); err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.Remove(ctx, boardID, in.UID, view, intent); err != nil {
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
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	move := func() error {
		if in.Before != "" {
			return svc.MoveCardBefore(ctx, boardID, in.UID, in.Before)
		}
		return svc.MoveCard(ctx, boardID, in.UID, in.After)
	}
	if err := move(); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

// deferCardInput pushes a card's scheduled day ahead.
type deferCardInput struct {
	cardRef
	Days int `json:"days" jsonschema:"days to push ahead of today (or of the already-deferred slot): 1 for a day, 7 for a week (required)"`
}

func (h *server) deferCard(ctx context.Context, _ *mcp.CallToolRequest, in deferCardInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.Defer(ctx, boardID, in.UID, in.Days); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

// setInProgress moves a card to the implicit In Progress status: the stage is
// cleared and the progress nudged into the [10, 90] band at the edges.
func (h *server) setInProgress(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	if err := svc.SetInProgress(ctx, boardID, in.UID); err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, in.UID)
}

// sendToReviewInput sends a card to review for a reviewer.
type sendToReviewInput struct {
	cardRef
	Reviewer string `json:"reviewer" jsonschema:"GitHub login of the reviewer (required)"`
	Day      string `json:"day,omitempty" jsonschema:"day as yyyy-mm-dd; defaults to today"`
	Zone     string `json:"zone,omitempty" jsonschema:"the band the review card lands in for the reviewer: urgent, unplanned (the default), planned or niceToHave. Unplanned is right for nearly everything — for the reviewer it is work that turned up during their day; the original's band says where the WORK stands, not where the asking belongs"`
}

func (h *server) sendToReview(ctx context.Context, _ *mcp.CallToolRequest, in sendToReviewInput) (*mcp.CallToolResult, apiserver.Card, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	zone, err := domainZone(in.Zone)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	review, err := svc.SendToReview(ctx, boardID, in.UID, in.Reviewer, in.Day, zone)
	if err != nil {
		return nil, apiserver.Card{}, err
	}
	return h.cardResource(ctx, svc, boardID, review.ItemID)
}

func (h *server) removeReviewer(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.RemoveReviewer(ctx, boardID, in.UID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "ok", UID: in.UID}, nil
}

// --- Sprint actions ----------------------------------------------------------

// carryOverInput names a team for the sprint carry-over.
type carryOverInput struct {
	boardRef
	Team   string `json:"team,omitempty" jsonschema:"EXISTING team key from get_board metadata.teams; empty is the no-team group. Carrying an unknown team quietly bootstraps a sprint pointer for it — never invent a key here"`
	DryRun bool   `json:"dryRun,omitempty" jsonschema:"report the would-be counts without changing anything"`
}

func (h *server) carryOver(ctx context.Context, _ *mcp.CallToolRequest, in carryOverInput) (*mcp.CallToolResult, boardservice.CarryReport, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, boardservice.CarryReport{}, err
	}
	rep, err := svc.CarryOver(ctx, boardID, in.Team, in.DryRun)
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
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.LogList{}, err
	}
	card, events, truncated, err := svc.Log(ctx, boardID, in.UID)
	if err != nil {
		return nil, apiserver.LogList{}, err
	}
	return nil, apiserver.CardLogFrom(card, events, truncated), nil
}

// listLinks returns the URLs found in a card's description: GitHub issue/PR
// references first (resolved to their titles when possible), plain links after.
func (h *server) listLinks(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, linkListOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, linkListOutput{}, err
	}
	links, err := svc.CardLinks(ctx, boardID, in.UID)
	if err != nil {
		return nil, linkListOutput{}, err
	}
	if links == nil {
		links = []board.Link{}
	}
	return nil, linkListOutput{Kind: "LinkList", Items: links}, nil
}

func (h *server) listNotes(ctx context.Context, _ *mcp.CallToolRequest, in cardRef) (*mcp.CallToolResult, noteListOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, noteListOutput{}, err
	}
	_, card, err := h.loadCard(ctx, svc, boardID, in.UID)
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
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.AddNote(ctx, boardID, in.UID, in.Text); err != nil {
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
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.EditNote(ctx, boardID, in.UID, in.NoteID, in.Text); err != nil {
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
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteNote(ctx, boardID, in.UID, in.NoteID); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted", UID: in.UID}, nil
}

// --- People ------------------------------------------------------------------

type setCapacityInput struct {
	boardRef
	Login    string `json:"login" jsonschema:"the person's login (required)"`
	Capacity int    `json:"capacity" jsonschema:"points a week, 1..999; 0 takes a set number back, leaving the person with no number at all"`
}

type setTeamCapacityInput struct {
	boardRef
	Team     string `json:"team" jsonschema:"the team key; empty is the no-team group"`
	Capacity int    `json:"capacity" jsonschema:"points a week, 1..999; 0 takes a set number back, leaving the team with no number at all"`
}

// The answer is the TEAM's sprint resource, which is where a team's capacity
// lives: the board resource has no per-team number on it, so answering with
// one told the caller everything except what it had just set.
func (h *server) setTeamCapacity(ctx context.Context, _ *mcp.CallToolRequest, in setTeamCapacityInput) (*mcp.CallToolResult, apiserver.Sprint, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.Sprint{}, err
	}
	if err := svc.SetTeamPoints(ctx, boardID, in.Team, in.Capacity); err != nil {
		return nil, apiserver.Sprint{}, err
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return nil, apiserver.Sprint{}, err
	}
	return nil, apiserver.SprintResourceOf(b, in.Team), nil
}

func (h *server) setCapacity(ctx context.Context, _ *mcp.CallToolRequest, in setCapacityInput) (*mcp.CallToolResult, apiserver.BoardInfo, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.BoardInfo{}, err
	}
	if err := svc.SetPersonCapacity(ctx, boardID, in.Login, in.Capacity); err != nil {
		return nil, apiserver.BoardInfo{}, err
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return nil, apiserver.BoardInfo{}, err
	}
	return nil, apiserver.BoardResource(b), nil
}

// ---- the customer roster (docs/design/ownership.md, O5, O9) ----

type customerNameInput struct {
	boardRef
	Name string `json:"name" jsonschema:"the customer's name, as people say it"`
}

func (h *server) addCustomer(ctx context.Context, _ *mcp.CallToolRequest, in customerNameInput) (*mcp.CallToolResult, apiserver.CustomerRef, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, apiserver.CustomerRef{}, err
	}
	cu, err := svc.AddCustomer(ctx, boardID, in.Name)
	if err != nil {
		return nil, apiserver.CustomerRef{}, err
	}
	return nil, apiserver.CustomerRef{ID: cu.ID, Name: cu.Name}, nil
}

type renameCustomerInput struct {
	boardRef
	Customer string `json:"customer" jsonschema:"the customer's id (get_board metadata.customers)"`
	Name     string `json:"name" jsonschema:"the new name"`
}

func (h *server) renameCustomer(ctx context.Context, _ *mcp.CallToolRequest, in renameCustomerInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.RenameCustomer(ctx, boardID, in.Customer, in.Name); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "renamed"}, nil
}

type reorderCustomersInput struct {
	boardRef
	Customers []string `json:"customers" jsonschema:"customer ids in the order the roster should read; the ones left out follow them"`
}

func (h *server) reorderCustomers(ctx context.Context, _ *mcp.CallToolRequest, in reorderCustomersInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.ReorderCustomers(ctx, boardID, in.Customers); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "reordered"}, nil
}

type customerInput struct {
	boardRef
	Customer string `json:"customer" jsonschema:"the customer's id (get_board metadata.customers)"`
}

func (h *server) deleteCustomer(ctx context.Context, _ *mcp.CallToolRequest, in customerInput) (*mcp.CallToolResult, statusOutput, error) {
	svc, boardID, err := h.ref(ctx, in.boardRef)
	if err != nil {
		return nil, statusOutput{}, err
	}
	if err := svc.DeleteCustomer(ctx, boardID, in.Customer); err != nil {
		return nil, statusOutput{}, err
	}
	return nil, statusOutput{Status: "deleted"}, nil
}
