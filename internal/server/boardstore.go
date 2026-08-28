package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
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
	// rights is what the subscriber may read, per domain (G17): a frame from
	// a domain they cannot read never reaches the socket, and roster frames
	// carry their projection of the board. Nil = everything.
	rights *domainRights
}

// view is the board as this subscriber sees it.
func (sub *subscription) view(b board.Board) board.Board {
	if sub.rights == nil {
		return b
	}
	return board.Visible(b, sub.rights.primary, sub.rights.readable)
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

// targetItemCtxKey carries the item id the client's request ADDRESSED — the
// {uid} of a /cards/{uid} route. Echo suppression is scoped to it: the author
// holds an optimistic copy of the card they patched, and only of that card.
type targetItemCtxKey struct{}

func withTargetItem(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, targetItemCtxKey{}, id)
}

// echoOrigin is clientIDFrom for one card's change: the author's own watch is
// spared the echo only when THIS card is the one their request addressed. A
// cascade onto any other card — an epic rename fanning out, a subtask
// following its parent — has no optimistic copy on the author's side, and
// suppressing it left the author's board stale until a reload (the "renamed
// column loses its cards" bug). A request that addressed no card (a batch
// action) suppresses nothing.
func echoOrigin(ctx context.Context, itemID string) string {
	origin := clientIDFrom(ctx)
	if origin == "" {
		return ""
	}
	if target, _ := ctx.Value(targetItemCtxKey{}).(string); target != itemID {
		return ""
	}
	return origin
}

func clientIDFrom(ctx context.Context) string {
	// Server-side work on nobody's behalf (a background title resolve) must
	// reach every watcher, including the tab that triggered the create — an
	// echo-suppressed rename would sit unseen until a reload.
	if board.IsUnattributed(ctx) {
		return ""
	}
	id, _ := ctx.Value(clientIDCtxKey{}).(string)
	return id
}

// boardFreshFor bounds how long a cached board is served before a read
// questions it. The git sync's fetch tick keeps the cache current in the
// steady state, so interactive reads and mutations land on a fresh cache
// instead of re-paying the full load themselves.
const boardFreshFor = 3 * time.Minute

// boardStaleMax bounds how old a snapshot a read-only request may still be
// served while the store revalidates in the background. Past it (the first
// visit of the morning) the read blocks on a fresh load instead of flashing
// hours-old state.
const boardStaleMax = 10 * time.Minute

