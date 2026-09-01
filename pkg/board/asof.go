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

// HasRecords reports that a view can be shown AS IT STOOD on a day: only the
// day boards can, because only they place a card by the day being looked at.
// The Project board lays every week out at once and the Process tab reads a
// structure — a day means nothing to either, so a record of one would be a
// claim about nothing. Mirrored by snapshotDay in web/src/viewquery.ts.
func HasRecords(view string) bool { return view == "me" || view == "team" }

// IsRecord reports that the day is over for this card: its team's sprint has
// moved past it, so what the card shows is a record and cannot be changed.
// Every door asks THIS — the merge that builds the day, and the guard that
// refuses a write made from it — or the two answer differently and a card is
// live on screen and refused by the server.
//
// A PERSONAL card is never a record: it belongs to no team and no sprint, its
// day comes from its own dates, and it lives in its owner's repository. Named
// by an empty team, it would otherwise answer to the no-team GROUP's sprint,
// which it shares nothing with but that empty name.
func IsRecord(c Card, past map[string]bool) bool {
	return past[c.Team] && !IsPersonalDomain(c.Domain)
}

// MergeAsOf is one day on one screen with two moments in it: the teams the
// day is over for (past) contribute what they held THEN, everyone else what
// they hold NOW. It returns the merged board and the ids of the cards that
// came from the past — a record, which a client must not offer to change.
//
// A card is judged by the copy that is CURRENT, whatever team the evening's
// copy names: a card moved between teams since, and a team renamed since,
// both name two different teams across the two boards, and asking each copy
// about its own team dropped the card from both halves — it stood on the
// board that evening and vanished from the record of it.
func MergeAsOf(live, then Board, past map[string]bool) (Board, map[string]bool) {
	// A copy, not the live board itself: what it shares stays shared (the
	// roster is today's on purpose — see docs/dates.md), but nothing here
	// writes through to the cached board the caller handed in.
	out := live
	out.Cards = make([]Card, 0, len(live.Cards))
	record := make(map[string]bool, len(live.Cards))
	seen := make(map[string]bool, len(live.Cards))
	for _, c := range live.Cards {
		if IsRecord(c, past) {
			record[c.ItemID] = true
			continue
		}
		out.Cards = append(out.Cards, c)
		seen[c.ItemID] = true
	}
	fromPast := map[string]bool{}
	for _, c := range then.Cards {
		if seen[c.ItemID] {
			continue
		}
		// The card is a record when TODAY says so; a card the live board no
		// longer holds at all is judged by the evening's own copy, which is
		// the only one there is.
		if !record[c.ItemID] && !IsRecord(c, past) {
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
	// A team the evening knew under another name keeps its pointer of that
	// evening too: its cards carry the old name, and the view places a card
	// by the pointer of the team it names.
	for team, st := range then.SprintStates {
		if _, ok := out.SprintStates[team]; !ok {
			out.SprintStates[team] = st
		}
	}
	sortCards(&out)
	return out, fromPast
}

// sortCards restores the board order the two halves were merged out of: by
// rank, the order every listing is served in.
func sortCards(b *Board) {
	sort.SliceStable(b.Cards, func(i, j int) bool { return b.Cards[i].Rank < b.Cards[j].Rank })
}
