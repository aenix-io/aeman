package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// frame decodes one marshalled watch frame from a subscription channel.
type frame struct {
	Type   string          `json:"type"`
	Kind   string          `json:"kind"`
	Object json.RawMessage `json:"object"`
}

func readFrame(t *testing.T, sub *subscription) frame {
	t.Helper()
	select {
	case data := <-sub.ch:
		var f frame
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("bad frame: %v (%s)", err, data)
		}
		return f
	default:
		t.Fatal("no frame queued")
		return frame{}
	}
}

func noFrame(t *testing.T, sub *subscription) {
	t.Helper()
	select {
	case data := <-sub.ch:
		t.Fatalf("unexpected frame: %s", data)
	default:
	}
}

func frameUID(t *testing.T, f frame) string {
	t.Helper()
	var c apiserver.Card
	if err := json.Unmarshal(f.Object, &c); err != nil {
		t.Fatalf("bad card object: %v", err)
	}
	return c.Metadata.UID
}

// seedEntry loads a board into a store entry as if LoadBoard had cached it.
func seedEntry(store *boardStore, key string, b board.Board) *boardEntry {
	e := store.entry(key)
	e.mu.Lock()
	e.board = b
	e.loaded = true
	e.mu.Unlock()
	return e
}

func watchBoard() board.Board {
	return board.Board{
		Owner: "acme", Number: 1,
		Cards: []board.Card{
			{ItemID: "c1", Team: "alpha", StartDate: "2026-01-10", SprintStart: "2026-01-10"},
			{ItemID: "c2", Team: "beta", StartDate: "2026-01-10", SprintStart: "2026-01-10"},
		},
		SprintStates: map[string]board.SprintState{
			"alpha": {Current: "2026-01-10", Previous: "2026-01-03", ItemID: "s1"},
			"beta":  {Current: "2026-01-10", ItemID: "s2"},
		},
	}
}

// V3: unscoped subscriptions get the raw verb per card, with echo suppression.
func TestWatchUnscopedCardEvents(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	sub, cancel := store.subscribe("k", "", nil, map[string]bool{"cards": true})
	defer cancel()

	e.mu.Lock()
	c := e.board.Cards[0]
	c.Progress = 50
	e.upsertCard(c)
	e.cardChanged("", c, "MODIFIED")
	e.mu.Unlock()

	f := readFrame(t, sub)
	if f.Type != "MODIFIED" || f.Kind != "Card" || frameUID(t, f) != "c1" {
		t.Fatalf("frame = %+v", f)
	}
}

func TestWatchEchoSuppression(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	mine, cancel1 := store.subscribe("k", "me", nil, map[string]bool{"cards": true})
	defer cancel1()
	other, cancel2 := store.subscribe("k", "other", nil, map[string]bool{"cards": true})
	defer cancel2()

	e.mu.Lock()
	e.cardChanged("me", e.board.Cards[0], "MODIFIED")
	e.mu.Unlock()

	noFrame(t, mine)
	if f := readFrame(t, other); f.Type != "MODIFIED" {
		t.Fatalf("frame = %+v", f)
	}
}

