package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// registerAPI wires the JSON API under /api/v1: a small set of Kubernetes-style
// resources (Card, Sprint, Note, Ordering) plus actions for everything with
// board-level rules. All board logic lives server-side in boardservice; clients
// state intent and mirror the result via LIST (+ selectors) and the watch.
//
// Routes:
//
//	GET    /api/v1                                    public route catalog (no auth)
//	GET    /api/v1/board                              board identity + team roster
//	GET    /api/v1/cards                              LIST cards (selectors: view/team/day/user/week/stage/zone/assignee)
//	POST   /api/v1/cards                              create a card
//	GET    /api/v1/cards/{uid}                        one card
//	PATCH  /api/v1/cards/{uid}                        edit spec fields (admission applies the rules)
//	DELETE /api/v1/cards/{uid}                        hard delete (cascades to the review card)
//	POST   /api/v1/cards/{uid}/actions/remove         the smart × (grid/plan semantics)
//	POST   /api/v1/cards/{uid}/actions/move           reorder after another card
//	POST   /api/v1/cards/{uid}/actions/defer          push the scheduled day N days ahead
//	POST   /api/v1/cards/{uid}/actions/in-progress    move to the implicit In Progress
//	POST   /api/v1/cards/{uid}/actions/reopen         undo a done mark, restoring the pre-done progress
//	POST   /api/v1/cards/{uid}/actions/send-to-review send (or reassign) to a reviewer
//	POST   /api/v1/cards/{uid}/actions/remove-reviewer delete the linked review card
//	POST   /api/v1/cards/{uid}/actions/take-into-plan  take a plan card into work
//	POST   /api/v1/cards/{uid}/actions/release-from-plan release from the weekly plan
//	GET    /api/v1/cards/{uid}/links                  links from the description (GitHub refs resolved)
//	GET    /api/v1/cards/{uid}/log                    unified activity feed (events + notes)
//	GET    /api/v1/cards/{uid}/notes                  the card's work notes
//	POST   /api/v1/cards/{uid}/notes                  append a note
//	PATCH  /api/v1/cards/{uid}/notes/{noteId}         edit a note
//	DELETE /api/v1/cards/{uid}/notes/{noteId}         delete a note
//	GET    /api/v1/sprints                            per-team sprint pointers
//	PATCH  /api/v1/sprints                            set a team's pointer directly
//	POST   /api/v1/sprints/actions/carry-over         advance a sprint, carry unfinished (dryRun)
//	GET    /api/v1/ordering                           the board-level manual order
//	GET    /api/v1/watch                              WebSocket stream (Card/Sprint/Ordering events)
func (s *Server) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1", s.handleAPIIndex)
	mux.HandleFunc("GET /api/v1/board", s.handleGetBoard)
	mux.HandleFunc("GET /api/v1/cards", s.handleListCards)
	mux.HandleFunc("POST /api/v1/cards", s.handleCreateCard)
	mux.HandleFunc("GET /api/v1/cards/{uid}", s.handleGetCard)
	mux.HandleFunc("PATCH /api/v1/cards/{uid}", s.handlePatchCard)
	mux.HandleFunc("DELETE /api/v1/cards/{uid}", s.handleDeleteCard)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/remove", s.handleRemoveCard)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/move", s.handleMoveCard)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/defer", s.handleDeferCard)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/in-progress", s.handleInProgress)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/reopen", s.handleReopen)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/send-to-review", s.handleSendToReview)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/mirror", s.handleMirror)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/unmirror", s.handleUnmirror)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/remove-from-project", s.handleRemoveFromProject)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/remove-reviewer", s.handleRemoveReviewer)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/take-into-plan", s.handleTakeIntoPlan)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/release-from-plan", s.handleReleaseFromPlan)
	mux.HandleFunc("GET /api/v1/cards/{uid}/links", s.handleListLinks)
	mux.HandleFunc("GET /api/v1/cards/{uid}/log", s.handleCardLog)
	mux.HandleFunc("GET /api/v1/logs", s.handleDayLogs)
	mux.HandleFunc("GET /api/v1/cards/{uid}/notes", s.handleListNotes)
	mux.HandleFunc("POST /api/v1/cards/{uid}/notes", s.handleAddNote)
	mux.HandleFunc("PATCH /api/v1/cards/{uid}/notes/{noteId}", s.handleEditNote)
	mux.HandleFunc("DELETE /api/v1/cards/{uid}/notes/{noteId}", s.handleDeleteNote)
	mux.HandleFunc("GET /api/v1/sprints", s.handleListSprints)
	mux.HandleFunc("PATCH /api/v1/sprints", s.handlePatchSprint)
	mux.HandleFunc("POST /api/v1/sprints/actions/carry-over", s.handleCarryOver)
	mux.HandleFunc("POST /api/v1/sprints/actions/reorder-teams", s.handleReorderTeams)
	mux.HandleFunc("POST /api/v1/sprints/actions/delete-team", s.handleDeleteTeam)
	mux.HandleFunc("POST /api/v1/epics", s.handleAddEpic)
	mux.HandleFunc("POST /api/v1/epics/actions/delete-epic", s.handleDeleteEpic)
	mux.HandleFunc("POST /api/v1/epics/actions/reorder-epics", s.handleReorderEpics)
	mux.HandleFunc("POST /api/v1/epics/actions/set-project", s.handleSetEpicProject)
	mux.HandleFunc("POST /api/v1/epics/actions/rename", s.handleRenameEpic)
	mux.HandleFunc("GET /api/v1/processes", s.handleListProcesses)
	mux.HandleFunc("POST /api/v1/processes", s.handleAddProcess)
	mux.HandleFunc("POST /api/v1/processes/actions/delete-process", s.handleDeleteProcess)
	mux.HandleFunc("POST /api/v1/processes/actions/rename", s.handleRenameProcess)
	mux.HandleFunc("POST /api/v1/processes/actions/set-project", s.handleSetProcessProject)
	mux.HandleFunc("POST /api/v1/processes/actions/set-paused", s.handleSetProcessPaused)
	mux.HandleFunc("POST /api/v1/processes/actions/reorder", s.handleReorderProcesses)
	mux.HandleFunc("POST /api/v1/processes/tasks/actions/reorder", s.handleReorderProcessTasks)
	mux.HandleFunc("POST /api/v1/processes/tasks", s.handleAddTask)
	mux.HandleFunc("PATCH /api/v1/processes/tasks/{uid}", s.handlePatchTask)
	mux.HandleFunc("DELETE /api/v1/processes/tasks/{uid}", s.handleDeleteTask)
	mux.HandleFunc("POST /api/v1/deadlines", s.handleAddDeadline)
	mux.HandleFunc("POST /api/v1/deadlines/actions/delete", s.handleDeleteDeadline)
	mux.HandleFunc("POST /api/v1/deadlines/actions/move", s.handleMoveDeadline)
	mux.HandleFunc("POST /api/v1/projects", s.handleAddProject)
	mux.HandleFunc("POST /api/v1/projects/actions/delete-project", s.handleDeleteProject)
	mux.HandleFunc("POST /api/v1/projects/actions/reorder-projects", s.handleReorderProjects)
	mux.HandleFunc("POST /api/v1/projects/actions/rename", s.handleRenameProject)
	mux.HandleFunc("POST /api/v1/teams/actions/rename", s.handleRenameTeam)
	mux.HandleFunc("GET /api/v1/me/personal", s.handleGetPersonal)
	mux.HandleFunc("PUT /api/v1/me/personal", s.handleLinkPersonal)
	mux.HandleFunc("DELETE /api/v1/me/personal", s.handleUnlinkPersonal)
	mux.HandleFunc("GET /api/v1/ordering", s.handleGetOrdering)
	mux.HandleFunc("POST /api/v1/presence", s.handleSetPresence)
	mux.HandleFunc("GET /api/v1/watch", s.handleWatch)
}

