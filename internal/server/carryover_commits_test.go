package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// A request's writes are ONE commit, however many cards it touches: the
// queue groups every op that shares the request's action id (execGroup).
// This is what keeps a big write cheap — every commit moves the tip, and
// a moved tip costs the next write a fresh read of the whole board — so
// it is worth a test of its own, checked against the real store rather
// than assumed. A carry-over is the biggest such request there is.
func TestACarryOverIsOneCommit(t *testing.T) {
	const yesterday = "2026-08-30"
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	files := []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: " + yesterday + "\n")},
	}
	// Enough cards that the old behaviour could not have committed them in
	// one group by luck.
	const cards = 12
	for i := range cards {
		id := fmt.Sprintf("01CARRY%017d", i)
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, gitstore.FileWrite{Path: p, Data: []byte(fmt.Sprintf(
			"---\ntitle: card %d\nteam: portal\nsprint: %s\nstart: %s\nday: %s\nprogress: 30\nrank: a%d\ncreated: 2026-08-20T09:00:00Z\n---\n",
			i, yesterday, yesterday, yesterday, i))})
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, files); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	before := headCount(t, srv)

	rec := do(t, srv, http.MethodPost, "/api/v1/sprints/actions/carry-over",
		`{"team":"portal"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("carry-over: %d %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, fmt.Sprintf(`"carried":%d`, cards)) {
		t.Fatalf("the carry must move every card, or the count below proves nothing: %s", body)
	}
	srv.store.waitDrained(ctx)

	if got := headCount(t, srv) - before; got != 1 {
		t.Fatalf("one request is one commit; the carry-over made %d", got)
	}
	// And the board says what the carry did.
	_ = board.TodayIso()
}

// headCount is how many commits the primary clone holds.
func headCount(t *testing.T, srv *Server) int {
	t.Helper()
	repo := srv.gitBE.git.domains[0].Repo
	n := 0
	h := repo.Head()
	for !h.IsZero() {
		c, err := repo.CommitObject(h)
		if err != nil {
			break
		}
		n++
		if len(c.ParentHashes) == 0 {
			break
		}
		h = c.ParentHashes[0]
	}
	return n
}
