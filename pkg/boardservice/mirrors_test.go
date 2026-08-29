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
	// The deleted card's plan was never cleared first: a write into the
	// very commit that removes the file is a dead write.
	if f.count("SetPlan i") != 0 || f.count("SetWeek i") != 0 {
		t.Fatalf("no dead writes into a file about to be removed: %v", f.log)
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

// A column is named by its EPIC alone. A card standing in no column has an
// empty home, which once "matched" ("", "") and fell through to the
// last-column branch — the call that asked to remove a card from nowhere
// deleted it outright. Requiring the epic half closes that hole; requiring
// the project half too broke the × of every no-project column, which is a
// real column with a real ×. The MCP tool feeds this service without
// validating, and an agent calling remove_from_project with empty halves
// on a column-less card is the most expectable mistake there is.
func TestRemoveFromProjectRefusesTheEmptyPair(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "no column", Team: "platform"},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "", ""); !errors.Is(err, ErrNotInProject) {
		t.Fatalf("the empty pair must be refused: %v", err)
	}
	// ("", epic) is a lawful pair — the no-project bucket — but this card
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
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-inbox", Title: board.EpicStateTitle, Epic: "Inbox"},
		{ItemID: "cold", Title: "untouched", Team: "platform", Epic: "Inbox"},
		{ItemID: "warm", Title: "worked", Team: "platform", Epic: "Inbox",
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
	if !ok || c.Epic != "" {
		t.Fatalf("a worked card survives as a working-area orphan: %+v", c)
	}
}

// A no-project column names no repository, so it cannot be a mirror HOME:
// there is nothing to compare a target against, and the picker offers such
// a card no mirrors at all (placements.test.ts pins the UI half).
func TestACardInANoProjectColumnCannotBeMirrored(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-inbox", Title: board.EpicStateTitle, Epic: "Inbox"},
		{ItemID: "c1", Title: "unbound", Team: "platform", Epic: "Inbox"},
	})
	if err := New(f).Mirror(ctx, "acme", "c1", "freedom", "Launch"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("no repository to mirror within: %v", err)
	}
}

// The tie is a reference by name, and issue #124's rule follows it: a
// renamed process carries its ties along, with the same trace in the
// card's log the tie itself leaves.
func TestRenameProcessRewritesTheTies(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "chore", Team: "platform", Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	svc := New(f)
	if err := svc.RenameProcess(ctx, "acme", "Invoicing", "Billing"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Process != "Billing" {
		t.Fatalf("the tie follows the rename: %+v", c)
	}
	found := false
	for _, e := range f.eventsOf("c1") {
		if e.Kind == board.EventProcess && e.From == "Invoicing" && e.To == "Billing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the rewrite must be logged: %+v", f.eventsOf("c1"))
	}
}

