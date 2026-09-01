package gitstore

import (
	"testing"
	"time"
)

// A replayed commit is COMMITTED NOW, whatever it was authored at. The board's
// history is read by committer time — "the board as of that evening" is the
// newest commit made at or before it (LoadAsOf) — so a replay that kept the
// old committer time could put a commit stamped 23:58 on top of one from the
// next morning, and the record of that day would then be read from a tree
// already holding the next day's work. Git's own rebase draws the same line:
// the author is who wrote it and when, the committer is who replayed it and
// when.
func TestAReplayedCommitIsCommittedNow(t *testing.T) {
	remote, a, b := twoReplicas(t)
	// Ours is authored late on the 31st; theirs lands first, past midnight.
	if _, err := a.Commit(Action{Name: "progress", Actor: "kvaps", Summary: "mine",
		At: at("2026-08-31T23:58:00Z")}, []FileWrite{
		{Path: "cards/a/2/A2.md", Data: []byte("---\ntitle: b\nprogress: 10\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Commit(Action{Name: "progress", Actor: "lex", Summary: "theirs",
		At: at("2026-09-01T00:03:00Z")}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 70\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	tip := pushRejected(t, remote, a, b)
	before := time.Now()
	if _, err := a.Rebase(tip); err != nil {
		t.Fatal(err)
	}

	head, err := a.CommitObject(a.Head())
	if err != nil {
		t.Fatal(err)
	}
	// Authored when it was written…
	if got := head.Author.When.UTC().Format(time.RFC3339); got != "2026-08-31T23:58:00Z" {
		t.Fatalf("author time = %s; a replay must not rewrite who wrote it and when", got)
	}
	// …and committed at the replay, so the history stays ordered in the time
	// the record of a day is read by.
	if head.Committer.When.Before(before.Add(-time.Minute)) {
		t.Fatalf("committer time = %v, want the replay's own moment", head.Committer.When)
	}
	parent, err := head.Parent(0)
	if err != nil {
		t.Fatal(err)
	}
	if !head.Committer.When.After(parent.Committer.When) {
		t.Fatalf("the replayed commit (%v) is older than the one it sits on (%v): a day's record would be read from a tree holding the next day's work",
			head.Committer.When, parent.Committer.When)
	}
}
