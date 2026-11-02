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

// Attaching a card to a column of ANOTHER repository is refused whether or
// not that column has a project: a card with no project and no team lives in
// the primary, and a no-project column declared in the closed repository
// cannot hold a file its readers may not have. The create door and this one
// must answer alike, or one of them is a way in.
func TestAttachingToAForeignNoProjectColumnIsRefused(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-far", Title: board.ScopeStateTitle, Scope: "Far", Domain: "founders"},
		{ItemID: "c1", Title: "a card of the primary"},
	})
	none := ""
	if err := New(f).SetScope(ctx, "acme", "c1", "Far", &none); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a column of the closed repository cannot hold a card of the primary: %v", err)
	}
}

// Every refusal fires BEFORE anything is written. The grouping's week
// handover ran first, so a refused grouping still took the child's week away
// and gave it to the parent — in the cache only, since the commit never
// happened: the board then showed a state git had never held. The refusal
// here is H4's (a card in a column is nobody's subtask); the rule it pins is
// the order.
func TestARefusedGroupingLeavesTheWeekAlone(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Team: "platform"},
		{ItemID: "c1", Title: "planned child", Project: "engineering", Scope: "Cozystack",
			Week: "2026-08-24"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "c1", "far"); !errors.Is(err, ErrProjectCard) {
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

// The remaining doors onto G57's invariant. A card's column must name the
// repository that holds its file, and EVERY act that moves the file has to
// say so — the guard that knows the rule was reached from three of them
// and not from the other four.

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
	}); !errors.Is(err, ErrProjectCard) {
		t.Fatalf("a parent that stands in a column groups nothing (H4): %v", err)
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

// Create is a door like any other: the guard used to skip every request
// that named no link, so the one gesture that could still write "file here,
// column there" was the one that makes the card. Both of the shapes the rest
// of the branch refuses elsewhere — a project card has no team and no link,
// so what decides where it lives is its project, or the primary when the
// column has none.
func TestCreatingIntoAColumnOfAnotherRepositoryIsRefused(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "ep-closed-loose", Title: board.ScopeStateTitle, Scope: "ClosedLoose", Domain: "founders"},
		{ItemID: "ep-far", Title: board.ScopeStateTitle, Scope: "Far", Project: "engineering", Domain: "founders"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-far": "founders", "ep-closed-loose": "founders"}
	svc := New(f)

	// A card with no project lives in the primary; the no-project column is
	// declared in founders.
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "primary card in a closed no-project column", Scope: "ClosedLoose",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a no-project column of the closed repository cannot hold it: %v", err)
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
