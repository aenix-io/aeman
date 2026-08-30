package boardservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func processBoard() *fakeBackend {
	return newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Marketing"},
		{ItemID: "pr1", Title: board.ProcessStateTitle, Process: "Articles", Project: "Marketing"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
}

// A process is declared by a state card inside a project; a task is a
// whole card — title and body in its description — kept out of the rows.
func TestProcessAndTemplate(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()

	if err := svc.AddProcess(ctx, "acme", "articles", "Marketing"); !errors.Is(err, ErrProcessExists) {
		t.Fatalf("a case-insensitive duplicate must be refused, got %v", err)
	}
	if err := svc.AddProcess(ctx, "acme", "Billing", "Ghost"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown project must be refused, got %v", err)
	}
	if _, err := svc.AddProcessTask(ctx, "acme", "Nope", TaskArgs{Title: "x", Recurrence: "week"}); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("a task needs an existing process, got %v", err)
	}
	if _, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{Title: "x"}); err == nil {
		t.Fatal("a task needs a cycle")
	}
	// The anchor is NEXT week, and relative to today on purpose: adding a
	// task files this week's card at once when the week is already owed
	// (spawnDue), so a fixed anchor made this test pass or fail by the
	// calendar — a monthly one anchored on the 3rd is owed in every week
	// that contains a 3rd, and the run in such a week saw the spawned turn
	// among the rows it asserts are empty.
	anchor := board.AddDays(board.MondayOf(board.TodayIso()), 10)
	tpl, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Technical article", Description: "1500 words, one code sample",
		Recurrence: "month", Start: anchor, Team: "alpha", Assignee: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if TaskTitle(tpl) != "Technical article" || TaskDescription(tpl) != "1500 words, one code sample" {
		t.Fatalf("task packs title+body in its description; got %q", tpl.Description)
	}
	b, _ := svc.Board(ctx, "acme")
	if len(board.TasksOf(b, "Articles")) != 1 || len(b.Cards) != 0 {
		t.Fatalf("the task must be out of the card rows; cards=%d tasks=%d", len(b.Cards), len(b.Tasks))
	}
	// A process with tasks is protected; rename carries the tasks.
	if err := svc.DeleteProcess(ctx, "acme", "Articles"); !errors.Is(err, ErrProcessInUse) {
		t.Fatalf("a process with tasks must be protected, got %v", err)
	}
	if err := svc.RenameProcess(ctx, "acme", "Articles", "Publishing"); err != nil {
		t.Fatal(err)
	}
	if got := fake.get(tpl.ItemID); got.Process != "Publishing" {
		t.Fatalf("the task must follow the rename, got %q", got.Process)
	}
}

// The calendar is the clock: a monthly task anchored on 3 March is due
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
// card stays as it is, and the next one is the task again. A task due
// this week also hands its card over at once — waiting for someone to carry
// the week is how a new process looked broken.
func TestSpawnCopiesTheTemplateNotThePreviousIteration(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())
	next := board.AddDays(week, 7)
	tpl, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Technical article", Description: "the brief",
		Recurrence: "week", Start: week, Team: "alpha", Assignee: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme")
	its := board.Iterations(b, tpl.ItemID)
	if len(its) != 1 {
		t.Fatalf("a task due this week files its card at once; iterations = %d", len(its))
	}
	first := its[0]
	if first.Title != "Technical article" || first.Description != "the brief" ||
		first.Plan != board.PlanFri || first.Week != week || first.Team != "alpha" ||
		first.Stage != board.StageRecurrent || len(first.Assignees) != 1 || first.Assignees[0] != "writer" {
		t.Fatalf("iteration = %+v", first)
	}

	// The team renames and finishes it…
	if err := svc.Rename(ctx, "acme", first.ItemID, "Article about etcd"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProgress(ctx, "acme", first.ItemID, 100); err != nil {
		t.Fatal(err)
	}
	// …and the next week's iteration is the task again, not the rename.
	b, _ = svc.Board(ctx, "acme")
	if n, err := svc.SpawnIterations(ctx, b, "alpha", next, false); err != nil || n != 1 {
		t.Fatalf("next week: spawned %d, err %v", n, err)
	}
	b, _ = svc.Board(ctx, "acme")
	its = board.Iterations(b, tpl.ItemID)
	if len(its) != 2 {
		t.Fatalf("iterations = %d", len(its))
	}
	if its[0].Title != "Article about etcd" {
		t.Fatalf("the live card must keep its rename, got %q", its[0].Title)
	}
	if its[1].Title != "Technical article" {
		t.Fatalf("the next iteration must come from the task, got %q", its[1].Title)
	}
}

