package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// drainFrames empties a subscription channel into parsed frames.
func drainFrames(t *testing.T, sub *subscription) []map[string]any {
	t.Helper()
	var out []map[string]any
	for {
		select {
		case raw := <-sub.ch:
			var f map[string]any
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatal(err)
			}
			out = append(out, f)
		default:
			return out
		}
	}
}

// cardTitles collects the UNIQUE titles of Card frames with the given verb —
// the write-behind queue may legitimately re-announce a card after its op
// lands, and the SPA upserts by item id, so duplicates carry no meaning.
func cardTitles(frames []map[string]any, verb string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range frames {
		if f["type"] != verb || f["kind"] != "Card" {
			continue
		}
		obj, _ := f["object"].(map[string]any)
		spec, _ := obj["spec"].(map[string]any)
		if s, ok := spec["title"].(string); ok && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// The author's own watch is spared the echo ONLY for the card their request
// addressed. A batch operation — an epic rename fanning out over its cards —
// has no optimistic copy on the author's side: suppressing its echoes made
// the renamed column's cards vanish from the very board that renamed it.
func TestBatchEchoesReachTheirAuthor(t *testing.T) {
	store := newBoardStore()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra", Project: "p"},
		{ItemID: "c1", Title: "one", Epic: "Infra", Project: "p"},
		{ItemID: "c2", Title: "two", Epic: "Infra", Project: "p"},
	}, nil)
	be := &storeBackend{inner: fake, store: store}
	svc := boardservice.New(be)
	ctx := context.Background()
	if _, err := be.LoadBoard(ctx, "acme", 1); err != nil {
		t.Fatal(err)
	}

	author, cancelA := store.subscribe(storeKey("acme", 1), "tab-A", nil, map[string]bool{"cards": true})
	defer cancelA()
	other, cancelB := store.subscribe(storeKey("acme", 1), "tab-B", nil, map[string]bool{"cards": true})
	defer cancelB()

	// The rename arrives as the author's request (client id, NO target card).
	rctx := withClientID(ctx, "tab-A")
	if err := svc.RenameEpic(rctx, "acme", 1, "p", "Infra", "Infra2"); err != nil {
		t.Fatal(err)
	}
	got := cardTitles(drainFrames(t, author), "MODIFIED")
	if len(got) != 2 || !strings.Contains(strings.Join(got, " "), "one") {
		t.Fatalf("the author saw %v, want both member cards", got)
	}
	if got := cardTitles(drainFrames(t, other), "MODIFIED"); len(got) != 2 {
		t.Fatalf("the other tab saw %v, want both member cards", got)
	}
}

// A single-card patch stays suppressed for its author — that is what the
// optimistic UI depends on — while cascades onto OTHER cards (a subtask
// following its parent) echo even to the author.
func TestAddressedCardStaysSuppressed(t *testing.T) {
	store := newBoardStore()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "c1", Title: "one", Progress: 10},
	}, nil)
	be := &storeBackend{inner: fake, store: store}
	svc := boardservice.New(be)
	ctx := context.Background()
	if _, err := be.LoadBoard(ctx, "acme", 1); err != nil {
		t.Fatal(err)
	}
	author, cancelA := store.subscribe(storeKey("acme", 1), "tab-A", nil, map[string]bool{"cards": true})
	defer cancelA()
	other, cancelB := store.subscribe(storeKey("acme", 1), "tab-B", nil, map[string]bool{"cards": true})
	defer cancelB()

	rctx := withTargetItem(withClientID(ctx, "tab-A"), "c1")
	if err := svc.SetProgress(rctx, "acme", 1, "c1", 40); err != nil {
		t.Fatal(err)
	}
	if got := cardTitles(drainFrames(t, author), "MODIFIED"); len(got) != 0 {
		t.Fatalf("the author was echoed their own patch: %v", got)
	}
	if got := cardTitles(drainFrames(t, other), "MODIFIED"); len(got) != 1 {
		t.Fatalf("the other tab saw %v, want the patch", got)
	}
}
