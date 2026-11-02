package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// columnBoard: two projects in the primary repository (engineering with the
// column Cozystack, freedom with Launch), one in another (strategy with
// Fundraising).
func columnBoard(cards []board.Card) *fakeBackend {
	roster := []board.Card{
		{ItemID: "pr-e", Title: board.ProjectStateTitle, Project: "engineering"},
		{ItemID: "pr-f", Title: board.ProjectStateTitle, Project: "freedom"},
		{ItemID: "pr-s", Title: board.ProjectStateTitle, Project: "strategy", Domain: "founders"},
		{ItemID: "ep-cozy", Title: board.ScopeStateTitle, Scope: "Cozystack", Project: "engineering"},
		{ItemID: "ep-launch", Title: board.ScopeStateTitle, Scope: "Launch", Project: "freedom"},
		{ItemID: "ep-fund", Title: board.ScopeStateTitle, Scope: "Fundraising", Project: "strategy", Domain: "founders"},
	}
	return newFake(append(roster, cards...), map[string]board.SprintState{
		"platform": {Current: board.TodayIso(), Previous: board.AddDays(board.TodayIso(), -1), ItemID: "st-p"},
	})
}

func TestRemoveFromLastProjectDropsTheWeekAndKeepsOnlyWorkedCards(t *testing.T) {
	worked := board.Card{ItemID: "w", Title: "worked", Team: "platform", Project: "engineering", Scope: "Cozystack",
		Assignees: []string{"kvaps"}, Progress: 40, Week: "2026-08-24",
		SprintStart: board.TodayIso(), StartDate: board.TodayIso(), Day: board.TodayIso()}
	idle := board.Card{ItemID: "i", Title: "idle", Team: "platform", Project: "engineering", Scope: "Cozystack",
		Week:        "2026-08-24",
		SprintStart: board.TodayIso(), StartDate: board.TodayIso(), Day: board.TodayIso()}
	f := columnBoard([]board.Card{worked, idle})
	svc := New(f)

	if err := svc.RemoveFromProject(ctx, "acme", "w", "engineering", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "w")
	if !ok {
		t.Fatal("a worked card survives as an orphan of the working area")
	}
	if c.Week != "" {
		t.Fatalf("the week was the slot's row and goes with the column: %+v", c)
	}
	if c.Project != "" || c.Scope != "" {
		t.Fatalf("the column is gone: %+v", c)
	}
	if c.SprintStart == "" || len(c.Assignees) == 0 {
		t.Fatalf("the working area — the person and the sprint — stays: %+v", c)
	}

	if err := svc.RemoveFromProject(ctx, "acme", "i", "engineering", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	// The deleted card's week was never cleared first: a write into the
	// very commit that removes the file is a dead write.
	if f.count("SetWeek i") != 0 {
		t.Fatalf("no dead writes into a file about to be removed: %v", f.log)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "i"); ok {
		t.Fatal("an untouched card with no other column is deleted outright")
	}
}

func TestRemoveFromProjectRefusesAColumnTheCardIsNotIn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "c1", Title: "x", Team: "platform", Project: "engineering", Scope: "Cozystack"},
	})
	if err := New(f).RemoveFromProject(ctx, "acme", "c1", "freedom", "Launch"); err == nil {
		t.Fatal("the card does not stand in that column")
	}
}

// A column is named by its SCOPE alone. A card standing in no column has an
// empty home, which once "matched" ("", "") and fell through to the
// last-column branch — the call that asked to remove a card from nowhere
// deleted it outright. Requiring the scope half closes that hole; requiring
// the project half too broke the × of every no-project column, which is a
// real column with a real ×. The MCP tool feeds this service without
// validating, and an agent calling remove_from_project with empty halves
// on a column-less card is the most expectable mistake there is.
func TestRemoveFromProjectRefusesTheEmptyPair(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "c1", Title: "no column", Team: "platform"},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "", ""); !errors.Is(err, ErrNotInProject) {
		t.Fatalf("the empty pair must be refused: %v", err)
	}
	// ("", scope) is a lawful pair — the no-project bucket — but this card
	// is not in it: an empty home mismatches honestly, nothing is deleted.
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "", "Launch"); !errors.Is(err, ErrNotInProject) {
		t.Fatalf("a no-project column the card is not in: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "c1"); !ok {
		t.Fatal("the refused call must not have deleted the card")
	}
}

// The no-project bucket is a full column — its own chip, its own × — and
// removal from it follows the same last-column rules: an untouched card is
// deleted, a worked card survives as a working-area orphan.
func TestRemoveFromANoProjectColumnWorks(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-inbox", Title: board.ScopeStateTitle, Scope: "Inbox"},
		{ItemID: "cold", Title: "untouched", Team: "platform", Scope: "Inbox"},
		{ItemID: "warm", Title: "worked", Team: "platform", Scope: "Inbox",
			Assignees: []string{"kvaps"}, Progress: 40},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "cold", "", "Inbox"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "cold"); ok {
		t.Fatal("an untouched card in its last column is deleted")
	}
	if err := svc.RemoveFromProject(ctx, "acme", "warm", "", "Inbox"); err != nil {
		t.Fatal(err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "warm")
	if !ok || c.Scope != "" {
		t.Fatalf("a worked card survives as a working-area orphan: %+v", c)
	}
}

