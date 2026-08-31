package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// A carry-over moves a whole team's unfinished work, and a good share of it
// is process turns. Every turn announced the board's STRUCTURE to every open
// tab — and a roster frame carries the board: it projected the board per
// watcher, built the board resource and the full process structure, and
// marshalled all of it, once per card, holding the lock that every read
// waits on. On the real board (2435 cards, twenty tabs) a carry-over took a
// minute and a half during which nothing else was served: the board, the
// sprints, the cards and the process list all queued behind it and came back
// after a minute apiece.
//
// The board is announced once for the whole request, not once per card.
func TestACarryOverAnnouncesTheBoardOnceNotPerCard(t *testing.T) {
	const (
		yesterday = "2026-08-30"
		cards     = 400 // the rows a carry-over walks
		turns     = 120 // of which this many are process turns
		tabs      = 12  // open boards, each one an announcement target
	)
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	files := []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: " + yesterday + "\n")},
		{Path: gitstore.ProjectPath("01JB4PROJ"), Data: []byte("name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
	}
	// The processes the turns belong to: a real board keeps dozens, each
	// with its own recurring task, and the roster frame carries all of them.
	for i := range turns {
		pid := fmt.Sprintf("01PROC%018d", i)
		tid := fmt.Sprintf("01TASK%018d", i)
		files = append(files,
			gitstore.FileWrite{Path: gitstore.ProcessPath(pid), Data: []byte(fmt.Sprintf(
				"name: process %d\nproject: portal\nrank: a%d\ncreated: 2026-06-04T08:00:00Z\n", i, i))},
			gitstore.FileWrite{Path: gitstore.TaskPath(pid, tid), Data: []byte(fmt.Sprintf(
				"---\ntitle: turn %d\nteam: portal\nrecurrence: week\nrank: a\ncreated: 2026-06-04T08:00:00Z\n---\n\nDo it.\n", i))},
		)
	}
	for i := range cards {
		id := fmt.Sprintf("01CARRY%017d", i)
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		front := fmt.Sprintf(
			"---\ntitle: card %d\nteam: portal\nsprint: %s\nstart: %s\nday: %s\nprogress: 30\nrank: a%05d\ncreated: 2026-08-20T09:00:00Z\n",
			i, yesterday, yesterday, yesterday, i)
		if i < turns {
			front += fmt.Sprintf("process: %s\ntask: %s\n", fmt.Sprintf("01PROC%018d", i), fmt.Sprintf("01TASK%018d", i))
		}
		files = append(files, gitstore.FileWrite{Path: p, Data: []byte(front + "---\n")})
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

	// Twelve tabs, each draining its stream the way a browser does — a frame
	// nobody reads is dropped, and a dropped frame would hide the cost that
	// building it already paid.
	key := storeKey(srv.boardRef(nil))
	var boards, all atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := range tabs {
		sub, cancelSub := srv.store.subscribe(key, fmt.Sprintf("tab-%d", i), nil,
			map[string]bool{"cards": true, "sprints": true, "ordering": true})
		defer cancelSub()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case data := <-sub.ch:
					all.Add(1)
					var frame struct {
						Kind string `json:"kind"`
					}
					if json.Unmarshal(data, &frame) == nil && frame.Kind == "Board" {
						boards.Add(1)
					}
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

	t.Logf("carry-over of %d cards (%d turns) to %d tabs: %v, %d frames, %d of them the whole board",
		cards, turns, tabs, took, all.Load(), boards.Load())
	// A tab hears the board once per coalescing window the request spans —
	// a handful — and never once per card it moved.
	windows := int64(took/rosterCoalesce) + 2
	if got := boards.Load(); got > int64(tabs)*windows {
		t.Fatalf("the board was announced %d times to %d tabs in %v; that is %d windows' worth at most",
			got, tabs, took, windows)
	}
	// And the request itself stays interactive: it is a person waiting on a
	// button, with the whole board's reads queued behind it.
	if took > 5*time.Second {
		t.Fatalf("the carry-over took %v with %d tabs open", took, tabs)
	}
}
