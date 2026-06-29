package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/aenix-org/aeman/internal/ghprojects"
)

// errMissingBoard is returned when neither query parameters nor server defaults
// identify a board.
var errMissingBoard = errors.New("owner and project are required (set ?owner=&project= or server defaults)")

// registerAPI wires the native JSON API under /api/v1.
func (s *Server) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/board", s.handleGetBoard)
	mux.HandleFunc("GET /api/v1/cards", s.handleListCards)
	mux.HandleFunc("POST /api/v1/cards", s.handleCreateCard)
	mux.HandleFunc("PATCH /api/v1/cards/{id}", s.handleUpdateCard)
	mux.HandleFunc("POST /api/v1/cards/{id}/move", s.handleMoveCard)
	mux.HandleFunc("DELETE /api/v1/cards/{id}", s.handleDeleteCard)
	mux.HandleFunc("POST /api/v1/cards/{id}/notes", s.handleAddNote)
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

// loadBoard resolves the board reference, builds a client and loads the board.
// On failure it writes an error response and returns ok=false.
func (s *Server) loadBoard(w http.ResponseWriter, r *http.Request) (*ghprojects.Client, *ghprojects.Board, bool) {
	owner, project, err := s.boardRef(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}
	client, err := s.apiClient(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
		return nil, nil, false
	}
	board, err := client.LoadBoard(r.Context(), owner, project)
	if err != nil {
		s.apiError(w, err)
		return nil, nil, false
	}
	return client, board, true
}

// boardMeta is the /api/v1/board response: identity plus field metadata.
type boardMeta struct {
	ID     string                    `json:"id"`
	Number int                       `json:"number"`
	Title  string                    `json:"title"`
	URL    string                    `json:"url"`
	Owner  string                    `json:"owner"`
	Fields []ghprojects.ProjectField `json:"fields"`
}

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	_, board, ok := s.loadBoard(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, boardMeta{
		ID:     board.ID,
		Number: board.Number,
		Title:  board.Title,
		URL:    board.URL,
		Owner:  board.Owner,
		Fields: board.Fields,
	})
}

func (s *Server) handleListCards(w http.ResponseWriter, r *http.Request) {
	_, board, ok := s.loadBoard(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": board.Cards})
}

func (s *Server) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	var in ghprojects.CreateCardInput
	if !decodeJSON(w, r, &in) {
		return
	}
	client, board, ok := s.loadBoard(w, r)
	if !ok {
		return
	}
	card, err := client.CreateCard(r.Context(), board, in)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

func (s *Server) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	var in ghprojects.UpdateCardInput
	if !decodeJSON(w, r, &in) {
		return
	}
	client, board, ok := s.loadBoard(w, r)
	if !ok {
		return
	}
	if err := client.UpdateCard(r.Context(), board, r.PathValue("id"), in); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AfterID *string `json:"afterId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	client, board, ok := s.loadBoard(w, r)
	if !ok {
		return
	}
	if err := client.MoveCard(r.Context(), board, r.PathValue("id"), in.AfterID); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	client, board, ok := s.loadBoard(w, r)
	if !ok {
		return
	}
	if err := client.DeleteCard(r.Context(), board, r.PathValue("id")); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	client, board, ok := s.loadBoard(w, r)
	if !ok {
		return
	}
	if err := client.AddNote(r.Context(), board, r.PathValue("id"), in.Text); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

// apiError maps a ghprojects error onto an HTTP status and JSON body.
func (s *Server) apiError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ghprojects.ErrBoardNotFound), errors.Is(err, ghprojects.ErrCardNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ghprojects.ErrFieldNotFound), errors.Is(err, ghprojects.ErrNoContent):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		s.log.Error("api error", "err", err)
		writeJSONError(w, http.StatusBadGateway, err.Error())
	}
}
