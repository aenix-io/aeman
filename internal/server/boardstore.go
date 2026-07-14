package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aenix-org/aeman/pkg/apiserver"
	"github.com/aenix-org/aeman/pkg/board"
	"github.com/aenix-org/aeman/pkg/boardservice"
)

// watchFrame is one event on the watch stream: a typed change to a Card,
// Sprint or Ordering resource, mirroring the Kubernetes watch verbs.
type watchFrame struct {
	Type   string `json:"type"` // ADDED | MODIFIED | DELETED
	Kind   string `json:"kind"` // Card | Sprint | Ordering
	Object any    `json:"object,omitempty"`
}

// subscription is one watch connection's registration: the client id for echo
// suppression, the resource kinds it wants, and — when scoped by a selector —
// its current view membership, kept server-side so entering/leaving the scope
// turns into ADDED/DELETED events.
type subscription struct {
	ch        chan []byte
	clientID  string
	sel       *apiserver.Selector
	resources map[string]bool
	members   map[string]bool
}

// send marshals and delivers one frame; a slow subscriber drops it and
// reconciles on its next re-list.
func (sub *subscription) send(frame watchFrame) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	select {
	case sub.ch <- data:
	default:
	}
}

// clientIDCtxKey carries the mutating client's self-assigned id (the
// X-Aeman-Client header) down to the store, so its own watch connection is not
// echoed events for changes it made itself — the client already holds that
// state optimistically, and mid-sequence echoes would make cards jump around.
type clientIDCtxKey struct{}

func withClientID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, clientIDCtxKey{}, id)
}

func clientIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(clientIDCtxKey{}).(string)
	return id
}

// boardFreshFor bounds how long a cached board is served before a read reloads
// it from the backend, so edits made outside aeman eventually surface.
const boardFreshFor = 30 * time.Second

// boardStaleMax bounds how old a snapshot a read-only request may still be
// served while the store revalidates in the background. Past it (the first
// visit of the morning) the read blocks on a fresh load instead of flashing
// hours-old state.
const boardStaleMax = 10 * time.Minute

// authFreshFor bounds how long a login's proven read access backs a fresh
// cache hit. An older proof (up to boardStaleMax) degrades the hit to stale:
// still served to read paths, while the background reload re-checks the
// token — so access revoked on GitHub takes effect within boardStaleMax, and
// within this window for callers that cannot accept stale data.
const authFreshFor = 60 * time.Second

// boardEntry is the cached board plus its watcher set for one owner/project.
type boardEntry struct {
	mu sync.Mutex
	// loadMu serializes full backend loads so concurrent cache misses (e.g. a
	// burst of parallel mutations right after an invalidation) share one upstream
	// fetch instead of stampeding GitHub.
	loadMu   sync.Mutex
	board    board.Board
	loadedAt time.Time
	loaded   bool
	// watchers is the subscription set; each carries its own scope and echo
	// suppression id.
	watchers map[*subscription]struct{}
	// presence maps a client id to the card its user has selected in the Me
	// view — ephemeral shared-cursor state, never persisted, cleared when the
	// client's watch connection goes away.
	presence map[string]presenceEntry
	// authed records, per login, when that user's own GitHub token last proved
	// it can read this board (a token-scoped backend load succeeded). The
	// shared cache is keyed only by owner/project, so without this a warm board
	// would be served to any signed-in user regardless of access; a cache hit
	// is allowed only for a login that authorized within authFreshFor.
	authed map[string]time.Time
	// recentCards / recentGone guard the cache against GitHub's eventually
	// consistent item list: a card created (or deleted) through aeman seconds
	// ago may still be missing from (or present in) a fresh full load, and a
	// TTL reload right after the mutation would lose (or resurrect) it. Cards
	// touched within recentGrace are re-applied on top of every full reload.
	recentCards map[string]time.Time
	recentGone  map[string]time.Time
	// pending is the write-behind queue: changes already live in this cache
	// that GitHub has not confirmed yet (see writequeue.go). inflight is the
	// op currently on the wire (still unconfirmed, so counters and reload
	// replays include it); draining marks the background worker as running.
	pending  []pendingOp
	inflight *pendingOp
	draining bool
	// recentMove is when a local reorder last touched this board: within
	// recentGrace the cached order outweighs a fresh (possibly stale) read.
	recentMove time.Time
}

// recentGrace is how long a local mutation outweighs a full reload.
const recentGrace = 90 * time.Second

// markRecent records a locally created/updated card. The caller holds e.mu.
func (e *boardEntry) markRecent(itemID string) {
	if e.recentCards == nil {
		e.recentCards = map[string]time.Time{}
	}
	e.recentCards[itemID] = time.Now()
	delete(e.recentGone, itemID)
}

// markGone records a locally deleted card. The caller holds e.mu.
func (e *boardEntry) markGone(itemID string) {
	if e.recentGone == nil {
		e.recentGone = map[string]time.Time{}
	}
	e.recentGone[itemID] = time.Now()
	delete(e.recentCards, itemID)
}

