package gitstore

import (
	"context"
	"testing"
)

// A forced push writes the local branch over a remote that has moved on —
// what a migration re-run with --force needs: the earlier import and
// whatever was written since are replaced by the new snapshot's history. The
// ordinary push keeps refusing such a remote, so nothing else acquires the
// power by accident.
func TestForcedPushWritesOverADivergedRemote(t *testing.T) {
	remote, a, b := twoReplicas(t)
	ctx := context.Background()
	commitFile(t, b, "theirs", "cards/a/1/A1.md", "---\ntitle: theirs\n---\n")
	if err := b.Push(ctx, remote); err != nil {
		t.Fatal(err)
	}
	commitFile(t, a, "ours", "cards/a/1/A1.md", "---\ntitle: ours\n---\n")
	if err := a.Push(ctx, remote); err == nil {
		t.Fatal("an ordinary push over a moved remote must be refused")
	}
	if err := a.PushForce(ctx, remote); err != nil {
		t.Fatalf("forced push: %v", err)
	}
	fresh := cloneFull(t, remote)
	if got := mustRead(t, fresh, "cards/a/1/A1.md"); got != "---\ntitle: ours\n---\n" {
		t.Fatalf("the remote holds %q after the forced push, want ours", got)
	}
	if fresh.Head() != a.Head() {
		t.Fatalf("remote head = %s, want a's %s", fresh.Head(), a.Head())
	}
	// The tracking ref moved with it: a later ordinary push has nothing to say.
	if err := a.Push(ctx, remote); err != nil {
		t.Fatalf("push after the forced push: %v", err)
	}
}
