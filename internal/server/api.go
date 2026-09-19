package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// GetCardLog serves a card's unified activity feed: its recorded events and
// work notes merged chronologically. The day delta — "what happened on this
// card since yesterday" — reads straight off this list.
func (a surface) GetCardLog(ctx context.Context, req apiv1.GetCardLogRequestObject) (apiv1.GetCardLogResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	card, events, truncated, err := svc.Log(ctx, boardID, req.UID)
	if err != nil {
		return nil, err
	}
	return apiv1.GetCardLog200JSONResponse(apiserver.CardLogFrom(card, events, truncated)), nil
}

// GetDayLogs answers the day feed: one day's notes and events for every card
// named in uids. The day board asks this once instead of a whole history per
// card — the read that made a page load fire dozens of second-long requests.
func (a surface) GetDayLogs(ctx context.Context, req apiv1.GetDayLogsRequestObject) (apiv1.GetDayLogsResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	uids := splitList(strOf(req.Params.Uids))
	if len(uids) > maxDayLogCards {
		return nil, problem(http.StatusBadRequest, "tooManyCards",
			fmt.Sprintf("uids: at most %d cards per request", maxDayLogCards))
	}
	day := strOf(req.Params.Day)
	if day != "" && !board.IsDayIso(day) {
		return nil, problem(http.StatusBadRequest, "invalidDay", "day: want yyyy-mm-dd")
	}
	per, err := svc.DayLogs(ctx, boardID, uids, day)
	if err != nil {
		return nil, err
	}
	if day == "" {
		day = board.TodayIso()
	}
	entries := make(map[string]apiserver.DayEntries, len(per))
	for uid, d := range per {
		entries[uid] = apiserver.DayEntries{Notes: d.Notes, Events: d.Events}
	}
	return apiv1.GetDayLogs200JSONResponse(apiserver.DayLogsFrom(day, entries)), nil
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

// GetBoard answers the board itself: its roster, the Project board's
// structure, the people and the visitor's domains.
func (a surface) GetBoard(ctx context.Context, _ apiv1.GetBoardRequestObject) (apiv1.GetBoardResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	// A record's board carries that day's SPRINT POINTERS with its cards —
	// the view rules place a card by its team's pointer, and today's would
	// drop nearly all of them. The roster itself (projects, columns,
	// processes, deadlines) stays TODAY's: a column added since shows on the
	// record of an older day, which is the price of not rebuilding the whole
	// structure per read, and is what docs/dates.md says.
	b, _, _, err := boardOfRequest(ctx, svc, boardID, "")
	if err != nil {
		return nil, err
	}
	info := apiserver.BoardResourceWithPeople(b, a.s.store.member)
	// Always, one repository or many: the payload's stamps are domain
	// NAMES — the store stamps the primary's entries too (G59) — so a
	// board that listed none left the client comparing "aeman" against "",
	// two names for one repository, and every rule that asks "the same
	// repository?" answered no. The UI keys its multi-repository parts off
	// the COUNT (isMultiDomain), not off the list's presence, so a single
	// entry shows no badges and no repository pickers.
	if a.s.gitCfg != nil {
		logins := make([]string, 0, len(info.Metadata.Members))
		for _, m := range info.Metadata.Members {
			logins = append(logins, m.Login)
		}
		info.Metadata.Domains = a.s.domainsFor(ctx, logins)
	}
	return apiv1.GetBoard200JSONResponse(info), nil
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

// ListCards lists what a board draws, in board order.
func (a surface) ListCards(ctx context.Context, req apiv1.ListCardsRequestObject) (apiv1.ListCardsResponseObject, error) {
	view, err := viewOf(req.View)
	if err != nil {
		return nil, err
	}
	// The board is the path segment, and "who am I" is resolved inside
	// selectorOf: a Me request needs no user (an explicit ?user= still wins,
	// for a lead looking at somebody else's day).
	sel, err := selectorOf(ctx, view)
	if err != nil {
		return nil, err
	}
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	// A PAST day can be asked for as it stood, rather than as today's board
	// filtered by that day's dates (see boardOfRequest).
	b, asOf, records, err := boardOfRequest(ctx, svc, boardID, req.View)
	if err != nil {
		return nil, err
	}
	if asOf != "" {
		// A record of the day gives back what the × took off it — of the
		// cards this listing IS a record of, and no others (G60).
		sel.LeftOn, sel.RecordCards = sel.Day, records
	}
	list := apiserver.ListCards(b, sel)
	apiserver.MarkRecords(&list, records, asOf)
	return apiv1.ListCards200JSONResponse(list), nil
}

// boardOfRequest is the board a read answers from: today's, or — when the
// request asks for a PAST day as it stood (`snapshot=1`) — the board of that
// day. The day itself is built by the board service (BoardOfDay), which is
// what every other door reads it through: an agent over MCP and this handler
// must not answer "what did that day look like" differently.
//
// view is the path segment where there is one; a read without one (/board,
// /sprints) is asked for a day by the board that is open, which says which
// view it is in the query.
//
// records names the cards the day's board took from that evening (empty on a
// live read), and asOf the moment the record reflects.
func boardOfRequest(ctx context.Context, svc *boardservice.Service, boardID, view string) (bd board.Board, asOf string, records map[string]bool, err error) {
	q := queryFrom(ctx)
	day := q.Get("day")
	asked := q.Get("snapshot") == "1" || q.Get("snapshot") == "true"
	if view == "" {
		view = q.Get("view")
	}
	// Only a DAY board has a day to be a record of (G60). The flag is
	// ignored elsewhere rather than refused: /board and /sprints carry no
	// board segment at all, and every reader of them is a day board asking
	// for its own moment.
	if !asked || (view != "" && !board.HasRecords(view)) {
		day = ""
	}
	bd, records, at, err := svc.BoardOfDay(ctx, boardID, day)
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

// ListSprints answers the per-team sprint pointers.
func (a surface) ListSprints(ctx context.Context, _ apiv1.ListSprintsRequestObject) (apiv1.ListSprintsResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	// A past day's pointers come with its cards: the client's own view rules
	// compare a card's sprint against them, so today's pointers over that
	// day's cards drop nearly all of them.
	b, _, _, err := boardOfRequest(ctx, svc, boardID, "")
	if err != nil {
		return nil, err
	}
	return apiv1.ListSprints200JSONResponse{
		Kind:  apiv1.SprintListKindSprintList,
		Items: apiserver.SprintResources(b),
	}, nil
}

// --- Create / patch ------------------------------------------------------------

// CreateCard makes the card the board it was typed into makes.
func (a surface) CreateCard(ctx context.Context, req apiv1.CreateCardRequestObject) (apiv1.CreateCardResponseObject, error) {
	view, err := viewOf(req.View)
	if err != nil {
		return nil, err
	}
	in := req.Body
	zone, err := parseZone(zoneOf(in.Zone))
	if err != nil {
		return nil, err
	}
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	// Made from a past day: judged by the team the card is being added to,
	// which the guard could not read (recordWriteGuard). A team still inside
	// that sprint is still working those days — the lead reading the day the
	// sprint began adds a card there — and only a team the day is OVER for is
	// refused.
	if day := asOfDay(ctx); day != "" {
		if bd, err := svc.Board(ctx, boardID); err == nil && board.TeamsPast(bd, day)[strOf(in.Team)] {
			team := strOf(in.Team)
			if team == "" {
				team = "no team"
			}
			return nil, problem(http.StatusConflict, "dayIsARecord",
				"the board of "+day+" is a record for «"+team+"»: that day is over for them, so nothing can be added to it from there")
		}
	}
	size, err := parseSize(strOf(in.Size))
	if err != nil {
		return nil, err
	}
	args := boardservice.CreateCardArgs{
		Team:           strOf(in.Team),
		Zone:           zone,
		Size:           size,
		Title:          strOf(in.Title),
		Epic:           strOf(in.Epic),
		Project:        strOf(in.Project),
		ReviewOf:       strOf(in.ReviewOf),
		Parent:         strOf(in.Parent),
		StartNewSprint: in.StartNewSprint,
		NoSprint:       boolOf(in.NoSprint),
		Parked:         boolOf(in.Parked),
		Week:           strOf(in.Week),
	}
	if in.Dates != nil {
		args.Day, args.Start, args.SprintStart = strOf(in.Dates.End), strOf(in.Dates.Start), strOf(in.Dates.Sprint)
	}
	if in.Assignees != nil && len(*in.Assignees) > 0 {
		args.Assignee = (*in.Assignees)[0]
	}
	card, err := svc.CreateInView(ctx, boardID, view, args)
	if err != nil {
		return nil, err
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return apiv1.CreateCard201JSONResponse(apiserver.CardResource(b, card)), nil
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

// RemoveCard is the board's ×: hand the card back to a home it still has, or
// delete it by that board's rules.
func (a surface) RemoveCard(ctx context.Context, req apiv1.RemoveCardRequestObject) (apiv1.RemoveCardResponseObject, error) {
	// An absent body is the intentless call, which is the gesture deciding
	// for itself.
	intent := boardservice.RemoveAuto
	if req.Body != nil {
		intent = boardservice.RemoveIntent(strOf(req.Body.Intent))
	}
	switch intent {
	case boardservice.RemoveAuto, boardservice.Unassign, boardservice.OffBoard:
	default:
		// Before the gate: an intent the × does not have is answered by
		// reading the body, not by building the board's listing first.
		return nil, problem(http.StatusBadRequest, "unknownIntent",
			"unknown intent (use unassign, off-board, or leave it out)")
	}
	view, err := viewOf(req.View)
	if err != nil {
		return nil, err
	}
	if err := a.s.gestureOn(ctx, view, boardservice.GestureRemove, req.UID); err != nil {
		return nil, err
	}
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	if err := svc.Remove(ctx, boardID, req.UID, view, intent); err != nil {
		return nil, err
	}
	return apiv1.RemoveCard204Response{}, nil
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

// PlaceCard puts a card in a week of the Triage board, which is what triaging
// it means (docs/design/triage.md).
func (a surface) PlaceCard(ctx context.Context, req apiv1.PlaceCardRequestObject) (apiv1.PlaceCardResponseObject, error) {
	card, err := a.gesture(ctx, req.View, req.UID, boardservice.GesturePlace,
		func(svc *boardservice.Service, boardID string) error {
			return svc.Place(ctx, boardID, req.UID, strOf(req.Body.Week))
		})
	if err != nil {
		return nil, err
	}
	return apiv1.PlaceCard200JSONResponse(card), nil
}

// FinishedEarlier sends finished work back to the sprint it was done in — its
// dates and the day it counts as done along with it. It is a MOVE and not a
// removal, so it has a door of its own: Remove's law is about emptying the
// working area, and this fills a different one.
func (a surface) FinishedEarlier(ctx context.Context, req apiv1.FinishedEarlierRequestObject) (apiv1.FinishedEarlierResponseObject, error) {
	card, err := a.gesture(ctx, req.View, req.UID, boardservice.GestureFinishedEarlier,
		func(svc *boardservice.Service, boardID string) error {
			return svc.FinishedEarlier(ctx, boardID, req.UID)
		})
	if err != nil {
		return nil, err
	}
	return apiv1.FinishedEarlier200JSONResponse(card), nil
}

// UntriageCard takes a card out of every week — back to the strip.
func (a surface) UntriageCard(ctx context.Context, req apiv1.UntriageCardRequestObject) (apiv1.UntriageCardResponseObject, error) {
	card, err := a.gesture(ctx, req.View, req.UID, boardservice.GestureUntriage,
		func(svc *boardservice.Service, boardID string) error {
			return svc.Untriage(ctx, boardID, req.UID)
		})
	if err != nil {
		return nil, err
	}
	return apiv1.UntriageCard200JSONResponse(card), nil
}

// gesture runs one press made ON a board — the board has to draw it and has to
// draw the card — and answers with the card as it stands after it.
func (a surface) gesture(ctx context.Context, name, uid string, g boardservice.Gesture,
	act func(svc *boardservice.Service, boardID string) error,
) (apiserver.Card, error) {
	view, err := viewOf(name)
	if err != nil {
		return apiserver.Card{}, err
	}
	if err := a.s.gestureOn(ctx, view, g, uid); err != nil {
		return apiserver.Card{}, err
	}
	return a.cardAfter(ctx, uid, act)
}

// --- Notes ----------------------------------------------------------------------

// ListLinks serves the URLs found in a card's description: GitHub issue/PR
// references first (resolved to their titles when possible), plain links after.
func (a surface) ListLinks(ctx context.Context, req apiv1.ListLinksRequestObject) (apiv1.ListLinksResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	links, err := svc.CardLinks(ctx, boardID, req.UID)
	if err != nil {
		return nil, err
	}
	if links == nil {
		links = []board.Link{}
	}
	return apiv1.ListLinks200JSONResponse{Kind: apiv1.LinkListKindLinkList, Items: links}, nil
}

// notesOf is a card's notes, which every note mutation answers with, so
// clients always converge on the server's view of the thread.
func notesOf(ctx context.Context, svc *boardservice.Service, boardID, uid string) (apiv1.NoteList, error) {
	card, err := svc.Card(ctx, boardID, uid)
	if err != nil {
		return apiv1.NoteList{}, err
	}
	return apiv1.NoteList{Kind: apiv1.NoteListKindNoteList, Items: apiserver.NoteResources(card)}, nil
}

// notesAfter runs one note mutation and answers with the thread as it stands
// after it.
func (a surface) notesAfter(ctx context.Context, uid string, act func(svc *boardservice.Service, boardID string) error) (apiv1.NoteList, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return apiv1.NoteList{}, err
	}
	if act != nil {
		if err := act(svc, boardID); err != nil {
			return apiv1.NoteList{}, err
		}
	}
	return notesOf(ctx, svc, boardID, uid)
}

// ListNotes is the card's work notes.
func (a surface) ListNotes(ctx context.Context, req apiv1.ListNotesRequestObject) (apiv1.ListNotesResponseObject, error) {
	notes, err := a.notesAfter(ctx, req.UID, nil)
	if err != nil {
		return nil, err
	}
	return apiv1.ListNotes200JSONResponse(notes), nil
}

// AddNote appends a work note.
func (a surface) AddNote(ctx context.Context, req apiv1.AddNoteRequestObject) (apiv1.AddNoteResponseObject, error) {
	notes, err := a.notesAfter(ctx, req.UID, func(svc *boardservice.Service, boardID string) error {
		return svc.AddNote(ctx, boardID, req.UID, strOf(req.Body.Text))
	})
	if err != nil {
		return nil, err
	}
	return apiv1.AddNote201JSONResponse(notes), nil
}

// EditNote edits a work note.
func (a surface) EditNote(ctx context.Context, req apiv1.EditNoteRequestObject) (apiv1.EditNoteResponseObject, error) {
	notes, err := a.notesAfter(ctx, req.UID, func(svc *boardservice.Service, boardID string) error {
		return svc.EditNote(ctx, boardID, req.UID, req.NoteID, strOf(req.Body.Text))
	})
	if err != nil {
		return nil, err
	}
	return apiv1.EditNote200JSONResponse(notes), nil
}

// DeleteNote deletes a work note.
func (a surface) DeleteNote(ctx context.Context, req apiv1.DeleteNoteRequestObject) (apiv1.DeleteNoteResponseObject, error) {
	notes, err := a.notesAfter(ctx, req.UID, func(svc *boardservice.Service, boardID string) error {
		return svc.DeleteNote(ctx, boardID, req.UID, req.NoteID)
	})
	if err != nil {
		return nil, err
	}
	return apiv1.DeleteNote200JSONResponse(notes), nil
}

// --- Sprints ----------------------------------------------------------------------

// PatchSprint sets a team's sprint pointers directly — the repair tool.
func (a surface) PatchSprint(ctx context.Context, req apiv1.PatchSprintRequestObject) (apiv1.PatchSprintResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	in := req.Body
	team, current, previous := strOf(in.Team), strOf(in.Current), strOf(in.Previous)
	if err := svc.SetSprintState(board.WithDomain(ctx, strOf(in.Domain)), boardID, team, current, previous); err != nil {
		return nil, err
	}
	return apiv1.PatchSprint200JSONResponse{
		Kind:     "Sprint",
		Metadata: apiserver.SprintMetadata{Team: team},
		Spec:     apiserver.SprintSpec{Current: current, Previous: previous},
	}, nil
}

// CarryOver advances a team's sprint to today and carries its unfinished
// cards.
func (a surface) CarryOver(ctx context.Context, req apiv1.CarryOverRequestObject) (apiv1.CarryOverResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	// The carry ops read the board through the SAME snapshot every other
	// mutation already trusts: with write-behind the cache (queue replayed on
	// top) IS the live truth, while a blocking GitHub reload here only adds
	// seconds of latency and a chance to read a lagging replica. Stale-window
	// semantics apply; a cold cache still loads.
	rep, err := svc.CarryOver(staleOK(ctx), boardID, strOf(req.Body.Team), boolOf(req.Body.DryRun))
	if err != nil {
		return nil, err
	}
	return apiv1.CarryOver200JSONResponse(rep), nil
}

// ReorderTeams applies a shared team order: the hidden sprint-state cards are
// moved into the given sequence, so every client reads the same order back
// from the board metadata.
func (a surface) ReorderTeams(ctx context.Context, req apiv1.ReorderTeamsRequestObject) (apiv1.ReorderTeamsResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	// Same cached-snapshot read as carry-over (see CarryOver).
	if err := svc.ReorderTeams(staleOK(ctx), boardID, listOf(req.Body.Teams)); err != nil {
		return nil, err
	}
	return apiv1.ReorderTeams204Response{}, nil
}

// DeleteTeam deletes a team's hidden sprint-state card. A team that still has
// cards is protected server-side (422).
func (a surface) DeleteTeam(ctx context.Context, req apiv1.DeleteTeamRequestObject) (apiv1.DeleteTeamResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	if err := svc.DeleteTeam(ctx, boardID, strOf(req.Body.Team)); err != nil {
		return nil, err
	}
	return apiv1.DeleteTeam204Response{}, nil
}

// rosterWrite runs one write to the board's own structure — a project, a
// column, a process, a deadline. The board is read through a snapshot that may
// be minutes old: these writes read it to check a name and to carry its ids
// into the write, both of which a stale answer settles, and blocking on a full
// reload made adding a column feel broken on a big board. The background
// revalidation catches the rest up.
func (a surface) rosterWrite(ctx context.Context, act func(ctx context.Context, svc *boardservice.Service, boardID string) error) error {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return err
	}
	return act(staleOK(ctx), svc, boardID)
}

// AddEpic declares a new Project-board column.
func (a surface) AddEpic(ctx context.Context, req apiv1.AddEpicRequestObject) (apiv1.AddEpicResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.AddEpic(ctx, boardID, strOf(req.Body.Name), strOf(req.Body.Project))
	}); err != nil {
		return nil, err
	}
	return apiv1.AddEpic204Response{}, nil
}

// SetEpicProject moves a column from one project to another; an empty target
// detaches it.
func (a surface) SetEpicProject(ctx context.Context, req apiv1.SetEpicProjectRequestObject) (apiv1.SetEpicProjectResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.SetEpicProject(ctx, boardID, strOf(req.Body.From), strOf(req.Body.Epic), strOf(req.Body.Project))
	}); err != nil {
		return nil, err
	}
	return apiv1.SetEpicProject204Response{}, nil
}

