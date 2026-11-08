package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// orderingBoard seeds a git remote with epics A < B and team portal's queue
// A1 B2 A2 B1 (the brief's §4), and starts a git-mode server over it.
func orderingBoard(t *testing.T) (*Server, map[string]string) {
	t.Helper()
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	files := []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte(fmt.Sprintf("schema: %d\ntitle: t\n", gitstore.SchemaVersion))},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n")},
	}
	add := func(name, body string) {
		id := fmt.Sprintf("01ORD%s%024d", strings.ToUpper(name), len(ids))
		id = id[:26]
		ids[name] = id
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, gitstore.FileWrite{Path: p, Data: []byte(body)})
	}
	add("a", "---\ntitle: A\ntype: epic\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")
	add("b", "---\ntitle: B\ntype: epic\nrank: b\ncreated: 2026-08-20T09:00:00Z\n---\n")
	for i, task := range []struct{ name, epic, rank string }{{"a1", "a", "c"}, {"b2", "b", "d"}, {"a2", "a", "e"}, {"b1", "b", "f"}} {
		add(task.name, fmt.Sprintf("---\ntitle: %s\nteam: portal\nparked: true\nparent: %s\nrank: %s\ncreated: 2026-08-2%dT09:00:00Z\n---\n",
			strings.ToUpper(task.name), ids[task.epic], task.rank, i))
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, files); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	return srv, ids
}

