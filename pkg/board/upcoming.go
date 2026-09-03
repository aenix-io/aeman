package board

// What a process is going to file is knowable before it files it: a task's
// recurrence is a calendar, and the weeks ahead can be read off it. A board
// that plans several weeks out has to show that work — a week already spoken
// for by a process is not a week the team is free in.

// DueInWeek reports whether a process task comes due in the week beginning on
// `week`: the first turn falling after the day before it lands on or before
// the week's end. It is the calendar question spawnIfDue asks before filing
// anything, and asking it the same way is what keeps a projection honest —
// the week the board draws a turn in is the week the sweep will file it in.
func DueInWeek(recurrence, start, week string) bool {
	due := NextAfter(recurrence, start, AddDays(week, -1))
	return due != "" && due <= AddDays(week, 6)
}

// UpcomingTurns is every week, of the `weeks` beginning at `from`, in which a
// task comes due and whose OCCURRENCE has no turn yet.
//
// An occurrence whose turn is already filed is left out: that turn is a card,
// and the board draws cards. What is left is what is coming — the turns
// nobody can act on yet but everybody has to plan around.
//
// The question is asked of the occurrence and not of the week, because a turn
// can be MOVED inside its own cycle (CycleWindow). Asking about the week left
// a ghost standing where a moved turn came from — the same work drawn twice,
// once as a card where it now is and once as a projection where it was.
//
// A task with no recurrence has no calendar and nothing is coming; a task
// whose process is paused files nothing, which the caller knows and this
// does not (it is given the task, not the process).
func UpcomingTurns(b Board, task Card, from string, weeks int) []string {
	if task.Recurrence == "" || weeks <= 0 {
		return nil
	}
	out := []string{}
	for i := 0; i < weeks; i++ {
		week := AddDays(MondayOf(from), 7*i)
		if DueInWeek(task.Recurrence, task.StartDate, week) && !occurrenceHasTurn(b, task, week) {
			out = append(out, week)
		}
	}
	return out
}
