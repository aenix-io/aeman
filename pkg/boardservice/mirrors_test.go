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

// A no-project column is a home like any other: it names a repository —
// its own, the one its stub was declared in — so a card standing in it
// mirrors freely INSIDE that repository and not out of it. The refusal
// this test used to pin ("a no-project home cannot mirror at all") was the
// PROJECT's answer to a question that belongs to the column.
func TestACardInANoProjectColumnMirrorsWithinItsRepository(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-inbox", Title: board.EpicStateTitle, Epic: "Inbox"},
		{ItemID: "c1", Title: "unbound", Team: "platform", Epic: "Inbox"},
	})
	svc := New(f)
	if err := svc.Mirror(ctx, "acme", "c1", "freedom", "Launch"); err != nil {
		t.Fatalf("both columns are of the primary repository: %v", err)
	}
	if err := svc.Mirror(ctx, "acme", "c1", "strategy", "Fundraising"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the closed repository's column still cannot show it: %v", err)
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

// Dropping a process's project keeps it where it is when it already lives
// in the primary — the stub does not move, so no tie is stranded and there
// is nothing to refuse. The board NAMES its primary, the way a real store
// hands it over, and reading "no project" as a repository of its own made
// the unbinding look like a move out of it.
func TestDroppingTheProjectOfAProcessInThePrimaryKeepsItsTies(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "chore", Team: "platform", Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	f.b.Primary = "aeman-db"
	svc := New(f)
	if err := svc.SetProcessProject(ctx, "acme", "Invoicing", "engineering"); err != nil {
		t.Fatalf("a move within the repository is free: %v", err)
	}
	if err := svc.SetProcessProject(ctx, "acme", "Invoicing", ""); err != nil {
		t.Fatalf("and so is dropping the project, which moves the stub nowhere: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Process != "Invoicing" {
		t.Fatalf("the tie stands: %+v", c)
	}
}

// A review link inside ONE repository moves nothing, so a mirrored card
// keeps its mirrors through it — the refusal is for a link that would
// carry the card out, and this rule had no test on either side. The board
// NAMES its primary here, the way a real store hands it over: the two
// sides of the question are read through HomeDomain, in one namespace, so
// that an unstamped entry beside a stamped one cannot make a link that
// moves nothing look like a move.
func TestLinkingAMirroredCardAsAReviewInsideOneRepositoryIsAllowed(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "orig", Title: "the work", Team: "platform"},
		{ItemID: "rev", Title: "review", Team: "platform", Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	f.b.Primary = "aeman-db"
	if err := New(f).SetReviewOf(ctx, "acme", "rev", "orig"); err != nil {
		t.Fatalf("both cards live in the primary; nothing is stranded: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "rev")
	if c.ReviewOf != "orig" {
		t.Fatalf("the link is written: %+v", c)
	}
	if len(c.Mirrors) != 1 {
		t.Fatalf("and the mirrors stand: %+v", c.Mirrors)
	}
}

// What rides a follower is not only its column. A review card tied to a
// process of its original's repository is dragged along by the re-file
// (the cascade follows reviewOf), and its tie — a reference by name that
// never crosses a repository — would be left naming a process the new one
// does not declare. The direct door refuses such a tie; the door that
// moves the card underneath it must refuse too.
func TestReFilingACardCannotStrandAFollowersProcessTie(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "the work", Team: "platform"},
		{ItemID: "rev", Title: "review of the work", ReviewOf: "c1", Team: "platform",
			Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders"}
	err := New(f).SetTeam(ctx, "acme", "c1", "founders", "")
	if !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the review card follows the original and its tie cannot: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Team != "platform" {
		t.Fatalf("a refused re-file writes nothing: %+v", c)
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

// A subtask carries at most ONE column of its own (G57), which the Project
// board draws and counts like any other slot. A second placement it may
// not have: its file rides its parent, so every mirror would be stranded
// the moment the parent changes repository — naming a column of a
// repository that no longer holds the card. Mirroring a subtask is refused, and grouping a
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
// back under refileGuard's radar: any re-file of the PARENT drags the
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

// A SUBTASK has another home: its parent. The Project board draws it in
// the column it carries (G14 blesses that), so the × must be able to take
// it out of the column — but never to delete it, however untouched it is.
// Deleting here would destroy a card that is still riding its parent on
// every other board, which is the two-homes rule the × exists to honour.
func TestRemoveFromColumnNeverDeletesASubtask(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "untouched child", Team: "platform", Parent: "p",
			Project: "engineering", Epic: "Cozystack"},
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
	if c.Epic != "" || c.Project != "" {
		t.Fatalf("the column is gone: %+v", c)
	}
	if c.Parent != "p" {
		t.Fatalf("the parent is untouched: %+v", c)
	}
}

// A card's column names a repository, and that repository must be the one
// that HOLDS the card's file. For an ordinary card the two agree by
// construction — the project decides the domain — but when a LINK outranks
// the project (a subtask riding its parent, a review card following its
// original, G14) the file stays put while the column can be dragged
// anywhere. The result is the state ErrSubtaskMirror was written against:
// a column of one repository counting a card whose file another repository
// holds, so DeleteEpic refuses for a card nobody there can see.
func TestAColumnCannotNameARepositoryThatDoesNotHoldTheCard(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Project: "engineering", Epic: "Cozystack"},
		{ItemID: "c1", Title: "child", Parent: "p"},
		{ItemID: "rev", Title: "review", ReviewOf: "p"},
		// Teamless on purpose: a team of the primary repository would refuse
		// the move first (G46), before this rule gets a say.
		{ItemID: "plain", Title: "ordinary", Project: "engineering", Epic: "Cozystack"},
	})
	svc := New(f)
	strategy := "strategy"
	if err := svc.SetEpic(ctx, "acme", "c1", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the subtask's file rides its parent, in the primary repository: %v", err)
	}
	if err := svc.SetEpic(ctx, "acme", "rev", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the review card's file follows its original: %v", err)
	}
	// The same repository is free — a subtask may carry its own column (G14).
	freedom := "freedom"
	if err := svc.SetEpic(ctx, "acme", "c1", "Launch", &freedom); err != nil {
		t.Fatalf("a column of its own repository is a subtask's right: %v", err)
	}
	// And an ordinary teamless card still moves BETWEEN repositories freely:
	// there the project is what decides, so the file follows the column.
	if err := svc.SetEpic(ctx, "acme", "plain", "Fundraising", &strategy); err != nil {
		t.Fatalf("an ordinary card's file follows its project: %v", err)
	}
}

// The column-holds-the-file rule has a second door: GROUPING. A subtask's
// file follows its parent, so grouping a columned card under a parent of
// another repository would leave its column naming a repository that no
// longer holds it — the state SetEpic now refuses, reached by a drag
// instead of a menu. The Project board draws such a card and counts it,
// so the stale column is load-bearing, not merely untidy.
func TestGroupingUnderAParentInAnotherRepositoryIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
		{ItemID: "near", Title: "parent here", Project: "engineering", Epic: "Cozystack"},
		{ItemID: "c1", Title: "columned child", Project: "engineering", Epic: "Cozystack"},
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
	if c.Epic != "Cozystack" {
		t.Fatalf("a subtask keeps the one column it carries: %+v", c)
	}
}

// And the mirror image: moving the PARENT drags every child's file with it
// (MultiBackend cascades the re-file), while each child's own column stays
// behind. The guard has to look at the children, not only at the card the
// caller named.
func TestReFilingAParentCannotStrandItsSubtasksColumn(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Project: "engineering", Epic: "Cozystack"},
		{ItemID: "c1", Title: "child", Parent: "p", Project: "engineering", Epic: "Cozystack"},
	})
	svc := New(f)
	strategy := "strategy"
	if err := svc.SetEpic(ctx, "acme", "p", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the child's column would be stranded: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "p")
	if c.Project != "engineering" {
		t.Fatalf("the refused move must not land: %+v", c)
	}
	// A move within the repository leaves every child's column valid.
	freedom := "freedom"
	if err := svc.SetEpic(ctx, "acme", "p", "Launch", &freedom); err != nil {
		t.Fatalf("a move inside the repository is free: %v", err)
	}
}

