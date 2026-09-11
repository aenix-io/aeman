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
	// (unspecified-view) list to their personal Me board. Optional: when nil
	// or it errors, an unspecified list falls back to the Me view for
	// everyone in the active sprint rather than a personal one.
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
	mcp.AddTool(s, &mcp.Tool{Name: "get_board", Description: "Get the board identity, its team roster (metadata.teams) and the Project board's structure: the project roster (metadata.projects), the epic columns with the project each belongs to (metadata.epics), and the weeks carrying a deadline (metadata.deadlines). \"Project\" here is aeman's planning entity — a group of epic columns — not the GitHub board, which is addressed by owner+board."}, h.getBoard)
	mcp.AddTool(s, &mcp.Tool{Name: "list_cards", Description: "List cards as board ROWS — title, team, zone, assignees, progress, stage, dates, link refs — the same light shape a person sees on the board. Card BODIES are not included: read the one card you act on with get_card (its detail pane), its feed with list_log, its resolved links with list_links. To find a card someone mentioned by name, pass title=<substring> (case-insensitive) — one cheap call resolves it to a uid. With no view it defaults to YOUR personal Me board — your own cards in the active sprint — because that is where everyone works and you normally act only on your own cards. Pass view=team&team=X for a team lead's grid of the whole team's cards (the lead view; you usually don't need it and shouldn't edit others' cards unless you're the lead or creating one), view=triage for the weeks work is scheduled in, or view=all for every card on the board. Also filter by stage, semantic zone (urgent/unplanned/planned/niceToHave), assignee or team, and focus=true to keep only cards workable right now (drops done, on-review and locked) — the go-to way to answer \"what should I pick up next\". Cards can be grouped: a card with a parent field is a subtask riding under that parent (views deliver subtasks alongside their parent, and a parent's progress bar derives from its subtasks)."}, h.listCards)
	mcp.AddTool(s, &mcp.Tool{Name: "get_card", Description: "Get a single card by uid — the card's DETAIL PANE: the full body (description) plus everything a row shows. This is the follow-up to a list_cards row; pair with list_log for the activity feed and list_links for resolved links. A card whose parent field is set is a subtask of that card; a card can itself have subtasks (its progress then derives from them, and it cannot be done while any subtask is open)."}, h.getCard)
	mcp.AddTool(s, &mcp.Tool{Name: "create_card", Description: "Create a card on a BOARD, and the board says what the card is: view=me (the default) files it on the person you are acting for, for the day, in their sprint and in the UNPLANNED band, which is the only one that board adds in; view=team files it for the team, in the Unassigned column unless you name an assignee, in any band (that board is where planning is done); view=triage schedules it for a WEEK and no day (pass week — a card of the week being worked may carry today's dates too); view=backlog parks it on the team's shelf; view=project files it under a column (pass epic and project); view=personal puts it in your own repository. A board refuses the fields it does not own and names the field. Zones are semantic (urgent/unplanned/planned/niceToHave). A title that is just a GitHub issue/PR URL is auto-filled from that issue/PR (its real title, with the link kept in the card description). To attach reference links afterwards, put them in the description via update_card. Pass parent to create the card as a subtask of an existing one (a subtask takes its parent's band, not the board's); a card cannot be both a subtask and scheduled for a week of its own, so naming a parent and a week together is refused, since grouping hands a subtask's week to its parent."}, h.createCard)
	mcp.AddTool(s, &mcp.Tool{Name: "update_card", Description: "Patch a card: only the provided fields change, an explicit empty string clears a field; zones are semantic (urgent/unplanned/planned/niceToHave). Use the description field to leave context the whole team should see on the card — it is the card body everyone sees and it live-syncs onto the linked review card. Reference links belong in the description: when the work involves GitHub PRs or issues, DO include their full URLs — free-form text is fine, links are extracted from anywhere in it, surfaced on the card, and GitHub issue/PR links resolve to live titles/states (read them with list_links). Keeping the open PR/issue links on the card is encouraged: it is how the team sees what the card is waiting on. (For shareable context prefer the description over add_note, which is a private per-person log.) Set parent to a card uid to group this card as its subtask (one level deep; the parent's progress bar derives from its subtasks and the parent cannot be done while a subtask is open); an empty parent ungroups it back to standalone. A card's COLUMN must live in the repository that holds the card's file: for a subtask (whose file rides its parent) or a review card (whose file follows its original) that is the linked card's repository, not the column's project — attaching, grouping, ungrouping, re-teaming or linking against that rule is refused. A card cannot be both a subtask and scheduled for a week of its own either: giving a subtask a week is refused (clearing one is free), so ungroup it first."}, h.updateCard)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_card", Description: "Delete a card for real, cascading to its linked review card."}, h.deleteCard)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_card", Description: "Smart-remove a card (the UI's x). A card you are CARRYING but did not create is not yours to take off the board (403, and delete_card the same): your answer to work somebody else planned for you is update_card stage=refuse, which leaves the card standing for your lead. It empties the working area — the sprint and the days the card stands on. With the WEEK it is scheduled for, or a Project-board column (the EPIC side — a bare project name is on no board), to fall back on the card goes there; with nowhere else to be it is DELETED, progress and all — ask the person first when the card carries work. A subtask with nowhere else to be is deleted outright: it has no sprint history of its own. One standing in a project column is not — that column is a home of its own, and the card is released to it. Deleting a parent frees its subtasks into standalone cards rather than destroying them. A card filed under a project column is not deleted here while that column can hold it. One exception: a SUBTASK whose column belongs to its PARENT's repository loses that column when the x pulls it out of the group — its file follows its own team — and is then answered like any other columnless card, demoted or deleted. Deleting deliberately is delete_card."}, h.removeCard)
	mcp.AddTool(s, &mcp.Tool{Name: "mirror_card", Description: "Show the card in a SECOND Project-board column ({uid, project, epic}): the same card, one file, one log, one set of dates, standing in both projects — so shared work is one card on one person, not a duplicate per project. The card must already be in a column (attach it first via update_card epic/project); the target column must exist and be in the card's own repository. Mirroring where it already stands is a no-op."}, h.mirrorCard)
	mcp.AddTool(s, &mcp.Tool{Name: "unmirror_card", Description: "Take one mirror column away from the card ({uid, project, epic}); its home column and everything else stay."}, h.unmirrorCard)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_from_project", Description: "The Project board's x: remove the card from ONE column ({uid, project, epic}). A mirror simply goes; the home column, with mirrors left, hands its role to the first mirror; the LAST column also drops the week that was its row, and the card survives only in the working area — only when someone had it and moved it (assignee + progress). An untouched card is DELETED outright — ask the person first when work would go. A SUBTASK is never deleted here: its other home is its parent, so the x only takes the column away."}, h.removeFromProject)
	mcp.AddTool(s, &mcp.Tool{Name: "move_card", Description: "Reorder a card to sit after another; an empty after moves it to the top."}, h.moveCard)
	mcp.AddTool(s, &mcp.Tool{Name: "in_progress", Description: "Move a card to the implicit In Progress status (clears the stage, nudges progress into 10-90)."}, h.setInProgress)
	mcp.AddTool(s, &mcp.Tool{Name: "defer_card", Description: "Push a card's scheduled day N days ahead of today (or of its already-deferred slot)."}, h.deferCard)
	mcp.AddTool(s, &mcp.Tool{Name: "send_to_review", Description: "Create a linked review card for a reviewer and put the card on review; returns the review card."}, h.sendToReview)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_reviewer", Description: "Delete a card's linked review card."}, h.removeReviewer)
	mcp.AddTool(s, &mcp.Tool{Name: "place_card", Description: "The Triage board's drop: schedule the card for a WEEK (its Monday). That is what TRIAGING a card means — until then nobody has said when the work is due. A week AHEAD of this one takes the card off the day boards (it is on none until that Monday); the CURRENT week joins the team's sprint instead. A Project-board slot is refused: its week IS its start date's week, so move the dates."}, h.placeCard)
	mcp.AddTool(s, &mcp.Tool{Name: "untriage_card", Description: "The Triage board's opposite: take the card's week away and put it back in the STRIP, where it waits for somebody to say when. Nothing else about the card changes."}, h.untriageCard)
	mcp.AddTool(s, &mcp.Tool{Name: "finished_earlier", Description: "A day board's answer for work that was done in the sprint BEFORE this one and only marked done now: the card is sent back to the sprint it was actually worked in, so this one is not credited with it. Refused when the card is not finished, or when there is no earlier sprint to send it to."}, h.finishedEarlier)
	mcp.AddTool(s, &mcp.Tool{Name: "list_sprints", Description: "When each team's sprint began: the current pointer and the one before it. Every date rule on the board is reckoned against these — what is in the sprint, what carry-over will take, which day a card joins — so read them before reasoning about days rather than guessing from the cards."}, h.listSprints)
	mcp.AddTool(s, &mcp.Tool{Name: "list_day_logs", Description: "One day's feed for MANY cards at once — what a day board shows. Pass the uids (at most 200) and a day; the answer is each card's events and notes of that day, oldest first, and a card that was quiet is present with an empty list. This is the cheap way to answer \"what happened here since yesterday\" for a whole board: asking list_log per card reads a full history each time."}, h.listDayLogs)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_teams", Description: "Apply the shared team order: the teams in the order they are to stand in on every board and every device (it moves their hidden sprint-state cards, so it is the board's own order, not a local preference). Pass EVERY team key, the no-team group as an empty string."}, h.reorderTeams)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_epics", Description: "Apply one project's column order — the Project board's columns left to right, shared by everyone. Pass that project's column names in full; an empty project is the no-project bucket."}, h.reorderEpics)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_team", Description: "Delete a team's sprint pointer — the team stops being one the board plans for. Refused while any card or process task still names it, so move or re-team its work first. The no-team group cannot be deleted."}, h.deleteTeam)
	mcp.AddTool(s, &mcp.Tool{Name: "set_sprint_state", Description: "Set a team's sprint pointers by hand: which day its current sprint began and which day the one before it did. This is the repair tool, not the daily one — carry_over is what advances a sprint and carries the unfinished work with it. Use it when the pointers are wrong (a mis-started sprint, a board imported from elsewhere)."}, h.setSprintState)
	mcp.AddTool(s, &mcp.Tool{Name: "carry_over", Description: "Advance a team's sprint to today and carry its unfinished cards forward (dryRun reports the counts)."}, h.carryOver)
	mcp.AddTool(s, &mcp.Tool{Name: "list_log", Description: "The card's activity feed: recorded events (who changed the stage, progress, assignee, review or week, and when) merged chronologically with its work notes. The go-to way to see a card's delta — \"what happened here since yesterday\" — instead of asking people for morning reports; untouched cards simply have no fresh entries."}, h.listLog)
	mcp.AddTool(s, &mcp.Tool{Name: "add_epic", Description: "Declare a new epic — a column of the Project board (weeks as rows, a project's epics as columns) — inside an existing project: pass project=<name> from get_board metadata.projects (add_project creates one). Leaving project empty is allowed but deliberate — it files the column in the no-project bucket, where only that chip shows it. Column names are unique WITHIN a project, so every project can have its own \"Docs\"; a card names the (project, epic) pair. Cards are filed under a column via create_card/update_card epic=<name>+project=<name>; their week is the row and start/end may span several weeks. Only on an explicit request for a new epic: read the existing ones from get_board metadata.epics first."}, h.addEpic)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_epic", Description: "Delete an EMPTY epic column from the Project board (name=<epic>, project=<its project>). A column that still has cards is protected (move or clear them first) — orphaning planned work silently is exactly what the Project board exists to prevent."}, h.deleteEpic)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_epic", Description: "Rename a column in place (project=<project>, name=<current>, to=<new>). Its cards follow — they store the column's name, so the rename rewrites both sides. A name already used by another column of the SAME project is refused; the same name in another project is fine."}, h.renameEpic)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_project", Description: "Rename a project in place (name=<current>, to=<new>). Its columns and their cards follow."}, h.renameProject)
	mcp.AddTool(s, &mcp.Tool{Name: "set_capacity", Description: "Set how many points a week a PERSON gets through — the number the Triage board measures their load against (get_board metadata.members[].capacity, beside load). 0 takes a set number back, leaving them with no number at all: the board derives none, because the record it would read is only as old as the day the board started keeping doneAt. Work the number out with the derive-capacity skill, or set one you know better than the record — a half week, a newcomer, somebody covering for two."}, h.setCapacity)
	mcp.AddTool(s, &mcp.Tool{Name: "set_team_capacity", Description: "Set how many points a week a TEAM gets through — the number the Triage board holds each week's scheduled points against (a sprint resource's capacity.points). It is a number of its own, NOT a sum of the team's people: working that sum out means splitting anybody who works across teams by a record days old. 0 takes it back. Use the derive-capacity skill to arrive at the number."}, h.setTeamCapacity)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_team", Description: "Rename a team in place (team=<current>, to=<new>): the team's declaration keeps its sprint pointer, and every card and process task that names the team follows. A team name is declared once across ALL the board's repositories — a name another repository already declares is refused. Teams, projects and processes keep SEPARATE namespaces, so a team may carry the name of a project."}, h.renameTeam)
	mcp.AddTool(s, &mcp.Tool{Name: "set_epic_project", Description: "Move a column from one project to another (name=<epic>, from=<current project>, project=<target>). An empty target detaches the column, leaving it visible only in the all-projects view — do that only when explicitly asked. The column's cards are rewritten along with it. A column cannot change REPOSITORY: its stub is written back to the repository that holds it, so a target project of another one is refused, and the name must be free in the destination — the no-project bucket included."}, h.setEpicProject)
	mcp.AddTool(s, &mcp.Tool{Name: "list_processes", Description: "The Process tab: recurring work the team keeps doing — every process with its tasks, and each task's history (done / open / late per iteration), which is how to tell whether a process is actually alive. Scope with project=<name>."}, h.listProcesses)
	mcp.AddTool(s, &mcp.Tool{Name: "add_process", Description: "Declare a process — recurring work inside a project, e.g. \"Publishing\" or \"Collecting payment\". A process groups tasks; add them with add_process_task. Only on an explicit request: read the existing ones from list_processes first."}, h.addProcess)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_process", Description: "Delete an EMPTY process. One that still has tasks is protected: delete those first, on purpose."}, h.deleteProcess)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_process", Description: "Rename a process (name=<current>, to=<new>); its tasks follow."}, h.renameProcess)
	mcp.AddTool(s, &mcp.Tool{Name: "set_process_project", Description: "Move a process to another project (project=\"\" is the no-project bucket). Its tasks and their iterations are untouched: a process belongs to a project, the work it spawns belongs to the process."}, h.setProcessProject)
	mcp.AddTool(s, &mcp.Tool{Name: "set_process_paused", Description: "Pause a process, or resume it. A paused process files no iterations; its tasks and their history are untouched, and resuming files what the current week is already owed. Use this rather than deleting a process that is only stopping for a while."}, h.setProcessPaused)
	mcp.AddTool(s, &mcp.Tool{Name: "reopen_card", Description: "Undo a done mark: the stage clears and the progress RETURNS to what the card had when done was set (its activity log records the jump) — an accidental done+undo round-trips instead of leaving the card at 90. A card with no recorded jump falls back to In Progress."}, h.reopenCard)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_processes", Description: "Apply a shared process order — the list every client reads back. Pass every process name in the desired order."}, h.reorderProcesses)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_process_tasks", Description: "Apply one process's task order. A uid from another process is adopted into this one at its position — that is how a task moves between processes; its past turns keep their history."}, h.reorderProcessTasks)
	mcp.AddTool(s, &mcp.Tool{Name: "add_process_task", Description: "Add what a process iterates on: the title and body every iteration is created with, its cycle (week | 2weeks | month | quarter, counted on the calendar from start), the team the iterations land in, and the standing owner. Every iteration is a fresh copy of THIS — a renamed live card stays renamed, the next one is the task again. The server files what each week is owed on its own. The cycle is per task because one process (collecting payment) has one task per client, and clients pay on different schedules."}, h.addProcessTask)
	mcp.AddTool(s, &mcp.Tool{Name: "update_process_task", Description: "Change what the NEXT iterations will be (only the provided fields apply). CONTENT stays with the next turn: a new title, body or cycle leaves the iteration currently running untouched. ROUTING follows at once: a new team or assignee moves this week's turn to the new owner — an untouched one (0%) is deleted and a fresh card is spawned for them, one that has been worked on stays with whoever worked it and the new owner gets a second card. So renaming is safe on a live turn; reassigning may destroy it."}, h.updateProcessTask)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_process_task", Description: "Delete a task. Its past iterations are ordinary cards and stay — they are the record of what was done."}, h.deleteProcessTask)
	mcp.AddTool(s, &mcp.Tool{Name: "add_deadline", Description: "Mark a week of the Project board with a deadline — the red line across the grid (week=<any day of it>). A deadline belongs to a PROJECT: one line per project per week, so marking a week this project already marked changes nothing, while another project's line on the same week is a second line and not a clash. Read the current ones from get_board metadata.deadlines."}, h.addDeadline)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_deadline", Description: "Clear a week's deadline line (week=<any day of it>)."}, h.deleteDeadline)
	mcp.AddTool(s, &mcp.Tool{Name: "move_deadline", Description: "Move a deadline to another week (week=<where it is>, to=<where it goes>). Landing on a week this SAME project already marked merges the two into one line — a project holds at most one per week. Another project's line on that week is left where it is: two projects can have something due the same week, and those are two deadlines."}, h.moveDeadline)
	mcp.AddTool(s, &mcp.Tool{Name: "add_project", Description: "Declare a new project — the Project board's top-level grouping, one chip and one grid of epic columns. A project may be created empty and filled with epics afterwards (add_epic project=<name>). Only on an explicit request: read the existing roster from get_board metadata.projects first. This is aeman's planning entity, NOT a GitHub project board."}, h.addProject)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_project", Description: "Delete an EMPTY project from the Project board. A project that still owns epic columns is protected: delete those columns (or move them with set_epic_project) first, so planned work is never quietly detached from its plan."}, h.deleteProject)
	mcp.AddTool(s, &mcp.Tool{Name: "reorder_projects", Description: "Set the order the project chips appear in, passing every project name in the wanted order."}, h.reorderProjects)
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
