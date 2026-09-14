package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A date that is not a real board day is refused at every date door and at
// create; empty still clears. Written unchecked it stranded a card: a value
// like "2026-13-99" string-compares after today, so the card read as future,
// its sprint was cleared, and no carry-over could reach it — alive and on no
// board anyone can open (security finding: unvalidated dates, also the trailer
// injection's delivery vector).
func TestDatesMustBeRealBoardDays(t *testing.T) {
	seed := func() *fakeBackend {
		return newFake([]board.Card{{ItemID: "c1", Team: "alpha", StartDate: "2026-01-05"}},
			map[string]board.SprintState{"alpha": {Current: "2026-01-05"}})
	}
	const bad = "2026-13-99"
	for _, tc := range []struct {
		name string
		call func(*Service) error
	}{
		{"SetDates start", func(s *Service) error { return s.SetDates(ctx, "acme", "c1", bad, "") }},
		{"SetDates end", func(s *Service) error { return s.SetDates(ctx, "acme", "c1", "2026-01-05", bad) }},
		{"SetDay", func(s *Service) error { return s.SetDay(ctx, "acme", "c1", bad) }},
		{"SetStart", func(s *Service) error { return s.SetStart(ctx, "acme", "c1", bad) }},
		{"SetSprintStart", func(s *Service) error { return s.SetSprintStart(ctx, "acme", "c1", bad) }},
		{"create day", func(s *Service) error {
			_, err := s.CreateCard(WithActor(ctx, "kvaps"), "acme", CreateCardArgs{Title: "x", Team: "alpha", Day: bad})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := seed()
			if err := tc.call(f2svc(f)); !errors.Is(err, ErrBadDay) {
				t.Fatalf("%s: err = %v, want ErrBadDay", tc.name, err)
			}
			if len(f.log) != 0 {
				t.Fatalf("%s: the refusal wrote something: %v", tc.name, f.log)
			}
		})
	}
	// A real date, and clearing, both still pass.
	f := seed()
	if err := f2svc(f).SetDates(ctx, "acme", "c1", "2026-02-02", "2026-02-03"); err != nil {
		t.Fatalf("a real range was refused: %v", err)
	}
	if err := f2svc(f).SetStart(ctx, "acme", "c1", ""); err != nil {
		t.Fatalf("clearing was refused: %v", err)
	}
}
