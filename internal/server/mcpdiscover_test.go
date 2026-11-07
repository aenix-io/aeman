package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerTrace sends every request with the bearer token and keeps what went
// over the wire: the protocol version and the method of each POST, and the
// status that came back.
type bearerTrace struct {
	token string
	mu    sync.Mutex
	seen  []string
}

func (b *bearerTrace) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+b.token)
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err == nil {
		b.mu.Lock()
		b.seen = append(b.seen, r.Method+" "+r.Header.Get("MCP-Protocol-Version")+" "+r.Header.Get("Mcp-Method")+" -> "+resp.Status)
		b.mu.Unlock()
	}
	return resp, err
}

// mcpOverHTTP serves the OAuth-mode server on a real listener (the client
// transports need one) under the base URL's Host, and returns a bearer
// token minted the way a connector gets it.
func mcpOverHTTP(t *testing.T) (endpoint string, trace *bearerTrace) {
	t.Helper()
	srv, _ := newOAuthServer(t)
	code, clientID, redirectURI, verifier := mintCode(t, srv)
	tok := redeemToken(t, srv, code, verifier, redirectURI, clientID)
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tok.Body.Bytes(), &tr); err != nil || tr.AccessToken == "" {
		t.Fatalf("token: %v, body = %s", err, tok.Body.String())
	}
	host := strings.TrimPrefix(strings.TrimPrefix(testBaseURL, "https://"), "http://")
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = host
		srv.handler.ServeHTTP(w, r)
	}))
	t.Cleanup(hs.Close)
	return hs.URL + "/mcp", &bearerTrace{token: tr.AccessToken}
}

// A CLIENT OF THE STATELESS PROTOCOL DISCOVERS THE TOOLS (G86). ChatGPT's
// connector speaks MCP 2026-07-28: it asks server/discover first and then
// tools/list, with no session. A session-keeping transport does not offer
// that version at all, so the connector signed in and then reported "action
// discovery failed". The SDK's own client takes the same path.
func TestMCPDiscoveryOverTheStatelessProtocol(t *testing.T) {
	endpoint, trace := mcpOverHTTP(t)
	// A session-keeping server leaves this client waiting, not refused:
	// bound it, so the failure is a failure and not a hung run.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c := mcp.NewClient(&mcp.Implementation{Name: "openai-mcp", Version: "1.0.0"}, nil)
	cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: trace}}, nil)
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, strings.Join(trace.seen, "\n"))
	}
	t.Cleanup(func() { _ = cs.Close() })
	if v := cs.InitializeResult().ProtocolVersion; v != "2026-07-28" {
		t.Errorf("negotiated %q, want 2026-07-28\n%s", v, strings.Join(trace.seen, "\n"))
	}
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v\n%s", err, strings.Join(trace.seen, "\n"))
	}
	if len(res.Tools) == 0 {
		t.Fatal("tools/list answered no tools")
	}
	if len(trace.seen) == 0 || !strings.Contains(trace.seen[0], "server/discover -> 200") {
		t.Errorf("the first request is not a discovered server/discover:\n%s", strings.Join(trace.seen, "\n"))
	}
}

// …AND A CLIENT OF THE INITIALIZE HANDSHAKE STILL CONNECTS (G86). Claude
// and the other connectors of the 2025 protocol initialize, send
// notifications/initialized and list the tools; the stateless transport
// answers each as a request of its own, no session id needed.
func TestMCPDiscoveryOverTheInitializeHandshake(t *testing.T) {
	endpoint, trace := mcpOverHTTP(t)
	post := func(body, version string) (int, string) {
		r, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if version != "" {
			r.Header.Set("MCP-Protocol-Version", version)
		}
		resp, err := (&http.Client{Transport: trace}).Do(r)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude","version":"1"}}}`, ""); code != http.StatusOK || !strings.Contains(body, `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize: %d %s", code, body)
	}
	if code, _ := post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, "2025-06-18"); code != http.StatusAccepted {
		t.Fatalf("notifications/initialized: %d", code)
	}
	if code, body := post(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, "2025-06-18"); code != http.StatusOK || !strings.Contains(body, `"name":"get_board"`) {
		t.Fatalf("tools/list: %d %.300s", code, body)
	}
}