// An open iteration holds the next one back — the stuck card is the process,
// and it goes overdue — unless the task accumulates. And a week never
// gets a second iteration of the same task.
func TestSpawnWaitsForTheOpenIterationUnlessAccumulating(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())
	next := board.AddDays(week, 7)
	waits, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Article", Recurrence: "week", Start: week, Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	piles, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Invoice client X", Recurrence: "week", Start: week, Team: "alpha", Accumulate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme")
	if len(board.Iterations(b, waits.ItemID)) != 1 || len(board.Iterations(b, piles.ItemID)) != 1 {
		t.Fatalf("each task files this week's card on creation")
	}
	// Re-running this week is a no-op.
	if n, _ := svc.SpawnIterations(ctx, b, "alpha", week, true); n != 0 {
		t.Fatalf("re-running a week must spawn nothing, got %d", n)
	}
	// Nobody finished either. Next week only the accumulating one spawns.
	if n, _ := svc.SpawnIterations(ctx, b, "alpha", next, false); n != 1 {
		t.Fatalf("only the accumulating task spawns while open, got %d", n)
	}
	b, _ = svc.Board(ctx, "acme")
	if len(board.Iterations(b, waits.ItemID)) != 1 || len(board.Iterations(b, piles.ItemID)) != 2 {
		t.Fatalf("waits=%d piles=%d", len(board.Iterations(b, waits.ItemID)), len(board.Iterations(b, piles.ItemID)))
	}
}

// The calendar is the clock, and only the calendar: a monthly task is due
// in the week its anchor's day falls in, and in no other week. Built directly
// on the board, so the check is the arithmetic and nothing else.
func TestMonthlyTemplateIsDueOnceAMonth(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Marketing"},
		{ItemID: "pr1", Title: board.ProcessStateTitle, Process: "Articles", Project: "Marketing"},
		{ItemID: "tpl", Title: board.ProcessTaskTitle, Process: "Articles",
			Description: "Monthly", Recurrence: "month", StartDate: "2026-03-03", Team: "alpha"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
	svc := New(fake)
	ctx := context.Background()
	b, _ := svc.Board(ctx, "acme")
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

// An open iteration is NOT carried into the next week: the week it was owed
// is the record, and the task decides whether the next week gets its own.
func TestIterationsStayInTheirWeek(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())
	if _, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Article", Recurrence: "week", Start: week, Team: "alpha",
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := svc.CarryWeek(ctx, "acme", "alpha", board.AddDays(week, 7), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 0 {
		t.Fatalf("an open iteration must not be carried, got carried=%d", rep.Carried)
	}
	b, _ := svc.Board(ctx, "acme")
	for _, c := range b.Cards {
		if c.Task != "" && c.Week != week {
			t.Fatalf("the iteration moved to %s; it belongs to the week it was owed", c.Week)
		}
	}
}

// A turn with an owner is dated across its week, so it reaches that person's
// day board — Me is "what I do now", and a card with a name on it belongs
// there. A turn nobody owns stays dateless and waits in the team's plan.
func TestOwnedIterationsAreDated(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())
	owned, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Owned", Recurrence: "week", Start: week, Team: "alpha", Assignee: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	loose, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Unowned", Recurrence: "week", Start: week, Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme")

	its := board.Iterations(b, owned.ItemID)
	if len(its) != 1 {
		t.Fatalf("owned iterations = %d", len(its))
	}
	if its[0].StartDate != week || its[0].Day != board.AddDays(week, 6) {
		t.Fatalf("an owned turn spans its week, got %s..%s", its[0].StartDate, its[0].Day)
	}
	// …and the owner finds it on their day board.
	mine := board.MeView(b, "writer", board.TodayIso())
	found := false
	for _, c := range mine {
		if c.ItemID == its[0].ItemID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the owner's day board must carry their turn; got %d cards", len(mine))
	}

	its = board.Iterations(b, loose.ItemID)
	if len(its) != 1 || its[0].StartDate != "" || its[0].Day != "" {
		t.Fatalf("an unowned turn stays dateless, got %+v", its)
	}
}

// A Project slot with an owner reaches that person's day board as well: it is
// their work, and it shows across the days its own dates cover. An unowned
// slot stays on the Project board, where a plan belongs.
func TestOwnedSlotsShowOnTheDayBoard(t *testing.T) {
	today := board.TodayIso()
	b := board.NewBoard([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "P"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "E", Project: "P"},
		{ItemID: "owned", Title: "owned slot", Epic: "E", Project: "P",
			StartDate: board.AddDays(today, -3), Day: board.AddDays(today, 10),
			Assignees: []string{"writer"}},
		{ItemID: "loose", Title: "unowned slot", Epic: "E", Project: "P",
			StartDate: board.AddDays(today, -3), Day: board.AddDays(today, 10)},
	})
	mine := board.MeView(b, "writer", today)
	if len(mine) != 1 || mine[0].ItemID != "owned" {
		t.Fatalf("the owner's day board must carry their slot and nothing else; got %+v", mine)
	}
	if got := board.MeView(b, "", today); len(got) != 1 {
		t.Fatalf("an unowned slot stays off the day boards; got %d", len(got))
	}
}

