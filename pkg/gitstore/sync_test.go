package gitstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Sync is fetch, push, deepen. The non-shallow paths run against go-git's
// in-process server (hermetic, no binary); the shallow paths need the real
// git-upload-pack — that server rejects shallow requests — and skip when
// the binaries are absent unless CI says they must be there.

// testRemote registers an in-memory bare repository on the "test" scheme
// and returns its URL.
var testRemotes = server.MapLoader{}

func init() {
	client.InstallProtocol("test", server.NewClient(testRemotes))
}

func newTestRemote(t *testing.T) (Remote, *memory.Storage) {
	t.Helper()
	st := memory.NewStorage()
	url := "test://remotes/" + strings.ReplaceAll(t.Name(), "/", "_") + ".git"
	testRemotes[url] = st
	return Remote{URL: url}, st
}

// A local repo whose first commit seeds the remote, so tests have a shared
// starting point. Cloned copies come from Clone.
func seedRemote(t *testing.T, remote Remote) plumbing.Hash {
	t.Helper()
	r := newRepo(t)
	h, err := r.Commit(Action{Name: "import", Summary: "seed", At: at("2026-08-01T09:00:00Z")}, []FileWrite{
		{Path: BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 40\n---\n")},
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	return h
}

func cloneFull(t *testing.T, remote Remote) *Repo {
	t.Helper()
	r, err := Clone(context.Background(), memory.NewStorage(), remote, Options{Committer: serverID, Branch: plumbing.NewBranchReferenceName("main")}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// G19 — a commit pushed from elsewhere arrives by fetch, and the diff names
// exactly the touched paths so the cache reloads one card, not the board.
func TestFetchBringsRemoteCommitAndDiffNamesTouchedPaths(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	a := cloneFull(t, remote)
	b := cloneFull(t, remote)

	old := a.Head()
	if _, err := b.Commit(Action{Name: "progress", Actor: "timur", Summary: "b writes"}, []FileWrite{
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nprogress: 10\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}

	tip, changed, err := a.Fetch(context.Background(), remote)
	if err != nil || !changed {
		t.Fatalf("fetch: changed=%v err=%v", changed, err)
	}
	if tip != b.Head() {
		t.Fatalf("remote tip %v, want %v", tip, b.Head())
	}
	paths, err := a.ChangedPaths(old, tip)
	if err != nil || len(paths) != 1 || paths[0] != "cards/b/2/B2.md" {
		t.Fatalf("changed paths = %v %v", paths, err)
	}
	// Nothing new the second time.
	if _, changed, err := a.Fetch(context.Background(), remote); err != nil || changed {
		t.Fatalf("second fetch: changed=%v err=%v", changed, err)
	}
	// The local branch has not moved by itself — fetch reports, the caller
	// decides when to adopt the tip (after re-applying its own queue).
	if a.Head() != old {
		t.Fatal("fetch moved the local branch")
	}
	if err := a.ResetTo(tip); err != nil {
		t.Fatal(err)
	}
	if a.Head() != tip {
		t.Fatal("ResetTo did not move the branch")
	}
}

// G10 — a rejected push is recognised by fetching, never by error type: new
// commits arrived → re-apply and retry; nothing new → the push really
// failed and the local commits stay.
func TestRejectedPushIsDetectedByFetch(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	a := cloneFull(t, remote)
	b := cloneFull(t, remote)

	if _, err := a.Commit(Action{Name: "rename", Actor: "kvaps", Summary: "a"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a renamed\nprogress: 40\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}

	// b's write, on the old tip.
	bHead, err := b.Commit(Action{Name: "progress", Actor: "timur", Summary: "b"}, []FileWrite{
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nprogress: 10\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	pushErr := b.Push(context.Background(), remote)
	if pushErr == nil {
		t.Fatal("a non-fast-forward push must be rejected")
	}
	tip, changed, err := b.Fetch(context.Background(), remote)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("the fetch after a rejected push must report the remote moved")
	}
	// Local commits are still there, untouched, for re-application.
	if b.Head() != bHead {
		t.Fatal("a rejected push lost the local commit")
	}
	// Re-apply: reset onto the remote tip and redo the write on top.
	if err := b.ResetTo(tip); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Commit(Action{Name: "progress", Actor: "timur", Summary: "b again"}, []FileWrite{
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nprogress: 10\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Push(context.Background(), remote); err != nil {
		t.Fatalf("retry after re-apply: %v", err)
	}
	// Both writes are on the remote.
	c := cloneFull(t, remote)
	if got, _ := c.ReadFile("cards/a/1/A1.md"); !strings.Contains(string(got), "a renamed") {
		t.Fatalf("a's write lost: %s", got)
	}
	if got, _ := c.ReadFile("cards/b/2/B2.md"); !strings.Contains(string(got), "progress: 10") {
		t.Fatalf("b's write lost: %s", got)
	}
}

// When the remote cannot be reached at all, push and fetch both fail: the
// caller learns "nothing new, push failed" and keeps its commits.
func TestUnreachableRemoteKeepsCommits(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	a := cloneFull(t, remote)
	h, err := a.Commit(Action{Name: "progress", Actor: "kvaps", Summary: "x"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 90\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	gone := Remote{URL: "test://remotes/does-not-exist.git"}
	if err := a.Push(context.Background(), gone); err == nil {
		t.Fatal("push to a missing remote must fail")
	}
	if _, changed, err := a.Fetch(context.Background(), gone); err == nil || changed {
		t.Fatalf("fetch from a missing remote: changed=%v err=%v", changed, err)
	}
	if a.Head() != h {
		t.Fatal("the failed push lost the local commit")
	}
	if n, err := a.Unpushed(); err != nil || n != 1 {
		t.Fatalf("unpushed = %d %v, want 1", n, err)
	}
}

// G20 — unpushed commits live in the object store, so reopening the same
// directory finds them queued, not gone.
func TestUnpushedSurvivesReopen(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	dir := t.TempDir()
	open := func() *Repo {
		return Open(filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault()), Options{Committer: serverID})
	}
	a, err := Clone(context.Background(), filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault()), remote, Options{Committer: serverID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.Commit(Action{Name: "progress", Actor: "kvaps", Summary: "offline"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 70\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	// "Restart": a fresh Repo over the same directory.
	b := open()
	if b.Head() != h {
		t.Fatalf("reopened head %v, want %v", b.Head(), h)
	}
	if n, _ := b.Unpushed(); n != 1 {
		t.Fatalf("unpushed after reopen = %d, want 1", n)
	}
	if err := b.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	if n, _ := b.Unpushed(); n != 0 {
		t.Fatalf("unpushed after push = %d, want 0", n)
	}
}

// G21 — repack and prune leave every object reachable.
func TestRepackKeepsHistoryReadable(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault()), Options{Committer: serverID})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if _, err := r.Commit(Action{Name: "progress", Actor: "kvaps", Summary: "step", At: at("2026-08-01T09:00:00Z").Add(time.Duration(i) * time.Hour)}, []FileWrite{
			{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: " + strings.Repeat("1", i%9+1) + "\n---\n")},
		}); err != nil {
			t.Fatal(err)
		}
	}
	before := countFiles(t, filepath.Join(dir, "objects"))
	if err := r.Maintain(); err != nil {
		t.Fatal(err)
	}
	after := countFiles(t, filepath.Join(dir, "objects"))
	if after >= before/4 {
		t.Fatalf("repack did not consolidate: %d files before, %d after", before, after)
	}
	n := 0
	if err := r.Walk(r.Head(), func(*object.Commit) (bool, error) { n++; return true, nil }); err != nil || n != 40 {
		t.Fatalf("history after repack: %d commits, %v", n, err)
	}
	if _, err := Load(r); err != nil {
		t.Fatalf("load after repack: %v", err)
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ---- shallow paths: real git-upload-pack over the file transport ---------

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		if os.Getenv("AEMAN_TEST_REQUIRE_GIT") != "" {
			t.Fatal("AEMAN_TEST_REQUIRE_GIT is set and git is not on PATH")
		}
		t.Skip("git not on PATH; shallow paths need git-upload-pack")
	}
}

// bareRemote makes a real bare repository and seeds it with n dated
// commits, one per day from 2026-01-01, all touching card A1.
func bareRemote(t *testing.T, n int) (Remote, []plumbing.Hash) {
	t.Helper()
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", "-b", "main", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	remote := Remote{URL: dir}
	r := newRepo(t)
	var hs []plumbing.Hash
	for i := 0; i < n; i++ {
		when := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, i)
		h, err := r.Commit(Action{Name: "progress", Actor: "kvaps", At: when, Summary: "day"}, []FileWrite{
			{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: " + strings.Repeat("1", i%9+1) + "\n---\n")},
		})
		if err != nil {
			t.Fatal(err)
		}
		hs = append(hs, h)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	return remote, hs
}

// G9 — a depth-1 clone, deepened to a date, lands exactly at that date;
// deepening again goes further; past the root leaves no boundary at all.
func TestShallowCloneDeepenSince(t *testing.T) {
	remote, hs := bareRemote(t, 30)
	r, err := Clone(context.Background(), memory.NewStorage(), remote, Options{Committer: serverID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n := walkCount(t, r); n != 1 {
		t.Fatalf("depth-1 clone walks %d commits, want 1", n)
	}
	if r.Head() != hs[29] {
		t.Fatalf("head %v, want the remote tip %v", r.Head(), hs[29])
	}
	// Ten days back: commits from 2026-01-20 on → 11 commits (days 20..30).
	since := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	if err := r.DeepenSince(context.Background(), remote, since); err != nil {
		t.Fatal(err)
	}
	if n := walkCount(t, r); n != 11 {
		t.Fatalf("after deepen-since %s: %d commits, want 11", since.Format("2006-01-02"), n)
	}
	log, _ := r.CardLog("A1", 0)
	if log.TruncatedBefore.IsZero() || len(log.Entries) != 11 {
		t.Fatalf("log after deepen: %d entries, truncatedBefore %v", len(log.Entries), log.TruncatedBefore)
	}
	// Further back.
	if err := r.DeepenSince(context.Background(), remote, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if n := walkCount(t, r); n != 21 {
		t.Fatalf("second deepen: %d commits, want 21", n)
	}
	// Past the root: everything, and no shallow entry remains.
	if err := r.DeepenSince(context.Background(), remote, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if n := walkCount(t, r); n != 30 {
		t.Fatalf("full deepen: %d commits, want 30", n)
	}
	if sh, _ := r.Storer().Shallow(); len(sh) != 0 {
		t.Fatalf("shallow entries left after reaching the root: %v", sh)
	}
	log, _ = r.CardLog("A1", 0)
	if !log.TruncatedBefore.IsZero() {
		t.Fatalf("full history still reports truncation: %v", log.TruncatedBefore)
	}
}

// Pushing from a shallow clone works, and a plain fetch into it brings new
// commits without adding boundaries.
func TestShallowClonePushAndFetch(t *testing.T) {
	remote, hs := bareRemote(t, 10)
	r, err := Clone(context.Background(), memory.NewStorage(), remote, Options{Committer: serverID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	h, err := r.Commit(Action{Name: "progress", Actor: "kvaps", Summary: "from shallow"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 100\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatalf("push from a shallow clone: %v", err)
	}
	other, err := Clone(context.Background(), memory.NewStorage(), remote, Options{Committer: serverID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if other.Head() != h {
		t.Fatalf("fresh clone head %v, want the pushed %v", other.Head(), h)
	}
	// Someone else pushes; the everyday fetch brings it without a new boundary.
	if _, err := other.Commit(Action{Name: "progress", Actor: "timur", Summary: "elsewhere"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 90\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := other.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	before, _ := r.Storer().Shallow()
	tip, changed, err := r.Fetch(context.Background(), remote)
	if err != nil || !changed || tip != other.Head() {
		t.Fatalf("fetch: tip=%v changed=%v err=%v", tip, changed, err)
	}
	after, _ := r.Storer().Shallow()
	if len(after) != len(before) {
		t.Fatalf("a plain fetch added shallow entries: %v → %v", before, after)
	}
	_ = hs
}

func walkCount(t *testing.T, r *Repo) int {
	t.Helper()
	n := 0
	if err := r.Walk(r.Head(), func(*object.Commit) (bool, error) { n++; return true, nil }); err != nil {
		t.Fatal(err)
	}
	return n
}
