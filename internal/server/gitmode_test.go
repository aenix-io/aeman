package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Git mode end to end: a server configured with --repo serves the board
// from its clone, a write becomes a commit, the drain pushes it, health
// shows what is unpushed, and an unborn remote is refused with the hint.

func gitModeServer(t *testing.T, remote gitstore.Remote) *Server {
	t.Helper()
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git: &GitConfig{
			Repos:     []RepoSpec{{Name: "board", URL: remote.URL}},
			DataDir:   t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	releaseDataDir(t, srv)
	// Identity without a GitHub token: git mode needs none for the API.
	srv.apiTokens = func(*http.Request) (string, string, error) { return "", "tester", nil }
	srv.gitBE.git.pushDelay = 0 // tests push by hand; a timer firing after the test races TempDir's cleanup
	srv.handler = conforms(t, srv.handler)
	return srv
}

// G59 — the board NAMES its repository, one or many. Every stamp the
// payload carries is a domain name (the store stamps the primary's entries
// too), so a client that reads "no domains" as "the primary is the empty
// string" ends up with two names for one repository — and every rule that
// asks "the same repository?" answers no. On a single-repository board
// that hid "No team", and made the
// grid's × on a subtask look like a delete to the client while the server
// was ungrouping it.
func TestTheBoardNamesItsRepositoryEvenWhenThereIsOnlyOne(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv := gitModeServer(t, remote)
	var got struct {
		Metadata struct {
			Domains []struct {
				Name     string `json:"name"`
				Writable bool   `json:"writable"`
			} `json:"domains"`
			TeamDomains map[string]string `json:"teamDomains"`
		} `json:"metadata"`
	}
	rec := do(t, srv, http.MethodGet, "/api/v1/board", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Metadata.Domains) != 1 || got.Metadata.Domains[0].Name != "board" {
		t.Fatalf("the board names its own repository: %+v", got.Metadata.Domains)
	}
	// And what the roster is stamped with is that same name, in the same
	// namespace the client compares in.
	for team, domain := range got.Metadata.TeamDomains {
		if domain != "board" {
			t.Fatalf("team %q is stamped %q, not the board's own name", team, domain)
		}
	}
}

func TestGitModeServesTheConfiguredBoard(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv := gitModeServer(t, remote)

	// The board comes from the clone; owner/board in the query are ignored —
	// there is exactly one board, the configured repository.
	rec := do(t, srv, http.MethodGet, "/api/v1/board", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /board: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"portal"`) {
		t.Fatalf("board body lacks the seeded team: %s", rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/views/all/cards", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"one"`) || !strings.Contains(rec.Body.String(), `"two"`) {
		t.Fatalf("GET /cards: %d %s", rec.Code, rec.Body.String())
	}

	// A create answers at once with the final id, and its commit follows.
	rec = do(t, srv, http.MethodPost, "/api/v1/views/team/cards", `{"title":"three","team":"portal","zone":"planned","dates":{"start":"2026-08-27"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /cards: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Metadata struct{ UID string } `json:"metadata"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || len(created.Metadata.UID) != 26 {
		t.Fatalf("created uid = %q (%v)", created.Metadata.UID, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	p, _ := gitstore.CardPath(created.Metadata.UID)
	if _, err := srv.gitBE.git.primary().ReadFile(p); err != nil {
		t.Fatalf("created card not committed locally: %v", err)
	}

	// Health names the unpushed age until the drain pushes.
	rec = do(t, srv, http.MethodGet, "/api/healthz", "")
	if !strings.Contains(rec.Body.String(), `"unpushedAgeSeconds"`) {
		t.Fatalf("healthz = %s", rec.Body.String())
	}
	if err := srv.drainAndPush(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	other, err := gitstore.Clone(context.Background(), filesystem.NewStorage(osfs.New(t.TempDir()), cache.NewObjectLRUDefault()), remote, gitstore.Options{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ReadFile(p); err != nil {
		t.Fatalf("the pushed card is not on the remote: %v", err)
	}
	rec = do(t, srv, http.MethodGet, "/api/healthz", "")
	if !strings.Contains(rec.Body.String(), `"unpushedAgeSeconds":0`) {
		t.Fatalf("healthz after push = %s", rec.Body.String())
	}
}

// A repository that was never initialised is refused at start with the
// command that fixes it — not served as an empty board.
func TestGitModeRefusesUnbornRemote(t *testing.T) {
	remote := gitRemote(t) // registered, never pushed to
	_, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git:    &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: t.TempDir()},
	})
	if err == nil || !strings.Contains(err.Error(), "aeman init") {
		t.Fatalf("err = %v, want a refusal naming aeman init", err)
	}
}

// The claim is taken before the directory is touched, so every failure
// after that point has to hand it back: a start that refused once would
// otherwise leave the directory locked until the process died, and the
// second attempt would blame a process that is this very one.
func TestAFailedStartReleasesTheDataDir(t *testing.T) {
	remote := gitRemote(t) // registered, never seeded: the start refuses
	dir := t.TempDir()
	if _, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git:    &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir},
	}); err == nil {
		t.Fatal("an unborn remote must refuse the start")
	}
	l, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("the refused start kept the claim: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}

// A second start over the same data directory reopens the clone instead of
// cloning again, and picks up what the remote gained meanwhile.
func TestGitModeReopensTheClone(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	mk := func() *Server {
		srv, err := New(Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Git:    &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir},
		})
		if err != nil {
			t.Fatal(err)
		}
		srv.apiTokens = func(*http.Request) (string, string, error) { return "", "tester", nil }
		t.Cleanup(func() { _ = srv.Close() })
		srv.handler = conforms(t, srv.handler)
		return srv
	}
	first := mk()
	rec := do(t, first, http.MethodPost, "/api/v1/views/team/cards", `{"title":"offline","team":"portal","zone":"planned","dates":{"start":"2026-08-27"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first.store.waitDrained(ctx)
	// No push: the process "dies" — and dying releases its claim on the
	// data directory, which is what lets the second one open it at all.
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second := mk()
	rec = do(t, second, http.MethodGet, "/api/v1/views/all/cards", "")
	if !strings.Contains(rec.Body.String(), `"offline"`) {
		t.Fatalf("the unpushed card did not survive the restart: %s", rec.Body.String())
	}
	if n, _ := second.gitBE.git.primary().Unpushed(); n != 1 {
		t.Fatalf("unpushed after reopen = %d, want 1", n)
	}
}

// aeman mcp --repo owns its own store: OpenGitBackend hands out the backend
// without an HTTP server, and Drain pushes what a session left behind.
func TestOpenGitBackendStandalone(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	gb, err := OpenGitBackend(&GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gb.Close() })
	ctx := withAction(context.Background(), "01JB4KA0M2P4R6T8V0X2Z4B6M1", "progress")
	bd, err := gb.Backend().LoadBoard(ctx, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Cards) != 2 {
		t.Fatalf("cards = %d", len(bd.Cards))
	}
	if err := gb.Backend().SetProgress(ctx, bd, cardByTitle(bd, "one"), 60); err != nil {
		t.Fatal(err)
	}
	if err := gb.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	other, err := gitstore.Clone(context.Background(), filesystem.NewStorage(osfs.New(t.TempDir()), cache.NewObjectLRUDefault()), remote, gitstore.Options{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := gitstore.CardPath(cardByTitle(bd, "one").ItemID)
	if data, _ := other.ReadFile(p); !strings.Contains(string(data), "progress: 60") {
		t.Fatalf("the drained write is not on the remote:\n%s", data)
	}
}

// A clone whose directory was removed under a running process may have
// been written into afterwards: a branch ref and objects, but no HEAD.
// Reopening it would serve that remnant or refuse every sync, so the start
// sets it aside — kept, not deleted, since it may hold a write never
// pushed — and clones afresh.
func TestOpenGitBackendSetsAsideAStoreWithoutHEAD(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	first, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "repos", "board", "HEAD")); err != nil {
		t.Fatal(err)
	}

	var logs strings.Builder
	second, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("a store without HEAD must be cloned afresh: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	bd, err := second.Backend().LoadBoard(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Cards) != 2 {
		t.Fatalf("cards = %d, want the remote's 2", len(bd.Cards))
	}
	aside, _ := filepath.Glob(filepath.Join(dir, "repos", "board.lost-*"))
	if len(aside) != 1 {
		t.Fatalf("set-aside copies = %v, want one", aside)
	}
	if _, err := os.Stat(filepath.Join(aside[0], "refs", "heads", "main")); err != nil {
		t.Fatalf("the set-aside copy lost its branch: %v", err)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), aside[0]) {
		t.Fatalf("the set-aside is not reported with its path:\n%s", logs.String())
	}
}

// A store emptied under an older binary can be left with neither HEAD nor
// a branch: only the partial packs of fetches that never finished, and a
// tracking ref. The other files may be all that is left of a write, so the
// directory is set aside like a store with a branch; the partial packs —
// never readable by construction — are dropped rather than kept with it.
func TestOpenGitBackendSetsAsideAHeadlessStoreAndDropsItsPartialPacks(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	live := filepath.Join(dir, "repos", "board")
	partial := filepath.Join(live, "objects", "pack", "tmp_pack_123456")
	tracking := filepath.Join(live, "refs", "remotes", "origin", "main")
	for _, p := range []string{partial, tracking} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(partial, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracking, []byte(strings.Repeat("a", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	var logs strings.Builder
	be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("a headless store must be cloned afresh: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	bd, err := be.Backend().LoadBoard(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Cards) != 2 {
		t.Fatalf("cards = %d, want the remote's 2", len(bd.Cards))
	}
	if left, _ := filepath.Glob(filepath.Join(live, "objects", "pack", "tmp_pack_*")); len(left) != 0 {
		t.Fatalf("partial packs survived in the live clone: %v", left)
	}
	aside, _ := filepath.Glob(filepath.Join(dir, "repos", "board.lost-*"))
	if len(aside) != 1 {
		t.Fatalf("set-aside copies = %v, want one", aside)
	}
	if left, _ := filepath.Glob(filepath.Join(aside[0], "objects", "pack", "tmp_pack_*")); len(left) != 0 {
		t.Fatalf("partial packs were kept in the set-aside copy: %v", left)
	}
	if _, err := os.Stat(filepath.Join(aside[0], "refs", "remotes", "origin", "main")); err != nil {
		t.Fatalf("the set-aside copy lost its tracking ref: %v", err)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), aside[0]) {
		t.Fatalf("the set-aside is not reported with its path:\n%s", logs.String())
	}
}

// A clone that holds partial packs next to an intact HEAD and branch is
// reopened as it is, without them.
func TestOpenGitBackendDropsPartialPacksFromAHealthyClone(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	first, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(dir, "repos", "board", "objects", "pack", "tmp_pack_123456")
	if err := os.WriteFile(partial, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}

	var logs strings.Builder
	second, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := os.Stat(partial); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("partial pack survived the reopen: %v", err)
	}
	if aside, _ := filepath.Glob(filepath.Join(dir, "repos", "board.lost-*")); len(aside) != 0 {
		t.Fatalf("a healthy clone was set aside: %v", aside)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "count=1") {
		t.Fatalf("the dropped packs are not reported with their count:\n%s", logs.String())
	}
}

// Dropping partial packs is housekeeping: one that cannot be removed is
// reported and left, and the start goes on with the clone it has.
func TestOpenGitBackendStartsWhenAPartialPackCannotBeDropped(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory whose entries the process cannot remove")
	}
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	first, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	packs := filepath.Join(dir, "repos", "board", "objects", "pack")
	partial := filepath.Join(packs, "tmp_pack_123456")
	if err := os.WriteFile(partial, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(packs, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(packs, 0o750) })

	var logs strings.Builder
	second, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("a partial pack that cannot be removed refused the start: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("the undroppable pack is gone after all: %v", err)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), partial) {
		t.Fatalf("the undroppable pack is not reported with its path:\n%s", logs.String())
	}
}

// A headless directory whose only files were partial packs holds nothing
// once they are dropped, so it is cloned into rather than set aside with a
// warning about unpushed commits it cannot hold.
func TestOpenGitBackendDoesNotSetAsideADirectoryOfOnlyPartialPacks(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	partial := filepath.Join(dir, "repos", "board", "objects", "pack", "tmp_pack_123456")
	if err := os.MkdirAll(filepath.Dir(partial), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	var logs strings.Builder
	be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	bd, err := be.Backend().LoadBoard(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Cards) != 2 {
		t.Fatalf("cards = %d, want the remote's 2", len(bd.Cards))
	}
	if aside, _ := filepath.Glob(filepath.Join(dir, "repos", "board.lost-*")); len(aside) != 0 {
		t.Fatalf("a directory of only partial packs was set aside: %v", aside)
	}
	if strings.Contains(logs.String(), "unpushed") {
		t.Fatalf("warned about unpushed commits that cannot exist:\n%s", logs.String())
	}
}

// A headless directory with a subdirectory that cannot be read may hold
// anything, so it is set aside unread — a rename needs only its parent —
// and the start goes on with a fresh clone.
func TestOpenGitBackendSetsAsideAnUnreadableHeadlessStore(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process cannot read")
	}
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	refs := filepath.Join(dir, "repos", "board", "refs")
	tracking := filepath.Join(refs, "remotes", "origin", "main")
	if err := os.MkdirAll(filepath.Dir(tracking), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracking, []byte(strings.Repeat("a", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(refs, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		locked, _ := filepath.Glob(filepath.Join(dir, "repos", "board*", "refs"))
		for _, p := range locked {
			_ = os.Chmod(p, 0o750)
		}
	})
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("an unreadable headless store refused the start: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	bd, err := be.Backend().LoadBoard(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Cards) != 2 {
		t.Fatalf("cards = %d, want the remote's 2", len(bd.Cards))
	}
	if aside, _ := filepath.Glob(filepath.Join(dir, "repos", "board.lost-*")); len(aside) != 1 {
		t.Fatalf("set-aside copies = %v, want one", aside)
	}
}

// A clone directory may be a symlink to the real one; it is judged by what
// the target holds, and an empty target is cloned into.
func TestOpenGitBackendClonesThroughASymlinkedEmptyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir, target := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "repos"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "repos", "board")); err != nil {
		t.Fatal(err)
	}
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	for _, p := range []string{filepath.Join(dir, "repos", "board"), target} {
		if aside, _ := filepath.Glob(p + ".lost-*"); len(aside) != 0 {
			t.Fatalf("a symlinked empty directory was set aside: %v", aside)
		}
	}
	if fi, err := os.Lstat(filepath.Join(dir, "repos", "board")); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("the clone directory is no longer a symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "refs", "heads", "main")); err != nil {
		t.Fatalf("the clone did not land in the symlink's target: %v", err)
	}
}

// Starting the clone over after a failed shallow attempt empties the
// symlink's target rather than removing it, so a target whose parent the
// process cannot write still gets the clone.
func TestOpenGitBackendClonesThroughALinkToAFixedTarget(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process cannot write")
	}
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir, volume := t.TempDir(), t.TempDir()
	target := filepath.Join(volume, "board")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "repos"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "repos", "board")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(volume, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(volume, 0o750) })
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("a target with a read-only parent refused the start: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if _, err := os.Stat(filepath.Join(target, "refs", "heads", "main")); err != nil {
		t.Fatalf("the clone did not land in the symlink's target: %v", err)
	}
}

// A freshly formatted volume holds only lost+found, which a non-root
// process cannot read; a link to it is cloned into, not set aside.
func TestOpenGitBackendClonesThroughALinkToAFreshVolume(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process cannot read")
	}
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir, target := t.TempDir(), t.TempDir()
	found := filepath.Join(target, "lost+found")
	if err := os.Mkdir(found, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(found, "#12"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(found, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(found, 0o750) })
	if err := os.MkdirAll(filepath.Join(dir, "repos"), 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "repos", "board")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("a link to a fresh volume refused the start: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	for _, p := range []string{link, target} {
		if aside, _ := filepath.Glob(p + ".lost-*"); len(aside) != 0 {
			t.Fatalf("a link to a fresh volume was set aside: %v", aside)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "refs", "heads", "main")); err != nil {
		t.Fatalf("the clone did not land on the volume: %v", err)
	}
}

// A damaged clone directory that is a symlink is refused, naming the link
// and its target: setting either aside could move the clone off the volume
// the link points at, so the operator moves the target aside by hand.
func TestOpenGitBackendRefusesASymlinkedDamagedClone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	for name, damage := range map[string]string{
		"branch without HEAD": filepath.Join("refs", "heads", "main"),
		"headless":            filepath.Join("refs", "remotes", "origin", "main"),
	} {
		t.Run(name, func(t *testing.T) {
			remote := gitRemote(t)
			seedGitRemote(t, remote)
			dir, target := t.TempDir(), t.TempDir()
			left := filepath.Join(target, damage)
			if err := os.MkdirAll(filepath.Dir(left), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(left, []byte(strings.Repeat("a", 40)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dir, "repos"), 0o750); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "repos", "board")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

			be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil {
				_ = be.Close()
				t.Fatal("a symlinked damaged clone started")
			}
			resolved, _ := filepath.EvalSymlinks(target)
			for _, want := range []string{link, resolved, "by hand"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal %q does not name %q", err, want)
				}
			}
			if _, err := os.Stat(left); err != nil {
				t.Fatalf("the target was touched: %v", err)
			}
			for _, p := range []string{link, target} {
				if aside, _ := filepath.Glob(p + ".lost-*"); len(aside) != 0 {
					t.Fatalf("something was set aside: %v", aside)
				}
			}
		})
	}
}

// A plain damaged clone directory that cannot be renamed — a mount point,
// or under a parent the process cannot write — refuses the start, naming
// it and saying to move it aside by hand.
func TestOpenGitBackendRefusesADamagedCloneThatCannotBeSetAside(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process cannot write")
	}
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	repos := filepath.Join(dir, "repos")
	tracking := filepath.Join(repos, "board", "refs", "remotes", "origin", "main")
	if err := os.MkdirAll(filepath.Dir(tracking), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracking, []byte(strings.Repeat("a", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(repos, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(repos, 0o750) })
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		_ = be.Close()
		t.Fatal("a damaged clone that cannot be set aside started")
	}
	for _, want := range []string{filepath.Join(repos, "board"), "by hand"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not name %q", err, want)
		}
	}
}

// A store with HEAD but no branch — unborn, what a failed first clone
// leaves — holds no commit, so it is cloned over and not set aside.
func TestOpenGitBackendClonesOverAnUnbornStore(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	first, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "repos", "board")
	if err := os.Remove(filepath.Join(live, "refs", "heads", "main")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(live, "packed-refs")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}

	second, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("an unborn store must be cloned over: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	bd, err := second.Backend().LoadBoard(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Cards) != 2 {
		t.Fatalf("cards = %d, want the remote's 2", len(bd.Cards))
	}
	if aside, _ := filepath.Glob(filepath.Join(dir, "repos", "board.lost-*")); len(aside) != 0 {
		t.Fatalf("an unborn store was set aside: %v", aside)
	}
}

// An empty clone directory — what the start itself creates — is cloned
// into, with nothing set aside.
func TestOpenGitBackendClonesIntoAnEmptyDirectoryWithoutSettingAside(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "repos", "board"), 0o750); err != nil {
		t.Fatal(err)
	}
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}

	var logs strings.Builder
	be, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if aside, _ := filepath.Glob(filepath.Join(dir, "repos", "board.lost-*")); len(aside) != 0 {
		t.Fatalf("an empty directory was set aside: %v", aside)
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("a plain clone warned:\n%s", logs.String())
	}
}

// A reopened clone is refreshed before its backend is returned. The first
// read must therefore see work another process pushed while this one was
// stopped, without depending on the periodic sync ticker.
func TestOpenGitBackendRefreshesAReopenedCloneBeforeTheFirstRead(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	cfg := &GitConfig{
		Repos:        []RepoSpec{{Name: "board", URL: remote.URL}},
		DataDir:      dir,
		SyncInterval: 0,
	}

	first, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	writer, _ := gitStore(t, remote)
	ctx := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6R1", "progress")
	bd, err := writer.LoadBoard(ctx, "board")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.SetProgress(ctx, bd, cardByTitle(bd, "one"), 73); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, writer)
	if err := writer.syncNowWaiting(context.Background(), "board"); err != nil {
		t.Fatal(err)
	}

	second, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	fresh, err := second.Backend().LoadBoard(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if got := cardByTitle(fresh, "one").Progress; got != 73 {
		t.Fatalf("first read progress = %d, want remote progress 73", got)
	}
}

func TestStartupRefreshReconcilesLocalAndRemoteWork(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}
	first, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	first.be.git.pushDelay = time.Hour
	localCtx := withAction(board.WithActor(context.Background(), "local-author"), "01JB4KA0M2P4R6T8V0X2Z4B6R2", "progress")
	local, err := first.Backend().LoadBoard(localCtx, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Backend().SetProgress(localCtx, local, cardByTitle(local, "one"), 61); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, first.be)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	writer, _ := gitStore(t, remote)
	remoteCtx := withAction(board.WithActor(context.Background(), "remote-author"), "01JB4KA0M2P4R6T8V0X2Z4B6R3", "progress")
	remoteBoard, err := writer.LoadBoard(remoteCtx, "board")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.SetProgress(remoteCtx, remoteBoard, cardByTitle(remoteBoard, "two"), 72); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, writer)
	if err := writer.syncNowWaiting(context.Background(), "board"); err != nil {
		t.Fatal(err)
	}

	second, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	merged, err := second.Backend().LoadBoard(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if got := cardByTitle(merged, "one").Progress; got != 61 {
		t.Fatalf("local progress = %d, want 61", got)
	}
	if got := cardByTitle(merged, "two").Progress; got != 72 {
		t.Fatalf("remote progress = %d, want 72", got)
	}
	commits, err := second.be.git.primary().UnpushedCommits()
	if err != nil || len(commits) != 1 {
		t.Fatalf("unpushed commits = %d, %v; want the replayed local commit", len(commits), err)
	}
	if commits[0].Author.Name != "local-author" {
		t.Fatalf("replayed author = %q, want local-author", commits[0].Author.Name)
	}
}

func TestStartupRefreshAdoptsAnAlreadyFetchedTrackingTip(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}
	first, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	oldHead := first.be.git.primary().Head()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	writer, _ := gitStore(t, remote)
	ctx := withAction(context.Background(), "01JB4KA0M2P4R6T8V0X2Z4B6R4", "progress")
	bd, _ := writer.LoadBoard(ctx, "board")
	if err := writer.SetProgress(ctx, bd, cardByTitle(bd, "one"), 84); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, writer)
	if err := writer.syncNowWaiting(context.Background(), "board"); err != nil {
		t.Fatal(err)
	}

	cloneDir := filepath.Join(dir, "repos", "board")
	storer := filesystem.NewStorage(osfs.New(cloneDir), cache.NewObjectLRUDefault())
	repo := gitstore.Open(storer, gitstore.Options{Dir: cloneDir})
	if _, moved, err := repo.Fetch(context.Background(), remote); err != nil || !moved {
		t.Fatalf("pre-fetch = moved %v, %v", moved, err)
	}
	if repo.Head() != oldHead {
		t.Fatal("the setup fetch unexpectedly adopted the tracking tip")
	}

	second, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	fresh, _ := second.Backend().LoadBoard(context.Background(), "ignored")
	if got := cardByTitle(fresh, "one").Progress; got != 84 {
		t.Fatalf("first read progress = %d, want 84 from the already-fetched tip", got)
	}
}

func TestStartupRefreshIncludesEveryDomain(t *testing.T) {
	shared, closed := gitRemoteN(t, "shared"), gitRemoteN(t, "closed")
	seedGitRemote(t, shared)
	seedClosedRemote(t, closed)
	dir := t.TempDir()
	cfg := &GitConfig{Repos: []RepoSpec{{Name: "shared", URL: shared.URL}, {Name: "closed", URL: closed.URL}}, DataDir: dir}
	first, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	writer, _ := gitStore(t, closed)
	ctx := withAction(context.Background(), "01JB4KA0M2P4R6T8V0X2Z4B6R5", "progress")
	bd, _ := writer.LoadBoard(ctx, "closed")
	if err := writer.SetProgress(ctx, bd, cardByTitle(bd, "three-closed"), 88); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, writer)
	if err := writer.syncNowWaiting(context.Background(), "closed"); err != nil {
		t.Fatal(err)
	}

	second, err := OpenGitBackend(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	fresh, _ := second.Backend().LoadBoard(context.Background(), "ignored")
	if got := cardByTitle(fresh, "three-closed").Progress; got != 88 {
		t.Fatalf("secondary-domain progress = %d, want 88", got)
	}
}

func TestStartupRefreshOnlyContinuesForNetworkOutages(t *testing.T) {
	networkErr := &net.DNSError{Err: "temporary failure", Name: "forge.test", IsTemporary: true}
	for name, err := range map[string]error{
		"deadline": context.DeadlineExceeded,
		"network":  networkErr,
	} {
		t.Run(name, func(t *testing.T) {
			if !recoverableRefreshError(err) {
				t.Fatalf("%v should preserve the intact offline clone", err)
			}
		})
	}
	for name, err := range map[string]error{
		"authentication": transport.ErrAuthenticationRequired,
		"authorization":  transport.ErrAuthorizationFailed,
		"storage":        errors.New("corrupt object"),
	} {
		t.Run(name, func(t *testing.T) {
			if recoverableRefreshError(err) {
				t.Fatalf("%v was incorrectly classified as a network outage", err)
			}
		})
	}
}

// The daemon's whole health signal is this number, and this is the only
// place the delegation to the store is exercised: /healthz's own test
// supplies its own health function, so nothing there would notice this
// reading the wrong board. It must not rest on unpushedAge ignoring the
// key it is given.
func TestOpenGitBackendReportsItsUnpushedAge(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	gb, err := OpenGitBackend(&GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: t.TempDir()},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gb.Close() })

	if age := gb.UnpushedAge(); age != 0 {
		t.Fatalf("a fresh clone has nothing waiting, got %v", age)
	}

	ctx := withAction(context.Background(), "01JB4KA0M2P4R6T8V0X2Z4B6M1", "progress")
	bd, err := gb.Backend().LoadBoard(ctx, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if err := gb.Backend().SetProgress(ctx, bd, cardByTitle(bd, "one"), 60); err != nil {
		t.Fatal(err)
	}
	// The write has to become a commit before it counts as unpushed.
	wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gb.be.store.waitDrained(wait)
	if age := gb.UnpushedAge(); age <= 0 {
		t.Fatalf("a committed, unpushed write is not reported: %v", age)
	}

	if err := gb.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if age := gb.UnpushedAge(); age != 0 {
		t.Fatalf("after the push nothing is waiting, got %v", age)
	}
}

// The rejection replaced an index-out-of-range panic on cfg.Repos[0].
func TestOpenGitBackendRefusesNoRepository(t *testing.T) {
	_, err := OpenGitBackend(&GitConfig{DataDir: t.TempDir()},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("a backend with no repository was opened")
	}
	if !strings.Contains(err.Error(), "--repo") {
		t.Fatalf("the refusal does not name the flag: %v", err)
	}
}

// G62 — one process owns a board's clones. A second `aeman mcp` on the same
// --data is refused at start, naming the directory, rather than left to
// race the first one's fetch and push; and the refusal lasts exactly as
// long as the first process holds the directory.
func TestOpenGitBackendRefusesASecondProcessOnTheSameData(t *testing.T) {
	// What this pins is the refusal, not the grace a start gives a
	// predecessor that is leaving: this holder is not going anywhere.
	restore := dataLockWait
	dataLockWait = 50 * time.Millisecond
	t.Cleanup(func() { dataLockWait = restore })
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	open := func() (*GitBackend, error) {
		return OpenGitBackend(&GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir},
			slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	first, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(); err == nil {
		t.Fatal("a second backend opened the data directory the first one holds")
	} else if !strings.Contains(err.Error(), dir) {
		t.Fatalf("the refusal does not name the directory: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, err := open()
	if err != nil {
		t.Fatalf("the directory is still claimed after Close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// The claim does not care which subcommand took it: `aeman serve` and
// `aeman mcp` are one process each, and the second of them is told to talk
// to the first over HTTP instead of opening the clones a second time.
func TestServeAndMCPCannotShareOneDataDir(t *testing.T) {
	// What this pins is the refusal, not the grace a start gives a
	// predecessor that is leaving: this holder is not going anywhere.
	restore := dataLockWait
	dataLockWait = 50 * time.Millisecond
	t.Cleanup(func() { dataLockWait = restore })
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(Options{
		Logger: log,
		Git:    &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	mcpCfg := &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: dir}
	if _, err := OpenGitBackend(mcpCfg, log); err == nil {
		t.Fatal("aeman mcp opened the data directory the server holds")
	} else if !strings.Contains(err.Error(), "aeman mcp --listen") {
		t.Fatalf("the refusal does not point at the shared daemon: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	gb, err := OpenGitBackend(mcpCfg, log)
	if err != nil {
		t.Fatalf("the server's Close did not release the directory: %v", err)
	}
	if err := gb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// The request's action name comes from its route.
func TestActionNameFromRoute(t *testing.T) {
	cases := map[[2]string]string{
		{http.MethodPost, "/api/v1/views/team/cards"}:                 "create",
		{http.MethodPatch, "/api/v1/cards/01J"}:                       "update",
		{http.MethodDelete, "/api/v1/cards/01J"}:                      "delete",
		{http.MethodPost, "/api/v1/cards/01J/actions/send-to-review"}: "send-to-review",
		{http.MethodPost, "/api/v1/cards/01J/notes"}:                  "note",
		{http.MethodPatch, "/api/v1/cards/01J/notes/01K"}:             "note-edit",
		{http.MethodDelete, "/api/v1/cards/01J/notes/01K"}:            "note-delete",
		{http.MethodPost, "/api/v1/sprints/actions/carry-over"}:       "carry-over",
		{http.MethodPost, "/api/v1/customers"}:                        "add-customer",
		{http.MethodPost, "/api/v1/customers/actions/rename"}:         "rename",
		{http.MethodGet, "/api/v1/views/team/cards"}:                  "",
	}
	for in, want := range cases {
		if got := actionName(in[0], in[1]); got != want {
			t.Fatalf("actionName(%s %s) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

// Two repositories that already declare the same team cannot be served as
// one board: the store would merge them into
// one and the cards of two different teams would mix. The server refuses to
// start and names both files, so a maintainer renames one and starts again.
func TestGitModeRefusesCollidingRosterNames(t *testing.T) {
	shared, closed := gitRemoteN(t, "shared"), gitRemoteN(t, "closed")
	seedGitRemote(t, shared) // team portal at teams/01JB4TEAM.yaml
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 4\ntitle: closed\n")},
		{Path: gitstore.TeamPath("01JB4TEAMX"), Data: []byte("name: portal\nrank: a\ncreated: 2026-07-01T08:00:00Z\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), closed); err != nil {
		t.Fatal(err)
	}
	_, err = New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git: &GitConfig{Repos: []RepoSpec{{Name: "shared", URL: shared.URL}, {Name: "closed", URL: closed.URL}},
			DataDir: t.TempDir()},
	})
	if err == nil {
		t.Fatal("two domains declaring team portal must not be served as one board")
	}
	for _, want := range []string{`team "portal"`, "shared/teams/01JB4TEAM.yaml", "closed/teams/01JB4TEAMX.yaml", "rename"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q lacks %q", err.Error(), want)
		}
	}
}

// A failed first clone leaves an unborn store: HEAD and config, no branch.
// It holds nothing a later start must keep, so that start still takes the
// shallow clone rather than the full one — on a long history the full one
// may not fit the start's clone deadline. The test remote does not speak
// shallow, so the seam serves the shallow attempt with a full clone and
// records the depths asked for.
func TestAnUnbornStoreIsClonedShallow(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	dir := filepath.Join(t.TempDir(), "board")
	if _, err := git.Init(filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault()), nil); err != nil {
		t.Fatal(err)
	}
	var depths []int
	cloneRepo = func(ctx context.Context, s storage.Storer, rem gitstore.Remote, opts gitstore.Options, depth int) (*gitstore.Repo, error) {
		depths = append(depths, depth)
		return gitstore.Clone(ctx, s, rem, opts, 0)
	}
	t.Cleanup(func() { cloneRepo = gitstore.Clone })

	r, reopened, err := cloneOrOpen(dir, remote, gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}}, remote.URL, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if reopened || r.Head().IsZero() {
		t.Fatalf("reopened=%v head=%v, want a fresh clone with a branch", reopened, r.Head())
	}
	if len(depths) != 1 || depths[0] != 1 {
		t.Fatalf("clone depths asked = %v, want one shallow attempt [1]", depths)
	}
}