// V4: a scoped subscription sees membership transitions, not raw verbs.
func TestScopedWatchMembership(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	sel := apiserver.Selector{View: "team", Team: "alpha", Day: "2026-01-10"}
	sub, cancel := store.subscribe("k", "", &sel, map[string]bool{"cards": true})
	defer cancel()
	if !sub.members["c1"] || sub.members["c2"] {
		t.Fatalf("seeded members = %v", sub.members)
	}

	// c2 moves into team alpha: it enters the scope as ADDED.
	e.mu.Lock()
	c2 := e.board.Cards[1]
	c2.Team = "alpha"
	e.upsertCard(c2)
	e.cardChanged("", c2, "MODIFIED")
	e.mu.Unlock()
	if f := readFrame(t, sub); f.Type != "ADDED" || frameUID(t, f) != "c2" {
		t.Fatalf("frame = %+v", f)
	}

	// c1 changes within the scope: MODIFIED.
	e.mu.Lock()
	c1 := e.board.Cards[0]
	c1.Progress = 70
	e.upsertCard(c1)
	e.cardChanged("", c1, "MODIFIED")
	e.mu.Unlock()
	if f := readFrame(t, sub); f.Type != "MODIFIED" || frameUID(t, f) != "c1" {
		t.Fatalf("frame = %+v", f)
	}

	// c1 leaves for team beta: DELETED from this subscription's view.
	e.mu.Lock()
	c1.Team = "beta"
	e.upsertCard(c1)
	e.cardChanged("", c1, "MODIFIED")
	e.mu.Unlock()
	if f := readFrame(t, sub); f.Type != "DELETED" || frameUID(t, f) != "c1" {
		t.Fatalf("frame = %+v", f)
	}
}

// V4: the originator's membership is still tracked while its frames are
// suppressed, so later foreign changes do not mis-fire.
func TestScopedWatchOriginMembershipTracked(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	sel := apiserver.Selector{View: "team", Team: "alpha", Day: "2026-01-10"}
	sub, cancel := store.subscribe("k", "me", &sel, map[string]bool{"cards": true})
	defer cancel()

	// My own change brings c2 into scope: no frame, but membership updates.
	e.mu.Lock()
	c2 := e.board.Cards[1]
	c2.Team = "alpha"
	e.upsertCard(c2)
	e.cardChanged("me", c2, "MODIFIED")
	e.mu.Unlock()
	noFrame(t, sub)
	if !sub.members["c2"] {
		t.Fatal("origin membership must be tracked")
	}

	// A foreign change to c2 inside the scope is MODIFIED, not a spurious ADDED.
	e.mu.Lock()
	c2.Progress = 30
	e.upsertCard(c2)
	e.cardChanged("", c2, "MODIFIED")
	e.mu.Unlock()
	if f := readFrame(t, sub); f.Type != "MODIFIED" {
		t.Fatalf("frame = %+v", f)
	}
}

// V4/V5: a sprint move announces the Sprint and re-diffs scoped memberships.
func TestSprintChangeReevaluates(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	teamSel := apiserver.Selector{View: "team", Team: "alpha", Day: "2026-01-10"}
	sub, cancel := store.subscribe("k", "", &teamSel, map[string]bool{"cards": true, "sprints": true})
	defer cancel()
	if !sub.members["c1"] {
		t.Fatalf("members = %v", sub.members)
	}

	// The sprint advances to 01-12; the subscription's memberships are re-diffed
	// against the new pointers and the Sprint resource is announced.
	e.mu.Lock()
	st := e.board.SprintStates["alpha"]
	st.Current, st.Previous = "2026-01-12", "2026-01-10"
	e.board.SprintStates["alpha"] = st
	e.sprintChanged("", "alpha")
	e.mu.Unlock()

	sawSprint, sawDeleted := false, false
	for i := 0; i < 3; i++ {
		select {
		case data := <-sub.ch:
			var f frame
			_ = json.Unmarshal(data, &f)
			if f.Kind == "Sprint" && f.Type == "MODIFIED" {
				sawSprint = true
			}
			if f.Kind == "Card" && f.Type == "DELETED" {
				sawDeleted = true
			}
		default:
		}
	}
	if !sawSprint {
		t.Fatal("a sprint move must announce the Sprint resource")
	}
	// c1 keeps its passed-through pointer day, so it may legitimately stay; the
	// core assertion is that membership was re-diffed without a crash and the
	// member set matches the filter now.
	want := map[string]bool{}
	e.mu.Lock()
	for _, c := range apiserver.FilterCards(e.board, teamSel) {
		want[c.ItemID] = true
	}
	e.mu.Unlock()
	if len(sub.members) != len(want) {
		t.Fatalf("members = %v, want %v (deleted seen: %v)", sub.members, want, sawDeleted)
	}
}

