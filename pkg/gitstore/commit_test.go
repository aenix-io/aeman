package gitstore

import (
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

// G4 — one action is one commit touching every file it changed, with the
// machine-readable trailers; an action that changes nothing makes no commit.
// G6 — author is the actor, committer is the server, date is the action time.

var server = Identity{Name: "aeman", Email: "aeman@aenix.io"}

func newRepo(t *testing.T) *Repo {
	t.Helper()
	r, err := Init(memory.NewStorage(), Options{
		Committer:   server,
		AuthorEmail: func(login string) string { return login + "@users.noreply.github.com" },
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func at(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func seed(t *testing.T, r *Repo) plumbing.Hash {
	t.Helper()
	h, err := r.Commit(Action{Name: "import", Summary: "import board", At: at("2026-08-01T09:00:00Z")}, []FileWrite{
		{Path: "cards/c/1/A1", Data: []byte("a1 v1\n")},
		{Path: "cards/c/2/B2", Data: []byte("b2 v1\n")},
		{Path: "teams/_.yaml", Data: []byte("name: \"\"\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.IsZero() {
		t.Fatal("seed made no commit")
	}
	return h
}

func TestCommitTouchesEveryFileOnce(t *testing.T) {
	r := newRepo(t)
	first := seed(t, r)
	h, err := r.Commit(Action{
		Name: "carry-over", ID: "01JB4KA0M2P4R6T8V0X2Z4B6D8", Actor: "kvaps",
		At: at("2026-08-28T07:00:00Z"), Cards: []string{"A1", "B2"},
		Summary: "carry over 2 cards to 2026-08-28",
	}, []FileWrite{
		{Path: "cards/c/1/A1", Data: []byte("a1 v2\n")},
		{Path: "cards/c/2/B2", Data: []byte("b2 v2\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.CommitObject(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ParentHashes) != 1 || c.ParentHashes[0] != first {
		t.Fatalf("parent = %v, want %v", c.ParentHashes, first)
	}
	if got := firstLine(c.Message); got != "carry over 2 cards to 2026-08-28" {
		t.Fatalf("summary line = %q", got)
	}
	tr := ParseTrailers(c.Message)
	if tr.Action != "carry-over" || tr.ActionID != "01JB4KA0M2P4R6T8V0X2Z4B6D8" || tr.Actor != "kvaps" {
		t.Fatalf("trailers = %+v", tr)
	}
	if strings.Join(tr.Cards, " ") != "A1 B2" {
		t.Fatalf("Aeman-Cards = %v", tr.Cards)
	}
	// Both files carry the new content; the untouched one is still there.
	for path, want := range map[string]string{"cards/c/1/A1": "a1 v2\n", "cards/c/2/B2": "b2 v2\n", "teams/_.yaml": "name: \"\"\n"} {
		if got := readFile(t, r, h, path); got != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
	// Exactly the two changed blobs differ between the trees.
	if n := changedPaths(t, r, first, h); len(n) != 2 {
		t.Fatalf("changed paths = %v, want 2", n)
	}
}

// Writing what is already there is not a change: no commit, HEAD stays.
func TestCommitNoopMakesNoCommit(t *testing.T) {
	r := newRepo(t)
	first := seed(t, r)
	h, err := r.Commit(Action{Name: "progress", Actor: "kvaps", At: at("2026-08-28T07:00:00Z"), Summary: "same"}, []FileWrite{
		{Path: "cards/c/1/A1", Data: []byte("a1 v1\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !h.IsZero() {
		t.Fatalf("a no-op produced commit %v", h)
	}
	if head := r.Head(); head != first {
		t.Fatalf("HEAD moved to %v", head)
	}
	// And an empty write list is the same no-op.
	h, err = r.Commit(Action{Name: "carry-over", Summary: "nothing"}, nil)
	if err != nil || !h.IsZero() {
		t.Fatalf("empty action: %v %v", h, err)
	}
}

func TestCommitIdentity(t *testing.T) {
	r := newRepo(t)
	seed(t, r)
	when := at("2026-08-28T07:00:00Z")
	h, err := r.Commit(Action{Name: "progress", Actor: "kitsunoff", At: when, Cards: []string{"A1"}, Summary: "progress"}, []FileWrite{
		{Path: "cards/c/1/A1", Data: []byte("a1 v3\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r.CommitObject(h)
	if c.Author.Name != "kitsunoff" || c.Author.Email != "kitsunoff@users.noreply.github.com" {
		t.Fatalf("author = %s <%s>", c.Author.Name, c.Author.Email)
	}
	if !c.Author.When.Equal(when) || !c.Committer.When.Equal(when) {
		t.Fatalf("dates = %v / %v, want %v", c.Author.When, c.Committer.When, when)
	}
	if c.Committer.Name != server.Name || c.Committer.Email != server.Email {
		t.Fatalf("committer = %s <%s>", c.Committer.Name, c.Committer.Email)
	}
}

// Writers that are nobody in particular — the sweep, an import — are the
// server itself, and carry no actor trailer to be mistaken for a person.
func TestCommitUnattributedIsTheServer(t *testing.T) {
	r := newRepo(t)
	seed(t, r)
	h, err := r.Commit(Action{Name: "sweep", At: at("2026-08-28T07:00:00Z"), Summary: "file this week's iterations"}, []FileWrite{
		{Path: "cards/c/3/C3", Data: []byte("c3\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r.CommitObject(h)
	if c.Author.Name != server.Name || c.Author.Email != server.Email {
		t.Fatalf("author = %s <%s>, want the server", c.Author.Name, c.Author.Email)
	}
	if strings.Contains(c.Message, "Aeman-Actor:") {
		t.Fatalf("an unattributed commit carries an actor:\n%s", c.Message)
	}
	if tr := ParseTrailers(c.Message); tr.Action != "sweep" || tr.Actor != "" {
		t.Fatalf("trailers = %+v", tr)
	}
}

// A change whose payload is not a field diff rides an Aeman-Change trailer;
// an empty side is written as "-" so the four tokens always split.
func TestCommitChangeTrailers(t *testing.T) {
	r := newRepo(t)
	seed(t, r)
	h, err := r.Commit(Action{
		Name: "review-sent", Actor: "kvaps", At: at("2026-08-28T07:00:00Z"), Cards: []string{"A1"}, Summary: "send to review",
		Changes: []Change{{Card: "A1", Kind: "review-sent", To: "timur"}, {Card: "A1", Kind: "stage", From: "locked", To: "review"}},
	}, []FileWrite{{Path: "cards/c/1/A1", Data: []byte("a1 review\n")}})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r.CommitObject(h)
	if !strings.Contains(c.Message, "Aeman-Change: A1 review-sent - timur\n") {
		t.Fatalf("empty side not written as -:\n%s", c.Message)
	}
	tr := ParseTrailers(c.Message)
	if len(tr.Changes) != 2 || tr.Changes[0].To != "timur" || tr.Changes[0].From != "" || tr.Changes[1].From != "locked" {
		t.Fatalf("changes = %+v", tr.Changes)
	}
}

// A nil write deletes; an emptied leaf directory disappears from the tree
// rather than lingering as an empty tree.
func TestCommitDeleteRemovesFileAndEmptyDirs(t *testing.T) {
	r := newRepo(t)
	seed(t, r)
	h, err := r.Commit(Action{Name: "delete", Actor: "kvaps", At: at("2026-08-28T07:00:00Z"), Cards: []string{"B2"}, Summary: "delete B2"}, []FileWrite{
		{Path: "cards/c/2/B2", Data: nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r.CommitObject(h)
	tree, _ := c.Tree()
	if _, err := tree.File("cards/c/2/B2"); err == nil {
		t.Fatal("deleted file still in the tree")
	}
	if _, err := tree.Tree("cards/c/2"); err == nil {
		t.Fatal("an emptied leaf directory lingers")
	}
	if _, err := tree.File("cards/c/1/A1"); err != nil {
		t.Fatal("a sibling under the same parent was lost")
	}
	// Deleting what is not there is a no-op, not an error.
	h2, err := r.Commit(Action{Name: "delete", Summary: "again"}, []FileWrite{{Path: "cards/c/2/B2"}})
	if err != nil || !h2.IsZero() {
		t.Fatalf("double delete: %v %v", h2, err)
	}
}

// Trailers survive a body with blank lines and a summary that contains a
// colon; a message with no trailers parses to nothing, not to garbage.
func TestParseTrailersRobust(t *testing.T) {
	msg := "rename: Docs → Documentation\n\nSome body text: with a colon.\n\nAeman-Action: rename-epic\nAeman-Cards: A1 B2 C3\n"
	tr := ParseTrailers(msg)
	if tr.Action != "rename-epic" || len(tr.Cards) != 3 || tr.Actor != "" {
		t.Fatalf("trailers = %+v", tr)
	}
	if tr := ParseTrailers("just a line\n"); tr.Action != "" || len(tr.Cards) != 0 {
		t.Fatalf("no-trailer message parsed to %+v", tr)
	}
}

// ---- helpers ----------------------------------------------------------------

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func readFile(t *testing.T, r *Repo, h plumbing.Hash, path string) string {
	t.Helper()
	c, err := r.CommitObject(h)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := c.Tree()
	if err != nil {
		t.Fatal(err)
	}
	f, err := tree.File(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	s, err := f.Contents()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func changedPaths(t *testing.T, r *Repo, a, b plumbing.Hash) []string {
	t.Helper()
	ca, _ := r.CommitObject(a)
	cb, _ := r.CommitObject(b)
	ta, _ := ca.Tree()
	tb, _ := cb.Tree()
	changes, err := object.DiffTree(ta, tb)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, ch := range changes {
		out = append(out, ch.To.Name+ch.From.Name)
	}
	return out
}
