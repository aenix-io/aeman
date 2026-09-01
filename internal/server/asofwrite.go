package server

import (
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
		// A write that names a card is judged by THAT card's team; one that
		// names none (a create, a carry-over) cannot be, and a view holding
		// records is no place to make it from.
		if uid := cardOfPath(r.URL.Path); uid != "" {
			card, ok := findCardByID(bd, uid)
			if !ok || !past[card.Team] {
				next.ServeHTTP(w, r)
				return
			}
		}
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
