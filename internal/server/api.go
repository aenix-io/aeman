package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/aenix-io/aeman/api"
	"github.com/aenix-io/aeman/internal/server/apiv1"
	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// registerAPI wires the JSON API under /api/v1. The operations come from
// api/openapi.yaml through the code generated from it (registerStrict), so
// the document is the only description of the surface and a handler cannot
// drift from it. Registered here by hand: the index and the watch, which a
// generated operation cannot be, and the catch-all that closes the API off
// from the page behind it.
//
// The BOARD a caller is standing on is a path segment; the card keeps its own
// address (docs/design/view-scoped-api.md).
func (s *Server) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1", s.handleAPIIndex)
	mux.HandleFunc("GET /api/v1/views/{view}/watch", s.handleWatch)
	s.registerStrict(mux)
	// Last, and without a method, so every more specific pattern above wins:
	// what is left is a path no route serves, and the SPA's catch-all used to
	// answer it with index.html — 200 and a page, to a caller asking for JSON.
	mux.HandleFunc("/api/v1/", s.handleUnknownRoute)
}

// handleUnknownRoute closes the API off from the page behind it. Both halves
// of the request are echoed so a caller sees which one missed, and both are
// clipped: the METHOD is as much the caller's own bytes as the path — the
// grammar bounds neither — so bounding one leaves the sentence as long as the
// other.
func (s *Server) handleUnknownRoute(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, problem(http.StatusNotFound, "unknownRoute",
		"no such route: "+clipForDetail(r.Method)+" "+clipForDetail(r.URL.Path)))
}

// clipForDetail bounds one piece of a request on its way into a problem. The
// cut lands on a rune boundary: splitting a multi-byte rune leaves half a
// character, which is not text and reaches the reader as U+FFFD.
func clipForDetail(s string) string {
	if len(s) <= maxDetailPart {
		return s
	}
	cut := maxDetailPart
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// maxDetailPart bounds each echoed piece. Long enough for any route the
// document describes, short enough that a refusal stays a sentence.
const maxDetailPart = 120

// apiIndex is the GET /api/v1 answer: identity, the MCP mount point and where
// the description of this surface lives. It carries no board data, so it needs
// no board service.
type apiIndex struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	MCP     string `json:"mcp"`
	OpenAPI string `json:"openapi"`
}

// handleAPIIndex serves the index: what this server is, and where to read
// what it can do.
func (s *Server) handleAPIIndex(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, apiIndex{
		Name:    "aeman",
		Version: s.opts.Version,
		MCP:     "/mcp",
		OpenAPI: "/api/v1/openapi.json",
	})
}

// specJSON is the embedded document as JSON, converted once: the file is YAML
// so a person can read and edit it, and JSON is what a client's tooling reads.
var specJSON = sync.OnceValues(func() ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(api.Spec, &doc); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
})

// specResponse is the converted document on its way out, written as it stands.
// The operation's schema is a bare object, so the generated response would
// decode it into a map and encode that back per request.
type specResponse []byte

func (d specResponse) VisitGetOpenAPIResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, err := w.Write(d)
	return err
}

// GetOpenAPI serves the description of this surface. Like the index it
// touches no board service: what the routes ARE is not board data, and a
// client generating itself from the document has nothing to be authorized
// for yet.
func (a surface) GetOpenAPI(context.Context, apiv1.GetOpenAPIRequestObject) (apiv1.GetOpenAPIResponseObject, error) {
	data, err := specJSON()
	if err != nil {
		return nil, err
	}
	return specResponse(data), nil
}

// boardRef is the board a request addresses: the one this server is
// configured with. A client's board query parameter is ignored;
// "project" in the API is aeman's own planning entity (a group of epic
// columns on the Project board) and is a card filter, not an address.
func (s *Server) boardRef() (boardID string) {
	return s.gitBoard()
}

// defaultService is the production newService: the board service over the
// one shared store, as the visitor may use it — the request brings its
// identity (actor), its action and its rights; the visible backend projects
// reads and checks writes against those.
func (s *Server) defaultService() (*boardservice.Service, error) {
	if s.visibleBE == nil {
		return nil, errNoBoard
	}
	return boardservice.New(s.visibleBE), nil
}