// applyRecent reconciles a freshly loaded board with the local recency guards:
// recently created cards missing from the fetch are restored from the old
// cache, recently deleted ones still present in the fetch are dropped. The
// caller holds e.mu; the returned board replaces the cache.
func (e *boardEntry) applyRecent(fresh board.Board) board.Board {
	now := time.Now()
	for id, ts := range e.recentGone {
		if now.Sub(ts) > recentGrace {
			delete(e.recentGone, id)
		}
	}
	for id, ts := range e.recentCards {
		if now.Sub(ts) > recentGrace {
			delete(e.recentCards, id)
		}
	}
	if len(e.recentGone) > 0 {
		kept := fresh.Cards[:0]
		for _, c := range fresh.Cards {
			if _, gone := e.recentGone[c.ItemID]; !gone {
				kept = append(kept, c)
			}
		}
		fresh.Cards = kept
	}
	if len(e.recentCards) > 0 {
		// A recently-written card's CACHED copy outweighs the fresh read for
		// the whole grace window: GitHub's read replicas lag its writes by
		// seconds, so a revalidation right after the queue drained would
		// otherwise install the pre-write values — the user's progress/stage
		// silently rolled back, then came back on a later reload.
		oldByID := make(map[string]board.Card, len(e.board.Cards))
		for _, c := range e.board.Cards {
			oldByID[c.ItemID] = c
		}
		have := map[string]bool{}
		for i := range fresh.Cards {
			have[fresh.Cards[i].ItemID] = true
			if _, recent := e.recentCards[fresh.Cards[i].ItemID]; !recent {
				continue
			}
			if old, ok := oldByID[fresh.Cards[i].ItemID]; ok {
				fresh.Cards[i] = old
			}
		}
		// A recent card the lagging read hasn't caught up with is restored
		// AT ITS CACHED SLOT — right after the nearest cached predecessor the
		// fresh read knows — not appended at the end: the append made every
		// revalidation shortly after a create visibly throw the new card (and
		// everything below it) to the bottom of the board.
		pos := make(map[string]int, len(fresh.Cards))
		for i, c := range fresh.Cards {
			pos[c.ItemID] = i
		}
		for ci, old := range e.board.Cards {
			if _, recent := e.recentCards[old.ItemID]; !recent || have[old.ItemID] {
				continue
			}
			at := 0
			for j := ci - 1; j >= 0; j-- {
				if p, ok := pos[e.board.Cards[j].ItemID]; ok {
					at = p + 1
					break
				}
			}
			fresh.Cards = append(fresh.Cards, board.Card{})
			copy(fresh.Cards[at+1:], fresh.Cards[at:])
			fresh.Cards[at] = old
			have[old.ItemID] = true
			for id, p := range pos {
				if p >= at {
					pos[id] = p + 1
				}
			}
			pos[old.ItemID] = at
		}
	}
	// The same lag rolls back the ORDER a local move just wrote: while a
	// recent move is inside the grace window, the cached order wins for the
	// cards both sides know; fresh-only cards keep their fetched positions.
	if !e.recentMove.IsZero() && now.Sub(e.recentMove) <= recentGrace {
		rank := make(map[string]int, len(e.board.Cards))
		for i, c := range e.board.Cards {
			rank[c.ItemID] = i
		}
		known := make([]board.Card, 0, len(fresh.Cards))
		fixed := map[int]board.Card{}
		for i, c := range fresh.Cards {
			if _, ok := rank[c.ItemID]; ok {
				known = append(known, c)
			} else {
				fixed[i] = c
			}
		}
		sortCardsByRank(known, rank)
		merged := make([]board.Card, 0, len(fresh.Cards))
		ki := 0
		for i := 0; i < len(fresh.Cards); i++ {
			if c, ok := fixed[i]; ok {
				merged = append(merged, c)
				continue
			}
			merged = append(merged, known[ki])
			ki++
		}
		fresh.Cards = merged
	}
	return fresh
}

// sortCardsByRank orders cards by their previous cache positions (stable).
func sortCardsByRank(cards []board.Card, rank map[string]int) {
	sort.SliceStable(cards, func(i, j int) bool {
		return rank[cards[i].ItemID] < rank[cards[j].ItemID]
	})
}

// cacheState grades a cache lookup: a fresh hit is served to anyone allowed,
// a stale one only to read paths that revalidate in the background, a miss
// always loads.
type cacheState int

const (
	cacheMiss cacheState = iota
	cacheStale
	cacheFresh
)

// cached returns the board and how usable it is for this caller. cacheFresh
// needs both the board within boardFreshFor and, for a named login (OAuth
// multi-user mode; an empty login is the single local user, always allowed),
// token-scoped access proven within authFreshFor. Past either TTL — but with
// the board loaded and the login's proof both within boardStaleMax — the hit
// degrades to cacheStale: read paths may serve it while a background reload
// with the caller's own token refreshes the board AND re-proves their access,
// so a revoked token stops refreshing its proof and hard-fails within
// boardStaleMax. A login that never proved access always misses — the shared
// cache is keyed only by owner/project, and this gate is what keeps one
// user's warm board from leaking to another. multiUser forces the login check
// even if a login somehow arrives empty, so an OAuth deployment never serves
// the cache without a per-user authorization.
func (e *boardEntry) cached(login string, multiUser bool) (board.Board, cacheState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.loaded {
		return board.Board{}, cacheMiss
	}
	age := time.Since(e.loadedAt)
	if age >= boardStaleMax {
		return board.Board{}, cacheMiss
	}
	authAge := time.Duration(0)
	if multiUser || login != "" {
		ts, ok := e.authed[login]
		if login == "" || !ok {
			return board.Board{}, cacheMiss
		}
		authAge = time.Since(ts)
		if authAge >= boardStaleMax {
			return board.Board{}, cacheMiss
		}
	}
	if age >= boardFreshFor || authAge >= authFreshFor {
		return e.board, cacheStale
	}
	return e.board, cacheFresh
}

// markAuthed records that a login's own token just proved read access, and
// sweeps entries too old to back even a stale hit so the map tracks only
// currently-active users. The caller holds e.mu.
func (e *boardEntry) markAuthed(login string) {
	if login == "" {
		return
	}
	if e.authed == nil {
		e.authed = map[string]time.Time{}
	}
	now := time.Now()
	for l, ts := range e.authed {
		if now.Sub(ts) >= boardStaleMax {
			delete(e.authed, l)
		}
	}
	e.authed[login] = now
}

