package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
			writeProblem(w, problem(http.StatusBadRequest, "invalidAsOf", asOfHeader+": not a board day"))
			return
		}
		svc, err := s.newService()
		if err != nil {
			next.ServeHTTP(w, r) // the handler answers for the missing board
			return
		}
		bd, err := svc.Board(r.Context(), s.boardRef())
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
		// question the day's board is built with (board.IsRecord), so a card
		// that is live on screen is never refused here.
		if isBlockMove(r) {
			// A BLOCK names its cards in the body (S21): judged card by card
			// like any write that names one, the whole block refused when one
			// of them is a record. The body is read here and handed on intact.
			uids, raw, err := blockUIDs(r)
			if err != nil {
				writeProblem(w, problem(http.StatusBadRequest, "invalidBody", err.Error()))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			for _, uid := range uids {
				if card, ok := findCardByID(bd, uid); ok && board.IsRecord(bd, card, past) {
					writeProblem(w, problem(http.StatusConflict, "dayIsARecord",
						"the board of "+day+" is a record: that day is over for a card of this block's team, so it cannot be changed from there"))
					return
				}
			}
			next.ServeHTTP(w, r)
			return
		}
		if uid := cardOfPath(r.URL.Path); uid != "" {
			card, ok := findCardByID(bd, uid)
			if !ok || !board.IsRecord(bd, card, past) {
				next.ServeHTTP(w, r)
				return
			}
		} else if isCreate(r) {
			// A CREATE names its team in the body, so it is judged by that
			// team where the body is already parsed (surface.CreateCard). While
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
		writeProblem(w, problem(http.StatusConflict, "dayIsARecord",
			"the board of "+day+" is a record: that day is over for this card's team, so it cannot be changed from there"))
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

// cardOfPath is the uid a route addresses, or "": a card by its canonical
// address (/api/v1/cards/{uid}…) or through the board a gesture was made from
// (/api/v1/views/{view}/cards/{uid}/actions/…), which names the same card.
func cardOfPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/api/v1/cards/")
	if !ok {
		if rest, ok = strings.CutPrefix(path, "/api/v1/views/"); !ok {
			return ""
		}
		if _, rest, ok = strings.Cut(rest, "/cards/"); !ok {
			return ""
		}
	}
	uid, _, _ := strings.Cut(rest, "/")
	return uid
}

// findCardByID looks a card up on the board.
func findCardByID(b board.Board, uid string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ItemID == uid {
			return c, true
		}
	}
	return board.Card{}, false
}

// isCreate reports the one card-less write that names a team: a create, made
// from whichever board it was typed into.
func isCreate(r *http.Request) bool {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/v1/views/") {
		return false
	}
	return strings.HasSuffix(r.URL.Path, "/cards")
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

// isBlockMove reports the write that names its cards in the body: a block
// moved together (POST /cards/actions/move-together).
func isBlockMove(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v1/cards/actions/move-together"
}

// blockUIDs reads a block move's cards from its body, and the body itself so
// the handler can read it again.
func blockUIDs(r *http.Request) ([]string, []byte, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, err
	}
	var body struct {
		UIDs []string `json:"uids"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, nil, err
	}
	return body.UIDs, raw, nil
}
