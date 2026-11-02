package board

// A team keeps work that comes round — the weekly report, the monthly
// invoice — as a recurrent card, reseeded by carry-over. The weeks it will
// land in are spoken for, and a board that plans weeks ahead has to say so.

// DueInWeek reports whether a recurrence anchored at `start` comes due in
// the week beginning on `week`: the first occurrence falling after the day
// before it lands on or before the week's end. It is the one calendar
// question every projection asks, so the week the board draws a recurrence
// in is the week the reseed will put it in.
func DueInWeek(recurrence, start, week string) bool {
	due := NextAfter(recurrence, start, AddDays(week, -1))
	return due != "" && due <= AddDays(week, 6)
}

// UpcomingRecurrences is every week, of the `weeks` beginning at `from`, in
// which a recurrent card comes round again and no copy of it stands yet.
//
// The card's OWN week is never among them: the card is standing in it. Nor is
// a week already holding a copy — reseeding is what these weeks foretell, and
// a week that has had it is a card. Copies are known by what reseeding itself
// matches on: the title, within the team (Service.CarryOver's own dedup).
//
// Nothing is projected without a calendar to project from: a per-sprint
// recurrence turns with the sprint, which is not a date.
func UpcomingRecurrences(b Board, c Card, from string, weeks int) []string {
	if c.Stage != StageRecurrent || c.Week == "" || weeks <= 0 {
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
