package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The hierarchy's admission: what a planning card may be born as, which
// parent a card may take, and what a planning card never carries
// (docs/design/hierarchy.md, H1 H2 H3 H5 H6 H7).

func hierarchySeed(today string) *fakeBackend {
	return newFake([]board.Card{
		{ItemID: "goal", Title: "Reach Europe", Type: board.TypeGoal},
		{ItemID: "init", Title: "Portugal", Type: board.TypeInitiative, Parent: "goal"},
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Parent: "init"},
		{ItemID: "task", Title: "Invoices", Parent: "epic", Team: "alpha", Assignees: []string{"dev"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "loose", Title: "Loose", Team: "alpha", Assignees: []string{"dev"},
			StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today}})
}

func TestAPlanningCardIsBornOnTheRoadmapAndNowhereElse(t *testing.T) {
	today := board.TodayIso()
	ctx := WithActor(ctx, "kvaps")

	t.Run("the Roadmap makes a planning card: a type, dates, a parent — no owner, no team, no sprint", func(t *testing.T) {
		f := hierarchySeed(today)
		c, err := f2svc(f).CreateInView(ctx, "acme", board.ViewRoadmap, CreateCardArgs{
			Title: "Launch", Type: board.TypeInitiative, Parent: "goal",
			Start: today, Day: board.AddDays(today, 60),
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.Type != board.TypeInitiative || c.Parent != "goal" || c.Team != "" || len(c.Assignees) != 0 {
			t.Fatalf("card = %+v", c)
		}
		if c.SprintStart != "" || c.Zone != board.ZoneNone || c.Size != "" {
			t.Fatalf("a planning card joins no sprint, no band and no size: %+v", c)
		}
		if c.StartDate != today || c.Day != board.AddDays(today, 60) {
			t.Fatalf("dates = %s..%s", c.StartDate, c.Day)
		}
	})

	// A goal, an initiative or an epic is nobody's WORK: the people doing it
	// are on the tasks under it, so an ASSIGNEE is refused — by name at the
	// board's own door, as the card at the raw door. (Its team and its owner
	// — who carries it out, who answers for it — it may be born with, O1:
	// ownership_test.go.)
	t.Run("a planning card has no assignee", func(t *testing.T) {
		for what, args := range map[string]CreateCardArgs{
			"an assignee": {Title: "Launch", Type: board.TypeInitiative, Assignee: "po"},
		} {
			svc := f2svc(hierarchySeed(today))
			if _, err := svc.CreateInView(ctx, "acme", board.ViewRoadmap, args); !errors.Is(err, ErrNotOnThisBoard) {
				t.Errorf("the Roadmap's add-box with %s = %v, want ErrNotOnThisBoard", what, err)
			}
			if _, err := svc.CreateCard(ctx, "acme", args); !errors.Is(err, ErrPlanningCard) {
				t.Errorf("a planning card born with %s = %v, want ErrPlanningCard", what, err)
			}
		}
	})

	t.Run("the Roadmap needs a type, or a parent to put a task under", func(t *testing.T) {
		f := hierarchySeed(today)
		_, err := f2svc(f).CreateInView(ctx, "acme", board.ViewRoadmap, CreateCardArgs{Title: "x"})
		if !errors.Is(err, ErrViewNeedsField) {
			t.Fatalf("err = %v, want ErrViewNeedsField", err)
		}
	})

	// The epic's +: a TASK born on the Roadmap, under a planning card, with
	// a title and a parent and nothing a day board gives — no team, no owner,
	// no day it was not given, no sprint — so it is PARKED in the no-team
	// group's backlog (K3), and stands on the Roadmap under its parent.
	// Dates, when given, are kept, and then it is in work, not parked.
	t.Run("a task is born under a planning card from the Roadmap", func(t *testing.T) {
		f := hierarchySeed(today)
		svc := f2svc(f)
		c, err := svc.CreateInView(ctx, "acme", board.ViewRoadmap, CreateCardArgs{Title: "Receipts", Parent: "epic"})
		if err != nil {
			t.Fatal(err)
		}
		if c.Type != board.TypeTask || c.Parent != "epic" {
			t.Fatalf("card = %+v", c)
		}
		if c.Team != "" || len(c.Assignees) != 0 || c.Day != "" || c.StartDate != "" || c.SprintStart != "" {
			t.Fatalf("a Roadmap-born task has nothing a day board gives: %+v", c)
		}
		if !c.Parked {
			t.Fatalf("an undated Roadmap-born task is parked in the no-team backlog (K3): %+v", c)
		}
		if st := f.b.SprintStates[""]; st.Current != "" {
			t.Fatalf("a Roadmap-born task minted a sprint for the no-team group: %+v", st)
		}
		dated, err := svc.CreateInView(ctx, "acme", board.ViewRoadmap, CreateCardArgs{Title: "Dated", Parent: "epic", Start: today, Day: board.AddDays(today, 3)})
		if err != nil {
			t.Fatal(err)
		}
		if dated.StartDate != today || dated.Day != board.AddDays(today, 3) || dated.SprintStart != "" {
			t.Fatalf("dates given are kept, and still no sprint: %+v", dated)
		}
		for what, mutate := range map[string]func(*CreateCardArgs){
			"a team":    func(a *CreateCardArgs) { a.Team = "alpha" },
			"an owner":  func(a *CreateCardArgs) { a.Assignee = "dev" },
			"a band":    func(a *CreateCardArgs) { a.Zone = board.ZoneYellow },
			"a size":    func(a *CreateCardArgs) { a.Size = board.SizeM },
			"the shelf": func(a *CreateCardArgs) { a.Parked = true },
			"a review":  func(a *CreateCardArgs) { a.ReviewOf = "loose" },
			"a sprint":  func(a *CreateCardArgs) { a.SprintStart = today },
		} {
			args := CreateCardArgs{Title: "x", Parent: "epic"}
			mutate(&args)
			if _, err := svc.CreateInView(ctx, "acme", board.ViewRoadmap, args); !errors.Is(err, ErrNotOnThisBoard) {
				t.Errorf("a Roadmap task with %s = %v, want ErrNotOnThisBoard", what, err)
			}
		}
		// Under a TASK it would be a subtask, which is the day boards' gesture.
		if _, err := svc.CreateInView(ctx, "acme", board.ViewRoadmap, CreateCardArgs{Title: "x", Parent: "loose"}); err == nil {
			t.Error("a task under a task from the Roadmap must not be born as a subtask of nobody's board")
		}
	})

	t.Run("no day board makes a planning card", func(t *testing.T) {
		for _, v := range []board.View{board.ViewMe, board.ViewTeam, board.ViewBacklog} {
			f := hierarchySeed(today)
			_, err := f2svc(f).CreateInView(ctx, "acme", v, CreateCardArgs{
				Title: "x", Type: board.TypeEpic, Team: "alpha",
			})
			if !errors.Is(err, ErrNotOnThisBoard) {
				t.Errorf("%s: err = %v, want ErrNotOnThisBoard", v, err)
			}
		}
	})

	t.Run("the raw door refuses an unknown type and a planning card with a task's state", func(t *testing.T) {
		f := hierarchySeed(today)
		if _, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{Title: "x", Type: "widget"}); !errors.Is(err, ErrUnknownType) {
			t.Fatalf("widget: %v, want ErrUnknownType", err)
		}
		for _, args := range []CreateCardArgs{
			{Title: "x", Type: board.TypeGoal, Parked: true},
			{Title: "x", Type: board.TypeGoal, ReviewOf: "task"},
			{Title: "x", Type: board.TypeGoal, SprintStart: today},
		} {
			if _, err := f2svc(f).CreateCard(ctx, "acme", args); !errors.Is(err, ErrPlanningCard) {
				t.Errorf("%+v: err = %v, want ErrPlanningCard", args, err)
			}
		}
		if f.count("CreateCard") != 0 {
			t.Fatal("a refused create writes nothing")
		}
	})
}

// A parent is of a HIGHER level than its child; a task may skip levels; two
// tasks are a subtask and nothing else; a project card may sit under a
// planning card and never under a task (H2, H4).
func TestTheLevelRuleGovernsTheParent(t *testing.T) {
	today := board.TodayIso()
	ctx := WithActor(ctx, "kvaps")
	set := func(t *testing.T, f *fakeBackend, child, parent string) error {
		t.Helper()
		return f2svc(f).SetParent(ctx, "acme", child, parent)
	}

	t.Run("up is allowed, across and down are not", func(t *testing.T) {
		f := hierarchySeed(today)
		// A task straight under the goal: levels may be skipped.
		if err := set(t, f, "loose", "goal"); err != nil {
			t.Fatalf("task under goal: %v", err)
		}
		// An epic under a goal, skipping the initiative.
		if err := set(t, f, "epic", "goal"); err != nil {
			t.Fatalf("epic under goal: %v", err)
		}
		for _, tc := range []struct{ child, parent string }{
			{"init", "epic"}, // down
			{"epic", "epic"}, // itself (the level rule and the depth rule both say no)
			{"goal", "init"}, // a goal has no parent
			{"init", "task"}, // a planning card under a task
		} {
			err := set(t, f, tc.child, tc.parent)
			if !errors.Is(err, ErrParentLevel) && !errors.Is(err, ErrSubtaskDepth) {
				t.Errorf("%s under %s: %v, want the level rule", tc.child, tc.parent, err)
			}
		}
		// Two epics are the same level: not up.
		f2 := hierarchySeed(today)
		f2.b.Cards = append(f2.b.Cards, board.Card{ItemID: "epic2", Type: board.TypeEpic})
		if err := set(t, f2, "epic2", "epic"); !errors.Is(err, ErrParentLevel) {
			t.Errorf("epic under epic: %v, want ErrParentLevel", err)
		}
	})

	t.Run("a task under an epic is not a subtask: nothing else moves", func(t *testing.T) {
		f := hierarchySeed(today)
		if err := set(t, f, "loose", "epic"); err != nil {
			t.Fatal(err)
		}
		c := f.get("loose")
		if c.Parent != "epic" {
			t.Fatalf("parent = %q", c.Parent)
		}
		// The epic's owner is "po"; the task keeps "dev", its team and its sprint.
		if len(c.Assignees) != 1 || c.Assignees[0] != "dev" || c.Team != "alpha" || c.SprintStart != today {
			t.Fatalf("a hierarchy link inherits nothing: %+v", c)
		}
		// And the EPIC has nothing to hand down: it takes no owner at all, and
		// the task under it keeps its own.
		if err := f2svc(f).SetAssignee(ctx, "acme", "epic", "lead"); !errors.Is(err, ErrPlanningCard) {
			t.Fatalf("an owner on the epic: %v, want ErrPlanningCard", err)
		}
		if got := f.get("task").Assignees; len(got) != 1 || got[0] != "dev" {
			t.Fatalf("the task under the epic lost its owner: %v", got)
		}
		// Nor does the epic's progress derive from the task's.
		if err := f2svc(f).SetProgress(ctx, "acme", "task", 50); err != nil {
			t.Fatal(err)
		}
		if f.get("epic").Progress != 0 {
			t.Fatal("a planning card's progress is derived on read, never written")
		}
	})

	t.Run("a task under a task is still a subtask, with everything that means", func(t *testing.T) {
		f := hierarchySeed(today)
		f.b.Cards = append(f.b.Cards, board.Card{ItemID: "other", Title: "Other", Team: "beta", Assignees: []string{"x"}})
		if err := set(t, f, "other", "loose"); err != nil {
			t.Fatal(err)
		}
		c := f.get("other")
		if c.Team != "alpha" || c.Assignees[0] != "dev" {
			t.Fatalf("a subtask takes its parent's team and person: %+v", c)
		}
		// A subtask's parent — a task under an epic — is not itself a
		// subtask, so the depth rule allows it.
		if err := set(t, f, "other", "task"); err != nil {
			t.Fatalf("under a task that sits under an epic: %v", err)
		}
	})

	t.Run("clearing a parent is always allowed", func(t *testing.T) {
		f := hierarchySeed(today)
		if err := set(t, f, "task", ""); err != nil {
			t.Fatal(err)
		}
		if f.get("task").Parent != "" {
			t.Fatal("parent not cleared")
		}
		if err := set(t, f, "init", ""); err != nil {
			t.Fatal(err)
		}
		if f.get("init").Parent != "" {
			t.Fatal("parent not cleared")
		}
	})

	t.Run("a create under an impossible parent creates nothing", func(t *testing.T) {
		f := hierarchySeed(today)
		_, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{Title: "x", Type: board.TypeInitiative, Parent: "epic"})
		if !errors.Is(err, ErrParentLevel) {
			t.Fatalf("initiative under epic at create: %v", err)
		}
		if f.count("CreateCard") != 0 {
			t.Fatal("a refused create writes nothing")
		}
	})
}

