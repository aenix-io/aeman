package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Every open board is a SCOPED watch — a team grid, somebody's Me view, the
// Triage weeks — and a scoped subscription has to know whether a changed card
// entered or left its view. Asking that question card by card meant building
// the whole view again for every card and every tab: FilterCards over the
// entire board, sorted and projected, to look up one uid. A carry-over moves
// hundreds of cards, so a morning's twenty tabs turned one button into
// thousands of full board views, under the lock every read waits on — three
// and a half minutes on the real board, with the memory churn to match.
//
// Membership is decided ONCE for the burst. What that buys is checked at the
// assertion below, against a budget rather than a ratio, so the machine does
// not cancel out. The reasoning for the numbers lives there, not here.
func TestACarryOverDoesNotCostMoreForEveryOpenBoard(t *testing.T) {
	lone, frames := carryWithTabs(t, "lone", 1)
	if frames < 1 {
		t.Fatalf("the one tab heard nothing (%d frames); the comparison would be empty", frames)
	}
	many, frames := carryWithTabs(t, "many", 12)
	t.Logf("carry-over with 1 tab: %v; with 12 tabs: %v (%.1fx), %d frames",
		lone, many, float64(many)/float64(max(lone, 1)), frames)
	// A tab costs one view per coalescing window, not one per card: twelve
	// of them may cost more than one, but not an order of magnitude more.
	//
	// Ten rather than three, because the budget scales with the quantity that
	// varies and bounds the one that does not. `many` reproduces across
	// machines: around 800ms under -race, around 27ms plain. `lone` does not —
	// measured at 166ms on one machine and 368ms on another, each stable over
	// repeated runs on its own. So `3*lone + 250ms` comes to 748ms on the
	// first and 1354ms on the second while `many` is the same, and a slack of
	// 3 fails on one machine and passes on the other with identical code.
	// That flakiness is a property of the assertion and predates this change.
	// The change is not free, though, and the two should not be confused:
	// cloning the board adds about ten percent to `many` under -race, which
	// two independent measurements agree on. Ten absorbs both the machine
	// spread and that.
	//
	// The regression actually guarded is a view rebuilt per card per board,
	// two orders of magnitude, which lands far outside every number here.
	const carryTabSlack = 10
	if many > carryTabSlack*lone+250*time.Millisecond {
		t.Fatalf("a carry-over took %v with one tab and %v with twelve; the cost follows the tabs", lone, many)
	}
}

// carryWithTabs seeds a board of unfinished work, opens n scoped watches on
// it — the views a team actually keeps open — and returns how long the
// carry-over took and how many frames the tabs were told.
func carryWithTabs(t *testing.T, name string, tabs int) (time.Duration, int64) {
	t.Helper()
	const (
		yesterday = "2026-08-30"
		cards     = 400
	)
	remote := gitRemoteN(t, name)
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	files := []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: " + yesterday + "\n")},
	}
	for i := range cards {
		id := fmt.Sprintf("01CARRY%017d", i)
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, gitstore.FileWrite{Path: p, Data: []byte(fmt.Sprintf(
			"---\ntitle: card %d\nteam: portal\nassignees:\n  - kvaps\nsprint: %s\nstart: %s\nday: %s\nprogress: 30\nrank: a%05d\ncreated: 2026-08-20T09:00:00Z\n---\n",
			i, yesterday, yesterday, yesterday, i))})
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, files); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	srv.store.waitDrained(ctx)

	// The views a team keeps open on a planning morning: the grid, the weeks
	// ahead and people's own boards — every one of them scoped.
	key := storeKey(srv.boardRef())
	sels := []apiserver.Selector{
		{View: "team", Team: "portal"},
		{View: "me", User: "kvaps"},
		{View: "triage", Team: "portal"},
	}
	var frames atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := range tabs {
		sel := sels[i%len(sels)]
		sub, cancelSub := srv.store.subscribe(key, fmt.Sprintf("tab-%d", i), &sel,
			map[string]bool{"cards": true, "sprints": true})
		defer cancelSub()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-sub.ch:
					frames.Add(1)
				case <-stop:
					return
				}
			}
		}()
	}

	started := time.Now()
	rec := do(t, srv, http.MethodPost, "/api/v1/sprints/actions/carry-over", `{"team":"portal"}`)
	took := time.Since(started)
	if rec.Code != http.StatusOK {
		t.Fatalf("carry-over: %d %s", rec.Code, rec.Body.String())
	}
	srv.store.waitDrained(ctx)
	time.Sleep(200 * time.Millisecond) // let the tabs finish reading
	close(stop)
	wg.Wait()
	return took, frames.Load()
}
