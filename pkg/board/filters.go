package board

import "slices"

// TeamGrid returns the cards on the Team board for one team on a given day:
// what the people are WORKING ON then, and nothing else. It is the Triage
// board's own "now" column for that day, less anything put off to later, and
// it mirrors filteredCards in TeamBoard.tsx.
//
// A card is in hand on a day when it is this team's, is not a subtask (those
// ride with their parent), is not parked on a list, has not been planned into
// a week AFTER that day's (B1: a card placed in a week ahead is on no day
// board until its Monday), has not been deferred past that day, and is either
// open or was finished on that very day — a card finished yesterday belongs to
// yesterday, and a board that keeps it is a board nobody can read.
//
// One day answers differently, and deliberately: the day a SPRINT BEGAN shows
// that sprint's own work whatever has become of it since. That is the view a
// team opens to read the sprint it is in, and the day navigator jumps to it.
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
// works here, because the two gates it applies are both relative to the day
// asked about — a card deferred to Thursday is in hand on Thursday.
func TeamGrid(b Board, team, day string) []Card {
	out := []Card{}
	for _, c := range b.Cards {
		if c.Parent != "" || c.Team != team || InBacklog(c) {
			continue
		}
		if c.Week != "" && c.Week > MondayOf(day) {
			continue
		}
		if c.StartDate != "" && c.StartDate > day {
			continue
		}
		// The day a sprint BEGAN shows that sprint's own work, whatever has
		// become of it since. It is not another way of asking what is in hand
		// — it is the view a team opens to read the sprint it is in, and the
		// one they open it on: the day navigator's jump lands here. Dropping
		// it made the board answer differently depending on which day you
		// arrived at, which is the confusion this rule set out to end.
		if c.SprintStart == day {
			out = append(out, c)
			continue
		}
		if Complete(c.Stage, c.Progress) && finishedOn(c) != day {
			continue
		}
		out = append(out, c)
	}
	return out
}

// finishedOn is the day a finished card belongs to. doneAt is the record and
// is what the board writes — but it is a young field, and these repositories
// are open: a card finished by any other writer, or before the field existed,
// carries none. Falling back to the card's own dates is what keeps such a
// card on the day somebody worked it rather than dropping it out of every
// day at once, and a card with no dates at all is left alone: nothing says
// when it was finished, so nothing may say it was not that day.
func finishedOn(c Card) string {
	switch {
	case c.DoneAt != "":
		return c.DoneAt
	case c.Day != "":
		return c.Day
	default:
		return c.StartDate
	}
}

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
