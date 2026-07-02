package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/aenix-org/aeman/pkg/apiserver"
	"github.com/aenix-org/aeman/pkg/board"
	"github.com/aenix-org/aeman/pkg/boardservice"
	"github.com/aenix-org/aeman/pkg/ghprojects"
)

// errMissingBoard is returned when neither query parameters nor server defaults
// identify a board.
var errMissingBoard = errors.New("owner and project are required (set ?owner=&project= or server defaults)")

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
//	POST   /api/v1/cards/{uid}/actions/send-to-review send (or reassign) to a reviewer
//	POST   /api/v1/cards/{uid}/actions/remove-reviewer delete the linked review card
//	POST   /api/v1/cards/{uid}/actions/take-into-plan  take a plan card into work
//	POST   /api/v1/cards/{uid}/actions/release-from-plan release from the weekly plan
//	GET    /api/v1/cards/{uid}/links                  links from the description (GitHub refs resolved)
//	GET    /api/v1/cards/{uid}/notes                  the card's work notes
//	POST   /api/v1/cards/{uid}/notes                  append a note
//	PATCH  /api/v1/cards/{uid}/notes/{noteId}         edit a note
//	DELETE /api/v1/cards/{uid}/notes/{noteId}         delete a note
//	GET    /api/v1/sprints                            per-team sprint pointers
//	PATCH  /api/v1/sprints                            set a team's pointer directly
//	POST   /api/v1/sprints/actions/carry-over         advance a sprint, carry unfinished (dryRun)
//	POST   /api/v1/sprints/actions/carry-week         pull unfinished plan cards forward (dryRun)
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
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/send-to-review", s.handleSendToReview)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/remove-reviewer", s.handleRemoveReviewer)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/take-into-plan", s.handleTakeIntoPlan)
	mux.HandleFunc("POST /api/v1/cards/{uid}/actions/release-from-plan", s.handleReleaseFromPlan)
	mux.HandleFunc("GET /api/v1/cards/{uid}/links", s.handleListLinks)
	mux.HandleFunc("GET /api/v1/cards/{uid}/notes", s.handleListNotes)
	mux.HandleFunc("POST /api/v1/cards/{uid}/notes", s.handleAddNote)
	mux.HandleFunc("PATCH /api/v1/cards/{uid}/notes/{noteId}", s.handleEditNote)
	mux.HandleFunc("DELETE /api/v1/cards/{uid}/notes/{noteId}", s.handleDeleteNote)
	mux.HandleFunc("GET /api/v1/sprints", s.handleListSprints)
	mux.HandleFunc("PATCH /api/v1/sprints", s.handlePatchSprint)
	mux.HandleFunc("POST /api/v1/sprints/actions/carry-over", s.handleCarryOver)
	mux.HandleFunc("POST /api/v1/sprints/actions/carry-week", s.handleCarryWeek)
	mux.HandleFunc("GET /api/v1/ordering", s.handleGetOrdering)
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
			{"GET", "/api/v1/board", "Board identity and team roster"},
			{"GET", "/api/v1/cards", "List cards; selectors: view=team|me|weekly, team, day, user, week, stage, zone, assignee"},
			{"POST", "/api/v1/cards", "Create a card (joins or starts a sprint; plan cards via spec.plan)"},
			{"GET", "/api/v1/cards/{uid}", "One card"},
			{"PATCH", "/api/v1/cards/{uid}", "Edit spec fields; the server applies clamps, links and date rules"},
			{"DELETE", "/api/v1/cards/{uid}", "Hard delete (cascades to the linked review card)"},
			{"POST", "/api/v1/cards/{uid}/actions/remove", "The smart remove: demote, release or delete by board rules ({from: grid|plan})"},
			{"POST", "/api/v1/cards/{uid}/actions/move", "Reorder after another card ({after}, empty = top)"},
			{"POST", "/api/v1/cards/{uid}/actions/defer", "Push the scheduled day {days} ahead of today"},
			{"POST", "/api/v1/cards/{uid}/actions/in-progress", "Move to the implicit In Progress status"},
			{"POST", "/api/v1/cards/{uid}/actions/send-to-review", "Send to review ({reviewer, day}); reassigns if a review card exists"},
			{"POST", "/api/v1/cards/{uid}/actions/remove-reviewer", "Delete the linked review card"},
			{"POST", "/api/v1/cards/{uid}/actions/take-into-plan", "Take a plan card into work ({engineer, zone, day})"},
			{"POST", "/api/v1/cards/{uid}/actions/release-from-plan", "Release a card from the weekly plan"},
			{"GET", "/api/v1/cards/{uid}/links", "URLs from the card's description; GitHub issue/PR refs resolved with titles, listed first"},
			{"GET", "/api/v1/cards/{uid}/notes", "The card's work notes"},
			{"POST", "/api/v1/cards/{uid}/notes", "Append a work note ({text})"},
			{"PATCH", "/api/v1/cards/{uid}/notes/{noteId}", "Edit a work note ({text})"},
			{"DELETE", "/api/v1/cards/{uid}/notes/{noteId}", "Delete a work note"},
			{"GET", "/api/v1/sprints", "Per-team sprint pointers"},
			{"PATCH", "/api/v1/sprints", "Set a team's sprint pointer directly ({team, current, previous})"},
			{"POST", "/api/v1/sprints/actions/carry-over", "Advance a team's sprint to today, carry unfinished ({team, dryRun})"},
			{"POST", "/api/v1/sprints/actions/carry-week", "Pull unfinished plan cards into the week ({team, week, dryRun})"},
			{"GET", "/api/v1/ordering", "The board-level manual card order"},
			{"GET", "/api/v1/watch", "WebSocket stream of Card/Sprint/Ordering events; selector-scoped with view params"},
		},
	})
}