// A process with standing ties will not delete: the ties would dangle on a
// name that no longer exists — the same protection tasks already had.
func TestDeleteProcessRefusesWhileCardsAreTied(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "chore", Team: "platform", Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	svc := New(f)
	if err := svc.DeleteProcess(ctx, "acme", "Invoicing"); !errors.Is(err, ErrProcessInUse) {
		t.Fatalf("ties stand: %v", err)
	}
	if err := svc.SetCardProcess(ctx, "acme", "c1", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteProcess(ctx, "acme", "Invoicing"); err != nil {
		t.Fatalf("untied, the process deletes: %v", err)
	}
}

// Moving a process to a project of another repository re-files its stub
// there — and every standing tie would turn cross-repository in one
// stroke, the very state the tie guard exists to prevent. Refused while
// ties stand; a move within the repository, and any move once untied, is
// free.
func TestMovingAProcessToAnotherRepositoryIsRefusedWhileCardsAreTied(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "chore", Team: "platform", Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	svc := New(f)
	if err := svc.SetProcessProject(ctx, "acme", "Invoicing", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the move would strand every tie: %v", err)
	}
	if err := svc.SetProcessProject(ctx, "acme", "Invoicing", "engineering"); err != nil {
		t.Fatalf("a move within the repository leaves the ties valid: %v", err)
	}
	if err := svc.SetCardProcess(ctx, "acme", "c1", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProcessProject(ctx, "acme", "Invoicing", "strategy"); err != nil {
		t.Fatalf("no ties, no objection: %v", err)
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
	// A project rename is roster metadata: the home side writes no per-card
	// line, and the mirror side stays symmetric — no EventMirror either.
	for _, e := range f.eventsOf("c1") {
		if e.Kind == board.EventMirror {
			t.Fatalf("a project rename must not log per card: %+v", e)
		}
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

// The slot rule applies only to a card with NO dates of its own: an attach
// never rewrites a schedule someone chose — the card keeps its dates, and
// its row is wherever those dates put it.
func TestAttachingADatedPlanCardKeepsItsDates(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "scheduled", Team: "platform", Plan: board.PlanFri, Week: "2026-08-24",
			StartDate: "2026-09-07", Day: "2026-09-09"},
	})
	svc := New(f)
	project := "engineering"
	if err := svc.SetEpic(ctx, "acme", "c1", "Cozystack", &project); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.StartDate != "2026-09-07" || c.Day != "2026-09-09" {
		t.Fatalf("the chosen schedule survives the attach: %+v", c)
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

// The tie is a stored reference, and references never cross a domain
// boundary: a card of the closed repository naming a process declared in
// the shared one would hand the closed card's existence to readers who may
// not have it — the same rule every other reference on the board obeys.
func TestTyingACardToAProcessOfAnotherRepositoryIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "closed chore", Stage: board.StageRecurrent, Domain: "founders"},
	})
	if err := New(f).SetCardProcess(ctx, "acme", "c1", "Invoicing"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("Invoicing lives in the primary repository, the card does not: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Process != "" {
		t.Fatalf("the refused tie must not land: %+v", c)
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

// The review link decides the card's repository before its project (linked
// cards first, G14), so SetReviewOf is a re-file in disguise: pointing a
// mirrored review card at an original in another repository would carry
// the file away and leave the mirrors naming columns of the repository it
// left — for whose readers the column looks empty while DeleteEpic refuses
// "occupied". Refused, symmetric with SetEpic; a same-repository original
// moves nothing and links freely; and clearing a link that holds the card
// elsewhere is the same move in reverse, refused the same way.
func TestTheReviewLinkCannotCarryAMirroredCardAway(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "review", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
		{ItemID: "orig", Title: "original", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
		{ItemID: "near", Title: "neighbour", Team: "platform", Project: "freedom", Epic: "Launch"},
	})
	svc := New(f)
	if err := svc.SetReviewOf(ctx, "acme", "c1", "orig"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the link would re-file the card into another repository: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.ReviewOf != "" {
		t.Fatalf("the refused link must not land: %+v", c)
	}
	if err := svc.SetReviewOf(ctx, "acme", "c1", "near"); err != nil {
		t.Fatalf("a same-repository original links freely: %v", err)
	}
	if err := svc.SetReviewOf(ctx, "acme", "c1", ""); err != nil {
		t.Fatalf("clearing a same-repository link moves nothing: %v", err)
	}
}

// The other door of the same room: a review card whose link already points
// into another repository LIVES there (its file follows the original), so
// no column of its home project's repository may show it.
func TestACardLinkedIntoAnotherRepositoryCannotBeMirrored(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "orig", Title: "original", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
		{ItemID: "rev", Title: "review", Team: "platform", Project: "engineering", Epic: "Cozystack",
			ReviewOf: "orig", Domain: "founders"},
	})
	if err := New(f).Mirror(ctx, "acme", "rev", "freedom", "Launch"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the card's file lives where its original does: %v", err)
	}
}

// A process turn belongs to its task, and the task names the process:
// writing process: on the turn itself would contradict the task and lose
// to it on every read (processOf resolves the task first) — the silent
// no-op the picker used to show as a flicker-then-revert. Refused.
func TestATurnsProcessCannotBeReTied(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "turn", Title: "invoice week 35", Team: "platform",
			Stage: board.StageRecurrent, Task: "task-1"},
	})
	if err := New(f).SetCardProcess(ctx, "acme", "turn", "Invoicing"); !errors.Is(err, ErrTurnProcess) {
		t.Fatalf("a turn's process is its task's: %v", err)
	}
}