// Grouping hands a card's WEEK to its parent, which stands for it from then
// on — but not the week of a card that stands in a COLUMN: there the week is
// the row the Project board draws it in, and clearing it took the card's row
// away, to be re-derived from dates the client is not shown.
func TestGroupingKeepsTheWeekOfACardInAColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "slot", Title: "a card in a column", Team: "platform",
			Project: "engineering", Scope: "Cozystack",
			Week: "2026-08-31", StartDate: "2026-08-31", Day: "2026-09-04"},
		{ItemID: "plain", Title: "a card of a week", Team: "platform", Week: "2026-08-31"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "slot", "p"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "slot")
	if c.Week != "2026-08-31" {
		t.Fatalf("the week stays — it is the row the card is drawn in: %+v", c)
	}
	// A card with no column has no row of its own: the week goes to the parent.
	if err := svc.SetParent(ctx, "acme", "plain", "p"); err != nil {
		t.Fatal(err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	c, _ = findCard(b, "plain")
	if c.Week != "" {
		t.Fatalf("the week goes to the parent: %+v", c)
	}
	if parent, _ := findCard(b, "p"); parent.Week != "2026-08-31" {
		t.Fatalf("and the parent stands for it: %+v", parent)
	}
}

// A card scheduled for a WEEK and attached to a project takes the slot of
// that week: Monday to Friday, the whole of the week it was owed in, so the
// card does not jump to another week on the way to the Project board.
func TestAttachingACardOfAWeekTakesThatWeeksSlot(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "c1", Title: "planned", Team: "platform", Week: "2026-08-24"},
	})
	svc := New(f)
	project := "engineering"
	if err := svc.SetScope(ctx, "acme", "c1", "Cozystack", &project); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.StartDate != "2026-08-24" || c.Day != "2026-08-28" {
		t.Fatalf("it spans the week it came from, Monday to Friday: %+v", c)
	}
	if c.Week != "2026-08-24" {
		t.Fatalf("the slot's row is the week it came from: %+v", c)
	}
}

// The slot rule applies only to a card with NO dates of its own: an attach
// never rewrites a schedule someone chose — the card keeps its dates, and
// its row is wherever those dates put it.
func TestAttachingADatedCardKeepsItsDates(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "c1", Title: "scheduled", Team: "platform", Week: "2026-08-24",
			StartDate: "2026-09-07", Day: "2026-09-09"},
	})
	svc := New(f)
	project := "engineering"
	if err := svc.SetScope(ctx, "acme", "c1", "Cozystack", &project); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.StartDate != "2026-09-07" || c.Day != "2026-09-09" {
		t.Fatalf("the chosen schedule survives the attach: %+v", c)
	}
}

// A SUBTASK has another home: its parent. The Project board draws it in
// the column it carries (G14 blesses that), so the × must be able to take
// it out of the column — but never to delete it, however untouched it is.
// Deleting here would destroy a card that is still riding its parent on
// every other board, which is the two-homes rule the × exists to honour.
func TestRemoveFromColumnNeverDeletesASubtask(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "untouched child", Team: "platform", Parent: "p",
			Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "engineering", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("a subtask survives its column: its home is the parent")
	}
	if c.Scope != "" || c.Project != "" {
		t.Fatalf("the column is gone: %+v", c)
	}
	if c.Parent != "p" {
		t.Fatalf("the parent is untouched: %+v", c)
	}
}

// The column-holds-the-file rule has a second door: GROUPING. A subtask's
// file follows its parent, so grouping a columned card under a parent of
// another repository would leave its column naming a repository that no
// longer holds it — the state SetScope now refuses, reached by a drag
// instead of a menu. The Project board draws such a card and counts it,
// so the stale column is load-bearing, not merely untidy.
func TestGroupingUnderAParentInAnotherRepositoryIsRefused(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Scope: "Fundraising", Domain: "founders"},
		{ItemID: "near", Title: "parent here", Project: "engineering", Scope: "Cozystack"},
		{ItemID: "c1", Title: "columned child", Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "c1", "far"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the column would name a repository that no longer holds the file: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Parent != "" {
		t.Fatalf("the refused grouping must not land: %+v", c)
	}
	// Within the repository, grouping is free and the column stays (G14).
	if err := svc.SetParent(ctx, "acme", "c1", "near"); err != nil {
		t.Fatal(err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	c, _ = findCard(b, "c1")
	if c.Scope != "Cozystack" {
		t.Fatalf("a subtask keeps the one column it carries: %+v", c)
	}
}

// And the mirror image: moving the PARENT drags every child's file with it
// (MultiBackend cascades the re-file), while each child's own column stays
// behind. The guard has to look at the children, not only at the card the
// caller named.
func TestReFilingAParentCannotStrandItsSubtasksColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Project: "engineering", Scope: "Cozystack"},
		{ItemID: "c1", Title: "child", Parent: "p", Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	strategy := "strategy"
	if err := svc.SetScope(ctx, "acme", "p", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the child's column would be stranded: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "p")
	if c.Project != "engineering" {
		t.Fatalf("the refused move must not land: %+v", c)
	}
	// A move within the repository leaves every child's column valid.
	freedom := "freedom"
	if err := svc.SetScope(ctx, "acme", "p", "Launch", &freedom); err != nil {
		t.Fatalf("a move inside the repository is free: %v", err)
	}
}

// A column's repository is the column's OWN — the domain of the scope stub
// that declares it — not its project's. The two agree wherever a project
// owns the column, but the NO-PROJECT bucket is a real column with no
// project to ask: reading the project made every card in it ungroupable,
// because ProjectStates never holds "" and the lookup always missed.
func TestGroupingACardFromANoProjectColumnIsFree(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-loose", Title: board.ScopeStateTitle, Scope: "Loose"},
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "loose column child", Team: "platform", Scope: "Loose"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "c1", "p"); err != nil {
		t.Fatalf("a no-project column of this repository strands nothing: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Scope != "Loose" || c.Parent != "p" {
		t.Fatalf("the column survives the grouping (G14): %+v", c)
	}
}

// The same question through the other door: attaching a card to a column
// of ANOTHER repository is refused whether or not that column has a
// project — the two guards must answer alike, or one of them is a way in.
func TestAttachingToAForeignNoProjectColumnIsRefused(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-far", Title: board.ScopeStateTitle, Scope: "Far", Domain: "founders"},
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "child", Team: "platform", Parent: "p"},
	})
	none := ""
	if err := New(f).SetScope(ctx, "acme", "c1", "Far", &none); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a column of the closed repository cannot hold a card of the primary: %v", err)
	}
}

