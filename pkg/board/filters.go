package board

import "slices"

// TeamGrid returns the cards on the Team board for one team on a given day:
// what the people are WORKING ON then, and nothing else. It mirrors
// inHandOn in web/src/teamgrid.ts.
//
// A card is in hand on a day when it is this team's, is not a subtask (those
// ride with their parent), is not parked in the team's backlog, is IN WORK —
// it has a date or a sprint (K1; a task with neither is in its backlog), has
// not been deferred past that day, and is either open or was finished on
// that very day — a card finished yesterday belongs to yesterday, and a board
// that keeps it is a board nobody can read. A task has no week (K7).
//
// One day answers differently, and deliberately: the day a SPRINT BEGAN is
// the whole sprint. A lead opens it every few mornings and goes through it
// with the team, so it holds the work the sprint opened with, the work typed
// into it since, and the work already closed — not only what is still in
// hand. What still leaves that day is what was taken OUT of the sprint: work
// deferred past today.
//
// That is the whole rule. It used to be seven, layered: the week's own work,
// the sprint's start day, the card's own scheduled day, the range between its
// dates, the days of sprints it had passed through, and two special cases for
// deferral. Every one of them was a different reason for a card to be drawn,
// and between them they managed both halves of being wrong — work nobody was
// doing appeared on the day, while a card scheduled for last Tuesday and
// never finished fell off the board entirely, since no rule reached it any
// more. A day board answers one question, so it asks one.
//
// Looking BACK is not this function's job: a day already past is answered
// from the board's history, as it was (the snapshot path). Looking forward
// works here, because every gate it applies is relative to the day asked
// about — a card deferred to Thursday is in hand on Thursday.
func TeamGrid(b Board, team, day string) []Card { return TeamGridIn(b, NewIndex(b), team, day) }