// A column's repository is the column's OWN — the domain of the epic stub
// that declares it — not its project's. The two agree wherever a project
// owns the column, but the NO-PROJECT bucket is a real column with no
// project to ask: reading the project made every card in it ungroupable,
// because ProjectStates never holds "" and the lookup always missed.
func TestGroupingACardFromANoProjectColumnIsFree(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-loose", Title: board.EpicStateTitle, Epic: "Loose"},
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "loose column child", Team: "platform", Epic: "Loose"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "c1", "p"); err != nil {
		t.Fatalf("a no-project column of this repository strands nothing: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Epic != "Loose" || c.Parent != "p" {
		t.Fatalf("the column survives the grouping (G14): %+v", c)
	}
}

// The same question through the other door: attaching a card to a column
// of ANOTHER repository is refused whether or not that column has a
// project — the two guards must answer alike, or one of them is a way in.
func TestAttachingToAForeignNoProjectColumnIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-far", Title: board.EpicStateTitle, Epic: "Far", Domain: "founders"},
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "child", Team: "platform", Parent: "p"},
	})
	none := ""
	if err := New(f).SetEpic(ctx, "acme", "c1", "Far", &none); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a column of the closed repository cannot hold a card of the primary: %v", err)
	}
}

// Every refusal fires BEFORE anything is written. The grouping's weekly-plan
// handover ran first, so a refused grouping still took the child's slot
// away and gave it to the parent — in the cache only, since the commit
// never happened: the board then showed a state git had never held.
func TestARefusedGroupingLeavesThePlanSlotAlone(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
		{ItemID: "c1", Title: "planned child", Project: "engineering", Epic: "Cozystack",
			Plan: board.PlanWed, Week: "2026-08-24"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "c1", "far"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the grouping is refused: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Plan != board.PlanWed || c.Week != "2026-08-24" {
		t.Fatalf("the refused grouping must not have taken the slot: %+v", c)
	}
	parent, _ := findCard(b, "far")
	if parent.Plan != board.PlanNone || parent.Week != "" {
		t.Fatalf("nor handed it to the parent: %+v", parent)
	}
}

// The file cascade follows reviewOf as well as parent (MultiBackend
// cascade), so a columned REVIEW CARD is stranded by moving its original
// exactly as a subtask is stranded by moving its parent.
func TestReFilingAnOriginalCannotStrandItsReviewCardsColumn(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "orig", Title: "original", Project: "engineering", Epic: "Cozystack"},
		{ItemID: "rev", Title: "review", ReviewOf: "orig", Project: "engineering", Epic: "Cozystack"},
	})
	svc := New(f)
	strategy := "strategy"
	if err := svc.SetEpic(ctx, "acme", "orig", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
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
	f := mirrorBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
		{ItemID: "orig", Title: "original", Team: "platform"},
		{ItemID: "rev", Title: "review", ReviewOf: "orig", Project: "engineering", Epic: "Cozystack"},
	})
	if err := New(f).SetParent(ctx, "acme", "orig", "far"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the review card follows the original and would be stranded: %v", err)
	}
}

