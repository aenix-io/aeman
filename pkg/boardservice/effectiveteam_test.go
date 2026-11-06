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

// O13, M1 — a task's team can change without a write to the task: its plan
// item takes another team, or it is filed under another plan item, or taken
// out from under one. A task IN WORK then stands under the new team, so it
// joins that team's current sprint in the same action, as a direct team
// change does — or it would sit in a sprint its new team never opens and no
// carry-over of either team would reach. A task with its own team is not
// moved; a parked task has no sprint to move.
func followSeed() *fakeBackend {
	old, docs := "2026-01-05", "2026-01-07"
	return newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "docsEpic", Title: "Docs", Type: board.TypeEpic, Team: "docs"},
		{ItemID: "w", Title: "In work", Parent: "epic", Assignees: []string{"ann"}, SprintStart: old, StartDate: old, Day: old},
		{ItemID: "ws", Title: "Its subtask", Parent: "w", Assignees: []string{"ann"}, SprintStart: old, StartDate: old, Day: old},
		{ItemID: "own", Title: "Own team", Parent: "epic", Team: "ops", Assignees: []string{"ann"}, SprintStart: old, StartDate: old, Day: old},
		{ItemID: "p", Title: "Parked", Parent: "epic", Parked: true},
	}, map[string]board.SprintState{
		"portal": {Current: old, ItemID: "sp"},
		"docs":   {Current: docs, ItemID: "sd"},
		"ops":    {Current: old, ItemID: "so"},
		"":       {Current: "2026-01-01", ItemID: "s0"},
	})
}

func TestAPlanItemsNewTeamMovesTheWorkBelowIntoItsSprint(t *testing.T) {
	f := followSeed()
	if err := New(f).SetTeam(ctx, "acme", "epic", "docs", ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"w", "ws"} {
		if c := f.get(id); c.SprintStart != "2026-01-07" || c.Team != "" {
			t.Fatalf("%s after the epic went to docs: sprint %q team %q, want docs's sprint and no own team", id, c.SprintStart, c.Team)
		}
	}
	if got := f.get("own").SprintStart; got != "2026-01-05" {
		t.Fatalf("a task with its own team was moved to sprint %q", got)
	}
	if got := f.get("p"); got.SprintStart != "" || !got.Parked {
		t.Fatalf("a parked task was given a sprint: %+v", got)
	}
}

func TestFilingATaskUnderAnotherPlanItemMovesItIntoThatTeamsSprint(t *testing.T) {
	f := followSeed()
	if err := New(f).SetParent(ctx, "acme", "w", "docsEpic"); err != nil {
		t.Fatal(err)
	}
	if got := f.get("w").SprintStart; got != "2026-01-07" {
		t.Fatalf("filed under docs's epic, the task stayed in sprint %q", got)
	}
	// Taken out from under any plan item: the no-team group's sprint.
	if err := New(f).SetParent(ctx, "acme", "w", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.get("w").SprintStart; got != "2026-01-01" {
		t.Fatalf("unfiled, the task stayed in sprint %q, want the no-team group's", got)
	}
}

// "No owner" on a task under an epic takes its own team off: it goes on
// standing under the epic's team, in that team's sprint.
func TestNoOwnerOnATaskUnderAnEpicKeepsItUnderTheEpicsTeam(t *testing.T) {
	f := followSeed()
	if err := New(f).SetTeam(ctx, "acme", "own", "", ""); err != nil {
		t.Fatal(err)
	}
	if c := f.get("own"); c.Team != "" || c.SprintStart != "2026-01-05" {
		t.Fatalf("team off: own team %q sprint %q, want none and portal's sprint", c.Team, c.SprintStart)
	}
}

// M2 — a recurrent task inheriting its team is reseeded UNDER THE SAME PLAN
// ITEM, so the copy inherits the same way, rather than as an orphan in the
// no-team group.
func TestARecurrentInheritingTaskIsReseededUnderItsPlanItem(t *testing.T) {
	old := "2026-01-05"
	f := newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "wk", Title: "Weekly report", Parent: "epic", Assignees: []string{"ann"}, SprintStart: old, StartDate: old, Day: old,
			Stage: board.StageRecurrent, Progress: 100, Recurrence: "daily"},
	}, map[string]board.SprintState{"portal": {Current: old, ItemID: "sp"}})
	if _, err := New(f).CarryOver(ctx, "acme", "portal", false); err != nil {
		t.Fatal(err)
	}
	var copies []board.Card
	for _, c := range f.b.Cards {
		if c.Title == "Weekly report" && c.ItemID != "wk" {
			copies = append(copies, c)
		}
	}
	if len(copies) != 1 || copies[0].Parent != "epic" || copies[0].Team != "" {
		t.Fatalf("the reseeded copies = %+v, want one under the epic with no own team", copies)
	}
}

// Dates, a review and "finished earlier" on an inheriting task read its
// effective team's sprints.
func TestAnInheritingTasksDatesReviewAndEarlierSprintAreItsTeams(t *testing.T) {
	f := followSeed()
	svc := New(f)
	if err := svc.SetDates(ctx, "acme", "p", "2026-01-02", "2026-01-02"); err != nil {
		t.Fatal(err)
	}
	if got := f.get("p").SprintStart; got != "2026-01-05" {
		t.Fatalf("dated: sprint %q, want portal's current", got)
	}
}

func TestFinishedEarlierOnAnInheritingTaskUsesItsTeamsSprints(t *testing.T) {
	cur, prev := "2026-01-05", "2025-12-29"
	f := newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Team: "portal"},
		{ItemID: "d", Title: "Done", Parent: "epic", Assignees: []string{"ann"}, SprintStart: cur, StartDate: cur, Progress: 100},
	}, map[string]board.SprintState{"portal": {Current: cur, Previous: prev, ItemID: "sp"}})
	if err := New(f).FinishedEarlier(ctx, "acme", "d"); err != nil {
		t.Fatalf("finished earlier on an inheriting task = %v, want portal's previous sprint", err)
	}
	if got := f.get("d").SprintStart; got != prev {
		t.Fatalf("sent back to %q, want portal's previous %q", got, prev)
	}
}
