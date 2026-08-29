package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// mirrorBoard: two projects in the primary repository (engineering with the
// column Cozystack, freedom with Launch), one in another (strategy with
// Fundraising), and a process.
func mirrorBoard(cards []board.Card) *fakeBackend {
	roster := []board.Card{
		{ItemID: "pr-e", Title: board.ProjectStateTitle, Project: "engineering"},
		{ItemID: "pr-f", Title: board.ProjectStateTitle, Project: "freedom"},
		{ItemID: "pr-s", Title: board.ProjectStateTitle, Project: "strategy", Domain: "founders"},
		{ItemID: "ep-cozy", Title: board.EpicStateTitle, Epic: "Cozystack", Project: "engineering"},
		{ItemID: "ep-launch", Title: board.EpicStateTitle, Epic: "Launch", Project: "freedom"},
		{ItemID: "ep-fund", Title: board.EpicStateTitle, Epic: "Fundraising", Project: "strategy", Domain: "founders"},
		{ItemID: "proc-pay", Title: board.ProcessStateTitle, Process: "Invoicing"},
	}
	return newFake(append(roster, cards...), map[string]board.SprintState{
		"platform": {Current: board.TodayIso(), Previous: board.AddDays(board.TodayIso(), -1), ItemID: "st-p"},
	})
}

// Mirror adds a second column to a card: the same card, one file and one
// log, shown in two projects — so shared work is one card on one person,
// not a duplicate per project drifting apart.
func TestMirrorAddsASecondColumn(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			StartDate: "2026-08-24", Day: "2026-08-28", Week: "2026-08-24"},
	})
	svc := New(f)
	if err := svc.Mirror(ctx, "acme", "c1", "freedom", "Launch"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if !board.Mirrored(c, "freedom", "Launch") {
		t.Fatalf("the mirror was not recorded: %+v", c.Mirrors)
	}
	if c.Project != "engineering" || c.Epic != "Cozystack" {
		t.Fatalf("the home column must not move: %+v", c)
	}
	if !board.InEpic(c, "freedom", "Launch") || !board.InEpic(c, "engineering", "Cozystack") {
		t.Fatal("the card stands in both columns")
	}
	// Idempotent: mirroring where it already stands changes nothing.
	if err := svc.Mirror(ctx, "acme", "c1", "freedom", "Launch"); err != nil {
		t.Fatal(err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	c, _ = findCard(b, "c1")
	if len(c.Mirrors) != 1 {
		t.Fatalf("mirroring twice must not duplicate: %+v", c.Mirrors)
	}
}

// The guards: no mirror without a home column, no mirror onto the home
// itself, no mirror into a column that does not exist, and no mirror across
// repositories — a card is one file in one repository, and a column
// elsewhere cannot show a file its readers may not have (G15).
func TestMirrorGuards(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack"},
		{ItemID: "plain", Title: "no column", Team: "platform"},
	})
	svc := New(f)
	if err := svc.Mirror(ctx, "acme", "plain", "freedom", "Launch"); err == nil {
		t.Fatal("a card without a home column cannot be mirrored — attach it first")
	}
	if err := svc.Mirror(ctx, "acme", "c1", "engineering", "Cozystack"); err == nil {
		t.Fatal("the home column is not a mirror target")
	}
	if err := svc.Mirror(ctx, "acme", "c1", "freedom", "Nope"); !errors.Is(err, ErrEpicNotFound) {
		t.Fatalf("a column that does not exist: %v", err)
	}
	if err := svc.Mirror(ctx, "acme", "c1", "strategy", "Fundraising"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a column in another repository: %v, want ErrCrossDomain", err)
	}
}

