package board

import (
	"reflect"
	"testing"
)

// The hierarchy is ONE link, `parent`, and the kind of the parent is read off
// the parent card — never encoded in the key (docs/design/hierarchy.md, H1).
// These tests pin the lookups every rule above the day boards is built on.

func hierarchyBoard() Board {
	return NewBoard([]Card{
		{ItemID: "goal", Title: "Reach Europe", Type: TypeGoal},
		{ItemID: "init", Title: "Launch in Portugal", Type: TypeInitiative, Parent: "goal",
			StartDate: "2026-11-01", Day: "2026-12-31"},
		{ItemID: "epic", Title: "Billing", Type: TypeEpic, Parent: "init"},
		// A task under the epic, open, with a subtask of its own.
		{ItemID: "t1", Title: "Invoices", Parent: "epic", Team: "alpha", Progress: 40},
		{ItemID: "t1a", Title: "Invoice PDF", Parent: "t1", Team: "alpha", Progress: 100},
		// A task straight under the goal, finished.
		{ItemID: "t2", Title: "Pricing page", Parent: "goal", Team: "alpha", Progress: 100},
		// A project card under the initiative.
		{ItemID: "p1", Title: "Legal review", Parent: "init", Scope: "Compliance", Project: "eu"},
		// A plain task, and a subtask of it: no planning card above either.
		{ItemID: "t3", Title: "Fix build", Team: "alpha"},
		{ItemID: "t3a", Title: "Flaky test", Parent: "t3", Team: "alpha"},
		// A dangling parent: the card it names is not on the board.
		{ItemID: "t4", Title: "Orphan", Parent: "gone", Team: "alpha"},
		// An epic with nothing under it.
		{ItemID: "empty", Title: "Empty epic", Type: TypeEpic},
	})
}

func TestTheLevelsAreStrictlyOrdered(t *testing.T) {
	t.Parallel()
	ordered := Level(TypeGoal) > Level(TypeInitiative) && Level(TypeInitiative) > Level(TypeEpic) &&
		Level(TypeEpic) > Level(TypeTask)
	if !ordered {
		t.Fatal("goal > initiative > epic > task is the whole level rule")
	}
	// An unknown type is read as a task: the lowest level, and never a parent
	// of anything but a subtask.
	if Level("widget") != Level(TypeTask) {
		t.Fatal("an unknown type is a task")
	}
	for _, typ := range []string{TypeTask, TypeGoal, TypeInitiative, TypeEpic} {
		if !ValidType(typ) {
			t.Errorf("%q is a type and was refused", typ)
		}
	}
	if ValidType("widget") || ValidType("task") {
		t.Error("only the three planning levels (and the empty task) are types")
	}
	if IsPlanning(Card{}) || !IsPlanning(Card{Type: TypeEpic}) {
		t.Error("IsPlanning is Type != \"\"")
	}
}

// A SUBTASK is a task whose parent is a task. A task under an epic is not one:
// it is not hidden from the day boards, inherits nothing, rides nowhere.
func TestASubtaskIsATaskUnderATask(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	find := func(id string) Card {
		c, ok := Find(b, id)
		if !ok {
			t.Fatalf("%s not on the board", id)
		}
		return c
	}
	for id, want := range map[string]bool{
		"t1a": true, "t3a": true, // under a task
		"t1": false, "t2": false, "p1": false, // under a planning card
		"init": false, "epic": false, // planning cards are never subtasks
		"t3": false, "goal": false, // no parent at all
	} {
		if got := IsSubtask(b, find(id)); got != want {
			t.Errorf("IsSubtask(%s) = %v, want %v", id, got, want)
		}
	}
	// A DANGLING parent never reaches the rules from a read board: the
	// loader drops it (TestADanglingParentIsDroppedOnLoad), so the card is a
	// top-level task, not a subtask of nothing hidden from every board.
	if c := find("t4"); c.Parent != "" || IsSubtask(b, c) {
		t.Errorf("a card whose parent is not on the board is read as a root, got parent %q", c.Parent)
	}
	// The rules' own reading of one, on a board nobody read (a cache between
	// a delete and its reload): still a subtask, as it always was.
	if !IsSubtask(Board{}, Card{ItemID: "x", Parent: "gone"}) {
		t.Error("IsSubtask reads an unknown parent as a task's")
	}
	// The set form answers the same, and is what a loop over the board uses.
	pl := PlanningIDs(b)
	if !reflect.DeepEqual(pl, map[string]bool{"goal": true, "init": true, "epic": true, "empty": true}) {
		t.Fatalf("PlanningIDs = %v", pl)
	}
	for _, c := range b.Cards {
		if IsSubtaskIn(c, pl) != IsSubtask(b, c) {
			t.Errorf("IsSubtaskIn(%s) disagrees with IsSubtask", c.ItemID)
		}
	}
}

