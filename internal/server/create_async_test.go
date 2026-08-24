package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// slowCreates wraps the test backend so a create takes real time, the way a
// GitHub round trip does — which is the whole point of answering first.
type slowCreates struct {
	boardservice.Backend
	mu      sync.Mutex
	delay   time.Duration
	created []board.Card
}

func (s *slowCreates) CreateCard(ctx context.Context, b board.Board, in board.CreateInput) (board.Card, error) {
	time.Sleep(s.delay)
	card, err := s.Backend.CreateCard(ctx, b, in)
	s.mu.Lock()
	s.created = append(s.created, card)
	s.mu.Unlock()
	return card, err
}

// A create answers from the cache: the caller gets a provisional card at
// once, the real create rides the queue, and when GitHub's id arrives every
// place the cache held the provisional one — and every request still naming
// it — follows.
func TestCreateAnswersFromTheCache(t *testing.T) {
	store := newBoardStore()
	fake := boardservicetest.New(nil, map[string]board.SprintState{
		"alpha": {Current: board.TodayIso(), ItemID: "st1"},
	})
	slow := &slowCreates{Backend: fake, delay: 300 * time.Millisecond}
	be := &storeBackend{inner: &resolvingBackend{inner: slow, store: store}, store: store}
	svc := boardservice.New(be)
	ctx := context.Background()

	// Warm the cache (a create needs the entry loaded).
	if _, err := svc.Board(ctx, "acme", 1); err != nil {
		t.Fatal(err)
	}

	t0 := time.Now()
	card, err := svc.CreateCard(ctx, "acme", 1, boardservice.CreateCardArgs{
		Title: "answered at once", Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(t0); took >= slow.delay {
		t.Fatalf("the create waited for GitHub: %v", took)
	}
	if !strings.HasPrefix(card.ItemID, localIDPrefix) {
		t.Fatalf("expected a provisional id, got %q", card.ItemID)
	}

	// The card is on the board right now, under the provisional id.
	b, _ := svc.Board(ctx, "acme", 1)
	if _, ok := boardCard(b, card.ItemID); !ok {
		t.Fatal("the provisional card is not on the board")
	}

	// …and a rename by that id works even AFTER adoption: wait out the queue.
	e := store.entry(storeKey("acme", 1))
	deadline := time.Now().Add(3 * time.Second)
	for {
		e.mu.Lock()
		adopted := e.locals[card.ItemID].ItemID != ""
		e.mu.Unlock()
		if adopted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the queued create never adopted a real id")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := svc.Rename(ctx, "acme", 1, card.ItemID, "renamed via the old id"); err != nil {
		t.Fatalf("a rename by the provisional id after adoption: %v", err)
	}
	b, _ = svc.Board(ctx, "acme", 1)
	e.mu.Lock()
	realID := e.locals[card.ItemID].ItemID
	e.mu.Unlock()
	got, ok := boardCard(b, realID)
	if !ok || got.Title != "renamed via the old id" {
		t.Fatalf("the rename did not land on the adopted card: %+v", got)
	}
	if strings.HasPrefix(realID, localIDPrefix) {
		t.Fatalf("adoption kept a provisional id: %q", realID)
	}
}

// Writes queued behind a create resolve the provisional id at execution time,
// so a create-then-edit burst lands on the real card in order.
func TestQueuedWritesFollowTheAdoptedID(t *testing.T) {
	store := newBoardStore()
	fake := boardservicetest.New(nil, map[string]board.SprintState{
		"alpha": {Current: board.TodayIso(), ItemID: "st1"},
	})
	slow := &slowCreates{Backend: fake, delay: 150 * time.Millisecond}
	be := &storeBackend{inner: &resolvingBackend{inner: slow, store: store}, store: store}
	svc := boardservice.New(be)
	ctx := context.Background()
	if _, err := svc.Board(ctx, "acme", 1); err != nil {
		t.Fatal(err)
	}
	card, err := svc.CreateCard(ctx, "acme", 1, boardservice.CreateCardArgs{
		Title: "burst", Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Edit immediately, while the create is still in flight.
	if err := svc.SetProgress(ctx, "acme", 1, card.ItemID, 40); err != nil {
		t.Fatal(err)
	}
	// Wait for the whole queue to drain, then check GitHub's copy.
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	store.waitDrained(dctx)
	slow.mu.Lock()
	created := append([]board.Card(nil), slow.created...)
	slow.mu.Unlock()
	if len(created) != 1 {
		t.Fatalf("GitHub saw %d creates", len(created))
	}
	upstream := fake.Card(created[0].ItemID)
	if upstream == nil || upstream.Progress != 40 {
		t.Fatalf("the queued edit did not reach GitHub's card: %+v", upstream)
	}
}

func boardCard(b board.Board, id string) (board.Card, bool) {
	if real, ok := b.Aliases[id]; ok && real != "" {
		id = real
	}
	for _, c := range b.Cards {
		if c.ItemID == id {
			return c, true
		}
	}
	return board.Card{}, false
}

// A provisional card must survive a warm install: the warmer replaces the
// cached board every few minutes, and the create's pending op is what
// re-imposes the not-yet-adopted row onto the fresh snapshot. With an empty
// apply, a warm tick between create and adoption WIPED the row — an
// immediate rename answered "card not found: local-…", and the client that
// still held the provisional saw a duplicate once the real card arrived.
func TestProvisionalSurvivesAWarmInstall(t *testing.T) {
	store := newBoardStore()
	fake := boardservicetest.New(nil, map[string]board.SprintState{
		"alpha": {Current: board.TodayIso(), ItemID: "st1"},
	})
	slow := &slowCreates{Backend: fake, delay: 300 * time.Millisecond}
	be := &storeBackend{inner: &resolvingBackend{inner: slow, store: store}, store: store}
	svc := boardservice.New(be)
	ctx := context.Background()
	if _, err := be.LoadBoard(ctx, "acme", 1); err != nil {
		t.Fatal(err)
	}

	created, err := svc.CreateCard(ctx, "acme", 1, boardservice.CreateCardArgs{
		Title: "born mid-warm", Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ItemID, "local-") {
		t.Fatalf("expected a provisional id, got %q", created.ItemID)
	}

	// The warmer replaces the cache with a fresh snapshot that does not
	// contain the provisional row (GitHub does not know it yet).
	fresh, err := fake.LoadBoard(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	e := store.entry(storeKey("acme", 1))
	be.install(e, fresh, "")

	// The provisional row is still on the cached board…
	bd, err := be.LoadBoard(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	present := false
	for _, c := range bd.Cards {
		if c.ItemID == created.ItemID {
			present = true
		}
	}
	if !present {
		t.Fatal("the warm install wiped the provisional card")
	}
	// …and an immediate rename by the provisional id lands.
	if err := svc.Rename(ctx, "acme", 1, created.ItemID, "renamed mid-warm"); err != nil {
		t.Fatalf("rename by the provisional id: %v", err)
	}

	// Once the queue drains, the adopted real card carries the rename.
	deadline := time.Now().Add(3 * time.Second)
	for {
		bd, _ := be.LoadBoard(ctx, "acme", 1)
		var found *board.Card
		for i := range bd.Cards {
			if bd.Cards[i].Title == "renamed mid-warm" && !strings.HasPrefix(bd.Cards[i].ItemID, "local-") {
				found = &bd.Cards[i]
			}
		}
		if found != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the adopted card never carried the rename")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The resolving wrapper must not hide its inner backend's capabilities: the
// store type-asserts b.inner for the cheap access probe (and the body
// syncer, and the link resolver), and a wrapper that swallows them silently
// turned every returning user's read into a full 50-second board load.
func TestResolvingForwardsCapabilities(t *testing.T) {
	r := &resolvingBackend{inner: probingBackend{}, store: newBoardStore()}
	var asIface any = r
	p, ok := asIface.(accessProber)
	if !ok {
		t.Fatal("the wrapper does not offer the access probe")
	}
	if err := p.CheckBoardAccess(context.Background(), "acme", 1); err != nil {
		t.Fatalf("the probe was not delegated: %v", err)
	}
	// An inner backend WITHOUT the probe reports failure rather than lying.
	r2 := &resolvingBackend{inner: boardservicetest.New(nil, nil), store: newBoardStore()}
	var as2 any = r2
	if err := as2.(accessProber).CheckBoardAccess(context.Background(), "acme", 1); err == nil {
		t.Fatal("a probe-less inner backend must report the probe failed")
	}
}

// probingBackend is a fake that offers CheckBoardAccess.
type probingBackend struct{ boardservice.Backend }

func (probingBackend) CheckBoardAccess(context.Context, string, int) error { return nil }
