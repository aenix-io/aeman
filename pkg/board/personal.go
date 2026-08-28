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
