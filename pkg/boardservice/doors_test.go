package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// EVERY DOOR PASSES THROUGH THE SERVICE (ADR 0002). These rules were written
// in the HTTP handler, where only one of the two doors passes: the browser
// obeyed them and MCP walked straight past. An agent could file a card with
// no title, defer one BACKWARDS, send a card to review with nobody to review
// it, and leave an empty note — none of which a person can do with a mouse.
//
// They are not view rules. A card with no title is wrong on every board, so
// the check belongs where every caller arrives.
func TestTheServiceHoldsTheRulesTheHandlerHeld(t *testing.T) {
	seed := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "c1", Title: "the work", Team: "alpha", Assignees: []string{"kvaps"},
				StartDate: "2026-09-11", Day: "2026-09-11", SprintStart: "2026-09-11"},
		}, map[string]board.SprintState{"alpha": {Current: "2026-09-11"}})
	}

	t.Run("a card needs a title", func(t *testing.T) {
		f := seed()
		if _, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{Team: "alpha"}); err == nil {
			t.Fatal("a card with no title was created; nothing on a board can be named nothing")
		}
		// Spaces are nothing too — a card called "   " is a blank row on
		// every board, and the guard reads the title trimmed for exactly
		// that reason.
		if _, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{Team: "alpha", Title: "   "}); !errors.Is(err, ErrEmptyTitle) {
			t.Fatalf("a card named in spaces = %v, want ErrEmptyTitle", err)
		}
	})

	t.Run("defer moves a card forward, never back", func(t *testing.T) {
		f := seed()
		before := f.get("c1").StartDate
		err := f2svc(f).Defer(ctx, "acme", "c1", -5)
		if err == nil {
			t.Fatalf("defer -5 was accepted and moved the card to %s; putting work OFF is the whole gesture",
				f.get("c1").StartDate)
		}
		if got := f.get("c1").StartDate; got != before {
			t.Fatalf("a refused defer still moved the card: %s → %s", before, got)
		}
		// Zero is the same request with nothing in it: the rule is days <= 0,
		// and a defer that moves a card nowhere is a press that did nothing.
		if err := f2svc(f).Defer(ctx, "acme", "c1", 0); !errors.Is(err, ErrBackwardsDefer) {
			t.Fatalf("defer 0 = %v, want ErrBackwardsDefer", err)
		}
	})

	t.Run("a review needs somebody to review it", func(t *testing.T) {
		f := seed()
		if _, err := f2svc(f).SendToReview(ctx, "acme", "c1", "", "2026-09-11", ""); err == nil {
			t.Fatal("a review card was created with no reviewer; it is the artefact of asking somebody")
		}
		if _, err := f2svc(f).SendToReview(ctx, "acme", "c1", "  ", "2026-09-11", ""); !errors.Is(err, ErrNoReviewer) {
			t.Fatalf("a reviewer named in spaces = %v, want ErrNoReviewer", err)
		}
	})

	// The same rule from the other side, which the sentinels' own words
	// already claimed: a RENAME with nothing to call the card by leaves it
	// nameless on every board, and an edit that empties a note is a delete
	// wearing another gesture's name.
	t.Run("a rename needs a title", func(t *testing.T) {
		f := seed()
		if err := f2svc(f).Rename(ctx, "acme", "c1", "   "); !errors.Is(err, ErrEmptyTitle) {
			t.Fatalf("renaming a card to nothing = %v, want ErrEmptyTitle", err)
		}
		if got := f.get("c1").Title; got != "the work" {
			t.Fatalf("a refused rename still wrote %q", got)
		}
	})

	t.Run("an edit needs words too", func(t *testing.T) {
		f := seed()
		svc := f2svc(f)
		if err := svc.AddNote(ctx, "acme", "c1", "a note"); err != nil {
			t.Fatal(err)
		}
		// The fake records the write rather than keeping the thread, so the
		// note is put on the card by hand: the rule under test is the TEXT,
		// which is read before the note is even looked up.
		f.get("c1").Notes = []board.Note{{ID: "n1", Body: "a note", Author: "kvaps"}}
		if err := svc.EditNote(ctx, "acme", "c1", "n1", "  "); !errors.Is(err, ErrEmptyNote) {
			t.Fatalf("emptying a note by editing it = %v, want ErrEmptyNote", err)
		}
		if err := svc.EditNote(ctx, "acme", "c1", "n1", "said something"); err != nil {
			t.Fatalf("an edit with words = %v, want it taken", err)
		}
	})

	t.Run("a note needs words", func(t *testing.T) {
		f := seed()
		if err := f2svc(f).AddNote(ctx, "acme", "c1", "   "); err == nil {
			t.Fatal("an empty note was filed")
		}
	})

	// And the card it answers with is the review that now EXISTS. Reassigning
	// does not edit the old review card in place: a reviewer who has already
	// worked on it keeps their card (it is unlinked and a fresh one is made
	// for the new reviewer), and a finished one is reactivated with its
	// progress reset. Answering from the board as it stood BEFORE the write
	// hands the caller the old card — the one with the old reviewer still on
	// it — and an agent reads "the reviewer is still Bob" from a call that
	// just moved the review to Carol.
	t.Run("and answers with the review that now exists", func(t *testing.T) {
		f := seed()
		svc := f2svc(f)
		first, err := svc.SendToReview(ctx, "acme", "c1", "carol", "2026-09-11", "")
		if err != nil {
			t.Fatal(err)
		}
		// Carol starts on it: her card is hers to keep, so the reassign makes
		// a new one for dave rather than moving hers.
		if err := svc.SetProgress(ctx, "acme", first.ItemID, 40); err != nil {
			t.Fatal(err)
		}
		second, err := svc.SendToReview(ctx, "acme", "c1", "dave", "2026-09-11", "")
		if err != nil {
			t.Fatal(err)
		}
		if second.ItemID == first.ItemID {
			t.Fatalf("the answer is carol's old card (%s); dave's is a new one", second.ItemID)
		}
		if len(second.Assignees) != 1 || second.Assignees[0] != "dave" {
			t.Fatalf("the answer names %v, want the new reviewer", second.Assignees)
		}
	})

	// Sending a card to review TWICE is how a reviewer is changed — the
	// handler folded that in by hand, so MCP grew a second review card on
	// the same original instead. Two reviewers for one card is a state no
	// board can draw.
	t.Run("sending again reassigns rather than growing a second review", func(t *testing.T) {
		f := seed()
		svc := f2svc(f)
		if _, err := svc.SendToReview(ctx, "acme", "c1", "carol", "2026-09-11", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SendToReview(ctx, "acme", "c1", "dave", "2026-09-11", ""); err != nil {
			t.Fatal(err)
		}
		reviews := 0
		var who []string
		bd, err := f.LoadBoard(ctx, "acme")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range bd.Cards {
			if c.ReviewOf == "c1" {
				reviews++
				who = append(who, c.Assignees...)
			}
		}
		if reviews != 1 {
			t.Fatalf("review cards on one original = %d %v, want one reassigned", reviews, who)
		}
		if len(who) != 1 || who[0] != "dave" {
			t.Fatalf("the review is on %v, want dave", who)
		}
	})

	// A size the scale does not know is refused when SET; it was waved
	// through on CREATE, so the same value arrived by the other door and
	// weighed nothing at all on every board that sums points.
	t.Run("a size the scale does not know is refused on create too", func(t *testing.T) {
		f := seed()
		_, err := f2svc(f).CreateCard(ctx, "acme", CreateCardArgs{
			Title: "sized wrong", Team: "alpha", Size: board.SizeKey("large"),
		})
		if err == nil {
			t.Fatal("an unknown size was stored by the create door")
		}
		if !errors.Is(err, ErrUnknownSize) && !strings.Contains(err.Error(), "size") {
			t.Fatalf("the refusal must name the size: %v", err)
		}
	})

	// The calendar cannot produce it and Defer refuses it, but SetDates wrote
	// whatever it was given: a card due before it begins is overdue for ever.
	t.Run("an end never lands before the start", func(t *testing.T) {
		f := seed()
		if err := f2svc(f).SetDates(ctx, "acme", "c1", "2026-09-20", "2026-09-01"); err == nil {
			c := f.get("c1")
			t.Fatalf("end before start was stored: start %s day %s", c.StartDate, c.Day)
		}
	})
}

