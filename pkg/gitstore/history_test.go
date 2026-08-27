package gitstore

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// G8 — the history walker visits the shallow boundary commit and does not
// cross it. go-git's own Log ignores the shallow list and errors at the
// missing parent; this walker is what the log and the day-state replay use.

func chain(t *testing.T, r *Repo, n int) []plumbing.Hash {
	t.Helper()
	var hs []plumbing.Hash
	for i := 0; i < n; i++ {
		h, err := r.Commit(Action{Name: "progress", Actor: "kvaps", At: at("2026-08-0" + string(rune('1'+i)) + "T09:00:00Z"), Cards: []string{"A1"}, Summary: "step"}, []FileWrite{
			{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: " + string(rune('0'+i)) + "0\n---\n")},
		})
		if err != nil {
			t.Fatal(err)
		}
		hs = append(hs, h)
	}
	return hs
}

func TestWalkVisitsWholeHistoryWithoutShallow(t *testing.T) {
	r := newRepo(t)
	hs := chain(t, r, 5)
	var seen []plumbing.Hash
	err := r.Walk(r.Head(), func(c *object.Commit) (bool, error) {
		seen = append(seen, c.Hash)
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 5 || seen[0] != hs[4] || seen[4] != hs[0] {
		t.Fatalf("visited %v", seen)
	}
}

func TestWalkStopsAtShallowBoundary(t *testing.T) {
	r := newRepo(t)
	hs := chain(t, r, 5)
	// Mark the third commit as the boundary, as a depth-3 clone would.
	if err := r.Storer().SetShallow([]plumbing.Hash{hs[2]}); err != nil {
		t.Fatal(err)
	}
	var seen []plumbing.Hash
	err := r.Walk(r.Head(), func(c *object.Commit) (bool, error) {
		seen = append(seen, c.Hash)
		return true, nil
	})
	if err != nil {
		t.Fatalf("the walk must stop cleanly at the boundary, got %v", err)
	}
	if len(seen) != 3 || seen[2] != hs[2] {
		t.Fatalf("visited %v, want the tip, its parent and the boundary itself", seen)
	}
	// A depth-1 clone: the boundary IS the tip — one entry, no error.
	if err := r.Storer().SetShallow([]plumbing.Hash{hs[4]}); err != nil {
		t.Fatal(err)
	}
	seen = nil
	if err := r.Walk(r.Head(), func(c *object.Commit) (bool, error) { seen = append(seen, c.Hash); return true, nil }); err != nil || len(seen) != 1 {
		t.Fatalf("depth-1: %v %v", seen, err)
	}
}

// The callback can stop early; the walker honours it.
func TestWalkStopsWhenAsked(t *testing.T) {
	r := newRepo(t)
	chain(t, r, 5)
	n := 0
	err := r.Walk(r.Head(), func(*object.Commit) (bool, error) { n++; return n < 2, nil })
	if err != nil || n != 2 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
}

// G7 — a card's log is the commits touching it within the horizon; the
// from → to of each line comes from an Aeman-Change trailer when there is
// one for that card and kind, else from the diff of its front-matter.

func TestCardLogReadsTrailersFirstThenDiff(t *testing.T) {
	r := newRepo(t)
	seedTwo := []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nstage: locked\nprogress: 40\n---\n")},
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nprogress: 10\n---\n")},
	}
	if _, err := r.Commit(Action{Name: "import", Summary: "seed", At: at("2026-08-01T09:00:00Z")}, seedTwo); err != nil {
		t.Fatal(err)
	}
	// A plain field write: no trailer, the diff says progress 40 → 80.
	if _, err := r.Commit(Action{Name: "progress", Actor: "kvaps", At: at("2026-08-02T09:00:00Z"), Cards: []string{"A1"}, Summary: "progress"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nstage: locked\nprogress: 80\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	// A review: the reviewer rides a trailer, the stage change is a diff.
	if _, err := r.Commit(Action{Name: "review-sent", Actor: "kvaps", At: at("2026-08-03T09:00:00Z"), Cards: []string{"A1"}, Summary: "review",
		Changes: []Change{{Card: "A1", Kind: "review-sent", To: "timur"}}}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nstage: review\nprogress: 80\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	// A commit that touches only the other card is not in A1's log.
	if _, err := r.Commit(Action{Name: "progress", Actor: "timur", At: at("2026-08-04T09:00:00Z"), Cards: []string{"B2"}, Summary: "b"}, []FileWrite{
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nprogress: 50\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	// One that touches both is in both.
	if _, err := r.Commit(Action{Name: "carry-over", Actor: "kvaps", At: at("2026-08-05T09:00:00Z"), Cards: []string{"A1", "B2"}, Summary: "carry"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nstage: review\nprogress: 80\nsprint: 2026-08-05\n---\n")},
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nprogress: 50\nsprint: 2026-08-05\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}

	log, err := r.CardLog("A1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Entries) != 4 { // import, progress, review, carry — not timur's B2 write
		t.Fatalf("entries = %d: %+v", len(log.Entries), log.Entries)
	}
	if !log.TruncatedBefore.IsZero() {
		t.Fatalf("full history must not be truncated: %v", log.TruncatedBefore)
	}
	// Newest first.
	carry, review, progress, created := log.Entries[0], log.Entries[1], log.Entries[2], log.Entries[3]
	if carry.Action != "carry-over" || len(carry.Changes) != 1 || carry.Changes[0].Kind != "sprint" || carry.Changes[0].To != "2026-08-05" || carry.Changes[0].From != "" {
		t.Fatalf("carry entry = %+v", carry)
	}
	if review.Action != "review-sent" || review.Actor != "kvaps" || len(review.Changes) != 2 {
		t.Fatalf("review entry = %+v", review)
	}
	if review.Changes[0].Kind != "review-sent" || review.Changes[0].To != "timur" || review.Changes[1].Kind != "stage" || review.Changes[1].From != "locked" || review.Changes[1].To != "review" {
		t.Fatalf("review changes = %+v (trailer first, diff second)", review.Changes)
	}
	if progress.Action != "progress" || len(progress.Changes) != 1 || progress.Changes[0].From != "40" || progress.Changes[0].To != "80" {
		t.Fatalf("progress entry = %+v", progress)
	}
	if created.Action != "import" || created.Actor != "" {
		t.Fatalf("import entry = %+v", created)
	}

	blog, err := r.CardLog("B2", 0)
	if err != nil || len(blog.Entries) != 3 { // import, timur's write, carry
		t.Fatalf("B2 log = %d %v", len(blog.Entries), err)
	}
	if blog.Entries[1].Actor != "timur" {
		t.Fatalf("B2's own write missing: %+v", blog.Entries)
	}
}

// Past the shallow boundary the log says where it was cut.
func TestCardLogTruncatedBefore(t *testing.T) {
	r := newRepo(t)
	hs := chain(t, r, 5)
	if err := r.Storer().SetShallow([]plumbing.Hash{hs[2]}); err != nil {
		t.Fatal(err)
	}
	log, err := r.CardLog("A1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Entries) != 3 {
		t.Fatalf("entries = %d", len(log.Entries))
	}
	boundary, _ := r.CommitObject(hs[2])
	if !log.TruncatedBefore.Equal(boundary.Author.When) {
		t.Fatalf("truncatedBefore = %v, want the boundary's time %v", log.TruncatedBefore, boundary.Author.When)
	}
	// A limit cuts the list but does not pretend the history is short.
	short, _ := r.CardLog("A1", 2)
	if len(short.Entries) != 2 || !short.TruncatedBefore.Equal(boundary.Author.When) {
		t.Fatalf("limited log = %+v", short)
	}
}
