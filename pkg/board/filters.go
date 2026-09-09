package board

import "slices"

// TeamGrid returns the cards on the Team board for one team on a given day:
// what the people are WORKING ON then, and nothing else. It is the Triage
// board's own "now" column for that day, less anything put off to later, and
// it mirrors filteredCards in TeamBoard.tsx.
//
// A card is in hand on a day when it is this team's, is not a subtask (those
// ride with their parent), is not parked on a list, somebody has said WHEN it
// is for (a week, a date or a sprint — a card with none of the three is the
// Triage strip, which is an inbox rather than a day's work), has not been
// planned into a week AFTER that day's (B1: a card placed in a week ahead is
// on no day board until its Monday), has not been deferred past that day, and
// is either open or was finished on that very day — a card finished yesterday
// belongs to yesterday, and a board that keeps it is a board nobody can read.
//
// One day answers differently, and deliberately: the day a SPRINT BEGAN is
// the whole sprint. A lead opens it every few mornings and goes through it
// with the team, so it holds the work the sprint opened with, the work typed
// into it since, and the work already closed — not only what is still in
// hand. What still leaves that day is what was taken OUT of the sprint: work
// deferred past today, and work planned into a week still to come.
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
func TeamGrid(b Board, team, day string) []Card {
	today := TodayIso()
	out := []Card{}
	for _, c := range b.Cards {
		if c.Parent != "" || c.Team != team || InBacklog(c) {
			continue
		}
		// Somebody has to have said WHEN, one way or another: a week, a date,
		// or a sprint. A card with none of the three is the Triage strip —
		// "nobody has said" — and the strip is the one thing a day board must
		// not draw, or the inbox lands in everybody's column and stays there
		// for good. It is also the only bound this rule has on how far back
		// or forward a card reaches.
		if c.Week == "" && c.StartDate == "" && c.Day == "" && c.SprintStart == "" {
			continue
		}
		if c.Week != "" && c.Week > MondayOf(day) {
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
		// Two things still leave that day. Work planned into a week still to
		// come never reached it (the gate above). And work somebody DEFERRED
		// past today goes at once: deferring is the act of taking a card out
		// of the sprint in progress — sent to tomorrow it leaves today's
		// sprint and arrives tomorrow; sent three days out, the sprint that
		// opens tomorrow starts without it.
		if c.SprintStart == day && !deferredPast(c, today) {
			out = append(out, c)
			continue
		}
		// Finished work belongs to the day it was finished on, and to no
		// other: a card finished yesterday belongs to yesterday, and a board
		// that keeps it is a board nobody can read.
		if Complete(c.Stage, c.Progress) && !finishedOn(c, day) {
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

// deferredPast reports a card put off to a day later than the one given: it
// is off the board until that day arrives.
func deferredPast(c Card, day string) bool { return c.StartDate != "" && c.StartDate > day }

// plannedFor reports whether somebody put the card on a day: its own dates
// reach that day, or it was placed in the week the day belongs to.
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
	if c.Week != "" && c.Week == MondayOf(day) {
		return true
	}
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

// MeView returns the cards on the personal day board for a user on a given day:
// the user's cards (user = "" means everyone) that belong to the sprint that was
// active on the viewed day (activeSprint) and whose scheduled day has arrived
// (startDate empty or on or before the viewed day). Today shows the current
// sprint; rolling back into the previous sprint's days shows that sprint's cards.
// A card whose team had no active sprint on the day, or that is deferred to the
// future, never shows. It mirrors myCards in MeBoard.tsx.
func MeView(b Board, user, day string) []Card {
	today := TodayIso()
	out := []Card{}
	for _, c := range b.Cards {
		// Subtasks are never listed on their own; they ride with their parent.
		if c.Parent != "" {
			continue
		}
		// An epic card lives on the Project board until it joins a sprint: its
		// week-spanning dates would otherwise smear it across the day boards.
		// The column is what holds it, and a column needs the epic side —
		// TeamGrid draws the same line. Unless someone owns it: then it is
		// that person's work, and their day board is where they look for it;
		// it shows across the days its own dates cover, like any other dated
		// card.
		if c.Epic != "" && c.SprintStart == "" && len(c.Assignees) == 0 {
			continue
		}
		if user != "" && !slices.Contains(c.Assignees, user) && !childAssigned(b, c.ItemID, user) {
			continue
		}
		// A card placed in a week ahead waits in the backlog (B1).
		if PlacedAhead(c, today) {
			continue
		}
		// And one parked on a list is not this person's day either: it is
		// waiting to be planned, not being worked on.
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
		// A card with an end date spans a range: it shows on every day from its
		// start through its end regardless of sprint boundaries.
		if c.StartDate != "" && c.Day != "" && c.Day >= c.StartDate &&
			day >= c.StartDate && day <= c.Day {
			out = append(out, c)
			continue
		}
		as := ActiveSprint(b, c.Team, day)
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
// the personal board shows the parent when the person owns only a subtask.
func childAssigned(b Board, itemID, user string) bool {
	for _, c := range b.Cards {
		if c.Parent == itemID && slices.Contains(c.Assignees, user) {
			return true
		}
	}
	return false
}