// A rewritten mirror entry leaves the same trace a home rename does: the
// activity log is the second documentation, and a mirror that silently
// changed columns read as one appearing from nowhere.
func TestARenamedMirrorLandsInTheCardsLog(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "shared", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	svc := New(f)
	if err := svc.RenameEpic(ctx, "acme", "freedom", "Launch", "Liftoff"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range f.eventsOf("c1") {
		if e.Kind == board.EventMirror && e.From == "freedom / Launch" && e.To == "freedom / Liftoff" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the rewrite must be logged: %+v", f.eventsOf("c1"))
	}
}

// The UI offers the process picker to recurrent cards alone, and the
// service holds the same line for the callers that skip the UI: tying a
// non-recurrent card is refused — a tie the recurring shelf never draws
// would be invisible state — while CLEARING stays free whatever the stage
// became since.
func TestOnlyARecurrentCardTakesAProcessTie(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "plain", Title: "one-off", Team: "platform"},
		{ItemID: "was", Title: "used to recur", Team: "platform", Process: "Invoicing"},
	})
	svc := New(f)
	if err := svc.SetCardProcess(ctx, "acme", "plain", "Invoicing"); !errors.Is(err, ErrNotRecurrent) {
		t.Fatalf("a one-off card has no recurring shelf to show the tie: %v", err)
	}
	if err := svc.SetCardProcess(ctx, "acme", "was", ""); err != nil {
		t.Fatalf("clearing is free whatever the stage is now: %v", err)
	}
}

// The tie never crosses a domain boundary — and neither may the CARD slip
// out from under it. Four doors, each a re-file in disguise, each refused
// (or, for grouping, cleared) before anything is written:
//
//   - SetTeam: a recurrent card without a project follows its team, so
//     re-teaming it into another repository would strand the tie;
//   - SetEpic: attaching a teamless tied card to a project of another
//     repository moves the file the same way;
//   - SetEpicProject: a column move carries its home cards along;
//   - SetReviewOf: a link outranks everything (linked cards first);
//   - SetParent clears the tie instead, the way it clears mirrors — a
//     subtask rides its parent, and the shelf never draws its tie.
func TestReTeamingATiedCardToAnotherRepositoryIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "chore", Team: "platform", Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	f.b.SprintStates["board"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-b"}
	f.b.Domains = map[string]string{"st-b": "founders"}
	svc := New(f)
	if err := svc.SetTeam(ctx, "acme", "c1", "board", ""); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the team decides where a project-less card lives: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Team != "platform" || c.Process != "Invoicing" {
		t.Fatalf("the refused move must leave the card whole: %+v", c)
	}
}

func TestAttachingATiedCardToAProjectOfAnotherRepositoryIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "chore", Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	strategy := "strategy"
	if err := New(f).SetEpic(ctx, "acme", "c1", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the attach is a repository move in disguise: %v", err)
	}
}

func TestMovingAColumnToAnotherRepositoryIsRefusedWhileCardsAreTied(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "chore", Stage: board.StageRecurrent, Process: "Invoicing",
			Project: "freedom", Epic: "Launch"},
	})
	if err := New(f).SetEpicProject(ctx, "acme", "freedom", "Launch", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the column would carry the tied card away: %v", err)
	}
}

