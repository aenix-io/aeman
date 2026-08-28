package boardservice

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// A personal card is created into the actor's own domain — the service
// names it, the caller only says "personal" — and carries none of the team
// board's placement: no team, no column, no plan band. It is a backlog item
// with a zone, dates and a description.
func TestCreatePersonalCardGoesToTheActorsDomain(t *testing.T) {
	fake := newFake(nil, nil)
	svc := New(fake)
	ctx := board.WithActor(t.Context(), "kvaps")
	c, err := svc.CreateCard(ctx, "o", CreateCardArgs{Title: "read the paper", Zone: board.ZoneYellow, Personal: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.Domain != "~kvaps" {
		t.Fatalf("personal card's domain = %q, want ~kvaps", c.Domain)
	}
	if c.Team != "" || c.SprintStart != "" {
		t.Fatalf("a personal card joins no team and no sprint: %+v", c)
	}
	if len(fake.creates) != 1 || !fake.creates[0].Personal || fake.creates[0].Domain != "~kvaps" {
		t.Fatalf("backend got %+v", fake.creates)
	}
	// It is still a card: zone and description are its own.
	if c.Zone != board.ZoneYellow {
		t.Fatalf("zone = %q", c.Zone)
	}
}

// The team board's placement has no meaning on a personal card, and an
// anonymous caller has no domain to file into.
// The owner reading their personal board is what turns the day over there:
// a finished recurrent card gets its fresh copy — same title, zone and body,
// 0%, assigned to the owner, in the personal domain — once its cycle is due,
// and only once, whatever the number of reads.
func TestReseedPersonalSeedsTheNextIterationOnce(t *testing.T) {
	daily := board.Card{ItemID: "d1", Title: "inbox zero", Domain: "~kvaps", Zone: board.ZoneGreen,
		Stage: board.StageRecurrent, Progress: 100, StartDate: "2026-08-27", Day: "2026-08-27", DoneAt: "2026-08-27",
		Assignees: []string{"kvaps"}, Description: "clear the inbox"}
	weekly := board.Card{ItemID: "w1", Title: "weekly review", Domain: "~kvaps", Zone: board.ZoneYellow,
		Stage: board.StageRecurrent, Recurrence: board.RecurrenceWeek, Progress: 100,
		StartDate: "2026-08-26", DoneAt: "2026-08-26", Assignees: []string{"kvaps"}}
	fake := newFake([]board.Card{daily, weekly}, nil)
	svc := New(fake)
	ctx := board.WithActor(t.Context(), "kvaps")

	n, err := svc.ReseedPersonal(ctx, "o", "kvaps", "2026-08-28")
	if err != nil || n != 1 {
		t.Fatalf("reseed = %d, %v; want 1 (the daily card), log %v", n, err, fake.log)
	}
	if len(fake.creates) != 1 {
		t.Fatalf("creates = %+v", fake.creates)
	}
	in := fake.creates[0]
	if !in.Personal || in.Domain != "~kvaps" || in.Title != "inbox zero" || in.Zone != board.ZoneGreen ||
		in.Start != "2026-08-28" || in.Day != "2026-08-28" || in.Assignee != "kvaps" || in.Team != "" || in.SprintStart != "" {
		t.Fatalf("the copy was created as %+v", in)
	}
	fresh := fake.get("new1")
	if fresh == nil || fresh.Stage != board.StageRecurrent || fresh.Progress != 0 || fresh.Description != "clear the inbox" {
		t.Fatalf("the copy = %+v", fresh)
	}
	if old := fake.get("d1"); old.Progress != 100 || old.DoneAt != "2026-08-27" {
		t.Fatalf("the finished card must stay as it was: %+v", old)
	}

	// The same day again: the copy is there, nothing to do.
	if n, err := svc.ReseedPersonal(ctx, "o", "kvaps", "2026-08-28"); err != nil || n != 0 {
		t.Fatalf("second read of the day reseeded %d, %v", n, err)
	}
	// The next day: the fresh daily copy is open, so it is not due; the
	// weekly card is still resting.
	if n, err := svc.ReseedPersonal(ctx, "o", "kvaps", "2026-08-29"); err != nil || n != 0 {
		t.Fatalf("with the daily copy open, reseeded %d, %v", n, err)
	}
	// A week after the weekly card's start it comes due, with its cycle.
	if n, err := svc.ReseedPersonal(ctx, "o", "kvaps", "2026-09-02"); err != nil || n != 1 {
		t.Fatalf("weekly = %d, %v", n, err)
	}
	if c := fake.get("new2"); c == nil || c.Title != "weekly review" || c.Recurrence != board.RecurrenceWeek || c.Stage != board.StageRecurrent {
		t.Fatalf("the weekly copy = %+v", c)
	}
	// Nobody's board, or no login at all: a no-op, never an error.
	if n, err := svc.ReseedPersonal(ctx, "o", "", "2026-09-02"); err != nil || n != 0 {
		t.Fatalf("no login: %d, %v", n, err)
	}
}

// Planning on a personal board is dates alone: the calendar and the defer
// move a personal card's start (and end) exactly as on a team card, but the
// card joins no sprint on the way — the team rule would pin it to the sprint
// active on its start day, and a personal board has none.
func TestDatesOnAPersonalCardKeepItOutOfSprints(t *testing.T) {
	today := board.TodayIso()
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: "read the paper", Domain: "~kvaps", Assignees: []string{"kvaps"},
			StartDate: today, Day: today, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		{ItemID: "t1", Title: "team card", Domain: "aeman-db", Team: "", StartDate: today, Day: today,
			CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	}, nil)
	svc := New(fake)
	ctx := board.WithActor(t.Context(), "kvaps")

	// The calendar: a start on or before today would give a team card the
	// sprint active that day (or the day itself); the personal card gets none.
	yesterday := board.AddDays(today, -1)
	if err := svc.SetDates(ctx, "o", "p1", yesterday, today); err != nil {
		t.Fatal(err)
	}
	if c := fake.get("p1"); c.StartDate != yesterday || c.Day != today || c.SprintStart != "" {
		t.Fatalf("personal card after SetDates = start %q end %q sprint %q; want the dates and no sprint", c.StartDate, c.Day, c.SprintStart)
	}
	if err := svc.SetDates(ctx, "o", "t1", yesterday, today); err != nil {
		t.Fatal(err)
	}
	if c := fake.get("t1"); c.SprintStart == "" {
		t.Fatal("the team rule is unchanged: a team card dated to a past day joins a sprint")
	}

	// Defer a week ahead: a card created today relocates fully on a team
	// board (sprint and end date follow); the personal card's dates follow,
	// its sprint stays empty.
	if err := svc.Defer(ctx, "o", "p1", 7); err != nil {
		t.Fatal(err)
	}
	week := board.AddDays(today, 7)
	if c := fake.get("p1"); c.StartDate != week || c.Day != week || c.SprintStart != "" {
		t.Fatalf("personal card after Defer = start %q end %q sprint %q; want %s, %s and no sprint", c.StartDate, c.Day, c.SprintStart, week, week)
	}
	// Presses stack: another day from the deferred slot, not from today.
	if err := svc.Defer(ctx, "o", "p1", 1); err != nil {
		t.Fatal(err)
	}
	if c := fake.get("p1"); c.StartDate != board.AddDays(week, 1) || c.SprintStart != "" {
		t.Fatalf("second defer = start %q sprint %q", c.StartDate, c.SprintStart)
	}
	for _, line := range fake.log {
		if strings.HasPrefix(line, "SetSprintStart p1 ") && !strings.HasSuffix(line, " ") {
			t.Fatalf("a sprint was written on the personal card: %q", line)
		}
	}
}

// The × on a personal board (Remove from the grid) has no sprint to demote
// into: a worked-on card is left behind on yesterday's board — leftAt set on
// it and on its subtasks, nothing deleted — and an untouched one, or one that
// started today, is deleted for real. Re-dating a left card brings it back:
// the calendar and the defer clear leftAt on it and its subtasks.
func TestRemoveOnAPersonalCardLeavesItBehindOrDeletes(t *testing.T) {
	today := board.TodayIso()
	yesterday := board.AddDays(today, -1)
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: "half done", Domain: "~kvaps", Progress: 40, StartDate: "2026-08-20", Assignees: []string{"kvaps"}},
		{ItemID: "s1", Title: "a step", Domain: "~kvaps", Parent: "p1", StartDate: "2026-08-20"},
		{ItemID: "p2", Title: "untouched", Domain: "~kvaps", Progress: 0, StartDate: "2026-08-20"},
		{ItemID: "p3", Title: "started today", Domain: "~kvaps", Progress: 60, StartDate: today},
	}, nil)
	svc := New(fake)
	ctx := board.WithActor(t.Context(), "kvaps")

	if err := svc.Remove(ctx, "o", "p1", "grid"); err != nil {
		t.Fatal(err)
	}
	if c := fake.get("p1"); c == nil || c.LeftAt != yesterday || c.Progress != 40 {
		t.Fatalf("worked card after ×: %+v; want kept, leftAt %s", c, yesterday)
	}
	if c := fake.get("s1"); c == nil || c.LeftAt != yesterday {
		t.Fatalf("its subtask after ×: %+v; want left with its parent", c)
	}
	if fake.count("DeleteCard") != 0 {
		t.Fatalf("the × must delete nothing here; log = %v", fake.log)
	}
	for _, id := range []string{"p2", "p3"} {
		if err := svc.Remove(ctx, "o", id, "grid"); err != nil {
			t.Fatal(err)
		}
		if fake.get(id) != nil {
			t.Fatalf("%s: an untouched or just-started card is deleted by the ×", id)
		}
	}

	// Back on the board by re-dating it (to yesterday…today: a card that
	// started today would be deleted by the next ×, not left behind).
	if err := svc.SetDates(ctx, "o", "p1", yesterday, today); err != nil {
		t.Fatal(err)
	}
	if c := fake.get("p1"); c.LeftAt != "" || c.StartDate != yesterday {
		t.Fatalf("after the calendar: %+v; want leftAt cleared", c)
	}
	if c := fake.get("s1"); c.LeftAt != "" {
		t.Fatalf("its subtask after the calendar: %+v; want leftAt cleared", c)
	}
	if err := svc.Remove(ctx, "o", "p1", "grid"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Defer(ctx, "o", "p1", 1); err != nil {
		t.Fatal(err)
	}
	if c := fake.get("p1"); c.LeftAt != "" || c.StartDate != board.AddDays(today, 1) {
		t.Fatalf("after the defer: %+v; want leftAt cleared and the start moved", c)
	}
}

func TestCreatePersonalCardRefusesPlacementAndAnonymity(t *testing.T) {
	fake := newFake(nil, map[string]board.SprintState{"portal": {Current: "2026-08-24", ItemID: "st"}})
	svc := New(fake)
	ctx := board.WithActor(t.Context(), "kvaps")
	for name, args := range map[string]CreateCardArgs{
		"team":   {Title: "x", Personal: true, Team: "portal"},
		"column": {Title: "x", Personal: true, Project: "freedom", Epic: "Docs"},
		"plan":   {Title: "x", Personal: true, Plan: board.PlanWed, Week: "2026-08-24"},
	} {
		if _, err := svc.CreateCard(ctx, "o", args); !errors.Is(err, ErrPersonalPlacement) {
			t.Fatalf("%s on a personal card: err = %v, want ErrPersonalPlacement", name, err)
		}
	}
	if _, err := svc.CreateCard(t.Context(), "o", CreateCardArgs{Title: "x", Personal: true}); err == nil {
		t.Fatal("a personal card without an actor has no domain and must be refused")
	}
	if fake.count("CreateCard") != 0 {
		t.Fatalf("refused creates must write nothing; log = %v", fake.log)
	}
}