// Unmirror takes one column away and touches nothing else.
func TestUnmirrorRemovesOnePlacement(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	if err := svc.Unmirror(ctx, "acme", "c1", "freedom", "Launch"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if len(c.Mirrors) != 0 || c.Project != "engineering" || c.Epic != "Cozystack" {
		t.Fatalf("only the mirror goes: %+v", c)
	}
	if err := svc.Unmirror(ctx, "acme", "c1", "freedom", "Launch"); err == nil {
		t.Fatal("unmirroring a column the card does not mirror is an error")
	}
}

// The Project board's × removes the card FROM THAT COLUMN, and what that
// means depends on which column it is: a mirror just goes; the home, with
// mirrors left, hands the home role to the first mirror; the last column
// drops the card from the weekly plan ALWAYS, and the card survives only in
// the working area — only when it was worked on. An untouched card with no
// other column is deleted outright.
func TestRemoveFromProjectMirrorGoesHomeStays(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "freedom", "Launch"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if len(c.Mirrors) != 0 || c.Project != "engineering" {
		t.Fatalf("the mirror goes, the home stays: %+v", c)
	}
}

func TestRemoveFromProjectPromotesTheFirstMirror(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			StartDate: "2026-08-24", Day: "2026-08-28", Week: "2026-08-24",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "engineering", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Project != "freedom" || c.Epic != "Launch" || len(c.Mirrors) != 0 {
		t.Fatalf("the first mirror becomes the home: %+v", c)
	}
	if c.StartDate != "2026-08-24" || c.Week != "2026-08-24" {
		t.Fatalf("the dates are shared and stay: %+v", c)
	}
}

func TestRemoveFromLastProjectDropsThePlanAndKeepsOnlyWorkedCards(t *testing.T) {
	worked := board.Card{ItemID: "w", Title: "worked", Team: "platform", Project: "engineering", Epic: "Cozystack",
		Assignees: []string{"kvaps"}, Progress: 40,
		Plan: board.PlanFri, Week: "2026-08-24",
		SprintStart: board.TodayIso(), StartDate: board.TodayIso(), Day: board.TodayIso()}
	idle := board.Card{ItemID: "i", Title: "idle", Team: "platform", Project: "engineering", Epic: "Cozystack",
		Plan: board.PlanFri, Week: "2026-08-24",
		SprintStart: board.TodayIso(), StartDate: board.TodayIso(), Day: board.TodayIso()}
	f := mirrorBoard([]board.Card{worked, idle})
	svc := New(f)

	if err := svc.RemoveFromProject(ctx, "acme", "w", "engineering", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "w")
	if !ok {
		t.Fatal("a worked card survives as an orphan of the working area")
	}
	if c.Plan != board.PlanNone || c.Week != "" {
		t.Fatalf("the weekly plan is left ALWAYS on the last column: %+v", c)
	}
	if c.Project != "" || c.Epic != "" {
		t.Fatalf("the column is gone: %+v", c)
	}
	if c.SprintStart == "" || len(c.Assignees) == 0 {
		t.Fatalf("the working area — the person and the sprint — stays: %+v", c)
	}

	if err := svc.RemoveFromProject(ctx, "acme", "i", "engineering", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "i"); ok {
		t.Fatal("an untouched card with no other column is deleted outright")
	}
}

func TestRemoveFromProjectRefusesAColumnTheCardIsNotIn(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "x", Team: "platform", Project: "engineering", Epic: "Cozystack"},
	})
	if err := New(f).RemoveFromProject(ctx, "acme", "c1", "freedom", "Launch"); err == nil {
		t.Fatal("the card does not stand in that column")
	}
}

// An empty pair is no column. A card standing in no column has an empty
// home, which "matched" ("", "") and fell through to the last-column branch
// — the call that asked to remove a card from nowhere deleted it outright.
// The MCP tool feeds this service without validating, and an agent calling
// remove_from_project with empty halves on a column-less card is the most
// expectable mistake there is.
func TestRemoveFromProjectRefusesTheEmptyPair(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "no column", Team: "platform"},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "", ""); !errors.Is(err, ErrNotInProject) {
		t.Fatalf("the empty pair must be refused: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "c1"); !ok {
		t.Fatal("the refused call must not have deleted the card")
	}
}

