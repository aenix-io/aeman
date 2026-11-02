package boardservice

import (
	"context"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func scopeBoard() *fakeBackend {
	return newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e1", Title: board.ScopeStateTitle, Scope: "Infra", Project: "Cozystack"},
		{ItemID: "c1", Team: "alpha", Title: "vGPU setup", Scope: "Infra", Project: "Cozystack", Week: "2026-08-17",
			StartDate: "2026-08-17", Day: "2026-08-28"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
}

// An scope is declared by its hidden state card — the team-roster mechanism —
// and duplicates are refused case-insensitively.
func TestAddScope(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	if err := svc.AddScope(context.Background(), "acme", "Console", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	last := fake.creates[len(fake.creates)-1]
	if last.Title != board.ScopeStateTitle || last.Scope != "Console" || last.Project != "Cozystack" {
		t.Fatalf("scope-state create = %+v", last)
	}
	if err := svc.AddScope(context.Background(), "acme", "infra", "Cozystack"); !errors.Is(err, ErrScopeExists) {
		t.Fatalf("a case-insensitive duplicate must be refused, got %v", err)
	}
	if err := svc.AddScope(context.Background(), "acme", "  ", "Cozystack"); err == nil {
		t.Fatal("an empty name must be refused")
	}
	// A typo is refused; the no-project bucket is a deliberate destination.
	if err := svc.AddScope(context.Background(), "acme", "Loose", "Ghost"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown project must be refused, got %v", err)
	}
	if err := svc.AddScope(context.Background(), "acme", "Loose", ""); err != nil {
		t.Fatalf("the no-project bucket is a real destination: %v", err)
	}
}

// A project is declared by its own hidden state card, and is deletable only
// while it owns no columns — detaching planned work silently is the anti-goal.
func TestProjectLifecycle(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	if err := svc.AddProject(context.Background(), "acme", "Portal"); err != nil {
		t.Fatal(err)
	}
	last := fake.creates[len(fake.creates)-1]
	if last.Title != board.ProjectStateTitle || last.Project != "Portal" {
		t.Fatalf("project-state create = %+v", last)
	}
	if err := svc.AddProject(context.Background(), "acme", "cozystack"); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("a case-insensitive duplicate must be refused, got %v", err)
	}
	if err := svc.AddProject(context.Background(), "acme", " "); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if err := svc.DeleteProject(context.Background(), "acme", "Cozystack"); !errors.Is(err, ErrProjectInUse) {
		t.Fatalf("a project owning columns must be protected, got %v", err)
	}
	// Detach the column, and the project becomes deletable.
	if err := svc.SetScopeProject(context.Background(), "acme", "Cozystack", "Infra", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteProject(context.Background(), "acme", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if fake.count("DeleteCard p1") == 0 {
		t.Fatalf("the state card must be deleted; log=%v", fake.log)
	}
}

// Moving a column between projects rewrites the scope-state card, not the cards
// under it: a card's project always follows its scope.
func TestSetScopeProject(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	if err := svc.AddProject(context.Background(), "acme", "Portal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetScopeProject(context.Background(), "acme", "Cozystack", "Infra", "Ghost"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown target project must be refused, got %v", err)
	}
	if err := svc.SetScopeProject(context.Background(), "acme", "Cozystack", "Nope", "Portal"); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("an unknown column must be refused, got %v", err)
	}
	if err := svc.SetScopeProject(context.Background(), "acme", "Cozystack", "Infra", "Portal"); err != nil {
		t.Fatal(err)
	}
	if fake.count("SetProject e1 Portal") == 0 {
		t.Fatalf("the state card must be rewritten; log=%v", fake.log)
	}
	// The card rides along: it names the (project, scope) pair, so leaving it
	// behind would file it under a column that no longer exists.
	if c := fake.get("c1"); c.Project != "Portal" {
		t.Fatalf("the column's cards must follow it, got %q", c.Project)
	}
}

// Deleting an scope with cards is protected; an empty one removes its state card.
func TestDeleteScope(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	if err := svc.DeleteScope(context.Background(), "acme", "Infra", "Cozystack"); !errors.Is(err, ErrScopeInUse) {
		t.Fatalf("an scope with cards must be protected, got %v", err)
	}
	if err := svc.SetScope(context.Background(), "acme", "c1", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteScope(context.Background(), "acme", "Infra", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if fake.count("DeleteCard e1") == 0 {
		t.Fatalf("the state card must be deleted; log=%v", fake.log)
	}
}

// Filing a card under an scope validates the column exists — a typo must not
// mint a phantom column.
func TestSetScopeValidates(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	if err := svc.SetScope(context.Background(), "acme", "c1", "Nope", nil); err == nil {
		t.Fatal("an unknown scope must be refused")
	}
	if err := svc.SetScope(context.Background(), "acme", "c1", "Infra", nil); err != nil {
		t.Fatalf("a no-op re-file must pass: %v", err)
	}
}

// An scope card is created on the Project board: filed under its column, anchored
// to its week, spanning its dates, and joining NO sprint — so it stays off
// the day boards until a team takes it up.
func TestCreateCardUnderScope(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	card, err := svc.CreateCard(context.Background(), "acme", CreateCardArgs{
		Title: "KMS encryption", Scope: "Infra", Project: "Cozystack",
		Start: "2026-09-14", Day: "2026-09-25",
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.Scope != "Infra" {
		t.Fatalf("scope = %q", card.Scope)
	}
	// A project card's dates are its row; it takes no week and joins no
	// sprint, since neither the weekly plan nor a day board draws it (H4).
	if card.Week != "" || card.SprintStart != "" {
		t.Fatalf("a project card is on no weekly plan and in no sprint, got week %q sprint %q", card.Week, card.SprintStart)
	}
	if card.StartDate != "2026-09-14" || card.Day != "2026-09-25" {
		t.Fatalf("span = %q..%q", card.StartDate, card.Day)
	}
	if _, err := svc.CreateCard(context.Background(), "acme", CreateCardArgs{
		Title: "typo", Scope: "Nope", Project: "Cozystack",
	}); err == nil {
		t.Fatal("an unknown scope must be refused on create")
	}

	// And it is the Project board's alone: on no day of the day grid, whether
	// its span covers the day or not.
	b, err := svc.Board(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range []string{"2026-09-07", "2026-09-15", "2026-09-25"} {
		for _, c := range board.TeamGrid(b, "", day) {
			if c.ItemID == card.ItemID {
				t.Fatalf("a project card stands on the day grid on %s; it is the Project board's", day)
			}
		}
	}
}

// The board parses the hidden state cards into the ordered project and column
// rosters, and binds each column to its project.
func TestBoardParsesProjectsAndScopes(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e2", Title: board.ScopeStateTitle, Scope: "Console", Project: "Cozystack"},
		{ItemID: "e1", Title: board.ScopeStateTitle, Scope: "Infra", Project: "Cozystack"},
		{ItemID: "e3", Title: board.ScopeStateTitle, Scope: "Loose"},
		{ItemID: "c1", Title: "работа", Scope: "Infra", Project: "Cozystack"},
	})
	if len(b.Scopes) != 3 || b.Scopes[0].Name != "Console" || b.Scopes[1].Name != "Infra" {
		t.Fatalf("scopes = %v (board order = state-card positions)", b.Scopes)
	}
	if col, ok := board.FindScope(b, "Cozystack", "Infra"); !ok || col.ItemID != "e1" {
		t.Fatalf("FindScope = %+v / %v", col, ok)
	}
	if len(b.Projects) != 1 || b.Projects[0] != "Cozystack" || b.ProjectStates["Cozystack"] != "p1" {
		t.Fatalf("projects = %v / %v", b.Projects, b.ProjectStates)
	}
	if got := board.ScopesOf(b, "Cozystack"); len(got) != 2 || got[0].Name != "Console" {
		t.Fatalf("ScopesOf = %v, want the project's columns in board order", got)
	}
	// A column naming no project belongs to none — never silently adopted.
	if _, ok := board.FindScope(b, "Cozystack", "Loose"); ok {
		t.Fatal("a column with no project must not be adopted by one")
	}
	if got := board.ScopesOf(b, ""); len(got) != 1 || got[0].Name != "Loose" {
		t.Fatalf("the no-project bucket = %v", got)
	}
	// The same column name in two projects is two columns, not a clash.
	two := board.NewBoard([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "A"},
		{ItemID: "p2", Title: board.ProjectStateTitle, Project: "B"},
		{ItemID: "e1", Title: board.ScopeStateTitle, Scope: "Docs", Project: "A"},
		{ItemID: "e2", Title: board.ScopeStateTitle, Scope: "Docs", Project: "B"},
	})
	if len(two.Scopes) != 2 {
		t.Fatalf("same name in two projects = two columns, got %v", two.Scopes)
	}
	if a, _ := board.FindScope(two, "A", "Docs"); a.ItemID != "e1" {
		t.Fatalf("FindScope(A, Docs) = %+v", a)
	}
	if len(b.Cards) != 1 {
		t.Fatalf("state cards must be split out of Cards, got %d", len(b.Cards))
	}
}

// Handing an scope card to a team files it into the team's WEEKLY plan (band +
// week carry it there); it must NOT join today's sprint — a multi-week slot
// in the current sprint would smear across the team's whole day grid.
// A PROJECT card stands in its column alone (H4, docs/design/hierarchy.md):
// every door that would make it somebody's work in hand — a team, an owner,
// a week of the weekly plan, a place on the Triage grid, a review — is
// refused, and refused before anything is written. Taking a stale team or
// owner OFF one (a card written before schema 2) is allowed: that is the
// state the migration produces, reached by hand.
func TestAProjectCardTakesNoTeamNoOwnerNoWeek(t *testing.T) {
	today := board.TodayIso()
	week := board.MondayOf(today)
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e1", Title: board.ScopeStateTitle, Scope: "Infra", Project: "Cozystack"},
		{ItemID: "plan", Title: "vGPU rollout", Scope: "Infra", Project: "Cozystack",
			StartDate: week, Day: board.AddDays(week, 18)},
		// Written before schema 2: a team, an owner and a week it may no
		// longer carry.
		{ItemID: "legacy", Title: "an old slot", Scope: "Infra", Project: "Cozystack",
			Team: "alpha", Assignees: []string{"kvaps"}, Week: week, StartDate: week, Day: week},
		{ItemID: "task", Title: "ordinary", Team: "alpha", StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)

	refused := map[string]error{
		"a team":      svc.SetTeam(ctx, "acme", "plan", "alpha", ""),
		"an owner":    svc.SetAssignee(ctx, "acme", "plan", "kvaps"),
		"a week":      svc.SetWeek(ctx, "acme", "plan", week),
		"a place":     svc.Place(ctx, "acme", "plan", week),
		"an untriage": svc.Untriage(ctx, "acme", "plan"),
		"a review":    func() error { _, err := svc.SendToReview(ctx, "acme", "plan", "bob", "", ""); return err }(),
		"a reviewOf":  svc.SetReviewOf(ctx, "acme", "plan", "task"),
		"the shelf":   svc.SetBacklog(ctx, "acme", "plan", true),
	}
	for what, err := range refused {
		if what == "the shelf" {
			if !errors.Is(err, ErrNotYoursToPark) {
				t.Errorf("parking a project card = %v, want ErrNotYoursToPark", err)
			}
			continue
		}
		if !errors.Is(err, ErrProjectCard) {
			t.Errorf("giving a project card %s = %v, want ErrProjectCard", what, err)
		}
	}
	got := fake.get("plan")
	if got.Team != "" || len(got.Assignees) != 0 || got.Week != "" || got.SprintStart != "" || got.ReviewOf != "" {
		t.Fatalf("a refused write still landed: %+v", got)
	}

	// Clearing is the other direction, and is allowed.
	if err := svc.SetTeam(ctx, "acme", "legacy", "", ""); err != nil {
		t.Fatalf("taking a stale team off a project card: %v", err)
	}
	if err := svc.SetAssignee(ctx, "acme", "legacy", ""); err != nil {
		t.Fatalf("taking a stale owner off a project card: %v", err)
	}
	if err := svc.SetWeek(ctx, "acme", "legacy", ""); err != nil {
		t.Fatalf("taking a stale week off a project card: %v", err)
	}
	if got := fake.get("legacy"); got.Team != "" || len(got.Assignees) != 0 || got.Week != "" || got.SprintStart != "" {
		t.Fatalf("the stale fields stayed: %+v", got)
	}

	// An ordinary card still joins the team's sprint on assignment.
	if err := svc.SetTeam(ctx, "acme", "task", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("task"); got.SprintStart != today {
		t.Fatalf("an ordinary card joins the team sprint, got %q", got.SprintStart)
	}
}

// A project card is BORN that way: the create door refuses a team, an owner,
// a parent or a review link together with a column, whichever board asked
// (the Project board's own add-box through CreateInView, a raw CreateCard
// from an embedder or an agent).
func TestAProjectCardIsMadeWithoutATeamOrAnOwner(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	base := CreateCardArgs{Title: "KMS encryption", Scope: "Infra", Project: "Cozystack", Start: "2026-09-14"}
	for what, args := range map[string]CreateCardArgs{
		"a team":   with(base, func(a *CreateCardArgs) { a.Team = "alpha" }),
		"an owner": with(base, func(a *CreateCardArgs) { a.Assignee = "kvaps" }),
		"a parent": with(base, func(a *CreateCardArgs) { a.Parent = "c1" }),
		"a review": with(base, func(a *CreateCardArgs) { a.ReviewOf = "c1" }),
	} {
		if _, err := svc.CreateCard(ctx, "acme", args); !errors.Is(err, ErrProjectCard) {
			t.Errorf("creating a project card with %s = %v, want ErrProjectCard", what, err)
		}
		if _, err := svc.CreateInView(ctx, "acme", board.ViewProject, args); !errors.Is(err, ErrNotOnThisBoard) {
			t.Errorf("the Project board's add-box with %s = %v, want ErrNotOnThisBoard", what, err)
		}
	}
	card, err := svc.CreateCard(ctx, "acme", base)
	if err != nil {
		t.Fatal(err)
	}
	if card.Team != "" || len(card.Assignees) != 0 || card.Parent != "" || card.Week != "" || card.SprintStart != "" {
		t.Fatalf("a project card is born with none of those: %+v", card)
	}
}

func with[T any](v T, f func(*T)) T {
	f(&v)
	return v
}

// Filing a card INTO a column takes it off the plans it was on: a card
// scheduled for a week and nothing else takes that week's span as its dates
// — Monday to Friday, the whole of the week it was owed in, so it lands in
// the column with a row — and its week and sprint go, since neither the
// weekly plan nor a day board draws a project card (H4). A card that has
// dates of its own keeps them. One that carries a team, an owner or a
// parent is refused: it is somebody's work in hand, and the person takes
// those off first.
func TestFilingACardIntoAColumnTakesItOffThePlansItWasOn(t *testing.T) {
	today := board.TodayIso()
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e1", Title: board.ScopeStateTitle, Scope: "Infra", Project: "Cozystack"},
		{ItemID: "planned", Title: "of a week", Week: "2026-08-24"},
		{ItemID: "dated", Title: "with dates", Week: "2026-08-24", StartDate: "2026-09-07", Day: "2026-09-09", SprintStart: today},
		{ItemID: "teamed", Title: "a team's", Team: "alpha", Week: "2026-08-24"},
		{ItemID: "owned", Title: "somebody's", Assignees: []string{"kvaps"}, Week: "2026-08-24"},
		{ItemID: "parent", Title: "a parent"},
		{ItemID: "child", Title: "a subtask", Parent: "parent"},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)
	project := "Cozystack"

	if err := svc.SetScope(ctx, "acme", "planned", "Infra", &project); err != nil {
		t.Fatal(err)
	}
	if c := fake.get("planned"); c.StartDate != "2026-08-24" || c.Day != "2026-08-28" || c.Week != "" {
		t.Fatalf("a card of a week takes that week's span and leaves the plan: %+v", c)
	}
	if err := svc.SetScope(ctx, "acme", "dated", "Infra", &project); err != nil {
		t.Fatal(err)
	}
	if c := fake.get("dated"); c.StartDate != "2026-09-07" || c.Day != "2026-09-09" || c.Week != "" || c.SprintStart != "" {
		t.Fatalf("a dated card keeps its dates and leaves the week and the sprint: %+v", c)
	}
	for _, id := range []string{"teamed", "owned", "child"} {
		if err := svc.SetScope(ctx, "acme", id, "Infra", &project); !errors.Is(err, ErrProjectCard) {
			t.Errorf("filing %s into a column = %v, want ErrProjectCard", id, err)
		}
		if c := fake.get(id); c.Scope != "" {
			t.Errorf("the refused filing still landed on %s", id)
		}
	}
}

// A project card is NOBODY'S subtask and HAS none (H4): a subtask rides its
// parent's team, person and sprint onto the day boards, and a project card
// has none of those to give or to take. Both directions are refused at the
// group door and at the create door.
func TestAProjectCardIsNobodysSubtaskAndHasNone(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "Cozystack"},
		{ItemID: "e1", Title: board.ScopeStateTitle, Scope: "Infra", Project: "Cozystack"},
		{ItemID: "plan", Title: "the plan", Scope: "Infra", Project: "Cozystack", StartDate: "2026-08-24", Day: "2026-08-28"},
		{ItemID: "task", Title: "a task", Team: "alpha"},
		{ItemID: "other", Title: "another task", Team: "alpha"},
	}, nil)
	svc := New(fake)
	if err := svc.SetParent(ctx, "acme", "plan", "task"); !errors.Is(err, ErrProjectCard) {
		t.Fatalf("grouping a project card under a task = %v, want ErrProjectCard", err)
	}
	if err := svc.SetParent(ctx, "acme", "task", "plan"); !errors.Is(err, ErrProjectCard) {
		t.Fatalf("grouping a task under a project card = %v, want ErrProjectCard", err)
	}
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{Title: "a piece", Parent: "plan", Team: "alpha"}); !errors.Is(err, ErrProjectCard) {
		t.Fatalf("creating a card under a project card = %v, want ErrProjectCard", err)
	}
	for _, id := range []string{"plan", "task"} {
		if c := fake.get(id); c.Parent != "" {
			t.Fatalf("a refused grouping still landed on %s", id)
		}
	}
	// Ordinary grouping is untouched.
	if err := svc.SetParent(ctx, "acme", "other", "task"); err != nil {
		t.Fatal(err)
	}
}