// RenameEpic renames a column in place, cards and all.
func (a surface) RenameEpic(ctx context.Context, req apiv1.RenameEpicRequestObject) (apiv1.RenameEpicResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.RenameEpic(ctx, boardID, strOf(req.Body.Project), strOf(req.Body.Epic), strOf(req.Body.To))
	}); err != nil {
		return nil, err
	}
	return apiv1.RenameEpic204Response{}, nil
}

// RenameProject renames a project in place, columns and cards along with it.
func (a surface) RenameProject(ctx context.Context, req apiv1.RenameProjectRequestObject) (apiv1.RenameProjectResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.RenameProject(ctx, boardID, strOf(req.Body.Project), strOf(req.Body.To))
	}); err != nil {
		return nil, err
	}
	return apiv1.RenameProject204Response{}, nil
}

// RenameTeam renames a team where it is declared, its cards and process tasks
// along with it.
func (a surface) RenameTeam(ctx context.Context, req apiv1.RenameTeamRequestObject) (apiv1.RenameTeamResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	if err := svc.RenameTeam(staleOK(ctx), boardID, strOf(req.Body.Team), strOf(req.Body.To)); err != nil {
		return nil, err
	}
	return apiv1.RenameTeam204Response{}, nil
}

// SetTeamCapacity records the points a week a team gets through — the number a
// week of its plan is weighed against. It is somebody's judgement, like a
// person's: the board works out nothing (board.PointsAWeekOf).
func (a surface) SetTeamCapacity(ctx context.Context, req apiv1.SetTeamCapacityRequestObject) (apiv1.SetTeamCapacityResponseObject, error) {
	if req.Body.Points == nil {
		return nil, problem(http.StatusBadRequest, "pointsRequired", "points is required")
	}
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	if err := svc.SetTeamPoints(staleOK(ctx), boardID, strOf(req.Body.Team), *req.Body.Points); err != nil {
		return nil, err
	}
	return apiv1.SetTeamCapacity204Response{}, nil
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

// ListProcesses is the Process tab: every process with its tasks and each
// task's history.
func (a surface) ListProcesses(ctx context.Context, req apiv1.ListProcessesRequestObject) (apiv1.ListProcessesResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	b, err := svc.Board(staleOK(ctx), boardID)
	if err != nil {
		return nil, err
	}
	return apiv1.ListProcesses200JSONResponse(apiserver.ProcessesResource(b, strOf(req.Params.Project))), nil
}

// AddProcess declares a process — recurring work inside a project.
func (a surface) AddProcess(ctx context.Context, req apiv1.AddProcessRequestObject) (apiv1.AddProcessResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.AddProcess(board.WithDomain(ctx, strOf(req.Body.Domain)), boardID, strOf(req.Body.Name), strOf(req.Body.Project))
	}); err != nil {
		return nil, err
	}
	return apiv1.AddProcess204Response{}, nil
}