// A hierarchy link never shows a reader of the child a hole where the parent
// is (H6). A planning card lives in the PRIMARY — the one repository every
// reader of the board can read — so a task of a CLOSED repository may sit
// under it; a parent whose file stands in the child's own repository is
// readable to the child's readers too; a parent in a repository the child's
// readers cannot open is refused.
func TestAHierarchyLinkStaysInsideOneRepository(t *testing.T) {
	today := board.TodayIso()
	ctx := WithActor(ctx, "kvaps")
	f := newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic, Domain: "primary"},
		{ItemID: "closedEpic", Title: "Secret", Type: board.TypeEpic, Domain: "closed"},
		{ItemID: "task", Title: "Invoices", Team: "beta", Domain: "closed", StartDate: today, Day: today},
		{ItemID: "sharedTask", Title: "Docs", Team: "alpha", Domain: "primary", StartDate: today, Day: today},
		{ItemID: "s-alpha", Title: board.SprintStateTitle, Team: "alpha", Domain: "primary"},
		{ItemID: "s-beta", Title: board.SprintStateTitle, Team: "beta", Domain: "closed"},
	}, nil)
	f.b.Primary = "primary"
	f.b.Domains = map[string]string{"s-alpha": "primary", "s-beta": "closed"}
	f.b.SprintStates = map[string]board.SprintState{"alpha": {ItemID: "s-alpha"}, "beta": {ItemID: "s-beta"}}
	// Up to the primary, from the closed repository: every reader of the
	// closed task can read the primary's epic.
	if err := f2svc(f).SetParent(ctx, "acme", "task", "epic"); err != nil {
		t.Fatalf("a closed task under a primary epic: %v, want it allowed", err)
	}
	// Inside the closed repository: the same readers on both sides.
	if err := f2svc(f).SetParent(ctx, "acme", "task", "closedEpic"); err != nil {
		t.Fatalf("inside the closed repository: %v", err)
	}
	// A primary task under a parent that stands in the closed repository:
	// the task's readers cannot open the parent — refused.
	if err := f2svc(f).SetParent(ctx, "acme", "sharedTask", "closedEpic"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a primary task under a closed parent: %v, want ErrCrossDomain", err)
	}
	// The create door answers the same, before anything is written.
	_, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{Title: "x", Team: "alpha", Parent: "closedEpic"})
	if !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("create across: %v, want ErrCrossDomain", err)
	}
	if _, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{Title: "y", Team: "beta", Parent: "epic"}); err != nil {
		t.Fatalf("a closed create under the primary's epic: %v, want it allowed", err)
	}
}

