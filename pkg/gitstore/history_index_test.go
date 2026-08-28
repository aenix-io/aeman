package gitstore

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

// The path index is what makes a card's log cheap on a long history: one
// walk with a tree diff per commit, then a lookup. Every commit that touched
// the path — by diff or by naming the card in Aeman-Cards — is listed once,
// newest first; a commit that touched only other cards is not.
func TestCommitsTouchingListsEveryCommitOfAPathOnce(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Commit(Action{Name: "import", Summary: "seed", At: at("2026-08-01T09:00:00Z")}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 40\n---\n")},
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	progress, err := r.Commit(Action{Name: "progress", Actor: "kvaps", At: at("2026-08-02T09:00:00Z"), Cards: []string{"A1"}, Summary: "progress"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 80\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A commit that rewrites only B2 but names A1 in its trailers (a review
	// sent from A1's side) belongs to A1's history too.
	named, err := r.Commit(Action{Name: "review-sent", Actor: "kvaps", At: at("2026-08-03T09:00:00Z"), Cards: []string{"A1", "B2"}, Summary: "review",
		Changes: []Change{{Card: "A1", Kind: "review-sent", To: "timur"}}}, []FileWrite{
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nreviewOf: A1\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := r.Commit(Action{Name: "progress", Actor: "timur", At: at("2026-08-04T09:00:00Z"), Cards: []string{"B2"}, Summary: "b"}, []FileWrite{
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nreviewOf: A1\nprogress: 50\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}

	a1, err := r.commitsTouching("cards/a/1/A1.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(a1) != 3 || a1[0] != named || a1[1] != progress {
		t.Fatalf("A1's commits = %v, want [review-sent, progress, import] newest first", a1)
	}
	for _, h := range a1 {
		if h == other {
			t.Fatal("timur's B2-only write is not in A1's history")
		}
	}
	b2, err := r.commitsTouching("cards/b/2/B2.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(b2) != 3 || b2[0] != other || b2[1] != named {
		t.Fatalf("B2's commits = %v, want [b, review-sent, import]", b2)
	}
	if none, _ := r.commitsTouching("cards/c/3/C3.md"); len(none) != 0 {
		t.Fatalf("an unknown card has no history, got %v", none)
	}
}

// New commits extend the index without a rebuild: the second lookup after a
// write walks only what the head gained.
func TestCommitsTouchingExtendsWithoutRebuilding(t *testing.T) {
	r := newRepo(t)
	seed(t, r)
	if _, err := r.commitsTouching("cards/a/1/A1.md"); err != nil {
		t.Fatal(err)
	}
	builds := r.indexBuilds()
	h, err := r.Commit(Action{Name: "progress", Actor: "kvaps", At: at("2026-08-28T08:00:00Z"), Cards: []string{"A1"}, Summary: "p"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 90\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	a1, err := r.commitsTouching("cards/a/1/A1.md")
	if err != nil {
		t.Fatal(err)
	}
	if a1[0] != h {
		t.Fatalf("the new commit leads A1's history, got %v", a1)
	}
	if r.indexBuilds() != builds {
		t.Fatalf("a new head on top of the indexed one must extend, not rebuild (builds %d → %d)", builds, r.indexBuilds())
	}
}

// A head that is not a descendant of the indexed one (a reset, a rebase) and
// a moved shallow boundary (a deepen) both rebuild the index — the old lists
// would lie.
func TestCommitsTouchingRebuildsAfterResetAndShallowChange(t *testing.T) {
	r := newRepo(t)
	hs := chain(t, r, 5)
	if _, err := r.commitsTouching("cards/a/1/A1.md"); err != nil {
		t.Fatal(err)
	}
	builds := r.indexBuilds()
	if err := r.ResetTo(hs[2]); err != nil {
		t.Fatal(err)
	}
	a1, err := r.commitsTouching("cards/a/1/A1.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(a1) != 3 || a1[0] != hs[2] {
		t.Fatalf("after the reset A1's history is the three older commits, got %v", a1)
	}
	if r.indexBuilds() != builds+1 {
		t.Fatalf("a reset rebuilds once (builds %d → %d)", builds, r.indexBuilds())
	}
	if err := r.Storer().SetShallow([]plumbing.Hash{hs[1]}); err != nil {
		t.Fatal(err)
	}
	a1, err = r.commitsTouching("cards/a/1/A1.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(a1) != 2 || a1[1] != hs[1] {
		t.Fatalf("with the boundary at the second commit the history is two commits, got %v", a1)
	}
	if r.indexBuilds() != builds+2 {
		t.Fatalf("a moved boundary rebuilds once more (builds %d → %d)", builds, r.indexBuilds())
	}
}
