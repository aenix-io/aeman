package board

import "strings"

// personalMarker prefixes a personal domain's name. No repository is named
// with it (`--repo name=url` names are plain), so a personal domain never
// collides with a configured one and is recognisable wherever the name
// shows up — status.domain, a badge, a selector.
const personalMarker = "~"

// PersonalDomain is the name of a person's personal domain: their own
// repository, attached to the board for them alone.
func PersonalDomain(login string) string { return personalMarker + login }

// IsPersonalDomain reports whether a domain name is a personal one.
func IsPersonalDomain(name string) bool {
	return len(name) > len(personalMarker) && strings.HasPrefix(name, personalMarker)
}

// PersonalOwner is the login a personal domain belongs to; "" for any other
// domain.
func PersonalOwner(name string) string {
	if !IsPersonalDomain(name) {
		return ""
	}
	return strings.TrimPrefix(name, personalMarker)
}

// PersonalView is a person's personal repository seen as a backlog: every
// open card in it, in board order, plus the cards finished on `day` — a done
// card is seen the day it was done and is gone the next morning, since no
// carry-over sweeps a personal board. A done card without a doneAt (finished
// before the field existed) is treated as done on an earlier day.
func PersonalView(b Board, login, day string) []Card {
	if login == "" {
		return nil
	}
	domain := PersonalDomain(login)
	var out []Card
	for _, c := range b.Cards {
		if c.Domain != domain {
			continue
		}
		if Complete(c.Stage, c.Progress) && c.DoneAt != day {
			continue
		}
		out = append(out, c)
	}
	return out
}

// PersonalReseed lists the finished recurrent cards of login's personal
// board whose next iteration is due on day — the cards a reader of the board
// seeds fresh copies of. A personal board has no carry-over, so the calendar
// turns its cycles: the per-sprint default means every day there (the copy is
// due the day after the card was finished), and a week/month cycle counts
// from the card's start day, as the team rule counts from its sprint. Never
// twice in one day — a card finished today, however late, rests until
// tomorrow — and never once a fresh copy (same title, later start) exists,
// so reading the board again reseeds nothing. A finished card without a day
// on record is never due: there is nothing to count from.
func PersonalReseed(b Board, login, day string) []Card {
	if login == "" {
		return nil
	}
	domain := PersonalDomain(login)
	var out []Card
	for _, c := range b.Cards {
		if c.Domain != domain || c.Stage != StageRecurrent || c.Progress < 100 {
			continue
		}
		if c.DoneAt == "" || c.DoneAt >= day || !personalCycleElapsed(c, day) {
			continue
		}
		if hasNewerPersonalCopy(b, c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// personalCycleElapsed reports whether a personal card's cycle has run its
// course by day, counted from the card's start: a day for the default, else
// the week/month the card asks for.
func personalCycleElapsed(c Card, day string) bool {
	switch c.Recurrence {
	case RecurrenceWeek:
		return c.StartDate != "" && AddDays(c.StartDate, 7) <= day
	case RecurrenceFortnight:
		return c.StartDate != "" && AddDays(c.StartDate, 14) <= day
	case RecurrenceMonth:
		return c.StartDate != "" && addMonths(c.StartDate, 1) <= day
	case RecurrenceQuarter:
		return c.StartDate != "" && addMonths(c.StartDate, 3) <= day
	default:
		return true // every day: the day after it was finished, checked by the caller
	}
}

// hasNewerPersonalCopy is the reseed guard: a recurrent card of the same
// title started later in the same domain is the copy already seeded.
func hasNewerPersonalCopy(b Board, c Card) bool {
	for _, o := range b.Cards {
		if o.ItemID != c.ItemID && o.Domain == c.Domain && o.Title == c.Title &&
			o.Stage == StageRecurrent && o.StartDate > c.StartDate {
			return true
		}
	}
	return false
}