// Every refusal fires BEFORE anything is written. The grouping's week
// handover ran first, so a refused grouping still took the child's week away
// and gave it to the parent — in the cache only, since the commit never
// happened: the board then showed a state git had never held.
func TestARefusedGroupingLeavesTheWeekAlone(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Scope: "Fundraising", Domain: "founders"},
		{ItemID: "c1", Title: "planned child", Project: "engineering", Scope: "Cozystack",
			Week: "2026-08-24"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "c1", "far"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the grouping is refused: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Week != "2026-08-24" {
		t.Fatalf("the refused grouping must not have taken the week: %+v", c)
	}
	parent, _ := findCard(b, "far")
	if parent.Week != "" {
		t.Fatalf("nor handed it to the parent: %+v", parent)
	}
}

// The file cascade follows reviewOf as well as parent (MultiBackend
// cascade), so a columned REVIEW CARD is stranded by moving its original
// exactly as a subtask is stranded by moving its parent.
func TestReFilingAnOriginalCannotStrandItsReviewCardsColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "orig", Title: "original", Project: "engineering", Scope: "Cozystack"},
		{ItemID: "rev", Title: "review", ReviewOf: "orig", Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	strategy := "strategy"
	if err := svc.SetScope(ctx, "acme", "orig", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the review card's column would be stranded: %v", err)
	}
}

// The remaining doors onto G57's invariant. A card's column must name the
// repository that holds its file, and EVERY act that moves the file has to
// say so — the guard that knows the rule was reached from three of them
// and not from the other four.

// Grouping moves the card's file, and the cascade drags its review card
// along: the review card's own column stays behind.
func TestGroupingCannotStrandAReviewCardsColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Scope: "Fundraising", Domain: "founders"},
		{ItemID: "orig", Title: "original", Team: "platform"},
		{ItemID: "rev", Title: "review", ReviewOf: "orig", Project: "engineering", Scope: "Cozystack"},
	})
	if err := New(f).SetParent(ctx, "acme", "orig", "far"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the review card follows the original and would be stranded: %v", err)
	}
}

// Pulling a subtask back out is a re-file too: the child leaves its
// parent's repository for whatever its own project or team names.
func TestUngroupingCannotStrandAFollowersColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Scope: "Fundraising", Domain: "founders"},
		{ItemID: "kid", Title: "child", Team: "platform", Parent: "far"},
		{ItemID: "rev", Title: "review of the child", ReviewOf: "kid",
			Project: "strategy", Scope: "Fundraising", Domain: "founders"},
	})
	if err := New(f).SetParent(ctx, "acme", "kid", ""); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the pull-out takes the review card's file out of its column's repository: %v", err)
	}
}

// A card with no project follows its TEAM, and the no-project bucket is a
// real column — re-teaming across repositories leaves that column behind.
func TestReTeamingCannotStrandTheCardsOwnColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-loose", Title: board.ScopeStateTitle, Scope: "Loose"},
		{ItemID: "c1", Title: "loose column card", Team: "platform", Scope: "Loose"},
	})
	f.b.SprintStates["board"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-b"}
	f.b.Domains = map[string]string{"st-b": "founders"}
	svc := New(f)
	if err := svc.SetTeam(ctx, "acme", "c1", "board", ""); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the card's column stays in the primary repository: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Team != "platform" {
		t.Fatalf("the refused move must not land: %+v", c)
	}
}

// The review link re-files the card to the original's repository (linked
// cards first), and its own column does not follow.
func TestSettingTheReviewLinkCannotStrandTheCardsOwnColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "orig", Title: "original", Project: "strategy", Scope: "Fundraising", Domain: "founders"},
		{ItemID: "c1", Title: "review", Project: "engineering", Scope: "Cozystack"},
	})
	if err := New(f).SetReviewOf(ctx, "acme", "c1", "orig"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the card would live in founders while its column names the primary: %v", err)
	}
}

// The file cascade is TRANSITIVE — it moves a card's followers, and
// theirs — so the guard has to walk as far as the cascade does. One hop
// was enough to blind it: a columnless child hides the columned review
// card hanging off it.
func TestTheGuardFollowsTheCascadeAllTheWayDown(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "grandparent", Project: "engineering", Scope: "Cozystack"},
		{ItemID: "kid", Title: "child", Parent: "p"},
		{ItemID: "rev", Title: "review of the child", ReviewOf: "kid",
			Project: "engineering", Scope: "Cozystack"},
	})
	strategy := "strategy"
	if err := New(f).SetScope(ctx, "acme", "p", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the review card two hops down would be stranded: %v", err)
	}
}

