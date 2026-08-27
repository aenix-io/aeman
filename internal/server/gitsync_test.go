package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The store over the git backend: the cache answers in milliseconds as
// before; behind it one request becomes one commit, coalesced writes commit
// once with their final value, and a background sync pushes, fetches and
// re-applies. These tests drive the store the way the service does.

var gitTestRemotes = server.MapLoader{}

func init() {
	client.InstallProtocol("gittest", server.NewClient(gitTestRemotes))
}

func gitRemote(t *testing.T) gitstore.Remote {
	t.Helper()
	url := "gittest://remotes/" + strings.ReplaceAll(t.Name(), "/", "_") + ".git"
	gitTestRemotes[url] = memory.NewStorage()
	return gitstore.Remote{URL: url}
}

// seedGitRemote pushes a small board to the remote and returns nothing; the
// stores under test clone from it.
func seedGitRemote(t *testing.T, remote gitstore.Remote) {
	t.Helper()
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("_"), Data: []byte("rank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
		{Path: "cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md", Data: []byte("---\ntitle: one\nteam: portal\nzone: yellow\nprogress: 40\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n")},
		{Path: "cards/b/2/01JB4K2E7QZMX3R8V0N5T9WYB2.md", Data: []byte("---\ntitle: two\nteam: portal\nzone: gray\nrank: b\ncreated: 2026-08-26T09:15:03Z\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
}

// gitStore is one replica: its own clone, cache and sync.
func gitStore(t *testing.T, remote gitstore.Remote) (*storeBackend, *gitstore.Repo) {
	t.Helper()
	repo, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	store := newBoardStore()
	be := newGitBackend(store, []gitDomain{{Domain: gitstore.Domain{Name: "board", Repo: repo}, remote: remote}}, gitOptions{})
	be.git.pushDelay = 0 // tests drive the sync by hand
	return be, repo
}

func waitQueue(t *testing.T, be *storeBackend) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be.store.waitDrained(ctx)
}

func commitsSince(t *testing.T, r *gitstore.Repo, since plumbing.Hash) []*object.Commit {
	t.Helper()
	var out []*object.Commit
	err := r.Walk(r.Head(), func(c *object.Commit) (bool, error) {
		if c.Hash == since {
			return false, nil
		}
		out = append(out, c)
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func cardByTitle(b board.Board, title string) board.Card {
	for _, c := range b.Cards {
		if c.Title == title {
			return c
		}
	}
	return board.Card{}
}

// G4 — the writes of one request land in ONE commit, whatever their number,
// with the event payload as a trailer.
func TestGitOneRequestOneCommit(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	be, repo := gitStore(t, remote)
	ctx := withAction(board.WithActor(context.Background(), "kvaps"), "01JB4KA0M2P4R6T8V0X2Z4B6D8", "send-to-review")
	bd, err := be.LoadBoard(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	card := cardByTitle(bd, "one")
	before := repo.Head()
	if err := be.SetProgress(ctx, bd, card, 90); err != nil {
		t.Fatal(err)
	}
	if err := be.SetStage(ctx, bd, card, board.StageReview); err != nil {
		t.Fatal(err)
	}
	if err := be.AppendEvent(ctx, bd, card, board.Event{Kind: board.EventReviewSent, To: "timur"}); err != nil {
		t.Fatal(err)
	}
	// The cache already says so.
	now, _ := be.LoadBoard(ctx, "acme", 1)
	if c := cardByTitle(now, "one"); c.Progress != 90 || c.Stage != board.StageReview {
		t.Fatalf("cache = %+v", c)
	}
	waitQueue(t, be)
	commits := commitsSince(t, repo, before)
	if len(commits) != 1 {
		t.Fatalf("%d commits for one request, want 1", len(commits))
	}
	tr := gitstore.ParseTrailers(commits[0].Message)
	if tr.Action != "send-to-review" || tr.ActionID != "01JB4KA0M2P4R6T8V0X2Z4B6D8" || tr.Actor != "kvaps" {
		t.Fatalf("trailers = %+v", tr)
	}
	if len(tr.Changes) != 1 || tr.Changes[0].Kind != board.EventReviewSent || tr.Changes[0].To != "timur" {
		t.Fatalf("changes = %+v", tr.Changes)
	}
	if commits[0].Author.Name != "kvaps" {
		t.Fatalf("author = %s", commits[0].Author.Name)
	}
	p, _ := gitstore.CardPath(card.ItemID)
	data, _ := repo.ReadFile(p)
	if !strings.Contains(string(data), "progress: 90") || !strings.Contains(string(data), "stage: review") {
		t.Fatalf("file after the commit:\n%s", data)
	}
}

// G5 — a slider's writes coalesce into one commit with the final value; two
// actors on the same slider are two commits, each attributed.
func TestGitProgressCoalescesByActor(t *testing.T) {
	old := coalesceWindow
	coalesceWindow = 40 * time.Millisecond
	defer func() { coalesceWindow = old }()

	remote := gitRemote(t)
	seedGitRemote(t, remote)
	be, repo := gitStore(t, remote)
	bd, _ := be.LoadBoard(context.Background(), "acme", 1)
	card := cardByTitle(bd, "one")

	before := repo.Head()
	for i, v := range []int{50, 60, 70, 80} {
		ctx := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6E"+string(rune('0'+i)), "progress")
		if err := be.SetProgress(ctx, bd, card, v); err != nil {
			t.Fatal(err)
		}
	}
	waitQueue(t, be)
	commits := commitsSince(t, repo, before)
	if len(commits) != 1 {
		t.Fatalf("%d commits for one drag, want 1", len(commits))
	}
	p, _ := gitstore.CardPath(card.ItemID)
	if data, _ := repo.ReadFile(p); !strings.Contains(string(data), "progress: 80") {
		t.Fatalf("final value not the last write:\n%s", data)
	}

	// Two people on the same slider: neither overwrites the other silently.
	before = repo.Head()
	a := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6F1", "progress")
	b := withAction(board.WithActor(context.Background(), "bob"), "01JB4KA0M2P4R6T8V0X2Z4B6F2", "progress")
	if err := be.SetProgress(a, bd, card, 85); err != nil {
		t.Fatal(err)
	}
	if err := be.SetProgress(b, bd, card, 30); err != nil {
		t.Fatal(err)
	}
	if err := be.SetProgress(a, bd, card, 88); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, be)
	commits = commitsSince(t, repo, before)
	if len(commits) != 2 {
		t.Fatalf("%d commits for two actors, want 2", len(commits))
	}
	authors := commits[0].Author.Name + "," + commits[1].Author.Name
	if !strings.Contains(authors, "alice") || !strings.Contains(authors, "bob") {
		t.Fatalf("authors = %s", authors)
	}
}

// G5 — an action on a card flushes that card's pending coalesced writes
// first: slider→100 then send-to-review commits the progress, then the
// review — never the review under a stale 100.
func TestGitActionFlushesCoalescedFirst(t *testing.T) {
	old := coalesceWindow
	coalesceWindow = 200 * time.Millisecond
	defer func() { coalesceWindow = old }()

	remote := gitRemote(t)
	seedGitRemote(t, remote)
	be, repo := gitStore(t, remote)
	bd, _ := be.LoadBoard(context.Background(), "acme", 1)
	card := cardByTitle(bd, "one")
	before := repo.Head()

	drag := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6G1", "progress")
	if err := be.SetProgress(drag, bd, card, 100); err != nil {
		t.Fatal(err)
	}
	review := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6G2", "send-to-review")
	if err := be.SetStage(review, bd, card, board.StageReview); err != nil {
		t.Fatal(err)
	}
	if err := be.SetProgress(review, bd, card, 90); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, be)
	commits := commitsSince(t, repo, before) // newest first
	if len(commits) != 2 {
		t.Fatalf("%d commits, want the drag and the review", len(commits))
	}
	if gitstore.ParseTrailers(commits[1].Message).Action != "progress" || gitstore.ParseTrailers(commits[0].Message).Action != "send-to-review" {
		t.Fatalf("order: %q then %q", firstLine(commits[1].Message), firstLine(commits[0].Message))
	}
	// The drag's commit recorded doneFrom: 40 on the way to 100; the review's
	// clamp to 90 then cleared it — a drop below 100 is what a reopen is, and
	// today's Reopen restores the LAST jump's from-side too.
	p, _ := gitstore.CardPath(card.ItemID)
	if data, _ := repo.ReadFile(p); !strings.Contains(string(data), "progress: 90") || strings.Contains(string(data), "doneFrom:") {
		t.Fatalf("final file (progress 90 after the clamp, doneFrom cleared):\n%s", data)
	}
	drag100, _ := repo.CommitObject(commits[1].Hash)
	tree, _ := drag100.Tree()
	f, _ := tree.File(p)
	if s, _ := f.Contents(); !strings.Contains(s, "progress: 100") || !strings.Contains(s, "doneFrom: 40") {
		t.Fatalf("the drag's own commit must carry progress 100 and doneFrom 40:\n%s", s)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// A create hands out the FINAL id at once — the store mints it — so there is
// no provisional id to alias later.
func TestGitCreateReturnsFinalID(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	be, repo := gitStore(t, remote)
	ctx := withAction(board.WithActor(context.Background(), "kvaps"), "01JB4KA0M2P4R6T8V0X2Z4B6H1", "create")
	bd, _ := be.LoadBoard(ctx, "acme", 1)
	c, err := be.CreateCard(ctx, bd, board.CreateInput{Title: "three", Team: "portal", Zone: board.ZoneRed})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ItemID) != 26 || strings.HasPrefix(c.ItemID, localIDPrefix) {
		t.Fatalf("id = %q, want a final ULID", c.ItemID)
	}
	if now, _ := be.LoadBoard(ctx, "acme", 1); cardByTitle(now, "three").ItemID != c.ItemID {
		t.Fatal("the cache does not show the created card under its id")
	}
	waitQueue(t, be)
	p, _ := gitstore.CardPath(c.ItemID)
	if _, err := repo.ReadFile(p); err != nil {
		t.Fatalf("created card not on disk: %v", err)
	}
}

// G19 — a commit pushed by another replica reaches this one's cache on the
// next sync, as one MODIFIED frame for the touched card.
func TestGitRemoteChangeReachesCache(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	a, _ := gitStore(t, remote)
	b, _ := gitStore(t, remote)
	ctx := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6J1", "progress")
	bdA, _ := a.LoadBoard(ctx, "acme", 1)
	bdB, _ := b.LoadBoard(ctx, "acme", 1)
	if cardByTitle(bdB, "one").Progress != 40 {
		t.Fatal("precondition")
	}
	sub, cancel := b.store.subscribe("acme/1", "", nil, map[string]bool{"cards": true})
	defer cancel()

	if err := a.SetProgress(ctx, bdA, cardByTitle(bdA, "one"), 75); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, a)
	if err := a.syncNow(context.Background(), "acme/1"); err != nil {
		t.Fatalf("a push: %v", err)
	}
	if err := b.syncNow(context.Background(), "acme/1"); err != nil {
		t.Fatalf("b fetch: %v", err)
	}
	now, _ := b.LoadBoard(ctx, "acme", 1)
	if got := cardByTitle(now, "one").Progress; got != 75 {
		t.Fatalf("b's cache progress = %d, want 75", got)
	}
	modified := 0
	for done := false; !done; {
		select {
		case data := <-sub.ch:
			if strings.Contains(string(data), `"MODIFIED"`) && strings.Contains(string(data), `"Card"`) {
				modified++
			}
		default:
			done = true
		}
	}
	if modified != 1 {
		t.Fatalf("%d MODIFIED card frames, want exactly 1", modified)
	}
}

// G10/G11 — a rejected push is re-applied on the new tip and retried; two
// replicas writing different fields of one card both land.
func TestGitRejectedPushReappliesAndRetries(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	a, _ := gitStore(t, remote)
	b, _ := gitStore(t, remote)
	ctxA := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6K1", "rename")
	ctxB := withAction(board.WithActor(context.Background(), "bob"), "01JB4KA0M2P4R6T8V0X2Z4B6K2", "progress")
	bdA, _ := a.LoadBoard(ctxA, "acme", 1)
	bdB, _ := b.LoadBoard(ctxB, "acme", 1)
	one := cardByTitle(bdA, "one")

	if err := a.RenameCard(ctxA, bdA, one, "one, renamed"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetProgress(ctxB, bdB, cardByTitle(bdB, "one"), 65); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, a)
	waitQueue(t, b)
	if err := a.syncNow(context.Background(), "acme/1"); err != nil {
		t.Fatalf("a push: %v", err)
	}
	// b's push is rejected, re-applied on a's tip, pushed.
	if err := b.syncNow(context.Background(), "acme/1"); err != nil {
		t.Fatalf("b sync: %v", err)
	}
	if n, _ := b.git.primary().Unpushed(); n != 0 {
		t.Fatalf("b still has %d unpushed commits", n)
	}
	// A third replica sees both fields.
	c, _ := gitStore(t, remote)
	bdC, _ := c.LoadBoard(context.Background(), "acme", 1)
	got := cardByTitle(bdC, "one, renamed")
	if got.ItemID == "" || got.Progress != 65 {
		t.Fatalf("after the re-apply: %+v", got)
	}
	// And b's own cache agrees with the remote.
	bdB, _ = b.LoadBoard(ctxB, "acme", 1)
	if g := cardByTitle(bdB, "one, renamed"); g.ItemID == "" || g.Progress != 65 {
		t.Fatalf("b's cache after the re-apply: %+v", g)
	}
}

// G26 — a push that cannot land is visible: the oldest unpushed commit's age.
func TestGitUnpushedAge(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	be, _ := gitStore(t, remote)
	be.git.domains[0].remote = gitstore.Remote{URL: "gittest://remotes/gone.git"}
	ctx := withAction(board.WithActor(context.Background(), "alice"), "01JB4KA0M2P4R6T8V0X2Z4B6L1", "progress")
	bd, _ := be.LoadBoard(ctx, "acme", 1)
	if age := be.unpushedAge("acme/1"); age != 0 {
		t.Fatalf("nothing written yet, age = %v", age)
	}
	if err := be.SetProgress(ctx, bd, cardByTitle(bd, "one"), 55); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, be)
	if err := be.syncNow(context.Background(), "acme/1"); err == nil {
		t.Fatal("a push to a missing remote must fail")
	}
	if age := be.unpushedAge("acme/1"); age <= 0 {
		t.Fatalf("unpushed age = %v, want > 0", age)
	}
	// The commit is still there for the next attempt.
	if n, _ := be.git.primary().Unpushed(); n != 1 {
		t.Fatalf("unpushed = %d", n)
	}
}