// apiEndpoint describes one route in the GET /api/v1 catalog.
type apiEndpoint struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// apiIndex is the public GET /api/v1 catalog: identity, the MCP mount point and
// the full route list. It carries no board data, so it needs no authentication.
type apiIndex struct {
	Name      string        `json:"name"`
	Version   string        `json:"version"`
	MCP       string        `json:"mcp"`
	Endpoints []apiEndpoint `json:"endpoints"`
}

// handleAPIIndex serves the public API catalog. It mirrors the routes wired in
// registerAPI so a client can discover them without a token.
func (s *Server) handleAPIIndex(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, apiIndex{
		Name:    "aeman",
		Version: s.opts.Version,
		MCP:     "/mcp",
		Endpoints: []apiEndpoint{
			{"GET", "/api/v1/board", "Board identity, team roster, deadlines, and the Project board's projects and epic columns. The board is the one the server was started with — no addressing parameter; \"project\" is aeman's planning entity"},
			{"GET", "/api/v1/cards", "List cards as board rows (no descriptions; status.links carries extracted refs); selectors: view=team|me|weekly|project, team, day, user, week, project, stage, zone, assignee, fields=full for complete cards"},
			{"POST", "/api/v1/cards", "Create a card (joins or starts a sprint; plan cards via spec.plan)"},
			{"GET", "/api/v1/cards/{uid}", "One card in full (the body lives here, not in listings)"},
			{"PATCH", "/api/v1/cards/{uid}", "Edit spec fields; the server applies clamps, links and date rules"},
			{"DELETE", "/api/v1/cards/{uid}", "Hard delete (cascades to the linked review card)"},
			{"POST", "/api/v1/cards/{uid}/actions/remove", "The smart remove: demote, release or delete by board rules ({from: grid|plan})"},
			{"POST", "/api/v1/cards/{uid}/actions/move", "Reorder after ({after}) or before ({before}) another card; empty = top"},
			{"POST", "/api/v1/cards/{uid}/actions/defer", "Push the scheduled day {days} ahead of today"},
			{"POST", "/api/v1/cards/{uid}/actions/reopen", "Undo a done mark: the stage clears and the progress returns to what the card had when done was set (its log records the jump); no history falls back to In Progress"},
			{"POST", "/api/v1/cards/{uid}/actions/in-progress", "Move to the implicit In Progress status"},
			{"POST", "/api/v1/cards/{uid}/actions/send-to-review", "Send to review ({reviewer, day}); reassigns if a review card exists"},
			{"POST", "/api/v1/cards/{uid}/actions/remove-reviewer", "Delete the linked review card"},
			{"POST", "/api/v1/cards/{uid}/actions/take-into-plan", "Take a plan card into work ({engineer, zone, day})"},
			{"POST", "/api/v1/cards/{uid}/actions/release-from-plan", "Release a card from the weekly plan"},
			{"GET", "/api/v1/cards/{uid}/links", "URLs from the card's description; GitHub issue/PR refs resolved with titles, listed first"},
			{"GET", "/api/v1/cards/{uid}/log", "The card's activity feed: recorded events (stage/progress/review/plan changes) and work notes, one chronological list"},
			{"GET", "/api/v1/cards/{uid}/notes", "The card's work notes"},
			{"POST", "/api/v1/cards/{uid}/notes", "Append a work note ({text})"},
			{"PATCH", "/api/v1/cards/{uid}/notes/{noteId}", "Edit a work note ({text})"},
			{"DELETE", "/api/v1/cards/{uid}/notes/{noteId}", "Delete a work note"},
			{"GET", "/api/v1/sprints", "Per-team sprint pointers"},
			{"PATCH", "/api/v1/sprints", "Set a team's sprint pointer directly ({team, current, previous})"},
			{"POST", "/api/v1/sprints/actions/carry-over", "Advance a team's sprint to today, carry unfinished ({team, dryRun})"},
			{"POST", "/api/v1/sprints/actions/reorder-teams", "Apply a shared team order (moves the hidden sprint-state cards; body {teams:[...]})"},
			{"POST", "/api/v1/sprints/actions/delete-team", "Delete a team's sprint pointer; refused while cards still use the team (body {team})"},
			{"POST", "/api/v1/epics", "Declare an epic column inside a project ({name, project}); the project is required"},
			{"POST", "/api/v1/epics/actions/delete-epic", "Delete an EMPTY epic column; refused while cards sit under it ({epic, project})"},
			{"POST", "/api/v1/epics/actions/reorder-epics", "Apply one project's column order (moves the hidden epic-state cards; body {project, epics:[...]})"},
			{"POST", "/api/v1/epics/actions/set-project", "Move a column from one project to another ({epic, from, project}); an empty target detaches it"},
			{"POST", "/api/v1/epics/actions/rename", "Rename a column in place, cards and all ({project, epic, to})"},
			{"GET", "/api/v1/processes", "The Process tab: every process with its tasks and each task's history (?project= filters)"},
			{"POST", "/api/v1/processes", "Declare a process — recurring work inside a project ({name, project})"},
			{"POST", "/api/v1/processes/actions/delete-process", "Delete an EMPTY process ({process}); refused while it has tasks"},
			{"POST", "/api/v1/processes/actions/rename", "Rename a process; its tasks follow ({process, to})"},
			{"POST", "/api/v1/processes/actions/set-project", "Move a process to another project ({process, project}; empty project = the no-project bucket)"},
			{"POST", "/api/v1/processes/actions/reorder", "Apply a shared process order ({processes: [names]})"},
			{"POST", "/api/v1/processes/tasks/actions/reorder", "Apply one process's task order ({process, uids}); a uid from another process is adopted into this one — how a cross-process drop lands"},
			{"POST", "/api/v1/processes/actions/set-paused", "Pause a process, or resume it ({process, paused}); a paused process files no iterations"},
			{"POST", "/api/v1/processes/tasks", "Add what a process iterates on ({process, title, description, recurrence, start, team, assignee, accumulate})"},
			{"PATCH", "/api/v1/processes/tasks/{uid}", "Change what the NEXT iterations will be; the running one is untouched"},
			{"DELETE", "/api/v1/processes/tasks/{uid}", "Delete a task; its past iterations stay as the record"},
			{"POST", "/api/v1/deadlines", "Mark a week with a project's deadline line ({week, project}); a project holds at most one per week"},
			{"POST", "/api/v1/deadlines/actions/delete", "Clear a project's deadline on a week ({week, project})"},
			{"POST", "/api/v1/deadlines/actions/move", "Drag a project's deadline to another week ({project, from, to}); landing where it already has one leaves a single line"},
			{"POST", "/api/v1/projects", "Declare a project — the Project board's top grouping, which owns epic columns ({name})"},
			{"POST", "/api/v1/projects/actions/delete-project", "Delete an EMPTY project; refused while it owns epic columns ({project})"},
			{"POST", "/api/v1/projects/actions/reorder-projects", "Apply the shared project order (body {projects:[...]})"},
			{"POST", "/api/v1/projects/actions/rename", "Rename a project in place, columns and cards along with it ({project, to})"},
			{"POST", "/api/v1/teams/actions/rename", "Rename a team in place, its cards and process tasks along with it ({team, to}); a name another team has is refused"},
			{"GET", "/api/v1/me/personal", "The caller's personal board: the repository linked as their own domain ({domain, url}), 404 when none"},
			{"PUT", "/api/v1/me/personal", "Link a repository the caller can push to as their personal board ({url}); an empty repository is given a board. Personal cards: POST /cards with personal=true, GET /cards?view=personal"},
			{"DELETE", "/api/v1/me/personal", "Unlink the caller's personal board; the repository is left as it is"},
			{"GET", "/api/v1/ordering", "The board-level manual card order"},
			{"POST", "/api/v1/presence", "Share the caller's live card selection ({login, card}; empty card clears)"},
			{"GET", "/api/v1/watch", "WebSocket stream of Card/Sprint/Ordering events; selector-scoped with view params"},
		},
	})
}