func TestParentOfAndChildrenOf(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	c, _ := Find(b, "t1")
	if p, ok := ParentOf(b, c); !ok || p.ItemID != "epic" {
		t.Fatalf("ParentOf(t1) = %v %v", p.ItemID, ok)
	}
	c, _ = Find(b, "t4")
	if _, ok := ParentOf(b, c); ok {
		t.Fatal("a dangling parent is not found")
	}
	c, _ = Find(b, "goal")
	if _, ok := ParentOf(b, c); ok {
		t.Fatal("a root has no parent")
	}
	if got := ids(ChildrenOf(b, "init")); !reflect.DeepEqual(got, []string{"epic", "p1"}) {
		t.Fatalf("ChildrenOf(init) = %v: every card under it, planning and not, in board order", got)
	}
	if got := ids(ChildrenOf(b, "t1")); !reflect.DeepEqual(got, []string{"t1a"}) {
		t.Fatalf("ChildrenOf(t1) = %v", got)
	}
	if ChildrenOf(b, "") != nil {
		t.Fatal("nothing is under nobody")
	}
}

// The breadcrumb: every ancestor, root first, a task parent included. It is
// what the detail pane draws above a card, and what says whether a card is on
// the Roadmap at all (a planning card somewhere above it).
func TestLineageIsEveryAncestorRootFirst(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	cases := map[string][]string{
		"t1a":   {"goal", "init", "epic", "t1"},
		"t1":    {"goal", "init", "epic"},
		"p1":    {"goal", "init"},
		"t2":    {"goal"},
		"epic":  {"goal", "init"},
		"goal":  {},
		"t3":    {},
		"t3a":   {"t3"},
		"t4":    {}, // the parent is not here to be an ancestor
		"empty": {},
	}
	for id, want := range cases {
		c, _ := Find(b, id)
		if got := ids(Lineage(b, c)); !reflect.DeepEqual(got, want) {
			t.Errorf("Lineage(%s) = %v, want %v", id, got, want)
		}
	}
	for id, want := range map[string]bool{"t1a": true, "t1": true, "p1": true, "t2": true, "epic": true,
		"goal": false, "t3": false, "t3a": false, "t4": false, "empty": false} {
		c, _ := Find(b, id)
		if got := HasPlanningAncestor(b, c); got != want {
			t.Errorf("HasPlanningAncestor(%s) = %v, want %v", id, got, want)
		}
	}
	// A cycle written straight into the repository cannot hang a reader: the
	// walk stops on a repeat.
	loop := NewBoard([]Card{
		{ItemID: "a", Type: TypeEpic, Parent: "b"},
		{ItemID: "b", Type: TypeEpic, Parent: "a"},
		{ItemID: "t", Parent: "a"},
	})
	c, _ := Find(loop, "t")
	if got := ids(Lineage(loop, c)); len(got) > 3 {
		t.Fatalf("a cycle must terminate: %v", got)
	}
}

