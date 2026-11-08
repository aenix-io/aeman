package board

// A visitor's board is the union of the domains they can read (G17). The
// server reads every domain and evaluates every rule on the whole board —
// sprint pointers, carry-over, the sweep — then hands each visitor the part
// they may see. Filtering is therefore a projection of the full board, never
// a different board: an unreadable domain is absent, not empty.

// IsStateTitle reports whether a title marks a roster stub — the hidden cards
// NewBoard splits out of the card rows.
func IsStateTitle(title string) bool {
	return title == SprintStateTitle
}

// Visible is the board as a visitor who can read only some domains sees it.
// Cards and teams of an unreadable domain are absent. A card whose team is unreadable stays, under its team name — the
// team's order slot and sprint pointer do not. An entry with no recorded
// domain belongs to the primary. The input is not modified.
func Visible(b Board, primary string, readable func(domain string) bool) Board {
	can := func(d string) bool {
		if d == "" {
			d = primary
		}
		return readable(d)
	}
	entry := func(id string) bool { return can(b.Domains[id]) }

	v := b
	v.Cards = filterCards(b.Cards, func(c Card) bool { return can(c.Domain) })
	// What a visitor cannot read still locks what they can (N4): the whole
	// board's blocker count is recorded for every kept card that has any,
	// so the resource can count the ones it cannot name.
	v.BlockerTotals = nil
	if len(v.Cards) < len(b.Cards) {
		ix := NewIndex(b)
		for _, c := range v.Cards {
			if n := len(ix.Blockers(c)); n > 0 {
				if v.BlockerTotals == nil {
					v.BlockerTotals = map[string]int{}
				}
				v.BlockerTotals[c.ItemID] = n
			}
		}
	}

	v.TeamOrder = nil
	v.SprintStates = map[string]SprintState{}
	for _, team := range b.TeamOrder {
		st, ok := b.SprintStates[team]
		if ok && !entry(st.ItemID) {
			continue
		}
		v.TeamOrder = append(v.TeamOrder, team)
	}
	for team, st := range b.SprintStates {
		if entry(st.ItemID) {
			v.SprintStates[team] = st
		}
	}

	if b.Domains != nil {
		v.Domains = map[string]string{}
		for id, d := range b.Domains {
			if can(d) {
				v.Domains[id] = d
			}
		}
	}
	return v
}

func filterCards(cards []Card, keep func(Card) bool) []Card {
	if cards == nil {
		return nil
	}
	out := make([]Card, 0, len(cards))
	for _, c := range cards {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// boardResolver answers DomainOf's questions from a board snapshot: cards by
// their own Domain, teams by the roster's.
type boardResolver struct {
	b       Board
	primary string
}

// Resolver is a DomainResolver over a board snapshot, for deciding where a
// card WOULD live after a change — the write check of a move needs the
// destination before anything is written.
func Resolver(b Board, primary string) DomainResolver {
	return boardResolver{b: b, primary: primary}
}

func (r boardResolver) or(d string) string {
	if d == "" {
		return r.primary
	}
	return d
}

func (r boardResolver) CardDomain(id string) (string, bool) {
	for _, c := range r.b.Cards {
		if c.ItemID == id {
			return r.or(c.Domain), true
		}
	}
	return "", false
}

func (r boardResolver) TeamDomain(name string) (string, bool) {
	st, ok := r.b.SprintStates[name]
	if !ok {
		return "", false
	}
	return r.or(r.b.Domains[st.ItemID]), true
}