// cardChanged fans one card change out to the subscriptions. The caller holds
// e.mu with the cache already updated. Unscoped subscriptions get the verb as
// is; scoped ones get the membership transition (entering the scope is ADDED,
// leaving it is DELETED, staying is MODIFIED). The originating client's own
// events are suppressed — it already holds the change optimistically — but its
// membership is still tracked, or later diffs would mis-fire.
func (e *boardEntry) cardChanged(origin string, c board.Card, verb string) {
	res := apiserver.CardResource(e.board, c)
	for sub := range e.watchers {
		if !sub.resources["cards"] {
			continue
		}
		suppressed := origin != "" && sub.clientID == origin
		if sub.sel == nil {
			if !suppressed {
				sub.send(watchFrame{Type: verb, Kind: "Card", Object: res})
			}
			continue
		}
		was := sub.members[c.ItemID]
		now := verb != "DELETED" && sub.sel.Matches(e.board, c)
		if now {
			sub.members[c.ItemID] = true
		} else {
			delete(sub.members, c.ItemID)
		}
		if suppressed {
			continue
		}
		switch {
		case now && !was:
			sub.send(watchFrame{Type: "ADDED", Kind: "Card", Object: res})
		case was && !now:
			sub.send(watchFrame{Type: "DELETED", Kind: "Card", Object: res})
		case was && now:
			sub.send(watchFrame{Type: "MODIFIED", Kind: "Card", Object: res})
		}
	}
}

// reevaluate recomputes every scoped subscription's membership against the
// cached board and emits the deltas — the board shifted in a way no single
// card event expresses (a sprint pointer moved, or the day rolled over).
// The caller holds e.mu.
func (e *boardEntry) reevaluate(origin string) {
	for sub := range e.watchers {
		if sub.sel == nil || !sub.resources["cards"] {
			continue
		}
		suppressed := origin != "" && sub.clientID == origin
		now := map[string]bool{}
		for _, c := range apiserver.FilterCards(e.board, *sub.sel) {
			now[c.ItemID] = true
			if !sub.members[c.ItemID] && !suppressed {
				sub.send(watchFrame{Type: "ADDED", Kind: "Card", Object: apiserver.CardResource(e.board, c)})
			}
		}
		for id := range sub.members {
			if now[id] || suppressed {
				continue
			}
			obj := apiserver.Card{Kind: "Card", Metadata: apiserver.CardMetadata{UID: id}}
			for _, c := range e.board.Cards {
				if c.ItemID == id {
					obj = apiserver.CardResource(e.board, c)
					break
				}
			}
			sub.send(watchFrame{Type: "DELETED", Kind: "Card", Object: obj})
		}
		sub.members = now
	}
}

// sprintChanged announces a team's moved sprint pointer and re-evaluates the
// scoped memberships it may have shifted. The caller holds e.mu with the
// cached pointer already updated.
func (e *boardEntry) sprintChanged(origin, team string) {
	st := e.board.SprintStates[team]
	res := apiserver.Sprint{
		Kind:     "Sprint",
		Metadata: apiserver.SprintMetadata{Team: team},
		Spec:     apiserver.SprintSpec{Current: st.Current, Previous: st.Previous},
	}
	for sub := range e.watchers {
		if !sub.resources["sprints"] || (origin != "" && sub.clientID == origin) {
			continue
		}
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Sprint", Object: res})
	}
	e.reevaluate(origin)
}

// orderingChanged announces the board's new manual order. The caller holds
// e.mu with the cache already reordered; membership is unaffected.
func (e *boardEntry) orderingChanged(origin string) {
	res := apiserver.OrderingResource(e.board)
	for sub := range e.watchers {
		if !sub.resources["ordering"] || (origin != "" && sub.clientID == origin) {
			continue
		}
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Ordering", Object: res})
	}
}

// presenceEntry is one user's live selection: the card their Me view has
// selected right now.
type presenceEntry struct {
	Login string `json:"login"`
	Card  string `json:"card"`
}

// presenceChanged announces one user's selection to every subscription that
// wants presence, skipping the originator (their own tab already shows it).
// The caller holds e.mu.
func (e *boardEntry) presenceChanged(origin string, entry presenceEntry) {
	for sub := range e.watchers {
		if !sub.resources["presence"] || (origin != "" && sub.clientID == origin) {
			continue
		}
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Presence", Object: entry})
	}
}

// SetPresence records (card != "") or clears (card == "") the selection of the
// client's user and broadcasts it.
func (s *boardStore) SetPresence(key, clientID, login, card string) {
	if clientID == "" {
		return
	}
	e := s.entry(key)
	e.mu.Lock()
	if card == "" {
		delete(e.presence, clientID)
	} else {
		e.presence[clientID] = presenceEntry{Login: login, Card: card}
	}
	e.presenceChanged(clientID, presenceEntry{Login: login, Card: card})
	e.mu.Unlock()
}

// ClearPresence drops a departing client's selection (the watch connection
// closed) and tells everyone.
func (s *boardStore) ClearPresence(key, clientID string) {
	if clientID == "" {
		return
	}
	e := s.entry(key)
	e.mu.Lock()
	entry, ok := e.presence[clientID]
	if ok {
		delete(e.presence, clientID)
		e.presenceChanged(clientID, presenceEntry{Login: entry.Login, Card: ""})
	}
	e.mu.Unlock()
}

// moveCardAfter reorders cards so itemID sits after afterID ("" = the top),
// keeping snapshots (and write-behind replays) in the board's real order.
func moveCardAfter(cards []board.Card, itemID, afterID string) []board.Card {
	idx := -1
	for i := range cards {
		if cards[i].ItemID == itemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return cards
	}
	card := cards[idx]
	rest := make([]board.Card, 0, len(cards)-1)
	rest = append(rest, cards[:idx]...)
	rest = append(rest, cards[idx+1:]...)
	pos := 0
	if afterID != "" {
		for i := range rest {
			if rest[i].ItemID == afterID {
				pos = i + 1
				break
			}
		}
	}
	out := make([]board.Card, 0, len(rest)+1)
	out = append(out, rest[:pos]...)
	out = append(out, card)
	out = append(out, rest[pos:]...)
	return out
}