// Handing a turn to somebody else works the way reassigning a review does: a
// card nobody has touched is deleted rather than passed along, and the new
// owner always gets a fresh one — carrying this week's turn, dated, so it is
// on their day board.
func TestReassigningATurnReplacesTheUntouchedCard(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())
	task, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Article", Recurrence: "month", Start: week, Team: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme")
	first := board.Iterations(b, task.ItemID)
	if len(first) != 1 || len(first[0].Assignees) != 0 || first[0].StartDate != "" {
		t.Fatalf("the first turn starts unowned and dateless, got %+v", first)
	}
	// Renamed, but not started: nobody has put work into it.
	if err := svc.Rename(ctx, "acme", first[0].ItemID, "Article about etcd"); err != nil {
		t.Fatal(err)
	}
	who, team := "writer", "alpha"
	if err := svc.UpdateProcessTask(ctx, "acme", task.ItemID, TaskPatch{
		Assignee: &who, Team: &team,
	}); err != nil {
		t.Fatal(err)
	}
	b, _ = svc.Board(ctx, "acme")
	its := board.Iterations(b, task.ItemID)
	if len(its) != 1 {
		t.Fatalf("the untouched card is replaced, not added to; got %d", len(its))
	}
	got := its[0]
	if got.ItemID == first[0].ItemID {
		t.Fatal("the untouched card should have been deleted")
	}
	if got.Title != "Article" || got.Team != team ||
		len(got.Assignees) != 1 || got.Assignees[0] != who {
		t.Fatalf("the fresh turn comes from the task, for the new owner; got %+v", got)
	}
	if got.StartDate != week || got.Day != board.AddDays(week, 6) {
		t.Fatalf("an owned turn is dated across its week, got %s..%s", got.StartDate, got.Day)
	}
	if !contains(board.MeView(b, who, board.TodayIso()), got.ItemID) {
		t.Fatal("the owner's day board must carry it")
	}
}

// Work already started stays with the person who started it — only the new
// owner's fresh turn is added.
func TestReassigningKeepsAStartedCard(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())
	first := "alice"
	task, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Article", Recurrence: "month", Start: week, Team: "alpha", Assignee: first,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme")
	started := board.Iterations(b, task.ItemID)[0]
	if err := svc.SetProgress(ctx, "acme", started.ItemID, 40); err != nil {
		t.Fatal(err)
	}
	second := "bob"
	if err := svc.UpdateProcessTask(ctx, "acme", task.ItemID, TaskPatch{Assignee: &second}); err != nil {
		t.Fatal(err)
	}
	b, _ = svc.Board(ctx, "acme")
	its := board.Iterations(b, task.ItemID)
	if len(its) != 2 {
		t.Fatalf("the started card stays and the new owner gets one; got %d", len(its))
	}
	var kept, fresh board.Card
	for _, c := range its {
		if c.ItemID == started.ItemID {
			kept = c
		} else {
			fresh = c
		}
	}
	if kept.ItemID == "" || len(kept.Assignees) != 1 || kept.Assignees[0] != first || kept.Progress != 40 {
		t.Fatalf("work in progress stays with whoever did it, got %+v", kept)
	}
	if len(fresh.Assignees) != 1 || fresh.Assignees[0] != second {
		t.Fatalf("the new owner's turn, got %+v", fresh)
	}
}