// Deleting a planning card re-parents what sat under it to ITS parent, so a
// goal's tree does not fall apart when an initiative goes.
func TestDeletingAPlanningCardReparentsItsChildren(t *testing.T) {
	today := board.TodayIso()
	ctx := WithActor(ctx, "kvaps")
	f := hierarchySeed(today)
	if err := f2svc(f).Remove(ctx, "acme", "init", board.ViewRoadmap, OffBoard); err != nil {
		t.Fatal(err)
	}
	if f.get("init") != nil {
		t.Fatal("the initiative is still there")
	}
	if got := f.get("epic").Parent; got != "goal" {
		t.Fatalf("the epic now sits under the goal, got %q", got)
	}
	if got := f.get("task").Parent; got != "epic" {
		t.Fatalf("the task still sits under the epic, got %q", got)
	}
	// The root goes: its children become roots.
	if err := f2svc(f).DeleteCard(ctx, "acme", "goal"); err != nil {
		t.Fatal(err)
	}
	if got := f.get("epic").Parent; got != "" {
		t.Fatalf("the epic is a root now, got %q", got)
	}
	// Unassign means nothing on a planning card: it has no working area.
	if err := f2svc(f).Remove(ctx, "acme", "epic", board.ViewRoadmap, Unassign); !errors.Is(err, ErrPlanningCard) {
		t.Fatalf("unassign a planning card: %v, want ErrPlanningCard", err)
	}
}

