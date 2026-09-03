package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The headless MCP daemon. One process holds the board's clone and the MCP
// clients on the machine talk to it over loopback HTTP, instead of each
// spawning its own `aeman mcp` child with a clone, a cache and a push of its
// own — which the data-directory lock now forbids outright.

// mcpDrainBudget and mcpShutdownBudget are how long a stop waits for the
// write queue and then for the connections still open. Package variables so
// the tests need not sit through the real ones.
var (
	mcpDrainBudget    = 20 * time.Second
	mcpShutdownBudget = 5 * time.Second
	// mcpLastDrainBudget covers only what a call answered during the first
	// drain can have left behind it — a mop-up, not the main flush, so it
	// is short. The whole stop has to fit inside the service manager's
	// patience, and a slow push is exactly when it would not.
	mcpLastDrainBudget = 5 * time.Second
	// mcpSessionTimeout is how long a session with no request on it is kept.
	// A client that dies without sending DELETE leaves its session behind,
	// and without this nothing ever reclaims one: the SDK drops a session on
	// that DELETE or on this timeout, and no keepalive pings the peer. It is
	// deliberately long, because only a POST pauses the timer — an open
	// event stream does not — so a short value would end the session of a
	// client that is merely idle. The Go SDK's client then fails the call;
	// another client may re-initialize, as the spec tells it to. Twelve
	// hours outlasts an idle editor either way.
	mcpSessionTimeout = 12 * time.Hour
)

// checkListenAddr refuses an address that puts the board on the network.
// Nothing authenticates the endpoint: whoever reaches the port holds the
// board with the daemon's own push credential, so binding it anywhere but
// loopback has to be typed out in full.
func checkListenAddr(addr string, insecure bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--listen %q: want host:port, e.g. 127.0.0.1:8766: %w", addr, err)
	}
	// SplitHostPort only splits, and net.LookupPort is not the check it
	// looks like: it resolves "" to 0 — Go pins that legacy deliberately —
	// and a service name to its number, so `127.0.0.1:` bound an ephemeral
	// port and `127.0.0.1:http` bound 80. A literal number is the only
	// thing meant here. Zero stays legal: a run somebody is watching may
	// let the kernel choose, and the ready line names what it picked.
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("--listen %q: %q is not a port number", addr, port)
	}
	if insecure || host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	// An empty host is the trap: ":8766" reads local and binds every
	// interface the machine has.
	bound := host
	if host == "" || (ip != nil && ip.IsUnspecified()) {
		bound = "every interface"
	}
	return fmt.Errorf("--listen %s binds %s and nothing authenticates the MCP endpoint: bind loopback instead "+
		"(127.0.0.1:8766, [::1]:8766, localhost:8766), or pass --listen-insecure to expose the board there anyway", addr, bound)
}

// daemonHealth is what GET /healthz answers. Whether the port is open is
// the less useful half: a push credential that expired leaves the daemon
// answering happily while the board quietly stops syncing, and nobody is
// watching this process. Same judgement the server's own /api/healthz makes
// (G26), over the same number.
type daemonHealth struct {
	Status             string `json:"status"`
	UnpushedAgeSeconds int    `json:"unpushedAgeSeconds"`
}

// daemonReport turns the store's unpushed age into that answer. The commits
// are safe on disk either way; past the threshold they are not reaching
// anyone, which is what degraded says.
func daemonReport(unpushed, warn time.Duration) daemonHealth {
	h := daemonHealth{Status: "ok", UnpushedAgeSeconds: int(unpushed.Seconds())}
	if warn > 0 && unpushed > warn {
		h.Status = "degraded"
	}
	return h
}

// mcpHTTPHandler mounts one MCP server for every HTTP session. The server is
// shared: it keeps no per-call state of its own — what a call needs rides
// its context — so a single one answers every client, over the single store
// this process holds.
func mcpHTTPHandler(srv *mcp.Server, health func() daemonHealth) http.Handler {
	mux := http.NewServeMux()
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{SessionTimeout: mcpSessionTimeout})
	// Clients disagree about the trailing slash, and the redirect a single
	// registration would serve loses the POST body.
	mux.Handle("/mcp", streamable)
	mux.Handle("/mcp/", streamable)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(health())
	})
	// Loopback is no boundary against a browser: any page the person opens
	// can post to 127.0.0.1. With nothing authenticating the endpoint, the
	// origin check is what stands between a web page and the board.
	return http.NewCrossOriginProtection().Handler(mux)
}

// serveMCPHTTP runs the daemon until ctx is cancelled, then flushes the write
// queue before it stops answering. The shape follows the HTTP server's own
// stop (internal/server: Server.Run) with one pass more: nothing else keeps
// answering tool calls while it drains. The three phases together are what
// a stop can cost, and whatever supervises the daemon has to allow for it.
func serveMCPHTTP(ctx context.Context, addr string, h http.Handler, drain func(context.Context) error, log *slog.Logger) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	// After the bind, not before: on a busy port under KeepAlive the log
	// would otherwise fill with "ready" lines each followed by the listen
	// error — on exactly the path the install message sends people to read.
	// The bound address, not the requested one: port 0 means the kernel
	// chose, and printing ":0" back tells nobody where to connect.
	log.Info("aeman MCP server ready", "url", "http://"+ln.Addr().String()+"/mcp")
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// The queue flushes on its own budget first, with the port still
		// open: a change a client saw applied a second ago must not be lost
		// to a restart.
		drainCtx, cancel := context.WithTimeout(context.Background(), mcpDrainBudget)
		if err := drain(drainCtx); err != nil {
			log.Warn("final push failed; unpushed commits stay in the clone", "err", err)
		}
		cancel()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), mcpShutdownBudget)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			// A client holding its event stream open keeps Shutdown waiting
			// out the whole budget, which is the ordinary case rather than a
			// fault. The commits are already pushed: drop the connections and
			// exit zero, so the stop is recorded as a stop. The exit code
			// does not decide whether the daemon comes back — `KeepAlive`
			// and `Restart=always` restart it on any exit the manager did
			// not ask for — it decides whether the manager's own state and
			// `aeman service status` show a clean stop or a failure for the
			// operator to chase.
			log.Info("stopped with a connection still open", "err", err)
			_ = srv.Close()
		}
		// The port stayed open through the first drain on purpose, so a
		// client's last write would be pushed — which means a call answered
		// during it enqueued behind a flush that had already returned. This
		// pass catches those. Neither pass is a guarantee: Close does not
		// wait for handlers, so a call still inside one can enqueue after
		// this returns, and a push already in flight when the stop begins is
		// awaited by neither pass, so the flush they report may not have
		// happened. Those commits wait in the clone for the next start.
		lastCtx, cancelLast := context.WithTimeout(context.Background(), mcpLastDrainBudget)
		defer cancelLast()
		if err := drain(lastCtx); err != nil {
			log.Warn("final push failed; unpushed commits stay in the clone", "err", err)
		}
		return nil
	}
}