// boardRef resolves the target board from query parameters, honouring the
// lock-board pin and server defaults.
func (s *Server) boardRef(r *http.Request) (owner string, project int, err error) {
	owner = s.opts.DefaultOwner
	project = s.opts.DefaultProject
	if !s.opts.LockBoard {
		if v := r.URL.Query().Get("owner"); v != "" {
			owner = v
		}
		if v := r.URL.Query().Get("project"); v != "" {
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				return "", 0, fmt.Errorf("invalid project number %q", v)
			}
			project = n
		}
	}
	if owner == "" || project == 0 {
		return "", 0, errMissingBoard
	}
	return owner, project, nil
}

// apiClient builds a Projects v2 client, resolving the token via the same
// source the /api/github proxy uses (OAuth session or local gh CLI).
func (s *Server) apiClient(r *http.Request) (*ghprojects.Client, error) {
	tok, _, err := s.apiTokens(r)
	if err != nil {
		return nil, err
	}
	opts := []ghprojects.Option{ghprojects.WithHTTPClient(s.httpClient)}
	if s.graphqlEndpoint != "" {
		opts = append(opts, ghprojects.WithEndpoint(s.graphqlEndpoint))
	}
	return ghprojects.New(tok, opts...), nil
}

// defaultService is the production newService: it builds a boardservice over the
// per-request ghprojects client (*ghprojects.Client satisfies boardservice.Backend).
func (s *Server) defaultService(r *http.Request) (*boardservice.Service, error) {
	client, err := s.apiClient(r)
	if err != nil {
		return nil, err
	}
	return boardservice.New(&storeBackend{inner: client, store: s.store}), nil
}

// service resolves the board reference and builds the per-request board service.
// On failure it writes the response (400 on a bad ref, 401 on no token) and
// returns ok=false.
func (s *Server) service(w http.ResponseWriter, r *http.Request) (svc *boardservice.Service, owner string, project int, ok bool) {
	owner, project, err := s.boardRef(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, "", 0, false
	}
	svc, err = s.newService(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
		return nil, "", 0, false
	}
	return svc, owner, project, true
}