// Pulling a subtask back out is a re-file too: the child leaves its
// parent's repository for whatever its own project or team names.
func TestUngroupingCannotStrandAFollowersColumn(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
		{ItemID: "kid", Title: "child", Team: "platform", Parent: "far"},
		{ItemID: "rev", Title: "review of the child", ReviewOf: "kid",
			Project: "strategy", Epic: "Fundraising", Domain: "founders"},
	})
	if err := New(f).SetParent(ctx, "acme", "kid", ""); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the pull-out takes the review card's file out of its column's repository: %v", err)
	}
}

// A card with no project follows its TEAM, and the no-project bucket is a
// real column — re-teaming across repositories leaves that column behind.
func TestReTeamingCannotStrandTheCardsOwnColumn(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-loose", Title: board.EpicStateTitle, Epic: "Loose"},
		{ItemID: "c1", Title: "loose column card", Team: "platform", Epic: "Loose"},
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
	f := mirrorBoard([]board.Card{
		{ItemID: "orig", Title: "original", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
		{ItemID: "c1", Title: "review", Project: "engineering", Epic: "Cozystack"},
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
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "grandparent", Project: "engineering", Epic: "Cozystack"},
		{ItemID: "kid", Title: "child", Parent: "p"},
		{ItemID: "rev", Title: "review of the child", ReviewOf: "kid",
			Project: "engineering", Epic: "Cozystack"},
	})
	strategy := "strategy"
	if err := New(f).SetEpic(ctx, "acme", "p", "Fundraising", &strategy); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the review card two hops down would be stranded: %v", err)
	}
}

// Moving a COLUMN is the seventh door: the stub is re-filed into the
// target project's repository while a card whose file is held by a link
// stays where it is — and its project field is rewritten to name the
// column that just left.
func TestMovingAColumnCannotStrandALinkedCardStandingInIt(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent"},
		{ItemID: "c1", Title: "subtask", Parent: "p", Project: "engineering", Epic: "Cozystack"},
	})
	svc := New(f)
	if err := svc.SetEpicProject(ctx, "acme", "engineering", "Cozystack", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the subtask's file rides its parent in the primary repository: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Project != "engineering" {
		t.Fatalf("the refused move must not have rewritten the card: %+v", c)
	}
	// A move within the repository carries everyone along as before.
	if err := svc.SetEpicProject(ctx, "acme", "engineering", "Cozystack", "freedom"); err != nil {
		t.Fatalf("a move inside the repository is free: %v", err)
	}
}