// Filing under a column that does not exist is a rejected input, not an
// upstream failure: the API must answer 422 (and say which name), so a typo
// reads as a typo instead of "the server is broken".
func TestUnknownScopeIsTyped(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	err := svc.SetScope(context.Background(), "acme", "c1", "Ghost", nil)
	if !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("SetScope error = %v, want ErrScopeNotFound", err)
	}
	_, err = svc.CreateCard(context.Background(), "acme", CreateCardArgs{Title: "x", Scope: "Ghost", Project: "Cozystack"})
	if !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("CreateCard error = %v, want ErrScopeNotFound", err)
	}
}

// Column names are unique WITHIN a project: every project gets its own "Docs",
// and renaming one carries its cards along.
func TestScopeNamesAreScopedToTheirProject(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	if err := svc.AddProject(context.Background(), "acme", "Portal"); err != nil {
		t.Fatal(err)
	}
	// "Infra" already exists in Cozystack; in Portal it is a different column.
	if err := svc.AddScope(context.Background(), "acme", "Infra", "Portal"); err != nil {
		t.Fatalf("the same name in another project must be allowed: %v", err)
	}
	if err := svc.AddScope(context.Background(), "acme", "infra", "Portal"); !errors.Is(err, ErrScopeExists) {
		t.Fatalf("a duplicate INSIDE the project must be refused, got %v", err)
	}
	// The card under Cozystack/Infra must not be dragged along by Portal/Infra.
	if err := svc.DeleteScope(context.Background(), "acme", "Infra", "Portal"); err != nil {
		t.Fatalf("Portal's empty column must delete cleanly: %v", err)
	}
	if err := svc.DeleteScope(context.Background(), "acme", "Infra", "Cozystack"); !errors.Is(err, ErrScopeInUse) {
		t.Fatalf("Cozystack's column has a card and must be protected, got %v", err)
	}
}

