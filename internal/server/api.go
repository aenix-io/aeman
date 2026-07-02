package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/aenix-org/aeman/internal/board"
	"github.com/aenix-org/aeman/internal/boardservice"
	"github.com/aenix-org/aeman/internal/ghprojects"
)

// errMissingBoard is returned when neither query parameters nor server defaults
// identify a board.
var errMissingBoard = errors.New("owner and project are required (set ?owner=&project= or server defaults)")

// registerAPI wires the native JSON API under /api/v1. The API mirrors the UI's
// board views and actions by calling the boardservice layer (the same logic the
// web frontend's TeamBoard/MeBoard run), not by proxying GitHub directly.
//
// Routes:
//
//	GET    /api/v1                             public route catalog (no auth)
//	GET    /api/v1/board                       board meta + per-team sprint states
//	GET    /api/v1/snapshot                    full board snapshot (the watch LIST)
//	GET    /api/v1/watch                       WebSocket stream of board change events
//	GET    /api/v1/team?team=&day=             Team grid view (day defaults to today)
//	GET    /api/v1/me?user=&day=               personal day view
//	GET    /api/v1/weekly?team=&week=          weekly plan, split into wed/fri bands
//	POST   /api/v1/cards                       create a card (joins/starts a sprint)
//	POST   /api/v1/carry-over                  advance a team's sprint, carry unfinished
//	POST   /api/v1/carry-week                  pull unfinished plan cards into the week
//	POST   /api/v1/sprint-state                set a team's sprint pointer directly
//	POST   /api/v1/cards/{id}/stage            set the stage (locked/review/done/"")
//	POST   /api/v1/cards/{id}/in-progress      move to the implicit In Progress status
//	POST   /api/v1/cards/{id}/progress         set the readiness percentage
//	POST   /api/v1/cards/{id}/zone             set the colour zone
//	POST   /api/v1/cards/{id}/day              set the due day
//	POST   /api/v1/cards/{id}/start            set the scheduled start date
//	POST   /api/v1/cards/{id}/sprint-start     set the sprint the card belongs to
//	POST   /api/v1/cards/{id}/plan             set the weekly-plan band
//	POST   /api/v1/cards/{id}/week             set the plan week
//	POST   /api/v1/cards/{id}/assignee         set/clear the assignee
//	POST   /api/v1/cards/{id}/team             move to a team (joins its sprint)
//	POST   /api/v1/cards/{id}/take-plan        take a plan card into work
//	POST   /api/v1/cards/{id}/release-plan     release a card from the weekly plan
//	POST   /api/v1/cards/{id}/move             reorder a card after another
//	POST   /api/v1/cards/{id}/note             append a work note
//	PATCH  /api/v1/cards/{id}/notes/{noteId}   edit a work note
//	DELETE /api/v1/cards/{id}/notes/{noteId}   delete a work note
//	POST   /api/v1/cards/{id}/description      set the free-form description
//	POST   /api/v1/cards/{id}/rename           rename a card
//	POST   /api/v1/cards/{id}/review           send to review (creates a review card)
//	POST   /api/v1/cards/{id}/review/reassign  point the review at another reviewer
//	POST   /api/v1/cards/{id}/review/remove    delete the linked review card
//	DELETE /api/v1/cards/{id}                  delete a card (cascades to its review)
func (s *Server) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1", s.handleAPIIndex)
	mux.HandleFunc("GET /api/v1/board", s.handleGetBoard)
	mux.HandleFunc("GET /api/v1/team", s.handleTeamView)
	mux.HandleFunc("GET /api/v1/me", s.handleMeView)
	mux.HandleFunc("GET /api/v1/weekly", s.handleWeeklyView)
	mux.HandleFunc("GET /api/v1/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /api/v1/watch", s.handleWatch)
	mux.HandleFunc("POST /api/v1/cards", s.handleCreateCard)
	mux.HandleFunc("POST /api/v1/carry-over", s.handleCarryOver)
	mux.HandleFunc("POST /api/v1/sprint-state", s.handleSetSprintState)
	mux.HandleFunc("POST /api/v1/carry-week", s.handleCarryWeek)
	mux.HandleFunc("DELETE /api/v1/cards/{id}", s.handleDeleteCard)
	mux.HandleFunc("POST /api/v1/cards/{id}/stage", s.handleSetStage)
	mux.HandleFunc("POST /api/v1/cards/{id}/in-progress", s.handleSetInProgress)
	mux.HandleFunc("POST /api/v1/cards/{id}/progress", s.handleSetProgress)
	mux.HandleFunc("POST /api/v1/cards/{id}/zone", s.handleSetZone)
	mux.HandleFunc("POST /api/v1/cards/{id}/day", s.handleSetDay)
	mux.HandleFunc("POST /api/v1/cards/{id}/start", s.handleSetStart)
	mux.HandleFunc("POST /api/v1/cards/{id}/sprint-start", s.handleSetSprintStart)
	mux.HandleFunc("POST /api/v1/cards/{id}/plan", s.handleSetPlan)
	mux.HandleFunc("POST /api/v1/cards/{id}/week", s.handleSetWeek)
	mux.HandleFunc("POST /api/v1/cards/{id}/assignee", s.handleSetAssignee)
	mux.HandleFunc("POST /api/v1/cards/{id}/team", s.handleSetTeam)
	mux.HandleFunc("POST /api/v1/cards/{id}/take-plan", s.handleTakePlan)
	mux.HandleFunc("POST /api/v1/cards/{id}/release-plan", s.handleReleasePlan)
	mux.HandleFunc("POST /api/v1/cards/{id}/move", s.handleMoveCard)
	mux.HandleFunc("POST /api/v1/cards/{id}/note", s.handleAddNote)
	mux.HandleFunc("PATCH /api/v1/cards/{id}/notes/{noteId}", s.handleEditNote)
	mux.HandleFunc("DELETE /api/v1/cards/{id}/notes/{noteId}", s.handleDeleteNote)
	mux.HandleFunc("POST /api/v1/cards/{id}/description", s.handleSetDescription)
	mux.HandleFunc("POST /api/v1/cards/{id}/review-of", s.handleSetReviewOf)
	mux.HandleFunc("POST /api/v1/cards/{id}/rename", s.handleRename)
	mux.HandleFunc("POST /api/v1/cards/{id}/review", s.handleSendToReview)
	mux.HandleFunc("POST /api/v1/cards/{id}/review/reassign", s.handleReassignReviewer)
	mux.HandleFunc("POST /api/v1/cards/{id}/review/remove", s.handleRemoveReviewer)
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
			{"GET", "/api/v1/board", "Board identity, fields and per-team sprint states"},
			{"GET", "/api/v1/snapshot", "Full board snapshot: fields, cards and sprint states"},
			{"GET", "/api/v1/watch", "WebSocket stream of board change events (LIST via /snapshot)"},
			{"GET", "/api/v1/team?team=&day=", "Team grid view (day defaults to today)"},
			{"GET", "/api/v1/me?user=&day=", "Personal day view"},
			{"GET", "/api/v1/weekly?team=&week=", "Weekly plan, split into wed/fri bands"},
			{"POST", "/api/v1/cards", "Create a card (joins or starts a sprint)"},
			{"POST", "/api/v1/carry-over", "Advance a team's sprint, carry unfinished cards"},
			{"POST", "/api/v1/carry-week", "Pull unfinished plan cards into the week"},
			{"POST", "/api/v1/sprint-state", "Set a team's sprint pointer directly"},
			{"DELETE", "/api/v1/cards/{id}", "Delete a card (cascades to its review)"},
			{"POST", "/api/v1/cards/{id}/stage", "Set the stage (locked/review/done/empty)"},
			{"POST", "/api/v1/cards/{id}/in-progress", "Move to the implicit In Progress status"},
			{"POST", "/api/v1/cards/{id}/progress", "Set the readiness percentage"},
			{"POST", "/api/v1/cards/{id}/zone", "Set the colour zone"},
			{"POST", "/api/v1/cards/{id}/day", "Set the due day"},
			{"POST", "/api/v1/cards/{id}/start", "Set the scheduled start date"},
			{"POST", "/api/v1/cards/{id}/sprint-start", "Set the sprint the card belongs to"},
			{"POST", "/api/v1/cards/{id}/plan", "Set the weekly-plan band (wed/fri/empty)"},
			{"POST", "/api/v1/cards/{id}/week", "Set the plan week (a Monday)"},
			{"POST", "/api/v1/cards/{id}/assignee", "Set or clear the assignee"},
			{"POST", "/api/v1/cards/{id}/team", "Move to a team (joins its sprint)"},
			{"POST", "/api/v1/cards/{id}/take-plan", "Take a plan card into work"},
			{"POST", "/api/v1/cards/{id}/release-plan", "Release a card from the weekly plan"},
			{"POST", "/api/v1/cards/{id}/move", "Reorder a card after another"},
			{"POST", "/api/v1/cards/{id}/note", "Append a work note"},
			{"PATCH", "/api/v1/cards/{id}/notes/{noteId}", "Edit a work note"},
			{"DELETE", "/api/v1/cards/{id}/notes/{noteId}", "Delete a work note"},
			{"POST", "/api/v1/cards/{id}/description", "Set the free-form description"},
			{"POST", "/api/v1/cards/{id}/review-of", "Set or clear the review link"},
			{"POST", "/api/v1/cards/{id}/rename", "Rename a card"},
			{"POST", "/api/v1/cards/{id}/review", "Send to review (creates a review card)"},
			{"POST", "/api/v1/cards/{id}/review/reassign", "Point the review at another reviewer"},
			{"POST", "/api/v1/cards/{id}/review/remove", "Delete the linked review card"},
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