// Renames follow the mirrors. The rename loops match cards through InEpic,
// which now sees mirrors — rewriting the card's HOME fields for a mirror
// match would corrupt it, and not rewriting the mirror would strand it
// under a name that no longer exists (issue #124's lesson, again).
func TestRenameEpicRewritesMirrorsAndLeavesTheHomeAlone(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	if err := svc.RenameEpic(ctx, "acme", "freedom", "Launch", "Liftoff"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Project != "engineering" || c.Epic != "Cozystack" {
		t.Fatalf("the home fields must survive a mirror's rename: %+v", c)
	}
	if !board.Mirrored(c, "freedom", "Liftoff") || board.Mirrored(c, "freedom", "Launch") {
		t.Fatalf("the mirror follows the rename: %+v", c.Mirrors)
	}
}

func TestRenameProjectRewritesMirrors(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	if err := svc.RenameProject(ctx, "acme", "freedom", "liberty"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if !board.Mirrored(c, "liberty", "Launch") {
		t.Fatalf("the mirror follows the project rename: %+v", c.Mirrors)
	}
	if c.Project != "engineering" {
		t.Fatalf("the home is untouched: %+v", c)
	}
}

// A weekly-plan card attached to a project takes the weekly slot it was
// taken from: its plan week becomes the slot's row — start on that Monday,
// end on its band's day — so the card does not jump to another week on the
// way to the Project board.
func TestAttachingAPlanCardTakesItsWeeksSlot(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "planned", Team: "platform", Plan: board.PlanFri, Week: "2026-08-24"},
		{ItemID: "c2", Title: "planned wed", Team: "platform", Plan: board.PlanWed, Week: "2026-08-24"},
	})
	svc := New(f)
	project := "engineering"
	if err := svc.SetEpic(ctx, "acme", "c1", "Cozystack", &project); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.StartDate != "2026-08-24" || c.Day != "2026-08-28" {
		t.Fatalf("a by-Friday card spans its week to Friday: %+v", c)
	}
	if c.Week != "2026-08-24" {
		t.Fatalf("the slot's row is the week it came from: %+v", c)
	}
	if err := svc.SetEpic(ctx, "acme", "c2", "Cozystack", &project); err != nil {
		t.Fatal(err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	c, _ = findCard(b, "c2")
	if c.StartDate != "2026-08-24" || c.Day != "2026-08-26" {
		t.Fatalf("a by-Wednesday card ends on its Wednesday: %+v", c)
	}
}

// A recurrent card is attached to a PROCESS instead of a project — the same
// gesture, the recurring shelf: its Process field names it, and the process
// must exist (a typo is not a new process).
func TestSetCardProcess(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "weekly chore", Team: "platform", Stage: board.StageRecurrent},
	})
	svc := New(f)
	if err := svc.SetCardProcess(ctx, "acme", "c1", "Invoicing"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Process != "Invoicing" {
		t.Fatalf("the card names its process: %+v", c)
	}
	if err := svc.SetCardProcess(ctx, "acme", "c1", "Nope"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("an unknown process: %v", err)
	}
	if err := svc.SetCardProcess(ctx, "acme", "c1", ""); err != nil {
		t.Fatal(err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	c, _ = findCard(b, "c1")
	if c.Process != "" {
		t.Fatalf("empty clears: %+v", c)
	}
}

// The mirror invariant must survive the OLD ways a card is re-filed, not
// only the new actions. Three collisions, each reachable from the UI or a
// plain PATCH before this test existed:
//
//   - the home dragged onto a column the card already mirrors left a
//     mirror entry EQUAL to the home — Mirrored(home) turned true, the ×
//     unmirrored instead of removing, and the Project board drew the slot
//     twice under one key;
//   - clearing the epic left mirrors on a card outside every project —
//     invisible on the Project board, yet counted by InEpic, so DeleteEpic
//     refused for cards nobody could see;
//   - re-filing a teamless card into a project of another repository (or
//     moving a whole column there) carried the home across while the
//     mirrors stayed behind: the very state ErrCrossDomain exists to
//     forbid.
func TestReFilingTheHomeOntoAMirrorDropsTheDuplicate(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	project := "freedom"
	if err := svc.SetEpic(ctx, "acme", "c1", "Launch", &project); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Project != "freedom" || c.Epic != "Launch" {
		t.Fatalf("the home moved: %+v", c)
	}
	if len(c.Mirrors) != 0 {
		t.Fatalf("a mirror equal to the home is no mirror — it is dropped: %+v", c.Mirrors)
	}
}

func TestClearingTheEpicClearsTheMirrorsToo(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	if err := svc.SetEpic(ctx, "acme", "c1", "", nil); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Epic != "" || c.Project != "" {
		t.Fatalf("the column is cleared: %+v", c)
	}
	if len(c.Mirrors) != 0 {
		t.Fatalf("a card outside every project has no mirrors: %+v", c.Mirrors)
	}
}

func TestReFilingAMirroredCardIntoAnotherRepositoryIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "teamless", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	project := "strategy"
	err := svc.SetEpic(ctx, "acme", "c1", "Fundraising", &project)
	if !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the home cannot leave its mirrors' repository: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Project != "engineering" {
		t.Fatalf("a refused move changes nothing: %+v", c)
	}
}