// Status and progress are DERIVED from the leaf tasks under a planning card,
// through every level, and never stored (H5).
func TestHierarchyStatusIsDerivedFromTheLeafTasks(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	// Leaf tasks: a task whose parent is a planning card. t1's subtask t1a is
	// not counted — t1 answers for it through its own derived progress.
	if got := ids(LeafTasks(b, "goal")); !reflect.DeepEqual(got, []string{"t1", "t2", "p1"}) {
		t.Fatalf("LeafTasks(goal) = %v", got)
	}
	if got := ids(LeafTasks(b, "epic")); !reflect.DeepEqual(got, []string{"t1"}) {
		t.Fatalf("LeafTasks(epic) = %v", got)
	}
	if got := LeafTasks(b, "empty"); len(got) != 0 {
		t.Fatalf("LeafTasks(empty) = %v", ids(got))
	}
	// A task is not a planning card and has no leaf tasks of this kind.
	if got := LeafTasks(b, "t1"); len(got) != 0 {
		t.Fatalf("LeafTasks(t1) = %v", ids(got))
	}

	status := func(id string) (string, int, int) {
		c, _ := Find(b, id)
		return HierarchyStatus(b, c)
	}
	if st, done, total := status("goal"); st != HierarchyInProgress || done != 1 || total != 3 {
		t.Errorf("goal: %s %d/%d, want in-progress 1/3 (t2 done, t1 at 40, p1 at 0)", st, done, total)
	}
	if st, done, total := status("epic"); st != HierarchyInProgress || done != 0 || total != 1 {
		t.Errorf("epic: %s %d/%d, want in-progress 0/1 (t1 is at 40)", st, done, total)
	}
	// Nothing under it at all: NO ITEMS — not "to do", which would read as
	// work waiting when there is no work.
	if st, done, total := status("empty"); st != HierarchyNoItems || done != 0 || total != 0 {
		t.Errorf("empty: %s %d/%d, want no-items 0/0", st, done, total)
	}
	// Only project cards under it, none started and nobody's: TO DO. One
	// of them done and the rest untouched: in progress. All done: done.
	plan := NewBoard([]Card{
		{ItemID: "e", Type: TypeEpic},
		{ItemID: "a", Parent: "e", Scope: "S"},
		{ItemID: "b", Parent: "e", Scope: "S"},
	})
	e, _ := Find(plan, "e")
	if st, _, total := HierarchyStatus(plan, e); st != HierarchyTodo || total != 2 {
		t.Errorf("untouched project cards: %s/%d, want todo/2", st, total)
	}
	plan.Cards[1].Progress = 100
	if st, done, _ := HierarchyStatus(plan, e); st != HierarchyInProgress || done != 1 {
		t.Errorf("one done: %s %d, want in-progress 1", st, done)
	}
	plan.Cards[2].Progress = 100
	if st, done, total := HierarchyStatus(plan, e); st != HierarchyDone || done != 2 || total != 2 {
		t.Errorf("all done: %s %d/%d, want done 2/2", st, done, total)
	}
	// A task is not a planning card: no hierarchy status at all.
	task, _ := Find(b, "t1")
	if st, _, _ := HierarchyStatus(b, task); st != "" {
		t.Errorf("a task has no hierarchy status, got %q", st)
	}
}

// A card in a column and a planning card have no band and no size — the
// Project board and the Roadmap draw neither, and a file that still carries
// the keys (a repository migrated before the rule, a direct writer) must not
// put them back on the wire. They are taken off on READ, where every other
// reading of a file is settled, and the card's next rewrite drops the keys.
// A task keeps both.
func TestACardInAColumnOrOfALevelReadsWithNoZoneOrSize(t *testing.T) {
	t.Parallel()
	b := NewBoard([]Card{
		{ItemID: "plan", Scope: "Infra", Project: "P", Zone: ZoneRed, Size: SizeM, SprintStart: "2026-08-24", Week: "2026-08-24"},
		{ItemID: "epic", Type: TypeEpic, Zone: ZoneYellow, Size: SizeXL, SprintStart: "2026-08-24", Week: "2026-08-24"},
		{ItemID: "task", Team: "t", Zone: ZoneGreen, Size: SizeS, SprintStart: "2026-08-24", Week: "2026-08-24"},
	})
	for _, id := range []string{"plan", "epic"} {
		c, _ := Find(b, id)
		if c.Zone != ZoneNone || c.Size != "" {
			t.Errorf("%s reads with zone %q size %q, want neither", id, c.Zone, c.Size)
		}
		// Nor a sprint or a week: no day board draws it, so a sprint on it
		// is a sprint a carry-over would move for nothing (H3, H4).
		if c.SprintStart != "" || c.Week != "" {
			t.Errorf("%s reads with sprint %q week %q, want neither", id, c.SprintStart, c.Week)
		}
	}
	if c, _ := Find(b, "task"); c.Zone != ZoneGreen || c.Size != SizeS || c.SprintStart == "" || c.Week == "" {
		t.Errorf("a task keeps its band, size, sprint and week, got %+v", c)
	}
}

