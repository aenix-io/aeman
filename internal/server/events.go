package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sseHub fans "board changed" messages out to every connected client. Each
// message is the SSE data payload — a JSON array of the project item ids that
// changed (so clients re-fetch just those cards), or "[]" to mean "reload the
// whole board" (create / draft edit, where no item id is known up front).
type sseHub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: map[chan string]struct{}{}}
}

func (h *sseHub) add() chan string {
	ch := make(chan string, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) remove(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

// broadcast delivers msg to every client. Sends are non-blocking; if a client's
// buffer is full it is dropped for this message (the client coalesces bursts and
// a later message reconciles).
func (h *sseHub) broadcast(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// handleEvents streams a text/event-stream that emits a "changed" event whenever
// the board is mutated through this server, carrying the changed item ids so open
// clients can re-fetch just those cards.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if _, _, err := s.tokenForRequest(r); err != nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx / cloudflared) so events arrive promptly.
	w.Header().Set("X-Accel-Buffering", "no")

	ch := s.events.add()
	defer s.events.remove(ch)

	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "event: changed\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// mutationSignal inspects a proxied /api/github request. When it carries a
// GraphQL mutation it returns true plus the SSE payload to broadcast: a JSON
// array of the project item ids (PVTI_…) found in the variables, so clients can
// re-fetch just those cards. An empty array means "reload the whole board" (a
// create or draft edit, where no item id is present). It reads and restores
// r.Body.
func mutationSignal(r *http.Request) (isMutation bool, payload string) {
	if r.Method != http.MethodPost || r.Body == nil {
		return false, ""
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false, ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false, ""
	}
	if !strings.HasPrefix(strings.TrimSpace(req.Query), "mutation") {
		return false, ""
	}
	items := []string{}
	for _, v := range req.Variables {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "PVTI_") {
			items = append(items, s)
		}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return true, "[]"
	}
	return true, string(b)
}

// statusWriter captures the response status so the proxy only broadcasts on a
// successful mutation, while still passing Flush through to the real writer.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