// Renaming a column rewrites its state card and every card filed under it —
// cards store the name, so the two cannot drift apart.
func TestRenameScope(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	if err := svc.RenameScope(context.Background(), "acme", "Cozystack", "Nope", "X"); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("an unknown column must be refused, got %v", err)
	}
	if err := svc.RenameScope(context.Background(), "acme", "Cozystack", "Infra", " "); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if err := svc.RenameScope(context.Background(), "acme", "Cozystack", "Infra", "Platform"); err != nil {
		t.Fatal(err)
	}
	if fake.count("SetScope e1 Platform") == 0 {
		t.Fatalf("the state card must be renamed; log=%v", fake.log)
	}
	if c := fake.get("c1"); c.Scope != "Platform" {
		t.Fatalf("the column's cards must follow the rename, got %q", c.Scope)
	}
}

// Renaming a project carries its columns and their cards.
func TestRenameProject(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	if err := svc.RenameProject(context.Background(), "acme", "Ghost", "X"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown project must be refused, got %v", err)
	}
	if err := svc.RenameProject(context.Background(), "acme", "Cozystack", "Cozy"); err != nil {
		t.Fatal(err)
	}
	if fake.count("SetProject p1 Cozy") == 0 || fake.count("SetProject e1 Cozy") == 0 {
		t.Fatalf("the project and its columns must be rewritten; log=%v", fake.log)
	}
	if c := fake.get("c1"); c.Project != "Cozy" {
		t.Fatalf("the cards must follow the project rename, got %q", c.Project)
	}
}