// errNoBoard is a server started without a repository.
var errNoBoard = errors.New("no board is configured: start aeman with --repo")

// serviceOf resolves the board reference and builds the per-request board
// service, or the 401 a caller without a usable credential gets.
func (s *Server) serviceOf() (svc *boardservice.Service, boardID string, err error) {
	svc, err = s.newService()
	if err != nil {
		return nil, "", notAuthenticated(err)
	}
	return svc, s.boardRef(), nil
}

// service is serviceOf for a handler that still writes its own response; it
// goes with the last of them.
func (s *Server) service(w http.ResponseWriter, _ *http.Request) (svc *boardservice.Service, boardID string, ok bool) {
	svc, boardID, err := s.serviceOf()
	if err != nil {
		writeProblem(w, problemFor(err))
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
		writeProblem(w, problem(http.StatusBadRequest, "tooManyCards",
			fmt.Sprintf("uids: at most %d cards per request", maxDayLogCards)))
		return
	}
	day := r.URL.Query().Get("day")
	if day != "" && !board.IsDayIso(day) {
		writeProblem(w, problem(http.StatusBadRequest, "invalidDay", "day: want yyyy-mm-dd"))
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
		info.Metadata.Domains = s.domainsFor(r.Context(), logins)
	}
	writeJSON(w, http.StatusOK, info)
}

// domainsFor lists the visitor's readable domains, primary first, with
// whether they may write each and which of the board's members can read it
// (G16). Rights come from the request; who else reads a domain is the
// forge's answer with the server credential.
func (s *Server) domainsFor(ctx context.Context, members []string) []apiserver.DomainInfo {
	rights := rightsFrom(ctx)
	out := make([]apiserver.DomainInfo, 0, len(s.gitCfg.Repos))
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
			// A copy of the empty slice, never of nil: the readers are a
			// list on the wire, and a board nobody is assigned anything on
			// would otherwise answer null.
			out = append(out, apiserver.DomainInfo{Name: d.Name, Writable: rights.canWrite(d.Name), Members: append([]string{}, members...)})
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
	return out
}

