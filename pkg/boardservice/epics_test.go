package boardservice

import (
	"context"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func epicBoard() *fakeBackend {
	return newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra", Project: "Cozystack"},
		{ItemID: "c1", Team: "alpha", Title: "vGPU setup", Epic: "Infra", Project: "Cozystack", Week: "2026-08-17",
			StartDate: "2026-08-17", Day: "2026-08-28"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
}

// An epic is declared by its hidden state card — the team-roster mechanism —
// and duplicates are refused case-insensitively.
func TestAddEpic(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.AddEpic(context.Background(), "acme", 1, "Console", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	last := fake.creates[len(fake.creates)-1]
	if last.Title != board.EpicStateTitle || last.Epic != "Console" || last.Project != "Cozystack" {
		t.Fatalf("epic-state create = %+v", last)
	}
	if err := svc.AddEpic(context.Background(), "acme", 1, "infra", "Cozystack"); !errors.Is(err, ErrEpicExists) {
		t.Fatalf("a case-insensitive duplicate must be refused, got %v", err)
	}
	if err := svc.AddEpic(context.Background(), "acme", 1, "  ", "Cozystack"); err == nil {
		t.Fatal("an empty name must be refused")
	}
	// A typo is refused; the no-project bucket is a deliberate destination.
	if err := svc.AddEpic(context.Background(), "acme", 1, "Loose", "Ghost"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown project must be refused, got %v", err)
	}
	if err := svc.AddEpic(context.Background(), "acme", 1, "Loose", ""); err != nil {
		t.Fatalf("the no-project bucket is a real destination: %v", err)
	}
}

// A project is declared by its own hidden state card, and is deletable only
// while it owns no columns — detaching planned work silently is the anti-goal.
func TestProjectLifecycle(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.AddProject(context.Background(), "acme", 1, "Portal"); err != nil {
		t.Fatal(err)
	}
	last := fake.creates[len(fake.creates)-1]
	if last.Title != board.ProjectStateTitle || last.Project != "Portal" {
		t.Fatalf("project-state create = %+v", last)
	}
	if err := svc.AddProject(context.Background(), "acme", 1, "cozystack"); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("a case-insensitive duplicate must be refused, got %v", err)
	}
	if err := svc.AddProject(context.Background(), "acme", 1, " "); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if err := svc.DeleteProject(context.Background(), "acme", 1, "Cozystack"); !errors.Is(err, ErrProjectInUse) {
		t.Fatalf("a project owning columns must be protected, got %v", err)
	}
	// Detach the column, and the project becomes deletable.
	if err := svc.SetEpicProject(context.Background(), "acme", 1, "Cozystack", "Infra", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteProject(context.Background(), "acme", 1, "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if fake.count("DeleteCard p1") == 0 {
		t.Fatalf("the state card must be deleted; log=%v", fake.log)
	}
}

// Moving a column between projects rewrites the epic-state card, not the cards
// under it: a card's project always follows its epic.
func TestSetEpicProject(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.AddProject(context.Background(), "acme", 1, "Portal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEpicProject(context.Background(), "acme", 1, "Cozystack", "Infra", "Ghost"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown target project must be refused, got %v", err)
	}
	if err := svc.SetEpicProject(context.Background(), "acme", 1, "Cozystack", "Nope", "Portal"); !errors.Is(err, ErrEpicNotFound) {
		t.Fatalf("an unknown column must be refused, got %v", err)
	}
	if err := svc.SetEpicProject(context.Background(), "acme", 1, "Cozystack", "Infra", "Portal"); err != nil {
		t.Fatal(err)
	}
	if fake.count("SetProject e1 Portal") == 0 {
		t.Fatalf("the state card must be rewritten; log=%v", fake.log)
	}
	// The card rides along: it names the (project, epic) pair, so leaving it
	// behind would file it under a column that no longer exists.
	if c := fake.get("c1"); c.Project != "Portal" {
		t.Fatalf("the column's cards must follow it, got %q", c.Project)
	}
}

// Deleting an epic with cards is protected; an empty one removes its state card.
func TestDeleteEpic(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.DeleteEpic(context.Background(), "acme", 1, "Infra", "Cozystack"); !errors.Is(err, ErrEpicInUse) {
		t.Fatalf("an epic with cards must be protected, got %v", err)
	}
	if err := svc.SetEpic(context.Background(), "acme", 1, "c1", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEpic(context.Background(), "acme", 1, "Infra", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if fake.count("DeleteCard e1") == 0 {
		t.Fatalf("the state card must be deleted; log=%v", fake.log)
	}
}

// Filing a card under an epic validates the column exists — a typo must not
// mint a phantom column.
func TestSetEpicValidates(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.SetEpic(context.Background(), "acme", 1, "c1", "Nope", nil); err == nil {
		t.Fatal("an unknown epic must be refused")
	}
	if err := svc.SetEpic(context.Background(), "acme", 1, "c1", "Infra", nil); err != nil {
		t.Fatalf("a no-op re-file must pass: %v", err)
	}
}

// An epic card is created on the Project board: filed under its column, anchored
// to its week, spanning its dates, and joining NO sprint — so it stays off
// the day boards until a team takes it up.
func TestCreateCardUnderEpic(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	card, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Title: "KMS encryption", Epic: "Infra", Project: "Cozystack",
		Start: "2026-09-14", Day: "2026-09-25",
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.Epic != "Infra" || card.Week != "2026-09-14" {
		t.Fatalf("epic/week = %q/%q", card.Epic, card.Week)
	}
	if card.SprintStart != "" {
		t.Fatalf("an epic card must join no sprint, got %q", card.SprintStart)
	}
	if card.StartDate != "2026-09-14" || card.Day != "2026-09-25" {
		t.Fatalf("span = %q..%q", card.StartDate, card.Day)
	}
	if _, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Title: "typo", Epic: "Nope", Project: "Cozystack",
	}); err == nil {
		t.Fatal("an unknown epic must be refused on create")
	}

	// And the day boards do not smear its multi-week span.
	b, err := svc.Board(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := board.TeamGrid(b, "", "2026-09-15"); len(got) != 0 {
		t.Fatalf("a sprint-less epic card must stay off the day grid, got %+v", got)
	}
}

// The board parses the hidden state cards into the ordered project and column
// rosters, and binds each column to its project.
func TestBoardParsesProjectsAndEpics(t *testing.T) {
	b := board.NewBoard(nil, []board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e2", Title: board.EpicStateTitle, Epic: "Console", Project: "Cozystack"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra", Project: "Cozystack"},
		{ItemID: "e3", Title: board.EpicStateTitle, Epic: "Loose"},
		{ItemID: "c1", Title: "работа", Epic: "Infra", Project: "Cozystack"},
	})
	if len(b.Epics) != 3 || b.Epics[0].Name != "Console" || b.Epics[1].Name != "Infra" {
		t.Fatalf("epics = %v (board order = state-card positions)", b.Epics)
	}
	if col, ok := board.FindEpic(b, "Cozystack", "Infra"); !ok || col.ItemID != "e1" {
		t.Fatalf("FindEpic = %+v / %v", col, ok)
	}
	if len(b.Projects) != 1 || b.Projects[0] != "Cozystack" || b.ProjectStates["Cozystack"] != "p1" {
		t.Fatalf("projects = %v / %v", b.Projects, b.ProjectStates)
	}
	if got := board.EpicsOf(b, "Cozystack"); len(got) != 2 || got[0].Name != "Console" {
		t.Fatalf("EpicsOf = %v, want the project's columns in board order", got)
	}
	// A column naming no project belongs to none — never silently adopted.
	if _, ok := board.FindEpic(b, "Cozystack", "Loose"); ok {
		t.Fatal("a column with no project must not be adopted by one")
	}
	if got := board.EpicsOf(b, ""); len(got) != 1 || got[0].Name != "Loose" {
		t.Fatalf("the no-project bucket = %v", got)
	}
	// The same column name in two projects is two columns, not a clash.
	two := board.NewBoard(nil, []board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "A"},
		{ItemID: "p2", Title: board.ProjectStateTitle, Project: "B"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Docs", Project: "A"},
		{ItemID: "e2", Title: board.EpicStateTitle, Epic: "Docs", Project: "B"},
	})
	if len(two.Epics) != 2 {
		t.Fatalf("same name in two projects = two columns, got %v", two.Epics)
	}
	if a, _ := board.FindEpic(two, "A", "Docs"); a.ItemID != "e1" {
		t.Fatalf("FindEpic(A, Docs) = %+v", a)
	}
	if len(b.Cards) != 1 {
		t.Fatalf("state cards must be split out of Cards, got %d", len(b.Cards))
	}
}

