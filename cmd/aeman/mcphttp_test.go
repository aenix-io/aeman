package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
	"github.com/aenix-io/aeman/pkg/mcpserver"
)

// The headless MCP daemon: one process holds the clone and every client on
// the machine connects to it over loopback HTTP, instead of each spawning
// its own stdio child with a clone of its own. Nothing authenticates the
// endpoint, so what binds it and who may post to it are the two things
// these tests pin.

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeMCPServer is the aeman tool set over an in-memory board.
func fakeMCPServer(backend *boardservicetest.Backend) *mcp.Server {
	return mcpserver.New(mcpserver.Config{Board: "board", Lock: true, Version: "test", Backend: backend})
}

// testMCPHandler is the daemon's handler over an in-memory board, reporting
// itself healthy; the tests that care about health say otherwise.
func testMCPHandler(backend *boardservicetest.Backend) http.Handler {
	return mcpHTTPHandler(fakeMCPServer(backend), func() daemonHealth { return daemonHealth{Status: "ok"} })
}

// dial connects an MCP client to endpoint over streamable HTTP.
func dial(t *testing.T, endpoint string, httpClient *http.Client) (*mcp.ClientSession, error) {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := c.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient}, nil)
	if cs != nil {
		t.Cleanup(func() { _ = cs.Close() })
	}
	return cs, err
}

func TestMCPListenRefusesNonLoopback(t *testing.T) {
	// The daemon has no authentication of its own: whatever can reach the
	// port owns the board. A bare ":8766" is the trap — it reads local and
	// binds every interface.
	// The quiet ones: SplitHostPort only splits, and net.LookupPort — which
	// this used before — resolves "" to 0 and a service name to its number,
	// so an empty port bound an ephemeral one and ":http" bound 80.
	refused := []string{":8766", "0.0.0.0:8766", "10.0.0.5:8766", "board.local:8766",
		"127.0.0.1:abc", "127.0.0.1:", "[::1]:", "127.0.0.1:http", "127.0.0.1:70000"}
	for _, addr := range refused {
		err := checkListenAddr(addr, false)
		if err == nil {
			t.Errorf("--listen %s was accepted", addr)
			continue
		}
		// A malformed port has no way through to name; the rest do.
		if !strings.Contains(err.Error(), "port number") && !strings.Contains(err.Error(), "--listen-insecure") {
			t.Errorf("--listen %s: the refusal does not name the way through: %v", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:8766", "[::1]:0", "localhost:8766"} {
		if err := checkListenAddr(addr, false); err != nil {
			t.Errorf("--listen %s: %v", addr, err)
		}
	}
	// Exposing the board is allowed, but it has to be typed.
	if err := checkListenAddr("0.0.0.0:8766", true); err != nil {
		t.Errorf("--listen-insecure did not let 0.0.0.0 through: %v", err)
	}
}

// --listen-insecure alone applies to nothing: without --listen the process
// speaks stdio and exposes no port, so accepting it would let the flag that
// exists to make exposure deliberate pass unnoticed on a run it cannot
// affect.
func TestMCPRefusesListenInsecureWithoutListen(t *testing.T) {
	t.Setenv("AEMAN_MCP_LISTEN", "")
	t.Setenv("AEMAN_GITHUB_APP_ID", "")
	err := runMCP([]string{"--repo", "board=https://example.invalid/board.git", "--listen-insecure"})
	if err == nil {
		t.Fatal("--listen-insecure alone was accepted")
	}
	if !strings.Contains(err.Error(), "--listen") {
		t.Fatalf("refusal = %v", err)
	}
}

func TestMCPListenRefusesAMalformedAddress(t *testing.T) {
	if err := checkListenAddr("8766", false); err == nil {
		t.Fatal("a bare port was accepted as a listen address")
	}
}

// One process, one store: two clients on the daemon see one board, which is
// the whole point of it — two stdio children would each hold a clone and
// each miss the other's writes until a push and a fetch went round.
func TestHeadlessServesTwoSessions(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	ts := httptest.NewServer(testMCPHandler(fake))
	t.Cleanup(ts.Close)

	a, err := dial(t, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("first client: %v", err)
	}
	b, err := dial(t, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	for i, cs := range []*mcp.ClientSession{a, b} {
		tools, err := cs.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatalf("client %d ListTools: %v", i, err)
		}
		if len(tools.Tools) == 0 {
			t.Fatalf("client %d got an empty tool set", i)
		}
	}

	if _, err := a.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "create_card",
		Arguments: map[string]any{"team": "alpha", "title": "shared", "zone": "planned"},
	}); err != nil {
		t.Fatalf("create through the first client: %v", err)
	}
	res, err := b.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_cards",
		Arguments: map[string]any{"view": "all"},
	})
	if err != nil {
		t.Fatalf("list through the second client: %v", err)
	}
	var body strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			body.WriteString(tc.Text)
		}
	}
	if !strings.Contains(body.String(), "shared") {
		t.Fatalf("the second session does not see the first session's card: %s", body.String())
	}
}

