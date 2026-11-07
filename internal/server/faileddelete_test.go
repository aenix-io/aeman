package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// refusingDeletes is the store's backend with every delete refused, as a
// write the repository will not take.
type refusingDeletes struct{ boardservice.Backend }

func (refusingDeletes) DeleteCard(context.Context, board.Board, board.Card) error {
	return errors.New("the repository refused the delete")
}

// A delete made inside a request rides the request's queue and answers at
// once; when its write then FAILS, the card comes back for everybody at
// once — in the listing and on the watch — rather than staying dropped by
// the recent-delete guard for its whole grace window. The guard is there to
// outweigh a lagging read of a delete that landed; one that did not land
// has nothing to outweigh.
func TestAFailedQueuedDeleteBringsTheCardBackAtOnce(t *testing.T) {
	const today = "2026-08-31"
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	const id = "01FAILDEL0000000000000001"
	p, err := gitstore.CardPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 3\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: " + today + "\n")},
		{Path: p, Data: []byte(fmt.Sprintf("---\ntitle: keep me\nteam: portal\nparked: true\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n"))},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	if rec := do(t, srv, http.MethodGet, "/api/v1/views/all/cards", ""); !strings.Contains(rec.Body.String(), id) {
		t.Fatalf("the card is not on the board to begin with: %s", rec.Body.String())
	}
	srv.gitBE.inner = refusingDeletes{srv.gitBE.inner}
	sub, unsubscribe := srv.store.subscribe(storeKey(srv.gitBoard()), "", nil, map[string]bool{"cards": true})
	defer unsubscribe()

	if rec := do(t, srv, http.MethodDelete, "/api/v1/cards/"+id, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	srv.store.waitDrained(ctx)

	if rec := do(t, srv, http.MethodGet, "/api/v1/views/all/cards", ""); !strings.Contains(rec.Body.String(), id) {
		t.Fatalf("the delete failed and the card is still gone from the listing: %s", rec.Body.String())
	}
	deleted, back := false, false
	for !back {
		select {
		case frame := <-sub.ch:
			s := string(frame)
			if !strings.Contains(s, id) {
				continue
			}
			if strings.Contains(s, `"DELETED"`) {
				deleted = true
				continue
			}
			if deleted && (strings.Contains(s, `"ADDED"`) || strings.Contains(s, `"MODIFIED"`)) {
				back = true
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("the watch never got the card back (deleted frame seen: %v)", deleted)
		}
	}
}
