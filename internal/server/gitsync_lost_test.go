package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// lockedBuffer is a log sink the sync goroutine and the test can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func openLostTestBackend(t *testing.T, sync time.Duration, logs io.Writer) (*GitBackend, string) {
	t.Helper()
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir, SyncInterval: sync}
	gb, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gb.Close() })
	return gb, dir
}

// G90 — a clone removed under the running process cannot be repaired by it,
// only by a restart. The operator is told that once, at ERROR, naming the
// repository; the sync stops instead of failing the same way every tick.
func TestALostStoreIsLoggedOnceAndStopsTheSync(t *testing.T) {
	logs := &lockedBuffer{}
	gb, dir := openLostTestBackend(t, 10*time.Millisecond, logs)
	if err := os.Remove(filepath.Join(dir, "repos", "board", "HEAD")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "level=ERROR") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // twenty more ticks to repeat it in

	out := logs.String()
	if n := strings.Count(out, "level=ERROR"); n != 1 {
		t.Fatalf("ERROR lines = %d, want exactly one:\n%s", n, out)
	}
	if !strings.Contains(out, "restart required") || !strings.Contains(out, "domain=board") || !strings.Contains(out, dir) {
		t.Fatalf("the ERROR does not name the repository, its path and the remedy:\n%s", out)
	}
	if strings.Contains(out, "level=WARN msg=sync") {
		t.Fatalf("the lost store is still warned about every tick:\n%s", out)
	}
	if err := gb.be.syncNow(context.Background(), storeKey(gb.board)); !errors.Is(err, gitstore.ErrStoreLost) {
		t.Fatalf("syncNow on a lost store = %v, want ErrStoreLost", err)
	}
	if err := gb.StoreLost(); err == nil || strings.Contains(err.Error(), dir) {
		t.Fatalf("StoreLost() on a lost store = %v, want a reason without the data path", err)
	}
}

// G91 — the lock file lives in the data directory, so removing the directory
// lets a second process start, clone into the same path and take a fresh
// lock. The first one's clone then has a HEAD again, so the HEAD check alone
// would let it write into the newcomer's clone. Its lock file being replaced
// is the same lost directory: the write is refused.
func TestAWriteIsRefusedOnceTheDataDirLockIsReplaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the lock is opened without sharing: the file cannot be removed while held")
	}
	logs := &lockedBuffer{}
	gb, dir := openLostTestBackend(t, 0, logs)
	be := gb.be
	ctx := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6L1", "progress")
	bd, err := be.LoadBoard(ctx, gb.board)
	if err != nil {
		t.Fatal(err)
	}
	head := be.git.primary().Head()

	if err := os.Remove(filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	newcomer, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("the second process could not take the data dir: %v", err)
	}
	t.Cleanup(func() { _ = newcomer.Close() })

	_ = be.SetProgress(ctx, bd, cardByTitle(bd, "one"), 55)
	waitQueue(t, be)
	if got := be.git.primary().Head(); got != head {
		t.Fatalf("a write landed in a data directory another process now owns: head %s → %s", head, got)
	}
	if err := gb.StoreLost(); err == nil || strings.Contains(err.Error(), dir) {
		t.Fatalf("StoreLost() after the lock file was replaced = %v, want a reason without the data path", err)
	}
	if err := be.syncNow(context.Background(), storeKey(gb.board)); !errors.Is(err, errDataDirLost) {
		t.Fatalf("syncNow after the lock file was replaced = %v, want errDataDirLost", err)
	}
	// Writes that do not ride the queue reach the repository directly.
	if _, err := be.CreateCard(ctx, bd, board.CreateInput{Title: board.SprintStateTitle, Team: "newteam"}); !errors.Is(err, errDataDirLost) {
		t.Fatalf("a direct create after the lock file was replaced = %v, want errDataDirLost", err)
	}
	if _, err := be.maintainNow(context.Background(), storeKey(gb.board)); !errors.Is(err, errDataDirLost) {
		t.Fatalf("maintenance after the lock file was replaced = %v, want errDataDirLost", err)
	}
	if got := be.git.primary().Head(); got != head {
		t.Fatalf("a direct write landed in a data directory another process now owns: head %s → %s", head, got)
	}
	if n := strings.Count(logs.String(), "level=ERROR"); n != 1 {
		t.Fatalf("ERROR lines = %d, want exactly one:\n%s", n, logs.String())
	}
}

// G90 — /api/healthz says degraded, with the reason, for as long as the
// store is lost: a restart is the only way back, so the answer is 503 for a
// liveness probe to act on. The endpoint needs no login, so the reason names
// no path; the ERROR log line has it.
func TestHealthzDegradesWhileTheStoreIsLost(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git: &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir,
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if rec := do(t, srv, "GET", "/api/healthz", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health before the removal: %d %s", rec.Code, rec.Body.String())
	}
	if err := os.Remove(filepath.Join(dir, "repos", "board", "HEAD")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		rec := do(t, srv, "GET", "/api/healthz", "")
		body := rec.Body.String()
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("health status code with the store removed = %d, want 503", rec.Code)
		}
		if !strings.Contains(body, `"status":"degraded"`) || !strings.Contains(body, `"storeLost":"`) || !strings.Contains(body, "restart required") {
			t.Fatalf("health with the store removed: %s", body)
		}
		if strings.Contains(body, dir) {
			t.Fatalf("health discloses the data path %s: %s", dir, body)
		}
	}
}