// upsertCard replaces the cached card with the same item id, or appends it.
func (e *boardEntry) upsertCard(card board.Card) {
	for i := range e.board.Cards {
		if e.board.Cards[i].ItemID == card.ItemID {
			e.board.Cards[i] = card
			return
		}
	}
	e.board.Cards = append(e.board.Cards, card)
}

// removeCard drops the cached card with the given item id.
func (e *boardEntry) removeCard(itemID string) {
	out := e.board.Cards[:0]
	for _, c := range e.board.Cards {
		if c.ItemID != itemID {
			out = append(out, c)
		}
	}
	e.board.Cards = out
}

// boardStore holds the per-owner/project board cache and watcher sets, shared
// across requests so a mutation served by one request reaches watchers opened by
// another. It is the in-memory reflector the watch stream and snapshot read from.
type boardStore struct {
	mu      sync.Mutex
	entries map[string]*boardEntry
}

func newBoardStore() *boardStore {
	return &boardStore{entries: map[string]*boardEntry{}}
}

func storeKey(owner string, project int) string {
	return owner + "/" + strconv.Itoa(project)
}

// entry returns the entry for a key, creating an empty one on first use.
func (s *boardStore) entry(key string) *boardEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		e = &boardEntry{
			watchers: map[*subscription]struct{}{},
			presence: map[string]presenceEntry{},
		}
		s.entries[key] = e
	}
	return e
}

// subscribe registers a watch subscription: clientID keys echo suppression
// ("" = unknown), sel scopes it to a view/selector (nil = every card), and
// resources picks the kinds ("cards", "sprints", "ordering"). A scoped
// subscription's membership is seeded from the cached board, so subscribe
// after the board is loaded and LIST after subscribing.
func (s *boardStore) subscribe(key, clientID string, sel *apiserver.Selector, resources map[string]bool) (*subscription, func()) {
	e := s.entry(key)
	sub := &subscription{
		ch:        make(chan []byte, 64),
		clientID:  clientID,
		sel:       sel,
		resources: resources,
		members:   map[string]bool{},
	}
	e.mu.Lock()
	if sel != nil && e.loaded {
		for _, c := range apiserver.FilterCards(e.board, *sel) {
			sub.members[c.ItemID] = true
		}
	}
	e.watchers[sub] = struct{}{}
	if resources["presence"] {
		for _, entry := range e.presence {
			sub.send(watchFrame{Type: "MODIFIED", Kind: "Presence", Object: entry})
		}
	}
	// Seed the unsynced-changes counter: a tab opened while the write-behind
	// queue is non-empty must show the number right away.
	if len(e.pending) > 0 {
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Queue", Object: map[string]int{
			"pending": len(e.pending),
		}})
	}
	e.mu.Unlock()
	return sub, func() {
		e.mu.Lock()
		delete(e.watchers, sub)
		e.mu.Unlock()
	}
}

// reevaluateAll re-diffs every scoped subscription on a board — the watch
// handlers call it when the local day rolls over, since day-relative views
// shift at midnight without any board change.
func (s *boardStore) reevaluateAll(key string) {
	e := s.entry(key)
	e.mu.Lock()
	if e.loaded {
		e.reevaluate("")
	}
	e.mu.Unlock()
}

// storeBackend wraps a boardservice.Backend so reads are served from the shared
// board store and writes update the cache and notify watchers. It keeps the API
// and MCP fast — a mutation reloads only the card it touched instead of paging
// the whole board — and turns every aeman-mediated change into a watch event.
type storeBackend struct {
	inner boardservice.Backend
	store *boardStore
	// multiUser is set in OAuth mode, where each request carries a distinct
	// user's token: a cache hit is then gated on that login having proven
	// token-scoped access. Off in local-proxy mode (a single gh identity).
	multiUser bool
}

var _ boardservice.Backend = (*storeBackend)(nil)

// LoadBoard serves the cached board while it is fresh. A stale-but-recent
// board is still served instantly to read paths that opted in (staleControl in
// ctx) while a background reload revalidates it — watchers then receive the
// diff as ordinary events plus a Sync frame. Everything else (mutation reads,
// cold or too-old caches, unauthorized logins) blocks on a fresh load
// (single-flight: concurrent misses share one fetch).
func (b *storeBackend) LoadBoard(ctx context.Context, owner string, project int) (board.Board, error) {
	e := b.store.entry(storeKey(owner, project))
	login := board.ActorFrom(ctx)
	bd, state := e.cached(login, b.multiUser)
	if state == cacheFresh {
		return bd, nil
	}
	if state == cacheStale {
		if sc := staleControlFrom(ctx); sc != nil {
			b.revalidate(ctx, e, owner, project)
			sc.served.Store(true)
			return bd, nil
		}
	}
	e.loadMu.Lock()
	defer e.loadMu.Unlock()
	// A concurrent loader may have refreshed the cache while we waited.
	if bd, state := e.cached(login, b.multiUser); state == cacheFresh {
		return bd, nil
	}
	// The token-scoped backend load is the authorization check: GitHub rejects a
	// token that can't read this board, so reaching a cached result requires
	// having passed it (recorded per login in install).
	fresh, err := b.inner.LoadBoard(ctx, owner, project)
	if err != nil {
		return board.Board{}, err
	}
	return b.install(e, fresh, login), nil
}

// revalidate refreshes a stale cache in the background with the requesting
// user's token (kept past the response via WithoutCancel). Single-flight: when
// a load is already running its completion will broadcast, so this one backs
// off instead of stacking a second upstream fetch.
func (b *storeBackend) revalidate(ctx context.Context, e *boardEntry, owner string, project int) {
	if !e.loadMu.TryLock() {
		return
	}
	login := board.ActorFrom(ctx)
	bctx := context.WithoutCancel(ctx)
	go func() {
		defer e.loadMu.Unlock()
		fresh, err := b.inner.LoadBoard(bctx, owner, project)
		if err != nil {
			// The stale snapshot stays served; tell clients to drop their
			// revalidation hold and let a later read retry.
			e.mu.Lock()
			e.syncBroadcast()
			e.mu.Unlock()
			return
		}
		b.install(e, fresh, login)
	}()
}