// A card in a column is DRAWN by its dates — they are its row on the Project
// board — and the week is read off it (above). A card that had a week and no
// dates (a slot of the weekly plan, filed before the week→dates conversion
// existed) would then have no row at all: on no board, and nothing to bring
// it back. So the week it was owed in BECOMES its dates, Monday to Friday,
// before it is dropped — the same conversion the door that files a card into
// a column makes (WeekAsDates). Dates it already has win; a task keeps its
// week; a planning card never had one to convert.
func TestAWeekOnlyProjectCardReadsAsItsWeeksDates(t *testing.T) {
	t.Parallel()
	b := NewBoard([]Card{
		{ItemID: "slot", Scope: "Infra", Project: "P", Week: "2026-10-05"},
		{ItemID: "dated", Scope: "Infra", Project: "P", StartDate: "2026-10-01", Day: "2026-10-03", Week: "2026-10-05"},
		{ItemID: "task", Team: "t", Week: "2026-10-05"},
		{ItemID: "epic", Type: TypeEpic, Week: "2026-10-05"},
	})
	if c, _ := Find(b, "slot"); c.StartDate != "2026-10-05" || c.Day != "2026-10-09" || c.Week != "" {
		t.Errorf("a week-only column card reads as start %q day %q week %q, want its week's Monday..Friday and no week", c.StartDate, c.Day, c.Week)
	}
	if c, _ := Find(b, "dated"); c.StartDate != "2026-10-01" || c.Day != "2026-10-03" || c.Week != "" {
		t.Errorf("a column card's own dates win over its week, got start %q day %q week %q", c.StartDate, c.Day, c.Week)
	}
	if c, _ := Find(b, "task"); c.Week != "2026-10-05" || c.StartDate != "" {
		t.Errorf("a task keeps its week and gets no dates, got %+v", c)
	}
	if c, _ := Find(b, "epic"); c.StartDate != "" || c.Day != "" || c.Week != "" {
		t.Errorf("a planning card's stray week is dropped and no dates invented, got %+v", c)
	}
}

// A DANGLING parent — one the board does not hold — is dropped when the
// board is read. The rules read a card with a parent that is not a planning
// card as a subtask, hidden from every board; before the hierarchy only a
// deleted TASK could leave one behind, and the × never did without freeing
// its children. Now an epic or a goal deleted by a direct writer (or a torn
// cross-replica move) would take its whole subtree off Me, Team, Triage and
// the Roadmap with nothing to bring it back. Read as a root, the card stands
// where its own dates and team put it, and its next rewrite drops the key.
func TestADanglingParentIsDroppedOnLoad(t *testing.T) {
	t.Parallel()
	today := TodayIso()
	b := NewBoard([]Card{
		// Its epic's file is gone.
		{ItemID: "t", Parent: "gone-epic", Team: "alpha", StartDate: today, Day: today, SprintStart: today},
		// Its task's file is gone.
		{ItemID: "s", Parent: "gone-task", Team: "alpha", StartDate: today, Day: today, SprintStart: today},
		// A real subtask keeps its parent.
		{ItemID: "p", Team: "alpha", StartDate: today, Day: today, SprintStart: today},
		{ItemID: "pa", Parent: "p", Team: "alpha", StartDate: today, Day: today, SprintStart: today},
	})
	b.SprintStates = map[string]SprintState{"alpha": {Current: today}}
	for _, id := range []string{"t", "s"} {
		c, _ := Find(b, id)
		if c.Parent != "" || IsSubtask(b, c) {
			t.Errorf("%s reads with parent %q (subtask %v), want a top-level card", id, c.Parent, IsSubtask(b, c))
		}
	}
	if c, _ := Find(b, "pa"); c.Parent != "p" || !IsSubtask(b, c) {
		t.Errorf("a subtask whose task is here keeps its parent, got %+v", c)
	}
	// What the reading dropped is named, for a tool that must report it.
	if b.DroppedParents["t"] != "gone-epic" || b.DroppedParents["s"] != "gone-task" || len(b.DroppedParents) != 2 {
		t.Errorf("DroppedParents = %v", b.DroppedParents)
	}
	got := map[string]bool{}
	for _, c := range TeamGrid(b, "alpha", today) {
		got[c.ItemID] = true
	}
	if !got["t"] || !got["s"] || got["pa"] {
		t.Errorf("the Team grid draws the two orphans and not the real subtask, got %v", got)
	}
}