func contains(cards []board.Card, id string) bool {
	for _, c := range cards {
		if c.ItemID == id {
			return true
		}
	}
	return false
}

// A task named like a log line keeps its name: the draft body parser files
// "[anything] text" as a log entry, so a title written bare would vanish and
// the task would come back nameless.
func TestBracketedTaskTitleSurvives(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	task, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "[Urgent] Invoice client X", Description: "the usual template",
		Recurrence: "week", Start: board.MondayOf(board.TodayIso()), Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := fake.get(task.ItemID).Description
	if strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Fatalf("a bare bracketed first line is note-shaped: %q", body)
	}
	stored := board.Card{Description: body}
	if got := TaskTitle(stored); got != "[Urgent] Invoice client X" {
		t.Fatalf("title = %q", got)
	}
	if got := TaskDescription(stored); got != "the usual template" {
		t.Fatalf("description = %q", got)
	}
	// And a body written before the mark existed still reads.
	old := board.Card{Description: "Plain title\nand its text"}
	if TaskTitle(old) != "Plain title" || TaskDescription(old) != "and its text" {
		t.Fatalf("legacy bodies must still read: %q / %q", TaskTitle(old), TaskDescription(old))
	}
}

// The invariant holds at every door, not just the stage menu: "in progress"
// and "send to review" used to walk around it, and the Process tab's own
// un-tick button was one of them.
func TestATurnKeepsItsMarkerWhicheverDoor(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())
	task, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Article", Recurrence: "week", Start: week, Team: "alpha", Assignee: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme")
	turn := board.Iterations(b, task.ItemID)[0]

	for _, door := range []struct {
		name string
		call func() error
	}{
		{"the stage menu", func() error { return svc.SetStage(ctx, "acme", turn.ItemID, board.StageReview) }},
		{"in progress", func() error { return svc.SetInProgress(ctx, "acme", turn.ItemID) }},
	} {
		if err := door.call(); !errors.Is(err, ErrInvalidStage) {
			t.Errorf("%s: err = %v, want the turn to keep its marker", door.name, err)
		}
		if got := fake.get(turn.ItemID); got.Stage != board.StageRecurrent {
			t.Fatalf("%s left the turn on stage %q", door.name, got.Stage)
		}
	}
	// Done is allowed, and reopening by progress keeps the marker.
	if err := svc.SetStage(ctx, "acme", turn.ItemID, board.StageDone); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProgress(ctx, "acme", turn.ItemID, 90); err != nil {
		t.Fatal(err)
	}
	if got := fake.get(turn.ItemID); got.Stage != board.StageRecurrent || got.Progress != 90 {
		t.Fatalf("a reopened turn = stage %q progress %d", got.Stage, got.Progress)
	}
}

// Deleting a task frees the turns it leaves behind: they keep their record but
// stop pointing at a task that is gone, and can be moved off recurrent again.
func TestDeletingATaskFreesItsTurns(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	week := board.MondayOf(board.TodayIso())
	task, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Article", Recurrence: "week", Start: week, Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Board(ctx, "acme")
	turn := board.Iterations(b, task.ItemID)[0]
	if err := svc.DeleteProcessTask(ctx, "acme", task.ItemID); err != nil {
		t.Fatal(err)
	}
	got := fake.get(turn.ItemID)
	if got == nil {
		t.Fatal("the turn is the record of work done and must stay")
	}
	if got.Task != "" || got.Recurrence != "" {
		t.Fatalf("a freed turn keeps no dead link: task=%q recurrence=%q", got.Task, got.Recurrence)
	}
	if err := svc.SetInProgress(ctx, "acme", turn.ItemID); err != nil {
		t.Fatalf("a freed turn must move like any card: %v", err)
	}
}

