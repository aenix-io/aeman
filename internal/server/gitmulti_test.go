package server

import (
	"context"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The store over several domains, and a sync that finds its work on disk:
// a write that re-files a card commits to both repositories under one
// action id and both push; another replica adopts both; what a run left
// unpushed when it stopped is pushed — re-applied if need be — by the next.

var gitTestOpts = gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}}

// gitRemoteN is gitRemote with a distinct name, for tests with more than
// one remote.
func gitRemoteN(t *testing.T, name string) gitstore.Remote {
	t.Helper()
	url := "gittest://remotes/" + strings.ReplaceAll(t.Name(), "/", "_") + "-" + name + ".git"
	gitTestRemotes[url] = memory.NewStorage()
	return gitstore.Remote{URL: url}
}

// seedClosedRemote pushes a closed domain: the project "secret" and its
// column "Risk", no teams.
func seedClosedRemote(t *testing.T, remote gitstore.Remote) {
	t.Helper()
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: closed\n")},
		{Path: gitstore.ProjectPath("01JB4PROJSECRET"), Data: []byte("name: secret\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.EpicPath("01JB4PROJSECRET", "01JB4EPICRISK"), Data: []byte("name: Risk\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: "cards/c/3/01JB4K2E7QZMX3R8V0N5T9WYC3.md", Data: []byte("---\ntitle: three-closed\nteam: portal\nproject: secret\nepic: Risk\nzone: yellow\nprogress: 20\nrank: c\ncreated: 2026-08-26T09:16:03Z\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
}

// gitStoreOver is one replica over several remotes, primary first, named
// shared / closed / third.
func gitStoreOver(t *testing.T, remotes ...gitstore.Remote) *storeBackend {
	t.Helper()
	names := []string{"shared", "closed", "third"}
	domains := make([]gitDomain, 0, len(remotes))
	for i, remote := range remotes {
		repo, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitTestOpts, 0)
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, gitDomain{Domain: gitstore.Domain{Name: names[i], Repo: repo}, remote: remote})
	}
	be := newGitBackend(newBoardStore(), domains, gitOptions{})
	be.git.pushDelay = 0 // tests drive the sync by hand
	return be
}

func actionCtx(id, name string) context.Context {
	return withAction(board.WithActor(context.Background(), "kvaps"), id, name)
}

// G14/G22 through the store — a card filed under the closed project moves:
// both clones commit under the request's action id, the cache knows the new
// domain at once, one sync pushes both, and a replica that was watching
// adopts both and serves the card once, from the closed domain.
func TestGitTwoDomainsMovePushesBothAndIsAdopted(t *testing.T) {
	shared, closed := gitRemoteN(t, "shared"), gitRemoteN(t, "closed")
	seedGitRemote(t, shared)
	seedClosedRemote(t, closed)
	a := gitStoreOver(t, shared, closed)
	watcher := gitStoreOver(t, shared, closed)
	ctx := actionCtx("01JB4KA0M2P4R6T8V0X2Z4B6E1", "update")
	if _, err := watcher.LoadBoard(ctx, "acme", 1); err != nil {
		t.Fatal(err)
	}
	bd, err := a.LoadBoard(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Projects) != 1 || bd.Projects[0] != "secret" {
		t.Fatalf("projects = %v, want the closed domain's project merged in", bd.Projects)
	}
	card := cardByTitle(bd, "one")
	if card.Domain != "shared" {
		t.Fatalf("card domain = %q before the move", card.Domain)
	}
	if err := a.SetProject(ctx, bd, card, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetEpic(ctx, bd, card, "Risk"); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, a)

	sharedRepo, closedRepo := a.git.domains[0].Repo, a.git.domains[1].Repo
	p, _ := gitstore.CardPath(card.ItemID)
	if _, err := closedRepo.ReadFile(p); err != nil {
		t.Fatalf("moved card not in the closed clone: %v", err)
	}
	if _, err := sharedRepo.ReadFile(p); err == nil {
		t.Fatal("moved card still in the shared clone")
	}
	cc, _ := closedRepo.CommitObject(closedRepo.Head())
	sc, _ := sharedRepo.CommitObject(sharedRepo.Head())
	if id := gitstore.ParseTrailers(cc.Message).ActionID; id != "01JB4KA0M2P4R6T8V0X2Z4B6E1" || gitstore.ParseTrailers(sc.Message).ActionID != id {
		t.Fatalf("action ids: closed %q shared %q", id, gitstore.ParseTrailers(sc.Message).ActionID)
	}
	// The cache learnt the new domain from the commit, not from a reload.
	now, _ := a.LoadBoard(ctx, "acme", 1)
	if c := cardByTitle(now, "one"); c.Domain != "closed" || c.Project != "secret" || c.Epic != "Risk" {
		t.Fatalf("cached card after move = domain %q project %q epic %q", c.Domain, c.Project, c.Epic)
	}

	if err := a.syncNow(ctx, "acme/1"); err != nil {
		t.Fatal(err)
	}
	for _, d := range a.git.domains {
		if n, _ := d.Repo.Unpushed(); n != 0 {
			t.Fatalf("%s still has %d unpushed commits", d.Name, n)
		}
	}
	if err := watcher.syncNow(ctx, "acme/1"); err != nil {
		t.Fatal(err)
	}
	seen, _ := watcher.LoadBoard(ctx, "acme", 1)
	count := 0
	for _, c := range seen.Cards {
		if c.Title == "one" {
			count++
			if c.Domain != "closed" || c.Project != "secret" {
				t.Fatalf("adopted card = domain %q project %q", c.Domain, c.Project)
			}
		}
	}
	if count != 1 {
		t.Fatalf("the watcher serves the moved card %d times, want once", count)
	}
}

// A commit the replica made but never pushed — it stopped first — is found
// on the branch by the next run: health counts it, and the sync pushes it,
// re-applied because the remote moved meanwhile.
func TestGitSyncPushesCommitsFromBeforeARestart(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	ctx := actionCtx("01JB4KA0M2P4R6T8V0X2Z4B6F1", "update")

	st := memory.NewStorage()
	repo, err := gitstore.Clone(context.Background(), st, remote, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	first := newGitBackend(newBoardStore(), []gitDomain{{Domain: gitstore.Domain{Name: "board", Repo: repo}, remote: remote}}, gitOptions{})
	first.git.pushDelay = 0
	bd, _ := first.LoadBoard(ctx, "acme", 1)
	if err := first.SetProgress(ctx, bd, cardByTitle(bd, "one"), 90); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, first) // committed, never pushed

	other, _ := gitStore(t, remote)
	bd2, _ := other.LoadBoard(ctx, "acme", 1)
	if err := other.SetProgress(ctx, bd2, cardByTitle(bd2, "two"), 10); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, other)
	if err := other.syncNow(ctx, "acme/1"); err != nil {
		t.Fatal(err)
	}

	// "Restart": a store over the same clone, with no memory of the commit.
	second := newGitBackend(newBoardStore(), []gitDomain{{Domain: gitstore.Domain{Name: "board", Repo: gitstore.Open(st, gitTestOpts)}, remote: remote}}, gitOptions{})
	second.git.pushDelay = 0
	if age := second.unpushedAge("acme/1"); age <= 0 {
		t.Fatal("the pre-restart commit must count as unpushed")
	}
	if err := second.syncNow(ctx, "acme/1"); err != nil {
		t.Fatal(err)
	}
	if n, _ := second.git.primary().Unpushed(); n != 0 {
		t.Fatalf("%d commits still unpushed after the sync", n)
	}
	if age := second.unpushedAge("acme/1"); age != 0 {
		t.Fatalf("unpushed age after push = %v", age)
	}
	check, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := check.ReadFile("cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md")
	two, _ := check.ReadFile("cards/b/2/01JB4K2E7QZMX3R8V0N5T9WYB2.md")
	if !strings.Contains(string(one), "progress: 90") || !strings.Contains(string(two), "progress: 10") {
		t.Fatalf("remote lacks a write:\none: %s\ntwo: %s", one, two)
	}
}
