package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// wsAcceptOptions is strict same-origin in OAuth mode and additionally allows
// the Vite dev proxy's localhost origins in local-proxy mode.
func TestWSAcceptOptionsPerMode(t *testing.T) {
	oauth := &Server{auth: &authManager{}}
	if got := oauth.wsAcceptOptions(); len(got.OriginPatterns) != 0 || got.InsecureSkipVerify {
		t.Fatalf("oauth mode must be strict same-origin, got %+v", got)
	}
	local := &Server{}
	got := local.wsAcceptOptions()
	if got.InsecureSkipVerify {
		t.Fatal("origin verification must never be skipped")
	}
	if len(got.OriginPatterns) == 0 {
		t.Fatal("local mode must allow the localhost dev origins")
	}
}

// The Origin check actually rejects a cross-site handshake while allowing the
// localhost dev origin — a remote page cannot open the watch and read the board.
func TestWatchOriginEnforced(t *testing.T) {
	local := &Server{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, local.wsAcceptOptions())
		if err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer ts.Close()

	dial := func(origin string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c, resp, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):], &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": {origin}},
		})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			_ = c.Close(websocket.StatusNormalClosure, "")
		}
		return err
	}

	if err := dial("http://evil.example"); err == nil {
		t.Fatal("cross-site origin was accepted (board is exfiltratable)")
	}
	if err := dial("http://localhost:5173"); err != nil {
		t.Fatalf("localhost dev origin was rejected: %v", err)
	}
}