// statusResponse is the acknowledgement returned by actions that leave no single
// card to echo (delete, remove, move).
type statusResponse struct {
	Status string `json:"status"`
}

// --- Reads -------------------------------------------------------------------

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	b, err := svc.Board(r.Context(), owner, project)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiserver.BoardResource(b))
}

func (s *Server) handleListCards(w http.ResponseWriter, r *http.Request) {
	sel, err := apiserver.ParseSelector(r.URL.Query())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	b, err := svc.Board(r.Context(), owner, project)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiserver.ListCards(b, sel))
}

func (s *Server) handleGetCard(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	s.cardResponse(w, r, svc, owner, project, r.PathValue("uid"))
}

func (s *Server) handleListSprints(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	b, err := svc.Board(r.Context(), owner, project)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":  "SprintList",
		"items": apiserver.SprintResources(b),
	})
}

func (s *Server) handleGetOrdering(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	b, err := svc.Board(r.Context(), owner, project)
	if err != nil {
		s.apiError(w, err)
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
	ReviewOf       string `json:"reviewOf"`
	StartNewSprint *bool  `json:"startNewSprint"`
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
	svc, owner, project, ok := s.service(w, r)
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
		ReviewOf:       in.ReviewOf,
		StartNewSprint: in.StartNewSprint,
	}
	if len(in.Assignees) > 0 {
		args.Assignee = in.Assignees[0]
	}
	if in.Plan != nil {
		band, ok := parsePlanBand(w, in.Plan.Band)
		if !ok {
			return
		}
		args.Plan = band
		args.Week = in.Plan.Week
	}
	card, err := svc.CreateCard(r.Context(), owner, project, args)
	if err != nil {
		s.apiError(w, err)
		return
	}
	b, err := svc.Board(r.Context(), owner, project)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apiserver.CardResource(b, card))
}