// A planning card is nobody's and no team's: the people and the teams are
// on the tasks under it. A file that names an owner or a team on a goal — a
// repository written before the rule, a direct writer — is read without
// them, so a stray team on a goal never mints a team chip or a column.
func TestAPlanningCardReadsWithNoTeamOrOwner(t *testing.T) {
	t.Parallel()
	b := NewBoard([]Card{
		{ItemID: "goal", Type: TypeGoal, Team: "ghost", Assignees: []string{"po"}},
		{ItemID: "task", Team: "t", Assignees: []string{"dev"}},
	})
	if c, _ := Find(b, "goal"); c.Team != "" || len(c.Assignees) != 0 {
		t.Errorf("a goal reads with team %q owners %v, want neither", c.Team, c.Assignees)
	}
	if c, _ := Find(b, "task"); c.Team != "t" || len(c.Assignees) != 1 {
		t.Errorf("a task keeps its team and owner, got %q %v", c.Team, c.Assignees)
	}
	for _, team := range b.TeamOrder {
		if team == "ghost" {
			t.Error("a stray team on a goal minted a team")
		}
	}
}

// Somebody being ON a task is work in progress, before the first percent is
// typed: an initiative whose tasks are all assigned and none started is
// being worked, and the Roadmap must say so. An unassigned, unstarted task
// is to do; a task somebody owns is in progress; and a card that is a
// subtask's parent counts once, assigned or not.
func TestAnAssignedLeafTaskPutsItsPlanningCardInProgress(t *testing.T) {
	t.Parallel()
	b := NewBoard([]Card{
		{ItemID: "i", Type: TypeInitiative},
		{ItemID: "e", Type: TypeEpic, Parent: "i"},
		{ItemID: "t1", Parent: "e"},
		{ItemID: "t2", Parent: "e"},
	})
	i, _ := Find(b, "i")
	if st, done, total := HierarchyStatus(b, i); st != HierarchyTodo || done != 0 || total != 2 {
		t.Fatalf("nobody on anything: %s %d/%d, want todo 0/2", st, done, total)
	}
	b.Cards[2].Assignees = []string{"ann"}
	if st, done, total := HierarchyStatus(b, i); st != HierarchyInProgress || done != 0 || total != 2 {
		t.Fatalf("one task owned, none started: %s %d/%d, want in-progress 0/2", st, done, total)
	}
	// Its subtask is nobody's and counts through its parent only.
	b.Cards = append(b.Cards, Card{ItemID: "t1a", Parent: "t1"})
	if st, done, total := HierarchyStatus(b, i); st != HierarchyInProgress || total != 2 {
		t.Fatalf("a subtask changes nothing: %s %d/%d, want in-progress 0/2", st, done, total)
	}
	// Done beats owned: when every leaf task is complete the card is done,
	// whoever is still named on it.
	b.Cards[2].Progress, b.Cards[3].Progress = 100, 100
	if st, done, total := HierarchyStatus(b, i); st != HierarchyDone || done != 2 || total != 2 {
		t.Fatalf("all done: %s %d/%d, want done 2/2", st, done, total)
	}
}