// G91 — a push never goes through the store check, and on a lost data
// directory the shutdown's drain would push from a clone that may now be
// another process's. The sync refuses before it pushes anything.
func TestADrainDoesNotPushFromALostDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the lock is opened without sharing: the file cannot be removed while held")
	}
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	lock, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	repo, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	be := newGitBackend(newBoardStore(), []gitDomain{{Domain: gitstore.Domain{Name: "board", Repo: repo}, remote: remote}},
		gitOptions{Lock: lock, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ctx := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6L1", "progress")
	bd, err := be.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := be.SetProgress(ctx, bd, cardByTitle(bd, "one"), 55); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, be)
	if n, _ := repo.Unpushed(); n != 1 {
		t.Fatalf("unpushed before the removal = %d, want 1", n)
	}

	if err := os.Remove(filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	if err := be.syncNowWaiting(context.Background(), "acme"); !errors.Is(err, errDataDirLost) {
		t.Fatalf("drain from a lost data dir = %v, want errDataDirLost", err)
	}
	if n, _ := repo.Unpushed(); n != 1 {
		t.Fatalf("unpushed after the refused drain = %d, want the commit still unpushed", n)
	}
}

// G90 — the board is one store over every domain: once one domain's clone
// is lost the sync stops for all of them, so a write into a surviving domain
// would be accepted and then sit unpushed, and unseen by the fetch, until a
// restart. It is refused like the rest.
func TestALostDomainStopsWritesToTheOthers(t *testing.T) {
	primary, closed := gitRemoteN(t, "board"), gitRemoteN(t, "closed")
	seedGitRemote(t, primary)
	seedClosedRemote(t, closed)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: primary.URL}, {Name: "closed", URL: closed.URL}}, DataDir: dir}
	gb, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gb.Close() })
	be := gb.be
	ctx := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6L1", "progress")
	bd, err := be.LoadBoard(ctx, gb.board)
	if err != nil {
		t.Fatal(err)
	}
	head := be.git.primary().Head()

	if err := os.Remove(filepath.Join(dir, "repos", "closed", "HEAD")); err != nil {
		t.Fatal(err)
	}
	if gb.StoreLost() == nil {
		t.Fatal("StoreLost() = nil with a domain's HEAD removed")
	}
	_ = be.SetProgress(ctx, bd, cardByTitle(bd, "one"), 55)
	waitQueue(t, be)
	if got := be.git.primary().Head(); got != head {
		t.Fatalf("a write landed in a surviving domain of a lost store: head %s → %s", head, got)
	}
}

// G90 — a write that arrives while the data directory is lost is refused at
// the door, before anything is applied: a 2xx the drain later rolls back
// tells the caller a change was kept that never was.
func TestARESTWriteIsRefusedAtTheDoorWhileTheStoreIsLost(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv := gitModeServer(t, remote)
	uid := cardUID(t, srv, "tester", "one")
	if err := os.Remove(filepath.Join(srv.gitCfg.DataDir, "repos", "board", "HEAD")); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, "PATCH", "/api/v1/cards/"+uid, `{"progress":55}`)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"code":"storeLost"`) {
		t.Fatalf("PATCH with the store lost = %d %s, want 503 storeLost", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	if strings.Contains(rec.Body.String(), srv.gitCfg.DataDir) {
		t.Fatalf("the refusal discloses the data path: %s", rec.Body.String())
	}
	got := do(t, srv, "GET", "/api/v1/cards/"+uid, "")
	if got.Code != http.StatusOK || strings.Contains(got.Body.String(), `"progress":55`) {
		t.Fatalf("the refused write reached the board: %d %s", got.Code, got.Body.String())
	}
}

// G90 — an MCP tool call is refused the same way, as a tool error the agent
// reads, and the tool never runs: nothing is applied to be rolled back.
func TestAnMCPToolCallIsRefusedWhileTheStoreIsLost(t *testing.T) {
	gb, dir := openLostTestBackend(t, 0, io.Discard)
	if err := os.Remove(filepath.Join(dir, "repos", "board", "HEAD")); err != nil {
		t.Fatal(err)
	}
	ran := false
	h := gb.MCPActionMiddleware()(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		ran = true
		return &mcp.CallToolResult{}, nil
	})
	res, err := h(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "update_card"}})
	if err != nil {
		t.Fatalf("a refused tool call is a tool error, not a protocol error: %v", err)
	}
	if ran {
		t.Fatal("the tool ran with the store lost")
	}
	r, ok := res.(*mcp.CallToolResult)
	if !ok || !r.IsError || len(r.Content) == 0 {
		t.Fatalf("result = %#v, want an error result", res)
	}
	text, _ := r.Content[0].(*mcp.TextContent)
	if text == nil || !strings.Contains(text.Text, "restart required") || strings.Contains(text.Text, dir) {
		t.Fatalf("error text = %#v, want the fixed reason without the data path", r.Content[0])
	}
}
