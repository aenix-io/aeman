package board

// A repeating card is not always a process's. A team keeps its own work that
// comes round — the weekly report, the monthly invoice — as a plain recurrent
// card, reseeded by carry-over rather than filed by a sweep. The weeks it
// will land in are as spoken for as a process turn's, and a board that plans
// weeks ahead has to say so.

// UpcomingRecurrences is every week, of the `weeks` beginning at `from`, in
// which a recurrent card comes round again and no copy of it stands yet.
//
// The card's OWN week is never among them: the card is standing in it. Nor is
// a week already holding a copy — reseeding is what these weeks foretell, and
// a week that has had it is a card. Copies are known by what reseeding itself
// matches on: the title, within the team (Service.CarryOver's own dedup).
//
// Nothing is projected without a calendar to project from: a per-sprint
// recurrence turns with the sprint, which is not a date. A process TURN is
// projected by its task's calendar instead (UpcomingTurns) — answering here
// as well would draw every one of them twice.
func UpcomingRecurrences(b Board, c Card, from string, weeks int) []string {
	if c.Stage != StageRecurrent || c.Task != "" || c.Week == "" || weeks <= 0 {
		return nil
	}
	if c.Recurrence == RecurrenceSprint || !ValidRecurrence(c.Recurrence) {
		return nil
	}
	// The calendar is anchored where the work began, and on the card's own
	// week when nothing says otherwise.
	anchor := c.StartDate
	if anchor == "" {
		anchor = c.Week
	}
	taken := map[string]bool{c.Week: true}
	for _, o := range b.Cards {
		if o.ItemID == c.ItemID || o.Title != c.Title || o.Team != c.Team ||
			o.Stage != StageRecurrent {
			continue
		}
		// A copy holds its week however it came to stand in one. Reseeding
		// gives its copy a START and a day and no week at all (CarryOver), so
		// asking only for a stored week matched none of the copies the board
		// actually makes: the projection drew a ghost in the very week the
		// real card was already standing in.
		if week := weekOf(o); week != "" {
			taken[week] = true
		}
	}
	out := []string{}
	for i := 0; i < weeks; i++ {
		week := AddDays(MondayOf(from), 7*i)
		if !taken[week] && DueInWeek(c.Recurrence, anchor, week) {
			out = append(out, week)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// weekOf is the week a card stands in: the one it was placed in, or — for a
// card that was never placed but has dates — the week of its start. A copy
// made by a sprint turn-over is of the second kind, so a rule that reads only
// the stored field does not see it at all.
func weekOf(c Card) string {
	if c.Week != "" {
		return c.Week
	}
	if c.StartDate != "" {
		return MondayOf(c.StartDate)
	}
	return ""
}