// A project NAME can be declared in two repositories, and then its columns
// are merged under one entry (G13) while each column keeps the repository
// it was declared in. The question "may this card stand in this column" is
// therefore the COLUMN's to answer, never the project's — reading the
// project's offered a column the store could not hold the card in.
func TestAColumnOfAnAliasProjectAnswersForItself(t *testing.T) {
	f := mirrorBoard([]board.Card{
		// "engineering" is declared in the primary; the same NAME is
		// declared again in founders, carrying its own column.
		{ItemID: "pr-e2", Title: board.ProjectStateTitle, Project: "engineering", Domain: "founders"},
		{ItemID: "ep-closed", Title: board.EpicStateTitle, Epic: "Closed work",
			Project: "engineering", Domain: "founders"},
		{ItemID: "c1", Title: "primary card", Project: "engineering", Epic: "Cozystack"},
	})
	svc := New(f)
	if err := svc.Mirror(ctx, "acme", "c1", "engineering", "Closed work"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the column lives in founders, the card does not: %v", err)
	}
	none := "engineering"
	if err := svc.SetEpic(ctx, "acme", "c1", "Closed work", &none); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("and attaching to it is the same question: %v", err)
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
	f := mirrorBoard([]board.Card{
		// Teamless: a team of the primary would refuse the move first (G46),
		// and this is exactly the card that would be stranded.
		{ItemID: "plain", Title: "plain card", Project: "engineering", Epic: "Cozystack"},
	})
	svc := New(f)
	if err := svc.SetEpicProject(ctx, "acme", "engineering", "Cozystack", "strategy"); !errors.Is(err, ErrCrossDomain) {
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
	if err := svc.SetEpicProject(ctx, "acme", "freedom", "Launch", "strategy"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("an empty column cannot move either: %v", err)
	}
}

// UNBINDING a column of a non-primary repository keeps it exactly where it
// is — the stub does not move — so the cards in it stay held by that same
// repository through their team. Comparing against the target PROJECT's
// repository (the primary, for the no-project bucket) refused this.
func TestUnbindingANonPrimaryColumnKeepsItsCards(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "closed card", Team: "founders",
			Project: "strategy", Epic: "Fundraising", Domain: "founders"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders"}
	if err := New(f).SetEpicProject(ctx, "acme", "strategy", "Fundraising", ""); err != nil {
		t.Fatalf("the column stays in its repository and so do its cards: %v", err)
	}
}

// The grid × obeys the same two-homes rule the Project board's does: a
// card standing in a COLUMN is never deleted by it — and a subtask that
// carries one is now a drawn, counted slot in someone else's planner, so
// deleting it there destroys work that is visibly planned elsewhere.
func TestTheGridRemoveKeepsAColumnedSubtask(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "columned child", Team: "platform", Parent: "p",
			Project: "engineering", Epic: "Cozystack",
			StartDate: board.TodayIso(), Day: board.TodayIso(), SprintStart: board.TodayIso()},
	})
	if err := New(f).Remove(ctx, "acme", "c1", "grid"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("a card filed under a column is never deleted by either ×")
	}
	if c.Epic != "Cozystack" {
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
	// Off the day grid: an epic card out of the sprint is skipped there
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
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "plain child", Team: "platform", Parent: "p",
			SprintStart: board.TodayIso()},
	})
	if err := New(f).Remove(ctx, "acme", "c1", "grid"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("nowhere else to be: the × deletes it")
	}
}

// Grouping CLEARS the tie (clearRiders) — it does not refuse over it, as
// its own docstring and G56 say. The guard was reading the state before
// the change, so a parent in another repository tripped the tie check that
// grouping was about to make moot.
func TestGroupingATiedCardAcrossRepositoriesStillClearsTheTie(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
		{ItemID: "c1", Title: "tied chore", Stage: board.StageRecurrent, Process: "Invoicing"},
	})
	svc := New(f)
	if err := svc.SetParent(ctx, "acme", "c1", "far"); err != nil {
		t.Fatalf("grouping clears the tie, it does not refuse: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "c1")
	if c.Process != "" || c.Parent != "far" {
		t.Fatalf("the tie is cleared and the grouping lands: %+v", c)
	}
}

// A subtask standing in a column is a first-class state now (G57), so the
// create door must produce it rather than silently drop the parent: the
// caller asked for a child and got a top-level card, which on the Project
// board is a slot in the wrong place with no way to tell.
func TestCreatingACardWithBothAParentAndAColumnKeepsBoth(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
	})
	c, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child in a column", Team: "platform", Parent: "p",
		Project: "engineering", Epic: "Cozystack",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	got, _ := findCard(b, c.ItemID)
	if got.Parent != "p" {
		t.Fatalf("the parent was asked for and must land: %+v", got)
	}
	if got.Epic != "Cozystack" {
		t.Fatalf("and the column with it: %+v", got)
	}
}

// A column move INSIDE one repository strands nothing, mirrors or not.
// The guard asked ColumnDomain(to, epic) — a column that cannot exist yet,
// since epicNameFree has just proved the name free there — so it answered
// "no such repository" every time and refused every move. The question is
// the domain the column will HAVE once it lands: its new project's.
func TestMovingAColumnInsideOneRepositoryIsFreeWhileCardsMirrorIt(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "mirrored into the column", Team: "platform",
			Project: "engineering", Epic: "Cozystack",
			Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	// c1's HOME is the column being moved, and it mirrors elsewhere in the
	// same repository: the move takes it along, and both placements stay
	// inside the primary.
	if err := New(f).SetEpicProject(ctx, "acme", "engineering", "Cozystack", "freedom"); err != nil {
		t.Fatalf("nothing crosses a boundary here: %v", err)
	}
}