// A deadline belongs to a project: two projects can both have one on the same
// week, and dragging a project's line onto its own other line merges them.
func TestDeadlines(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	ctx := context.Background()
	if err := svc.AddProject(ctx, "acme", "Portal"); err != nil {
		t.Fatal(err)
	}
	const w1, w2 = "2026-09-07", "2026-09-14"
	// Any day of the week resolves to its Monday — the line sits on a row.
	if err := svc.AddDeadline(ctx, "acme", "2026-09-09", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddDeadline(ctx, "acme", w1, "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddDeadline(ctx, "acme", w1, "Portal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddDeadline(ctx, "acme", w2, "Cozystack"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddDeadline(ctx, "acme", w1, "Ghost"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("an unknown project must be refused, got %v", err)
	}
	b, err := svc.Board(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Deadlines) != 3 {
		t.Fatalf("deadlines = %+v, want one per (project, week) pair", b.Deadlines)
	}

	// Dragging Cozystack's second line onto its first leaves one — and does
	// not touch Portal's line on that same week.
	if err := svc.MoveDeadline(ctx, "acme", "Cozystack", w2, w1); err != nil {
		t.Fatal(err)
	}
	b, err = svc.Board(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Deadlines) != 2 {
		t.Fatalf("two of one project on one week are one; got %+v", b.Deadlines)
	}
	if _, ok := board.FindDeadline(b, "Portal", w1); !ok {
		t.Fatalf("another project's deadline must survive the merge; got %+v", b.Deadlines)
	}

	if err := svc.DeleteDeadline(ctx, "acme", w1, "Portal"); err != nil {
		t.Fatal(err)
	}
	b, err = svc.Board(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := board.FindDeadline(b, "Portal", w1); ok {
		t.Fatalf("the deleted line is still there: %+v", b.Deadlines)
	}
}

// Re-dating a project card moves its row on the Project board and nothing
// else: it takes no week (no weekly plan draws it) and joins no sprint (no
// day board does), through every door that moves dates — the calendar, the
// single-date path and a defer. The week used to be derived from the start
// and synced on every re-date; with no board reading it there is nothing to
// keep in step.
func TestReDatingAProjectCardMovesItsRowAndNothingElse(t *testing.T) {
	fake := scopeBoard()
	svc := New(fake)
	card, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "Workshop", Scope: "Infra", Project: "Cozystack",
		Start: "2026-08-24", Day: "2026-08-28",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDates(ctx, "acme", card.ItemID, "2026-08-06", "2026-08-06"); err != nil {
		t.Fatal(err)
	}
	if got := fake.get(card.ItemID); got.StartDate != "2026-08-06" || got.Week != "" || got.SprintStart != "" {
		t.Fatalf("the calendar moves the dates alone: %+v", got)
	}
	if err := svc.SetStart(ctx, "acme", card.ItemID, "2026-09-16"); err != nil {
		t.Fatal(err)
	}
	if got := fake.get(card.ItemID); got.StartDate != "2026-09-16" || got.Week != "" {
		t.Fatalf("SetStart moves the start alone: %+v", got)
	}
	if err := svc.Defer(ctx, "acme", card.ItemID, 7); err != nil {
		t.Fatal(err)
	}
	if got := fake.get(card.ItemID); got.Week != "" || got.SprintStart != "" {
		t.Fatalf("a defer joins a project card to no sprint: %+v", got)
	}
}