// DeleteProcess deletes an EMPTY process; it is refused while it has tasks.
func (a surface) DeleteProcess(ctx context.Context, req apiv1.DeleteProcessRequestObject) (apiv1.DeleteProcessResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.DeleteProcess(ctx, boardID, strOf(req.Body.Process))
	}); err != nil {
		return nil, err
	}
	return apiv1.DeleteProcess204Response{}, nil
}

// RenameProcess renames a process; its tasks follow.
func (a surface) RenameProcess(ctx context.Context, req apiv1.RenameProcessRequestObject) (apiv1.RenameProcessResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.RenameProcess(ctx, boardID, strOf(req.Body.Process), strOf(req.Body.To))
	}); err != nil {
		return nil, err
	}
	return apiv1.RenameProcess204Response{}, nil
}

// SetProcessProject moves a process to another project; an empty project is
// the no-project bucket.
func (a surface) SetProcessProject(ctx context.Context, req apiv1.SetProcessProjectRequestObject) (apiv1.SetProcessProjectResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.SetProcessProject(ctx, boardID, strOf(req.Body.Process), strOf(req.Body.Project))
	}); err != nil {
		return nil, err
	}
	return apiv1.SetProcessProject204Response{}, nil
}

// SetProcessPaused pauses a process, or resumes it.
func (a surface) SetProcessPaused(ctx context.Context, req apiv1.SetProcessPausedRequestObject) (apiv1.SetProcessPausedResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.SetProcessPaused(ctx, boardID, strOf(req.Body.Process), boolOf(req.Body.Paused))
	}); err != nil {
		return nil, err
	}
	return apiv1.SetProcessPaused204Response{}, nil
}