func TestMovingAColumnInsideOneRepositoryIsFreeWhenACardMirrorsIntoIt(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "mirrors this column", Team: "platform",
			Project: "freedom", Epic: "Launch",
			Mirrors: []board.Placement{{Project: "engineering", Epic: "Cozystack"}}},
	})
	if err := New(f).SetEpicProject(ctx, "acme", "engineering", "Cozystack", "freedom"); err != nil {
		t.Fatalf("the mirror and its home stay in one repository: %v", err)
	}
}

// The create door validates the parent BEFORE writing anything: a card
// born and then deleted because its parent was never findable is a stray
// ADDED broadcast and a created event for a card that should not exist.
func TestCreatingUnderAnImpossibleParentCreatesNothing(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "far", Title: "parent elsewhere", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
	})
	svc := New(f)
	before, _ := f.LoadBoard(ctx, "acme")
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "ghost",
		Project: "engineering", Epic: "Cozystack",
	}); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("a parent that does not exist: %v", err)
	}
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "far",
		Project: "engineering", Epic: "Cozystack",
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

// `from` picks WHICH home the × empties — the whole two-homes contract of
// this endpoint. The subtask branch sat above the dispatch, so a caller
// asking for the plan (REST, MCP) got the grid's gesture instead: the card
// ungrouped and released, in answer to a request about a band it does not
// even have.
func TestRemoveFromThePlanIsNotTheGridsGestureForASubtask(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "columned child", Team: "platform", Parent: "p",
			Project: "engineering", Epic: "Cozystack",
			StartDate: board.TodayIso(), Day: board.TodayIso(), SprintStart: board.TodayIso()},
	})
	if err := New(f).Remove(ctx, "acme", "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("the plan × has no business deleting a card that stands in a column")
	}
	// A subtask carries no plan band (grouping clears it), so there is
	// nothing to empty: the card is left exactly as it was.
	if c.Parent != "p" || c.SprintStart == "" {
		t.Fatalf("the grid's gesture must not run in the plan's name: %+v", c)
	}
	// And nothing was written: an empty band cleared again is a write in
	// the request's commit for a change nobody made.
	if n := f.count("SetPlan"); n != 0 {
		t.Fatalf("nothing to empty, nothing to write: %d plan writes", n)
	}
}

// When the grouping fails the create is undone — and if the undo fails
// too, BOTH reasons travel. One of the two create paths used to swallow
// the second and hand back the card it had just tried to delete.
func TestAFailedGroupingUndoesTheCreateAndReportsWhatItCannot(t *testing.T) {
	f := mirrorBoard([]board.Card{
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
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform", Assignees: []string{"kvaps"}},
		{ItemID: "kid", Title: "columned child", Team: "platform", Parent: "p",
			Project: "engineering", Epic: "Cozystack"},
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
	cd, known := board.ColumnDomain(b, c.Project, c.Epic)
	if !known {
		t.Fatalf("the column it kept must still be declared: %+v", c)
	}
	if mine := board.DomainOf(c, board.Resolver(b, "")); cd != mine {
		t.Fatalf("and must name the repository that holds the card: column %q, card %q", cd, mine)
	}
}

// The review link the same way, on the epic path.
func TestCreatingAColumnedReviewCardKeepsTheLink(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "orig", Title: "original", Team: "platform"},
	})
	c, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "review", Team: "platform", ReviewOf: "orig",
		Project: "engineering", Epic: "Cozystack",
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
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-loose", Title: board.EpicStateTitle, Epic: "Launch"},
	})
	if err := New(f).SetEpicProject(ctx, "acme", "freedom", "Launch", ""); err == nil {
		t.Fatal("the no-project bucket already holds a Launch column")
	}
}

// The create door's guard ran only when a PARENT was named, and a review
// link outranks the project just as a parent does (G14): a card created
// with reviewOf in one repository and a column in another was born into
// the state every other door refuses — file here, column there.
func TestCreatingAColumnedReviewCardAcrossRepositoriesIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "orig", Title: "original", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
	})
	svc := New(f)
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "review", ReviewOf: "orig",
		Project: "engineering", Epic: "Cozystack",
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
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "orig", Title: "original", Project: "strategy", Epic: "Fundraising", Domain: "founders"},
	})
	svc := New(f)
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "review of an original elsewhere", Team: "platform",
		Parent: "p", ReviewOf: "orig",
		Project: "engineering", Epic: "Cozystack",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the link decides where this card lives: %v", err)
	}
	if n := f.count("CreateCard"); n != 0 {
		t.Fatalf("nothing may be written before the refusal: %d creates", n)
	}
}

// The plan's × empties the weekly plan. A card with no band there has
// nothing to empty — and a SUBTASK never has one, since grouping clears
// it — so the gesture writes nothing at all rather than clearing an empty
// field twice into the request's commit.
func TestRemoveFromThePlanWritesNothingWhenThereIsNoBand(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "plain child", Team: "platform", Parent: "p",
			SprintStart: board.TodayIso()},
	})
	if err := New(f).Remove(ctx, "acme", "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "c1"); !ok {
		t.Fatal("the plan × does not delete a card that was never in the plan")
	}
	if n := f.count("SetPlan") + f.count("SetWeek"); n != 0 {
		t.Fatalf("nothing to empty, nothing to write: %d writes (%v)", n, f.log)
	}
}