// Moving a COLUMN is the seventh door: the stub is re-filed into the
// target project's repository while a card whose file is held by a link
// stays where it is — and its project field is rewritten to name the
// column that just left.
func TestMovingAColumnCannotStrandALinkedCardStandingInIt(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent"},
		{ItemID: "c1", Title: "subtask", Parent: "p", Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	if err := svc.SetScopeProject(ctx, "acme", "engineering", "Cozystack", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the subtask's file rides its parent in the primary repository: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Project != "engineering" {
		t.Fatalf("the refused move must not have rewritten the card: %+v", c)
	}
	// A move within the repository carries everyone along as before.
	if err := svc.SetScopeProject(ctx, "acme", "engineering", "Cozystack", "freedom"); err != nil {
		t.Fatalf("a move inside the repository is free: %v", err)
	}
}

// A column cannot be moved to a project of ANOTHER repository, because its
// stub cannot follow: the store hands a stub back to the backend that
// holds it (isStub), so the column would stay declared where it was while
// its new project lives elsewhere — and every ordinary card in it, whose
// project decides where IT lives, would be re-filed away from the column
// it stands in. That is the state G57 forbids, and it is a trap: from then
// on every guard refuses the card, and no gesture in the product frees it.
func TestAColumnCannotMoveToAProjectOfAnotherRepository(t *testing.T) {
	f := columnBoard([]board.Card{
		// Teamless: a team of the primary would refuse the move first (G46),
		// and this is exactly the card that would be stranded.
		{ItemID: "plain", Title: "plain card", Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	if err := svc.SetScopeProject(ctx, "acme", "engineering", "Cozystack", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the column's stub cannot follow: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "plain")
	if c.Project != "engineering" {
		t.Fatalf("and nothing moved: %+v", c)
	}
	// An empty column is refused for the same reason: the column itself
	// would end up declared in one repository and owned by a project of
	// another, which no reader can make sense of.
	if err := svc.SetScopeProject(ctx, "acme", "freedom", "Launch", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("an empty column cannot move either: %v", err)
	}
}

// UNBINDING a column of a non-primary repository keeps it exactly where it
// is — the stub does not move — so the cards in it stay held by that same
// repository through their team. Comparing against the target PROJECT's
// repository (the primary, for the no-project bucket) refused this.
func TestUnbindingANonPrimaryColumnKeepsItsCards(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "c1", Title: "closed card", Team: "founders",
			Project: "strategy", Scope: "Fundraising", Domain: "founders"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders"}
	if err := New(f).SetScopeProject(ctx, "acme", "strategy", "Fundraising", ""); err != nil {
		t.Fatalf("the column stays in its repository and so do its cards: %v", err)
	}
}

// The grid × obeys the same two-homes rule the Project board's does: a
// card standing in a COLUMN is never deleted by it — and a subtask that
// carries one is now a drawn, counted slot in someone else's planner, so
// deleting it there destroys work that is visibly planned elsewhere.
func TestTheGridRemoveKeepsAColumnedSubtask(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "columned child", Team: "platform", Parent: "p",
			Project: "engineering", Scope: "Cozystack",
			StartDate: board.TodayIso(), Day: board.TodayIso(), SprintStart: board.TodayIso()},
	})
	if err := New(f).Remove(ctx, "acme", "c1", board.ViewAll, RemoveAuto); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("a card filed under a column is never deleted by either ×")
	}
	if c.Scope != "Cozystack" {
		t.Fatalf("the work stays planned in its column: %+v", c)
	}
	// It LEAVES THE GROUP. A subtask's person and sprint must equal its
	// parent's (S9, syncChildrenSprint), so a card released from the grid
	// while still a subtask would either break that pair or be dragged
	// back by the next carry-over (carryFollowers takes every open child).
	// Leaving the family is what makes the × mean something here.
	if c.Parent != "" {
		t.Fatalf("the × takes it out of the group: %+v", c)
	}
	// Off the day grid: an scope card out of the sprint is skipped there
	// (TeamGrid). Its DATES stay — for a card in a column they are its row
	// on the Project board, not a working-area life.
	if c.SprintStart != "" {
		t.Fatalf("and out of the sprint: %+v", c)
	}
	if c.StartDate == "" || c.Day == "" {
		t.Fatalf("its Project-board row is its dates and must survive: %+v", c)
	}
	// The parent keeps its own life.
	if p, _ := findCard(b, "p"); p.Team != "platform" {
		t.Fatalf("the parent is untouched: %+v", p)
	}
}

// A subtask with nowhere else to be is still deleted: there is no column to
// release it into, and leaving it grouped would be an × that did nothing.
func TestTheGridRemoveStillDeletesAColumnlessSubtask(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "plain child", Team: "platform", Parent: "p",
			SprintStart: board.TodayIso()},
	})
	if err := New(f).Remove(ctx, "acme", "c1", board.ViewAll, RemoveAuto); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("nowhere else to be: the × deletes it")
	}
}

// A subtask standing in a column is a first-class state now (G57), so the
// create door must produce it rather than silently drop the parent: the
// caller asked for a child and got a top-level card, which on the Project
// board is a slot in the wrong place with no way to tell.
func TestCreatingACardWithBothAParentAndAColumnKeepsBoth(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
	})
	c, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child in a column", Team: "platform", Parent: "p",
		Project: "engineering", Scope: "Cozystack",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	got, _ := findCard(b, c.ItemID)
	if got.Parent != "p" {
		t.Fatalf("the parent was asked for and must land: %+v", got)
	}
	if got.Scope != "Cozystack" {
		t.Fatalf("and the column with it: %+v", got)
	}
}