// boardMeta is the GET /api/v1/board response: board identity, field metadata
// and the per-team sprint pointers.
type boardMeta struct {
	ID           string                       `json:"id"`
	Number       int                          `json:"number"`
	Owner        string                       `json:"owner"`
	Fields       []board.ProjectField         `json:"fields"`
	SprintStates map[string]board.SprintState `json:"sprintStates"`
}

// cardsResponse wraps a list of cards returned by a view.
type cardsResponse struct {
	Cards []board.Card `json:"cards"`
}

// statusResponse is the acknowledgement returned by actions that leave no single
// card to echo (delete, release, carry-over).
type statusResponse struct {
	Status string `json:"status"`
	ItemID string `json:"itemId,omitempty"`
}

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
	writeJSON(w, http.StatusOK, boardMeta{
		ID:           b.ID,
		Number:       b.Number,
		Owner:        b.Owner,
		Fields:       b.Fields,
		SprintStates: b.SprintStates,
	})
}

func (s *Server) handleTeamView(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cards, err := svc.TeamView(r.Context(), owner, project, q.Get("team"), q.Get("day"))
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cardsResponse{Cards: cards})
}

func (s *Server) handleMeView(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cards, err := svc.MeView(r.Context(), owner, project, q.Get("user"), q.Get("day"))
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cardsResponse{Cards: cards})
}