// install replaces the cache with a freshly loaded board, records the loading
// login's proven access, fans the diff against the previous snapshot out to
// watchers as ordinary events, and closes with a Sync frame. Returns the
// installed board (with recent local mutations re-applied).
func (b *storeBackend) install(e *boardEntry, fresh board.Board, login string) board.Board {
	e.mu.Lock()
	defer e.mu.Unlock()
	old := e.board
	hadOld := e.loaded
	fresh = e.applyRecent(fresh)
	e.board = fresh
	// Replay the write-behind queue on top (the in-flight op first): these
	// changes are live in the cache but not confirmed by GitHub yet, so a
	// fresh load — which predates them — must not roll them back.
	if e.inflight != nil {
		e.inflight.apply(&e.board)
	}
	for _, op := range e.pending {
		op.apply(&e.board)
	}
	e.loaded = true
	e.loadedAt = time.Now()
	e.markAuthed(login)
	if hadOld {
		e.diffNotify(old)
	}
	e.syncBroadcast()
	return e.board
}

// diffNotify announces everything a full reload changed against the previous
// snapshot — external edits made outside aeman become watch events just like
// aeman-mediated ones. The caller holds e.mu with the new board installed.
func (e *boardEntry) diffNotify(old board.Board) {
	oldByID := make(map[string]board.Card, len(old.Cards))
	for _, c := range old.Cards {
		oldByID[c.ItemID] = c
	}
	seen := make(map[string]bool, len(e.board.Cards))
	for _, c := range e.board.Cards {
		seen[c.ItemID] = true
		prev, ok := oldByID[c.ItemID]
		switch {
		case !ok:
			e.cardChanged("", c, "ADDED")
		case !reflect.DeepEqual(prev, c):
			e.cardChanged("", c, "MODIFIED")
		}
	}
	for _, c := range old.Cards {
		if !seen[c.ItemID] {
			e.cardChanged("", c, "DELETED")
		}
	}
	for team, st := range e.board.SprintStates {
		if old.SprintStates[team] != st {
			e.sprintChanged("", team)
		}
	}
	for team := range old.SprintStates {
		if _, ok := e.board.SprintStates[team]; !ok {
			e.sprintChanged("", team)
		}
	}
	orderChanged := len(old.Cards) != len(e.board.Cards)
	if !orderChanged {
		for i := range e.board.Cards {
			if e.board.Cards[i].ItemID != old.Cards[i].ItemID {
				orderChanged = true
				break
			}
		}
	}
	if orderChanged {
		e.orderingChanged("")
	}
}

// syncBroadcast tells every watcher a full reload just finished. Clients that
// were served a stale snapshot use it to drop their revalidation hold — the
// data itself already arrived as the diff's ordinary events. The caller holds
// e.mu.
func (e *boardEntry) syncBroadcast() {
	frame := watchFrame{Type: "MODIFIED", Kind: "Sync", Object: map[string]string{
		"loadedAt": e.loadedAt.UTC().Format(time.RFC3339),
	}}
	for sub := range e.watchers {
		sub.send(frame)
	}
}

// LoadCards passes straight through: it is already a partial read.
func (b *storeBackend) LoadCards(ctx context.Context, bd board.Board, ids []string) ([]board.Card, error) {
	return b.inner.LoadCards(ctx, bd, ids)
}

// touched reloads the one card a mutation changed, updates the cache and emits a
// MODIFIED event. Reload failures are swallowed: the periodic re-list reconciles.
// The write-behind queue (in-flight op included) is replayed on top of the fresh
// copy: this refresh runs while later queued writes to the same card may exist
// (rapid note adds), and the upstream copy predates them — without the replay
// their changes would vanish from the cache until the queue drains.
func (b *storeBackend) touched(ctx context.Context, bd board.Board, itemID string) {
	cards, err := b.inner.LoadCards(ctx, bd, []string{itemID})
	if err != nil || len(cards) == 0 {
		return
	}
	card := cards[0]
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	// Refresh, never resurrect: the card may have been deleted while this
	// op drained, and GitHub's lagging read replicas can still return it —
	// upserting would bring it back AND markRecent below would strip its
	// recentGone protection, so the ghost then survives every reload for
	// the whole grace window.
	known := false
	for i := range e.board.Cards {
		if e.board.Cards[i].ItemID == card.ItemID {
			known = true
			break
		}
	}
	if !known {
		e.mu.Unlock()
		return
	}
	e.upsertCard(card)
	if e.inflight != nil {
		e.inflight.apply(&e.board)
	}
	for _, op := range e.pending {
		op.apply(&e.board)
	}
	e.markRecent(card.ItemID)
	for i := range e.board.Cards {
		if e.board.Cards[i].ItemID == itemID {
			e.cardChanged(clientIDFrom(ctx), e.board.Cards[i], "MODIFIED")
			break
		}
	}
	e.mu.Unlock()
}

func (b *storeBackend) CreateCard(ctx context.Context, bd board.Board, in board.CreateInput) (board.Card, error) {
	card, err := b.inner.CreateCard(ctx, bd, in)
	if err != nil {
		return card, err
	}
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	e.upsertCard(card)
	e.markRecent(card.ItemID)
	e.cardChanged(clientIDFrom(ctx), card, "ADDED")
	e.mu.Unlock()
	return card, nil
}