// The create door validates the parent BEFORE writing anything: a card
// born and then deleted because its parent was never findable is a stray
// ADDED broadcast and a created event for a card that should not exist.
func TestCreatingUnderAnImpossibleParentCreatesNothing(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Scope: "Fundraising", Domain: "founders"},
	})
	svc := New(f)
	before, _ := f.LoadBoard(ctx, "acme")
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "ghost",
		Project: "engineering", Scope: "Cozystack",
	}); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("a parent that does not exist: %v", err)
	}
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "far",
		Project: "engineering", Scope: "Cozystack",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a parent whose repository the column is not in: %v", err)
	}
	after, _ := f.LoadBoard(ctx, "acme")
	if len(after.Cards) != len(before.Cards) {
		t.Fatalf("neither refusal may leave a card behind: %d → %d", len(before.Cards), len(after.Cards))
	}
	// And nothing was ever written: a create-then-delete pair broadcasts a
	// parentless instant that watchers and mid-sync reloads see as a stray
	// top-level card, and logs a created event for a card that never was.
	if n := f.count("CreateCard"); n != 0 {
		t.Fatalf("the refusal must come before the write, saw %d creates", n)
	}
}

// When the grouping fails the create is undone — and if the undo fails
// too, BOTH reasons travel. One of the two create paths used to swallow
// the second and hand back the card it had just tried to delete.
func TestAFailedGroupingUndoesTheCreateAndReportsWhatItCannot(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
	})
	f.parentErr = errors.New("the grouping would not take")
	f.deleteErr = errors.New("and the branch would not budge")
	svc := New(f)
	card, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "p",
	})
	if err == nil {
		t.Fatal("the grouping failed, so the create must not stand")
	}
	if !strings.Contains(err.Error(), "would not take") ||
		!strings.Contains(err.Error(), "would not budge") {
		t.Fatalf("both reasons must travel: %v", err)
	}
	if card.ItemID != "" {
		t.Fatalf("and no card is handed back: %+v", card)
	}
}

// Deleting a parent FREES its children, and a freed child keeps the one
// column it carries (G57). That release is the one ungroup that does not go
// through refileGuard — it cannot, since the parent is being deleted — so
// the invariant it would have checked is pinned here instead: the freed
// card's column still names the repository that holds it. It holds because
// a subtask's team is its parent's (S9) and a team cannot be paired with a
// project of another repository (G46), but "it follows from two other
// rules" is exactly the kind of claim that stops being true quietly.
func TestAFreedSubtasksColumnStillNamesItsRepository(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform", Assignees: []string{"kvaps"}},
		{ItemID: "kid", Title: "columned child", Team: "platform", Parent: "p",
			Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	if err := svc.DeleteCard(ctx, "acme", "p"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "kid")
	if !ok {
		t.Fatal("a subtask is freed, not deleted, with its parent")
	}
	if c.Parent != "" {
		t.Fatalf("freed: %+v", c)
	}
	cd, known := board.ColumnDomain(b, c.Project, c.Scope)
	if !known {
		t.Fatalf("the column it kept must still be declared: %+v", c)
	}
	if mine := board.DomainOf(c, board.Resolver(b, "")); cd != mine {
		t.Fatalf("and must name the repository that holds the card: column %q, card %q", cd, mine)
	}
}

// The review link the same way, on the scope path.
func TestCreatingAColumnedReviewCardKeepsTheLink(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "orig", Title: "original", Team: "platform"},
	})
	c, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "review", Team: "platform", ReviewOf: "orig",
		Project: "engineering", Scope: "Cozystack",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	got, _ := findCard(b, c.ItemID)
	if got.ReviewOf != "orig" {
		t.Fatalf("a review card was asked for: %+v", got)
	}
}

// Unbinding a column into the no-project bucket is a rename into a shared
// namespace: two columns of the same name merge under one entry there
// (NewBoard dedups by the pair), and the losing stub's cards are then
// refused at every door with nothing in the product to free them. The
// name has to be free, exactly as it must be for any other destination.
func TestUnbindingACollidingColumnIsRefused(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-loose", Title: board.ScopeStateTitle, Scope: "Launch"},
	})
	if err := New(f).SetScopeProject(ctx, "acme", "freedom", "Launch", ""); err == nil {
		t.Fatal("the no-project bucket already holds a Launch column")
	}
}

// The create door's guard ran only when a PARENT was named, and a review
// link outranks the project just as a parent does (G14): a card created
// with reviewOf in one repository and a column in another was born into
// the state every other door refuses — file here, column there.
func TestCreatingAColumnedReviewCardAcrossRepositoriesIsRefused(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "orig", Title: "original", Project: "strategy", Scope: "Fundraising", Domain: "founders"},
	})
	svc := New(f)
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "review", ReviewOf: "orig",
		Project: "engineering", Scope: "Cozystack",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the card would live in founders while its column names the primary: %v", err)
	}
	if n := f.count("CreateCard"); n != 0 {
		t.Fatalf("and the refusal comes before the write: %d creates", n)
	}
}

// The probe that answers before the write must model the card the request
// actually asks for. It carried the parent but not the review link, and
// DomainOf reads the link FIRST — so the two disagreed, the guard passed,
// and the real refusal came from inside SetParent: after the create, with
// the stray ADDED broadcast and created event that probe exists to avoid.
func TestTheCreateProbeReadsTheLinkBeforeTheParent(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "orig", Title: "original", Project: "strategy", Scope: "Fundraising", Domain: "founders"},
	})
	svc := New(f)
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "review of an original elsewhere", Team: "platform",
		Parent: "p", ReviewOf: "orig",
		Project: "engineering", Scope: "Cozystack",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the link decides where this card lives: %v", err)
	}
	if n := f.count("CreateCard"); n != 0 {
		t.Fatalf("nothing may be written before the refusal: %d creates", n)
	}
}