// A slot only visits a weekly plan; the × there takes it out of the plan and
// never deletes the roadmap card, whether or not anyone has touched it.
func TestThePlanCrossNeverDeletesASlot(t *testing.T) {
	today := board.TodayIso()
	week := board.MondayOf(today)
	newBoard := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "p1", Title: board.ProjectStateTitle, Project: "P"},
			{ItemID: "e1", Title: board.EpicStateTitle, Epic: "E", Project: "P"},
			{ItemID: "slot", Title: "a roadmap slot", Epic: "E", Project: "P",
				StartDate: today, Day: board.AddDays(today, 30), Week: week,
				Team: "alpha", Plan: board.PlanFri},
		}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	}
	for _, c := range []struct {
		name string
		call func(*Service) error
	}{
		{"the plan ×", func(s *Service) error { return s.Remove(context.Background(), "acme", "slot", "plan") }},
		{"release from plan", func(s *Service) error { return s.ReleaseFromPlan(context.Background(), "acme", "slot") }},
	} {
		fake := newBoard()
		svc := New(fake)
		if err := c.call(svc); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := fake.get("slot")
		if got == nil {
			t.Fatalf("%s deleted the slot", c.name)
		}
		if got.Plan != board.PlanNone {
			t.Errorf("%s left the band %q", c.name, got.Plan)
		}
		if got.Week != week || got.Epic != "E" || got.StartDate != today {
			t.Errorf("%s changed the slot: week %q epic %q start %q", c.name, got.Week, got.Epic, got.StartDate)
		}
	}
}

// Re-dating a slot is planning: one nobody started stays out of the sprints,
// and one somebody is working on keeps the sprint they are working in.
func TestRedatingASlotLeavesItsSprintAlone(t *testing.T) {
	today := board.TodayIso()
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "P"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "E", Project: "P"},
		{ItemID: "started", Title: "in work", Epic: "E", Project: "P", Team: "alpha",
			StartDate: today, Day: board.AddDays(today, 7), SprintStart: today, Progress: 40},
		{ItemID: "planned", Title: "not yet", Epic: "E", Project: "P", Team: "alpha",
			StartDate: today, Day: board.AddDays(today, 7)},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)
	ctx := context.Background()
	later := board.AddDays(today, 21)
	for _, id := range []string{"started", "planned"} {
		if err := svc.SetDates(ctx, "acme", id, later, board.AddDays(later, 4)); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.get("started"); got.SprintStart != today {
		t.Errorf("a started slot must keep its sprint, got %q", got.SprintStart)
	}
	if got := fake.get("planned"); got.SprintStart != "" {
		t.Errorf("a slot nobody started must stay out of the sprints, got %q", got.SprintStart)
	}
}

// A title is one line: a pasted newline used to split it silently, so the
// task came back named after its first word and the rest became its body.
func TestATaskTitleIsOneLine(t *testing.T) {
	fake := processBoard()
	svc := New(fake)
	ctx := context.Background()
	task, err := svc.AddProcessTask(ctx, "acme", "Articles", TaskArgs{
		Title: "Write the\nmonthly report", Description: "one page",
		Recurrence: "month", Start: board.MondayOf(board.TodayIso()), Team: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := fake.get(task.ItemID)
	if got := TaskTitle(*stored); got != "Write the monthly report" {
		t.Fatalf("title = %q", got)
	}
	if got := TaskDescription(*stored); got != "one page" {
		t.Fatalf("description = %q — the title's tail leaked into it", got)
	}
}

// A task whose process was deleted upstream stops filing turns: nobody can
// see it, pause it or delete it from the board, so it must not keep putting
// work into people's weeks.
func TestAnOrphanedTaskStopsFiling(t *testing.T) {
	today := board.TodayIso()
	week := board.MondayOf(today)
	b := board.NewBoard([]board.Card{
		{ItemID: "task", Title: board.ProcessTaskTitle, Process: "Ghost",
			Description: "# Orphan", Recurrence: "week", StartDate: week, Team: "alpha"},
	})
	fake := newFake(b.Cards, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)
	if n, err := svc.SpawnIterations(context.Background(), b, "alpha", week, true); err != nil || n != 0 {
		t.Fatalf("spawned %d (err %v), want none", n, err)
	}
}