// THE SAME ADR, THE OTHER WAY ROUND. These rules were written in the browser
// — in triage.gripOf, in the stage menu, in removal.ts — so the mouse obeyed
// them and every other caller did as it pleased. A rule a client draws is
// still a rule about the board: a turn carried out of its own occurrence
// stands where the NEXT turn belongs, and the two read as one process running
// twice, whoever moved it.
func TestTheServiceHoldsTheRulesOnlyTheBrowserHeld(t *testing.T) {
	// Monthly, anchored on the 2nd: September's occurrence runs 08-31..09-21
	// (board.CycleWindow), and October's begins on the 28th.
	monthly := board.Card{ItemID: "t1", Title: board.ProcessTaskTitle, Process: "ops", Team: "alpha",
		Description: "backups", Recurrence: board.RecurrenceMonth, StartDate: "2026-09-02"}
	turns := func(task board.Card) *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "pr1", Title: board.ProcessStateTitle, Process: "ops", Team: "alpha"},
			task,
			{ItemID: "i1", Title: "backups", Team: "alpha", Task: task.ItemID, Week: "2026-09-07"},
		}, map[string]board.SprintState{"alpha": {Current: "2026-09-07"}})
	}

	t.Run("a turn moves inside its own occurrence", func(t *testing.T) {
		f := turns(monthly)
		if err := f2svc(f).SetWeek(ctx, "acme", "i1", "2026-09-14"); err != nil {
			t.Fatalf("a week inside the occurrence = %v, want it taken", err)
		}
		if got := f.get("i1").Week; got != "2026-09-14" {
			t.Fatalf("week = %q, want 2026-09-14", got)
		}
	})

	t.Run("and not out of it", func(t *testing.T) {
		f := turns(monthly)
		err := f2svc(f).SetWeek(ctx, "acme", "i1", "2026-09-28")
		if !errors.Is(err, ErrOutsideCycle) {
			t.Fatalf("October's week for September's turn = %v, want ErrOutsideCycle", err)
		}
		if got := f.get("i1").Week; got != "2026-09-07" {
			t.Fatalf("a refused move still wrote the week: %q", got)
		}
	})

	// The Triage board's own door — where the drag actually lands — reaches
	// the same rule by a different path.
	t.Run("the Triage board's drop asks the same", func(t *testing.T) {
		f := turns(monthly)
		if err := f2svc(f).Place(ctx, "acme", "i1", "2026-09-28"); !errors.Is(err, ErrOutsideCycle) {
			t.Fatalf("placing September's turn in October = %v, want ErrOutsideCycle", err)
		}
	})

	// A task that ACCUMULATES is the one exception the board draws: its turns
	// are meant to pile up, so one standing in another's week is the point.
	t.Run("a turn of a task that piles up goes where it is wanted", func(t *testing.T) {
		task := monthly
		task.Accumulate = true
		f := turns(task)
		if err := f2svc(f).SetWeek(ctx, "acme", "i1", "2026-09-28"); err != nil {
			t.Fatalf("moving an accumulating turn = %v, want it taken", err)
		}
	})

	// No calendar, no occurrence to stay inside — and nothing to reckon a
	// move against either. A per-sprint task's turn does not move in time.
	t.Run("a turn with no calendar does not move in time", func(t *testing.T) {
		perSprint := monthly
		perSprint.Description, perSprint.Recurrence, perSprint.StartDate = "retro", board.RecurrenceSprint, ""
		f := turns(perSprint)
		if err := f2svc(f).SetWeek(ctx, "acme", "i1", "2026-09-14"); !errors.Is(err, ErrOutsideCycle) {
			t.Fatalf("moving a per-sprint turn = %v, want ErrOutsideCycle", err)
		}
	})

	// The one way back. A turn whose occurrence is already past stands on no
	// day board at all — its days ran out — and the Triage grid is the only
	// place it is still drawn. Dragging it into the week being worked is what
	// brings it back, and the board leaves that open by clipping the grip to
	// the weeks on screen. A refusal here would strand it for good.
	t.Run("an overdue turn comes back into the week being worked", func(t *testing.T) {
		thisWeek := board.MondayOf(board.TodayIso())
		weekly := monthly
		weekly.Description, weekly.Recurrence = "standup", board.RecurrenceWeek
		weekly.StartDate = board.AddDays(thisWeek, -70)
		f := turns(weekly)
		// Three weeks behind, where its own occurrence ended.
		f.get("i1").Week = board.AddDays(thisWeek, -21)
		if err := f2svc(f).Place(ctx, "acme", "i1", thisWeek); err != nil {
			t.Fatalf("bringing a turn three weeks overdue into this week = %v, want it taken", err)
		}
		// Forward past this week is still another occurrence's place.
		f2 := turns(weekly)
		f2.get("i1").Week = board.AddDays(thisWeek, -21)
		if err := f2svc(f2).Place(ctx, "acme", "i1", board.AddDays(thisWeek, 7)); !errors.Is(err, ErrOutsideCycle) {
			t.Fatalf("sending an overdue turn into NEXT week = %v, want ErrOutsideCycle", err)
		}
	})

	// The degenerate case beside the two exceptions: a turn that stands in NO
	// week is in no occurrence, so nothing bounds it — it is being given its
	// first week rather than carried out of one.
	t.Run("a turn with no week yet is in no occurrence", func(t *testing.T) {
		f := turns(monthly)
		f.get("i1").Week = ""
		far := board.AddDays(board.MondayOf(board.TodayIso()), 7*20)
		if err := f2svc(f).SetWeek(ctx, "acme", "i1", far); err != nil {
			t.Fatalf("giving a weekless turn a week = %v, want it taken", err)
		}
	})

	// A review card is auxiliary: it is the artefact of asking somebody to
	// look at the card it reviews. Putting it on the review stage would make
	// a review of a review, which nothing can draw and nobody asked for.
	t.Run("a review card cannot be put on the review stage", func(t *testing.T) {
		f := newFake([]board.Card{
			{ItemID: "c1", Title: "the work", Team: "alpha", Assignees: []string{"kvaps"}},
			{ItemID: "r1", Title: "Review: the work", Team: "alpha", ReviewOf: "c1", Assignees: []string{"carol"}},
		}, nil)
		if err := f2svc(f).SetStage(ctx, "acme", "r1", board.StageReview); !errors.Is(err, ErrInvalidStage) {
			t.Fatalf("a review of a review = %v, want ErrInvalidStage", err)
		}
	})

	// The shelf holds work with a week of its own to give up. A review card
	// follows the card it reviews and a subtask stands inside its parent:
	// neither has a place of its own, so parking one strands it where no
	// board draws it — which is exactly what the × on both never offered.
	t.Run("the shelf refuses a card with no place of its own", func(t *testing.T) {
		f := newFake([]board.Card{
			{ItemID: "c1", Title: "the work", Team: "alpha"},
			{ItemID: "r1", Title: "Review: the work", Team: "alpha", ReviewOf: "c1"},
			{ItemID: "s1", Title: "a step", Team: "alpha", Parent: "c1"},
		}, nil)
		for _, id := range []string{"r1", "s1"} {
			if err := f2svc(f).SetBacklog(ctx, "acme", id, true); !errors.Is(err, ErrNoPlaceOfItsOwn) {
				t.Fatalf("parking %s = %v, want ErrNoPlaceOfItsOwn", id, err)
			}
			if f.get(id).Parked {
				t.Fatalf("%s was parked anyway", id)
			}
		}
	})
}