// Create is a door like any other, and a COLUMN is what G57 is about: the
// guard skipped every request that named no link, so the one gesture that
// could still write "file here, column there" was the one that makes the
// card. Both of the shapes the rest of the branch refuses elsewhere.
func TestCreatingIntoAColumnOfAnotherRepositoryIsRefused(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-loose", Title: board.ScopeStateTitle, Scope: "Loose"},
		{ItemID: "ep-far", Title: board.ScopeStateTitle, Scope: "Far", Project: "engineering", Domain: "founders"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-far": "founders"}
	svc := New(f)

	// The team holds this card in founders; the column is of the primary.
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "closed card in a primary column", Team: "founders", Scope: "Loose",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a no-project column of the primary cannot hold it: %v", err)
	}
	// The same project NAME declared twice (G13): the COLUMN decides, and
	// this one was declared in founders.
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "primary card in a closed column", Project: "engineering", Scope: "Far",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the column's own repository is what answers: %v", err)
	}
	if n := f.count("CreateCard"); n != 0 {
		t.Fatalf("and both refusals come before the write: %d creates", n)
	}
}

// Freeing a subtask is the one ungroup that cannot ask the guard — its
// parent is being deleted — and the invariant does NOT simply follow from
// the other rules when the parent's domain comes from a LINK: a review
// card takes its original's repository, a child grouped under it is
// lawful there, and deleting the original drops the child into the
// primary while its column stays behind. The freed card keeps its column
// only while that column still names the repository that holds it.
func TestFreeingASubtaskDropsAColumnItsRepositoryNoLongerHolds(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.ScopeStateTitle, Scope: "Closed", Domain: "founders"},
		{ItemID: "orig", Title: "original", Team: "founders"},
		{ItemID: "rev", Title: "review", ReviewOf: "orig"},
		{ItemID: "kid", Title: "child", Parent: "rev", Scope: "Closed"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).DeleteCard(ctx, "acme", "orig"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "kid")
	if !ok {
		t.Fatal("a subtask is freed, not deleted, with its parent")
	}
	if c.Scope != "" {
		cd, _ := board.ColumnDomain(b, c.Project, c.Scope)
		t.Fatalf("the column names %q, the card lives in %q: %+v",
			cd, board.DomainOf(c, board.Resolver(b, "")), c)
	}
}

// A subtask has no week of its own — grouping hands it to the parent — so a
// create asking for both is asking for two contradictory things. It used to
// be answered by scheduling THE PARENT for the week the request named for
// the child: a mutation of a card nobody asked about.
func TestCreatingACardOfAWeekUnderAParentIsRefused(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
	})
	svc := New(f)
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "planned child", Team: "platform", Parent: "p", Week: "2026-08-24",
	}); !errors.Is(err, ErrSubtaskWeek) {
		t.Fatalf("a subtask has no week of its own: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	parent, _ := findCard(b, "p")
	if parent.Week != "" {
		t.Fatalf("and the parent keeps its own schedule: %+v", parent)
	}
	if n := f.count("CreateCard"); n != 0 {
		t.Fatalf("the refusal comes before the write: %d creates", n)
	}
}

// A card is a subtask or a card of its own week, never both (G58) — and a
// WEEK given to a standing subtask is how a person says "take it out of the
// group and schedule it". A refusal here would leave them with no gesture:
// grouping has already taken the card's week, and the × deletes a subtask,
// so a card dropped under the wrong parent could not be freed at all. It is
// pulled out, and the week lands on the card that comes back.
func TestGivingAStandingSubtaskAWeekTakesItOutOfTheGroup(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "kid", Title: "child", Team: "platform", Parent: "p"},
	})
	svc := New(f)
	if err := svc.SetWeek(ctx, "acme", "kid", "2026-08-24"); err != nil {
		t.Fatalf("the week takes it out of the group: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "kid")
	if c.Parent != "" {
		t.Fatalf("out of the group: %+v", c)
	}
	if c.Week != "2026-08-24" {
		t.Fatalf("with the week it was given: %+v", c)
	}
	// Clearing is still free, and still means nothing more than clearing.
	if err := svc.SetWeek(ctx, "acme", "kid", ""); err != nil {
		t.Fatalf("clearing is free: %v", err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	if c, _ := findCard(b, "kid"); c.Parent != "" {
		t.Fatalf("and does not regroup anything: %+v", c)
	}
}

// The CREATE door still refuses the pair: a card born as both is a request
// for two contradictory things, and there is no gesture to interpret.
func TestCreatingACardThatIsBothIsStillRefused(t *testing.T) {
	f := columnBoard([]board.Card{{ItemID: "p", Title: "parent", Team: "platform"}})
	if _, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "both", Team: "platform", Parent: "p", Week: "2026-08-24",
	}); !errors.Is(err, ErrSubtaskWeek) {
		t.Fatalf("a create names one or the other: %v", err)
	}
}

// A create whose grouping fails leaves NOTHING behind: the point of the
// undo, and the case the double-failure test cannot show, since there the
// undo fails too.
func TestAFailedGroupingLeavesNoCardBehind(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
	})
	f.parentErr = errors.New("the grouping would not take")
	before, _ := f.LoadBoard(ctx, "acme")
	card, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "p",
	})
	if err == nil {
		t.Fatal("the grouping failed")
	}
	if card.ItemID != "" {
		t.Fatalf("no card is handed back: %+v", card)
	}
	after, _ := f.LoadBoard(ctx, "acme")
	if len(after.Cards) != len(before.Cards) {
		t.Fatalf("and none is left on the board: %d → %d", len(before.Cards), len(after.Cards))
	}
}

