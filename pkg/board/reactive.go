package board

// PointsAWeekOf is a team's weekly capacity in POINTS: the number somebody
// set for it, and only that. 0 means nobody has said, which a client draws as
// the week's points alone rather than as a week with no room at all.
//
// It was arithmetic once: the capacities of the PEOPLE in the team, added up,
// with anyone who works across teams split between them in proportion to what
// they had closed in each over four weeks — and the total then cut by the
// share history said arrives unplanned. Three derivations stacked on one
// record, and on a real board that record is eleven days old, because doneAt
// is only written from the day a board starts keeping it. One busy week could
// hand a person's whole number to a team they had barely touched, and nothing
// on screen said the split rested on four closed cards.
//
// So the board stores what somebody decided and does no arithmetic. Working
// the number OUT is the derive-capacity skill's job, where the record's own
// thinness can be read out beside the answer — which a board printing a
// number beside a name could never do.
func PointsAWeekOf(b Board, team string) int {
	return b.SprintStates[team].Capacity.Points
}