func (b *storeBackend) DeleteCard(ctx context.Context, bd board.Board, card board.Card) error {
	if err := b.inner.DeleteCard(ctx, bd, card); err != nil {
		return err
	}
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	e.removeCard(card.ItemID)
	e.markGone(card.ItemID)
	e.cardChanged(clientIDFrom(ctx), card, "DELETED")
	e.mu.Unlock()
	return nil
}

// MoveCard applies the new position to the cached order (so lists keep the
// board's real order) and announces the new Ordering to other clients — the
// originator already reordered optimistically. The GitHub write is queued.
func (b *storeBackend) MoveCard(ctx context.Context, bd board.Board, card board.Card, afterID string) error {
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	e.board.Cards = moveCardAfter(e.board.Cards, card.ItemID, afterID)
	e.recentMove = time.Now()
	e.orderingChanged(clientIDFrom(ctx))
	e.mu.Unlock()
	b.enqueue(ctx, e, pendingOp{
		key:    "move:" + card.ItemID,
		itemID: card.ItemID,
		desc:   "move " + cardRef(card),
		apply: func(target *board.Board) {
			target.Cards = moveCardAfter(target.Cards, card.ItemID, afterID)
		},
		exec: func(ctx context.Context) error {
			return b.inner.MoveCard(ctx, bd, card, afterID)
		},
	})
	return nil
}

// draftBodySyncer is the one-shot draft-body writer the DeltaFIFO body merge
// uses when the upstream backend supports it (ghprojects does; test fakes
// without it fall back to per-op writes).
type draftBodySyncer interface {
	SyncDraftBody(ctx context.Context, card board.Card, description string, notes []board.Note, events []board.Event) ([]board.Note, []board.Event, error)
}

// bodyMutate queues a draft card's body-affecting change (description, note,
// event line) as ONE coalescing op per card (key "body:<id>"): the cache
// changes immediately as usual, and the queued write pushes the card's FINAL
// cached body state upstream in a single write — rapid note adds, description
// edits and logged events on one card merge DeltaFIFO-style instead of racing
// several read-modify-write cycles against the same draft body.
func (b *storeBackend) bodyMutate(ctx context.Context, bd board.Board, card board.Card, desc string, fn func(c *board.Card), fallback func(ctx context.Context) error) {
	syncer, ok := b.inner.(draftBodySyncer)
	if !ok || !card.IsDraft {
		b.mutateCard(ctx, bd, card.ItemID, "", desc, fn, fallback)
		return
	}
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	itemID := card.ItemID
	exec := func(ctx context.Context) error {
		e.mu.Lock()
		var snap *board.Card
		for i := range e.board.Cards {
			if e.board.Cards[i].ItemID == itemID {
				cp := e.board.Cards[i]
				cp.Notes = append([]board.Note(nil), cp.Notes...)
				cp.Events = append([]board.Event(nil), cp.Events...)
				snap = &cp
				break
			}
		}
		e.mu.Unlock()
		if snap == nil {
			return nil // deleted meanwhile — nothing left to write
		}
		notes, events, err := syncer.SyncDraftBody(ctx, card, snap.Description, snap.Notes, snap.Events)
		if err != nil {
			return err
		}
		// The written body IS the canonical state: swap the cached log to it
		// (real draft line-index ids included) and replay the still-pending
		// deltas on top. No upstream re-read — right after a write it is
		// often stale and would make fresh notes flicker away.
		e.mu.Lock()
		for i := range e.board.Cards {
			if e.board.Cards[i].ItemID != itemID {
				continue
			}
			e.board.Cards[i].Notes = notes
			e.board.Cards[i].Events = events
			if e.inflight != nil {
				e.inflight.apply(&e.board)
			}
			for _, op := range e.pending {
				op.apply(&e.board)
			}
			e.markRecent(itemID)
			e.cardChanged("", e.board.Cards[i], "MODIFIED")
			break
		}
		e.mu.Unlock()
		return nil
	}
	b.mutateCardOp(ctx, bd, itemID, "body", desc, true, fn, exec)
}

// wbSeq makes synthetic (:wb:) ids unique: timestamps alone carry second
// precision, so rapid same-second adds would collide and the id-dedupe in the
// cache apply would silently swallow all but the first.
var wbSeq atomic.Int64

func (b *storeBackend) AppendEvent(ctx context.Context, bd board.Board, card board.Card, ev board.Event) error {
	// Synthesize a stable id for the cached copy (the real one appears on the
	// next full read); the presence guard keeps the replayed append from
	// duplicating the event once GitHub starts returning it.
	ev.ID = fmt.Sprintf("%s:wb:%s:%s:%d", card.ItemID, ev.At, ev.Kind, wbSeq.Add(1))
	b.bodyMutate(ctx, bd, card, "log a change on "+cardRef(card), func(c *board.Card) {
		for _, x := range c.Events {
			if x.Kind == ev.Kind && x.At == ev.At && x.From == ev.From && x.To == ev.To {
				return
			}
		}
		c.Events = append(c.Events, ev)
	}, func(ctx context.Context) error {
		return b.inner.AppendEvent(ctx, bd, card, ev)
	})
	return nil
}

// Notes are write-behind like the field setters, with one extra wrinkle: a
// note's real id is assigned upstream (a comment node id, or a draft body
// line index), so the cached copy carries a synthetic ":wb:" id until the
// queued write lands and the worker re-reads the card. Edits and deletes of
// a note that is still synthetic resolve it upstream by its body at exec
// time (FIFO guarantees the add was pushed first).