// The one state this codebase repairs instead of refusing is RECORDED —
// a column that disappears from a card without a line in its history is a
// card nobody can account for afterwards.
func TestTheDroppedColumnOfAFreedSubtaskIsLogged(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.ScopeStateTitle, Scope: "Closed", Domain: "founders"},
		{ItemID: "orig", Title: "original", Team: "founders"},
		{ItemID: "rev", Title: "review", ReviewOf: "orig"},
		{ItemID: "kid", Title: "child", Parent: "rev", Scope: "Closed"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).DeleteCard(ctx, "acme", "orig"); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range f.eventsOf("kid") {
		if e.Kind == board.EventScope && e.From == " / Closed" && e.To == "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the repair must be in the card's history: %+v", f.eventsOf("kid"))
	}
}

// stampedBoard is the board the STORE hands over: every roster entry
// carries its domain's name, the primary included (gitstore stamps domain
// zero like any other), and the board names that primary. Fixtures that
// leave the primary blank model a shape no server produces — and the
// column rules, which compare a stamped column against a card's
// unstamped placement, were green on those fixtures and refused half the
// board on a real one.
func stampedBoard(cards []board.Card) *fakeBackend {
	roster := []board.Card{
		{ItemID: "pr-e", Title: board.ProjectStateTitle, Project: "engineering", Domain: "aeman-db"},
		{ItemID: "ep-cozy", Title: board.ScopeStateTitle, Scope: "Cozystack", Project: "engineering", Domain: "aeman-db"},
		{ItemID: "ep-inbox", Title: board.ScopeStateTitle, Scope: "Inbox", Domain: "aeman-db"},
		{ItemID: "ep-other", Title: board.ScopeStateTitle, Scope: "Other", Domain: "aeman-db"},
		{ItemID: "pr-s", Title: board.ProjectStateTitle, Project: "strategy", Domain: "founders"},
		{ItemID: "ep-fund", Title: board.ScopeStateTitle, Scope: "Fundraising", Project: "strategy", Domain: "founders"},
	}
	f := newFake(append(roster, cards...), map[string]board.SprintState{
		"platform": {Current: board.TodayIso(), ItemID: "st-p"},
	})
	f.b.Primary = "aeman-db"
	f.b.Domains = map[string]string{"st-p": "aeman-db"}
	return f
}

// The repair must not fire on a card whose column was fine: deleting a
// parent freed the child and stripped its column, because the column read
// "aeman-db" and the freed card read "".
func TestAStampedBoardKeepsAFreedSubtasksLawfulColumn(t *testing.T) {
	f := stampedBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "platform", Scope: "Inbox"},
	})
	if err := New(f).DeleteCard(ctx, "acme", "p"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "kid")
	if !ok {
		t.Fatal("the child is freed, not deleted")
	}
	if c.Scope != "Inbox" {
		t.Fatalf("its column was never stranded and must survive: %+v", c)
	}
}

// Every refusal fires BEFORE anything is written. The × above repairs the
// card's own stranded column rather than refusing over it — but a REVIEW
// CARD standing in a column of the parent's repository follows the subtask
// out and cannot, so the gesture is refused. Repairing first and asking
// afterwards left the refusal on top of a write: an emptied column in a
// commit the request never makes, kept by the server's cache until
// something else reloads the card.
func TestTheGridRemoveWritesNothingWhenItRefusesOverAFollower(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.ScopeStateTitle, Scope: "Closed", Domain: "founders"},
		{ItemID: "p", Title: "parent in the closed repository", Team: "founders", Domain: "founders"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "platform", Scope: "Closed",
			SprintStart: board.TodayIso()},
		{ItemID: "rev", Title: "review of the child", ReviewOf: "kid", Team: "founders",
			Scope: "Closed", Domain: "founders"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	err := New(f).Remove(ctx, "acme", "kid", board.ViewAll, RemoveAuto)
	if !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the follower's column refuses the pull-out: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "kid")
	if !ok {
		t.Fatal("a refused × deletes nothing")
	}
	if c.Parent != "p" || c.Scope != "Closed" {
		t.Fatalf("a refused × writes nothing: %+v", c)
	}
	if f.count("SetScope") > 0 {
		t.Fatalf("not even the repair: %v", f.log)
	}
}

// The × on a subtask whose column cannot come along answers like every
// other columnless card: the working area was its last home, so the card
// is deleted. It must never be left ALIVE with nothing — no sprint, no
// dates, no band and no column — which is a card on no board anyone can
// open, findable only by id by someone who already knows it exists. The
// day it stood on keeps it (G60).
func TestAStrandedColumnDoesNotLeaveACardNowhere(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.ScopeStateTitle, Scope: "Closed", Domain: "founders"},
		{ItemID: "p", Title: "parent in the closed repository", Team: "founders"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "platform", Scope: "Closed",
			SprintStart: board.TodayIso(), StartDate: board.AddDays(board.TodayIso(), -2)},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).Remove(ctx, "acme", "kid", board.ViewAll, RemoveAuto); err != nil {
		t.Fatalf("the × must complete: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if c, ok := findCard(b, "kid"); ok {
		t.Fatalf("nothing kept it: the column could not follow and the working area was its last home: %+v", c)
	}
}