// ReorderProcesses applies a shared process order.
func (a surface) ReorderProcesses(ctx context.Context, req apiv1.ReorderProcessesRequestObject) (apiv1.ReorderProcessesResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.ReorderProcesses(ctx, boardID, listOf(req.Body.Processes))
	}); err != nil {
		return nil, err
	}
	return apiv1.ReorderProcesses204Response{}, nil
}

// ReorderProcessTasks applies one process's task order.
func (a surface) ReorderProcessTasks(ctx context.Context, req apiv1.ReorderProcessTasksRequestObject) (apiv1.ReorderProcessTasksResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.ReorderProcessTasks(ctx, boardID, strOf(req.Body.Process), listOf(req.Body.Uids))
	}); err != nil {
		return nil, err
	}
	return apiv1.ReorderProcessTasks204Response{}, nil
}

// AddTask adds what a process iterates on.
func (a surface) AddTask(ctx context.Context, req apiv1.AddTaskRequestObject) (apiv1.AddTaskResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	in := req.Body
	tpl, err := svc.AddProcessTask(staleOK(ctx), boardID, strOf(in.Process), boardservice.TaskArgs{
		Title: strOf(in.Title), Description: strOf(in.Description), Recurrence: strOf(in.Recurrence),
		Start: strOf(in.Start), Team: strOf(in.Team), Assignee: strOf(in.Assignee),
		Accumulate: boolOf(in.Accumulate),
	})
	if err != nil {
		return nil, err
	}
	return apiv1.AddTask201JSONResponse{UID: tpl.ItemID}, nil
}