// TeamGridIn is TeamGrid over an index built once for a listing.
func TeamGridIn(b Board, ix *Index, team, day string) []Card {
	today := TodayIso()
	out := []Card{}
	planning := ix.planning
	for _, c := range b.Cards {
		// The day boards draw TASKS: a goal, an initiative or an epic is the
		// Roadmap's (H3), and a subtask rides its parent. A task under an
		// epic is neither — it stands here like any task (H1).
		// A task stands under its EFFECTIVE team (O13): its own, else the
		// plan item's above it.
		if IsPlanning(c) || IsSubtaskIn(c, planning) || ix.PlacedTeam(c) != team || InBacklog(c) {
			continue
		}
		// A card in a Scope column is a PROJECT CARD: the Project board's
		// plan, not work in hand, whatever its week or dates say (H4,
		// docs/design/hierarchy.md). It used to stand in its owner's column
		// once its week came, which put planning placeholders among the
		// day's work; the two boards are disjoint now.
		if c.Scope != "" {
			continue
		}
		// IN WORK: a date or a sprint (K1). A task with neither is in its
		// team's backlog, which is not a day's work, or the backlog would
		// land in everybody's column and stay there for good. It is also the
		// only bound this rule has on how far back or forward a card reaches.
		if !InWork(c) {
			continue
		}

		// THE SPRINT'S OWN DAY IS THE WHOLE SPRINT. Every few mornings a lead
		// opens the day the sprint began — the "current sprint" jump lands
		// there — and goes through it with the team, so that day must hold
		// everything the sprint has been: the work it opened with, the work
		// typed into it on its second and third days (most of a sprint is
		// created inside it), and the work already CLOSED. Hence this clause
		// stands above BOTH the gate on the card's own start date and the
		// gate on being finished — a day that showed only what is still open
		// answered "what is left", which is not the question the meeting
		// asks.
		//
		// One thing still leaves that day: work somebody DEFERRED past today
		// goes at once. Deferring is the act of taking a card out of the
		// sprint in progress — sent to tomorrow it leaves today's sprint and
		// arrives tomorrow; sent three days out, the sprint that opens
		// tomorrow starts without it.
		if inSprintOn(b, c, team, day, today) && !deferredPast(c, today) {
			out = append(out, c)
			continue
		}
		// Finished work belongs to the day it was finished on — with one
		// exception on TODAY's live board: a done card whose sprint the team
		// has CARRIED PAST was left behind by Carry Over on purpose, so it
		// does not come back to today merely because it was finished today. It
		// still stands on the day it recorded (its sprint's own day, served as
		// a snapshot once past). A done card of the CURRENT sprint is already
		// kept above; one of no sprint at all (a dated card) is never
		// closed and shows on its day as before.
		if Complete(c.Stage, c.Progress) &&
			(!finishedOn(c, day) || (day == today && inClosedSprint(b, c, team))) {
			continue
		}
		// Put off to a later day: gone from the board until that day comes.
		if deferredPast(c, day) {
			continue
		}
		// A day still to COME is a plan, not a state (see plannedFor).
		if day > today && !plannedFor(c, day) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// inSprintOn reports that a day answers for the card's SPRINT — which is what
// makes that day hold the sprint's work whatever has become of it, finished
// included.
//
// The team's CURRENT sprint accumulates all of its work — open AND closed — on
// every one of its days, from the day it began through today. A lead opening
// any day of the running sprint reads "what has this sprint been", the work it
// opened with, the work typed into it since, and the work already CLOSED,
// rather than "what is left on this one day". Carry Over is the reset: it
// leaves a finished card on the closing sprint (its SprintStart is not moved),
// so the moment a new sprint opens that done work is a PREVIOUS sprint's and
// stops showing on the current board — which is how "the sprint keeps its done
// work until carry-over moves on" is meant to read.
//
// A sprint that is no longer the team's current one answers for its whole self
// only on the day it BEGAN — its own page, served as a snapshot once the day
// is past. Without a sprint-state pointer the team's current sprint is
// unknown, so the same narrow day-it-began answer holds.
func inSprintOn(b Board, c Card, team, day, today string) bool {
	if c.SprintStart == "" {
		return false
	}
	if c.SprintStart == CurrentSprint(b, team) {
		// A card TAKEN OFF the board (LeftAt — the legacy × demote) is not part
		// of the live sprint's accumulation: it belongs to the record of the
		// day it was removed, which gives it back, not to every day of the
		// sprint it was demoted into.
		return c.LeftAt == "" && day >= c.SprintStart && day <= today
	}
	return c.SprintStart == day
}

// deferredPast reports a card put off to a day later than the one given: it
// is off the board until that day arrives.
func deferredPast(c Card, day string) bool { return c.StartDate != "" && c.StartDate > day }

// plannedFor reports whether somebody put the card on a day: its own dates
// reach that day.
//
// It is asked of the days AHEAD only, and that asymmetry is the point. TODAY
// is "what is in hand": a card planned for last Tuesday and still open stands
// there, because work that ran over is the work most in need of being looked
// at. TOMORROW is a plan — it holds what somebody actually put there, and
// today's unfinished work is today's problem rather than tomorrow's plan.
//
// Without the bound the day board had no forgetting at all: every open card
// stood on every future day, so a month out was simply the team's whole
// backlog (122 of 122 open cards on one production board), and "what is
// planned for tomorrow" could not be read anywhere.
func plannedFor(c Card, day string) bool {
	return ActiveOnDay(c.StartDate, c.Day, day)
}

// finishedOn reports whether a FINISHED card belongs to the day being looked
// at: the day it RECORDED being finished on (doneAt), and no other.
//
// It guessed at first — doneAt, else the card's end date, else its start — so
// that work closed before the field existed would land somewhere. Both
// fallbacks are a PLAN rather than evidence, and the guess showed it: a card
// stretched three weeks ahead and closed today stood on a day three weeks
// out, as though it had been finished then, and on none of the days it was
// actually worked.
//
// Nothing is lost by refusing to guess, because this rule answers only TODAY
// and the days ahead. Every day already gone is served as a SNAPSHOT of the
// tree at that day's last commit (snapshotDay: a me/team day before today is
// always asked for that way, unless the team is still inside that sprint) —
// and there the question is not guessed at either: the day's commits name the
// cards they touched, so a card that is done in the day's tree and was
// written during the day is filled in as finished then
// (gitstore.markFinishedInDay). What reaches HERE with nothing recorded was
// closed on no day this board can name, and there is no day ahead on which
// somebody finished it.
func finishedOn(c Card, day string) bool { return c.DoneAt == day }

// inClosedSprint reports a card whose sprint the team has already CARRIED
// PAST — its SprintStart is neither the current sprint nor empty. Such a
// card is that closed sprint's record (shown on its own day and in the
// history), not the live board, so a done one does not come back to today on
// the strength of having been finished today. A card in the CURRENT sprint,
// or in no sprint at all, is not closed. It needs the sprint-state pointer;
// without one (current unknown) nothing is treated as closed.
func inClosedSprint(b Board, c Card, team string) bool {
	cur := CurrentSprint(b, team)
	return c.SprintStart != "" && cur != "" && c.SprintStart != cur
}

// MeView returns the cards on a person's own day board on a given day:
// the user's cards (user = "" means everyone) that belong to the sprint that was
// active on the viewed day (activeSprint) and whose scheduled day has arrived
// (startDate empty or on or before the viewed day). Today shows the current
// sprint; rolling back into the previous sprint's days shows that sprint's cards.
// A card whose team had no active sprint on the day, or that is deferred to the
// future, never shows. It mirrors myCards in MeBoard.tsx.
func MeView(b Board, user, day string) []Card { return MeViewIn(b, NewIndex(b), user, day) }

// MeViewIn is MeView over an index built once for a listing.
func MeViewIn(b Board, ix *Index, user, day string) []Card {
	today := TodayIso()
	out := []Card{}
	planning := ix.planning
	for _, c := range b.Cards {
		// A planning card is the Roadmap's, not a person's day (H3).
		if IsPlanning(c) {
			continue
		}
		// Subtasks are never listed on their own; they ride with their
		// parent. A task under an epic is not one (H1).
		if IsSubtaskIn(c, planning) {
			continue
		}
		// A card in a Scope column is a PROJECT CARD and is drawn on the
		// Project board alone (H4): it has no owner to look for it here, and
		// one written before schema 2 that still names one is a plan all the
		// same. TeamGrid draws the same line.
		if c.Scope != "" {
			continue
		}
		if user != "" && !slices.Contains(c.Assignees, user) && !childAssigned(b, c.ItemID, user) {
			continue
		}
		// One parked in the team's backlog is not this person's day: it is
		// waiting to be taken into work, not being worked on (K1). A task has
		// no week to read (K7).
		if InBacklog(c) {
			continue
		}
		// A deferred / future-scheduled card (startDate past today) is hidden
		// until that day, then shows from it on (Carry Over re-syncs its sprint).
		if c.StartDate != "" && c.StartDate > today {
			if day >= c.StartDate {
				out = append(out, c)
			}
			continue
		}
		// An OPEN card of the person's keeps standing on their LIVE board (today)
		// even when its sprint has fallen behind the team's current one. A card
		// left out of a carry-over is still unfinished, and this board is the one
		// that keeps everything of theirs that is not closed — so it must not
		// vanish the way it did when its end date passed (the range gate below
		// stops there) and its sprint fell two carry-overs back (the sprint-span
		// gate below wants the active sprint). It is the Me-board echo of the Team
		// board keeping overran work. Scoped to TODAY and to a card that HAS a
		// sprint: a day still to come is a plan, a past day reads by its own
		// sprint, and an old sprint-LESS stray still does not resurface (below).
		if day == today && c.SprintStart != "" && !Complete(c.Stage, c.Progress) &&
			(c.StartDate == "" || c.StartDate <= day) {
			out = append(out, c)
			continue
		}
		// A card with an end date spans a range: it shows on every day from its
		// start through its end regardless of sprint boundaries.
		if c.StartDate != "" && c.Day != "" && c.Day >= c.StartDate &&
			day >= c.StartDate && day <= c.Day {
			out = append(out, c)
			continue
		}
		// A task reads its EFFECTIVE team's sprint (O13).
		as := ActiveSprint(b, ix.PlacedTeam(c), day)
		// A sprint-less day card (a "next sprint" create) stays visible from its
		// scheduled day on — the sprint gate below would otherwise hide it right
		// when its day arrives, until a carry-over adopts it into a sprint. Only
		// cards scheduled into the sprint active on the viewed day (or later)
		// qualify: an old sprint-less stray stays on its own past days instead
		// of resurfacing on today's board.
		if c.SprintStart == "" && c.StartDate != "" &&
			c.StartDate <= day && c.StartDate >= as {
			out = append(out, c)
			continue
		}
		// A card shows on every day of the sprints it spans — from the one it
		// started in up to the sprint it now belongs to — so a carried-over card
		// still appears on the previous sprint's days it came from.
		if as != "" && as <= c.SprintStart && (c.StartDate == "" || c.StartDate <= day) {
			out = append(out, c)
		}
	}
	return out
}

// childAssigned reports whether any subtask of a card is assigned to user —
// the Me board shows the parent when the person owns only a subtask.
func childAssigned(b Board, itemID, user string) bool {
	for _, c := range b.Cards {
		if c.Parent == itemID && slices.Contains(c.Assignees, user) {
			return true
		}
	}
	return false
}
