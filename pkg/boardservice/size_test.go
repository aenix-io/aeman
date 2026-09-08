package boardservice

import (
	"context"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Sizing a card is a decision somebody makes — on a daily sync, or a sizing
// tool on a lead's behalf — so it is written as one and recorded in the
// card's log like a zone or a stage: the feed says who sized it and from what.
func TestSizingACardIsRecordedInItsLog(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Title: "лендинг"}}, nil)
	svc := New(f)
	ctx := board.WithActor(context.Background(), "lead")

	if err := svc.SetSize(ctx, "acme", "c1", board.SizeM); err != nil {
		t.Fatal(err)
	}
	if got := f.get("c1").Size; got != board.SizeM {
		t.Fatalf("size = %q, want M", got)
	}
	if err := svc.SetSize(ctx, "acme", "c1", board.SizeL); err != nil {
		t.Fatal(err)
	}
	if !f.saw("SetSize c1 L") {
		t.Fatalf("the resize must reach the backend; log=%v", f.log)
	}
	// Saying the same size again is not a decision and leaves no trace: a
	// tool re-run over a sized board must not fill every log with echoes.
	before := f.count("SetSize c1")
	if err := svc.SetSize(ctx, "acme", "c1", board.SizeL); err != nil {
		t.Fatal(err)
	}
	if got := f.count("SetSize c1"); got != before {
		t.Fatalf("re-saying the size must write nothing; writes went %d -> %d", before, got)
	}
}

// Only the four letters are a size. A tool that sends "large" or "4" is
// refused with a named error rather than having something stored that
// weighs nothing and reads as unsized — and the empty size is how a size is
// taken back.
func TestOnlyTheFourLettersAreASize(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Size: board.SizeS}}, nil)
	svc := New(f)
	ctx := context.Background()

	for _, bad := range []board.SizeKey{"large", "4", "s", "XXL"} {
		err := svc.SetSize(ctx, "acme", "c1", bad)
		if !errors.Is(err, ErrUnknownSize) {
			t.Errorf("SetSize(%q) = %v, want ErrUnknownSize", bad, err)
		}
	}
	if got := f.get("c1").Size; got != board.SizeS {
		t.Fatalf("a refused size must leave the card as it was, got %q", got)
	}
	if err := svc.SetSize(ctx, "acme", "c1", board.SizeNone); err != nil {
		t.Fatal(err)
	}
	if got := f.get("c1").Size; got != board.SizeNone {
		t.Fatalf("the empty size clears, got %q", got)
	}
}

// A card created with a size carries it from the first commit — the sizing
// tool and a lead creating cards on a sync both say the size at birth, and a
// create-then-size pair would be two commits and a window in which the card
// weighs nothing.
func TestACardCanBeBornWithASize(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-09-07"}})
	svc := New(f)
	card, err := svc.CreateCard(context.Background(), "acme", CreateCardArgs{
		Team: "alpha", Title: "Реализация envoy-gateway", Size: board.SizeL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.get(card.ItemID).Size; got != board.SizeL {
		t.Fatalf("size at birth = %q, want L", got)
	}
}

// A size with whitespace round it is not one of the four letters, and the
// guard let it through: it compared the value to its own UPPER-CASING, and
// " L " is its own upper-casing, while ParseSize — which trims — agreed the
// string was fine. The raw " L " then reached the store, and a file holding
// it weighs nothing in every sum, with no chip drawn on the card: the silent
// failure this refusal exists to prevent, arriving through the door meant to
// stop it. Normalising is the doors' job; this one takes the letter as it is.
func TestASizeWithSpaceRoundItIsNotASize(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Title: "one", Size: board.SizeM}}, nil)
	svc := New(f)
	ctx := context.Background()

	for _, bad := range []board.SizeKey{" L ", "L ", " ", "\tXL"} {
		if err := svc.SetSize(ctx, "acme", "c1", bad); !errors.Is(err, ErrUnknownSize) {
			t.Errorf("SetSize(%q) = %v, want ErrUnknownSize", bad, err)
		}
	}
	if got := f.get("c1").Size; got != board.SizeM {
		t.Fatalf("a refused size must leave the card as it was, got %q", got)
	}
	if err := svc.SetSize(ctx, "acme", "c1", board.SizeL); err != nil {
		t.Fatal(err)
	}
	if got := f.get("c1").Size; got != board.SizeL {
		t.Fatalf("the letter itself is stored, got %q", got)
	}
}