// PatchTask changes what the NEXT turns will be; the running one is untouched.
func (a surface) PatchTask(ctx context.Context, req apiv1.PatchTaskRequestObject) (apiv1.PatchTaskResponseObject, error) {
	in := req.Body
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.UpdateProcessTask(ctx, boardID, req.UID, boardservice.TaskPatch{
			Title: in.Title, Description: in.Description, Recurrence: in.Recurrence,
			Start: in.Start, Team: in.Team, Assignee: in.Assignee, Accumulate: in.Accumulate,
		})
	}); err != nil {
		return nil, err
	}
	return apiv1.PatchTask204Response{}, nil
}

// DeleteTask deletes a task; its past turns stay as the record.
func (a surface) DeleteTask(ctx context.Context, req apiv1.DeleteTaskRequestObject) (apiv1.DeleteTaskResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.DeleteProcessTask(ctx, boardID, req.UID)
	}); err != nil {
		return nil, err
	}
	return apiv1.DeleteTask204Response{}, nil
}

// AddDeadline marks a week with one project's deadline line.
func (a surface) AddDeadline(ctx context.Context, req apiv1.AddDeadlineRequestObject) (apiv1.AddDeadlineResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.AddDeadline(ctx, boardID, strOf(req.Body.Week), strOf(req.Body.Project))
	}); err != nil {
		return nil, err
	}
	return apiv1.AddDeadline204Response{}, nil
}