// backlogOf is team portal's backlog as the listing reads it, by name.
func backlogOf(t *testing.T, srv *Server, ids map[string]string) string {
	t.Helper()
	rec := do(t, srv, http.MethodGet, "/api/v1/views/backlog/cards?team=portal", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("backlog: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			Metadata struct {
				UID string `json:"uid"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	name := map[string]string{}
	for n, id := range ids {
		name[id] = n
	}
	var got []string
	for _, it := range out.Items {
		got = append(got, strings.ToUpper(name[it.Metadata.UID]))
	}
	return strings.Join(got, " ")
}

// Q5, Q12 — AN EPIC MOVE IS ONE ACTION. Moving B above A over the move
// route gathers B's scattered tasks into one block ahead of A's
// (`A1 B2 A2 B1` → `B2 B1 A1 A2`), as ONE commit; the answer names the
// action and how many cards took a key.
func TestAnEpicMoveIsOneAction(t *testing.T) {
	srv, ids := orderingBoard(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if got := backlogOf(t, srv, ids); got != "A1 B2 A2 B1" {
		t.Fatalf("seeded backlog = %s", got)
	}
	before := headCount(t, srv)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/"+ids["b"]+"/actions/move", `{"before":"`+ids["a"]+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	var res struct {
		ActionID string `json:"actionId"`
		Moved    int    `json:"moved"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.ActionID == "" || res.Moved < 2 {
		t.Fatalf("answer = %+v, want the action and the cards it moved", res)
	}
	// The cache answers at once — no wait for the commit.
	if got := backlogOf(t, srv, ids); got != "B2 B1 A1 A2" {
		t.Fatalf("backlog after B moved above A = %s", got)
	}
	srv.store.waitDrained(ctx)
	if got := headCount(t, srv) - before; got != 1 {
		t.Fatalf("one epic move is one commit; it made %d", got)
	}
}

// failingReorder writes the first key of a plan and then fails — a fault in
// the middle of a mass move's write.
type failingReorder struct{ boardservice.Backend }

var errInjected = errors.New("injected write failure")

func (f failingReorder) Reorder(ctx context.Context, b board.Board, plan board.Planner) ([]board.RankChange, error) {
	changes, err := plan(b)
	if err != nil || len(changes) < 2 {
		return f.Backend.Reorder(ctx, b, plan)
	}
	if _, err := f.Backend.Reorder(ctx, b, board.Fixed(changes[:1])); err != nil {
		return nil, err
	}
	return nil, errInjected
}

// Q12 — A FAILED WRITE LEAVES NO PARTIAL MOVE, IN THE CACHE OR IN GIT. The
// epic move's keys are staged under one scope; a write that fails half-way
// commits nothing, and the queue's failure path reloads the cache from the
// tip — so every reader sees the order as it was, never half a block.
func TestAFailedGatherLeavesNoPartialMove(t *testing.T) {
	srv, ids := orderingBoard(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.gitBE.inner = failingReorder{srv.gitBE.inner}
	before := headCount(t, srv)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/"+ids["b"]+"/actions/move", `{"before":"`+ids["a"]+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	srv.store.waitDrained(ctx)
	if got := headCount(t, srv) - before; got != 0 {
		t.Fatalf("a failed write committed %d commits", got)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := backlogOf(t, srv, ids)
		if got == "A1 B2 A2 B1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the cache after the failure = %s, want the order as it was", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, err := gitstore.Load(srv.gitBE.git.domains[0].Repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range s.Cards {
		if c.ItemID == ids["b"] && c.Rank != "b" {
			t.Fatalf("the epic's key in git = %q, want it untouched", c.Rank)
		}
	}
}

// Q3 — THE REST DOOR: a task created under an epic over HTTP lands right
// after that epic's last parked task (the brief's §6), in the same request.
func TestARestCreateUnderAnEpicLandsInItsArea(t *testing.T) {
	srv, ids := orderingBoard(t)
	rec := do(t, srv, http.MethodPost, "/api/v1/views/backlog/cards",
		`{"title":"A3","team":"portal","parent":"`+ids["a"]+`"}`)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	ids["a3"] = created.Metadata.UID
	if got := backlogOf(t, srv, ids); got != "A1 B2 A2 A3 B1" {
		t.Fatalf("backlog = %s", got)
	}
}

// Q12 — CONCURRENT ORDERING ACTIONS REACH GIT IN THE ORDER THE CACHE TOOK
// THEM. Keys are absolute: if two moves of one card were queued in the
// other order than they were planned, git would end with the earlier key
// while the cache shows the later one. Planning, the cache and the queue
// position are taken under one lock; after a burst of concurrent moves
// the repository holds exactly the keys the cache shows.
func TestConcurrentMovesLeaveGitAndTheCacheAgreeing(t *testing.T) {
	srv, ids := orderingBoard(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	anchors := []string{ids["a1"], ids["b2"], ids["a2"], ""}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			card := []string{ids["b1"], ids["a1"]}[i%2]
			after := anchors[i%len(anchors)]
			if after == card {
				after = ""
			}
			do(t, srv, http.MethodPost, "/api/v1/cards/"+card+"/actions/move", `{"after":"`+after+`"}`)
		}(i)
	}
	wg.Wait()
	srv.store.waitDrained(ctx)
	cached := map[string]string{}
	var e *boardEntry
	srv.store.mu.Lock()
	for _, x := range srv.store.entries {
		e = x
	}
	srv.store.mu.Unlock()
	e.mu.Lock()
	for _, c := range e.board.Cards {
		cached[c.ItemID] = c.Rank
	}
	e.mu.Unlock()
	s, err := gitstore.Load(srv.gitBE.git.domains[0].Repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range s.Cards {
		if cached[c.ItemID] != c.Rank {
			t.Fatalf("%s: the cache holds %q, git %q", c.ItemID, cached[c.ItemID], c.Rank)
		}
	}
}

// A move to where the card already stands writes nothing, and says so:
// moved 0 and no action.
func TestAMoveToWhereTheCardStandsMovesNothing(t *testing.T) {
	srv, ids := orderingBoard(t)
	rec := do(t, srv, http.MethodPost, "/api/v1/cards/"+ids["b2"]+"/actions/move", `{"after":"`+ids["a1"]+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"moved":0}` {
		t.Fatalf("answer = %s, want nothing moved and no action", body)
	}
}
