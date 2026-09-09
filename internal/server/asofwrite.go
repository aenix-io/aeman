package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// asOfHeader is what a client says it is LOOKING AT: the board of a past day.
// A view of a day that has ended is a record, and a change made from it would
// land on today's card while the person is looking at a picture — it "saves",
// and the text is gone when they open the card again. The client hides every
// control on such a card; this is what makes a path that forgot fail loudly
// instead of quietly (G60).
const asOfHeader = "X-Aeman-As-Of"

// recordWriteGuard refuses a write made from a view of a day that is over for
// the card it addresses. Whether the day is over is the team's own answer, so
// a live card on the same mixed screen still writes: the guard asks the same
// question the listing answered.
//
// A write that claims no day is an ordinary write and is not touched — every
// other client makes those.
func (s *Server) recordWriteGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		day := r.Header.Get(asOfHeader)
		if day == "" || !writes(r.Method) || !strings.HasPrefix(r.URL.Path, "/api/v1") {
			next.ServeHTTP(w, r)
			return
		}
		if !board.IsDayIso(day) {
			writeJSONError(w, http.StatusBadRequest, asOfHeader+": not a board day")
			return
		}
		svc, err := s.newService(r)
		if err != nil {
			next.ServeHTTP(w, r) // the handler answers for the missing board
			return
		}
		bd, err := svc.Board(r.Context(), s.boardRef(r))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		past := board.TeamsPast(bd, day)
		if len(past) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		// A write that names a card is judged by THAT card — through the same
		// question the day's board is built with (board.IsRecord), or the two
		// answer differently: a personal card, which belongs to no team and
		// no sprint, would be live on screen and refused here the moment the
		// no-team GROUP carried over.
		if uid := cardOfPath(r.URL.Path); uid != "" {
			card, ok := findCardByID(bd, uid)
			if !ok || !board.IsRecord(card, past) {
				next.ServeHTTP(w, r)
				return
			}
		} else if isCreate(r) {
			// A CREATE names its team in the body, so it is judged by that
			// team where the body is already parsed (handleCreateCard). While
			// a sprint is OPEN its days are the team's to work — the lead
			// reading the day it began adds a card there, which is where the
			// standup is — and refusing every create because SOME team on the
			// screen had settled took the add boxes off the whole board.
			next.ServeHTTP(w, r.WithContext(withAsOf(r.Context(), day)))
			return
		}
		// A write that names no card and no team (a carry-over, a roster
		// change) cannot be judged that way, and a view holding records is no
		// place to make one from.
		writeJSONError(w, http.StatusConflict,
			"the board of "+day+" is a record: that day is over for this card's team, so it cannot be changed from there")
	})
}

// writes reports a method that changes something.
func writes(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// cardOfPath is the uid a /api/v1/cards/{uid}... route addresses, or "".
func cardOfPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/api/v1/cards/")
	if !ok {
		return ""
	}
	uid, _, _ := strings.Cut(rest, "/")
	return uid
}

// findCardByID looks a card up on the board, roster entries included.
func findCardByID(b board.Board, uid string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ItemID == uid {
			return c, true
		}
	}
	for _, c := range b.Tasks {
		if c.ItemID == uid {
			return c, true
		}
	}
	return board.Card{}, false
}

// isCreate reports the one card-less write that names a team: POST /cards.
func isCreate(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v1/cards"
}

// asOfCtxKey carries the day a create was made from to the handler that knows
// its team.
type asOfCtxKey struct{}

func withAsOf(ctx context.Context, day string) context.Context {
	return context.WithValue(ctx, asOfCtxKey{}, day)
}

// AsOfDay is the past day a request was made from, or "" — set only where the
// guard could not judge the write itself.
func asOfDay(ctx context.Context) string {
	day, _ := ctx.Value(asOfCtxKey{}).(string)
	return day
}