// DeleteDeadline clears one project's deadline on a week.
func (a surface) DeleteDeadline(ctx context.Context, req apiv1.DeleteDeadlineRequestObject) (apiv1.DeleteDeadlineResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.DeleteDeadline(ctx, boardID, strOf(req.Body.Week), strOf(req.Body.Project))
	}); err != nil {
		return nil, err
	}
	return apiv1.DeleteDeadline204Response{}, nil
}

// MoveDeadline drags a deadline to another week; landing on a week that
// already has one leaves a single line.
func (a surface) MoveDeadline(ctx context.Context, req apiv1.MoveDeadlineRequestObject) (apiv1.MoveDeadlineResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.MoveDeadline(ctx, boardID, strOf(req.Body.Project), strOf(req.Body.From), strOf(req.Body.To))
	}); err != nil {
		return nil, err
	}
	return apiv1.MoveDeadline204Response{}, nil
}

// AddProject declares a project — the Project board's top grouping, which owns
// epic columns.
func (a surface) AddProject(ctx context.Context, req apiv1.AddProjectRequestObject) (apiv1.AddProjectResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.AddProject(board.WithDomain(ctx, strOf(req.Body.Domain)), boardID, strOf(req.Body.Name))
	}); err != nil {
		return nil, err
	}
	return apiv1.AddProject204Response{}, nil
}

