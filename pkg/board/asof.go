package board

import "sort"

// TeamsPast names the teams for which a day is OVER: their sprint has moved
// on past it, so what they showed that day is a record and can be read from
// the history. A team still inside that sprint is not among them — the sprint
// lays itself out on its own day and the team works it from there, so the day
// is theirs to change. A team with no sprint at all answers for nothing.
func TeamsPast(live Board, day string) map[string]bool {
	out := map[string]bool{}
	for team, st := range live.SprintStates {
		if st.Current != "" && st.Current > day {
			out[team] = true
		}
	}
	return out
}

// settled reports that the day is over for this card: its team's sprint has
// moved past it. A PERSONAL card is never settled — it belongs to no team and
// no sprint, its day comes from its own dates, and it lives in its owner's
// own repository. Without this it would answer to the no-team group's sprint,
// which it shares nothing with but an empty team name, and somebody's
// personal column would freeze the day that group carried over.
func settled(c Card, past map[string]bool) bool {
	return past[c.Team] && !IsPersonalDomain(c.Domain)
}

// MergeAsOf is one day on one screen with two moments in it: the teams the
// day is over for (past) contribute what they held THEN, everyone else what
// they hold NOW. It returns the merged board and the ids of the cards that
// came from the past — a record, which a client must not offer to change.
//
// A card is placed by the team it belongs to in the board it comes from, so
// a card that has since moved between a past team and a live one is taken
// once, from the live side: its team is still working, and what they hold now
// is the truth about it.
func MergeAsOf(live, then Board, past map[string]bool) (Board, map[string]bool) {
	out := live
	out.Cards = make([]Card, 0, len(live.Cards))
	seen := make(map[string]bool, len(live.Cards))
	for _, c := range live.Cards {
		if settled(c, past) {
			continue
		}
		out.Cards = append(out.Cards, c)
		seen[c.ItemID] = true
	}
	fromPast := map[string]bool{}
	for _, c := range then.Cards {
		if !settled(c, past) || seen[c.ItemID] {
			continue
		}
		out.Cards = append(out.Cards, c)
		seen[c.ItemID] = true
		fromPast[c.ItemID] = true
	}
	// The pointers split the same way: the view rules compare a card's
	// sprint against its team's pointer, so a past team's cards need the
	// pointer they were placed by.
	out.SprintStates = make(map[string]SprintState, len(live.SprintStates))
	for team, st := range live.SprintStates {
		if past[team] {
			if was, ok := then.SprintStates[team]; ok {
				out.SprintStates[team] = was
				continue
			}
		}
		out.SprintStates[team] = st
	}
	sortCards(&out)
	return out, fromPast
}

// sortCards restores the board order the two halves were merged out of: by
// rank, the order every listing is served in.
func sortCards(b *Board) {
	sort.SliceStable(b.Cards, func(i, j int) bool { return b.Cards[i].Rank < b.Cards[j].Rank })
}
