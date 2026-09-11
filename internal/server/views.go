package server

import (
	"net/http"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// The BOARD a caller is standing on, as a path segment. It scopes a listing,
// says what a create means and says which gestures are on offer; the card
// itself stays addressed as itself. Why, and why this is not a namespace even
// though it is shaped like one: docs/design/view-scoped-api.md.

// viewOf reads the segment and refuses a board nobody has — 404, because a
// view that is not a board is a route that is not there.
func (s *Server) viewOf(w http.ResponseWriter, r *http.Request) (board.View, bool) {
	name := r.PathValue("view")
	if !board.KnownView(name) {
		writeJSONError(w, http.StatusNotFound, "no such board: "+name)
		return "", false
	}
	return board.View(name), true
}

// selectorOf is the view's scope: the segment plus the selectors that narrow
// it (team, day, user, the triage window). The me and personal boards are the
// caller's own unless they say whose — "who am I" is resolved here, server
// side, so a client never has to send it.
func (s *Server) selectorOf(w http.ResponseWriter, r *http.Request, view board.View) (apiserver.Selector, bool) {
	sel, err := apiserver.ParseViewSelector(string(view), r.URL.Query())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return apiserver.Selector{}, false
	}
	if (sel.View == string(board.ViewMe) || sel.View == string(board.ViewPersonal)) && sel.User == "" {
		if _, login, err := s.apiTokens(r); err == nil {
			sel.User = login
		}
	}
	return sel, true
}

// gestureOn answers both halves of "may this press be made here", in the order
// they bite, and writes the 404 itself when either says no.
//
// The board first: a gesture a board does not draw is a route that is not
// there (there is no week to drop into on the Me board). Then the CARD: a
// person cannot press × on a card they cannot see, and an agent standing on
// the Me board should not be able to either — it has to say `view=team&team=X`
// or `view=all`, and then it has said which board it is acting from.
//
// The scope the gate uses is the selector the LISTING would use, with the same
// defaults, so the two answer alike. What a client does not say is filled in
// generously: the day is today, the team is the card's own, the person is the
// caller. A gate stricter than that would refuse gestures the board plainly
// offers — the whole point is the board, not the parameters.
func (s *Server) gestureOn(w http.ResponseWriter, r *http.Request, view board.View, g boardservice.Gesture) bool {
	if !boardservice.Offers(view, g) {
		writeJSONError(w, http.StatusNotFound,
			"the "+string(view)+" board has no "+string(g)+" — it is not a gesture it draws")
		return false
	}
	if view == board.ViewAll {
		return true
	}
	sel, ok := s.selectorOf(w, r, view)
	if !ok {
		return false
	}
	svc, boardID, ok := s.service(w, r)
	if !ok {
		return false
	}
	b, err := svc.Board(r.Context(), boardID)
	if err != nil {
		s.apiError(w, r, err)
		return false
	}
	uid := r.PathValue("uid")
	if sel.Team == "" && namesATeam(view) {
		// The card's own team, so a caller that named no team is judged on
		// the grid the card is actually drawn on rather than on the no-team
		// group's. Only where the LISTING names one: the Me, personal and
		// Project boards list every team, so filling it there would ask a
		// stricter question than the board answered — a subtask whose team
		// differs from its parent's drops the parent out of the Me listing,
		// and the child rides in on the parent.
		for _, c := range b.Cards {
			if c.ItemID == uid {
				sel.Team = c.Team
				break
			}
		}
	}
	// A board can be drawn from more than one listing — the Triage grid
	// beside its drawer, the Me day beside the personal column — and the ×
	// in the drawer is the Triage board's × (board.Panes).
	drawn := false
	for _, pane := range board.Panes(view) {
		paneSel := sel
		paneSel.View = string(pane)
		if apiserver.Drawn(b, paneSel, uid) {
			drawn = true
			break
		}
	}
	if !drawn {
		writeJSONError(w, http.StatusNotFound,
			"that card is not on the "+string(view)+" board — act from the board that draws it, or say view=all")
		return false
	}
	return true
}

// namesATeam reports the boards whose LISTING is scoped by a team — the ones
// a gesture may be judged on the card's own team for. The others list every
// team, and "" is already the answer they gave.
func namesATeam(view board.View) bool {
	return view == board.ViewTeam || view == board.ViewTriage || view == board.ViewBacklog
}

// viewResource is one board in the catalog.
type viewResource struct {
	Name     string   `json:"name"`
	Gestures []string `json:"gestures"`
}

// handleListViews is the catalog: the boards a caller may open and the
// gestures each draws. It exists so a client — or an agent — can read the
// surface instead of being told it in a description, which is how the two
// drifted apart in the first place.
func (s *Server) handleListViews(w http.ResponseWriter, _ *http.Request) {
	out := make([]viewResource, 0, len(board.Views()))
	for _, v := range board.Views() {
		gestures := make([]string, 0, 4)
		for _, g := range boardservice.Gestures(v) {
			gestures = append(gestures, string(g))
		}
		out = append(out, viewResource{Name: string(v), Gestures: gestures})
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "ViewList", "items": out})
}
