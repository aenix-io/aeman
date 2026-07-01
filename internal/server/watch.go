package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// handleSnapshot returns the full board (meta, fields, cards and sprint pointers)
// as the informer's initial LIST. It is served from the board store's cache when
// fresh, otherwise loaded once and cached.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	svc, owner, project, ok := s.service(w, r)
	if !ok {
		return
	}
	b, err := svc.Board(r.Context(), owner, project)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleWatch streams board change events over a WebSocket, Kubernetes-watch
// style: the client LISTs via /snapshot, then applies the ADDED / MODIFIED /
// DELETED / RELOAD events it receives here. The stream is read-only — every
// mutation still goes through the REST API — so any writer (UI, API, MCP) that
// changes the board this server serves is reflected to all open clients.
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	owner, project, err := s.boardRef(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, _, err := s.tokenForRequest(r); err != nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
		return
	}
	// The stream carries nothing a same-origin page can't already read via the
	// API and performs no mutations, so cross-origin (the Vite dev proxy) is
	// allowed rather than rejected on an Origin mismatch.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	// We never read application messages; CloseRead drains control frames and
	// cancels ctx when the client goes away.
	ctx := conn.CloseRead(r.Context())

	// The client's self-assigned id keys echo suppression: changes made by this
	// same client (sent as X-Aeman-Client on its REST calls) are not echoed back.
	events, cancel := s.store.subscribe(storeKey(owner, project), r.URL.Query().Get("client"))
	defer cancel()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if err := writeWSMessage(ctx, conn, data); err != nil {
				return
			}
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

// writeWSMessage sends one text frame under a write deadline.
func writeWSMessage(ctx context.Context, conn *websocket.Conn, data []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, data)
}
