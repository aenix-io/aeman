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
	// Nothing is stranded any more, and that is the fix rather than the
	// failure: a day board shows what is open and not put off, so the July
	// card its own team never carried over is in somebody's hands and on
	// their board. The step stays as the net it always was — it just has
	// nothing left to catch on a board built by this server.
	if len(rep.Stranded) != 0 {
		t.Fatalf("stranded = %v, want none: open work is reachable now", rep.Stranded)
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
	// Every card arrives, the July one included: it is open work, and the
	// board it lands on shows open work.
	for _, old := range []string{"PVTI_live", "PVTI_stray", "PVTI_kid"} {
		if _, err := r.ReadFile(path(old)); err != nil {
			t.Fatalf("%s must arrive on the board: %v", old, err)
		}
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