// cardPatch is the PATCH /cards/{uid} body: only present fields are applied,
// so absent and empty are different things (empty clears).
type cardPatch struct {
	Title       *string     `json:"title"`
	Description *string     `json:"description"`
	Team        *string     `json:"team"`
	Zone        *string     `json:"zone"`
	Assignees   *[]string   `json:"assignees"`
	Progress    *int        `json:"progress"`
	Stage       *string     `json:"stage"`
	Dates       *datesPatch `json:"dates"`
	Plan        *planPatch  `json:"plan"`
	ReviewOf    *string     `json:"reviewOf"`
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

// handlePatchCard applies a spec patch field by field through the service, so
// every admission rule (clamps, the review-link sync, the review-cancel
// cascade, calendar date semantics) runs exactly as if the UI made the edit.
func (s *Server) handlePatchCard(w http.ResponseWriter, r *http.Request) {
	var p cardPatch
	if !decodeJSON(w, r, &p) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	uid := r.PathValue("uid")
	if p.Title != nil {
		if err := svc.Rename(ctx, owner, project, uid, *p.Title); err != nil {
			s.apiError(w, err)
			return
		}
	}
	if p.Description != nil {
		if err := svc.SetDescription(ctx, owner, project, uid, *p.Description); err != nil {
			s.apiError(w, err)
			return
		}
	}
	if p.Team != nil {
		if err := svc.SetTeam(ctx, owner, project, uid, *p.Team, ""); err != nil {
			s.apiError(w, err)
			return
		}
	}
	if p.Zone != nil {
		zone, ok := parseZone(w, *p.Zone)
		if !ok {
			return
		}
		if err := svc.SetZone(ctx, owner, project, uid, zone); err != nil {
			s.apiError(w, err)
			return
		}
	}
	if p.Assignees != nil {
		login := ""
		if len(*p.Assignees) > 0 {
			login = (*p.Assignees)[0]
		}
		if err := svc.SetAssignee(ctx, owner, project, uid, login); err != nil {
			s.apiError(w, err)
			return
		}
	}
	if p.Stage != nil {
		stage, ok := parseStage(w, *p.Stage)
		if !ok {
			return
		}
		if err := svc.SetStage(ctx, owner, project, uid, stage); err != nil {
			s.apiError(w, err)
			return
		}
	}
	if p.Progress != nil {
		if err := svc.SetProgress(ctx, owner, project, uid, *p.Progress); err != nil {
			s.apiError(w, err)
			return
		}
	}
	if p.Dates != nil {
		if !s.applyDatesPatch(w, r, svc, owner, project, uid, p.Dates) {
			return
		}
	}
	if p.Plan != nil {
		if !s.applyPlanPatch(w, r, svc, owner, project, uid, p.Plan) {
			return
		}
	}
	if p.ReviewOf != nil {
		if err := svc.SetReviewOf(ctx, owner, project, uid, *p.ReviewOf); err != nil {
			s.apiError(w, err)
			return
		}
	}
	s.cardResponse(w, r, svc, owner, project, uid)
}

// applyDatesPatch applies a spec.dates patch. A patched start runs the calendar
// semantics (the sprint follows the sprint active on the start day); patching
// only the end (or only the sprint) stays granular, and an explicit sprint
// always wins. Returns false after writing the error response.
func (s *Server) applyDatesPatch(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, owner string, project int, uid string, d *datesPatch) bool {
	ctx := r.Context()
	if d.Start != nil {
		end := ""
		if d.End != nil {
			end = *d.End
		} else if card, err := svc.Card(ctx, owner, project, uid); err == nil {
			end = card.Day
		}
		if err := svc.SetDates(ctx, owner, project, uid, *d.Start, end); err != nil {
			s.apiError(w, err)
			return false
		}
	} else if d.End != nil {
		if err := svc.SetDay(ctx, owner, project, uid, *d.End); err != nil {
			s.apiError(w, err)
			return false
		}
	}
	if d.Sprint != nil {
		if err := svc.SetSprintStart(ctx, owner, project, uid, *d.Sprint); err != nil {
			s.apiError(w, err)
			return false
		}
	}
	return true
}

// applyPlanPatch applies a spec.plan patch (band and/or week).
func (s *Server) applyPlanPatch(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, owner string, project int, uid string, pl *planPatch) bool {
	ctx := r.Context()
	if pl.Band != nil {
		band, ok := parsePlanBand(w, *pl.Band)
		if !ok {
			return false
		}
		if err := svc.SetPlan(ctx, owner, project, uid, band); err != nil {
			s.apiError(w, err)
			return false
		}
	}
	if pl.Week != nil {
		if err := svc.SetWeek(ctx, owner, project, uid, *pl.Week); err != nil {
			s.apiError(w, err)
			return false
		}
	}
	return true
}

// --- Card actions --------------------------------------------------------------

func (s *Server) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteCard(r.Context(), owner, project, r.PathValue("uid")); err != nil {
		s.apiError(w, err)
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
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.Remove(r.Context(), owner, project, r.PathValue("uid"), in.From); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *Server) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		After string `json:"after"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.MoveCard(r.Context(), owner, project, r.PathValue("uid"), in.After); err != nil {
		s.apiError(w, err)
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
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.Defer(r.Context(), owner, project, uid, in.Days); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, uid)
}

func (s *Server) handleInProgress(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.SetInProgress(r.Context(), owner, project, uid); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, uid)
}

// handleSendToReview sends a card to a reviewer. When a linked review card
// already exists the action reassigns it instead — the backend decides, the
// client just states the intent.
func (s *Server) handleSendToReview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reviewer string `json:"reviewer"`
		Day      string `json:"day"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Reviewer == "" {
		writeJSONError(w, http.StatusUnprocessableEntity, "reviewer is required")
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	uid := r.PathValue("uid")
	b, err := svc.Board(ctx, owner, project)
	if err != nil {
		s.apiError(w, err)
		return
	}
	for _, c := range b.Cards {
		if c.ReviewOf == uid {
			if err := svc.ReassignReviewer(ctx, owner, project, uid, in.Reviewer, in.Day); err != nil {
				s.apiError(w, err)
				return
			}
			s.cardResponse(w, r, svc, owner, project, c.ItemID)
			return
		}
	}
	review, err := svc.SendToReview(ctx, owner, project, uid, in.Reviewer, in.Day)
	if err != nil {
		s.apiError(w, err)
		return
	}
	b, err = svc.Board(ctx, owner, project)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apiserver.CardResource(b, review))
}

