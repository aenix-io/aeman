// Package mcpserver builds a Model Context Protocol (MCP) server for aeman's board
// operations, with transport chosen by the caller. Its tool set is a 1:1 projection
// of the /api/v1 resource API (Board/Card/Note resources, actions, LIST selectors),
// calling the boardservice layer — the same logic behind the HTTP handlers — not
// proxying GitHub directly.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/boardservice"
)

// Config configures the MCP server.
type Config struct {
	// Board is the default board — the name of its primary repository.
	Board string
	// Lock pins the board, ignoring per-tool overrides.
	Lock bool
	// Version is reported to MCP clients.
	Version string
	// ResolveLogin returns the caller's own login, used to scope the default
	// (unspecified-view) list to their own Me board. Optional: when nil
	// or it errors, an unspecified list falls back to the Me view for
	// everyone in the active sprint rather than one person's.
	ResolveLogin func(ctx context.Context) (string, error)
	// Backend is the board backend every tool call runs on — the server's
	// shared store, so MCP writes update the cache and reach watch clients
	// like every other write; the caller's identity and rights ride the
	// call's context.
	Backend boardservice.Backend
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
	mcp.AddTool(s, &mcp.Tool{Name: "get_board", Description: "Get the board identity, its team roster (metadata.teams), its people (metadata.members) and the customer roster (metadata.customers: id and name — cards name customers by id)."}, h.getBoard)
	mcp.AddTool(s, &mcp.Tool{Name: "list_cards", Description: "List cards as board ROWS — title, team, zone, assignees, progress, stage, dates, link refs — the same light shape a person sees on the board. Card BODIES are not included: read the one card you act on with get_card (its detail pane), its feed with list_log, its resolved links with list_links. To find a card someone mentioned by name, pass title=<substring> (case-insensitive) — one cheap call resolves it to a uid. With no view it defaults to YOUR own Me board — your own cards in the active sprint — because that is where everyone works and you normally act only on your own cards. Pass view=team&team=X for a team lead's grid of the whole team's cards (the lead view; you usually don't need it and shouldn't edit others' cards unless you're the lead or creating one), view=backlog&team=X for a team's backlog — its parked open tasks, ranked, top is next (a comma-separated team set lists several), view=strategy for the goals, initiatives and epics with everything under them plus the open work not yet in that tree, or view=all for every card on the board. Also filter by stage, semantic zone (urgent/unplanned/planned/niceToHave), assignee or team, and focus=true to keep only cards workable right now (drops done, on-review and locked) — the go-to way to answer \"what should I pick up next\". A card's parent is the hierarchy's one link: under a TASK the card is a subtask riding under it (views deliver subtasks alongside their parent, and a parent's progress bar derives from its subtasks); under a goal, an initiative or an epic it is work of that plan and stands on its own boards as before — status.lineage names its ancestors, and status.dueDate is a task's due date — the end of its nearest planning card; a review card's status.reviewed names the card it reviews and whose it is. Hand a task to a team or a person with own_card."}, h.listCards)
	mcp.AddTool(s, &mcp.Tool{Name: "get_card", Description: "Get a single card by uid — the card's DETAIL PANE: the full body (description) plus everything a row shows. This is the follow-up to a list_cards row; pair with list_log for the activity feed and list_links for resolved links. Its parent is the hierarchy's one link: a TASK parent makes it a subtask (and a task can have subtasks: its progress then derives from them, and it cannot be done while any is open); a goal, an initiative or an epic parent files it under that plan — status.lineage names every ancestor, root first. A goal, an initiative or an epic is itself a card with a type, and status.hierarchy is its standing, derived from the tasks under it."}, h.getCard)
	mcp.AddTool(s, &mcp.Tool{Name: "create_card", Description: "Create a card on a BOARD, and the board says what the card is: view=me (the default) files it on the person you are acting for, for the day, in their sprint and in the UNPLANNED band, which is the only one that board adds in; view=team files it for the team, in the Unassigned column unless you name an assignee, in any band (that board is where planning is done); view=backlog parks it in the team's backlog, last, on no day; view=strategy makes a goal, an initiative or an epic (pass type), or a task under one (pass parent) — a task given no dates is parked in the no-team group's backlog. A task has no week. A board refuses the fields it does not own and names the field. Zones are semantic (urgent/unplanned/planned/niceToHave). A title that is just a GitHub issue/PR URL is auto-filled from that issue/PR (its real title, with the link kept in the card description — appended to an explicit one that does not already contain it). Reference links belong in the description, passed at create or later via update_card. Pass parent to create the card as a subtask of an existing one (a subtask takes its parent's band, not the board's)."}, h.createCard)
	mcp.AddTool(s, &mcp.Tool{Name: "update_card", Description: "Patch a card: only the provided fields change, an explicit empty string clears a field; zones are semantic (urgent/unplanned/planned/niceToHave). Use the description field to leave context the whole team should see on the card — it is the card body everyone sees and it live-syncs onto the linked review card. Reference links belong in the description: when the work involves GitHub PRs or issues, DO include their full URLs — free-form text is fine, links are extracted from anywhere in it, surfaced on the card, and GitHub issue/PR links resolve to live titles/states (read them with list_links). Keeping the open PR/issue links on the card is encouraged: it is how the team sees what the card is waiting on. (For shareable context prefer the description over add_note, which is a private per-person log.) Set parent to file the card under another: under a TASK it becomes its subtask (one level deep; the parent's progress bar derives from its subtasks and the parent cannot be done while a subtask is open); under a goal, an initiative or an epic it joins that plan (a parent is of a higher level, a review card sits under nothing); an empty parent ungroups or unlinks it. A task is in work (a start date or a sprint) or parked in its team's backlog (parked=true); giving a parked task a date or a sprint takes it out of the backlog. A task has no week."}, h.updateCard)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_card", Description: "Delete a card for real, cascading to its linked review card. Deleting a REVIEW card withdraws the review: the reviewed task goes back to In progress (no door leaves a task on review with no review card)."}, h.deleteCard)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_card", Description: "Smart-remove a card (the UI's x). A card you are CARRYING but did not create is not yours to take off the board (403, and delete_card the same): your answer to work somebody else planned for you is update_card stage=refuse, which leaves the card standing for your lead. intent=unassign takes the card out of work and puts it back in its team's BACKLOG — parked, no person, no day, no sprint — destroying nothing (a subtask is pulled out of its group first; on a review card it is back to backlog: the reviewer is taken off and the review waits, nobody on it, at the top of the reviewed task's team backlog, the task staying on review). With no intent the x empties the working area — the sprint and the days the card stands on — and a card with nowhere else to be is DELETED, progress and all — ask the person first when the card carries work. On a REVIEW card — a reviewer's or one nobody has taken — the x without intent=unassign WITHDRAWS the review: the task goes back to In progress and the review card goes. A subtask is deleted by the plain x (it has no sprint history of its own); deleting a parent frees its subtasks into standalone cards. A goal, an initiative or an epic is removed with view=strategy intent=off-board, and what sat under it moves up to its parent. Deleting deliberately is delete_card."}, h.removeCard)
	mcp.AddTool(s, &mcp.Tool{Name: "move_card", Description: "Reorder a card to sit after another (before wins over after); an empty after moves it to the top. Keys are read off the whole board, so the neighbours may be of any team or epic: a TASK moved in a backlog becomes an exception to its epic's order, and its epic's place does not change. An EPIC is the epic move (the same as a drag on the Strategy board or in the Backlog): it takes its new place in the board-wide epic order and its parked tasks are gathered into one block in each team's backlog, their order kept; tasks in work and other epics' tasks stay. Prioritise epics this way BEFORE creating their tasks: a task created under an epic lands after that epic's last parked task, or where the epic's order puts it when the epic has none. parent re-parents the card first and then places it. Answers the card, how many cards moved and the actionId the keys were written under."}, h.moveCard)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_in_epic", Description: "Move a task WITHIN its epic, as the Strategy board's drag among an epic's tasks: the epic's parked tasks of the task's team keep the backlog slots they hold and are handed them out in the new order (after/before name another task of the epic), so other epics' tasks never move. A task in work just takes a place between its neighbours. Answers the card and how many moved."}, h.reorderInEpic)
	mcp.AddTool(s, &mcp.Tool{Name: "in_progress", Description: "Move a card to the implicit In Progress status (clears the stage, nudges progress into 10-90)."}, h.setInProgress)
	mcp.AddTool(s, &mcp.Tool{Name: "defer_card", Description: "Push a card's scheduled day N days ahead of today (or of its already-deferred slot)."}, h.deferCard)
	mcp.AddTool(s, &mcp.Tool{Name: "send_to_review", Description: "Create a linked review card for a reviewer and put the card on review; returns the review card."}, h.sendToReview)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_reviewer", Description: "Take the reviewer off a card. While the card is on review its review card goes back to the task's team backlog with nobody on it, waiting for a reviewer; a finished review, or one of a card no longer on review, is deleted."}, h.removeReviewer)
	mcp.AddTool(s, &mcp.Tool{Name: "own_card", Description: "Whose is this: hand a task to a TEAM and a PERSON on it, to a team alone, or to nobody (both left out). What it does depends on whether the card is in work yet — a date or a sprint. A person takes an UNPLANNED card into work: on the team, today, in the PLANNED band, in the team's sprint, out of its backlog — so it stands on their Team and Me boards at once; a planned card changes hands and keeps its plan. A team alone parks an unplanned card in that team's backlog with nobody on it (a parked card handed to another team stays parked); a planned card moves team and loses its person. Nobody takes both off: an unplanned card is parked in the no-team group's backlog. This is how a task is taken from the top of a backlog into work. A person needs a team; a subtask and a review card have no owner of their own — except a review NOBODY HAS TAKEN (a task put on review with no reviewer), where a person becomes its reviewer and takes it into work. On a GOAL, an INITIATIVE or an EPIC the answer is different: team is the team that carries it out and person is its OWNER, the one person who answers for it — a person needing no team there; a person alone keeps the card's team and a team alone keeps its owner, nobody takes both off, and update_card with team or owner empty takes one half off alone — and nothing about it reaches anybody's day board (the owner is not inherited by the levels below). The Strategy board's gesture, offered by every board that draws a task."}, h.ownCard)
	mcp.AddTool(s, &mcp.Tool{Name: "take_card", Description: "Take this card into work AS YOURSELF — the Backlog's own gesture. The card becomes yours on its OWN team (a card of the no-team group stays in it): a parked task is taken into work the way own_card with a person takes it — today, in the PLANNED band, in the team's sprint, out of the backlog — and a review nobody has taken makes you its reviewer. Refused where there is nothing to take: a goal, an initiative or an epic, a subtask, a review somebody already has, finished work, a task already in work, and a parked card somebody else is named on (hand those over with own_card). Use own_card to hand a card to somebody else."}, h.takeCard)
	mcp.AddTool(s, &mcp.Tool{Name: "unassign_work", Description: "The Strategy board's status menu, UNASSIGNED: take work in hand OUT of work and back to its team's backlog. On a TASK it takes the task itself, which must be open work in hand — a date or a sprint, not parked, not finished (refused otherwise). On a GOAL, an INITIATIVE or an EPIC it takes every such task under it at any depth, and answers how many it moved and which (uids). Each goes the way remove_card intent=unassign sends a card: nobody on it, no day, no sprint, its unfinished review withdrawn, parked in the backlog of the team it stands under, where it stands in the board's order. Finished work, review cards and subtasks are never touched. The person loses the work from their Team and Me boards: say so before doing it to somebody else's. uids names exactly the tasks to take (each must be one it reaches, else nothing moves); without it, every task it reaches. A task you may not write refuses the whole call before anything moves. dry_run names the tasks it would take and changes nothing."}, h.unassignWork)
	mcp.AddTool(s, &mcp.Tool{Name: "finished_earlier", Description: "A day board's answer for work that was done in the sprint BEFORE this one and only marked done now: the card is sent back to the sprint it was actually worked in, so this one is not credited with it. Refused when the card is not finished, or when there is no earlier sprint to send it to."}, h.finishedEarlier)
	mcp.AddTool(s, &mcp.Tool{Name: "list_sprints", Description: "When each team's sprint began: the current pointer and the one before it. Every date rule on the board is reckoned against these — what is in the sprint, what carry-over will take, which day a card joins — so read them before reasoning about days rather than guessing from the cards."}, h.listSprints)
	mcp.AddTool(s, &mcp.Tool{Name: "list_day_logs", Description: "One day's feed for MANY cards at once — what a day board shows. Pass the uids (at most 200) and a day; the answer is each card's events and notes of that day, oldest first, and a card that was quiet is present with an empty list. This is the cheap way to answer \"what happened here since yesterday\" for a whole board: asking list_log per card reads a full history each time."}, h.listDayLogs)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_teams", Description: "Apply the shared team order: the teams in the order they are to stand in on every board and every device (it moves their hidden sprint-state cards, so it is the board's own order, not a local preference). Pass EVERY team key, the no-team group as an empty string."}, h.reorderTeams)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_team", Description: "Delete a team's sprint pointer — the team stops being one the board plans for. Refused while any card still names it, so move or re-team its work first. The no-team group cannot be deleted."}, h.deleteTeam)
	mcp.AddTool(s, &mcp.Tool{Name: "set_sprint_state", Description: "Set a team's sprint pointers by hand: which day its current sprint began and which day the one before it did. This is the repair tool, not the daily one — carry_over is what advances a sprint and carries the unfinished work with it. Use it when the pointers are wrong (a mis-started sprint, a board imported from elsewhere)."}, h.setSprintState)
	mcp.AddTool(s, &mcp.Tool{Name: "carry_over", Description: "Advance a team's sprint to today and carry its unfinished cards forward (dryRun reports the counts)."}, h.carryOver)
	mcp.AddTool(s, &mcp.Tool{Name: "list_log", Description: "The card's activity feed: recorded events (who changed the stage, progress, assignee, review, dates or backlog, and when) merged chronologically with its work notes. The go-to way to see a card's delta — \"what happened here since yesterday\" — instead of asking people for morning reports; untouched cards simply have no fresh entries."}, h.listLog)
	mcp.AddTool(s, &mcp.Tool{Name: "set_capacity", Description: "Set how many points a week a PERSON gets through — the number their load is measured against (get_board metadata.members[].capacity, beside load; no board draws it since the Triage board was retired). 0 takes a set number back, leaving them with no number at all: the board derives none, because the record it would read is only as old as the day the board started keeping doneAt. Work the number out with the derive-capacity skill, or set one you know better than the record — a half week, a newcomer, somebody covering for two."}, h.setCapacity)
	mcp.AddTool(s, &mcp.Tool{Name: "set_team_capacity", Description: "Set how many points a week a TEAM gets through — the number a week of the team's work is weighed against (a sprint resource's capacity.points; stored and served, though no board draws it since the Triage board was retired). It is a number of its own, NOT a sum of the team's people: working that sum out means splitting anybody who works across teams by a record days old. 0 takes it back. Use the derive-capacity skill to arrive at the number."}, h.setTeamCapacity)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_team", Description: "Rename a team in place (team=<current>, to=<new>): the team's declaration keeps its sprint pointer, and every card that names the team follows. A team name is declared once across ALL the board's repositories — a name another repository already declares is refused."}, h.renameTeam)
	mcp.AddTool(s, &mcp.Tool{Name: "reopen_card", Description: "Undo a done mark: the stage clears and the progress RETURNS to what the card had when done was set (its activity log records the jump) — an accidental done+undo round-trips instead of leaving the card at 90. A card with no recorded jump falls back to In Progress."}, h.reopenCard)
	// The customer roster (docs/design/ownership.md): who the work is for.
	mcp.AddTool(s, &mcp.Tool{Name: "add_customer", Description: "Add a customer to the board's roster (last) and get its id. A customer is who the work is for; any card names customers by id (update_card customers), and the cards below a card inherit its customers. Look in get_board metadata.customers first and reuse an existing customer, mapping the user's wording onto it; a name the roster already has, case aside, is refused."}, h.addCustomer)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_customer", Description: "Rename a customer of the board's roster; every card that names it reads the new name at once, since cards hold the id."}, h.renameCustomer)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_customers", Description: "Set the order of the board's customer roster: the ids given first, in that order, the rest after them."}, h.reorderCustomers)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_customer", Description: "Delete a customer from the board's roster; it is taken off every card that names it. Ask the person first when cards name it. Refused (forbidden) unless you may write every repository holding such a card."}, h.deleteCustomer)
	mcp.AddTool(s, &mcp.Tool{Name: "list_links", Description: "List URLs from a card's description: GitHub issue/PR refs (resolved with titles) first, plain links after."}, h.listLinks)
	mcp.AddTool(s, &mcp.Tool{Name: "list_notes", Description: "List a card's work notes — the running personal work-log (timestamped lines surfaced in the assignee's own Me-view day panel, not on the team board). For the card's shared body read the description field instead."}, h.listNotes)
	mcp.AddTool(s, &mcp.Tool{Name: "add_note", Description: "Append one entry to a card's personal work-log — a timestamped line shown in the assignee's own Me-view day panel, NOT surfaced to the team browsing the board. This is NOT the place for context others must see: to leave review/handoff context for the team, set the card description via update_card instead."}, h.addNote)
	mcp.AddTool(s, &mcp.Tool{Name: "edit_note", Description: "Rewrite one of a card's work notes."}, h.editNote)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_note", Description: "Delete one of a card's work notes."}, h.deleteNote)
	return s
}

// resolve picks the effective board, honouring the lock and the default.
func (h *server) resolve(boardID string) (string, error) {
	b := h.cfg.Board
	if !h.cfg.Lock && boardID != "" {
		b = boardID
	}
	if b == "" {
		return "", fmt.Errorf("board is required (pass it or configure the server default)")
	}
	return b, nil
}

// defaultBackend is the production newBackend: the configured backend.
func (h *server) defaultBackend(context.Context) (boardservice.Backend, error) {
	if h.cfg.Backend == nil {
		return nil, fmt.Errorf("mcpserver: no board backend configured")
	}
	return h.cfg.Backend, nil
}

// ref resolves the board reference and builds the board service for a call.
func (h *server) ref(ctx context.Context, in boardRef) (svc *boardservice.Service, boardID string, err error) {
	boardID, err = h.resolve(in.Board)
	if err != nil {
		return nil, "", err
	}
	backend, err := h.newBackend(ctx)
	if err != nil {
		return nil, "", err
	}
	return boardservice.New(backend), boardID, nil
}