func (s *Server) handleListCards(w http.ResponseWriter, r *http.Request) {
	view, ok := s.viewOf(w, r)
	if !ok {
		return
	}
	// The board is the path segment, and "who am I" is resolved inside
	// selectorOf: a Me request needs no user (an explicit ?user= still wins,
	// for a lead looking at somebody else's day).
	sel, ok := s.selectorOf(w, r, view)
	if !ok {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
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
	// board segment at all, and every reader of them is a day board asking
	// for its own moment.
	// The board a read belongs to: the path segment where there is one, else
	// the query — /board and /sprints have no segment and are asked for a
	// day by the board that is open, which says which view it is.
	view := r.PathValue("view")
	if view == "" {
		view = q.Get("view")
	}
	if !asked || (view != "" && !board.HasRecords(view)) {
		day = ""
	}
	bd, records, at, err := svc.BoardOfDay(r.Context(), boardID, day)
	if err != nil || at.IsZero() {
		return bd, "", nil, err
	}
	return bd, at.Format(time.RFC3339), records, nil
}

// GetCard is one card in full: the body lives here, not in listings.
func (a surface) GetCard(ctx context.Context, req apiv1.GetCardRequestObject) (apiv1.GetCardResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	card, err := a.s.cardOf(ctx, svc, boardID, req.UID)
	if err != nil {
		return nil, err
	}
	return apiv1.GetCard200JSONResponse(card), nil
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

// --- Create / patch ------------------------------------------------------------

// createCardRequest mirrors the Card spec shape for creation.
type createCardRequest struct {
	Title     string   `json:"title"`
	Team      string   `json:"team"`
	Zone      string   `json:"zone"`
	Size      string   `json:"size"`
	Assignees []string `json:"assignees"`
	Dates     struct {
		Start  string `json:"start"`
		End    string `json:"end"`
		Sprint string `json:"sprint"`
	} `json:"dates"`
	// Week schedules the card for a WEEK (its Monday) instead of a day: no
	// dates are set and no sprint is joined.
	Week string `json:"week"`
	// Parked puts the card straight on its team's SHELF. Like week, no dates
	// are set and no sprint is joined — a parked card is on no day.
	Parked bool `json:"parked"`
	// Epic + Project file the card on the Project board, under the column that
	// pair identifies. Its row is the week of dates.start — week is what
	// anchors it when there are no dates — and dates may span weeks.
	Epic           string `json:"epic"`
	CardProject    string `json:"project"`
	ReviewOf       string `json:"reviewOf"`
	Parent         string `json:"parent"`
	StartNewSprint *bool  `json:"startNewSprint"`
	// NoSprint schedules the card for its day without joining any sprint (a
	// "next sprint" create); the next carry-over to reach its day adopts it.
	NoSprint bool `json:"noSprint"`
}

func (s *Server) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	view, viewOK := s.viewOf(w, r)
	if !viewOK {
		return
	}
	var in createCardRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	zone, err := parseZone(in.Zone)
	if err != nil {
		writeProblem(w, problemFor(err))
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	// Made from a past day: judged by the team the card is being added to,
	// which the guard could not read (recordWriteGuard). A team still inside
	// that sprint is still working those days — the lead reading the day the
	// sprint began adds a card there — and only a team the day is OVER for is
	// refused.
	if day := asOfDay(r.Context()); day != "" {
		if bd, err := svc.Board(r.Context(), boardID); err == nil && board.TeamsPast(bd, day)[in.Team] {
			team := in.Team
			if team == "" {
				team = "no team"
			}
			writeProblem(w, problem(http.StatusConflict, "dayIsARecord",
				"the board of "+day+" is a record for «"+team+"»: that day is over for them, so nothing can be added to it from there"))
			return
		}
	}
	size, err := parseSize(in.Size)
	if err != nil {
		writeProblem(w, problemFor(err))
		return
	}
	args := boardservice.CreateCardArgs{
		Team:           in.Team,
		Zone:           zone,
		Size:           size,
		Title:          in.Title,
		Day:            in.Dates.End,
		Start:          in.Dates.Start,
		SprintStart:    in.Dates.Sprint,
		Epic:           in.Epic,
		Project:        in.CardProject,
		ReviewOf:       in.ReviewOf,
		Parent:         in.Parent,
		StartNewSprint: in.StartNewSprint,
		NoSprint:       in.NoSprint,
		Parked:         in.Parked,
	}
	if len(in.Assignees) > 0 {
		args.Assignee = in.Assignees[0]
	}
	if in.Week != "" {
		args.Week = in.Week
	}
	card, err := svc.CreateInView(r.Context(), boardID, view, args)
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

// applyGroupingPatch is the parent/process fragment of a card patch: what
// the card is grouped under.
func applyGroupingPatch(ctx context.Context, svc *boardservice.Service, boardID, uid string, p *apiv1.CardPatch) error {
	if p.Parent != nil {
		if err := svc.SetParent(ctx, boardID, uid, *p.Parent); err != nil {
			return err
		}
	}
	if p.Process != nil {
		if err := svc.SetCardProcess(ctx, boardID, uid, *p.Process); err != nil {
			return err
		}
	}
	return nil
}

// PatchCard applies a spec patch field by field through the service, so every
// admission rule (clamps, the review-link sync, the review-cancel cascade,
// calendar date semantics) runs exactly as if the UI made the edit.
func (a surface) PatchCard(ctx context.Context, req apiv1.PatchCardRequestObject) (apiv1.PatchCardResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	uid, p := req.UID, req.Body
	if p.Title != nil {
		if err := svc.Rename(ctx, boardID, uid, *p.Title); err != nil {
			return nil, err
		}
	}
	if p.Description != nil {
		if err := svc.SetDescription(ctx, boardID, uid, *p.Description); err != nil {
			return nil, err
		}
	}
	if p.Team != nil {
		if err := svc.SetTeam(ctx, boardID, uid, *p.Team, ""); err != nil {
			return nil, err
		}
	}
	if p.Epic != nil || p.Project != nil {
		if err := patchColumn(ctx, svc, boardID, uid, p); err != nil {
			return nil, err
		}
	}
	if err := patchZoneAndSize(ctx, svc, boardID, uid, p); err != nil {
		return nil, err
	}
	if p.Stage != nil {
		stage, err := parseStage(string(*p.Stage))
		if err != nil {
			return nil, err
		}
		if err := svc.SetStage(ctx, boardID, uid, stage); err != nil {
			return nil, err
		}
	}
	if p.Recurrence != nil {
		if err := svc.SetRecurrence(ctx, boardID, uid, *p.Recurrence); err != nil {
			return nil, err
		}
	}
	if p.Progress != nil {
		if err := svc.SetProgress(ctx, boardID, uid, *p.Progress); err != nil {
			return nil, err
		}
	}
	if p.Dates != nil {
		if err := applyDatesPatch(ctx, svc, boardID, uid, p.Dates); err != nil {
			return nil, err
		}
	}
	if err := applyPlacementPatch(ctx, svc, boardID, uid, p); err != nil {
		return nil, err
	}
	if err := applyGroupingPatch(ctx, svc, boardID, uid, p); err != nil {
		return nil, err
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
			return nil, err
		}
	}
	if p.ReviewOf != nil {
		if err := svc.SetReviewOf(ctx, boardID, uid, *p.ReviewOf); err != nil {
			return nil, err
		}
	}
	card, err := a.s.cardOf(ctx, svc, boardID, uid)
	if err != nil {
		return nil, err
	}
	return apiv1.PatchCard200JSONResponse(card), nil
}

// applyDatesPatch applies a spec.dates patch. A patched start runs the calendar
// semantics (the sprint follows the sprint active on the start day); patching
// only the end (or only the sprint) stays granular, and an explicit sprint
// always wins.
func applyDatesPatch(ctx context.Context, svc *boardservice.Service, boardID, uid string, d *apiv1.CardDatesPatch) error {
	if d.Start != nil {
		end := ""
		if d.End != nil {
			end = *d.End
		} else if card, err := svc.Card(ctx, boardID, uid); err == nil {
			end = card.Day
		}
		if err := svc.SetDates(ctx, boardID, uid, *d.Start, end); err != nil {
			return err
		}
	} else if d.End != nil {
		if err := svc.SetDay(ctx, boardID, uid, *d.End); err != nil {
			return err
		}
	}
	if d.Sprint != nil {
		if err := svc.SetSprintStart(ctx, boardID, uid, *d.Sprint); err != nil {
			return err
		}
	}
	return nil
}

// --- Card actions --------------------------------------------------------------

// DeleteCard is the hard delete, cascading to the linked review card.
func (a surface) DeleteCard(ctx context.Context, req apiv1.DeleteCardRequestObject) (apiv1.DeleteCardResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	if err := svc.DeleteCard(ctx, boardID, req.UID); err != nil {
		return nil, err
	}
	return apiv1.DeleteCard204Response{}, nil
}

func (s *Server) handleRemoveCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Intent string `json:"intent"`
	}
	// The body is read whenever there is one to read. Asking ContentLength
	// instead skipped a CHUNKED body, which has no length to declare (-1), so
	// an explicit "unassign" silently became the intentless gesture — and the
	// gesture deletes where unassign refuses to. An empty body is the
	// intentless call, which is the same as sending none.
	if r.Body != nil && r.ContentLength != 0 && !decodeJSONAllowingEmpty(w, r, &in) {
		return
	}
	intent := boardservice.RemoveIntent(in.Intent)
	switch intent {
	case boardservice.RemoveAuto, boardservice.Unassign, boardservice.OffBoard:
	default:
		// Before the gate: an intent the × does not have is answered by
		// reading the body, not by building the board's listing first.
		writeProblem(w, problem(http.StatusBadRequest, "unknownIntent",
			"unknown intent (use unassign, off-board, or leave it out)"))
		return
	}
	view, ok := s.viewOf(w, r)
	if !ok {
		return
	}
	if !s.gestureOn(w, r, view, boardservice.GestureRemove) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	if err := svc.Remove(r.Context(), boardID, r.PathValue("uid"), view, intent); err != nil {
		s.apiError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MoveCard reorders the card after (or before) another one.
func (a surface) MoveCard(ctx context.Context, req apiv1.MoveCardRequestObject) (apiv1.MoveCardResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	if before := strOf(req.Body.Before); before != "" {
		err = svc.MoveCardBefore(ctx, boardID, req.UID, before)
	} else {
		err = svc.MoveCard(ctx, boardID, req.UID, strOf(req.Body.After))
	}
	if err != nil {
		return nil, err
	}
	return apiv1.MoveCard204Response{}, nil
}

// DeferCard pushes the scheduled day N days ahead of today.
func (a surface) DeferCard(ctx context.Context, req apiv1.DeferCardRequestObject) (apiv1.DeferCardResponseObject, error) {
	card, err := a.cardAfter(ctx, req.UID, func(svc *boardservice.Service, boardID string) error {
		return svc.Defer(ctx, boardID, req.UID, intOf(req.Body.Days))
	})
	if err != nil {
		return nil, err
	}
	return apiv1.DeferCard200JSONResponse(card), nil
}

// SetInProgress moves the card to the implicit In Progress status.
func (a surface) SetInProgress(ctx context.Context, req apiv1.SetInProgressRequestObject) (apiv1.SetInProgressResponseObject, error) {
	card, err := a.cardAfter(ctx, req.UID, func(svc *boardservice.Service, boardID string) error {
		return svc.SetInProgress(ctx, boardID, req.UID)
	})
	if err != nil {
		return nil, err
	}
	return apiv1.SetInProgress200JSONResponse(card), nil
}

// ReopenCard undoes a done mark, restoring the progress the card had.
func (a surface) ReopenCard(ctx context.Context, req apiv1.ReopenCardRequestObject) (apiv1.ReopenCardResponseObject, error) {
	card, err := a.cardAfter(ctx, req.UID, func(svc *boardservice.Service, boardID string) error {
		return svc.Reopen(ctx, boardID, req.UID)
	})
	if err != nil {
		return nil, err
	}
	return apiv1.ReopenCard200JSONResponse(card), nil
}

// placement runs one of the three column actions. A column is named by its
// EPIC, and the project half may be empty everywhere: the no-project bucket is
// a column like any other — a mirror home included, since a column's
// repository is read off the column's own stub and not off a project
// (G15/G59). This door used to require a project for a mirror, so a placement
// the service, the codec, MCP and the docs all accept was a 422 here — and the
// SPA's own picker, which offers the bucket, drove straight into it.
func (a surface) placement(in apiv1.PlacementBody,
	act func(svc *boardservice.Service, boardID, project, epic string) error,
) (*boardservice.Service, string, error) {
	if in.Epic == "" {
		return nil, "", problem(http.StatusUnprocessableEntity, "epicRequired",
			"the epic is required — a column is named by its epic")
	}
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, "", err
	}
	if err := act(svc, boardID, strOf(in.Project), in.Epic); err != nil {
		return nil, "", err
	}
	return svc, boardID, nil
}

// MirrorCard adds a second Project-board column to the card — the same card
// shown in both projects, one file and one log.
func (a surface) MirrorCard(ctx context.Context, req apiv1.MirrorCardRequestObject) (apiv1.MirrorCardResponseObject, error) {
	svc, boardID, err := a.placement(*req.Body,
		func(svc *boardservice.Service, boardID, project, epic string) error {
			return svc.Mirror(ctx, boardID, req.UID, project, epic)
		})
	if err != nil {
		return nil, err
	}
	card, err := a.s.cardOf(ctx, svc, boardID, req.UID)
	if err != nil {
		return nil, err
	}
	return apiv1.MirrorCard200JSONResponse(card), nil
}

// UnmirrorCard takes one mirror column away.
func (a surface) UnmirrorCard(ctx context.Context, req apiv1.UnmirrorCardRequestObject) (apiv1.UnmirrorCardResponseObject, error) {
	svc, boardID, err := a.placement(*req.Body,
		func(svc *boardservice.Service, boardID, project, epic string) error {
			return svc.Unmirror(ctx, boardID, req.UID, project, epic)
		})
	if err != nil {
		return nil, err
	}
	card, err := a.s.cardOf(ctx, svc, boardID, req.UID)
	if err != nil {
		return nil, err
	}
	return apiv1.UnmirrorCard200JSONResponse(card), nil
}

// RemoveFromProject is the Project board's ×: remove the card from one column,
// with the mirror/promote/last-column rules of the service. It cannot answer
// with the card the way mirror and unmirror do — its card may no longer exist.
func (a surface) RemoveFromProject(ctx context.Context, req apiv1.RemoveFromProjectRequestObject) (apiv1.RemoveFromProjectResponseObject, error) {
	if _, _, err := a.placement(*req.Body,
		func(svc *boardservice.Service, boardID, project, epic string) error {
			return svc.RemoveFromProject(ctx, boardID, req.UID, project, epic)
		}); err != nil {
		return nil, err
	}
	return apiv1.RemoveFromProject204Response{}, nil
}

// SendToReview sends a card to a reviewer. When a linked review card already
// exists the action reassigns it instead — the SERVICE decides, so the other
// door cannot grow a second review card on the same original; the client just
// states the intent.
func (a surface) SendToReview(ctx context.Context, req apiv1.SendToReviewRequestObject) (apiv1.SendToReviewResponseObject, error) {
	zone, err := parseZone(zoneOf(req.Body.Zone))
	if err != nil {
		return nil, err
	}
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	review, err := svc.SendToReview(ctx, boardID, req.UID, strOf(req.Body.Reviewer), strOf(req.Body.Day), zone)
	if err != nil {
		return nil, err
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return apiv1.SendToReview201JSONResponse(apiserver.CardResource(b, review)), nil
}

// zoneOf reads an optional zone field. Absent is the empty name, and what
// that means is the door's own: a review keeps the original's band, a create
// leaves the band to the board's rule.
func zoneOf[T ~string](z *T) string {
	if z == nil {
		return ""
	}
	return string(*z)
}

// RemoveReviewer deletes the linked review card.
func (a surface) RemoveReviewer(ctx context.Context, req apiv1.RemoveReviewerRequestObject) (apiv1.RemoveReviewerResponseObject, error) {
	card, err := a.cardAfter(ctx, req.UID, func(svc *boardservice.Service, boardID string) error {
		return svc.RemoveReviewer(ctx, boardID, req.UID)
	})
	if err != nil {
		return nil, err
	}
	return apiv1.RemoveReviewer200JSONResponse(card), nil
}

// handlePlaceCard puts a card in a week of the Triage board, which is what
// triaging it means (docs/design/triage.md).
func (s *Server) handlePlaceCard(w http.ResponseWriter, r *http.Request) {
	view, ok := s.viewOf(w, r)
	if !ok {
		return
	}
	if !s.gestureOn(w, r, view, boardservice.GesturePlace) {
		return
	}
	var in struct {
		Week string `json:"week"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.Place(r.Context(), boardID, uid, in.Week); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

// handleFinishedEarlier sends finished work back to the sprint it was done
// in — its dates and the day it counts as done along with it. It is a MOVE and
// not a removal, so it has a door of its own: Remove's law is about emptying
// the working area, and this fills a different one.
func (s *Server) handleFinishedEarlier(w http.ResponseWriter, r *http.Request) {
	view, ok := s.viewOf(w, r)
	if !ok {
		return
	}
	if !s.gestureOn(w, r, view, boardservice.GestureFinishedEarlier) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.FinishedEarlier(r.Context(), boardID, uid); err != nil {
		s.apiError(w, r, err)
		return
	}
	s.cardResponse(w, r, svc, boardID, uid)
}

// handleUntriageCard takes a card out of every week — back to the strip.
func (s *Server) handleUntriageCard(w http.ResponseWriter, r *http.Request) {
	view, ok := s.viewOf(w, r)
	if !ok {
		return
	}
	if !s.gestureOn(w, r, view, boardservice.GestureUntriage) {
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if err := svc.Untriage(r.Context(), boardID, uid); err != nil {
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
}

// handleSetTeamCapacity records the points a week a team gets through — the
// number a week of its plan is weighed against. It is somebody's judgement,
// like a person's: the board works out nothing (board.PointsAWeekOf).
func (s *Server) handleSetTeamCapacity(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Team   string `json:"team"`
		Points *int   `json:"points"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Points == nil {
		writeProblem(w, problem(http.StatusBadRequest, "pointsRequired", "points is required"))
		return
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	r = r.WithContext(staleOK(r.Context()))
	if err := svc.SetTeamPoints(r.Context(), boardID, in.Team, *in.Points); err != nil {
		s.apiError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// patchColumn re-files a card under a column — the (project, epic) pair.
// Naming only the project keeps the column name the card is already under,
// which is what moving a card between projects means.
func patchColumn(ctx context.Context, svc *boardservice.Service, boardID, uid string, p *apiv1.CardPatch) error {
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusNoContent)
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
	boardID := s.boardRef()
	if _, err := s.newService(); err != nil {
		writeProblem(w, problem(http.StatusUnauthorized, "notAuthenticated", "not authenticated: "+err.Error()))
		return
	}
	// The broadcast login is the caller's authenticated identity (stamped by
	// actorMiddleware), not a client-supplied value — otherwise any signed-in
	// user could show a chosen card as selected by someone else.
	login := board.ActorFrom(r.Context())
	s.store.SetPresence(storeKey(boardID), clientIDFrom(r.Context()), login, in.Card)
	w.WriteHeader(http.StatusNoContent)
}

// cardOf loads the card as the resource — a mutation echoes it exactly as a
// fresh GET would answer.
func (s *Server) cardOf(ctx context.Context, svc *boardservice.Service, boardID, uid string) (apiserver.Card, error) {
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return apiserver.Card{}, err
	}
	for _, c := range b.Cards {
		if c.ItemID == uid {
			return apiserver.CardResource(b, c), nil
		}
	}
	return apiserver.Card{}, fmt.Errorf("%w: %s", boardservice.ErrCardNotFound, uid)
}

// cardResponse is cardOf for a handler that still writes its own response; it
// goes with the last of them.
func (s *Server) cardResponse(w http.ResponseWriter, r *http.Request, svc *boardservice.Service, boardID string, uid string) {
	card, err := s.cardOf(r.Context(), svc, boardID, uid)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

// cardAfter runs one card action and answers with the card as it stands after
// it — what a card action with something to show does.
func (a surface) cardAfter(ctx context.Context, uid string, act func(svc *boardservice.Service, boardID string) error) (apiserver.Card, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return apiserver.Card{}, err
	}
	if err := act(svc, boardID); err != nil {
		return apiserver.Card{}, err
	}
	return a.s.cardOf(ctx, svc, boardID, uid)
}

// strOf reads an optional body field: what the document leaves optional is a
// pointer, and absent means the zero value here, which is what a handler
// reading a plain struct got.
func strOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func intOf(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// parseZone validates a semantic zone name ("" clears).
func parseZone(name string) (board.ZoneKey, error) {
	if name == "" {
		return "", nil
	}
	zone := apiserver.DomainZone(name)
	if zone == "" {
		return "", problem(http.StatusBadRequest, "unknownZone",
			"unknown zone (urgent, unplanned, planned, niceToHave or empty)")
	}
	return zone, nil
}

// patchZoneAndSize applies the two "what kind of work is this" fields of a
// patch — the zone and the size.
func patchZoneAndSize(ctx context.Context, svc *boardservice.Service, boardID, uid string, p *apiv1.CardPatch) error {
	if p.Zone != nil {
		zone, err := parseZone(string(*p.Zone))
		if err != nil {
			return err
		}
		if err := svc.SetZone(ctx, boardID, uid, zone); err != nil {
			return err
		}
	}
	if p.Size != nil {
		size, err := parseSize(*p.Size)
		if err != nil {
			return err
		}
		if err := svc.SetSize(ctx, boardID, uid, size); err != nil {
			return err
		}
	}
	return nil
}

// handlePatchPerson sets what the roster says about a person — for now their
// capacity, the points a week the Triage board measures their load against
// (`{"capacity": 40}`; 0 takes a set number back so the board derives one).
// It answers with the whole Board resource, whose members carry load and
// capacity: that is what the client redraws.
func (s *Server) handlePatchPerson(w http.ResponseWriter, r *http.Request) {
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return
	}
	var in struct {
		Capacity *int `json:"capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeProblem(w, problem(http.StatusBadRequest, "invalidBody", "invalid JSON body"))
		return
	}
	if in.Capacity == nil {
		writeProblem(w, problem(http.StatusBadRequest, "capacityRequired",
			"capacity is required (0 takes a set number back)"))
		return
	}
	ctx := r.Context()
	if err := svc.SetPersonCapacity(ctx, boardID, r.PathValue("login"), *in.Capacity); err != nil {
		s.apiError(w, r, err)
		return
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiserver.BoardResourceWithPeople(b, s.store.member))
}

// parseSize validates a size ("" clears): S, M, L or XL in any case.
func parseSize(raw string) (board.SizeKey, error) {
	size, ok := board.ParseSize(raw)
	if !ok {
		return "", problem(http.StatusBadRequest, "unknownSize", "unknown size (S, M, L, XL or empty)")
	}
	return size, nil
}

// parseStage validates a stage name ("" clears).
func parseStage(name string) (board.StageKey, error) {
	switch board.StageKey(name) {
	case board.StageNone, board.StageLocked, board.StageReview, board.StageRecurrent,
		board.StageRefuse, board.StageDone:
		return board.StageKey(name), nil
	}
	return "", problem(http.StatusBadRequest, "unknownStage",
		"unknown stage (locked, review, recurrent, refuse, done or empty)")
}

// decodeJSON reads the request body into dst, answering 400 on malformed input.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if tooBig(w, err) {
			return false
		}
		writeProblem(w, problem(http.StatusBadRequest, "invalidBody", "invalid JSON body: "+err.Error()))
		return false
	}
	return true
}

// tooBig answers a body that ran past maxBodyBytes with 413 rather than a
// confusing "invalid JSON". The cap is limitBody's; a legitimate payload never
// reaches it.
func tooBig(w http.ResponseWriter, err error) bool {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeProblem(w, problem(http.StatusRequestEntityTooLarge, "bodyTooLarge", "request body too large"))
		return true
	}
	return false
}

// decodeJSONAllowingEmpty reads a body that the caller may legitimately have
// left empty — an action whose every field is optional. A body that is there
// is still parsed, and still refused when it is malformed; only "nothing at
// all" is let through, leaving dst as it was.
func decodeJSONAllowingEmpty(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		if tooBig(w, err) {
			return false
		}
		writeProblem(w, problem(http.StatusBadRequest, "invalidBody", "invalid JSON body: "+err.Error()))
		return false
	}
	return true
}

// applyPlacementPatch sets where the card WAITS: the week it is scheduled for
// and the backlog list it is parked on. The two are one subject and exclusive
// of each other — the service takes either off when the other is given — so
// they are applied together, in the order they were sent.
func applyPlacementPatch(ctx context.Context, svc *boardservice.Service, boardID, uid string, p *apiv1.CardPatch) error {
	if p.Week != nil {
		if err := svc.SetWeek(ctx, boardID, uid, *p.Week); err != nil {
			return err
		}
	}
	if p.Parked != nil {
		if err := svc.SetBacklog(ctx, boardID, uid, *p.Parked); err != nil {
			return err
		}
	}
	return nil
}

// apiError answers a service error: what the sentinels table says it is.
func (s *Server) apiError(w http.ResponseWriter, _ *http.Request, err error) {
	writeProblem(w, problemFor(err))
}