func (s *Server) handleRemoveReviewer(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.RemoveReviewer(r.Context(), owner, project, uid); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, uid)
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
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.TakeIntoPlan(r.Context(), owner, project, uid, in.Engineer, zone, in.Day); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, uid)
}

func (s *Server) handleReleaseFromPlan(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.ReleaseFromPlan(r.Context(), owner, project, uid); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, uid)
}

// --- Notes ----------------------------------------------------------------------

// handleListLinks serves the URLs found in a card's description: GitHub
// issue/PR references first (resolved to their titles when possible), plain
// links after.
func (s *Server) handleListLinks(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	links, err := svc.CardLinks(r.Context(), owner, project, r.PathValue("uid"))
	if err != nil {
		s.apiError(w, err)
		return
	}
	if links == nil {
		links = []board.Link{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "LinkList", "items": links})
}

// notesResponse serves a card's notes after any note mutation, so clients
// always converge on the server's view of the thread.
func (s *Server) notesResponse(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, owner string, project int, uid string, status int) {
	card, err := svc.Card(r.Context(), owner, project, uid)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, status, map[string]any{
		"kind":  "NoteList",
		"items": apiserver.NoteResources(card),
	})
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	s.notesResponse(w, r, svc, owner, project, r.PathValue("uid"), http.StatusOK)
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
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.AddNote(r.Context(), owner, project, uid, in.Text); err != nil {
		s.apiError(w, err)
		return
	}
	s.notesResponse(w, r, svc, owner, project, uid, http.StatusCreated)
}

func (s *Server) handleEditNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.EditNote(r.Context(), owner, project, uid, r.PathValue("noteId"), in.Text); err != nil {
		s.apiError(w, err)
		return
	}
	s.notesResponse(w, r, svc, owner, project, uid, http.StatusOK)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.DeleteNote(r.Context(), owner, project, uid, r.PathValue("noteId")); err != nil {
		s.apiError(w, err)
		return
	}
	s.notesResponse(w, r, svc, owner, project, uid, http.StatusOK)
}

// --- Sprints ----------------------------------------------------------------------

func (s *Server) handlePatchSprint(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team     string `json:"team"`
		Current  string `json:"current"`
		Previous string `json:"previous"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.SetSprintState(r.Context(), owner, project, in.Team, in.Current, in.Previous); err != nil {
		s.apiError(w, err)
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
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	rep, err := svc.CarryOver(r.Context(), owner, project, in.Team, in.DryRun)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleCarryWeek(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team   string `json:"team"`
		Week   string `json:"week"`
		DryRun bool   `json:"dryRun"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	rep, err := svc.CarryWeek(r.Context(), owner, project, in.Team, in.Week, in.DryRun)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// --- Shared helpers ----------------------------------------------------------------

// cardResponse loads the (post-mutation) card and writes it as the resource —
// mutations echo the card exactly as a fresh GET would return it.
func (s *Server) cardResponse(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, owner string, project int, uid string) {
	b, err := svc.Board(r.Context(), owner, project)
	if err != nil {
		s.apiError(w, err)
		return
	}
	for _, c := range b.Cards {
		if c.ItemID == uid {
			writeJSON(w, http.StatusOK, apiserver.CardResource(b, c))
			return
		}
	}
	s.apiError(w, fmt.Errorf("%w: %s", boardservice.ErrCardNotFound, uid))
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
func (s *Server) apiError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, boardservice.ErrCardNotFound), errors.Is(err, boardservice.ErrNoteNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ghprojects.ErrFieldNotFound), errors.Is(err, ghprojects.ErrNoContent):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeJSONError(w, http.StatusBadGateway, err.Error())
	}
}
