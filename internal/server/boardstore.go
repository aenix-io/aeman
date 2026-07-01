package server

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/aenix-org/aeman/internal/board"
	"github.com/aenix-org/aeman/internal/boardservice"
)

// changeType classifies a watch event, mirroring the Kubernetes watch verbs.
type changeType string

const (
	changeAdded    changeType = "ADDED"
	changeModified changeType = "MODIFIED"
	changeDeleted  changeType = "DELETED"
	// changeReload asks watchers to re-list the whole board: a sprint pointer
	// moved, so board-wide filtering may have shifted in ways a single-card patch
	// can't express.
	changeReload changeType = "RELOAD"
)

// changeEvent is one board change delivered to watchers over the watch stream.
type changeEvent struct {
	Type changeType  `json:"type"`
	Card *board.Card `json:"card,omitempty"`
}

// boardFreshFor bounds how long a cached board is served before a read reloads
// it from the backend, so edits made outside aeman eventually surface.
const boardFreshFor = 30 * time.Second

// boardEntry is the cached board plus its watcher set for one owner/project.
type boardEntry struct {
	mu       sync.Mutex
	board    board.Board
	loadedAt time.Time
	loaded   bool
	watchers map[chan changeEvent]struct{}
}

// notify fans an event out to every watcher. The caller holds e.mu; sends are
// non-blocking, so a slow watcher drops this event and reconciles on its next
// periodic re-list.
func (e *boardEntry) notify(ev changeEvent) {
	for ch := range e.watchers {
		select {
		case ch <- ev:
		default:
		}
	}
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
		e = &boardEntry{watchers: map[chan changeEvent]struct{}{}}
		s.entries[key] = e
	}
	return e
}

// subscribe registers a watcher on a board and returns it with an unsubscribe.
func (s *boardStore) subscribe(key string) (<-chan changeEvent, func()) {
	e := s.entry(key)
	ch := make(chan changeEvent, 64)
	e.mu.Lock()
	e.watchers[ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.watchers, ch)
		e.mu.Unlock()
	}
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
// the backend and caches it. External edits surface on the next reload.
func (b *storeBackend) LoadBoard(ctx context.Context, owner string, project int) (board.Board, error) {
	e := b.store.entry(storeKey(owner, project))
	e.mu.Lock()
	if e.loaded && time.Since(e.loadedAt) < boardFreshFor {
		cached := e.board
		e.mu.Unlock()
		return cached, nil
	}
	e.mu.Unlock()

	bd, err := b.inner.LoadBoard(ctx, owner, project)
	if err != nil {
		return board.Board{}, err
	}
	e.mu.Lock()
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
	e.notify(changeEvent{Type: changeModified, Card: &card})
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
	created := card
	e.notify(changeEvent{Type: changeAdded, Card: &created})
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
	deleted := card
	e.notify(changeEvent{Type: changeDeleted, Card: &deleted})
	e.mu.Unlock()
	return nil
}

func (b *storeBackend) MoveCard(ctx context.Context, bd board.Board, card board.Card, afterID string) error {
	if err := b.inner.MoveCard(ctx, bd, card, afterID); err != nil {
		return err
	}
	b.touched(ctx, bd, card.ItemID)
	return nil
}

func (b *storeBackend) AddNote(ctx context.Context, bd board.Board, card board.Card, text string) error {
	if err := b.inner.AddNote(ctx, bd, card, text); err != nil {
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

// SetSprintState invalidates the cache (the created/updated sprint-state card
// carries a new id that must be re-split) and asks watchers to re-list.
func (b *storeBackend) SetSprintState(ctx context.Context, bd board.Board, team, current, previous string) error {
	if err := b.inner.SetSprintState(ctx, bd, team, current, previous); err != nil {
		return err
	}
	e := b.store.entry(storeKey(bd.Owner, bd.Number))
	e.mu.Lock()
	e.loaded = false
	e.notify(changeEvent{Type: changeReload})
	e.mu.Unlock()
	return nil
}