// boardEntry is the cached board plus its watcher set for one owner/project.
type boardEntry struct {
	mu sync.Mutex
	// member is the store's forge hook — avatar and name for a login — for
	// the roster frames this entry broadcasts.
	member func(login string) apiserver.Member
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
	// A roster entry created moments ago — a project, process, column,
	// deadline or task — outweighs a fresh read that predates it the same
	// way a recent card does: a reload that began before the create's commit
	// must not drop it. It goes back into the roster with its domain.
	if len(e.recentCards) > 0 {
		old := rosterStubs(e.board)
		for id := range e.recentCards {
			if stub, ok := old[id]; ok {
				installStub(&fresh, stub)
			}
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

// cacheState grades a cache lookup: a fresh hit is served as is, a stale one
// only to read paths that revalidate in the background, a miss always loads.
type cacheState int

const (
	cacheMiss cacheState = iota
	cacheStale
	cacheFresh
)

// cached returns the board and how usable it is: within boardFreshFor it is
// cacheFresh, past that (up to boardStaleMax) cacheStale — read paths may
// serve a stale hit while a background reload revalidates it — and beyond
// that, or before the first load, a miss. Who may SEE what is not the
// cache's business: the git backend serves one shared board and the visible
// backend projects it per visitor's domain rights.
// verify records that the cache was found current — a fetch that brought
// nothing new. The entry ages from this moment, not from the last full
// read: with a git store the sync knows whether anything changed, so time
// alone is no reason to re-read a board.
// age is how long ago the cache was last known current; zero when it holds
// no board at all.
func (e *boardEntry) age() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.loaded {
		return 0
	}
	return time.Since(e.loadedAt)
}

func (e *boardEntry) verify() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loaded {
		e.loadedAt = time.Now()
	}
}

func (e *boardEntry) cached() (board.Board, cacheState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.loaded {
		return board.Board{}, cacheMiss
	}
	age := time.Since(e.loadedAt)
	if age >= boardStaleMax {
		return board.Board{}, cacheMiss
	}
	state := cacheFresh
	if age >= boardFreshFor {
		state = cacheStale
	}
	return e.board, state
}

// cardChanged fans one card change out to the subscriptions. The caller holds
// e.mu with the cache already updated. Unscoped subscriptions get the verb as
// is; scoped ones get the membership transition (entering the scope is ADDED,
// leaving it is DELETED, staying is MODIFIED). The originating client's own
// events are suppressed — it already holds the change optimistically — but its
// membership is still tracked, or later diffs would mis-fire.
func (e *boardEntry) cardChanged(origin string, c board.Card, verb string) {
	// A turn of a process is a card AND a row of the Process tab's history:
	// its stage changing is a card event to the boards and a structure event
	// to that tab, which reads the turns from the board rather than from the
	// card stream.
	if c.Task != "" {
		e.rosterBroadcast()
	}
	res := apiserver.CardResource(e.board, c)
	for sub := range e.watchers {
		if !sub.resources["cards"] || !sub.rights.canRead(c.Domain) {
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
		for _, c := range apiserver.FilterCards(sub.view(e.board), *sub.sel) {
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
	teamDomain := e.board.Domains[st.ItemID]
	for sub := range e.watchers {
		if !sub.resources["sprints"] || (origin != "" && sub.clientID == origin) || !sub.rights.canRead(teamDomain) {
			continue
		}
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Sprint", Object: res})
	}
	e.reevaluate(origin)
}

// orderingChanged announces the board's new manual order. The caller holds
// e.mu with the cache already reordered; membership is unaffected.
func (e *boardEntry) orderingChanged(origin string) {
	for sub := range e.watchers {
		if !sub.resources["ordering"] || (origin != "" && sub.clientID == origin) {
			continue
		}
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Ordering", Object: apiserver.OrderingResource(sub.view(e.board))})
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
	// log receives board load events. Nil-safe via logger().
	log *slog.Logger
	// member is what the forge knows about a login — avatar, display name —
	// put on the board resource's members so clients assemble no forge URL
	// themselves. Nil leaves them bare (tests, no forge).
	member func(login string) apiserver.Member
}

// logger returns the store's logger, or a discard logger when none was wired
// (tests).
func (s *boardStore) logger() *slog.Logger {
	if s.log != nil {
		return s.log
	}
	return slog.New(slog.DiscardHandler)
}

func newBoardStore() *boardStore {
	return &boardStore{entries: map[string]*boardEntry{}}
}

func storeKey(boardID string) string {
	return boardID
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
			member:   s.member,
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
	return s.subscribeAs(key, clientID, sel, resources, nil)
}

// subscribeAs is subscribe for a visitor with domain rights: frames from
// domains they cannot read never reach them (G17).
func (s *boardStore) subscribeAs(key, clientID string, sel *apiserver.Selector, resources map[string]bool, rights *domainRights) (*subscription, func()) {
	e := s.entry(key)
	sub := &subscription{
		ch:        make(chan []byte, 64),
		clientID:  clientID,
		sel:       sel,
		resources: resources,
		members:   map[string]bool{},
		rights:    rights,
	}
	e.mu.Lock()
	if sel != nil && e.loaded {
		for _, c := range apiserver.FilterCards(sub.view(e.board), *sel) {
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
	// git, when set, is the sync behind a git backend: executed queue groups
	// become local commits (one per request) and a background push sends
	// them; nil for every other backend.
	git *gitSync
}

var _ boardservice.Backend = (*storeBackend)(nil)

// LoadBoard serves the cached board while it is fresh. A stale-but-recent
// board is still served instantly to read paths that opted in (staleControl in
// ctx) while a background reload revalidates it — watchers then receive the
// diff as ordinary events plus a Sync frame. Everything else (mutation reads,
// cold or too-old caches) blocks on a fresh load (single-flight: concurrent
// misses share one fetch).
func (b *storeBackend) LoadBoard(ctx context.Context, boardID string) (board.Board, error) {
	e := b.store.entry(storeKey(boardID))
	login := board.ActorFrom(ctx)
	bd, state := e.cached()
	if state == cacheFresh {
		return bd, nil
	}
	if state == cacheStale {
		if sc := staleControlFrom(ctx); sc != nil {
			b.revalidate(ctx, e, boardID)
			sc.served.Store(true)
			return bd, nil
		}
	}
	e.loadMu.Lock()
	// A concurrent loader may have refreshed the cache while we waited on
	// loadMu; a mutation that queued behind that reload must ride its result
	// instead of re-paying the full load.
	if bd, st := e.cached(); st == cacheFresh {
		e.loadMu.Unlock()
		return bd, nil
	}
	// The fetch is DETACHED from the request: a big board takes the better
	// part of a minute, the user refreshes the page, the refresh cancels the
	// request context — and with it, before this, the fetch. Every refresh
	// restarted the fetch from zero, so a refresh-happy user could keep a
	// board cold forever while every request appeared to hang. Now the fetch
	// runs on its own context, the goroutine owns loadMu until it is done
	// (waiters ride the SAME fetch), and the result lands in the cache
	// whether or not anyone is still waiting for it.
	type loaded struct {
		bd  board.Board
		err error
	}
	done := make(chan loaded, 1)
	b.store.logger().Info("board load began", "board", storeKey(boardID), "login", login)
	started := time.Now()
	go func() { //nolint:gosec // G118: the whole point — the fetch must outlive its request
		defer e.loadMu.Unlock()
		// Deliberately NOT the request context: the detachment is the fix.
		lctx, cancel := context.WithTimeout(context.Background(), detachedLoadTimeout)
		defer cancel()
		fresh, err := b.inner.LoadBoard(lctx, boardID)
		if err != nil {
			b.store.logger().Warn("board load failed", "board", storeKey(boardID),
				"dur", time.Since(started), "err", err)
			done <- loaded{err: err}
			return
		}
		b.store.logger().Info("board load done", "board", storeKey(boardID),
			"cards", len(fresh.Cards), "dur", time.Since(started))
		done <- loaded{bd: b.install(e, fresh)}
	}()
	select {
	case l := <-done:
		if l.err != nil {
			return board.Board{}, l.err
		}
		return l.bd, nil
	case <-ctx.Done():
		// The fetch carries on without us; the next request finds it warm.
		return board.Board{}, ctx.Err()
	}
}

// revalidate refreshes a stale cache in the background with the requesting
// user's token (kept past the response via WithoutCancel). Single-flight: when
// a load is already running its completion will broadcast, so this one backs
// off instead of stacking a second upstream fetch.
func (b *storeBackend) revalidate(ctx context.Context, e *boardEntry, boardID string) {
	if !e.loadMu.TryLock() {
		return
	}
	bctx := context.WithoutCancel(ctx)
	go func() {
		defer e.loadMu.Unlock()
		fresh, err := b.inner.LoadBoard(bctx, boardID)
		if err != nil {
			// The stale snapshot stays served; tell clients to drop their
			// revalidation hold and let a later read retry.
			e.mu.Lock()
			e.syncBroadcast()
			e.mu.Unlock()
			return
		}
		b.install(e, fresh)
	}()
}

// detachedLoadTimeout bounds one detached full load: it runs on its own
// context, not the request's, and holds loadMu for the duration.
const detachedLoadTimeout = 5 * time.Minute

// install replaces the cache with a freshly loaded board, fans the diff
// against the previous snapshot out to watchers as ordinary events, and
// closes with a Sync frame. Returns the installed board (with recent local
// mutations re-applied).
func (b *storeBackend) install(e *boardEntry, fresh board.Board) board.Board {
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
// rosterBroadcast tells every watcher that the board's STRUCTURE changed —
// its projects, epic columns or deadlines. Those live in the board metadata
// rather than in any card, so a client cannot learn about them from the card
// events it is already receiving: it has to re-read /board, and this is what
// tells it to. Sent to every watcher regardless of the view it selected,
// because the roster is the same for all of them.
func (e *boardEntry) rosterBroadcast() {
	// The frame CARRIES the board, the way a Card frame carries its card: a
	// client applies it and needs no round trip. A bare "something changed"
	// signal sent every open tab back to GET /board — a full snapshot each,
	// which is the opposite of what a cache is for. Processes ride along as
	// their full structure, since the Process tab is drawn from it.
	for sub := range e.watchers {
		view := sub.view(e.board)
		sub.send(watchFrame{Type: "MODIFIED", Kind: "Board", Object: boardFrame{
			BoardInfo: apiserver.BoardResourceWithPeople(view, e.member),
			Processes: apiserver.ProcessesResource(view, "").Items,
		}})
	}
}

// boardFrame is the Board watch frame's object: the board resource plus the
// process structure, so one frame repaints every roster-driven view.
type boardFrame struct {
	apiserver.BoardInfo
	Processes []apiserver.Process `json:"processes"`
}

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
	e := b.store.entry(storeKey(bd.Board))
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
			e.cardChanged(echoOrigin(ctx, e.board.Cards[i].ItemID), e.board.Cards[i], "MODIFIED")
			break
		}
	}
	e.mu.Unlock()
}

// CreateCard answers from the cache: the store mints the card's final id,
// installs it at once and the write rides the write-behind queue like any
// other change (createMinted). State cards (sprint pointers) stay
// synchronous: they anchor whole teams, are rare, and their callers read
// fields off the answer immediately.
func (b *storeBackend) CreateCard(ctx context.Context, bd board.Board, in board.CreateInput) (board.Card, error) {
	e := b.store.entry(storeKey(bd.Board))
	if in.Title == board.SprintStateTitle {
		return b.createSync(ctx, bd, e, in)
	}
	return b.createMinted(ctx, bd, e, in)
}

// createSync is the synchronous create path, kept for the sprint-state cards.
func (b *storeBackend) createSync(ctx context.Context, bd board.Board, e *boardEntry, in board.CreateInput) (board.Card, error) {
	card, err := b.inner.CreateCard(ctx, bd, in)
	if err != nil {
		return card, err
	}
	e.mu.Lock()
	if card.Title == board.ProcessStateTitle {
		if _, exists := board.FindProcess(e.board, card.Process); card.Process != "" && !exists {
			e.board.Processes = append(e.board.Processes,
				board.Process{Name: card.Process, Project: card.Project, ItemID: card.ItemID})
		}
		e.markRecent(card.ItemID)
		e.rosterBroadcast()
		e.mu.Unlock()
		return card, nil
	}
	if card.Title == board.ProcessTaskTitle {
		e.board.Tasks = append(e.board.Tasks, card)
		e.markRecent(card.ItemID)
		e.rosterBroadcast()
		e.mu.Unlock()
		return card, nil
	}
	if card.Title == board.DeadlineStateTitle {
		// A deadline is a line on a week, not a card row.
		if _, exists := board.FindDeadline(e.board, card.Project, card.Week); card.Week != "" && !exists {
			e.board.Deadlines = append(e.board.Deadlines,
				board.Deadline{Week: card.Week, Project: card.Project, ItemID: card.ItemID})
		}
		e.markRecent(card.ItemID)
		e.rosterBroadcast()
		e.mu.Unlock()
		return card, nil
	}
	if card.Title == board.ProjectStateTitle {
		// A project-state card is a roster entry, not a card row: surface it
		// as the board's project list (clients re-read /board).
		if card.Project != "" && e.board.ProjectStates[card.Project] == "" {
			if e.board.ProjectStates == nil {
				e.board.ProjectStates = map[string]string{}
			}
			e.board.Projects = append(e.board.Projects, card.Project)
			e.board.ProjectStates[card.Project] = card.ItemID
		}
		e.markRecent(card.ItemID)
		e.rosterBroadcast()
		e.mu.Unlock()
		return card, nil
	}
	if card.Title == board.EpicStateTitle {
		// A state card is a Project-board column, not a card: surface it as
		// the board's column roster (clients re-read /board), never as a row.
		if _, exists := board.FindEpic(e.board, card.Project, card.Epic); card.Epic != "" && !exists {
			e.board.Epics = append(e.board.Epics, board.EpicCol{
				Name: card.Epic, Project: card.Project, ItemID: card.ItemID,
			})
		}
		e.markRecent(card.ItemID)
		e.rosterBroadcast()
		e.mu.Unlock()
		return card, nil
	}
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
	e := b.store.entry(storeKey(bd.Board))
	e.mu.Lock()
	e.removeCard(card.ItemID)
	e.markGone(card.ItemID)
	// A deleted sprint-state card takes its team with it: drop the cached
	// pointer and the team's order slot so /board reads and watchers see the
	// team gone immediately, not after the next revalidation.
	for team, st := range e.board.SprintStates {
		if st.ItemID != card.ItemID {
			continue
		}
		delete(e.board.SprintStates, team)
		e.board.TeamOrder = removeString(e.board.TeamOrder, team)
		e.sprintChanged(clientIDFrom(ctx), team)
		break
	}
	// Likewise a deleted epic-state card takes its column with it.
	for i, col := range e.board.Epics {
		if col.ItemID != card.ItemID {
			continue
		}
		e.board.Epics = append(e.board.Epics[:i:i], e.board.Epics[i+1:]...)
		e.rosterBroadcast()
		break
	}
	// ...a deleted process or task leaves its roster...
	for i, p := range e.board.Processes {
		if p.ItemID != card.ItemID {
			continue
		}
		e.board.Processes = append(e.board.Processes[:i:i], e.board.Processes[i+1:]...)
		e.rosterBroadcast()
		break
	}
	for i, t := range e.board.Tasks {
		if t.ItemID != card.ItemID {
			continue
		}
		e.board.Tasks = append(e.board.Tasks[:i:i], e.board.Tasks[i+1:]...)
		e.rosterBroadcast()
		break
	}
	// ...a deleted deadline card takes its line off the grid...
	for i, d := range e.board.Deadlines {
		if d.ItemID != card.ItemID {
			continue
		}
		e.board.Deadlines = append(e.board.Deadlines[:i:i], e.board.Deadlines[i+1:]...)
		e.rosterBroadcast()
		break
	}
	// ...and a deleted project-state card takes its chip.
	for project, id := range e.board.ProjectStates {
		if id != card.ItemID {
			continue
		}
		delete(e.board.ProjectStates, project)
		e.board.Projects = removeString(e.board.Projects, project)
		e.rosterBroadcast()
		break
	}
	e.cardChanged(echoOrigin(ctx, card.ItemID), card, "DELETED")
	e.mu.Unlock()
	return nil
}

// renameInList swaps one entry for another, keeping its position.
func renameInList(list []string, from, to string) []string {
	out := append([]string(nil), list...)
	for i, v := range out {
		if v == from {
			out[i] = to
			return out
		}
	}
	return out
}

// removeString drops the first occurrence of v from list, in place.
func removeString(list []string, v string) []string {
	for i, s := range list {
		if s == v {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// teamOfSprintState finds the team whose sprint-state card has the item id.
func teamOfSprintState(states map[string]board.SprintState, itemID string) (string, bool) {
	for team, st := range states {
		if st.ItemID == itemID {
			return team, true
		}
	}
	return "", false
}

// moveTeamAfter re-slots team in the order list after the team owning the
// afterID sprint-state card ("" or unknown = to the front).
func moveTeamAfter(order []string, states map[string]board.SprintState, team, afterID string) []string {
	ids := make(map[string]string, len(states))
	for t, st := range states {
		ids[t] = st.ItemID
	}
	return moveNameAfter(order, ids, team, afterID)
}

// epicIndexOf finds a column by its state card's item id (-1 = not a column).
func epicIndexOf(cols []board.EpicCol, itemID string) int {
	for i, c := range cols {
		if c.ItemID == itemID {
			return i
		}
	}
	return -1
}

// moveEpicAfter re-slots the column at index from so it follows the column
// whose state card is afterID ("" or unknown = to the front).
func moveEpicAfter(cols []board.EpicCol, from int, afterID string) []board.EpicCol {
	col := cols[from]
	rest := append(append([]board.EpicCol(nil), cols[:from]...), cols[from+1:]...)
	at := 0
	if i := epicIndexOf(rest, afterID); i >= 0 {
		at = i + 1
	}
	out := make([]board.EpicCol, 0, len(rest)+1)
	out = append(out, rest[:at]...)
	out = append(out, col)
	out = append(out, rest[at:]...)
	return out
}

// nameOfState finds the roster entry whose state card has the item id.
// teamOfState is the team whose sprint pointer is the given state card.
func teamOfState(states map[string]board.SprintState, itemID string) (string, bool) {
	for name, st := range states {
		if st.ItemID == itemID {
			return name, true
		}
	}
	return "", false
}

func nameOfState(ids map[string]string, itemID string) (string, bool) {
	for name, id := range ids {
		if id == itemID {
			return name, true
		}
	}
	return "", false
}

// moveNameAfter re-slots name in an ordered roster after the entry owning the
// afterID state card ("" or unknown = to the front). Teams, epic columns and
// projects all order themselves by their state card's board position, so they
// all reorder through here.
func moveNameAfter(order []string, ids map[string]string, name, afterID string) []string {
	order = removeString(append([]string(nil), order...), name)
	at := 0
	if after, ok := nameOfState(ids, afterID); ok {
		for i, n := range order {
			if n == after {
				at = i + 1
				break
			}
		}
	}
	out := make([]string, 0, len(order)+1)
	out = append(out, order[:at]...)
	out = append(out, name)
	out = append(out, order[at:]...)
	return out
}

// MoveCard applies the new position to the cached order (so lists keep the
// board's real order) and announces the new Ordering to other clients — the
// originator already reordered optimistically. The GitHub write is queued.
func (b *storeBackend) MoveCard(ctx context.Context, bd board.Board, card board.Card, afterID string) error {
	e := b.store.entry(storeKey(bd.Board))
	e.mu.Lock()
	e.board.Cards = moveCardAfter(e.board.Cards, card.ItemID, afterID)
	// Moving a sprint-state card is a TEAM reorder: mirror it onto the cached
	// TeamOrder so /board reads agree with the new order immediately.
	if team, ok := teamOfSprintState(e.board.SprintStates, card.ItemID); ok {
		e.board.TeamOrder = moveTeamAfter(e.board.TeamOrder, e.board.SprintStates, team, afterID)
	}
	// The same for the two Project-board rosters: their order IS the board
	// position of their state cards, so a reorder must land in the cache or
	// /board keeps serving the old order until the next revalidation.
	if from := epicIndexOf(e.board.Epics, card.ItemID); from >= 0 {
		e.board.Epics = moveEpicAfter(e.board.Epics, from, afterID)
		e.rosterBroadcast()
	}
	if project, ok := nameOfState(e.board.ProjectStates, card.ItemID); ok {
		e.board.Projects = moveNameAfter(e.board.Projects, e.board.ProjectStates, project, afterID)
		e.rosterBroadcast()
	}
	// …and for the Process tab's two: processes and their tasks are state
	// cards whose board position is the order the tab draws.
	if from := processIndexOf(e.board.Processes, card.ItemID); from >= 0 {
		e.board.Processes = moveProcessAfter(e.board.Processes, from, afterID)
		e.rosterBroadcast()
	}
	if from := taskIndexOf(e.board.Tasks, card.ItemID); from >= 0 {
		e.board.Tasks = moveTaskAfter(e.board.Tasks, from, afterID)
		e.rosterBroadcast()
	}
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

// bodyMutate queues a card's body-affecting change (description, note, event
// line): the cache changes immediately as usual and the write rides the
// queue like any field write — each is one file edit in the request's
// commit, so nothing needs to merge.
func (b *storeBackend) bodyMutate(ctx context.Context, bd board.Board, card board.Card, desc string, fn func(c *board.Card), write func(ctx context.Context) error) {
	b.mutateCard(ctx, bd, card.ItemID, "", desc, fn, write)
}

// wbSeq makes synthetic (:wb:) ids unique: timestamps alone carry second
// precision, so rapid same-second adds would collide and the id-dedupe in the
// cache apply would silently swallow all but the first.
var wbSeq atomic.Int64

// AppendEvent changes nothing in the cache — the history is read from the
// store — but rides the queue like every write, so the event joins the same
// commit as the change it describes.
func (b *storeBackend) AppendEvent(ctx context.Context, bd board.Board, card board.Card, ev board.Event) error {
	b.bodyMutate(ctx, bd, card, "log a change on "+cardRef(card), func(*board.Card) {}, func(ctx context.Context) error {
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
	b.bodyMutate(ctx, bd, card, "edit the description of "+cardRef(card), apply, exec)
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
		// The storage's rule, mirrored so the cache answers as the commit
		// will: reaching 100 remembers where the card came from and the day
		// it got there (the personal view and its reseed read that day);
		// dropping below forgets both.
		switch {
		case progress >= 100 && c.Progress < 100:
			c.DoneFrom = c.Progress
			c.DoneAt = board.TodayIso()
		case progress < 100:
			c.DoneFrom = 0
			c.DoneAt = ""
		}
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

func (b *storeBackend) SetLeftAt(ctx context.Context, bd board.Board, card board.Card, day string) error {
	summary := "leave " + cardRef(card) + " behind"
	if day == "" {
		summary = "bring " + cardRef(card) + " back"
	}
	b.mutateCard(ctx, bd, card.ItemID, "left", summary, func(c *board.Card) {
		c.LeftAt = day
	}, func(ctx context.Context) error {
		return b.inner.SetLeftAt(ctx, bd, card, day)
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
	if card.Title == board.DeadlineStateTitle {
		// Dragging the line: its card is not in the cached card list, so the
		// cached deadline set is what has to follow.
		e := b.store.entry(storeKey(bd.Board))
		e.mu.Lock()
		for i := range e.board.Deadlines {
			if e.board.Deadlines[i].ItemID == card.ItemID {
				e.board.Deadlines[i].Week = week
				e.rosterBroadcast()
				break
			}
		}
		e.mu.Unlock()
		b.enqueue(ctx, e, pendingOp{
			key:    "week:" + card.ItemID,
			itemID: card.ItemID,
			desc:   "move the deadline to " + week,
			apply: func(fresh *board.Board) {
				for i := range fresh.Deadlines {
					if fresh.Deadlines[i].ItemID == card.ItemID {
						fresh.Deadlines[i].Week = week
						return
					}
				}
			},
			exec: func(ctx context.Context) error {
				return b.inner.SetWeek(ctx, bd, card, week)
			},
		})
		return nil
	}
	b.mutateCard(ctx, bd, card.ItemID, "week", "move "+cardRef(card)+" to another week", func(c *board.Card) {
		c.Week = week
	}, func(ctx context.Context) error {
		return b.inner.SetWeek(ctx, bd, card, week)
	})
	return nil
}

func (b *storeBackend) SetTeam(ctx context.Context, bd board.Board, card board.Card, team string) error {
	if card.Title == board.SprintStateTitle {
		// The team's own stub is not a card the cache holds: its name is the
		// key of the sprint pointer and an entry of the team order, so the
		// rename re-keys both in place — the next read says the new name
		// before the queue has written a byte.
		rekey := func(bd *board.Board) {
			old, ok := teamOfState(bd.SprintStates, card.ItemID)
			if !ok {
				return
			}
			st := bd.SprintStates[old]
			delete(bd.SprintStates, old)
			bd.SprintStates[team] = st
			bd.TeamOrder = renameInList(bd.TeamOrder, old, team)
		}
		e := b.store.entry(storeKey(bd.Board))
		e.mu.Lock()
		rekey(&e.board)
		e.rosterBroadcast()
		e.mu.Unlock()
		b.enqueue(ctx, e, pendingOp{
			key:    "team:" + card.ItemID,
			itemID: card.ItemID,
			desc:   "rename the team " + card.Team,
			apply:  rekey,
			exec: func(ctx context.Context) error {
				return b.inner.SetTeam(ctx, bd, card, team)
			},
		})
		return nil
	}
	b.mutateCard(ctx, bd, card.ItemID, "team", "move "+cardRef(card)+" to another team", func(c *board.Card) {
		c.Team = team
	}, func(ctx context.Context) error {
		return b.inner.SetTeam(ctx, bd, card, team)
	})
	return nil
}

func (b *storeBackend) SetEpic(ctx context.Context, bd board.Board, card board.Card, epic string) error {
	if card.Title == board.EpicStateTitle {
		// Renaming the column itself: the state card is not in the cached card
		// list (NewBoard splits it out), so mutateCard cannot reach it.
		e := b.store.entry(storeKey(bd.Board))
		e.mu.Lock()
		if i := epicIndexOf(e.board.Epics, card.ItemID); i >= 0 {
			e.board.Epics[i].Name = epic
			e.rosterBroadcast()
		}
		e.mu.Unlock()
		b.enqueue(ctx, e, pendingOp{
			key:    "epic:" + card.ItemID,
			itemID: card.ItemID,
			desc:   "rename the epic " + card.Epic,
			apply: func(fresh *board.Board) {
				if i := epicIndexOf(fresh.Epics, card.ItemID); i >= 0 {
					fresh.Epics[i].Name = epic
				}
			},
			exec: func(ctx context.Context) error {
				return b.inner.SetEpic(ctx, bd, card, epic)
			},
		})
		return nil
	}
	b.mutateCard(ctx, bd, card.ItemID, "epic", "file "+cardRef(card)+" under an epic", func(c *board.Card) {
		c.Epic = epic
	}, func(ctx context.Context) error {
		return b.inner.SetEpic(ctx, bd, card, epic)
	})
	return nil
}

// SetProject rebinds an epic column to a project. The target is a hidden
// state card, which is NOT in the cached card list (NewBoard splits it out),
// so mutateCard cannot reach it: update the roster map directly and queue the
// write like any other mutation.
func (b *storeBackend) SetProject(ctx context.Context, bd board.Board, card board.Card, project string) error {
	e := b.store.entry(storeKey(bd.Board))
	e.mu.Lock()
	if i := epicIndexOf(e.board.Epics, card.ItemID); i >= 0 {
		e.board.Epics[i].Project = project
		e.rosterBroadcast()
	} else if i := processIndexOf(e.board.Processes, card.ItemID); i >= 0 {
		// A process moved to another project: it is a roster entry, not a row.
		e.board.Processes[i].Project = project
		e.rosterBroadcast()
	} else if old, ok := nameOfState(e.board.ProjectStates, card.ItemID); ok {
		// A project-state card renamed: re-key the roster in place.
		e.board.Projects = renameInList(e.board.Projects, old, project)
		delete(e.board.ProjectStates, old)
		e.board.ProjectStates[project] = card.ItemID
		e.rosterBroadcast()
	} else {
		// An ordinary card: the project is half of the column it is filed
		// under, so it lives on the card row like any other field.
		for i := range e.board.Cards {
			if e.board.Cards[i].ItemID == card.ItemID {
				e.board.Cards[i].Project = project
				e.cardChanged(echoOrigin(ctx, e.board.Cards[i].ItemID), e.board.Cards[i], "MODIFIED")
				break
			}
		}
	}
	e.mu.Unlock()
	b.enqueue(ctx, e, pendingOp{
		key:    "project:" + card.ItemID,
		itemID: card.ItemID,
		desc:   "file " + cardRef(card) + " under a project",
		apply: func(fresh *board.Board) {
			if i := epicIndexOf(fresh.Epics, card.ItemID); i >= 0 {
				fresh.Epics[i].Project = project
				return
			}
			if i := processIndexOf(fresh.Processes, card.ItemID); i >= 0 {
				fresh.Processes[i].Project = project
				return
			}
			if old, ok := nameOfState(fresh.ProjectStates, card.ItemID); ok {
				fresh.Projects = renameInList(fresh.Projects, old, project)
				delete(fresh.ProjectStates, old)
				fresh.ProjectStates[project] = card.ItemID
				return
			}
			for i := range fresh.Cards {
				if fresh.Cards[i].ItemID == card.ItemID {
					fresh.Cards[i].Project = project
					return
				}
			}
		},
		exec: func(ctx context.Context) error {
			return b.inner.SetProject(ctx, bd, card, project)
		},
	})
	return nil
}

// A roster write is applied to the cache at once and to GitHub behind. If a
// full reload lands while it is still queued, every pending op is replayed
// over the fresh board — so each of these carries a replay that re-imposes
// its own change. Without one the reload put the old name (or the old pause)
// back in the cache and broadcast it to every tab, and only the NEXT reload
// undid that.

// moveProcessAfter and moveTaskAfter reorder the two Process-tab rosters the
// way moveEpicAfter does the columns: take the entry out, put it back after
// the anchor ("" = the front).
func moveProcessAfter(list []board.Process, from int, afterID string) []board.Process {
	entry := list[from]
	out := append(append([]board.Process{}, list[:from]...), list[from+1:]...)
	at := 0
	if afterID != "" {
		for i, p := range out {
			if p.ItemID == afterID {
				at = i + 1
				break
			}
		}
	}
	return append(out[:at:at], append([]board.Process{entry}, out[at:]...)...)
}

func moveTaskAfter(list []board.Card, from int, afterID string) []board.Card {
	entry := list[from]
	out := append(append([]board.Card{}, list[:from]...), list[from+1:]...)
	at := 0
	if afterID != "" {
		for i, t := range out {
			if t.ItemID == afterID {
				at = i + 1
				break
			}
		}
	}
	return append(out[:at:at], append([]board.Card{entry}, out[at:]...)...)
}

// taskIndexOf finds a task in the roster by its item id.
func taskIndexOf(list []board.Card, itemID string) int {
	for i := range list {
		if list[i].ItemID == itemID {
			return i
		}
	}
	return -1
}

// processIndexOf finds a process in the roster by the item id of the card
// that declares it.
func processIndexOf(list []board.Process, itemID string) int {
	for i := range list {
		if list[i].ItemID == itemID {
			return i
		}
	}
	return -1
}

// SetProcess renames a process on its state card, or re-points a task at
// a renamed process; neither is a card row, so the rosters are updated here.
func (b *storeBackend) SetProcess(ctx context.Context, bd board.Board, card board.Card, process string) error {
	e := b.store.entry(storeKey(bd.Board))
	e.mu.Lock()
	for i := range e.board.Processes {
		if e.board.Processes[i].ItemID == card.ItemID {
			e.board.Processes[i].Name = process
		}
	}
	for i := range e.board.Tasks {
		if e.board.Tasks[i].ItemID == card.ItemID {
			e.board.Tasks[i].Process = process
		}
	}
	e.rosterBroadcast()
	e.mu.Unlock()
	b.enqueue(ctx, e, pendingOp{
		key: "process:" + card.ItemID, itemID: card.ItemID,
		desc: "rename the process on " + cardRef(card),
		apply: func(fresh *board.Board) {
			for i := range fresh.Processes {
				if fresh.Processes[i].ItemID == card.ItemID {
					fresh.Processes[i].Name = process
				}
			}
			for i := range fresh.Tasks {
				if fresh.Tasks[i].ItemID == card.ItemID {
					fresh.Tasks[i].Process = process
				}
			}
		},
		exec: func(ctx context.Context) error { return b.inner.SetProcess(ctx, bd, card, process) },
	})
	return nil
}

func (b *storeBackend) SetTask(ctx context.Context, bd board.Board, card board.Card, task string) error {
	b.mutateCard(ctx, bd, card.ItemID, "task", "link "+cardRef(card)+" to its task", func(c *board.Card) {
		c.Task = task
	}, func(ctx context.Context) error {
		return b.inner.SetTask(ctx, bd, card, task)
	})
	return nil
}

// SetPaused pauses a process: a roster entry, not a card row.
func (b *storeBackend) SetPaused(ctx context.Context, bd board.Board, card board.Card, paused bool) error {
	e := b.store.entry(storeKey(bd.Board))
	e.mu.Lock()
	if i := processIndexOf(e.board.Processes, card.ItemID); i >= 0 {
		e.board.Processes[i].Paused = paused
		e.rosterBroadcast()
	}
	e.mu.Unlock()
	b.enqueue(ctx, e, pendingOp{
		key: "paused:" + card.ItemID, itemID: card.ItemID,
		desc: "pause or resume " + cardRef(card),
		apply: func(fresh *board.Board) {
			if i := processIndexOf(fresh.Processes, card.ItemID); i >= 0 {
				fresh.Processes[i].Paused = paused
			}
		},
		exec: func(ctx context.Context) error { return b.inner.SetPaused(ctx, bd, card, paused) },
	})
	return nil
}

func (b *storeBackend) SetAccumulate(ctx context.Context, bd board.Board, card board.Card, on bool) error {
	b.mutateCard(ctx, bd, card.ItemID, "accumulate", "set accumulate on "+cardRef(card), func(c *board.Card) {
		c.Accumulate = on
	}, func(ctx context.Context) error {
		return b.inner.SetAccumulate(ctx, bd, card, on)
	})
	return nil
}

func (b *storeBackend) SetRecurrence(ctx context.Context, bd board.Board, card board.Card, cycle string) error {
	b.mutateCard(ctx, bd, card.ItemID, "recurrence", "set the recurrence of "+cardRef(card), func(c *board.Card) {
		c.Recurrence = cycle
	}, func(ctx context.Context) error {
		return b.inner.SetRecurrence(ctx, bd, card, cycle)
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
	if b.git != nil && b.git.links != nil {
		return b.git.links.ResolveIssueRef(ctx, link)
	}
	resolver, ok := b.inner.(boardservice.LinkResolver)
	if !ok {
		return link, fmt.Errorf("%w: the backend keeps no forge credential", errUnresolvedLink)
	}
	return resolver.ResolveIssueRef(ctx, link)
}

// SetSprintState updates the cached pointer in place when the team's
// sprint-state card already exists (its id is stable) and queues the GitHub
// write — this is what makes Carry Over instant. A first pointer creates a
// state card whose id the cache does not know, so that (rare) path stays
// synchronous and reloads the board.
func (b *storeBackend) SetSprintState(ctx context.Context, bd board.Board, team, current, previous string) error {
	e := b.store.entry(storeKey(bd.Board))
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
				// The queue can drain long after enqueue: resolve the team's
				// sprint-state card AT WRITE TIME from the live cache, not
				// from the enqueue-era snapshot — a stale snapshot pointing
				// at a missing card is how duplicate sprint-state cards were
				// born (and how pointer writes scattered between them).
				ref := bd
				e.mu.Lock()
				ref.SprintStates = map[string]board.SprintState{}
				if live, ok := e.board.SprintStates[team]; ok {
					ref.SprintStates[team] = live
				}
				e.mu.Unlock()
				return b.inner.SetSprintState(ctx, ref, team, current, previous)
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
	if _, err := b.LoadBoard(ctx, bd.Board); err == nil {
		e.mu.Lock()
		e.sprintChanged(clientIDFrom(ctx), team)
		e.mu.Unlock()
	}
	return nil
}

// cardFromInput shapes the cached card exactly the way the store's backend
// echoes a real create, so nothing downstream can tell them apart.
func cardFromInput(in board.CreateInput, itemID string) board.Card {
	c := board.Card{
		ItemID:      itemID,
		Title:       in.Title,
		Zone:        in.Zone,
		Day:         in.Day,
		StartDate:   in.Start,
		SprintStart: in.SprintStart,
		Team:        in.Team,
		Plan:        in.Plan,
		Week:        in.Week,
		Epic:        in.Epic,
		Project:     in.Project,
		Process:     in.Process,
		Task:        in.Task,
		Recurrence:  in.Recurrence,
		Paused:      in.Paused,
		Description: in.Body,
		ReviewOf:    in.ReviewOf,
		Parent:      in.Parent,
		Domain:      in.Domain,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Assignees:   []string{},
	}
	if in.Assignee != "" {
		c.Assignees = []string{in.Assignee}
	}
	return c
}

// installCreated puts a freshly minted card into the cache the way the
// synchronous create does — rosters for state cards, a row otherwise — and
// broadcasts it.
func (b *storeBackend) installCreated(ctx context.Context, e *boardEntry, card board.Card) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if installStub(&e.board, card) {
		e.markRecent(card.ItemID)
		e.rosterBroadcast()
		return
	}
	e.upsertCard(card)
	e.markRecent(card.ItemID)
	e.cardChanged(clientIDFrom(ctx), card, "ADDED")
}

// rosterStubs is every roster entry of a board as the stub card that declared
// it, by id — what installStub takes to put one back.
func rosterStubs(b board.Board) map[string]board.Card {
	out := map[string]board.Card{}
	for name, id := range b.ProjectStates {
		out[id] = board.Card{ItemID: id, Title: board.ProjectStateTitle, Project: name, Domain: b.Domains[id]}
	}
	for _, p := range b.Processes {
		out[p.ItemID] = board.Card{ItemID: p.ItemID, Title: board.ProcessStateTitle, Process: p.Name, Project: p.Project, Paused: p.Paused, Domain: b.Domains[p.ItemID]}
	}
	for _, e := range b.Epics {
		out[e.ItemID] = board.Card{ItemID: e.ItemID, Title: board.EpicStateTitle, Epic: e.Name, Project: e.Project, Domain: b.Domains[e.ItemID]}
	}
	for _, d := range b.Deadlines {
		out[d.ItemID] = board.Card{ItemID: d.ItemID, Title: board.DeadlineStateTitle, Week: d.Week, Project: d.Project, Domain: b.Domains[d.ItemID]}
	}
	for _, k := range b.Tasks {
		c := k
		c.Domain = b.Domains[k.ItemID]
		out[k.ItemID] = c
	}
	return out
}

// installStub puts a roster stub — a process, task, project, column or
// deadline state card — into the board's roster, idempotently, and records
// its domain when it carries one. It reports false for a plain card, which
// is a row and not the roster's business. A queued create's apply uses it
// too, so a reload that lands while the create is still pending puts the
// roster entry back where it belongs instead of leaking a stub row.
func installStub(target *board.Board, card board.Card) bool {
	switch card.Title {
	case board.ProcessStateTitle:
		if _, exists := board.FindProcess(*target, card.Process); card.Process != "" && !exists {
			target.Processes = append(target.Processes,
				board.Process{Name: card.Process, Project: card.Project, ItemID: card.ItemID})
		}
	case board.ProcessTaskTitle:
		exists := false
		for _, t := range target.Tasks {
			if t.ItemID == card.ItemID {
				exists = true
				break
			}
		}
		if !exists {
			target.Tasks = append(target.Tasks, card)
		}
	case board.ProjectStateTitle:
		if card.Project != "" && target.ProjectStates[card.Project] == "" {
			if target.ProjectStates == nil {
				target.ProjectStates = map[string]string{}
			}
			target.Projects = append(target.Projects, card.Project)
			target.ProjectStates[card.Project] = card.ItemID
		}
	case board.EpicStateTitle:
		if _, exists := board.FindEpic(*target, card.Project, card.Epic); card.Epic != "" && !exists {
			target.Epics = append(target.Epics, board.EpicCol{
				Name: card.Epic, Project: card.Project, ItemID: card.ItemID,
			})
		}
	case board.DeadlineStateTitle:
		if _, exists := board.FindDeadline(*target, card.Project, card.Week); card.Week != "" && !exists {
			target.Deadlines = append(target.Deadlines,
				board.Deadline{Week: card.Week, Project: card.Project, ItemID: card.ItemID})
		}
	default:
		return false
	}
	if card.Domain != "" {
		if target.Domains == nil {
			target.Domains = map[string]string{}
		}
		target.Domains[card.ItemID] = card.Domain
	}
	return true
}