// Create is a door like any other, and a COLUMN is what G57 is about: the
// guard skipped every request that named no link, so the one gesture that
// could still write "file here, column there" was the one that makes the
// card. Both of the shapes the rest of the branch refuses elsewhere.
func TestCreatingIntoAColumnOfAnotherRepositoryIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-loose", Title: board.EpicStateTitle, Epic: "Loose"},
		{ItemID: "ep-far", Title: board.EpicStateTitle, Epic: "Far", Project: "engineering", Domain: "founders"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-far": "founders"}
	svc := New(f)

	// The team holds this card in founders; the column is of the primary.
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "closed card in a primary column", Team: "founders", Epic: "Loose",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a no-project column of the primary cannot hold it: %v", err)
	}
	// The same project NAME declared twice (G13): the COLUMN decides, and
	// this one was declared in founders.
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "primary card in a closed column", Project: "engineering", Epic: "Far",
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
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.EpicStateTitle, Epic: "Closed", Domain: "founders"},
		{ItemID: "orig", Title: "original", Team: "founders"},
		{ItemID: "rev", Title: "review", ReviewOf: "orig"},
		{ItemID: "kid", Title: "child", Parent: "rev", Epic: "Closed"},
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
	if c.Epic != "" {
		cd, _ := board.ColumnDomain(b, c.Project, c.Epic)
		t.Fatalf("the column names %q, the card lives in %q: %+v",
			cd, board.DomainOf(c, board.Resolver(b, "")), c)
	}
}

// The plan's × still deletes a card whose only home was the plan — the
// rule api.md states — while writing nothing for a card that was never in
// it. A SUBTASK is never deleted here: its home is its parent.
func TestRemoveFromThePlanStillDeletesACardWithNowhereElseToBe(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "c1", Title: "plan card", Team: "platform", Plan: board.PlanWed, Week: "2026-08-24"},
	})
	if err := New(f).Remove(ctx, "acme", "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("the plan was its only home")
	}
}