// Handing an epic card to a team files it into the team's WEEKLY plan (band +
// week carry it there); it must NOT join today's sprint — a multi-week slot
// in the current sprint would smear across the team's whole day grid.
func TestSetTeamKeepsEpicCardPlanLevel(t *testing.T) {
	today := board.TodayIso()
	fake := newFake([]board.Card{
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra", Project: "Cozystack"},
		{ItemID: "slot", Title: "vGPU rollout", Epic: "Infra", Project: "Cozystack", Week: board.MondayOf(today),
			StartDate: board.MondayOf(today), Day: board.AddDays(board.MondayOf(today), 18)},
		{ItemID: "day", Title: "ordinary", Team: "", StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)

	if err := svc.SetTeam(context.Background(), "acme", 1, "slot", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	got := fake.get("slot")
	if got.Team != "alpha" {
		t.Fatalf("team = %q", got.Team)
	}
	if got.SprintStart != "" {
		t.Fatalf("an epic card must stay out of the sprint on team assignment, got %q", got.SprintStart)
	}

	// An ordinary card still joins the team's sprint — the old behaviour.
	if err := svc.SetTeam(context.Background(), "acme", 1, "day", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("day"); got.SprintStart != today {
		t.Fatalf("an ordinary card joins the team sprint, got %q", got.SprintStart)
	}

	// An epic card ALREADY in work follows the normal rule (it has a sprint).
	if err := svc.SetSprintStart(context.Background(), "acme", 1, "slot", today); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTeam(context.Background(), "acme", 1, "slot", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("slot"); got.SprintStart != today {
		t.Fatalf("an in-work epic card keeps sprint semantics, got %q", got.SprintStart)
	}
}

// Filing under a column that does not exist is a rejected input, not an
// upstream failure: the API must answer 422 (and say which name), so a typo
// reads as a typo instead of "the server is broken".
func TestUnknownEpicIsTyped(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	err := svc.SetEpic(context.Background(), "acme", 1, "c1", "Ghost", nil)
	if !errors.Is(err, ErrEpicNotFound) {
		t.Fatalf("SetEpic error = %v, want ErrEpicNotFound", err)
	}
	_, err = svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{Title: "x", Epic: "Ghost", Project: "Cozystack"})
	if !errors.Is(err, ErrEpicNotFound) {
		t.Fatalf("CreateCard error = %v, want ErrEpicNotFound", err)
	}
}

// Column names are unique WITHIN a project: every project gets its own "Docs",
// and renaming one carries its cards along.
func TestEpicNamesAreScopedToTheirProject(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.AddProject(context.Background(), "acme", 1, "Portal"); err != nil {
		t.Fatal(err)
	}
	// "Infra" already exists in Cozystack; in Portal it is a different column.
	if err := svc.AddEpic(context.Background(), "acme", 1, "Infra", "Portal"); err != nil {
		t.Fatalf("the same name in another project must be allowed: %v", err)
	}
	if err := svc.AddEpic(context.Background(), "acme", 1, "infra", "Portal"); !errors.Is(err, ErrEpicExists) {
		t.Fatalf("a duplicate INSIDE the project must be refused, got %v", err)
	}
	// The card under Cozystack/Infra must not be dragged along by Portal/Infra.
	if err := svc.DeleteEpic(context.Background(), "acme", 1, "Infra", "Portal"); err != nil {
		t.Fatalf("Portal's empty column must delete cleanly: %v", err)
	}
	if err := svc.DeleteEpic(context.Background(), "acme", 1, "Infra", "Cozystack"); !errors.Is(err, ErrEpicInUse) {
		t.Fatalf("Cozystack's column has a card and must be protected, got %v", err)
	}
}

// Renaming a column rewrites its state card and every card filed under it —
// cards store the name, so the two cannot drift apart.
func TestRenameEpic(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.RenameEpic(context.Background(), "acme", 1, "Cozystack", "Nope", "X"); !errors.Is(err, ErrEpicNotFound) {
		t.Fatalf("an unknown column must be refused, got %v", err)
	}
	if err := svc.RenameEpic(context.Background(), "acme", 1, "Cozystack", "Infra", " "); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if err := svc.RenameEpic(context.Background(), "acme", 1, "Cozystack", "Infra", "Platform"); err != nil {
		t.Fatal(err)
	}
	if fake.count("SetEpic e1 Platform") == 0 {
		t.Fatalf("the state card must be renamed; log=%v", fake.log)
	}
	if c := fake.get("c1"); c.Epic != "Platform" {
		t.Fatalf("the column's cards must follow the rename, got %q", c.Epic)
	}
}

// Renaming a project carries its columns and their cards.
func TestRenameProject(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.RenameProject(context.Background(), "acme", 1, "Ghost", "X"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown project must be refused, got %v", err)
	}
	if err := svc.RenameProject(context.Background(), "acme", 1, "Cozystack", "Cozy"); err != nil {
		t.Fatal(err)
	}
	if fake.count("SetProject p1 Cozy") == 0 || fake.count("SetProject e1 Cozy") == 0 {
		t.Fatalf("the project and its columns must be rewritten; log=%v", fake.log)
	}
	if c := fake.get("c1"); c.Project != "Cozy" {
		t.Fatalf("the cards must follow the project rename, got %q", c.Project)
	}
}

// A slot that slipped is a debt, and a debt is not moved: carry-week counts
// it and leaves both its boundaries alone. The end date is the very thing
// that says it slipped — stretching it to the target week, as carry-week
// once did, made the board forget anything had.
func TestCarryWeekLeavesASlippedSlotAlone(t *testing.T) {
	thisWeek := board.MondayOf(board.TodayIso())
	twoBack := board.AddDays(thisWeek, -14)
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "P"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "E", Project: "P"},
		{ItemID: "slot", Title: "slipped", Epic: "E", Project: "P", Team: "alpha", Plan: board.PlanFri,
			Week: twoBack, StartDate: twoBack, Day: board.AddDays(twoBack, 4)},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
	svc := New(fake)
	rep, err := svc.CarryWeek(context.Background(), "acme", 1, "alpha", thisWeek, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Carried != 1 {
		t.Fatalf("the debt is counted: carried = %d", rep.Carried)
	}
	got := fake.get("slot")
	if got.Week != twoBack || got.StartDate != twoBack || got.Day != board.AddDays(twoBack, 4) {
		t.Fatalf("the slot moved: week %s start %s end %s", got.Week, got.StartDate, got.Day)
	}
	// …and it shows on this week's panel as the debt it is.
	b, _ := svc.Board(context.Background(), "acme", 1)
	now := board.WeeklyPlan(b, "alpha", thisWeek)
	if len(now.Fri) != 1 || now.Fri[0].ItemID != "slot" {
		t.Fatalf("the slipped slot must show on the current week's panel; got %+v", now)
	}
}

// A deadline belongs to a project: two projects can both have one on the same
// week, and dragging a project's line onto its own other line merges them.
func TestDeadlines(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	ctx := context.Background()
	if err := svc.AddProject(ctx, "acme", 1, "Portal"); err != nil {
		t.Fatal(err)
	}
	const w1, w2 = "2026-09-07", "2026-09-14"
	// Any day of the week resolves to its Monday — the line sits on a row.
	if err := svc.AddDeadline(ctx, "acme", 1, "2026-09-09", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddDeadline(ctx, "acme", 1, w1, "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddDeadline(ctx, "acme", 1, w1, "Portal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddDeadline(ctx, "acme", 1, w2, "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddDeadline(ctx, "acme", 1, w1, "Ghost"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown project must be refused, got %v", err)
	}
	b, err := svc.Board(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Deadlines) != 3 {
		t.Fatalf("deadlines = %+v, want one per (project, week) pair", b.Deadlines)
	}

	// Dragging Cozystack's second line onto its first leaves one — and does
	// not touch Portal's line on that same week.
	if err := svc.MoveDeadline(ctx, "acme", 1, "Cozystack", w2, w1); err != nil {
		t.Fatal(err)
	}
	b, err = svc.Board(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Deadlines) != 2 {
		t.Fatalf("two of one project on one week are one; got %+v", b.Deadlines)
	}
	if _, ok := board.FindDeadline(b, "Portal", w1); !ok {
		t.Fatalf("another project's deadline must survive the merge; got %+v", b.Deadlines)
	}

	if err := svc.DeleteDeadline(ctx, "acme", 1, w1, "Portal"); err != nil {
		t.Fatal(err)
	}
	b, err = svc.Board(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := board.FindDeadline(b, "Portal", w1); ok {
		t.Fatalf("the deleted line is still there: %+v", b.Deadlines)
	}
}

// The row of a Project-board slot is its start date's week — always, and
// without anyone having to say so twice. This is the bug that prompted the
// rule: cards created in one week, re-dated to another, stayed in the row they
// were created in because the week was a second value nobody updated.
func TestSlotWeekFollowsItsStart(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "acme", 1, CreateCardArgs{
		Title: "Workshop", Epic: "Infra", Project: "Cozystack",
		Start: "2026-08-24", Day: "2026-08-28",
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.Week != "2026-08-24" {
		t.Fatalf("created week = %q, want the start's Monday", card.Week)
	}

	// Re-date it to an earlier week: the row must move with the dates.
	if err := svc.SetDates(ctx, "acme", 1, card.ItemID, "2026-08-06", "2026-08-06"); err != nil {
		t.Fatal(err)
	}
	got := fake.get(card.ItemID)
	if got.Week != "2026-08-03" {
		t.Fatalf("week = %q, want the Monday of 2026-08-06", got.Week)
	}
	// ...and re-dating a plan slot must not drag it into a sprint.
	if got.SprintStart != "" {
		t.Fatalf("a slot that was never taken into work joined sprint %q", got.SprintStart)
	}

	// The same through the single-date path.
	if err := svc.SetStart(ctx, "acme", 1, card.ItemID, "2026-09-16"); err != nil {
		t.Fatal(err)
	}
	if got := fake.get(card.ItemID); got.Week != "2026-09-14" {
		t.Fatalf("week = %q after SetStart, want 2026-09-14", got.Week)
	}
}

// Setting a slot's week by hand is refused: there is nothing to set, and
// accepting a value here is how the two came to disagree. A weekly-plan card
// — which has no dates at all — still moves between weeks.
func TestSlotWeekIsNotSettable(t *testing.T) {
	today := board.TodayIso()
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra", Project: "Cozystack"},
		{ItemID: "slot", Title: "a slot", Epic: "Infra", Project: "Cozystack",
			StartDate: "2026-08-24", Day: "2026-08-28", Week: "2026-08-24"},
		{ItemID: "plan", Title: "a weekly-plan card", Team: "alpha",
			Plan: board.PlanFri, Week: board.MondayOf(today)},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)
	ctx := context.Background()

	err := svc.SetWeek(ctx, "acme", 1, "slot", "2026-09-07")
	if !errors.Is(err, ErrWeekDerived) {
		t.Fatalf("SetWeek on a slot = %v, want ErrWeekDerived", err)
	}
	if got := fake.get("slot"); got.Week != "2026-08-24" {
		t.Fatalf("the refused write still landed: week = %q", got.Week)
	}
	if err := svc.SetWeek(ctx, "acme", 1, "plan", "2026-09-07"); err != nil {
		t.Fatalf("a weekly-plan card must still move between weeks: %v", err)
	}
	if got := fake.get("plan"); got.Week != "2026-09-07" {
		t.Fatalf("plan card week = %q", got.Week)
	}
}

// A board read repairs the rows of cards written before the rule, so nobody
// has to migrate anything: the dates are the truth, whatever is stored.
func TestBoardRepairsStaleSlotWeeks(t *testing.T) {
	b := board.NewBoard(nil, []board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "P"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "E", Project: "P"},
		// Written by the old code: dates moved, the week stayed behind.
		{ItemID: "c1", Title: "stale", Epic: "E", Project: "P",
			StartDate: "2026-08-06", Day: "2026-08-07", Week: "2026-08-24"},
		// No start at all: nothing to derive from, so the week stands.
		{ItemID: "c2", Title: "dateless", Epic: "E", Project: "P", Week: "2026-08-24"},
		// Not a slot: a weekly-plan card keeps its own week.
		{ItemID: "c3", Title: "plan", Plan: board.PlanFri, Week: "2026-08-24"},
	})
	byID := map[string]board.Card{}
	for _, c := range b.Cards {
		byID[c.ItemID] = c
	}
	if got := byID["c1"].Week; got != "2026-08-03" {
		t.Fatalf("stale slot week = %q, want the Monday of its start", got)
	}
	if got := byID["c2"].Week; got != "2026-08-24" {
		t.Fatalf("a slot with no start keeps its week, got %q", got)
	}
	if got := byID["c3"].Week; got != "2026-08-24" {
		t.Fatalf("a weekly-plan card must be left alone, got %q", got)
	}
}

// Handing a slot to a team files it in that team's weekly plan, whichever door
// the change came through — the frontend used to add the band on its own, so
// the same assignment made over MCP left the card in nobody's plan.
func TestTeamFilesASlotInTheWeeklyPlan(t *testing.T) {
	today := board.TodayIso()
	week := board.MondayOf(today)
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra", Project: "Cozystack"},
		{ItemID: "slot", Title: "a slot", Epic: "Infra", Project: "Cozystack",
			StartDate: today, Day: today, Week: week},
		{ItemID: "day", Title: "an ordinary card", StartDate: today, Day: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)
	ctx := context.Background()

	if err := svc.SetTeam(ctx, "acme", 1, "slot", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	got := fake.get("slot")
	if got.Plan != board.PlanFri {
		t.Fatalf("band = %q, want the slot filed in the weekly plan", got.Plan)
	}
	if got.SprintStart != "" {
		t.Fatalf("filing a slot in a plan must not start it, got sprint %q", got.SprintStart)
	}
	b, err := svc.Board(ctx, "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	bands := board.WeeklyPlan(b, "alpha", week)
	if len(bands.Fri) != 1 || bands.Fri[0].ItemID != "slot" {
		t.Fatalf("the slot must show in alpha's plan for its week; got %+v", bands)
	}

	// Taking the team away takes an unstarted slot back out of the plan.
	if err := svc.SetTeam(ctx, "acme", 1, "slot", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("slot"); got.Plan != board.PlanNone {
		t.Fatalf("band = %q, want the slot out of the plan again", got.Plan)
	}

	// An ordinary day card is untouched by any of this.
	if err := svc.SetTeam(ctx, "acme", 1, "day", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("day"); got.Plan != board.PlanNone {
		t.Fatalf("an ordinary card must not be filed in the weekly plan, got %q", got.Plan)
	}
}

// SetWeek on a slot: re-asserting the week the slot already derives from its
// start date is a harmless no-op — the SPA and API writers echo the visible
// week back — while a CONFLICTING week is refused; accepting it is exactly
// how a slot's week and dates came to disagree. Ungrouping a slot subtask
// used to die on this: the pull-out wrote the PARENT's week.
func TestSetWeekOnASlot(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "s1", Title: "slot", Epic: "E", Project: "P", Team: "t",
			StartDate: "2026-08-25", Week: "2026-08-24", Day: "2026-08-28"},
	}, nil)
	svc := New(fake)
	ctx := context.Background()
	// The derived week (the start date's Monday) is accepted silently.
	if err := svc.SetWeek(ctx, "o", 1, "s1", "2026-08-24"); err != nil {
		t.Fatalf("re-asserting the derived week: %v", err)
	}
	// Any other week is refused.
	if err := svc.SetWeek(ctx, "o", 1, "s1", "2026-08-03"); !errors.Is(err, ErrWeekDerived) {
		t.Fatalf("a conflicting week got %v, want ErrWeekDerived", err)
	}
}