func (b *storeBackend) AddNote(ctx context.Context, bd board.Board, card board.Card, text string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	note := board.Note{
		ID:        fmt.Sprintf("%s:wb:%s:%d", card.ItemID, now, wbSeq.Add(1)),
		Body:      text,
		CreatedAt: now,
		Author:    board.ActorFrom(ctx),
		Source:    "draft",
	}
	if !card.IsDraft {
		note.Source = "comment"
	}
	b.bodyMutate(ctx, bd, card, "add a note on "+cardRef(card), func(c *board.Card) {
		for _, n := range c.Notes {
			if n.ID == note.ID || (n.Body == note.Body && n.Author == note.Author) {
				return
			}
		}
		c.Notes = append(c.Notes, note)
	}, func(ctx context.Context) error {
		if err := b.inner.AddNote(ctx, bd, card, text); err != nil {
			return err
		}
		// Swap the synthetic note for the real one (and its real id).
		b.touched(ctx, bd, card.ItemID)
		return nil
	})
	return nil
}

func (b *storeBackend) EditNote(ctx context.Context, bd board.Board, card board.Card, note board.Note, text string) error {
	b.bodyMutate(ctx, bd, card, "edit a note on "+cardRef(card), func(c *board.Card) {
		for i := range c.Notes {
			if c.Notes[i].ID == note.ID ||
				(c.Notes[i].Body == note.Body && c.Notes[i].Author == note.Author) {
				c.Notes[i].Body = text
				return
			}
		}
	}, func(ctx context.Context) error {
		real, err := b.resolveNote(ctx, bd, card.ItemID, note)
		if err != nil {
			return err
		}
		if err := b.inner.EditNote(ctx, bd, card, real, text); err != nil {
			return err
		}
		b.touched(ctx, bd, card.ItemID)
		return nil
	})
	return nil
}

func (b *storeBackend) DeleteNote(ctx context.Context, bd board.Board, card board.Card, note board.Note) error {
	b.bodyMutate(ctx, bd, card, "delete a note on "+cardRef(card), func(c *board.Card) {
		kept := c.Notes[:0]
		removed := false
		for _, n := range c.Notes {
			if !removed && (n.ID == note.ID || (n.Body == note.Body && n.Author == note.Author)) {
				removed = true
				continue
			}
			kept = append(kept, n)
		}
		c.Notes = kept
	}, func(ctx context.Context) error {
		real, err := b.resolveNote(ctx, bd, card.ItemID, note)
		if err != nil {
			return err
		}
		if err := b.inner.DeleteNote(ctx, bd, card, real); err != nil {
			return err
		}
		// Draft note ids are body line indexes: deleting a line shifts the
		// ones after it, so re-sync the cached ids right away.
		b.touched(ctx, bd, card.ItemID)
		return nil
	})
	return nil
}

// resolveNote maps a still-synthetic (":wb:") note reference onto the real
// upstream note by matching its body, taking the newest match. A real id
// passes through untouched.
func (b *storeBackend) resolveNote(ctx context.Context, bd board.Board, itemID string, note board.Note) (board.Note, error) {
	if !strings.Contains(note.ID, ":wb:") {
		return note, nil
	}
	cards, err := b.inner.LoadCards(ctx, bd, []string{itemID})
	if err != nil {
		return note, err
	}
	if len(cards) == 0 {
		return note, fmt.Errorf("card %s not found upstream", itemID)
	}
	found := -1
	for i, n := range cards[0].Notes {
		if n.Body == note.Body && (note.Author == "" || n.Author == "" || n.Author == note.Author) {
			found = i
		}
	}
	if found < 0 {
		return note, fmt.Errorf("the note was not found upstream")
	}
	return cards[0].Notes[found], nil
}

// The field setters below are write-behind: the cache changes (and watchers
// hear about it) immediately, the GitHub write is queued — see writequeue.go.

func (b *storeBackend) SetDescription(ctx context.Context, bd board.Board, card board.Card, description string) error {
	apply := func(c *board.Card) {
		c.Description = description
	}
	exec := func(ctx context.Context) error {
		return b.inner.SetDescription(ctx, bd, card, description)
	}
	if card.IsDraft {
		b.bodyMutate(ctx, bd, card, "edit the description of "+cardRef(card), apply, exec)
		return nil
	}
	b.mutateCard(ctx, bd, card.ItemID, "description", "edit the description of "+cardRef(card), apply, exec)
	return nil
}

func (b *storeBackend) RenameCard(ctx context.Context, bd board.Board, card board.Card, title string) error {
	b.mutateCard(ctx, bd, card.ItemID, "title", "rename "+cardRef(card), func(c *board.Card) {
		c.Title = title
	}, func(ctx context.Context) error {
		return b.inner.RenameCard(ctx, bd, card, title)
	})
	return nil
}

func (b *storeBackend) SetStage(ctx context.Context, bd board.Board, card board.Card, stage board.StageKey) error {
	b.mutateCard(ctx, bd, card.ItemID, "stage", "set the stage on "+cardRef(card), func(c *board.Card) {
		c.Stage = stage
	}, func(ctx context.Context) error {
		return b.inner.SetStage(ctx, bd, card, stage)
	})
	return nil
}

func (b *storeBackend) SetProgress(ctx context.Context, bd board.Board, card board.Card, progress int) error {
	b.mutateCard(ctx, bd, card.ItemID, "progress", "set progress on "+cardRef(card), func(c *board.Card) {
		c.Progress = progress
	}, func(ctx context.Context) error {
		return b.inner.SetProgress(ctx, bd, card, progress)
	})
	return nil
}

func (b *storeBackend) SetZone(ctx context.Context, bd board.Board, card board.Card, zone board.ZoneKey) error {
	b.mutateCard(ctx, bd, card.ItemID, "zone", "move "+cardRef(card)+" to another zone", func(c *board.Card) {
		c.Zone = zone
	}, func(ctx context.Context) error {
		return b.inner.SetZone(ctx, bd, card, zone)
	})
	return nil
}

func (b *storeBackend) SetDay(ctx context.Context, bd board.Board, card board.Card, day string) error {
	b.mutateCard(ctx, bd, card.ItemID, "day", "set the end date on "+cardRef(card), func(c *board.Card) {
		c.Day = day
	}, func(ctx context.Context) error {
		return b.inner.SetDay(ctx, bd, card, day)
	})
	return nil
}

