package board

// Person is what the roster says about one login: for now, the points a
// week they get through, when a lead has set it (0 = nobody has said). It is
// the primary repository's users/<login>.yaml.
type Person struct {
	// Capacity is the roster's number — points a week — or 0 for "nobody
	// has said" (see CapacityOfPerson).
	Capacity int `json:"capacity,omitempty"`
}

// CapacityOfPerson is how many points a week a person gets through: the
// roster's number, and only that. 0 means nobody has set one, which a client
// draws as the load alone rather than as a limit of none.
//
// The board deliberately does NOT derive a number from its own record. It
// can be derived — the median of the points closed in each of the last four
// complete weeks the person closed anything in — and that arithmetic is
// written down in the derive-capacity skill, for a lead to run and write the
// answer back like any other judgement. It is not run here because a number
// the board prints beside a name reads as a fact, and this one would not be
// one: doneAt is only written from the day a board starts keeping it, so the
// four-week window is mostly empty of RECORDS while being full of WORK, and
// the median of the little that is there lands far under the truth — and
// lands quietly, since nothing on screen says the window was half empty.
// One number somebody stands behind beats four weeks of arithmetic over a
// record that does not go back four weeks.
func CapacityOfPerson(b Board, login string) int {
	return b.People[login].Capacity
}