// The day boards draw TASKS. A planning card is on none of them, whatever
// its dates, team or owner say (H3); and a task under one is on them exactly
// as a task with no parent is — it is not a subtask.
func TestPlanningCardsAreOnNoDayBoardAndTasksUnderThemAre(t *testing.T) {
	t.Parallel()
	today := TodayIso()
	b := NewBoard([]Card{
		{ItemID: "epic", Type: TypeEpic, Team: "alpha", Assignees: []string{"u"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "under", Parent: "epic", Team: "alpha", Assignees: []string{"u"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "sub", Parent: "under", Team: "alpha", Assignees: []string{"u"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "loose", Team: "alpha", Assignees: []string{"u"}, StartDate: today, Day: today, SprintStart: today},
		// A task under an epic with no week: it is in the strip, like any
		// task nobody has scheduled.
		{ItemID: "unscheduled", Parent: "epic", Team: "alpha"},
	})
	b.SprintStates = map[string]SprintState{"alpha": {Current: today}}
	if got := ids(TeamGrid(b, "alpha", today)); !reflect.DeepEqual(got, []string{"under", "loose"}) {
		t.Errorf("TeamGrid = %v, want the two tasks and neither the epic nor the subtask", got)
	}
	if got := ids(MeView(b, "u", today)); !reflect.DeepEqual(got, []string{"under", "loose"}) {
		t.Errorf("MeView = %v, want the two tasks", got)
	}
	e, _ := Find(b, "epic")
	if NeedsTriage(b, e, today) {
		t.Error("a planning card is not in the strip")
	}
	u, _ := Find(b, "unscheduled")
	if !NeedsTriage(b, u, today) {
		t.Error("a task under an epic that nobody scheduled is in the strip like any other")
	}
	s, _ := Find(b, "sub")
	if NeedsTriage(b, s, today) {
		t.Error("a subtask is not in the strip")
	}
	// Load: the task under the epic is on its person's hands; the epic is
	// not work in hand, and the subtask rides its parent.
	if got := CarryingNow(b, today)["u"]; got != 2 {
		t.Errorf("CarryingNow = %d, want 2 (under, loose)", got)
	}
	// Reachable: a planning card is reachable (the Roadmap draws it); a task
	// under one by its own day; a subtask through its parent.
	r := Reachable(b, today)
	for _, id := range []string{"epic", "under", "sub", "loose", "unscheduled"} {
		if !r[id] {
			t.Errorf("%s is on a board and Reachable does not name it", id)
		}
	}
}

// The subtask helpers answer for TASKS only: a planning card's children are
// not its subtasks and never drive its progress, and the file cascade of a
// move stops at a planning card.
func TestSubtaskHelpersStopAtPlanningCards(t *testing.T) {
	t.Parallel()
	b := hierarchyBoard()
	if got := ids(Children(b, "t1")); !reflect.DeepEqual(got, []string{"t1a"}) {
		t.Fatalf("Children(t1) = %v", got)
	}
	if got := Children(b, "epic"); len(got) != 0 {
		t.Fatalf("Children(epic) = %v: a planning card has no subtasks", ids(got))
	}
	if OpenChildren(b, "epic") {
		t.Fatal("a planning card has no open subtasks to hold its stage")
	}
	if !OpenChildren(b, "t3") {
		t.Fatal("t3's subtask is open")
	}
	if got := Followers(b, "init"); len(got) != 0 {
		t.Fatalf("Followers(init) = %v: a hierarchy link does not cascade a move", ids(got))
	}
	if got := ids(Followers(b, "t1")); !reflect.DeepEqual(got, []string{"t1a"}) {
		t.Fatalf("Followers(t1) = %v", got)
	}
}

// The domain rule: a subtask lives where its parent lives; a task under a
// planning card lives where ITS OWN project or team lives (H6). The resolver
// says which kind the parent is.
func TestAHierarchyLinkDoesNotPlaceTheChild(t *testing.T) {
	t.Parallel()
	b := NewBoardIn("primary", []Card{
		{ItemID: "epic", Type: TypeEpic, Domain: "closed"},
		{ItemID: "task", Parent: "epic", Team: "alpha", Domain: "primary"},
		{ItemID: "parentTask", Team: "beta", Domain: "closed"},
		{ItemID: "sub", Parent: "parentTask", Team: "alpha", Domain: "primary"},
	})
	b.SprintStates = map[string]SprintState{"alpha": {ItemID: "s-alpha"}, "beta": {ItemID: "s-beta"}}
	b.Domains = map[string]string{"s-alpha": "primary", "s-beta": "closed"}
	r := Resolver(b, "primary")
	task, _ := Find(b, "task")
	if got := DomainOf(task, r); got != "primary" {
		t.Errorf("a task under an epic lives where its team lives, got %q", got)
	}
	sub, _ := Find(b, "sub")
	if got := DomainOf(sub, r); got != "closed" {
		t.Errorf("a subtask lives where its parent lives, got %q", got)
	}
}

func TestTheRoadmapIsAView(t *testing.T) {
	t.Parallel()
	if !KnownView("roadmap") {
		t.Fatal("roadmap is a board a person opens")
	}
	if ScopedByTeam(ViewRoadmap) {
		t.Fatal("the Roadmap is board-wide")
	}
	if got := Panes(ViewRoadmap); !reflect.DeepEqual(got, []View{ViewRoadmap}) {
		t.Fatalf("Panes(roadmap) = %v", got)
	}
}