// V5: a move announces one MODIFIED Ordering with the new order.
func TestOrderingEvent(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "k", watchBoard())
	sub, cancel := store.subscribe("k", "", nil, map[string]bool{"ordering": true})
	defer cancel()

	e.mu.Lock()
	e.board.Cards = moveCardAfter(e.board.Cards, "c2", "")
	e.orderingChanged("")
	e.mu.Unlock()

	f := readFrame(t, sub)
	if f.Type != "MODIFIED" || f.Kind != "Ordering" {
		t.Fatalf("frame = %+v", f)
	}
	var o apiserver.Ordering
	if err := json.Unmarshal(f.Object, &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Spec.UIDs) != 2 || o.Spec.UIDs[0] != "c2" {
		t.Fatalf("ordering = %+v", o.Spec.UIDs)
	}
}

// A TTL reload must not lose a card created through aeman seconds ago (the
// GitHub item list is eventually consistent), nor resurrect one just deleted.
func TestReloadKeepsRecentMutations(t *testing.T) {
	store := newBoardStore()
	e := seedEntry(store, "acme/1", watchBoard())

	// c3 was just created locally; c1 just deleted.
	e.mu.Lock()
	c3 := board.Card{ItemID: "c3", Team: "alpha"}
	e.upsertCard(c3)
	e.markRecent("c3")
	e.removeCard("c1")
	e.markGone("c1")
	e.mu.Unlock()

	// A stale upstream fetch: still lists c1, does not know c3 yet.
	e.mu.Lock()
	fresh := e.applyRecent(watchBoard())
	e.mu.Unlock()

	ids := map[string]bool{}
	for _, c := range fresh.Cards {
		ids[c.ItemID] = true
	}
	if !ids["c3"] {
		t.Fatalf("recently created card lost on reload: %v", ids)
	}
	if ids["c1"] {
		t.Fatalf("recently deleted card resurrected on reload: %v", ids)
	}
}

// authzBackend is a per-user inner backend: LoadBoard succeeds or fails
// depending on whether that user's token can read the board, and counts calls
// so the test can prove when the shared cache was (not) consulted.
type authzBackend struct {
	boardservice.Backend // nil: only LoadBoard is exercised
	loads                int
	deny                 bool
}

func (a *authzBackend) LoadBoard(_ context.Context, _ string, _ int) (board.Board, error) {
	a.loads++
	if a.deny {
		return board.Board{}, errors.New("github: not authorized")
	}
	return board.Board{Owner: "acme", Number: 1}, nil
}

// A warm board cached by one authorized user must not be served to a different
// signed-in user until that user's own token has proven access — the shared
// cache is keyed only by owner/project, so per-login authorization is the gate.
func TestBoardCachePerUserAuthz(t *testing.T) {
	store := newBoardStore()

	alice := &authzBackend{}
	aliceBE := &storeBackend{inner: alice, store: store, multiUser: true}
	mallory := &authzBackend{deny: true}
	malloryBE := &storeBackend{inner: mallory, store: store, multiUser: true}

	aliceCtx := board.WithActor(context.Background(), "alice")
	malloryCtx := board.WithActor(context.Background(), "mallory")

	// Alice's first read authorizes her token and warms the cache.
	if _, err := aliceBE.LoadBoard(aliceCtx, "acme", 1); err != nil {
		t.Fatalf("alice load: %v", err)
	}
	if alice.loads != 1 {
		t.Fatalf("alice loads = %d, want 1", alice.loads)
	}
	// Alice again within the TTL: served from cache, no second backend hit.
	if _, err := aliceBE.LoadBoard(aliceCtx, "acme", 1); err != nil {
		t.Fatalf("alice second load: %v", err)
	}
	if alice.loads != 1 {
		t.Fatalf("alice cache hit expected, loads = %d", alice.loads)
	}
	// Mallory hits the same warm cache but has never authorized: the store must
	// fall through to HER token-scoped load, which GitHub rejects. Without the
	// gate she would read alice's cached board.
	if _, err := malloryBE.LoadBoard(malloryCtx, "acme", 1); err == nil {
		t.Fatal("mallory read a board she cannot access (cache leak)")
	}
	if mallory.loads != 1 {
		t.Fatalf("mallory's token was never checked, loads = %d", mallory.loads)
	}
}