// Clients disagree about the trailing slash, and a redirect between them
// loses the POST body.
func TestHeadlessMountsMCPWithAndWithoutTrailingSlash(t *testing.T) {
	ts := httptest.NewServer(testMCPHandler(boardservicetest.New(nil, nil)))
	t.Cleanup(ts.Close)
	for _, path := range []string{"/mcp", "/mcp/"} {
		if _, err := dial(t, ts.URL+path, nil); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

// originTransport is a browser page: same machine, same port, another site.
type originTransport struct {
	origin string
	base   http.RoundTripper
}

func (t originTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Origin", t.origin)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// Loopback is not a boundary a browser respects: any page the person opens
// can post to 127.0.0.1. Nothing authenticates the endpoint, so the origin
// check is what keeps a web page off the board — alongside the SDK's own
// refusal of a non-loopback Host on a loopback listener, which is the
// rebinding half of the same problem.
func TestHeadlessRefusesCrossOriginPost(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	ts := httptest.NewServer(testMCPHandler(fake))
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_card","arguments":{"team":"alpha","title":"evil","zone":"urgent"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST = %d, want 403", resp.StatusCode)
	}

	// And it is refused at the door, not at the tool: a page cannot even
	// open a session to call one.
	if _, err := dial(t, ts.URL+"/mcp", &http.Client{Transport: originTransport{origin: "https://evil.example"}}); err == nil {
		t.Error("a cross-origin client initialized a session")
	}
	if n := len(fake.Creates()); n != 0 {
		t.Errorf("cross-origin requests created %d cards", n)
	}
}

// Whether the port is open is the less useful half of the question. Nobody
// watches this process, so a credential that expired a week ago must show
// up here rather than in a board that quietly stopped syncing.
// DNS rebinding is the other half of the browser problem, and the origin
// check does not close it: a rebound page sends Sec-Fetch-Site: same-origin,
// which CrossOriginProtection allows before it looks at Origin at all. What
// refuses it is the SDK rejecting a non-loopback Host on a loopback
// listener — a guarantee the SDK owns and aeman only depends on, behind an
// opt-out and a debug switch the SDK plans to remove. Pin it, so a
// dependency bump that drops it is a failing test rather than a silence.
func TestHeadlessRefusesAReboundHostHeader(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	ts := httptest.NewServer(testMCPHandler(fake))
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"rebound","version":"0"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// The connection still goes to loopback; only the name it was reached
	// by is not, which is what a rebound page looks like from here.
	req.Host = "board.evil.example"
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a non-loopback Host on the loopback listener = %d, want 403", resp.StatusCode)
	}
	if n := len(fake.Creates()); n != 0 {
		t.Fatalf("the rebound request reached %d writes", n)
	}
}