// boardRef is the board a request addresses: the one this server is
// configured with. A client's board query parameter is ignored;
// "project" in the API is aeman's own planning entity (a group of epic
// columns on the Project board) and is a card filter, not an address.
func (s *Server) boardRef(*http.Request) (boardID string) {
	return s.gitBoard()
}

// defaultService is the production newService: the board service over the
// one shared store, as the visitor may use it — the request brings its
// identity (actor), its action and its rights; the visible backend projects
// reads and checks writes against those.
func (s *Server) defaultService(*http.Request) (*boardservice.Service, error) {
	if s.visibleBE == nil {
		return nil, errNoBoard
	}
	return boardservice.New(s.visibleBE), nil
}

// errNoBoard is a server started without a repository.
var errNoBoard = errors.New("no board is configured: start aeman with --repo")

// service resolves the board reference and builds the per-request board service.
// On failure it writes the response (400 on a bad ref, 401 on no token) and
// returns ok=false.
func (s *Server) service(w http.ResponseWriter, r *http.Request) (svc *boardservice.Service, boardID string, ok bool) {
	boardID = s.boardRef(r)
	svc, err := s.newService(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
		return nil, "", false
	}
	return svc, boardID, true
}

// handleCardLog serves a card's unified activity feed: its recorded events and
// work notes merged chronologically. The day delta Ivan asked for — "what
// happened on this card since yesterday" — reads straight off this list.
func (s *Server) handleCardLog(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	card, events, truncated, err := svc.Log(r.Context(), boardID, r.PathValue("uid"))
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiserver.CardLogFrom(card, events, truncated))
}

// handleDayLogs answers the day feed: one day's notes and events for every
// card named in uids. The day board asks this once instead of a whole
// history per card — the read that made a page load fire dozens of
// second-long requests.
func (s *Server) handleDayLogs(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uids := splitList(r.URL.Query().Get("uids"))
	if len(uids) > maxDayLogCards {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("uids: at most %d cards per request", maxDayLogCards))
		return
	}
	day := r.URL.Query().Get("day")
	if day != "" && !board.IsDayIso(day) {
		writeJSONError(w, http.StatusBadRequest, "day: want yyyy-mm-dd")
		return
	}
	per, err := svc.DayLogs(r.Context(), boardID, uids, day)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	if day == "" {
		day = board.TodayIso()
	}
	entries := make(map[string]apiserver.DayEntries, len(per))
	for uid, d := range per {
		entries[uid] = apiserver.DayEntries{Notes: d.Notes, Events: d.Events}
	}
	writeJSON(w, http.StatusOK, apiserver.DayLogsFrom(day, entries))
}

// maxDayLogCards bounds one day-feed request: a day board shows tens of
// cards, and an unbounded list would be a way to ask for the whole board's
// history in one call.
const maxDayLogCards = 200

// splitList reads a comma-separated query value, dropping the blanks.
func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// statusResponse is the acknowledgement returned by actions that leave no single
// card to echo (delete, remove, move).
type statusResponse struct {
	Status string `json:"status"`
}

// --- Reads -------------------------------------------------------------------

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// A record's board carries that day's SPRINT POINTERS with its cards —
	// the view rules place a card by its team's pointer, and today's would
	// drop nearly all of them. The roster itself (projects, columns,
	// processes, deadlines) stays TODAY's: a column added since shows on the
	// record of an older day, which is the price of not rebuilding the whole
	// structure per read, and is what docs/dates.md says.
	b, _, _, err := s.boardOfRequest(r, svc, boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	info := apiserver.BoardResourceWithPeople(b, s.store.member)
	login, personal, linked := s.personalOf(r)
	// Always, one repository or many: the payload's stamps are domain
	// NAMES — the store stamps the primary's entries too (G59) — so a
	// board that listed none left the client comparing "aeman" against "",
	// two names for one repository, and every rule that asks "the same
	// repository?" answered no. The UI keys its multi-repository parts off
	// the COUNT (isMultiDomain), not off the list's presence, so a single
	// entry shows no badges and no repository pickers.
	if s.gitCfg != nil {
		logins := make([]string, 0, len(info.Metadata.Members))
		for _, m := range info.Metadata.Members {
			logins = append(logins, m.Login)
		}
		info.Metadata.Domains = s.domainsFor(r.Context(), logins, login)
	}
	if linked {
		info.Metadata.Personal = &personal
	}
	writeJSON(w, http.StatusOK, info)
}

