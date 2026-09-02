package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/internal/migrate/ghsource"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// plainSource is a source board with no event log: the migration writes the
// snapshot and reconciles to it, which is the shape a stranded card arrives
// in.
type plainSource struct{ *boardservicetest.Backend }

func (p plainSource) LoadBoard(ctx context.Context, owner string, _ int) (ghsource.Export, error) {
	b, err := p.Backend.LoadBoard(ctx, owner)
	if err != nil {
		return ghsource.Export{}, err
	}
	return ghsource.Export{Board: b, Items: map[string]ghsource.Item{}}, nil
}

// A board can arrive carrying cards no view will ever show: the old × demoted
// a worked card into the previous sprint, and once the sprint moved on again
// nothing reached it — not the day grid, not the Me board, and no carry-over,
// which takes the closing sprint's own cards. The migration does not carry
// that state forward: it takes such cards OFF the board in a commit of its
// own (the history keeps them, as it keeps every card the × removes) and says
// how many in its report.
func TestTheMigrationTakesStrandedCardsOffTheBoard(t *testing.T) {
	const cur, prev, old = "2026-09-01", "2026-08-31", "2026-07-31"
	src := boardservicetest.New([]board.Card{
		{ItemID: "PVTI_live", Title: "today's work", Team: "portal", Progress: 30,
			SprintStart: cur, StartDate: cur, Day: cur, CreatedAt: "2026-09-01T09:00:00Z"},
		{ItemID: "PVTI_stray", Title: "demoted in July and forgotten", Team: "portal", Progress: 90,
			SprintStart: old, StartDate: old, Day: old, CreatedAt: "2026-07-28T09:00:00Z"},
		{ItemID: "PVTI_kid", Title: "riding the stray", Team: "portal", Parent: "PVTI_stray",
			CreatedAt: "2026-07-28T09:30:00Z"},
	}, map[string]board.SprintState{
		"portal": {Current: cur, Previous: prev, ItemID: "PVTI_team"},
	})

	remote := newRemote(t, "")
	rep, err := Run(context.Background(), plainSource{src}, memory.NewStorage(), remote, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Stranded) != 2 {
		t.Fatalf("stranded = %v, want the card and the subtask riding it", rep.Stranded)
	}
	if !strings.Contains(rep.String(), "on no board") {
		t.Fatalf("the report must say it in words:\n%s", rep.String())
	}

	r := clone(t, remote)
	// The migration gives every card a ULID of its own; the report maps them.
	path := func(old string) string {
		id, ok := rep.IDMap[old]
		if !ok {
			t.Fatalf("no id mapped for %s", old)
		}
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	gone := path("PVTI_stray")
	if _, err := r.ReadFile(gone); err == nil {
		t.Fatal("the stranded card is off the board")
	}
	kid := path("PVTI_kid")
	if _, err := r.ReadFile(kid); err == nil {
		t.Fatal("and so is the subtask that rode it — it was on no board either")
	}
	if _, err := r.ReadFile(path("PVTI_live")); err != nil {
		t.Fatalf("today's card is untouched: %v", err)
	}

	// The history keeps what the board no longer holds: the commit before the
	// cleanup still has the card, so a record of its day shows it.
	head, err := r.CommitObject(r.Head())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(head.Message, "on no board") {
		t.Fatalf("the cleanup is a commit of its own:\n%s", head.Message)
	}
	parent, err := head.Parent(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parent.File(gone); err != nil {
		t.Fatalf("the commit before it still holds the card: %v", err)
	}
}

// A migration with nothing stranded writes no cleanup commit: an empty
// commit at the tip would say a board was tidied when nothing was.
func TestAMigrationWithNothingStrandedWritesNoCleanup(t *testing.T) {
	src := boardservicetest.New([]board.Card{
		{ItemID: "PVTI_live", Title: "today's work", Team: "portal", Progress: 30,
			SprintStart: "2026-09-01", StartDate: "2026-09-01", Day: "2026-09-01",
			CreatedAt: "2026-09-01T09:00:00Z"},
	}, map[string]board.SprintState{
		"portal": {Current: "2026-09-01", Previous: "2026-08-31", ItemID: "PVTI_team"},
	})
	remote := newRemote(t, "")
	rep, err := Run(context.Background(), plainSource{src}, memory.NewStorage(), remote, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Stranded) != 0 {
		t.Fatalf("stranded = %v", rep.Stranded)
	}
	r := clone(t, remote)
	head, err := r.CommitObject(r.Head())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(head.Message, "reconcile to the snapshot") {
		t.Fatalf("the tip is the reconcile, nothing after it:\n%s", head.Message)
	}
}
