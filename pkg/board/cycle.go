package board

// A process task's calendar divides the weeks into OCCURRENCES: one per due
// date, running from the week that date falls in up to the week before the
// next one does. An occurrence is the unit a turn belongs to — the turn IS
// that occurrence's work, wherever inside it somebody has moved the card.
//
// Asking about occurrences rather than weeks is what keeps a moved turn from
// being counted twice: the projection asks whether the OCCURRENCE has a turn,
// not whether that particular week does.

// CycleWindow is the occurrence containing `week`: the first and last week a
// turn of it may stand in, both Mondays and both inclusive. Empty strings
// when the task has no calendar to reckon with — no recurrence, a per-sprint
// one (sprints are not dates), or an anchor nobody can read.
//
// It is what bounds the grip on the Triage board: a turn moves inside its own
// occurrence, because a turn carried past the next due date would stand where
// the next turn belongs and the two would read as one process running twice.
func CycleWindow(task Card, week string) (string, string) {
	from := occurrenceStart(task, week)
	if from == "" {
		return "", ""
	}
	next := NextAfter(task.Recurrence, task.StartDate, AddDays(from, 6))
	if next == "" {
		return from, from
	}
	last := AddDays(MondayOf(next), -7)
	if last < from {
		last = from
	}
	return from, last
}

// occurrenceStart is the week the occurrence containing `week` came due in:
// the latest due week at or before it. "" when the task has no calendar, or
// when `week` is before the task begins — a turn cannot belong to an
// occurrence that has not happened.
func occurrenceStart(task Card, week string) string {
	if task.Recurrence == "" || task.Recurrence == RecurrenceSprint || week == "" {
		return ""
	}
	monday := MondayOf(week)
	if monday == "" {
		return ""
	}
	// The first due date at or after the week's own start, walked back one
	// occurrence when it lands past it. Reckoning forward from the anchor
	// would take a step per cycle since it, which for a weekly task anchored
	// years ago is a walk nobody needs: NextAfter jumps straight there.
	due := NextAfter(task.Recurrence, task.StartDate, AddDays(monday, -1))
	if due == "" {
		return ""
	}
	if MondayOf(due) <= monday {
		return MondayOf(due)
	}
	// `week` is inside the occurrence BEFORE that one — unless there is none,
	// which means the task had not begun yet.
	prev := previousDue(task, MondayOf(due))
	if prev == "" {
		return ""
	}
	return prev
}

// previousDue is the due week before `week`, found by stepping back through
// the calendar a cycle at a time until NextAfter lands on the week itself.
// The step is bounded: a cycle is at least a week, so the search cannot run
// past the task's own start.
func previousDue(task Card, week string) string {
	for back := week; back > MondayOf(task.StartDate); {
		back = AddDays(back, -7)
		due := NextAfter(task.Recurrence, task.StartDate, AddDays(back, -1))
		if due == "" {
			return ""
		}
		if MondayOf(due) <= back {
			return MondayOf(due)
		}
	}
	return ""
}

// occurrenceHasTurn reports whether any of the task's turns stands inside the
// occurrence that `week` belongs to.
func occurrenceHasTurn(b Board, task Card, week string) bool {
	from, to := CycleWindow(task, week)
	if from == "" {
		return false
	}
	for _, it := range Iterations(b, task.ItemID) {
		if it.Week >= from && it.Week <= to {
			return true
		}
	}
	return false
}
