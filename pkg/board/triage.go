package board

// The Triage board: the weekly plan seen several weeks ahead, with a limit
// on each week (docs/design/triage.md). A card in the backlog is a
// weekly-plan card placed in a week ahead; nothing here is stored beyond the
// card's own week and lane and the team's capacity.

// PlacedAhead reports whether the card is placed in a week ahead of today's.
// Such a card is on no day board until its Monday (B1): that is what makes
// the backlog a regulator rather than a list.
func PlacedAhead(c Card, today string) bool {
	return c.Week != "" && c.Week > MondayOf(today)
}

// NeedsTriage reports whether nobody has said WHEN the card's work is due: an
// open card of its own (no parent, no original) with no week (B5). The week
// is the whole of the decision — a card on today's board was put there by the
// day's planning, not by a week's, and until someone gives it a week it is
// work of unknown time. That is the pile the strip exists to show, however
// large it is at first.
//
// A card SENT TO REVIEW is not in that pile either. Its work is done and it
// is waiting on a reviewer, not on a week: asking the strip's reader for one
// asks them to decide something nobody is waiting on them to decide.
func NeedsTriage(_ Board, c Card, _ string) bool {
	if IsStateTitle(c.Title) || IsPersonalDomain(c.Domain) {
		return false
	}
	if c.Parent != "" || c.ReviewOf != "" || c.Week != "" {
		return false
	}
	// A card PARKED on a list is out of the strip. The strip is the inbox —
	// work that arrived and nobody has looked at — and putting a card on a
	// list is the act of looking: somebody read it and said "not now, and
	// here is where it waits". Leaving it in both would make the queue
	// unreadable, since a shelf may be long and a queue must be short.
	if InBacklog(c) {
		return false
	}
	if c.Stage == StageReview {
		return false
	}
	return !Complete(c.Stage, c.Progress)
}

// InWeek reports whether the card is the given week's own work: any of the
// weeks it covers (its own through the week its end date reaches), or — in
// the CURRENT week — a DEBT owed in an earlier one, which stands beside that
// week's work without leaving the week it was owed in.
//
// It is what a week's column holds on the Triage board, and what the Team
// board's grid carries all week: a card placed in a week is not invisible
// until somebody gives it a day.
func InWeek(c Card, week, today string) bool {
	if c.Week == "" {
		return false
	}
	for _, w := range WeeksCovered(c) {
		if w == week {
			return true
		}
	}
	return week == MondayOf(today) && c.Week < week && Overdue(c, today)
}

// TriageWeekOf is the Monday of the column a card stands in on the Triage
// board — its week, and nothing else. A card with no week stands in no
// column: it is in the strip, waiting for someone to say when.
//
// A PARKED card stands in none either, week or no week. The two are exclusive
// and every door that gives a week takes the card off its shelf — but the
// storage is a git repository anything may write to, so a card can arrive
// carrying both. Drawn by its week it would stand in the grid AND in the
// drawer: the same work in two places, counted twice against the week. The
// shelf wins, because the drawer is where such a card can be dealt with.
func TriageWeekOf(_ Board, c Card, _ string) string {
	if InBacklog(c) {
		return ""
	}
	if c.Week != "" {
		return c.Week
	}
	// A REVIEW card has no week of its own — the week belongs to the card it
	// reviews — and yet it is work in the reviewer's hands and counts in
	// their load. Drawn by nothing, it stood on no board at all once its
	// dates ran out: not a day board (they are past), not the strip (nobody
	// is waiting on a week for it), not the grid (no week) — while still
	// weighing on the number beside its reviewer's name. So it stands in the
	// week its own DATES fall in, and one whose week has gone arrives in the
	// current column by the same debt rule as everything else. Whether it is
	// DRAWN is the board's own question (the reviews toggle); where it would
	// stand is this one.
	if c.ReviewOf != "" {
		if c.StartDate != "" {
			return MondayOf(c.StartDate)
		}
		return MondayOf(c.Day)
	}
	return ""
}

// WeeksCovered is every week a card occupies on the Triage board: the week
// it was placed in, through the week its end date reaches. Stretching a card
// over three weeks says the work takes three weeks, and each of them counts
// it against what the team can do — a stretched card is not one week of work
// filed early.
//
// A card with no week covers none: it is in the strip, waiting for someone to
// say when. An end date inside (or before) its own week reaches nowhere.
func WeeksCovered(c Card) []string {
	if c.Week == "" {
		return nil
	}
	out := []string{c.Week}
	last := MondayOf(c.Day)
	for w := AddDays(c.Week, 7); last != "" && w <= last; w = AddDays(w, 7) {
		out = append(out, w)
	}
	return out
}