// DeleteProject removes an EMPTY project (422 while it still owns epic
// columns — detaching planned work silently is the anti-goal).
func (a surface) DeleteProject(ctx context.Context, req apiv1.DeleteProjectRequestObject) (apiv1.DeleteProjectResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.DeleteProject(ctx, boardID, strOf(req.Body.Project))
	}); err != nil {
		return nil, err
	}
	return apiv1.DeleteProject204Response{}, nil
}

// ReorderProjects applies the shared chip order.
func (a surface) ReorderProjects(ctx context.Context, req apiv1.ReorderProjectsRequestObject) (apiv1.ReorderProjectsResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.ReorderProjects(ctx, boardID, listOf(req.Body.Projects))
	}); err != nil {
		return nil, err
	}
	return apiv1.ReorderProjects204Response{}, nil
}

// DeleteEpic removes an EMPTY epic column (422 while cards still sit under
// it — the Project board's own anti-goal).
func (a surface) DeleteEpic(ctx context.Context, req apiv1.DeleteEpicRequestObject) (apiv1.DeleteEpicResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.DeleteEpic(ctx, boardID, strOf(req.Body.Epic), strOf(req.Body.Project))
	}); err != nil {
		return nil, err
	}
	return apiv1.DeleteEpic204Response{}, nil
}