// swrBackend serves a configurable board and counts loads (mutex-guarded: the
// background revalidation loads from its own goroutine).
type swrBackend struct {
	boardservice.Backend // nil: only LoadBoard is exercised
	mu                   sync.Mutex
	board                board.Board
	loads                int
}

func (s *swrBackend) LoadBoard(_ context.Context, _ string, _ int) (board.Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	return s.board, nil
}

func (s *swrBackend) set(b board.Board) {
	s.mu.Lock()
	s.board = b
	s.mu.Unlock()
}

func (s *swrBackend) loadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads
}

// waitFrame reads one frame, waiting for the background revalidation to emit it.
func waitFrame(t *testing.T, sub *subscription) frame {
	t.Helper()
	select {
	case data := <-sub.ch:
		var f frame
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("bad frame: %v (%s)", err, data)
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5s")
		return frame{}
	}
}

// A read that opted in (staleControl in ctx) gets the stale snapshot instantly
// while a background reload revalidates; watchers then receive the external
// change as an ordinary MODIFIED event followed by a Sync frame.
func TestStaleServeRevalidates(t *testing.T) {
	store := newBoardStore()
	inner := &swrBackend{board: watchBoard()}
	be := &storeBackend{inner: inner, store: store}

	// Warm the cache, then age it past the fresh TTL.
	if _, err := be.LoadBoard(context.Background(), "acme", 1); err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")
	e.mu.Lock()
	e.loadedAt = time.Now().Add(-2 * boardFreshFor)
	e.mu.Unlock()

	sub, cancel := store.subscribe("acme/1", "", nil, map[string]bool{"cards": true})
	defer cancel()

	// The upstream board changed outside aeman.
	changed := watchBoard()
	changed.Cards[0].Progress = 80
	inner.set(changed)

	ctx, sc := withStaleAllowed(context.Background())
	got, err := be.LoadBoard(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cards[0].Progress == 80 {
		t.Fatal("expected the stale snapshot, got the fresh board")
	}
	if !sc.served.Load() {
		t.Fatal("the stale serve was not recorded for the response header")
	}

	// The background revalidation delivers the diff, then the Sync frame.
	f := waitFrame(t, sub)
	if f.Type != "MODIFIED" || f.Kind != "Card" || frameUID(t, f) != "c1" {
		t.Fatalf("want MODIFIED Card c1, got %s %s", f.Type, f.Kind)
	}
	if f = waitFrame(t, sub); f.Kind != "Sync" {
		t.Fatalf("want Sync frame, got %s %s", f.Type, f.Kind)
	}
	if inner.loadCount() != 2 {
		t.Fatalf("backend loads = %d, want 2", inner.loadCount())
	}
	// The cache is fresh now: the next read hits it without another load.
	if got, err = be.LoadBoard(context.Background(), "acme", 1); err != nil || got.Cards[0].Progress != 80 {
		t.Fatalf("revalidated board not served: %+v %v", got.Cards[0], err)
	}
	if inner.loadCount() != 2 {
		t.Fatalf("backend loads = %d, want 2 (cache hit)", inner.loadCount())
	}
}

// A read without the opt-in (every mutation's internal load) must never see a
// stale snapshot: carry-over picks cards by the live sprint pointer, a move
// resolves its anchor from the live order.
func TestMutationReadsBlockForFresh(t *testing.T) {
	store := newBoardStore()
	inner := &swrBackend{board: watchBoard()}
	be := &storeBackend{inner: inner, store: store}

	if _, err := be.LoadBoard(context.Background(), "acme", 1); err != nil {
		t.Fatal(err)
	}
	e := store.entry("acme/1")
	e.mu.Lock()
	e.loadedAt = time.Now().Add(-2 * boardFreshFor)
	e.mu.Unlock()

	changed := watchBoard()
	changed.Cards[0].Progress = 80
	inner.set(changed)

	got, err := be.LoadBoard(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cards[0].Progress != 80 {
		t.Fatal("a non-opted read was served a stale snapshot")
	}
}

// The full HTTP path: a GET in the stale window is answered instantly from
// the stale snapshot and flagged with X-Aeman-Stale; a cold GET is not.
func TestStaleHeaderOnAPIRead(t *testing.T) {
	srv := newTestServer(t)
	inner := &swrBackend{board: watchBoard()}
	srv.newService = func(*http.Request) (*boardservice.Service, error) {
		return boardservice.New(&storeBackend{inner: inner, store: srv.store}), nil
	}

	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cards?owner=acme&project=1&view=all", nil)
		srv.handler.ServeHTTP(rec, req)
		return rec
	}

	rec := get()
	if rec.Code != http.StatusOK {
		t.Fatalf("cold read: status = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Aeman-Stale") != "" {
		t.Fatal("a cold read must not be flagged stale")
	}

	e := srv.store.entry("acme/1")
	e.mu.Lock()
	e.loadedAt = time.Now().Add(-2 * boardFreshFor)
	e.mu.Unlock()

	rec = get()
	if rec.Code != http.StatusOK {
		t.Fatalf("stale read: status = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Aeman-Stale") != "true" {
		t.Fatalf("a read in the stale window must be flagged, headers: %v", rec.Header())
	}
}

// cached gates a hit on load freshness AND, in multi-user mode, the caller's
// proven access: a fresh hit needs both within their fresh TTLs, an older
// board or proof (each up to boardStaleMax) degrades to a stale hit, and a
// login that never proved access — or nothing within boardStaleMax — misses.
// Local single-user mode (empty login, multiUser=false) skips the login gate.
func TestCachedAuthz(t *testing.T) {
	e := &boardEntry{loaded: true, loadedAt: time.Now()}
	if _, state := e.cached("", false); state != cacheFresh {
		t.Fatal("local single-user mode should serve the fresh cache")
	}
	if _, state := e.cached("alice", true); state != cacheMiss {
		t.Fatal("multi-user: unauthorized login must miss the cache")
	}
	e.markAuthed("alice")
	if _, state := e.cached("alice", true); state != cacheFresh {
		t.Fatal("multi-user: authorized login must hit the fresh cache")
	}
	if _, state := e.cached("", true); state != cacheMiss {
		t.Fatal("multi-user: an empty login must never hit the cache")
	}
	e.loadedAt = time.Now().Add(-2 * boardFreshFor)
	if _, state := e.cached("alice", true); state != cacheStale {
		t.Fatal("past the fresh TTL an authorized login must get a stale hit")
	}
	if _, state := e.cached("mallory", true); state != cacheMiss {
		t.Fatal("a stale hit must still require per-login authorization")
	}
	// A fresh board with an aging proof degrades the same way: the background
	// reload re-checks the token instead of blocking the read on it.
	e.loadedAt = time.Now()
	e.authed["alice"] = time.Now().Add(-2 * authFreshFor)
	if _, state := e.cached("alice", true); state != cacheStale {
		t.Fatal("an aged authorization must degrade a fresh board to a stale hit")
	}
	e.authed["alice"] = time.Now().Add(-boardStaleMax - time.Second)
	if _, state := e.cached("alice", true); state != cacheMiss {
		t.Fatal("an authorization older than boardStaleMax must miss")
	}
	e.markAuthed("alice")
	e.loadedAt = time.Now().Add(-boardStaleMax - time.Second)
	if _, state := e.cached("alice", true); state != cacheMiss {
		t.Fatal("past boardStaleMax the cache must miss regardless of authorization")
	}
}