func TestMovingAColumnToAnotherRepositoryIsRefusedWhileCardsMirrorIt(t *testing.T) {
	// The card is teamless on purpose: a team of the primary repository
	// would refuse the move first (G46), before the mirror rule gets a say.
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	// Moving Launch's column into the founders repository would leave c1's
	// mirror pointing across repositories: refused, not rewritten.
	if err := svc.SetEpicProject(ctx, "acme", "freedom", "Launch", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a column with cross-repo mirrors on it cannot move there: %v", err)
	}
	// And refused BEFORE anything moves: a guard that fires after the
	// column's stub has been re-parented leaves the stub in one project and
	// every card in another — half a column gone.
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := board.FindEpic(b, "freedom", "Launch"); !ok {
		t.Fatal("the refused move re-parented the column's stub anyway")
	}
}

// The other direction of the same rule: a card whose HOME is in the moved
// column, with mirrors elsewhere, must not be carried into another
// repository while its mirrors stay behind. (Teamless on purpose: a team
// would trip G46 first.)
func TestMovingAColumnCannotCarryAHomeAwayFromItsMirrors(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	if err := svc.SetEpicProject(ctx, "acme", "engineering", "Cozystack", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the home cannot leave its mirrors' repository by a column move: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := board.FindEpic(b, "engineering", "Cozystack"); !ok {
		t.Fatal("a refused move changes nothing")
	}
	c, _ := findCard(b, "c1")
	if c.Project != "engineering" {
		t.Fatalf("the card stays put: %+v", c)
	}
}

// Unbinding a column into the no-project bucket while mirrored cards stand
// on it is refused with a reason a person can act on — not a sentence about
// the repository of "".
func TestUnbindingAMirroredColumnSaysUnmirrorFirst(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	err := svc.SetEpicProject(ctx, "acme", "engineering", "Cozystack", "")
	if !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("unbinding under mirrors: %v", err)
	}
	if !strings.Contains(err.Error(), "unmirror") {
		t.Fatalf("the refusal must say what to do: %v", err)
	}
}

// A subtask is placed nowhere of its own — the boards skip it and render it
// under its parent — so a mirror on it would be a placement nobody sees:
// counted by InEpic, invisible on every grid, and DeleteEpic would refuse a
// column that looks empty. Mirroring a subtask is refused, and grouping a
// mirrored card clears its mirrors the way it clears its plan slot; the
// home column stays (G14 blesses a subtask carrying its own column), so the
// emptied column can then be deleted.
func TestSubtasksCarryNoMirrors(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "child-to-be", Team: "platform", Parent: "p",
			Project: "engineering", Epic: "Cozystack"},
	})
	svc := New(f)
	if err := svc.Mirror(ctx, "acme", "c1", "freedom", "Launch"); !errors.Is(err, ErrSubtaskMirror) {
		t.Fatalf("a subtask cannot be mirrored: %v", err)
	}

	f2 := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "mirrored", Team: "platform",
			Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc = New(f2)
	// While the mirror stands, the column it points at is OCCUPIED — by a
	// placement no board draws — and must refuse deletion: the negative
	// half, the one that fails without a mirrors-aware InEpic. (DeleteEpic
	// takes the column's NAME first, then its project — swapped once, this
	// whole test proved a no-op on a column that does not exist.)
	if err := svc.DeleteEpic(ctx, "acme", "Launch", "freedom"); !errors.Is(err, ErrEpicInUse) {
		t.Fatalf("a column held only by a mirror is still held: %v", err)
	}
	if err := svc.SetParent(ctx, "acme", "c1", "p"); err != nil {
		t.Fatal(err)
	}
	b, _ := f2.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if len(c.Mirrors) != 0 {
		t.Fatalf("grouping clears the mirrors: %+v", c.Mirrors)
	}
	if c.Epic != "Cozystack" {
		t.Fatalf("the home column stays: %+v", c)
	}
	// And the column the mirror pointed at is deletable again: nothing
	// invisible stands in it.
	if err := svc.DeleteEpic(ctx, "acme", "Launch", "freedom"); err != nil {
		t.Fatalf("the emptied column must be deletable: %v", err)
	}
}
