package boardservice

import (
	"context"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func processBoard() *fakeBackend {
	return newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Marketing"},
		{ItemID: "pr1", Title: board.ProcessStateTitle, Process: "Articles", Project: "Marketing"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
}

// A process is declared by a state card inside a project; a template is a
// whole card — title and body in its description — kept out of the rows.
func TestProcessAndTemplate(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()

	if err := svc.AddProcess(ctx, "acme", 1, "articles", "Marketing"); !errors.Is(err, ErrProcessExists) {
		t.Fatalf("a case-insensitive duplicate must be refused, got %v", err)
	}
	if err := svc.AddProcess(ctx, "acme", 1, "Billing", "Ghost"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown project must be refused, got %v", err)
	}
	if _, err := svc.AddProcessTemplate(ctx, "acme", 1, "Nope", TemplateArgs{Title: "x", Recurrence: "week"}); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("a template needs an existing process, got %v", err)
	}
	if _, err := svc.AddProcessTemplate(ctx, "acme", 1, "Articles", TemplateArgs{Title: "x"}); err == nil {
		t.Fatal("a template needs a cycle")
	}
	tpl, err := svc.AddProcessTemplate(ctx, "acme", 1, "Articles", TemplateArgs{
		Title: "Technical article", Description: "1500 words, one code sample",
		Recurrence: "month", Start: "2026-03-03", Team: "alpha", Assignee: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if TemplateTitle(tpl) != "Technical article" || TemplateDescription(tpl) != "1500 words, one code sample" {
		t.Fatalf("template packs title+body in its description; got %q", tpl.Description)
	}
	b, _ := svc.Board(ctx, "acme", 1)
	if len(board.TemplatesOf(b, "Articles")) != 1 || len(b.Cards) != 0 {
		t.Fatalf("the template must be out of the card rows; cards=%d templates=%d", len(b.Cards), len(b.Templates))
	}
	// A process with templates is protected; rename carries the templates.
	if err := svc.DeleteProcess(ctx, "acme", 1, "Articles"); !errors.Is(err, ErrProcessInUse) {
		t.Fatalf("a process with templates must be protected, got %v", err)
	}
	if err := svc.RenameProcess(ctx, "acme", 1, "Articles", "Publishing"); err != nil {
		t.Fatal(err)
	}
	if got := fake.get(tpl.ItemID); got.Process != "Publishing" {
		t.Fatalf("the template must follow the rename, got %q", got.Process)
	}
}

// The calendar is the clock: a monthly template anchored on 3 March is due
// in the weeks of 3 April, 3 May, … regardless of what happened in between.
func TestNextAfterIsCalendarArithmetic(t *testing.T) {
	cases := []struct{ cycle, anchor, from, want string }{
		{"month", "2026-03-03", "2026-03-29", "2026-04-03"},
		{"month", "2026-03-03", "2026-04-03", "2026-05-03"}, // strictly after
		{"week", "2026-03-03", "2026-03-20", "2026-03-24"},
		{"2weeks", "2026-03-03", "2026-03-20", "2026-03-31"},
		{"quarter", "2026-01-15", "2026-05-01", "2026-07-15"},
		{"month", "2026-09-01", "2026-03-01", "2026-09-01"}, // anchor in the future
		{"", "2026-03-03", "2026-03-20", ""},                // per-sprint has no calendar
	}
	for _, c := range cases {
		if got := board.NextAfter(c.cycle, c.anchor, c.from); got != c.want {
			t.Errorf("NextAfter(%q, %s, %s) = %q, want %q", c.cycle, c.anchor, c.from, got, c.want)
		}
	}
}

// Iterations are spawned from the TEMPLATE: a renamed or re-described live
// card stays as it is, and the next one is the template again.
func TestSpawnCopiesTheTemplateNotThePreviousIteration(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	tpl, err := svc.AddProcessTemplate(ctx, "acme", 1, "Articles", TemplateArgs{
		Title: "Technical article", Description: "the brief",
		Recurrence: "week", Start: "2026-03-02", Team: "alpha", Assignee: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme", 1)
	n, err := svc.SpawnIterations(ctx, b, "alpha", "2026-03-02", false)
	if err != nil || n != 1 {
		t.Fatalf("spawned = %d, err = %v", n, err)
	}
	b, _ = svc.Board(ctx, "acme", 1)
	its := board.Iterations(b, tpl.ItemID)
	if len(its) != 1 {
		t.Fatalf("iterations = %d", len(its))
	}
	first := its[0]
	if first.Title != "Technical article" || first.Description != "the brief" ||
		first.Plan != board.PlanFri || first.Week != "2026-03-02" || first.Team != "alpha" ||
		first.Stage != board.StageRecurrent || len(first.Assignees) != 1 || first.Assignees[0] != "writer" {
		t.Fatalf("iteration = %+v", first)
	}
	// The team renames and finishes it…
	if err := svc.Rename(ctx, "acme", 1, first.ItemID, "Article about etcd"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProgress(ctx, "acme", 1, first.ItemID, 100); err != nil {
		t.Fatal(err)
	}
	// …and the next week's iteration is the template again, not the rename.
	b, _ = svc.Board(ctx, "acme", 1)
	if _, err := svc.SpawnIterations(ctx, b, "alpha", "2026-03-09", false); err != nil {
		t.Fatal(err)
	}
	b, _ = svc.Board(ctx, "acme", 1)
	its = board.Iterations(b, tpl.ItemID)
	if len(its) != 2 {
		t.Fatalf("iterations = %d", len(its))
	}
	if its[0].Title != "Article about etcd" {
		t.Fatalf("the live card must keep its rename, got %q", its[0].Title)
	}
	if its[1].Title != "Technical article" {
		t.Fatalf("the next iteration must come from the template, got %q", its[1].Title)
	}
}

// An open iteration holds the next one back — the stuck card is the process,
// and it goes overdue — unless the template accumulates. And a week never
// gets a second iteration of the same template.
func TestSpawnWaitsForTheOpenIterationUnlessAccumulating(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	waits, _ := svc.AddProcessTemplate(ctx, "acme", 1, "Articles", TemplateArgs{
		Title: "Article", Recurrence: "week", Start: "2026-03-02", Team: "alpha",
	})
	piles, _ := svc.AddProcessTemplate(ctx, "acme", 1, "Articles", TemplateArgs{
		Title: "Invoice client X", Recurrence: "week", Start: "2026-03-02", Team: "alpha", Accumulate: true,
	})
	b, _ := svc.Board(ctx, "acme", 1)
	if n, _ := svc.SpawnIterations(ctx, b, "alpha", "2026-03-02", false); n != 2 {
		t.Fatalf("first week spawns both, got %d", n)
	}
	// Nobody finished either. Next week:
	b, _ = svc.Board(ctx, "acme", 1)
	if n, _ := svc.SpawnIterations(ctx, b, "alpha", "2026-03-09", false); n != 1 {
		t.Fatalf("only the accumulating template spawns while open, got %d", n)
	}
	b, _ = svc.Board(ctx, "acme", 1)
	if len(board.Iterations(b, waits.ItemID)) != 1 || len(board.Iterations(b, piles.ItemID)) != 2 {
		t.Fatalf("waits=%d piles=%d", len(board.Iterations(b, waits.ItemID)), len(board.Iterations(b, piles.ItemID)))
	}
	// Re-running the same week is a no-op.
	if n, _ := svc.SpawnIterations(ctx, b, "alpha", "2026-03-09", true); n != 0 {
		t.Fatalf("re-running a week must spawn nothing, got %d", n)
	}
}

// A monthly template is due in the week its anchor's day falls in, and in no
// other week: the calendar is the clock.
func TestMonthlyTemplateIsDueOnceAMonth(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	if _, err := svc.AddProcessTemplate(ctx, "acme", 1, "Articles", TemplateArgs{
		Title: "Monthly", Recurrence: "month", Start: "2026-03-03", Team: "alpha",
	}); err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme", 1)
	for week, want := range map[string]int{
		"2026-03-02": 1, // the anchor's own week
		"2026-03-16": 0, // mid-month: nothing due
		"2026-03-30": 1, // the week of 3 April
		"2026-04-06": 0,
	} {
		if n, _ := svc.SpawnIterations(ctx, b, "alpha", week, true); n != want {
			t.Errorf("week %s: spawned %d, want %d", week, n, want)
		}
	}
}