// domainsFor lists the visitor's readable domains, primary first, with
// whether they may write each and which of the board's members can read it
// (G16). Rights come from the request; who else reads a domain is the
// forge's answer with the server credential.
func (s *Server) domainsFor(ctx context.Context, members []string, login string) []apiserver.DomainInfo {
	rights := rightsFrom(ctx)
	out := make([]apiserver.DomainInfo, 0, len(s.gitCfg.Repos)+1)
	for _, d := range s.gitCfg.Repos {
		if !rights.canRead(d.Name) {
			continue
		}
		// One repository is the whole board: everyone on it can read it —
		// a visitor who cannot is refused the board itself (G17) — so the
		// forge is not asked who. That question exists to tell one
		// repository's readers from another's, and it is a blocking call
		// on a cold load for a login nothing is cached for.
		if len(s.gitCfg.Repos) == 1 {
			out = append(out, apiserver.DomainInfo{Name: d.Name, Writable: rights.canWrite(d.Name), Members: append([]string(nil), members...)})
			continue
		}
		readers, err := s.access.readers(ctx, d.Name, members)
		if err != nil {
			s.log.Warn("domain members", "domain", d.Name, "err", err)
			readers = []string{}
		}
		if readers == nil {
			readers = []string{}
		}
		out = append(out, apiserver.DomainInfo{Name: d.Name, Writable: rights.canWrite(d.Name), Members: readers})
	}
	// The visitor's own personal domain, when attached: theirs alone.
	if login != "" && s.gitBE != nil && s.gitBE.hasPersonal(login) {
		out = append(out, apiserver.DomainInfo{Name: board.PersonalDomain(login), Writable: true, Members: []string{login}, Personal: true})
	}
	return out
}