func (s *Server) handleWeeklyView(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	bands, err := svc.WeeklyPlan(r.Context(), owner, project, q.Get("team"), q.Get("week"))
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bands)
}

func (s *Server) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team           string `json:"team"`
		Zone           string `json:"zone"`
		Title          string `json:"title"`
		Assignee       string `json:"assignee"`
		Day            string `json:"day"`
		Start          string `json:"start"`
		SprintStart    string `json:"sprintStart"`
		Plan           string `json:"plan"`
		Week           string `json:"week"`
		ReviewOf       string `json:"reviewOf"`
		StartNewSprint *bool  `json:"startNewSprint"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Title == "" {
		writeJSONError(w, http.StatusBadRequest, "title is required")
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	card, err := svc.CreateCard(r.Context(), owner, project, boardservice.CreateCardArgs{
		Team:           in.Team,
		Zone:           board.ZoneKey(in.Zone),
		Title:          in.Title,
		Assignee:       in.Assignee,
		Day:            in.Day,
		Start:          in.Start,
		SprintStart:    in.SprintStart,
		Plan:           board.PlanBand(in.Plan),
		Week:           in.Week,
		ReviewOf:       in.ReviewOf,
		StartNewSprint: in.StartNewSprint,
	})
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

func (s *Server) handleCarryOver(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team string `json:"team"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.CarryOver(r.Context(), owner, project, in.Team); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *Server) handleCarryWeek(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team string `json:"team"`
		Week string `json:"week"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	carried, err := svc.CarryWeek(r.Context(), owner, project, in.Team, in.Week)
	if err != nil {
		s.apiError(w, err)
		return
	}
	if carried == nil {
		carried = []board.Card{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"carried": carried})
}

func (s *Server) handleSetStage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Stage string `json:"stage"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetStage(r.Context(), owner, project, id, board.StageKey(in.Stage)); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetInProgress(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetInProgress(r.Context(), owner, project, id); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetProgress(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Progress int `json:"progress"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetProgress(r.Context(), owner, project, id, in.Progress); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetZone(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Zone board.ZoneKey `json:"zone"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetZone(r.Context(), owner, project, id, in.Zone); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetDay(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Day string `json:"day"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetDay(r.Context(), owner, project, id, in.Day); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Start string `json:"start"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetStart(r.Context(), owner, project, id, in.Start); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetSprintStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SprintStart string `json:"sprintStart"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetSprintStart(r.Context(), owner, project, id, in.SprintStart); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Plan board.PlanBand `json:"plan"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetPlan(r.Context(), owner, project, id, in.Plan); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetWeek(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Week string `json:"week"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetWeek(r.Context(), owner, project, id, in.Week); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetDescription(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetDescription(r.Context(), owner, project, id, in.Description); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
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
	id := r.PathValue("id")
	if err := svc.EditNote(r.Context(), owner, project, id, r.PathValue("noteId"), in.Text); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.DeleteNote(r.Context(), owner, project, id, r.PathValue("noteId")); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetReviewOf(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ReviewOf string `json:"reviewOf"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetReviewOf(r.Context(), owner, project, id, in.ReviewOf); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetSprintState(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *Server) handleSetAssignee(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login string `json:"login"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetAssignee(r.Context(), owner, project, id, in.Login); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSetTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team string `json:"team"`
		Day  string `json:"day"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.SetTeam(r.Context(), owner, project, id, in.Team, in.Day); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleTakePlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Engineer string `json:"engineer"`
		Zone     string `json:"zone"`
		Day      string `json:"day"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.TakeIntoPlan(r.Context(), owner, project, id, in.Engineer, board.ZoneKey(in.Zone), in.Day); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleReleasePlan(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.ReleaseFromPlan(r.Context(), owner, project, id); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok", ItemID: id})
}

func (s *Server) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AfterID string `json:"afterId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.MoveCard(r.Context(), owner, project, id, in.AfterID); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Text == "" {
		writeJSONError(w, http.StatusBadRequest, "text is required")
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.AddNote(r.Context(), owner, project, id, in.Text); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok", ItemID: id})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Title == "" {
		writeJSONError(w, http.StatusBadRequest, "title is required")
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.Rename(r.Context(), owner, project, id, in.Title); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleSendToReview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reviewer string `json:"reviewer"`
		Day      string `json:"day"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	review, err := svc.SendToReview(r.Context(), owner, project, id, in.Reviewer, in.Day)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, review)
}

func (s *Server) handleReassignReviewer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reviewer string `json:"reviewer"`
		Day      string `json:"day"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.ReassignReviewer(r.Context(), owner, project, id, in.Reviewer, in.Day); err != nil {
		s.apiError(w, err)
		return
	}
	s.cardResponse(w, r, svc, owner, project, id)
}

func (s *Server) handleRemoveReviewer(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.RemoveReviewer(r.Context(), owner, project, id); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok", ItemID: id})
}

func (s *Server) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.DeleteCard(r.Context(), owner, project, id); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok", ItemID: id})
}

// cardResponse loads and returns the single card resulting from an action, so
// the API reply reflects the card the way the UI re-renders the one it changed.
func (s *Server) cardResponse(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, owner string, project int, id string) {
	card, err := svc.Card(r.Context(), owner, project, id)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

// decodeJSON decodes a JSON request body, writing a 400 on failure. An empty
// body is treated as an empty object.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// apiError maps a boardservice/ghprojects error onto an HTTP status and JSON body.
func (s *Server) apiError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, boardservice.ErrCardNotFound),
		errors.Is(err, boardservice.ErrNoteNotFound),
		errors.Is(err, ghprojects.ErrBoardNotFound),
		errors.Is(err, ghprojects.ErrCardNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ghprojects.ErrFieldNotFound), errors.Is(err, ghprojects.ErrNoContent):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		s.log.Error("api error", "err", err)
		writeJSONError(w, http.StatusBadGateway, err.Error())
	}
}
