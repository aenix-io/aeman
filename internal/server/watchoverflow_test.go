package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// A WATCH THAT FALLS BEHIND IS TOLD SO, NEVER LEFT STALE (G85). A watcher's
// frames wait in a bounded buffer; one a slow socket cannot take used to be
// dropped without a word, and the client kept a board missing that change
// until a reload. Now the first frame that does not fit marks the
// subscription overflowed: nothing more is queued for it, and the stream
// ends with one ERROR frame, Kubernetes-style — a Status of code 410, reason
// Expired — which the writer sends even though the buffer is full. The
// client re-LISTs and opens a new watch. Correctness does not hang on the
// buffer's size.

// statusFrame is the ERROR frame's object.
type statusFrame struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Reason string `json:"reason"`
	Code   int    `json:"code"`
}

func TestAnOverflowedSubscriptionEndsWithA410(t *testing.T) {
	store := newBoardStore()
	seedEntry(store, "k", board.NewBoardIn("acme", nil))
	sub, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	for i := 0; i < cap(sub.ch)+10; i++ {
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Card", Object: map[string]int{"n": i}})
	}
	if !sub.overflowed.Load() {
		t.Fatal("a frame that did not fit left the subscription as it was")
	}
	// The writer is woken, and what it takes off the buffer is the 410, the
	// last frame.
	select {
	case <-sub.gone:
	default:
		t.Fatal("the writer was not woken")
	}
	data, last := sub.deliver(<-sub.ch)
	if !last {
		t.Fatalf("after an overflow the writer was handed %s, want the 410 and the end", data)
	}
	var f struct {
		Type   string      `json:"type"`
		Kind   string      `json:"kind"`
		Object statusFrame `json:"object"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Type != "ERROR" || f.Kind != "Status" || f.Object.Code != http.StatusGone || f.Object.Reason != "Expired" || f.Object.Status != "Failure" {
		t.Fatalf("the closing frame = %s", data)
	}
	// Nothing is queued for it any more.
	before := len(sub.ch)
	sub.send(watchFrame{Type: "MODIFIED", Kind: "Card"})
	if len(sub.ch) != before {
		t.Fatal("a frame was queued after the overflow")
	}
}

// A subscription that keeps up is handed its frames in order and is never
// ended.
func TestAWatchThatKeepsUpIsNotEnded(t *testing.T) {
	store := newBoardStore()
	seedEntry(store, "k", board.NewBoardIn("acme", nil))
	sub, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()
	for i := 0; i < cap(sub.ch); i++ {
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Card"})
	}
	if sub.overflowed.Load() {
		t.Fatal("a full buffer, not an overflowing one, ended the watch")
	}
	for i := 0; i < cap(sub.ch); i++ {
		if _, last := sub.deliver(<-sub.ch); last {
			t.Fatalf("frame %d ended the watch", i)
		}
	}
}

// And over the wire: a socket that does not read while a burst far larger
// than the buffer is sent ends with the 410 frame and nothing after it, the
// frames before it being the ones that fitted.
func TestAFloodedWatchEndsWithA410OverTheWire(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }
	ts := httptest.NewServer(srv.handler)
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/api/v1/views/all/watch", nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(-1)

	var sub *subscription
	for deadline := time.Now().Add(5 * time.Second); sub == nil && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		srv.store.mu.Lock()
		for _, e := range srv.store.entries {
			e.mu.Lock()
			for s := range e.watchers {
				sub = s
			}
			e.mu.Unlock()
		}
		srv.store.mu.Unlock()
	}
	if sub == nil {
		t.Fatal("the watch never subscribed")
	}
	// Frames big enough that the socket stops taking them long before the
	// burst is over: the buffer fills behind a blocked writer.
	big := strings.Repeat("x", 256<<10)
	for i := 0; i < 4*cap(sub.ch); i++ {
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Card", Object: map[string]string{"pad": big}})
	}
	var lastType string
	var lastData []byte
	frames := 0
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("the stream never ended")
			}
			break
		}
		if lastType == "ERROR" {
			t.Fatalf("a frame came after the 410: %.80s", data)
		}
		var f struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(data, &f)
		lastType, lastData = f.Type, data
		frames++
	}
	if lastType != "ERROR" || !strings.Contains(string(lastData), `"code":410`) {
		t.Fatalf("the stream ended on %.120s after %d frames, want the 410", lastData, frames)
	}
	if frames >= 4*cap(sub.ch) {
		t.Fatalf("%d frames reached the client: the burst was never cut", frames)
	}
}