// ReorderEpics applies a shared column order, moving the hidden epic-state
// cards the way reorder-teams moves sprint-state.
func (a surface) ReorderEpics(ctx context.Context, req apiv1.ReorderEpicsRequestObject) (apiv1.ReorderEpicsResponseObject, error) {
	if err := a.rosterWrite(ctx, func(ctx context.Context, svc *boardservice.Service, boardID string) error {
		return svc.ReorderEpics(ctx, boardID, strOf(req.Body.Project), listOf(req.Body.Epics))
	}); err != nil {
		return nil, err
	}
	return apiv1.ReorderEpics204Response{}, nil
}

// --- Shared helpers ----------------------------------------------------------------

// SetPresence records the caller's live Me-view selection — ephemeral shared
// state broadcast over the watch, never persisted. The client id
// (X-Aeman-Client) keys it, so a closed tab clears its own mark.
func (a surface) SetPresence(ctx context.Context, req apiv1.SetPresenceRequestObject) (apiv1.SetPresenceResponseObject, error) {
	if _, err := a.s.newService(); err != nil {
		return nil, notAuthenticated(err)
	}
	// The broadcast login is the caller's authenticated identity (stamped by
	// actorMiddleware), not a client-supplied value — otherwise any signed-in
	// user could show a chosen card as selected by someone else.
	a.s.store.SetPresence(storeKey(a.s.boardRef()), clientIDFrom(ctx), board.ActorFrom(ctx), strOf(req.Body.Card))
	return apiv1.SetPresence204Response{}, nil
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

func boolOf(p *bool) bool {
	return p != nil && *p
}

func listOf(p *[]string) []string {
	if p == nil {
		return nil
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

// PatchPerson sets what the roster says about a person — for now their
// capacity, the points a week the Triage board measures their load against
// (`{"capacity": 40}`; 0 takes a set number back). It answers with the whole
// Board resource, whose members carry load and capacity: that is what the
// client redraws.
func (a surface) PatchPerson(ctx context.Context, req apiv1.PatchPersonRequestObject) (apiv1.PatchPersonResponseObject, error) {
	svc, boardID, err := a.s.serviceOf()
	if err != nil {
		return nil, err
	}
	if req.Body.Capacity == nil {
		return nil, problem(http.StatusBadRequest, "capacityRequired",
			"capacity is required (0 takes a set number back)")
	}
	if err := svc.SetPersonCapacity(ctx, boardID, req.Login, *req.Body.Capacity); err != nil {
		return nil, err
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return apiv1.PatchPerson200JSONResponse(apiserver.BoardResourceWithPeople(b, a.s.store.member)), nil
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
