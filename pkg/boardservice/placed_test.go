package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// O13 in the actions: a task with no team of its own stands under its plan
// item's team, so wherever a team's SPRINT is resolved for it — taken into
// work, dated, made under the epic, carried over — it is that team's sprint;
// and its own team is never written down by any of it. The no-team group's
// sprint is not its sprint.
func placedSeed() *fakeBackend {
	old := "2026-01-05"
	return newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Parked: true},
		{ItemID: "w", Title: "In work", Parent: "epic", Assignees: []string{"ann"}, SprintStart: old, StartDate: old, Day: old},
		{ItemID: "own", Title: "Own team", Parent: "epic", Team: "ops", Assignees: []string{"ann"}, SprintStart: old, StartDate: old, Day: old},
	}, map[string]board.SprintState{
		"portal": {Current: old, ItemID: "sp"},
		"ops":    {Current: old, ItemID: "so"},
		"":       {Current: "2026-01-01", ItemID: "s0"},
	})
}

func TestTakingAnInheritingTaskJoinsItsEffectiveTeamsSprint(t *testing.T) {
	f := placedSeed()
	if err := New(f).Take(WithActor(ctx, "kvaps"), "acme", "t"); err != nil {
		t.Fatal(err)
	}
	c := f.get("t")
	if c.SprintStart != "2026-01-05" {
		t.Fatalf("taken into sprint %q, want portal's (2026-01-05), not the no-team group's", c.SprintStart)
	}
	if c.Team != "" {
		t.Fatalf("taking it wrote team %q onto it; the epic's team is inherited, not written", c.Team)
	}
}

func TestACardMadeUnderAPlanItemJoinsItsTeamsSprint(t *testing.T) {
	f := placedSeed()
	c, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{Title: "New", Parent: "epic", Assignee: "ann", Day: board.TodayIso()})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.get(c.ItemID); got.SprintStart != "2026-01-05" || got.Team != "" {
		t.Fatalf("made under the epic: sprint %q team %q, want portal's sprint and no own team", got.SprintStart, got.Team)
	}
}

func TestACarryOverCarriesTheTasksStandingUnderTheTeam(t *testing.T) {
	f := placedSeed()
	today := board.TodayIso()
	if _, err := New(f).CarryOver(ctx, "acme", "portal", false); err != nil {
		t.Fatal(err)
	}
	if got := f.get("w").SprintStart; got != today {
		t.Fatalf("portal's carry-over left the inheriting task in %q", got)
	}
	if got := f.get("own").SprintStart; got == today {
		t.Fatal("portal's carry-over carried ops's task, whose own team wins")
	}
	// The no-team group's carry-over does not reach it.
	f = placedSeed()
	if _, err := New(f).CarryOver(ctx, "acme", "", false); err != nil {
		t.Fatal(err)
	}
	if got := f.get("w").SprintStart; got == today {
		t.Fatal("the no-team group's carry-over carried portal's task")
	}
}

// A team a plan item names is in use: deleting it would leave the tasks
// below it standing under a team the board no longer has.
func TestATeamAPlanItemNamesCannotBeDeleted(t *testing.T) {
	f := newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
	}, map[string]board.SprintState{"portal": {Current: "2026-01-05", ItemID: "sp"}})
	if err := New(f).DeleteTeam(ctx, "acme", "portal"); !errors.Is(err, ErrTeamInUse) {
		t.Fatalf("deleting a team a plan item names = %v, want ErrTeamInUse", err)
	}
}
