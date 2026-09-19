package server

import (
	"context"
	"net/http"

	"github.com/aenix-io/aeman/internal/server/apiv1"
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
func viewOf(name string) (board.View, error) {
	if !board.KnownView(name) {
		return "", problem(http.StatusNotFound, "noSuchView", "no such board: "+name)
	}
	return board.View(name), nil
}

// selectorOf is the view's scope: the segment plus the selectors that narrow
// it (team, day, user, the triage window). The me board is the caller's own
// unless they say whose — "who am I" is resolved here, server side, so a
// client never has to send it.
//
// The selectors are read from the RAW query (carryQuery) rather than from the
// bound parameters. apiserver.ParseViewSelector is the tree's only
// query-to-Selector parse, and the hand-registered watch goes through this
// same function, so reading the typed copy here would leave a second
// implementation of that parse beside the one the watch still needs. It also
// refuses an explicit `view=` query, which no binder can do: the generated
// parameters model no query `view` for these routes.
func selectorOf(ctx context.Context, view board.View) (apiserver.Selector, error) {
	sel, err := apiserver.ParseViewSelector(string(view), queryFrom(ctx))
	if err != nil {
		return apiserver.Selector{}, problem(http.StatusBadRequest, "invalidSelector", err.Error())
	}
	if sel.View == string(board.ViewMe) && sel.User == "" {
		// The actor the middleware resolved, which is the same login the
		// selector used to look up for itself.
		sel.User = board.ActorFrom(ctx)
	}
	return sel, nil
}

// gestureOn answers both halves of "may this press be made here", in the order
// they bite.
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
func (s *Server) gestureOn(ctx context.Context, view board.View, g boardservice.Gesture, uid string) error {
	if !boardservice.Offers(view, g) {
		return problem(http.StatusNotFound, "gestureNotOffered",
			"the "+string(view)+" board has no "+string(g)+" — it is not a gesture it draws")
	}
	if view == board.ViewAll {
		return nil
	}
	sel, err := selectorOf(ctx, view)
	if err != nil {
		return err
	}
	svc, boardID, err := s.serviceOf()
	if err != nil {
		return err
	}
	b, err := svc.Board(ctx, boardID)
	if err != nil {
		return err
	}
	if sel.Team == "" && board.ScopedByTeam(view) {
		// The card's own team, so a caller that named no team is judged on
		// the grid the card is actually drawn on rather than on the no-team
		// group's. Only where the LISTING names one (board.ScopedByTeam):
		// the Me and Project boards list every team, so filling it there
		// would ask a stricter question than the board answered — a subtask
		// whose team differs from its parent's drops the parent out of the
		// Me listing, and the child rides in on the parent.
		for _, c := range b.Cards {
			if c.ItemID == uid {
				sel.Team = c.Team
				break
			}
		}
	}
	// A board can be drawn from more than one listing — the Triage grid
	// beside its drawer — and the × in the drawer is the Triage board's ×
	// (board.Panes).
	for _, pane := range board.Panes(view) {
		paneSel := sel
		paneSel.View = string(pane)
		if apiserver.Drawn(b, paneSel, uid) {
			return nil
		}
	}
	return problem(http.StatusNotFound, "cardNotOnBoard",
		"that card is not on the "+string(view)+" board — act from the board that draws it, or say view=all")
}

// ListViews is the catalog: the boards a caller may open and the gestures each
// draws. It exists so a client — or an agent — can read the surface instead of
// being told it in a description, which is how the two drifted apart in the
// first place.
func (a surface) ListViews(context.Context, apiv1.ListViewsRequestObject) (apiv1.ListViewsResponseObject, error) {
	out := make([]apiv1.ViewResource, 0, len(board.Views()))
	for _, v := range board.Views() {
		gestures := make([]string, 0, 4)
		for _, g := range boardservice.Gestures(v) {
			gestures = append(gestures, string(g))
		}
		out = append(out, apiv1.ViewResource{Name: string(v), Gestures: gestures})
	}
	return apiv1.ListViews200JSONResponse{Kind: apiv1.ViewListKindViewList, Items: out}, nil
}
