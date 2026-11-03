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

// The importer writes the CURRENT schema, so it brings a board in already in
// the shape schema 3 leaves (K8), by schema 3's own rule for a task that had
// nothing but a WEEK in Projects v2 — read from the source item, since the
// loader reads a task's week as nothing:
//
//   - a week still AHEAD of the import day was the weekly plan: the task is
//     PARKED in its team's backlog;
//   - a week that has COME stood on the day boards as a debt: the task stays
//     in work, with that week's Monday to Friday as its dates;
//   - a FINISHED task only loses the week.
//
// An open task nobody had placed at all is parked too, and since a parked
// task is reachable, the stranded-card cleanup, which DELETES what
// Unreachable names, keeps every one of them.
func TestTheImporterPlacesAWeekOnlyTaskByWhereItsWeekStands(t *testing.T) {
	const cur = "2026-09-01"
	today := board.TodayIso()
	thisWeek := board.MondayOf(today)
	ahead := board.AddDays(thisWeek, 7)
	gone := board.AddDays(thisWeek, -14)
	cards := []board.Card{
		{ItemID: "PVTI_ahead", Title: "planned for next week", Team: "portal", Week: ahead,
			Assignees: []string{"kvaps"}, CreatedAt: "2026-08-28T09:00:00Z"},
		{ItemID: "PVTI_come", Title: "owed this week", Team: "portal", Week: thisWeek,
			Assignees: []string{"kvaps"}, CreatedAt: "2026-08-28T09:10:00Z"},
		{ItemID: "PVTI_gone", Title: "owed two weeks ago", Team: "portal", Week: gone,
			CreatedAt: "2026-08-28T09:20:00Z"},
		{ItemID: "PVTI_wdone", Title: "finished in its week", Team: "portal", Week: gone, Progress: 100,
			CreatedAt: "2026-08-28T09:30:00Z"},
		{ItemID: "PVTI_strip", Title: "nobody said when", Team: "portal", Progress: 20,
			CreatedAt: "2026-08-29T09:00:00Z"},
		{ItemID: "PVTI_done", Title: "finished, never placed", Team: "portal", Progress: 100,
			CreatedAt: "2026-08-29T09:30:00Z"},
		{ItemID: "PVTI_live", Title: "in work", Team: "portal", SprintStart: cur, StartDate: cur, Day: cur,
			CreatedAt: "2026-09-01T09:00:00Z"},
	}
	items := map[string]ghsource.Item{}
	for _, c := range cards {
		items[c.ItemID] = ghsource.Item{IsDraft: true, Week: c.Week}
	}
	src := boardservicetest.New(cards, map[string]board.SprintState{"portal": {Current: cur, ItemID: "PVTI_team"}})
	remote := newRemote(t, "")
	rep, err := Run(context.Background(), itemsSource{src, items}, memory.NewStorage(), remote, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Stranded) != 0 || len(rep.Placeless) != 0 {
		t.Fatalf("stranded = %v, placeless = %v; every task is in work or in its backlog", rep.Stranded, rep.Placeless)
	}
	s, err := gitstore.Load(clone(t, remote))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]board.Card{}
	for _, c := range s.Cards {
		got[c.Title] = c
		if c.Week != "" {
			t.Errorf("%q came in with a week", c.Title)
		}
	}
	type want struct {
		parked     bool
		start, day string
	}
	for title, w := range map[string]want{
		"planned for next week":  {parked: true},
		"owed this week":         {start: thisWeek, day: board.AddDays(thisWeek, 4)},
		"owed two weeks ago":     {start: gone, day: board.AddDays(gone, 4)},
		"finished in its week":   {},
		"nobody said when":       {parked: true},
		"finished, never placed": {},
		"in work":                {start: cur, day: cur},
	} {
		c := got[title]
		if c.Parked != w.parked || c.StartDate != w.start || c.Day != w.day {
			t.Errorf("%q = parked %v %s..%s, want parked %v %s..%s", title, c.Parked, c.StartDate, c.Day, w.parked, w.start, w.day)
		}
	}
}

// itemsSource is a source board with the GitHub side of each item given —
// the source WEEK among it, which the loader's board no longer carries.
type itemsSource struct {
	*boardservicetest.Backend
	items map[string]ghsource.Item
}

func (s itemsSource) LoadBoard(ctx context.Context, owner string, _ int) (ghsource.Export, error) {
	b, err := s.Backend.LoadBoard(ctx, owner)
	if err != nil {
		return ghsource.Export{}, err
	}
	return ghsource.Export{Board: b, Items: s.items}, nil
}
