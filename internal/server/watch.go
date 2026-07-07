package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/aenix-org/aeman/pkg/apiserver"
	"github.com/aenix-org/aeman/pkg/board"
)

// scopedQueryKeys are the query parameters that turn a watch into a scoped
// subscription (membership deltas for one view) instead of a raw board stream.
var scopedQueryKeys = []string{"view", "team", "day", "user", "week", "stage", "zone", "assignee"}

// handleWatch streams board change events over a WebSocket, Kubernetes-watch
// style: the client LISTs (GET /cards), then applies the ADDED / MODIFIED /
// DELETED frames it receives here. Each frame carries a full resource:
//
//	{ "type": "MODIFIED", "kind": "Card" | "Sprint" | "Ordering", "object": {...} }
//
// With selector parameters (view=, team=, stage=, ...) the subscription is
// scoped: a card entering the selection arrives as ADDED and one leaving it as
// DELETED, so thin clients can mirror a single view without the board-wide
// rules. `resources` picks the kinds (default "cards,sprints,ordering") and
// `client` keys echo suppression (changes made with the same X-Aeman-Client
// header are not echoed back). The stream is read-only.
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	owner, project, err := s.boardRef(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	svc, _, _, ok := s.service(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var sel *apiserver.Selector
	for _, key := range scopedQueryKeys {
		if q.Has(key) {
			parsed, err := apiserver.ParseSelector(q)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			sel = &parsed
			break
		}
	}
	resources := map[string]bool{"cards": true, "sprints": true, "ordering": true, "presence": true}
	if raw := q.Get("resources"); raw != "" {
		resources = map[string]bool{}
		for _, kind := range strings.Split(raw, ",") {
			resources[strings.TrimSpace(kind)] = true
		}
	}
	// Warm the cache before subscribing: a scoped subscription seeds its
	// membership from the cached board, and the client LISTs right after. A
	// stale snapshot is fine here — the diff events that follow the background
	// revalidation reconcile the membership (the middleware skips this endpoint
	// because the connection is hijacked, so opt in explicitly).
	warmCtx, _ := withStaleAllowed(r.Context())
	if _, err := svc.Board(warmCtx, owner, project); err != nil {
		s.apiError(w, err)
		return
	}
	// The Origin is checked so a cross-site page cannot open the stream and
	// read the board — in local-proxy mode the request is authenticated purely
	// by reaching the server, so without this any site could exfiltrate it.
	conn, err := websocket.Accept(w, r, s.wsAcceptOptions())
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	// We never read application messages; CloseRead drains control frames and
	// cancels ctx when the client goes away.
	ctx := conn.CloseRead(r.Context())

	key := storeKey(owner, project)
	clientID := q.Get("client")
	sub, cancel := s.store.subscribe(key, clientID, sel, resources)
	defer cancel()
	// A closed tab takes its live selection with it.
	defer s.store.ClearPresence(key, clientID)

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	// Day-relative views (team/me default to "today") shift at local midnight
	// with no board change; re-diff scoped memberships when the day rolls over.
	midnight := time.NewTimer(untilNextLocalMidnight())
	defer midnight.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-sub.ch:
			if err := writeWSMessage(ctx, conn, data); err != nil {
				return
			}
		case <-midnight.C:
			s.store.reevaluateAll(key)
			midnight.Reset(untilNextLocalMidnight())
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

// wsAcceptOptions authorizes the watch handshake by Origin. Same-origin is
// always allowed. In local-proxy mode (no OAuth: the request is trusted purely
// for reaching the server) the Vite dev proxy's localhost origins are allowed
// too; a remote page is rejected. In OAuth mode only same-origin is allowed —
// tighter, and the SameSite session cookie already blocks cross-site anyway.
func (s *Server) wsAcceptOptions() *websocket.AcceptOptions {
	if s.auth != nil {
		return &websocket.AcceptOptions{}
	}
	return &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*", "[::1]:*"},
	}
}

// untilNextLocalMidnight returns the wait until just past the next local
// midnight (a small slack keeps the re-diff on the new day's side).
func untilNextLocalMidnight() time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(24 * time.Hour)
	return time.Until(next) + 2*time.Second
}

// writeWSMessage sends one text frame under a write deadline.
func writeWSMessage(ctx context.Context, conn *websocket.Conn, data []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, data)
}

// unusedBoardImport keeps the board import until list handlers land here.
var _ = board.TodayIso