// The other half of the same law: with nothing to demote INTO, the × on
// such a card means what it means for every other card whose last home it
// empties — deletion. The alternative is the state above, kept alive for
// the sake of not deleting it.
func TestAStrandedColumnWithNoSprintToDemoteIntoDeletesTheCard(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.ScopeStateTitle, Scope: "Closed", Domain: "founders"},
		{ItemID: "st-n", Title: board.SprintStateTitle, Team: "newcomers"},
		{ItemID: "p", Title: "parent in the closed repository", Team: "founders"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "newcomers", Scope: "Closed",
			SprintStart: board.TodayIso()},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).Remove(ctx, "acme", "kid", board.ViewAll, RemoveAuto); err != nil {
		t.Fatalf("the × must complete: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "kid"); ok {
		t.Fatal("nowhere left to be, and nothing keeping it: the card is deleted")
	}
}

// The grid × on a subtask whose column belongs to its PARENT's repository
// completes: ungrouping re-files the card by its own team, so the column is
// stranded by the gesture itself, and refusing over it would name a column
// the person did not touch in answer to a move they did. The column is
// repaired — the same repair a deleted parent's release makes — and the
// card, now an ordinary columnless one, is deleted like any other.
func TestTheGridRemoveCompletesWhenUngroupingStrandsTheColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.ScopeStateTitle, Scope: "Closed", Domain: "founders"},
		{ItemID: "p", Title: "parent in the closed repository", Team: "founders"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "platform", Scope: "Closed",
			SprintStart: board.TodayIso()},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).Remove(ctx, "acme", "kid", board.ViewAll, RemoveAuto); err != nil {
		t.Fatalf("the × must complete: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if c, ok := findCard(b, "kid"); ok {
		t.Fatalf("the gesture completes and the card is gone: %+v", c)
	}
	// The repair happened all the same: the refusal it exists to prevent
	// never fired, and the × did not stop half way.
	if f.count("SetScope") == 0 {
		t.Fatalf("the stranded column is repaired on the way out: %v", f.log)
	}
}

// A card's own column must name the repository that HOLDS the card's file,
// which is not always the project's: a subtask's file rides its parent and a
// review card's follows its original, so a column of another repository on
// either is refused, while an ordinary teamless card follows its project.
func TestAColumnCannotNameARepositoryThatDoesNotHoldTheCard(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "p", Title: "parent", Project: "engineering", Scope: "Cozystack"},
		{ItemID: "c1", Title: "child", Parent: "p"},
		{ItemID: "rev", Title: "review", ReviewOf: "p"},
		// Teamless on purpose: a team of the primary repository would refuse
		// the move first (G46), before this rule gets a say.
		{ItemID: "plain", Title: "ordinary", Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	strategy := "strategy"
	if err := svc.SetScope(ctx, "acme", "c1", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the subtask's file rides its parent, in the primary repository: %v", err)
	}
	if err := svc.SetScope(ctx, "acme", "rev", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the review card's file follows its original: %v", err)
	}
	// The same repository is free — a subtask may carry its own column (G14).
	freedom := "freedom"
	if err := svc.SetScope(ctx, "acme", "c1", "Launch", &freedom); err != nil {
		t.Fatalf("a column of its own repository is a subtask's right: %v", err)
	}
	// And an ordinary teamless card still moves BETWEEN repositories freely:
	// there the project is what decides, so the file follows the column.
	if err := svc.SetScope(ctx, "acme", "plain", "Fundraising", &strategy); err != nil {
		t.Fatalf("an ordinary card's file follows its project: %v", err)
	}
}

// A project NAME may be declared in two repositories (G13); a column answers
// for the repository of its OWN stub, so attaching a primary card to the
// alias project's column is a cross-repository move, refused.
func TestAColumnOfAnAliasProjectAnswersForItself(t *testing.T) {
	f := columnBoard([]board.Card{
		// "engineering" is declared in the primary; the same NAME is
		// declared again in founders, carrying its own column.
		{ItemID: "pr-e2", Title: board.ProjectStateTitle, Project: "engineering", Domain: "founders"},
		{ItemID: "ep-closed", Title: board.ScopeStateTitle, Scope: "Closed work",
			Project: "engineering", Domain: "founders"},
		{ItemID: "c1", Title: "primary card", Project: "engineering", Scope: "Cozystack"},
	})
	svc := New(f)
	none := "engineering"
	if err := svc.SetScope(ctx, "acme", "c1", "Closed work", &none); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the column lives in founders, the card does not: %v", err)
	}
}

// On a board whose entries are stamped with their domain's name — the
// primary included — the rules compare in ONE namespace: a column of the
// primary and an unstamped card are the same repository.
func TestTheRulesReadOneNamespaceOnAStampedBoard(t *testing.T) {
	f := stampedBoard([]board.Card{
		{ItemID: "c1", Title: "in the bucket", Scope: "Inbox"},
	})
	svc := New(f)

	// Creating into that column is not a cross-repository move.
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{Title: "another", Scope: "Inbox"}); err != nil {
		t.Fatalf("the bucket is a column of this very board: %v", err)
	}
	// Nor is attaching to a sibling column.
	none := ""
	if err := svc.SetScope(ctx, "acme", "c1", "Other", &none); err != nil {
		t.Fatalf("the card may be re-filed between columns of the primary: %v", err)
	}
}
