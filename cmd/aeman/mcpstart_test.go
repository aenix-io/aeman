package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/server"
	"github.com/aenix-io/aeman/internal/tokenstore"
	"github.com/aenix-io/aeman/internal/tokenstore/tokenstoretest"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
	"github.com/aenix-io/aeman/pkg/mcpserver"
)

type testCredential struct{}

func (testCredential) Token(context.Context) (string, error) { return "test-token", nil }
func (testCredential) Login(context.Context) (string, error) { return "lexfrei", nil }

func TestMCPClientConnectsWhileBackendLoads(t *testing.T) {
	originalStore := openStore
	openStore = func(*slog.Logger) tokenstore.Store { return tokenstoretest.NewFake() }
	t.Cleanup(func() { openStore = originalStore })
	originalTool := forgeToolCLI
	forgeToolCLI = func(forge.Forge) forge.CLI { return testCredential{} }
	t.Cleanup(func() { forgeToolCLI = originalTool })
	original := openMCPBackend
	begin := make(chan struct{})
	release := make(chan struct{})
	openMCPBackend = func(_ *server.GitConfig, _ *slog.Logger) (*server.GitBackend, error) {
		close(begin)
		<-release
		return nil, errors.New("backend stopped for test")
	}
	t.Cleanup(func() { openMCPBackend = original })

	addr := freeAddr(t)
	done := make(chan error, 1)
	go func() {
		done <- runMCP([]string{"--repo", "board=https://example.invalid/board.git", "--data", t.TempDir(), "--listen", addr})
	}()
	select {
	case <-begin:
	case <-time.After(2 * time.Second):
		t.Fatal("backend initialization did not begin")
	}
	defer func() {
		close(release)
		if err := <-done; err == nil {
			t.Error("runMCP returned nil after the backend failed")
		}
	}()

	if !healthy(addr) {
		t.Fatal("listener is not bound while the backend is still loading")
	}
	_, status := healthzAnswers(t.Context(), addr)
	if status != "starting" {
		t.Fatalf("health status = %q, want starting", status)
	}
	cs, err := dial(t, "http://"+addr+"/mcp", nil)
	if err != nil {
		t.Fatalf("client could not initialize while backend loads: %v", err)
	}
	if _, err := cs.ListTools(t.Context(), nil); err != nil {
		t.Fatalf("client could not list tools while backend loads: %v", err)
	}
}

func TestAwaitHealthyWaitsForReady(t *testing.T) {
	var status atomic.Value
	status.Store("starting")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"` + status.Load().(string) + `"}`))
	}))
	t.Cleanup(ts.Close)
	m, _ := testMgr(t, "darwin")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if m.awaitHealthy(ctx, ts.Listener.Addr().String()) {
		t.Fatal("starting was accepted as ready")
	}
	readyCtx, readyCancel := context.WithTimeout(t.Context(), time.Second)
	defer readyCancel()
	status.Store("ok")
	if !m.awaitHealthy(readyCtx, ts.Listener.Addr().String()) {
		t.Fatal("ok was not accepted as ready")
	}
	status.Store("degraded")
	if !m.awaitHealthy(readyCtx, ts.Listener.Addr().String()) {
		t.Fatal("degraded was not accepted as answering")
	}
}

func TestMCPServerSwapKeepsSessionAndChangesHealth(t *testing.T) {
	placeholder := mcpserver.New(mcpserver.Config{Board: "board", Lock: true, Version: "test"})
	swap := newMCPServerSwap(placeholder, func() daemonHealth { return daemonHealth{Status: "starting"} })
	ts := httptest.NewServer(mcpHTTPHandler(swap.current(), swap.health()))
	t.Cleanup(ts.Close)

	cs, err := dial(t, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatalf("initialize before backend swap: %v", err)
	}
	backend := boardservicetest.New(nil, nil)
	swap.set(fakeMCPServer(backend), func() daemonHealth { return daemonHealth{Status: "ok"} })
	if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_board"}); err != nil {
		t.Fatalf("get_board after backend swap: %v", err)
	}
}