func TestLinkingATiedCardIntoAnotherRepositoryIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "review chore", Team: "platform", Stage: board.StageRecurrent, Process: "Invoicing"},
		{ItemID: "orig", Title: "original", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
	})
	if err := New(f).SetReviewOf(ctx, "acme", "c1", "orig"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the link outranks everything and would move the file: %v", err)
	}
}

func TestGroupingATiedCardClearsTheTie(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "chore", Team: "platform", Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "c1", "p"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Process != "" {
		t.Fatalf("a subtask rides its parent; the tie goes with the grouping: %+v", c)
	}
	found := false
	for _, e := range f.eventsOf("c1") {
		if e.Kind == board.EventProcess && e.From == "Invoicing" && e.To == "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the untying must be logged: %+v", f.eventsOf("c1"))
	}
}

// The x's last column is one more door out of the repository: orphaning
// clears the pair, and for a TEAMLESS card the domain falls through to
// the primary — a standing tie would be stranded. Refused before anything
// is written; a card whose orphan life stays in the same repository keeps
// its tie and goes free.
func TestRemoveFromLastColumnCannotStrandTheTie(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "proc-f", Title: board.ProcessStateTitle, Process: "Fundraising ops", Domain: "founders"},
		{ItemID: "c1", Title: "closed chore", Stage: board.StageRecurrent,
			Project: "strategy", Epic: "Fundraising", Domain: "founders",
			Process: "Fundraising ops", Assignees: []string{"kvaps"}, Progress: 40,
			Plan: board.PlanFri, Week: "2026-08-24"},
		{ItemID: "c2", Title: "open chore", Stage: board.StageRecurrent,
			Project: "engineering", Epic: "Cozystack",
			Process: "Invoicing", Assignees: []string{"kvaps"}, Progress: 40},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "strategy", "Fundraising"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("orphaning a teamless closed card is a move to the primary: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Epic != "Fundraising" || c.Plan != board.PlanFri {
		t.Fatalf("the refusal must fire before anything is written: %+v", c)
	}
	// Same repository: the orphan keeps its tie and goes free.
	if err := svc.RemoveFromProject(ctx, "acme", "c2", "engineering", "Cozystack"); err != nil {
		t.Fatal(err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	c, _ = findCard(b, "c2")
	if c.Epic != "" || c.Process != "Invoicing" {
		t.Fatalf("a same-repository orphan keeps its tie: %+v", c)
	}
}

// A DELETION moves nothing, so the tie guard has no say in it: an
// untouched tied card of a closed repository is deleted in place by its
// last column's x, the same way delete_card would take it — the guard
// bites only the orphaning, which really is a re-file.
func TestRemoveFromLastColumnDeletesAnUntouchedTiedCard(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "proc-f", Title: board.ProcessStateTitle, Process: "Fundraising ops", Domain: "founders"},
		{ItemID: "c1", Title: "untouched closed chore", Stage: board.StageRecurrent,
			Project: "strategy", Epic: "Fundraising", Domain: "founders",
			Process: "Fundraising ops"},
	})
	svc := New(f)
	if err := svc.RemoveFromProject(ctx, "acme", "c1", "strategy", "Fundraising"); err != nil {
		t.Fatalf("deletion is not a move — the tie guard must stay quiet: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("the untouched card is deleted in place")
	}
}

// A subtask rides its parent, and grouping clears its tie — a re-tie one
// request later (PATCH {parent, process} carries both) would put it right
// back under tiedMoveGuard's radar: any re-file of the PARENT drags the
// child across repositories unguarded. Refused outright, like the mirror.
func TestASubtaskCannotBeTied(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "child", Team: "platform", Stage: board.StageRecurrent, Parent: "p"},
	})
	if err := New(f).SetCardProcess(ctx, "acme", "c1", "Invoicing"); !errors.Is(err, ErrSubtaskTie) {
		t.Fatalf("a subtask rides its parent: %v", err)
	}
}