func TestHeadlessHealthzReportsWhetherCommitsAreLanding(t *testing.T) {
	var health daemonHealth
	ts := httptest.NewServer(mcpHTTPHandler(fakeMCPServer(boardservicetest.New(nil, nil)),
		func() daemonHealth { return health }))
	t.Cleanup(ts.Close)

	ask := func(t *testing.T) daemonHealth {
		t.Helper()
		resp, err := ts.Client().Get(ts.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /healthz = %d", resp.StatusCode)
		}
		var got daemonHealth
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}

	health = daemonReport(0, 5*time.Minute)
	if got := ask(t); got.Status != "ok" || got.UnpushedAgeSeconds != 0 {
		t.Fatalf("a daemon with nothing waiting = %+v, want ok and 0", got)
	}

	health = daemonReport(10*time.Minute, 5*time.Minute)
	got := ask(t)
	if got.Status != "degraded" {
		t.Fatalf("status = %q with commits older than --unpushed-warn, want degraded", got.Status)
	}
	if got.UnpushedAgeSeconds != 600 {
		t.Fatalf("unpushedAgeSeconds = %d, want 600", got.UnpushedAgeSeconds)
	}
}

// The threshold is the boundary, not an approximation of one, and a warning
// age of zero is the server's own way of switching the judgement off.
func TestDaemonReportAtAndAroundTheThreshold(t *testing.T) {
	warn := 5 * time.Minute
	if got := daemonReport(warn, warn); got.Status != "ok" {
		t.Errorf("exactly at the threshold = %q, want ok", got.Status)
	}
	if got := daemonReport(warn+time.Second, warn); got.Status != "degraded" {
		t.Errorf("one second past = %q, want degraded", got.Status)
	}
	if got := daemonReport(24*time.Hour, 0); got.Status != "ok" {
		t.Errorf("with no threshold configured = %q, want ok", got.Status)
	}
}

// freeAddr reserves and releases a loopback port, so a test can name the
// address serveMCPHTTP will bind before it binds it.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func healthy(addr string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func waitHealthy(t *testing.T, addr string) {
	t.Helper()
	for range 100 {
		if healthy(addr) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the daemon never answered on %s", addr)
}

// A change made a second before the stop must survive it: the queue flushes
// and pushes while the port is still open, and only then does the daemon
// stop answering.
func TestHeadlessDrainsBeforeItStopsListening(t *testing.T) {
	addr := freeAddr(t)
	var mu sync.Mutex
	var order []string
	drain := func(context.Context) error {
		mu.Lock()
		order = append(order, "drain")
		mu.Unlock()
		if healthy(addr) {
			mu.Lock()
			order = append(order, "still-listening")
			mu.Unlock()
		}
		return nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- serveMCPHTTP(ctx, addr, testMCPHandler(boardservicetest.New(nil, nil)), drain, quietLogger())
	}()
	waitHealthy(t, addr)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveMCPHTTP: %v", err)
	}
	if healthy(addr) {
		t.Fatal("the daemon still answers after it returned")
	}

	mu.Lock()
	defer mu.Unlock()
	// First with the port open, so a write a client just made is pushed;
	// then once more after it closes, because a call answered during that
	// first pass enqueued behind a flush that had already returned.
	want := []string{"drain", "still-listening", "drain"}
	if !slices.Equal(order, want) {
		t.Fatalf("drain ran %v, want %v", order, want)
	}
}

// The port already taken is the case an operator actually hits, and the
// refusal has to name the address rather than the syscall alone — it is
// what `aeman service status` and the log point them at.
func TestHeadlessSaysWhichAddressItCouldNotBind(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	addr := held.Addr().String()

	drained := false
	err = serveMCPHTTP(t.Context(), addr, testMCPHandler(boardservicetest.New(nil, nil)),
		func(context.Context) error { drained = true; return nil }, quietLogger())
	if err == nil {
		t.Fatal("binding a port already in use was reported as success")
	}
	if !strings.Contains(err.Error(), addr) {
		t.Fatalf("the refusal does not name the address: %v", err)
	}
	// Nothing was served, so there was nothing to flush.
	if drained {
		t.Error("the drain ran for a listener that never opened")
	}
}