func (b *storeBackend) SetStart(ctx context.Context, bd board.Board, card board.Card, date string) error {
	b.mutateCard(ctx, bd, card.ItemID, "start", "set the start date on "+cardRef(card), func(c *board.Card) {
		c.StartDate = date
	}, func(ctx context.Context) error {
		return b.inner.SetStart(ctx, bd, card, date)
	})
	return nil
}

func (b *storeBackend) SetSprintStart(ctx context.Context, bd board.Board, card board.Card, date string) error {
	b.mutateCard(ctx, bd, card.ItemID, "sprint-start", "move "+cardRef(card)+" to another sprint", func(c *board.Card) {
		c.SprintStart = date
	}, func(ctx context.Context) error {
		return b.inner.SetSprintStart(ctx, bd, card, date)
	})
	return nil
}

func (b *storeBackend) SetPlan(ctx context.Context, bd board.Board, card board.Card, plan board.PlanBand) error {
	b.mutateCard(ctx, bd, card.ItemID, "plan", "set the plan band on "+cardRef(card), func(c *board.Card) {
		c.Plan = plan
	}, func(ctx context.Context) error {
		return b.inner.SetPlan(ctx, bd, card, plan)
	})
	return nil
}

func (b *storeBackend) SetWeek(ctx context.Context, bd board.Board, card board.Card, week string) error {
	b.mutateCard(ctx, bd, card.ItemID, "week", "move "+cardRef(card)+" to another week", func(c *board.Card) {
		c.Week = week
	}, func(ctx context.Context) error {
		return b.inner.SetWeek(ctx, bd, card, week)
	})
	return nil
}

func (b *storeBackend) SetTeam(ctx context.Context, bd board.Board, card board.Card, team string) error {
	b.mutateCard(ctx, bd, card.ItemID, "team", "move "+cardRef(card)+" to another team", func(c *board.Card) {
		c.Team = team
	}, func(ctx context.Context) error {
		return b.inner.SetTeam(ctx, bd, card, team)
	})
	return nil
}

func (b *storeBackend) SetAssignee(ctx context.Context, bd board.Board, card board.Card, login string) error {
	b.mutateCard(ctx, bd, card.ItemID, "assignee", "reassign "+cardRef(card), func(c *board.Card) {
		if login == "" {
			c.Assignees = nil
		} else {
			c.Assignees = []string{login}
		}
	}, func(ctx context.Context) error {
		return b.inner.SetAssignee(ctx, bd, card, login)
	})
	return nil
}

func (b *storeBackend) SetParent(ctx context.Context, bd board.Board, card board.Card, parent string) error {
	b.mutateCard(ctx, bd, card.ItemID, "parent", "regroup "+cardRef(card), func(c *board.Card) {
		c.Parent = parent
	}, func(ctx context.Context) error {
		return b.inner.SetParent(ctx, bd, card, parent)
	})
	return nil
}

func (b *storeBackend) SetReviewOf(ctx context.Context, bd board.Board, card board.Card, reviewOf string) error {
	b.mutateCard(ctx, bd, card.ItemID, "review-of", "relink the review "+cardRef(card), func(c *board.Card) {
		c.ReviewOf = reviewOf
	}, func(ctx context.Context) error {
		return b.inner.SetReviewOf(ctx, bd, card, reviewOf)
	})
	return nil
}

func (b *storeBackend) SetReviewRound(ctx context.Context, bd board.Board, card board.Card, round int) error {
	b.mutateCard(ctx, bd, card.ItemID, "review-round", "bump the review round on "+cardRef(card), func(c *board.Card) {
		c.ReviewRound = round
	}, func(ctx context.Context) error {
		return b.inner.SetReviewRound(ctx, bd, card, round)
	})
	return nil
}

// ResolveIssueRef passes through to the inner backend's resolver — link
// titles are live lookups, never cached board state.
func (b *storeBackend) ResolveIssueRef(ctx context.Context, link board.Link) (board.Link, error) {
	resolver, ok := b.inner.(boardservice.LinkResolver)
	if !ok {
		return link, fmt.Errorf("backend cannot resolve github refs")
	}
	return resolver.ResolveIssueRef(ctx, link)
}

// SetSprintState updates the cached pointer in place when the team's
// sprint-state card already exists (its id is stable) and queues the GitHub
// write — this is what makes Carry Over instant. A first pointer creates a
// state card whose id the cache does not know, so that (rare) path stays
// synchronous and reloads the board.
func (b *storeBackend) SetSprintState(ctx context.Context, bd board.Board, team, current, previous string) error {
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	st, had := e.board.SprintStates[team]
	if e.loaded && had && st.ItemID != "" {
		st.Current, st.Previous = current, previous
		e.board.SprintStates[team] = st
		e.sprintChanged(clientIDFrom(ctx), team)
		e.mu.Unlock()
		label := team
		if label == "" {
			label = "no team"
		}
		b.enqueue(ctx, e, pendingOp{
			key:  "sprint:" + team,
			desc: "advance the «" + label + "» sprint",
			apply: func(target *board.Board) {
				if s, ok := target.SprintStates[team]; ok {
					s.Current, s.Previous = current, previous
					target.SprintStates[team] = s
				}
			},
			exec: func(ctx context.Context) error {
				return b.inner.SetSprintState(ctx, bd, team, current, previous)
			},
		})
		return nil
	}
	e.mu.Unlock()
	if err := b.inner.SetSprintState(ctx, bd, team, current, previous); err != nil {
		return err
	}
	e.mu.Lock()
	e.loaded = false
	e.mu.Unlock()
	if _, err := b.LoadBoard(ctx, bd.Owner, bd.Number); err == nil {
		e.mu.Lock()
		e.sprintChanged(clientIDFrom(ctx), team)
		e.mu.Unlock()
	}
	return nil
}