// A planning card has no stage, progress, band, week, shelf, sprint, column
// or review of its own (H3, H5): those are a task's. Its dates and its owner
// are its own.
func TestAPlanningCardHasNoDayBoardState(t *testing.T) {
	today := board.TodayIso()
	ctx := WithActor(ctx, "po")
	f := hierarchySeed(today)
	svc := f2svc(f)
	refused := map[string]error{
		"stage":     svc.SetStage(ctx, "acme", "epic", board.StageDone),
		"progress":  svc.SetProgress(ctx, "acme", "epic", 50),
		"inprog":    svc.SetInProgress(ctx, "acme", "epic"),
		"zone":      svc.SetZone(ctx, "acme", "epic", board.ZoneRed),
		"size":      svc.SetSize(ctx, "acme", "epic", board.SizeM),
		"backlog":   svc.SetBacklog(ctx, "acme", "epic", true),
		"finished":  svc.FinishedEarlier(ctx, "acme", "epic"),
		"sprint":    svc.SetSprintStart(ctx, "acme", "epic", today),
		"recurrent": svc.SetRecurrence(ctx, "acme", "epic", "week"),
		"owner":     svc.SetAssignee(ctx, "acme", "epic", "po"),
	}
	for what, err := range refused {
		if !errors.Is(err, ErrPlanningCard) {
			t.Errorf("%s on a planning card: %v, want ErrPlanningCard", what, err)
		}
	}
	if _, err := svc.SendToReview(ctx, "acme", "epic", "dev", today, board.ZoneYellow); !errors.Is(err, ErrPlanningCard) {
		t.Errorf("review of a planning card: %v, want ErrPlanningCard", err)
	}
	// Dates are its own, and set no sprint.
	if err := svc.SetDates(ctx, "acme", "epic", today, board.AddDays(today, 30)); err != nil {
		t.Fatal(err)
	}
	if c := f.get("epic"); c.StartDate != today || c.SprintStart != "" {
		t.Fatalf("dates on a planning card: %+v", c)
	}
	// And it is born with neither a band nor a size: the Roadmap draws
	// levels and dates, not the day boards' bands, and a weight belongs to
	// work somebody carries.
	for what, args := range map[string]CreateCardArgs{
		"a band": {Title: "Billing", Type: board.TypeEpic, Zone: board.ZoneYellow},
		"a size": {Title: "Billing", Type: board.TypeEpic, Size: board.SizeM},
	} {
		if _, err := svc.CreateCard(ctx, "acme", args); !errors.Is(err, ErrPlanningCard) {
			t.Errorf("a planning card born with %s = %v, want ErrPlanningCard", what, err)
		}
	}
}

// Carry Over carries a task under an epic like any task of the closing
// sprint; a subtask rides its parent; a planning card is not sprint work.
func TestCarryOverCarriesATaskUnderAnEpic(t *testing.T) {
	today := board.TodayIso()
	old := board.AddDays(today, -7)
	b := board.NewBoard([]board.Card{
		{ItemID: "epic", Type: board.TypeEpic, Team: "alpha", SprintStart: old},
		{ItemID: "under", Parent: "epic", Team: "alpha", SprintStart: old, Progress: 30},
		{ItemID: "sub", Parent: "under", Team: "alpha", SprintStart: old},
		{ItemID: "loose", Team: "alpha", SprintStart: old},
	})
	carry, _, _ := selectCarry(b, "alpha", old, today)
	got := map[string]bool{}
	for _, c := range carry {
		got[c.ItemID] = true
	}
	if !got["under"] || !got["loose"] {
		t.Fatalf("the tasks are carried: %v", got)
	}
	if got["sub"] || got["epic"] {
		t.Fatalf("a subtask rides its parent and a planning card is no sprint's: %v", got)
	}
}
