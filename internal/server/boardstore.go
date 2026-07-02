package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
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
	// recentCards / recentGone guard the cache against GitHub's eventually
	// consistent item list: a card created (or deleted) through aeman seconds
	// ago may still be missing from (or present in) a fresh full load, and a
	// TTL reload right after the mutation would lose (or resurrect) it. Cards
	// touched within recentGrace are re-applied on top of every full reload.
	recentCards map[string]time.Time
	recentGone  map[string]time.Time
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
		have := map[string]bool{}
		for _, c := range fresh.Cards {
			have[c.ItemID] = true
		}
		for _, old := range e.board.Cards {
			if _, recent := e.recentCards[old.ItemID]; recent && !have[old.ItemID] {
				fresh.Cards = append(fresh.Cards, old)
			}
		}
	}
	return fresh
}

// fresh returns the cached board while it is loaded and within its TTL.
func (e *boardEntry) fresh() (board.Board, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loaded && time.Since(e.loadedAt) < boardFreshFor {
		return e.board, true
	}
	return board.Board{}, false
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

// moveCardTo reorders the cached card to sit after afterID ("" = the top), so
// snapshots keep serving the board in its real order after a move.
func (e *boardEntry) moveCardTo(itemID, afterID string) {
	idx := -1
	for i := range e.board.Cards {
		if e.board.Cards[i].ItemID == itemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	card := e.board.Cards[idx]
	rest := make([]board.Card, 0, len(e.board.Cards)-1)
	rest = append(rest, e.board.Cards[:idx]...)
	rest = append(rest, e.board.Cards[idx+1:]...)
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
	e.board.Cards = out
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
		e = &boardEntry{watchers: map[*subscription]struct{}{}}
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
}

var _ boardservice.Backend = (*storeBackend)(nil)

// LoadBoard serves the cached board while it is fresh, otherwise reloads it from
// the backend and caches it (single-flight: concurrent misses share one fetch).
// External edits surface on the next reload.
func (b *storeBackend) LoadBoard(ctx context.Context, owner string, project int) (board.Board, error) {
	e := b.store.entry(storeKey(owner, project))
	if bd, ok := e.fresh(); ok {
		return bd, nil
	}
	e.loadMu.Lock()
	defer e.loadMu.Unlock()
	// A concurrent loader may have refreshed the cache while we waited.
	if bd, ok := e.fresh(); ok {
		return bd, nil
	}
	bd, err := b.inner.LoadBoard(ctx, owner, project)
	if err != nil {
		return board.Board{}, err
	}
	e.mu.Lock()
	bd = e.applyRecent(bd)
	e.board = bd
	e.loaded = true
	e.loadedAt = time.Now()
	e.mu.Unlock()
	return bd, nil
}

// LoadCards passes straight through: it is already a partial read.
func (b *storeBackend) LoadCards(ctx context.Context, bd board.Board, ids []string) ([]board.Card, error) {
	return b.inner.LoadCards(ctx, bd, ids)
}

// touched reloads the one card a mutation changed, updates the cache and emits a
// MODIFIED event. Reload failures are swallowed: the periodic re-list reconciles.
func (b *storeBackend) touched(ctx context.Context, bd board.Board, itemID string) {
	cards, err := b.inner.LoadCards(ctx, bd, []string{itemID})
	if err != nil || len(cards) == 0 {
		return
	}
	card := cards[0]
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	e.upsertCard(card)
	e.markRecent(card.ItemID)
	e.cardChanged(clientIDFrom(ctx), card, "MODIFIED")
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
// originator already reordered optimistically.
func (b *storeBackend) MoveCard(ctx context.Context, bd board.Board, card board.Card, afterID string) error {
	if err := b.inner.MoveCard(ctx, bd, card, afterID); err != nil {
		return err
	}
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	e.moveCardTo(card.ItemID, afterID)
	e.orderingChanged(clientIDFrom(ctx))
	e.mu.Unlock()
	return nil
}

func (b *storeBackend) AddNote(ctx context.Context, bd board.Board, card board.Card, text string) error {
	if err := b.inner.AddNote(ctx, bd, card, text); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) EditNote(ctx context.Context, bd board.Board, card board.Card, note board.Note, text string) error {
	if err := b.inner.EditNote(ctx, bd, card, note, text); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) DeleteNote(ctx context.Context, bd board.Board, card board.Card, note board.Note) error {
	if err := b.inner.DeleteNote(ctx, bd, card, note); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetDescription(ctx context.Context, bd board.Board, card board.Card, description string) error {
	if err := b.inner.SetDescription(ctx, bd, card, description); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) RenameCard(ctx context.Context, bd board.Board, card board.Card, title string) error {
	if err := b.inner.RenameCard(ctx, bd, card, title); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetStage(ctx context.Context, bd board.Board, card board.Card, stage board.StageKey) error {
	if err := b.inner.SetStage(ctx, bd, card, stage); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetProgress(ctx context.Context, bd board.Board, card board.Card, progress int) error {
	if err := b.inner.SetProgress(ctx, bd, card, progress); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetZone(ctx context.Context, bd board.Board, card board.Card, zone board.ZoneKey) error {
	if err := b.inner.SetZone(ctx, bd, card, zone); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetDay(ctx context.Context, bd board.Board, card board.Card, day string) error {
	if err := b.inner.SetDay(ctx, bd, card, day); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetStart(ctx context.Context, bd board.Board, card board.Card, date string) error {
	if err := b.inner.SetStart(ctx, bd, card, date); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetSprintStart(ctx context.Context, bd board.Board, card board.Card, date string) error {
	if err := b.inner.SetSprintStart(ctx, bd, card, date); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetPlan(ctx context.Context, bd board.Board, card board.Card, plan board.PlanBand) error {
	if err := b.inner.SetPlan(ctx, bd, card, plan); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetWeek(ctx context.Context, bd board.Board, card board.Card, week string) error {
	if err := b.inner.SetWeek(ctx, bd, card, week); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetTeam(ctx context.Context, bd board.Board, card board.Card, team string) error {
	if err := b.inner.SetTeam(ctx, bd, card, team); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetAssignee(ctx context.Context, bd board.Board, card board.Card, login string) error {
	if err := b.inner.SetAssignee(ctx, bd, card, login); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) SetReviewOf(ctx context.Context, bd board.Board, card board.Card, reviewOf string) error {
	if err := b.inner.SetReviewOf(ctx, bd, card, reviewOf); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
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
// sprint-state card already exists (its id is stable), announces the Sprint
// change and re-diffs scoped memberships. A first pointer creates a state card
// whose id the cache does not know, so that path reloads the board first.
func (b *storeBackend) SetSprintState(ctx context.Context, bd board.Board, team, current, previous string) error {
	if err := b.inner.SetSprintState(ctx, bd, team, current, previous); err != nil {
		return err
	}
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	st, had := e.board.SprintStates[team]
	if e.loaded && had && st.ItemID != "" {
		st.Current, st.Previous = current, previous
		e.board.SprintStates[team] = st
		e.sprintChanged(clientIDFrom(ctx), team)
		e.mu.Unlock()
		return nil
	}
	e.loaded = false
	e.mu.Unlock()
	if _, err := b.LoadBoard(ctx, bd.Owner, bd.Number); err == nil {
		e.mu.Lock()
		e.sprintChanged(clientIDFrom(ctx), team)
		e.mu.Unlock()
	}
	return nil
}