// The personal door is a create door like the others: a parent it names
// must exist, must not be a subtask itself, and must live in the
// repository the new card will — a personal card's file is in the
// actor's own repository, so a team parent elsewhere would put the two
// apart, which is the state the whole rule refuses. And it must actually
// GROUP: writing the field straight through left a card that was a
// subtask in name and in nothing else — no sprint or person synced, no
// plan slot handed over, no riders cleared.
func TestThePersonalDoorValidatesAndGroupsLikeTheOthers(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "mine", Title: "my own", Domain: board.PersonalDomain("kvaps"),
			Assignees: []string{"kvaps"}},
		{ItemID: "kid", Title: "already a subtask", Parent: "mine",
			Domain: board.PersonalDomain("kvaps")},
		{ItemID: "team", Title: "a team card", Team: "platform"},
	})
	svc := New(f)
	actor := board.WithActor(ctx, "kvaps")

	if _, err := svc.CreateCard(actor, "acme", CreateCardArgs{
		Title: "child", Personal: true, Parent: "ghost",
	}); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("a parent that does not exist: %v", err)
	}
	if _, err := svc.CreateCard(actor, "acme", CreateCardArgs{
		Title: "child", Personal: true, Parent: "kid",
	}); !errors.Is(err, ErrSubtaskDepth) {
		t.Fatalf("subtasks are one level deep: %v", err)
	}
	if _, err := svc.CreateCard(actor, "acme", CreateCardArgs{
		Title: "child", Personal: true, Parent: "team",
	}); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a personal card's file is the actor's own: %v", err)
	}
	// And the lawful one is really grouped: the parent's person is on it,
	// which only SetParent does.
	c, err := svc.CreateCard(actor, "acme", CreateCardArgs{
		Title: "child", Personal: true, Parent: "mine",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	got, _ := findCard(b, c.ItemID)
	if got.Parent != "mine" {
		t.Fatalf("grouped: %+v", got)
	}
	if !f.saw("SetParent " + c.ItemID + " mine") {
		t.Fatalf("the grouping must go through SetParent, not the field: %v", f.log)
	}
}

// A subtask is never a plan card — grouping hands its slot to the parent —
// so a create asking for both is asking for two contradictory things. It
// used to be answered by MOVING THE PARENT into the band the request named
// for the child: a mutation of a card nobody asked about.
func TestCreatingAPlanCardUnderAParentIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
	})
	svc := New(f)
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "planned child", Team: "platform", Parent: "p", Plan: board.PlanWed,
	}); !errors.Is(err, ErrPlanSubtask) {
		t.Fatalf("a subtask has no band of its own: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	parent, _ := findCard(b, "p")
	if parent.Plan != board.PlanNone {
		t.Fatalf("and the parent keeps its own plan: %+v", parent)
	}
	if n := f.count("CreateCard"); n != 0 {
		t.Fatalf("the refusal comes before the write: %d creates", n)
	}
}

// The same rule at the PATCH door: a card already grouped cannot be given
// a band by an edit, or the rule would hold on the way in and be undone by
// the next update (PATCH spec.plan.band, MCP update_card). Clearing one is
// free — that is how grouping hands the slot over.
func TestGivingAStandingSubtaskAPlanBandIsRefused(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "kid", Title: "child", Team: "platform", Parent: "p"},
	})
	svc := New(f)
	if err := svc.SetPlan(ctx, "acme", "kid", board.PlanWed); !errors.Is(err, ErrPlanSubtask) {
		t.Fatalf("a subtask has no band of its own: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, _ := findCard(b, "kid")
	if c.Plan != board.PlanNone {
		t.Fatalf("and the refusal writes nothing: %+v", c)
	}
	if err := svc.SetPlan(ctx, "acme", "kid", board.PlanNone); err != nil {
		t.Fatalf("clearing is free: %v", err)
	}
}

// A create whose grouping fails leaves NOTHING behind: the point of the
// undo, and the case the double-failure test cannot show, since there the
// undo fails too.
func TestAFailedGroupingLeavesNoCardBehind(t *testing.T) {
	f := mirrorBoard([]board.Card{
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
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.EpicStateTitle, Epic: "Closed", Domain: "founders"},
		{ItemID: "orig", Title: "original", Team: "founders"},
		{ItemID: "rev", Title: "review", ReviewOf: "orig"},
		{ItemID: "kid", Title: "child", Parent: "rev", Epic: "Closed"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).DeleteCard(ctx, "acme", "orig"); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range f.eventsOf("kid") {
		if e.Kind == board.EventEpic && e.From == " / Closed" && e.To == "" {
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
		{ItemID: "ep-cozy", Title: board.EpicStateTitle, Epic: "Cozystack", Project: "engineering", Domain: "aeman-db"},
		{ItemID: "ep-inbox", Title: board.EpicStateTitle, Epic: "Inbox", Domain: "aeman-db"},
		{ItemID: "ep-other", Title: board.EpicStateTitle, Epic: "Other", Domain: "aeman-db"},
		{ItemID: "pr-s", Title: board.ProjectStateTitle, Project: "strategy", Domain: "founders"},
		{ItemID: "ep-fund", Title: board.EpicStateTitle, Epic: "Fundraising", Project: "strategy", Domain: "founders"},
	}
	f := newFake(append(roster, cards...), map[string]board.SprintState{
		"platform": {Current: board.TodayIso(), ItemID: "st-p"},
	})
	f.b.Primary = "aeman-db"
	f.b.Domains = map[string]string{"st-p": "aeman-db"}
	return f
}

// A card in a no-project column of the PRIMARY, placed by nothing else:
// the column is stamped "aeman-db", the placement rule says "nothing
// places this", and the two must still mean the same repository.
func TestTheRulesReadOneNamespaceOnAStampedBoard(t *testing.T) {
	f := stampedBoard([]board.Card{
		{ItemID: "c1", Title: "in the bucket", Epic: "Inbox"},
	})
	svc := New(f)

	// Creating into that column is not a cross-repository move.
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{Title: "another", Epic: "Inbox"}); err != nil {
		t.Fatalf("the bucket is a column of this very board: %v", err)
	}
	// Nor is mirroring within it, nor attaching to a sibling column.
	if err := svc.Mirror(ctx, "acme", "c1", "", "Other"); err != nil {
		t.Fatalf("both columns are of the primary: %v", err)
	}
	none := ""
	if err := svc.SetEpic(ctx, "acme", "c1", "Other", &none); err != nil {
		t.Fatalf("and the card may be re-filed between them: %v", err)
	}
	// The closed repository is still closed to it.
	if err := svc.Mirror(ctx, "acme", "c1", "strategy", "Fundraising"); !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("a column of another repository still cannot show it: %v", err)
	}
}

// The repair must not fire on a card whose column was fine: deleting a
// parent freed the child and stripped its column, because the column read
// "aeman-db" and the freed card read "".
func TestAStampedBoardKeepsAFreedSubtasksLawfulColumn(t *testing.T) {
	f := stampedBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "platform", Epic: "Inbox"},
	})
	if err := New(f).DeleteCard(ctx, "acme", "p"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "kid")
	if !ok {
		t.Fatal("the child is freed, not deleted")
	}
	if c.Epic != "Inbox" {
		t.Fatalf("its column was never stranded and must survive: %+v", c)
	}
}

