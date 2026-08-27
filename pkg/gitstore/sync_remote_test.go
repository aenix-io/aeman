package gitstore

import (
	"context"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

// A rejected push leaves the remote exactly as it was: its branch still
// points at the other writer's commit and none of ours landed.
func TestRejectedPushLeavesRemoteIntact(t *testing.T) {
	remote, st := newTestRemote(t)
	seedRemote(t, remote)
	a := cloneFull(t, remote)
	b := cloneFull(t, remote)
	aHead, _ := a.Commit(Action{Name: "rename", Actor: "kvaps", Summary: "a"}, []FileWrite{{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a renamed\nprogress: 40\n---\n")}})
	if err := a.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	bHead, _ := b.Commit(Action{Name: "progress", Actor: "timur", Summary: "b"}, []FileWrite{{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\nprogress: 10\n---\n")}})
	if err := b.Push(context.Background(), remote); err == nil {
		t.Fatal("b's push must be rejected")
	}
	ref, err := st.Reference(plumbing.NewBranchReferenceName("main"))
	if err != nil || ref.Hash() != aHead {
		t.Fatalf("remote main = %v, want a's %v", ref, aHead)
	}
	if _, err := st.EncodedObject(plumbing.CommitObject, bHead); err == nil {
		t.Fatal("the rejected commit must not be on the remote's branch history")
	}
}
