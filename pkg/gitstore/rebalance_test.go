package gitstore

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// G12 — the rebalance is BOUNDED: an exhausted gap renumbers a window of
// neighbours around the place, grown until the keys fit, and never the
// whole list. On a board of thousands of cards a move into a crowded spot
// rewrites a handful of files, not every one.
func TestARebalanceRenumbersABoundedWindowNotTheList(t *testing.T) {
	tight := "k" + strings.Repeat("0", 30) + "1"
	files := map[string]string{BoardPath: "schema: 3\ntitle: b\n"}
	letters := "bcdefghijlmnopqrstuvw" // spacious single-character keys around k
	for i, ch := range letters {
		id := fmt.Sprintf("01CARD%02d", i)
		p, _ := CardPath(id)
		files[p] = "---\ntitle: c\nrank: " + string(ch) + "\n---\n"
	}
	k0, _ := CardPath("01CARDK0")
	k1, _ := CardPath("01CARDK1")
	files[k0] = "---\ntitle: k\nrank: k\n---\n"
	files[k1] = "---\ntitle: tight\nrank: " + tight + "\n---\n"
	r := repoWith(t, files)
	be := NewBackend(r, BackendOptions{})
	ctx := ctxAs("kvaps")
	bd, _ := be.LoadBoard(ctx, "x")
	mover, _ := findByID(bd, "01CARD00")
	head := r.Head()
	if err := be.MoveCard(ctx, bd, mover, "01CARDK0"); err != nil {
		t.Fatal(err)
	}
	paths := changedPathsSince(t, r, head)
	if len(paths) > 6 {
		t.Fatalf("the rebalance rewrote %d files (%v), want a bounded window", len(paths), paths)
	}
	after, _ := be.LoadBoard(ctx, "x")
	var order []string
	for _, c := range after.Cards {
		order = append(order, c.ItemID)
		if board.RankTooLong(c.Rank) {
			t.Fatalf("rank of %s = %q, still exhausted", c.ItemID, c.Rank)
		}
	}
	if !strings.Contains(strings.Join(order, " "), "01CARDK0 01CARD00 01CARDK1") {
		t.Fatalf("order = %v, want the mover between k and its tight neighbour", order)
	}
}