func (s *Server) handleListCards(w http.ResponseWriter, r *http.Request) {
	sel, err := apiserver.ParseSelector(r.URL.Query())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// An unspecified view defaults to the caller's personal Me board — their own
	// cards in the active sprint — since that is where everyone works day to day;
	// Team is the lead view, requested explicitly with ?view=team, and ?view=all
	// lists the whole board. "Who am I" is resolved here, server-side, so a Me
	// request needs no user (an explicit ?user= still wins, e.g. for "view as").
	if sel.View == "" {
		sel.View = "me"
	}
	if (sel.View == "me" || sel.View == "personal") && sel.User == "" {
		if _, login, err := s.apiTokens(r); err == nil {
			sel.User = login
		}
	}
	// The owner reading their personal board is what turns its day over: a
	// personal board has no carry-over, so the finished recurrent cards that
	// came due are reseeded here, and the list answers with the fresh copies.
	// As of the real today, whatever day the list asks for — `day` is a lens
	// (the column follows the day the board is flipped to), and looking at
	// tomorrow must not create tomorrow's copies early.
	if sel.View == "personal" && sel.User != "" && sel.User == board.ActorFrom(r.Context()) {
		if _, err := svc.ReseedPersonal(r.Context(), boardID, sel.User, ""); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	// A PAST day can be asked for as it stood, rather than as today's board
	// filtered by that day's dates (see boardOfRequest).
	b, asOf, records, err := s.boardOfRequest(r, svc, boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	if asOf != "" {
		// A record of the day gives back what the × took off it — of the
		// cards this listing IS a record of, and no others (G60).
		sel.LeftOn, sel.RecordCards = sel.Day, records
	}
	list := apiserver.ListCards(b, sel)
	apiserver.MarkRecords(&list, records, asOf)
	writeJSON(w, http.StatusOK, list)
}

// boardOfRequest is the board a read answers from: today's, or — when the
// request asks for a PAST day as it stood (`snapshot=1`) — the board of that
// day. The day itself is built by the board service (BoardOfDay), which is
// what every other door reads it through: an agent over MCP and this handler
// must not answer "what did that day look like" differently.
//
// records names the cards the day's board took from that evening (empty on a
// live read), and asOf the moment the record reflects.
func (s *Server) boardOfRequest(r *http.Request, svc *boardservice.Service, boardID string) (bd board.Board, asOf string, records map[string]bool, err error) {
	q := r.URL.Query()
	day := q.Get("day")
	asked := q.Get("snapshot") == "1" || q.Get("snapshot") == "true"
	// Only a DAY board has a day to be a record of (G60). The flag is
	// ignored elsewhere rather than refused: /board and /sprints carry no
	// view at all, and every reader of them is a day board asking for its
	// own moment.
	if !asked || (q.Get("view") != "" && !board.HasRecords(q.Get("view"))) {
		day = ""
	}
	bd, records, at, err := svc.BoardOfDay(r.Context(), boardID, day)
	if err != nil || at.IsZero() {
		return bd, "", nil, err
	}
	return bd, at.Format(time.RFC3339), records, nil
}

func (s *Server) handleGetCard(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	s.cardResponse(w, r, svc, boardID, r.PathValue("uid"))
}

func (s *Server) handleListSprints(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// A past day's pointers come with its cards: the client's own view rules
	// compare a card's sprint against them, so today's pointers over that
	// day's cards drop nearly all of them.
	b, _, _, err := s.boardOfRequest(r, svc, boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":  "SprintList",
		"items": apiserver.SprintResources(b),
	})
}

func (s *Server) handleGetOrdering(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	b, err := svc.Board(r.Context(), boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiserver.OrderingResource(b))
}

// --- Create / patch ------------------------------------------------------------

// createCardRequest mirrors the Card spec shape for creation.
type createCardRequest struct {
	Title     string   `json:"title"`
	Team      string   `json:"team"`
	Zone      string   `json:"zone"`
	Assignees []string `json:"assignees"`
	Dates     struct {
		Start  string `json:"start"`
		End    string `json:"end"`
		Sprint string `json:"sprint"`
	} `json:"dates"`
	Plan *struct {
		Band string `json:"band"`
		Week string `json:"week"`
	} `json:"plan"`
	// Epic + Project file the card on the Project board, under the column that
	// pair identifies. Its row is the week of dates.start — plan.week is for
	// weekly-plan cards and is ignored here — and dates may span weeks.
	Epic           string `json:"epic"`
	CardProject    string `json:"project"`
	ReviewOf       string `json:"reviewOf"`
	Parent         string `json:"parent"`
	StartNewSprint *bool  `json:"startNewSprint"`
	// NoSprint schedules the card for its day without joining any sprint (a
	// "next sprint" create); the next carry-over to reach its day adopts it.
	NoSprint bool `json:"noSprint"`
	// Personal files the card on the caller's personal board (their own
	// repository) instead of the team board; no team, column or plan band.
	Personal bool `json:"personal"`
}

func (s *Server) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	var in createCardRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Title == "" {
		writeJSONError(w, http.StatusUnprocessableEntity, "title is required")
		return
	}
	zone, ok := parseZone(w, in.Zone)
	if !ok {
		return
	}
	// A personal card needs the personal domain, and a domain that would not
	// attach is refused deeper down as "no write access to the card's
	// domain" — about a repository its owner plainly owns, with the real
	// reason left in the server's log. Say it here instead.
	if in.Personal {
		if _, login, err := s.apiTokens(r); err == nil && login != "" {
			if why, action := s.personalUnavailable(r.Context(), login); why != "" {
				writeJSONErrorAction(w, http.StatusForbidden, why, action)
				return
			}
		}
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	args := boardservice.CreateCardArgs{
		Team:           in.Team,
		Zone:           zone,
		Title:          in.Title,
		Day:            in.Dates.End,
		Start:          in.Dates.Start,
		SprintStart:    in.Dates.Sprint,
		Epic:           in.Epic,
		Project:        in.CardProject,
		Personal:       in.Personal,
		ReviewOf:       in.ReviewOf,
		Parent:         in.Parent,
		StartNewSprint: in.StartNewSprint,
		NoSprint:       in.NoSprint,
	}
	if len(in.Assignees) > 0 {
		args.Assignee = in.Assignees[0]
	}
	if in.Epic != "" && in.Plan != nil && in.Plan.Week != "" {
		args.Week = in.Plan.Week
	}
	if in.Plan != nil && in.Plan.Band != "" {
		band, ok := parsePlanBand(w, in.Plan.Band)
		if !ok {
			return
		}
		args.Plan = band
		args.Week = in.Plan.Week
	}
	card, err := svc.CreateCard(r.Context(), boardID, args)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	b, err := svc.Board(r.Context(), boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, apiserver.CardResource(b, card))
}

// cardPatch is the PATCH /cards/{uid} body: only present fields are applied,
// so absent and empty are different things (empty clears).
type cardPatch struct {
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Team        *string   `json:"team"`
	Zone        *string   `json:"zone"`
	Assignees   *[]string `json:"assignees"`
	Progress    *int      `json:"progress"`
	Stage       *string   `json:"stage"`
	// Recurrence is a recurrent card's reseed cycle ("", "week", "month").
	Recurrence *string `json:"recurrence"`
	// Epic and Project are the two halves of the card's column ("" clears).
	// Epic names repeat across projects, so filing into another project's
	// column names both; naming only the epic stays inside the card's project.
	Epic     *string     `json:"epic"`
	Project  *string     `json:"project"`
	Dates    *datesPatch `json:"dates"`
	Plan     *planPatch  `json:"plan"`
	ReviewOf *string     `json:"reviewOf"`
	// Parent groups the card as a subtask under another card ("" ungroups).
	Parent *string `json:"parent"`
	// Process ties the card to a process — the recurring shelf's counterpart
	// of a column ("" clears). The process must already exist.
	Process *string `json:"process"`
}

// datesPatch is the spec.dates fragment of a card patch.
type datesPatch struct {
	Start  *string `json:"start"`
	End    *string `json:"end"`
	Sprint *string `json:"sprint"`
}

// planPatch is the spec.plan fragment of a card patch.
type planPatch struct {
	Band *string `json:"band"`
	Week *string `json:"week"`
}

// applyGroupingPatch is the parent/process fragment of a card patch: what
// the card is grouped under.
func (s *Server) applyGroupingPatch(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, boardID, uid string, p *cardPatch) bool {
	ctx := r.Context()
	if p.Parent != nil {
		if err := svc.SetParent(ctx, boardID, uid, *p.Parent); err != nil {
			s.apiError(w, r, err)
			return false
		}
	}
	if p.Process != nil {
		if err := svc.SetCardProcess(ctx, boardID, uid, *p.Process); err != nil {
			s.apiError(w, r, err)
			return false
		}
	}
	return true
}

// handlePatchCard applies a spec patch field by field through the service, so
// every admission rule (clamps, the review-link sync, the review-cancel
// cascade, calendar date semantics) runs exactly as if the UI made the edit.
func (s *Server) handlePatchCard(w http.ResponseWriter, r *http.Request) {
	var p cardPatch
	if !decodeJSON(w, r, &p) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	uid := r.PathValue("uid")
	if p.Title != nil {
		if err := svc.Rename(ctx, boardID, uid, *p.Title); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.Description != nil {
		if err := svc.SetDescription(ctx, boardID, uid, *p.Description); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.Team != nil {
		if err := svc.SetTeam(ctx, boardID, uid, *p.Team, ""); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.Epic != nil || p.Project != nil {
		if err := patchColumn(ctx, svc, boardID, uid, p); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.Zone != nil {
		zone, ok := parseZone(w, *p.Zone)
		if !ok {
			return
		}
		if err := svc.SetZone(ctx, boardID, uid, zone); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.Stage != nil {
		stage, ok := parseStage(w, *p.Stage)
		if !ok {
			return
		}
		if err := svc.SetStage(ctx, boardID, uid, stage); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.Recurrence != nil {
		if err := svc.SetRecurrence(ctx, boardID, uid, *p.Recurrence); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.Progress != nil {
		if err := svc.SetProgress(ctx, boardID, uid, *p.Progress); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.Dates != nil {
		if !s.applyDatesPatch(w, r, svc, boardID, uid, p.Dates) {
			return
		}
	}
	if p.Plan != nil {
		if !s.applyPlanPatch(w, r, svc, boardID, uid, p.Plan) {
			return
		}
	}
	if !s.applyGroupingPatch(w, r, svc, boardID, uid, &p) {
		return
	}
	// Assignees are applied AFTER the parent on purpose. Ungrouping hands an
	// ownerless child the parent's person so it does not fall off every
	// board; a patch that also names assignees means the caller decided who
	// owns it — including "nobody" — and that decision must land last.
	if p.Assignees != nil {
		login := ""
		if len(*p.Assignees) > 0 {
			login = (*p.Assignees)[0]
		}
		if err := svc.SetAssignee(ctx, boardID, uid, login); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	if p.ReviewOf != nil {
		if err := svc.SetReviewOf(ctx, boardID, uid, *p.ReviewOf); err != nil {
			s.apiError(w, r, err)
			return
		}
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

// applyDatesPatch applies a spec.dates patch. A patched start runs the calendar
// semantics (the sprint follows the sprint active on the start day); patching
// only the end (or only the sprint) stays granular, and an explicit sprint
// always wins. Returns false after writing the error response.
func (s *Server) applyDatesPatch(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, boardID string, uid string, d *datesPatch) bool {
	ctx := r.Context()
	if d.Start != nil {
		end := ""
		if d.End != nil {
			end = *d.End
		} else if card, err := svc.Card(ctx, boardID, uid); err == nil {
			end = card.Day
		}
		if err := svc.SetDates(ctx, boardID, uid, *d.Start, end); err != nil {
			s.apiError(w, r, err)
			return false
		}
	} else if d.End != nil {
		if err := svc.SetDay(ctx, boardID, uid, *d.End); err != nil {
			s.apiError(w, r, err)
			return false
		}
	}
	if d.Sprint != nil {
		if err := svc.SetSprintStart(ctx, boardID, uid, *d.Sprint); err != nil {
			s.apiError(w, r, err)
			return false
		}
	}
	return true
}

// applyPlanPatch applies a spec.plan patch (band and/or week).
func (s *Server) applyPlanPatch(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, boardID string, uid string, pl *planPatch) bool {
	ctx := r.Context()
	if pl.Band != nil {
		band, ok := parsePlanBand(w, *pl.Band)
		if !ok {
			return false
		}
		if err := svc.SetPlan(ctx, boardID, uid, band); err != nil {
			s.apiError(w, r, err)
			return false
		}
	}
	if pl.Week != nil {
		if err := svc.SetWeek(ctx, boardID, uid, *pl.Week); err != nil {
			s.apiError(w, r, err)
			return false
		}
	}
	return true
}

// --- Card actions --------------------------------------------------------------

func (s *Server) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteCard(r.Context(), boardID, r.PathValue("uid")); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *Server) handleRemoveCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.From != "" && in.From != "grid" && in.From != "plan" {
		writeJSONError(w, http.StatusBadRequest, "from must be grid or plan")
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.Remove(r.Context(), boardID, r.PathValue("uid"), in.From); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *Server) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		After  string `json:"after"`
		Before string `json:"before"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	move := func() error {
		if in.Before != "" {
			return svc.MoveCardBefore(r.Context(), boardID, r.PathValue("uid"), in.Before)
		}
		return svc.MoveCard(r.Context(), boardID, r.PathValue("uid"), in.After)
	}
	if err := move(); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *Server) handleDeferCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Days int `json:"days"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Days <= 0 {
		writeJSONError(w, http.StatusBadRequest, "days must be positive")
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.Defer(r.Context(), boardID, uid, in.Days); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

func (s *Server) handleInProgress(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.SetInProgress(r.Context(), boardID, uid); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

func (s *Server) handleReopen(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.Reopen(r.Context(), boardID, uid); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

// placementBody is the {project, epic} pair the mirror actions take.
type placementBody struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
}

func (s *Server) placementAction(w http.ResponseWriter, r *http.Request, respondCard bool,
	act func(svc *boardservice.Service, boardID, uid, project, epic string) error,
) {
	var in placementBody
	if !decodeJSON(w, r, &in) {
		return
	}
	// A column is named by its EPIC, and the project half may be empty
	// everywhere: the no-project bucket is a column like any other — a
	// mirror home included, since a column's repository is read off the
	// column's own stub and not off a project (G15/G59). This door used to
	// require a project for a mirror, so a placement the service, the
	// codec, MCP and the docs all accept was a 422 here — and the SPA's
	// own picker, which offers the bucket, drove straight into it.
	if in.Epic == "" {
		writeJSONError(w, http.StatusUnprocessableEntity, "the epic is required — a column is named by its epic")
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := act(svc, boardID, r.PathValue("uid"), in.Project, in.Epic); err != nil {
		s.apiError(w, r, err)
		return
	}
	// Mirror and unmirror answer with the card resource, like the other
	// card actions; remove-from-project cannot — its card may no longer
	// exist — so it answers {"ok": true}.
	if respondCard {
		s.cardResponse(w, r, svc, boardID, r.PathValue("uid"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMirror adds a second Project-board column to the card — the same
// card shown in both projects, one file and one log.
func (s *Server) handleMirror(w http.ResponseWriter, r *http.Request) {
	s.placementAction(w, r, true, func(svc *boardservice.Service, boardID, uid, project, epic string) error {
		return svc.Mirror(r.Context(), boardID, uid, project, epic)
	})
}

// handleUnmirror takes one mirror column away.
func (s *Server) handleUnmirror(w http.ResponseWriter, r *http.Request) {
	s.placementAction(w, r, true, func(svc *boardservice.Service, boardID, uid, project, epic string) error {
		return svc.Unmirror(r.Context(), boardID, uid, project, epic)
	})
}

// handleRemoveFromProject is the Project board's ×: remove the card from
// one column, with the mirror/promote/last-column rules of the service.
func (s *Server) handleRemoveFromProject(w http.ResponseWriter, r *http.Request) {
	s.placementAction(w, r, false, func(svc *boardservice.Service, boardID, uid, project, epic string) error {
		return svc.RemoveFromProject(r.Context(), boardID, uid, project, epic)
	})
}

// handleSendToReview sends a card to a reviewer. When a linked review card
// already exists the action reassigns it instead — the backend decides, the
// client just states the intent.
func (s *Server) handleSendToReview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reviewer string `json:"reviewer"`
		Day      string `json:"day"`
		// Zone places the review card explicitly (the Me board sends it to the
		// reviewer's unplanned zone); empty keeps the original's zone.
		Zone string `json:"zone"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Reviewer == "" {
		writeJSONError(w, http.StatusUnprocessableEntity, "reviewer is required")
		return
	}
	zone, ok := parseZone(w, in.Zone)
	if !ok {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	uid := r.PathValue("uid")
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	for _, c := range b.Cards {
		if c.ReviewOf == uid {
			if err := svc.ReassignReviewer(ctx, boardID, uid, in.Reviewer, in.Day, zone); err != nil {
				s.apiError(w, r, err)
				return
			}
			s.cardResponse(w, r, svc, boardID, c.ItemID)
			return
		}
	}
	review, err := svc.SendToReview(ctx, boardID, uid, in.Reviewer, in.Day, zone)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	b, err = svc.Board(ctx, boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, apiserver.CardResource(b, review))
}

func (s *Server) handleRemoveReviewer(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.RemoveReviewer(r.Context(), boardID, uid); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

func (s *Server) handleTakeIntoPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Engineer string `json:"engineer"`
		Zone     string `json:"zone"`
		Day      string `json:"day"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Engineer == "" {
		writeJSONError(w, http.StatusUnprocessableEntity, "engineer is required")
		return
	}
	zone, ok := parseZone(w, in.Zone)
	if !ok {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.TakeIntoPlan(r.Context(), boardID, uid, in.Engineer, zone, in.Day); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

func (s *Server) handleReleaseFromPlan(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.ReleaseFromPlan(r.Context(), boardID, uid); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

// --- Notes ----------------------------------------------------------------------

// handleListLinks serves the URLs found in a card's description: GitHub
// issue/PR references first (resolved to their titles when possible), plain
// links after.
func (s *Server) handleListLinks(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	links, err := svc.CardLinks(r.Context(), boardID, r.PathValue("uid"))
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	if links == nil {
		links = []board.Link{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "LinkList", "items": links})
}

// notesResponse serves a card's notes after any note mutation, so clients
// always converge on the server's view of the thread.
func (s *Server) notesResponse(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, boardID string, uid string, status int) {
	card, err := svc.Card(r.Context(), boardID, uid)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, status, map[string]any{
		"kind":  "NoteList",
		"items": apiserver.NoteResources(card),
	})
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	s.notesResponse(w, r, svc, boardID, r.PathValue("uid"), http.StatusOK)
}

func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Text == "" {
		writeJSONError(w, http.StatusUnprocessableEntity, "text is required")
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.AddNote(r.Context(), boardID, uid, in.Text); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.notesResponse(w, r, svc, boardID, uid, http.StatusCreated)
}

func (s *Server) handleEditNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.EditNote(r.Context(), boardID, uid, r.PathValue("noteId"), in.Text); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.notesResponse(w, r, svc, boardID, uid, http.StatusOK)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.DeleteNote(r.Context(), boardID, uid, r.PathValue("noteId")); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.notesResponse(w, r, svc, boardID, uid, http.StatusOK)
}

// --- Sprints ----------------------------------------------------------------------

func (s *Server) handlePatchSprint(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team     string `json:"team"`
		Current  string `json:"current"`
		Previous string `json:"previous"`
		// Domain is the repository a NEW team is declared in; an existing
		// team's pointer stays where the team is.
		Domain string `json:"domain"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.SetSprintState(board.WithDomain(r.Context(), in.Domain), boardID, in.Team, in.Current, in.Previous); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiserver.Sprint{
		Kind:     "Sprint",
		Metadata: apiserver.SprintMetadata{Team: in.Team},
		Spec:     apiserver.SprintSpec{Current: in.Current, Previous: in.Previous},
	})
}

func (s *Server) handleCarryOver(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team   string `json:"team"`
		DryRun bool   `json:"dryRun"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The carry ops read the board through the SAME snapshot every other
	// mutation already trusts: with write-behind the cache (queue replayed on
	// top) IS the live truth, while a blocking GitHub reload here only adds
	// seconds of latency and a chance to read a lagging replica. Stale-window
	// semantics apply; a cold cache still loads.
	ctx, _ := withStaleAllowed(r.Context())
	rep, err := svc.CarryOver(ctx, boardID, in.Team, in.DryRun)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleReorderTeams applies a shared team order: the hidden sprint-state
// cards are moved into the given sequence, so every client reads the same
// order back from the board metadata.
func (s *Server) handleReorderTeams(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Teams []string `json:"teams"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// Same cached-snapshot read as carry-over (see handleCarryOver).
	ctx, _ := withStaleAllowed(r.Context())
	if err := svc.ReorderTeams(ctx, boardID, in.Teams); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleDeleteTeam deletes a team's hidden sprint-state card. A team that
// still has cards is protected server-side (422).
func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team string `json:"team"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteTeam(r.Context(), boardID, in.Team); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAddEpic declares a new Project-board column inside a project
// (body {name, project}). The project is required — see AddEpic.
func (s *Server) handleAddEpic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.AddEpic(r.Context(), boardID, in.Name, in.Project); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// handleSetEpicProject moves a column from one project to another
// (body {epic, from, project}); an empty target detaches it.
func (s *Server) handleSetEpicProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Epic    string `json:"epic"`
		From    string `json:"from"`
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.SetEpicProject(r.Context(), boardID, in.From, in.Epic, in.Project); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRenameEpic renames a column in place, cards and all
// (body {project, epic, to}).
func (s *Server) handleRenameEpic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
		Epic    string `json:"epic"`
		To      string `json:"to"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.RenameEpic(r.Context(), boardID, in.Project, in.Epic, in.To); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRenameProject renames a project in place, columns and cards along
// with it (body {project, to}).
func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
		To      string `json:"to"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.RenameProject(r.Context(), boardID, in.Project, in.To); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRenameTeam renames a team where it is declared, its cards and process
// tasks along with it.
func (s *Server) handleRenameTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team string `json:"team"`
		To   string `json:"to"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.RenameTeam(r.Context(), boardID, in.Team, in.To); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// patchColumn re-files a card under a column — the (project, epic) pair.
// Naming only the project keeps the column name the card is already under,
// which is what moving a card between projects means.
func patchColumn(ctx context.Context, svc *boardservice.Service, boardID string, uid string, p cardPatch) error {
	epic := ""
	if p.Epic != nil {
		epic = *p.Epic
	} else if card, err := svc.Card(ctx, boardID, uid); err == nil {
		epic = card.Epic
	}
	return svc.SetEpic(ctx, boardID, uid, epic, p.Project)
}

// --- Processes -----------------------------------------------------------------

func (s *Server) handleListProcesses(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	b, err := svc.Board(r.Context(), boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiserver.ProcessesResource(b, r.URL.Query().Get("project")))
}

func (s *Server) handleAddProcess(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Project string `json:"project"`
		// Domain is the repository to declare a project-less process in;
		// a process with a project lives with the project.
		Domain string `json:"domain"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(board.WithDomain(staleOK(r.Context()), in.Domain))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.AddProcess(r.Context(), boardID, in.Name, in.Project); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteProcess(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Process string `json:"process"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteProcess(r.Context(), boardID, in.Process); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRenameProcess(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Process string `json:"process"`
		To      string `json:"to"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.RenameProcess(r.Context(), boardID, in.Process, in.To); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetProcessProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Process string `json:"process"`
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.SetProcessProject(r.Context(), boardID, in.Process, in.Project); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetProcessPaused(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Process string `json:"process"`
		Paused  bool   `json:"paused"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.SetProcessPaused(r.Context(), boardID, in.Process, in.Paused); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleReorderProcesses(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Processes []string `json:"processes"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.ReorderProcesses(r.Context(), boardID, in.Processes); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleReorderProcessTasks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Process string   `json:"process"`
		UIDs    []string `json:"uids"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.ReorderProcessTasks(r.Context(), boardID, in.Process, in.UIDs); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// taskRequest is a task on the wire, for create (all fields) and
// patch (pointers: only the present ones apply).
type taskRequest struct {
	Process     string  `json:"process"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Recurrence  *string `json:"recurrence"`
	Start       *string `json:"start"`
	Team        *string `json:"team"`
	Assignee    *string `json:"assignee"`
	Accumulate  *bool   `json:"accumulate"`
}

func (s *Server) handleAddTask(w http.ResponseWriter, r *http.Request) {
	var in taskRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	tpl, err := svc.AddProcessTask(r.Context(), boardID, in.Process, boardservice.TaskArgs{
		Title: str(in.Title), Description: str(in.Description), Recurrence: str(in.Recurrence),
		Start: str(in.Start), Team: str(in.Team), Assignee: str(in.Assignee),
		Accumulate: in.Accumulate != nil && *in.Accumulate,
	})
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"uid": tpl.ItemID})
}

func (s *Server) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	var in taskRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	err := svc.UpdateProcessTask(r.Context(), boardID, r.PathValue("uid"), boardservice.TaskPatch{
		Title: in.Title, Description: in.Description, Recurrence: in.Recurrence,
		Start: in.Start, Team: in.Team, Assignee: in.Assignee, Accumulate: in.Accumulate,
	})
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(staleOK(r.Context()))
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteProcessTask(r.Context(), boardID, r.PathValue("uid")); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAddDeadline marks a week with one project's deadline line
// (body {week, project}).
func (s *Server) handleAddDeadline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Week    string `json:"week"`
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.AddDeadline(r.Context(), boardID, in.Week, in.Project); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// handleDeleteDeadline clears one project's deadline on a week
// (body {week, project}).
func (s *Server) handleDeleteDeadline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Week    string `json:"week"`
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.DeleteDeadline(r.Context(), boardID, in.Week, in.Project); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMoveDeadline drags a deadline to another week (body {from, to});
// landing on a week that already has one leaves a single line.
func (s *Server) handleMoveDeadline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
		From    string `json:"from"`
		To      string `json:"to"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.MoveDeadline(r.Context(), boardID, in.Project, in.From, in.To); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAddProject declares a project — the Project board's top grouping,
// which owns epic columns (body {name}).
func (s *Server) handleAddProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		// Domain is the repository to declare the project in (git mode with
		// several); empty is the primary.
		Domain string `json:"domain"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(board.WithDomain(staleOK(r.Context()), in.Domain))
	if err := svc.AddProject(r.Context(), boardID, in.Name); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// handleDeleteProject removes an EMPTY project (422 while it still owns epic
// columns — detaching planned work silently is the anti-goal).
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.DeleteProject(r.Context(), boardID, in.Project); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleReorderProjects applies the shared chip order (body {projects:[...]}).
func (s *Server) handleReorderProjects(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Projects []string `json:"projects"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.ReorderProjects(r.Context(), boardID, in.Projects); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleDeleteEpic removes an EMPTY epic column (422 while cards still sit
// under it — the Project board's own anti-goal).
func (s *Server) handleDeleteEpic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Epic    string `json:"epic"`
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.DeleteEpic(r.Context(), boardID, in.Epic, in.Project); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleReorderEpics applies a shared column order (body {epics:[...]}),
// moving the hidden epic-state cards the way reorder-teams moves sprint-state.
func (s *Server) handleReorderEpics(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string   `json:"project"`
		Epics   []string `json:"epics"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// The roster is read here to check a name and to carry the board's
	// ids into the write; a snapshot minutes old answers both, and
	// blocking on a full reload made adding a column feel broken on a
	// big board. The background revalidation catches the rest up.
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.ReorderEpics(r.Context(), boardID, in.Project, in.Epics); err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Shared helpers ----------------------------------------------------------------

// handleSetPresence records the caller's live Me-view selection — ephemeral
// shared-cursor state broadcast over the watch, never persisted. The client id
// (X-Aeman-Client) keys it, so a closed tab clears its own mark.
func (s *Server) handleSetPresence(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Card string `json:"card"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	boardID := s.boardRef(r)
	if _, err := s.newService(r); err != nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
		return
	}
	// The broadcast login is the caller's authenticated identity (stamped by
	// actorMiddleware), not a client-supplied value — otherwise any signed-in
	// user could show a chosen card as selected by someone else.
	login := board.ActorFrom(r.Context())
	s.store.SetPresence(storeKey(boardID), clientIDFrom(r.Context()), login, in.Card)
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

// cardResponse loads the (post-mutation) card and writes it as the resource —
// mutations echo the card exactly as a fresh GET would return it.
func (s *Server) cardResponse(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, boardID string, uid string) {
	b, err := svc.Board(r.Context(), boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	for _, c := range b.Cards {
		if c.ItemID == uid {
			writeJSON(w, http.StatusOK, apiserver.CardResource(b, c))
			return
		}
	}
	s.apiError(w, r, fmt.Errorf("%w: %s", boardservice.ErrCardNotFound, uid))
}

// parseZone validates a semantic zone name ("" clears); on failure it writes
// the 400 and returns ok=false.
func parseZone(w http.ResponseWriter, name string) (board.ZoneKey, bool) {
	if name == "" {
		return "", true
	}
	zone := apiserver.DomainZone(name)
	if zone == "" {
		writeJSONError(w, http.StatusBadRequest, "unknown zone (urgent, unplanned, planned, niceToHave or empty)")
		return "", false
	}
	return zone, true
}

// parseStage validates a stage name ("" clears).
func parseStage(w http.ResponseWriter, name string) (board.StageKey, bool) {
	switch board.StageKey(name) {
	case board.StageNone, board.StageLocked, board.StageReview, board.StageRecurrent, board.StageDone:
		return board.StageKey(name), true
	}
	writeJSONError(w, http.StatusBadRequest, "unknown stage (locked, review, recurrent, done or empty)")
	return "", false
}

// parsePlanBand validates a weekly-plan band ("" releases).
func parsePlanBand(w http.ResponseWriter, name string) (board.PlanBand, bool) {
	switch board.PlanBand(name) {
	case board.PlanNone, board.PlanWed, board.PlanFri:
		return board.PlanBand(name), true
	}
	writeJSONError(w, http.StatusBadRequest, "unknown plan band (wed, fri or empty)")
	return "", false
}

// decodeJSON reads the request body into dst, answering 400 on malformed input.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// apiError maps service errors onto HTTP statuses.
func (s *Server) apiError(w http.ResponseWriter, _ *http.Request, err error) {
	switch {
	case errors.Is(err, boardservice.ErrCardNotFound), errors.Is(err, boardservice.ErrNoteNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, boardservice.ErrForbidden):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, gitstore.ErrUnknownDomain):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, boardservice.ErrHistoryTruncated):
		// The day was there and is not any more — the clone's horizon moved
		// past it. Gone says exactly that, and a client can offer to widen
		// the horizon rather than retry.
		writeJSONError(w, http.StatusGone, err.Error())
	case errors.Is(err, boardservice.ErrNoHistory):
		// Storage that keeps no past cannot be asked about one; another
		// board (or another backend) can.
		writeJSONError(w, http.StatusNotImplemented, err.Error())
	case errors.Is(err, gitstore.ErrNameTaken),
		errors.Is(err, gitstore.ErrPersonalRoster),
		errors.Is(err, boardservice.ErrTeamExists),
		errors.Is(err, boardservice.ErrTeamNotFound),
		errors.Is(err, boardservice.ErrPersonalPlacement),
		errors.Is(err, boardservice.ErrInvalidStage),
		errors.Is(err, boardservice.ErrDescriptionTooLong),
		errors.Is(err, boardservice.ErrNoteTooLong),
		errors.Is(err, boardservice.ErrSubtaskDepth),
		errors.Is(err, boardservice.ErrPlanSubtask),
		errors.Is(err, boardservice.ErrParentNotFound),
		errors.Is(err, boardservice.ErrOpenSubtasks),
		errors.Is(err, boardservice.ErrTeamInUse),
		errors.Is(err, boardservice.ErrEpicInUse),
		errors.Is(err, boardservice.ErrEpicExists),
		errors.Is(err, boardservice.ErrEpicNotFound),
		errors.Is(err, boardservice.ErrProjectInUse),
		errors.Is(err, boardservice.ErrProjectExists),
		errors.Is(err, boardservice.ErrProjectNotFound),
		errors.Is(err, boardservice.ErrWeekDerived),
		errors.Is(err, boardservice.ErrProcessExists),
		errors.Is(err, boardservice.ErrProcessNotFound),
		errors.Is(err, boardservice.ErrTurnProcess),
		errors.Is(err, boardservice.ErrNotRecurrent),
		errors.Is(err, boardservice.ErrSubtaskTie),
		errors.Is(err, boardservice.ErrProcessInUse),
		errors.Is(err, boardservice.ErrTaskNotFound),
		errors.Is(err, boardservice.ErrDomainConflict),
		errors.Is(err, boardservice.ErrCrossDomain),
		errors.Is(err, boardservice.ErrNoColumn),
		errors.Is(err, boardservice.ErrOwnColumn),
		errors.Is(err, boardservice.ErrSubtaskMirror),
		errors.Is(err, boardservice.ErrNotInProject):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeJSONError(w, http.StatusBadGateway, err.Error())
	}
}
