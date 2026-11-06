package board

// The team a card STANDS UNDER on the day boards and the backlogs
// (docs/design/ownership.md, O13): its EFFECTIVE team (O3) — its own, else
// the nearest ancestor's — so a task with no team of its own under an epic
// of portal is portal's work on the Team board, in the Backlog, in every
// team filter and in portal's sprint, while the epic's team is never
// written down onto it. A review card stands under its task's; a project
// card under none (H4); a plan item keeps its own. The no-team group is
// the cards that stand under no team.
//
// The OWN team stays what storage, the domain rule (DomainOf) and every
// write read: placement is computed, as the effective team is. A board read
// for a visitor is their projection (Visible), so an ancestor they cannot
// read gives nothing — the task stands in the no-team group for them, as
// its lineage stops there.

// PlacedTeam is the team a card stands under (O13).
func (ix *Index) PlacedTeam(c Card) string {
	if IsPlanning(c) {
		return c.Team
	}
	return ix.EffectiveTeam(c)
}

// PlacedTeams is every card's placed team, by id: one index for a reading
// that places many cards.
func PlacedTeams(b Board) map[string]string {
	ix := NewIndex(b)
	out := make(map[string]string, len(b.Cards))
	for _, c := range b.Cards {
		out[c.ItemID] = ix.PlacedTeam(c)
	}
	return out
}

// PlacedTeam is one card's placed team on a board.
func PlacedTeam(b Board, c Card) string {
	if c.Team != "" && c.Scope == "" {
		return c.Team
	}
	return NewIndex(b).PlacedTeam(c)
}

// PlacedTeamAs is the team a card would stand under with `team` as its own:
// that team when it names one, else what it inherits.
func PlacedTeamAs(b Board, c Card, team string) string {
	if team != "" {
		return team
	}
	c.Team = ""
	return NewIndex(b).PlacedTeam(c)
}
