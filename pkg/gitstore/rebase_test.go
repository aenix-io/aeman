package gitstore

import (
	"context"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

// copyObjects brings every object of one repository into another, so a walk
// there can see commits it never fetched.
func copyObjects(from, to *Repo) error {
	iter, err := from.s.IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		return err
	}
	return iter.ForEach(func(o plumbing.EncodedObject) error {
		_, err := to.s.SetEncodedObject(o)
		return err
	})
}

// G10/G11 — a rejected push is re-applied on the remote's new tip. The
// re-application replays the commits the remote has not seen — found on the
// branch, not in memory, so what an earlier run left unpushed is replayed
// too — oldest first, each field by field onto the file as it now is: two
// writers on different fields of one card both land; the same field
// resolves to the replayed (later) write; a create whose path already exists
// is a no-op; a commit with nothing left to say is dropped. Messages,
// trailers, authors and dates survive the replay.

// twoReplicas clones the seeded remote twice.
func twoReplicas(t *testing.T) (Remote, *Repo, *Repo) {
	t.Helper()
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	return remote, cloneFull(t, remote), cloneFull(t, remote)
}

func commitFile(t *testing.T, r *Repo, name, path, data string) plumbing.Hash {
	t.Helper()
	h, err := r.Commit(Action{Name: name, Actor: "kvaps", Cards: []string{"A1"}, Summary: name, At: at("2026-08-02T09:00:00Z")}, []FileWrite{{Path: path, Data: []byte(data)}})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustRead(t *testing.T, r *Repo, p string) string {
	t.Helper()
	data, err := r.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(data)
}

// pushRejected pushes a's commits after b's landed: a's push is rejected,
// the fetch brings b's tip, and the test rebases onto it.
func pushRejected(t *testing.T, remote Remote, a, b *Repo) plumbing.Hash {
	t.Helper()
	ctx := context.Background()
	if err := b.Push(ctx, remote); err != nil {
		t.Fatal(err)
	}
	if err := a.Push(ctx, remote); err == nil {
		t.Fatal("a's push must be rejected")
	}
	tip, moved, err := a.Fetch(ctx, remote)
	if err != nil || !moved {
		t.Fatalf("fetch: moved=%v err=%v", moved, err)
	}
	return tip
}

func TestRebaseReplaysDisjointFieldsOfOneCard(t *testing.T) {
	remote, a, b := twoReplicas(t)
	commitFile(t, a, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 70\n---\n")
	commitFile(t, b, "update", "cards/a/1/A1.md", "---\ntitle: a\nzone: yellow\nprogress: 40\n---\n")
	tip := pushRejected(t, remote, a, b)
	res, err := a.Rebase(tip)
	if err != nil {
		t.Fatal(err)
	}
	if res.Replayed != 1 || res.Dropped != 0 {
		t.Fatalf("result = %+v, want 1 replayed", res)
	}
	got := mustRead(t, a, "cards/a/1/A1.md")
	if !strings.Contains(got, "progress: 70") || !strings.Contains(got, "zone: yellow") {
		t.Fatalf("both fields must land:\n%s", got)
	}
	// The replayed commit sits on b's tip and keeps its own message.
	head, _ := a.CommitObject(a.Head())
	if head.ParentHashes[0] != tip {
		t.Fatal("replayed commit is not on the new tip")
	}
	tr := ParseTrailers(head.Message)
	if tr.Action != "update" || tr.Actor != "kvaps" || len(tr.Cards) != 1 || tr.Cards[0] != "A1" {
		t.Fatalf("trailers lost in replay: %+v", tr)
	}
	if head.Author.Name != "kvaps" || !head.Author.When.Equal(at("2026-08-02T09:00:00Z")) {
		t.Fatalf("author/date lost in replay: %s %v", head.Author.Name, head.Author.When)
	}
	if err := a.Push(context.Background(), remote); err != nil {
		t.Fatalf("push after rebase: %v", err)
	}
}

func TestRebaseSameFieldReplayedWriteWins(t *testing.T) {
	remote, a, b := twoReplicas(t)
	commitFile(t, a, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 70\n---\n")
	commitFile(t, b, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 55\n---\n")
	tip := pushRejected(t, remote, a, b)
	if _, err := a.Rebase(tip); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, a, "cards/a/1/A1.md"); !strings.Contains(got, "progress: 70") {
		t.Fatalf("the replayed write must win the field:\n%s", got)
	}
	// History has both: b's commit, then a's replay.
	n := 0
	_ = a.Walk(a.Head(), func(c *object.Commit) (bool, error) { n++; return true, nil })
	if n != 3 {
		t.Fatalf("history has %d commits, want seed + b + a", n)
	}
}

func TestRebaseUntouchedFieldsOfTheOtherSideSurvive(t *testing.T) {
	// a re-zones; b renames AND adds a note. The replay keeps b's title and
	// note, adds a's zone.
	remote, a, b := twoReplicas(t)
	commitFile(t, a, "update", "cards/a/1/A1.md", "---\ntitle: a\nzone: green\nprogress: 40\n---\n")
	commitFile(t, b, "note", "cards/a/1/A1.md", "---\ntitle: renamed\nprogress: 40\n---\n\n## Notes\n\n- 01NOTEB [2026-08-02T09:00:00Z] timur — from b\n")
	tip := pushRejected(t, remote, a, b)
	if _, err := a.Rebase(tip); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, a, "cards/a/1/A1.md")
	for _, want := range []string{"title: renamed", "zone: green", "from b"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q after replay:\n%s", want, got)
		}
	}
}

func TestRebaseMergesNotesByID(t *testing.T) {
	remote, a, b := twoReplicas(t)
	commitFile(t, a, "note", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 40\n---\n\n## Notes\n\n- 01NOTEA [2026-08-02T09:00:00Z] kvaps — from a\n")
	commitFile(t, b, "note", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 40\n---\n\n## Notes\n\n- 01NOTEB [2026-08-02T09:01:00Z] timur — from b\n")
	tip := pushRejected(t, remote, a, b)
	if _, err := a.Rebase(tip); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, a, "cards/a/1/A1.md")
	if !strings.Contains(got, "from a") || !strings.Contains(got, "from b") {
		t.Fatalf("both notes must survive:\n%s", got)
	}
	if strings.Count(got, "01NOTE") != 2 {
		t.Fatalf("notes duplicated or lost:\n%s", got)
	}
}

func TestRebaseCreateOnExistingPathIsANoOp(t *testing.T) {
	// Two replicas spawn the same iteration: same path, different created.
	remote, a, b := twoReplicas(t)
	commitFile(t, a, "spawn", "cards/x/1/X1.md", "---\ntitle: weekly\ncreated: 2026-08-02T09:00:01Z\n---\n")
	commitFile(t, b, "spawn", "cards/x/1/X1.md", "---\ntitle: weekly\ncreated: 2026-08-02T09:00:00Z\n---\n")
	tip := pushRejected(t, remote, a, b)
	res, err := a.Rebase(tip)
	if err != nil {
		t.Fatal(err)
	}
	if res.Dropped != 1 || res.Replayed != 0 {
		t.Fatalf("result = %+v, want the create dropped", res)
	}
	if a.Head() != tip {
		t.Fatal("a dropped commit must leave no commit behind")
	}
	if got := mustRead(t, a, "cards/x/1/X1.md"); !strings.Contains(got, "09:00:00Z") {
		t.Fatalf("the winner's file must stay:\n%s", got)
	}
}

func TestRebaseReplaysDeleteAndCreate(t *testing.T) {
	remote, a, b := twoReplicas(t)
	// a deletes B2 and creates C3; b edits A1.
	if _, err := a.Commit(Action{Name: "delete", Summary: "delete", Cards: []string{"B2"}}, []FileWrite{{Path: "cards/b/2/B2.md"}}); err != nil {
		t.Fatal(err)
	}
	commitFile(t, a, "create", "cards/c/3/C3.md", "---\ntitle: c\n---\n")
	commitFile(t, b, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 41\n---\n")
	tip := pushRejected(t, remote, a, b)
	res, err := a.Rebase(tip)
	if err != nil {
		t.Fatal(err)
	}
	if res.Replayed != 2 {
		t.Fatalf("result = %+v, want both replayed", res)
	}
	if _, err := a.ReadFile("cards/b/2/B2.md"); err == nil {
		t.Fatal("the delete was not replayed")
	}
	mustRead(t, a, "cards/c/3/C3.md")
	if got := mustRead(t, a, "cards/a/1/A1.md"); !strings.Contains(got, "progress: 41") {
		t.Fatalf("b's edit lost:\n%s", got)
	}
}

func TestRebaseTheirDeleteWinsOverOurEdit(t *testing.T) {
	remote, a, b := twoReplicas(t)
	commitFile(t, a, "update", "cards/b/2/B2.md", "---\ntitle: b\nprogress: 10\n---\n")
	if _, err := b.Commit(Action{Name: "delete", Summary: "delete", Cards: []string{"B2"}}, []FileWrite{{Path: "cards/b/2/B2.md"}}); err != nil {
		t.Fatal(err)
	}
	tip := pushRejected(t, remote, a, b)
	res, err := a.Rebase(tip)
	if err != nil {
		t.Fatal(err)
	}
	if res.Dropped != 1 {
		t.Fatalf("result = %+v, want the edit of a deleted card dropped", res)
	}
	if _, err := a.ReadFile("cards/b/2/B2.md"); err == nil {
		t.Fatal("a deleted card must not come back through a replay")
	}
}

func TestRebaseRosterFileFieldLevel(t *testing.T) {
	// a advances portal's sprint pointer; b re-ranks the team.
	remote, a, b := twoReplicas(t)
	team := TeamPath("01T_PORTAL")
	seed := "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n"
	if _, err := a.Commit(Action{Name: "add-team", Summary: "add"}, []FileWrite{{Path: team, Data: []byte(seed)}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Fetch(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	if err := b.ResetTo(a.Head()); err != nil {
		t.Fatal(err)
	}
	commitFile(t, a, "carry-over", team, "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-31\n  previous: 2026-08-24\n")
	commitFile(t, b, "move-team", team, "name: portal\nrank: bz\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")
	tip := pushRejected(t, remote, a, b)
	if _, err := a.Rebase(tip); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, a, team)
	if !strings.Contains(got, "rank: bz") || !strings.Contains(got, "current: 2026-08-31") || !strings.Contains(got, "previous: 2026-08-24") {
		t.Fatalf("team file after replay:\n%s", got)
	}
}

// What an earlier run left unpushed is on the branch, and the replay finds
// it there — a restart between commit and push loses nothing.
func TestRebaseReplaysCommitsFromBeforeARestart(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	st := memory.NewStorage()
	first, err := Clone(context.Background(), st, remote, Options{Committer: serverID, Branch: plumbing.NewBranchReferenceName("main")}, 0)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, first, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 80\n---\n")
	// "Restart": a fresh Repo over the same storage, no memory of the commit.
	a := Open(st, Options{Committer: serverID, Branch: plumbing.NewBranchReferenceName("main")})
	b := cloneFull(t, remote)
	commitFile(t, b, "update", "cards/b/2/B2.md", "---\ntitle: b\nzone: red\n---\n")
	tip := pushRejected(t, remote, a, b)
	res, err := a.Rebase(tip)
	if err != nil {
		t.Fatal(err)
	}
	if res.Replayed != 1 {
		t.Fatalf("result = %+v, want the pre-restart commit replayed", res)
	}
	if got := mustRead(t, a, "cards/a/1/A1.md"); !strings.Contains(got, "progress: 80") {
		t.Fatalf("pre-restart write lost:\n%s", got)
	}
}

// A tip that shares no history with the branch (a rewritten remote) is
// refused: nothing is reset, nothing is lost.
func TestRebaseRefusesUnrelatedTip(t *testing.T) {
	remote, a, _ := twoReplicas(t)
	head := commitFile(t, a, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 70\n---\n")
	other := newRepo(t)
	unrelated, err := other.Commit(Action{Name: "import", Summary: "elsewhere"}, []FileWrite{{Path: BoardPath, Data: []byte("schema: 1\n")}})
	if err != nil {
		t.Fatal(err)
	}
	// Bring the unrelated commit's objects over so the walk can see them.
	if err := copyObjects(other, a); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Rebase(unrelated); err == nil {
		t.Fatal("an unrelated tip must be refused")
	}
	if a.Head() != head {
		t.Fatal("a refused rebase must not move the branch")
	}
	_ = remote
}

// Nothing to replay: the tip already contains our head (we are behind, not
// diverged) — the branch simply moves there.
func TestRebaseFastForwards(t *testing.T) {
	remote, a, b := twoReplicas(t)
	commitFile(t, b, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 41\n---\n")
	if err := b.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	tip, _, err := a.Fetch(context.Background(), remote)
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Rebase(tip)
	if err != nil || res.Replayed != 0 || res.Dropped != 0 || a.Head() != tip {
		t.Fatalf("fast-forward: res=%+v err=%v head==tip %v", res, err, a.Head() == tip)
	}
}

// UnpushedCommits lists what the remote has not seen, oldest first — the
// health age reads the oldest one's date.
func TestUnpushedCommitsOldestFirst(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	a := cloneFull(t, remote)
	if cs, err := a.UnpushedCommits(); err != nil || len(cs) != 0 {
		t.Fatalf("fresh clone unpushed = %d (%v)", len(cs), err)
	}
	h1 := commitFile(t, a, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 50\n---\n")
	h2 := commitFile(t, a, "update", "cards/a/1/A1.md", "---\ntitle: a\nprogress: 60\n---\n")
	cs, err := a.UnpushedCommits()
	if err != nil || len(cs) != 2 || cs[0].Hash != h1 || cs[1].Hash != h2 {
		t.Fatalf("unpushed = %v (%v), want [h1 h2]", cs, err)
	}
}