// A clean stop has to be recorded as a stop rather than as a failure; the
// reason for the exit code sits with the code that chooses it. A client
// holding an SSE stream open at the stop is the ordinary case, and this
// pins that it still exits zero.
func TestHeadlessCleanStopIsNotAnError(t *testing.T) {
	restore := mcpShutdownBudget
	mcpShutdownBudget = 100 * time.Millisecond
	t.Cleanup(func() { mcpShutdownBudget = restore })

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- serveMCPHTTP(ctx, addr, testMCPHandler(boardservicetest.New(nil, nil)),
			func(context.Context) error { return nil }, quietLogger())
	}()
	waitHealthy(t, addr)
	if _, err := dial(t, "http://"+addr+"/mcp", nil); err != nil {
		t.Fatalf("client: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a stop with a stream still open reported %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("serveMCPHTTP never returned")
	}
}

// The address is judged before anything opens the board: a clone takes up
// to two minutes, and spending it to then refuse a typo is a refusal that
// arrives long after the person has looked away. The data directory the run
// would have made is the evidence — it is not there afterwards.
func TestMCPRefusesTheListenAddressBeforeItTouchesTheDataDir(t *testing.T) {
	t.Setenv("AEMAN_GITHUB_APP_ID", "")
	t.Setenv("AEMAN_MCP_LISTEN", "")
	data := filepath.Join(t.TempDir(), "data")

	err := runMCP([]string{
		"--repo", "board=https://example.invalid/board.git",
		"--data", data,
		"--listen", "0.0.0.0:8766",
	})
	if err == nil {
		t.Fatal("0.0.0.0 was accepted")
	}
	if !strings.Contains(err.Error(), "--listen-insecure") {
		t.Fatalf("refusal = %v", err)
	}
	if _, err := os.Stat(data); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused run created the data directory: %v", err)
	}
}

// rawInitialize opens an MCP session over plain HTTP and returns its id, so
// a test can ask about that session after its client is gone.
func rawInitialize(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Header.Get("Mcp-Session-Id")
}

// pokeSession sends one request on an existing session and reports the
// status; 404 is the server saying it no longer knows that session.
func pokeSession(t *testing.T, ts *httptest.Server, sid string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sid)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// A client that exits cleanly sends DELETE and its session goes with it. One
// that is killed sends nothing, and the daemon is meant to run for weeks: an
// idle timeout is the only thing that ever reclaims those, since nothing
// pings the peer. Without one, each dead client left goroutines and its
// session behind for the life of the process.
func TestHeadlessReclaimsASessionItsClientNeverClosed(t *testing.T) {
	restore := mcpSessionTimeout
	mcpSessionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { mcpSessionTimeout = restore })

	ts := httptest.NewServer(testMCPHandler(boardservicetest.New(nil, nil)))
	t.Cleanup(ts.Close)

	sid := rawInitialize(t, ts)
	if sid == "" {
		t.Fatal("no session id came back from initialize")
	}
	if code := pokeSession(t, ts, sid); code == http.StatusNotFound {
		t.Fatal("the session was gone before it went idle")
	}

	time.Sleep(700 * time.Millisecond)
	if code := pokeSession(t, ts, sid); code != http.StatusNotFound {
		t.Fatalf("the abandoned session is still held: status %d", code)
	}
}

// The timeout has to outlast a client that is simply between prompts: only
// a POST pauses the SDK's timer, an open event stream does not, so a short
// one would end a live session. The Go SDK's client then fails the call;
// another client may re-initialize, as the spec tells it to. A client idle
// longer than the timeout does lose its session — this pins the floor, not
// a guarantee.
func TestHeadlessKeepsAnIdleClientForHours(t *testing.T) {
	if mcpSessionTimeout < 4*time.Hour {
		t.Fatalf("mcpSessionTimeout = %v, too short to outlast an idle editor", mcpSessionTimeout)
	}
}