// api.md and the matrix agree that the plan's × never deletes a SUBTASK —
// its home is its parent, so the plan cannot be its last one. A subtask
// carrying a plan WEEK but no band slipped past the no-band return and
// was deleted.
func TestThePlanRemoveNeverDeletesASubtaskThatCarriesAWeek(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "child with a stale week", Team: "platform", Parent: "p",
			Week: "2026-08-24"},
	})
	if err := New(f).Remove(ctx, "acme", "c1", "plan"); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("a subtask's home is its parent; the plan × cannot be its last home")
	}
	if c.Week != "" {
		t.Fatalf("the plan's records are emptied: %+v", c)
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
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.EpicStateTitle, Epic: "Closed", Domain: "founders"},
		{ItemID: "p", Title: "parent in the closed repository", Team: "founders", Domain: "founders"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "platform", Epic: "Closed",
			SprintStart: board.TodayIso()},
		{ItemID: "rev", Title: "review of the child", ReviewOf: "kid", Team: "founders",
			Epic: "Closed", Domain: "founders"},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	err := New(f).Remove(ctx, "acme", "kid", "grid")
	if !errors.Is(err, ErrCrossDomain) {
		t.Fatalf("the follower's column refuses the pull-out: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "kid")
	if !ok {
		t.Fatal("a refused × deletes nothing")
	}
	if c.Parent != "p" || c.Epic != "Closed" {
		t.Fatalf("a refused × writes nothing: %+v", c)
	}
	if f.count("SetEpic") > 0 {
		t.Fatalf("not even the repair: %v", f.log)
	}
}

// The × on a subtask whose column cannot come along leaves the card where
// every other columnless card lands — never alive with nothing. Releasing
// it "to its column" after the column had been repaired away cleared its
// sprint and its dates too, and a card with no sprint, no dates, no band
// and no column is on no board anyone can open: findable only by id, by
// someone who already knows it exists.
func TestAStrandedColumnLeavesTheCardOnABoard(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.EpicStateTitle, Epic: "Closed", Domain: "founders"},
		{ItemID: "p", Title: "parent in the closed repository", Team: "founders"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "platform", Epic: "Closed",
			SprintStart: board.TodayIso(), StartDate: board.AddDays(board.TodayIso(), -2)},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).Remove(ctx, "acme", "kid", "grid"); err != nil {
		t.Fatalf("the × must complete: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "kid")
	if !ok {
		t.Fatal("the card is kept: platform has a previous sprint to demote into")
	}
	if hasColumn(c) || c.Plan != board.PlanNone {
		t.Fatalf("the column could not come along, and it had no band: %+v", c)
	}
	if !inWorkingArea(c) {
		t.Fatalf("so the working area is where it stays — demoted, not nowhere: %+v", c)
	}
	if c.SprintStart != board.AddDays(board.TodayIso(), -1) {
		t.Fatalf("in the previous sprint, the way the × demotes any other card: %+v", c)
	}
}

// The other half of the same law: with nothing to demote INTO, the × on
// such a card means what it means for every other card whose last home it
// empties — deletion. The alternative is the state above, kept alive for
// the sake of not deleting it.
func TestAStrandedColumnWithNoSprintToDemoteIntoDeletesTheCard(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.EpicStateTitle, Epic: "Closed", Domain: "founders"},
		{ItemID: "st-n", Title: board.SprintStateTitle, Team: "newcomers"},
		{ItemID: "p", Title: "parent in the closed repository", Team: "founders"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "newcomers", Epic: "Closed",
			SprintStart: board.TodayIso()},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).Remove(ctx, "acme", "kid", "grid"); err != nil {
		t.Fatalf("the × must complete: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "kid"); ok {
		t.Fatal("nowhere left to be, and nothing keeping it: the card is deleted")
	}
}

// The grid × on a subtask whose column belongs to its PARENT's repository
// completes: ungrouping re-files the card by its own team, so the column
// is stranded by the gesture itself, and refusing over it would name a
// column the person did not touch in answer to a move they did. The
// column is repaired — the same repair a deleted parent's release makes —
// and the card lands out of the group.
func TestTheGridRemoveCompletesWhenUngroupingStrandsTheColumn(t *testing.T) {
	f := mirrorBoard([]board.Card{
		{ItemID: "ep-closed", Title: board.EpicStateTitle, Epic: "Closed", Domain: "founders"},
		{ItemID: "p", Title: "parent in the closed repository", Team: "founders"},
		{ItemID: "kid", Title: "child", Parent: "p", Team: "platform", Epic: "Closed",
			SprintStart: board.TodayIso()},
	})
	f.b.SprintStates["founders"] = board.SprintState{Current: board.TodayIso(), ItemID: "st-f"}
	f.b.Domains = map[string]string{"st-f": "founders", "ep-closed": "founders"}
	if err := New(f).Remove(ctx, "acme", "kid", "grid"); err != nil {
		t.Fatalf("the × must complete: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	c, ok := findCard(b, "kid")
	if !ok {
		t.Fatal("the card is kept")
	}
	if c.Parent != "" {
		t.Fatalf("out of the group: %+v", c)
	}
	if c.Epic != "" {
		t.Fatalf("and out of a column its repository does not hold: %+v", c)
	}
}
