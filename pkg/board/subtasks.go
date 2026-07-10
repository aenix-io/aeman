package board

// Subtasks: a card may group child cards one level deep. A child carries its
// parent's item id in Parent; it works like any card (own description, notes,
// log, stage, progress) but rides with its parent through Carry Over and is
// rendered nested under it.

// Children returns the subtasks of a card, in board order.
func Children(b Board, itemID string) []Card {
	var out []Card
	for _, c := range b.Cards {
		if c.Parent == itemID {
			out = append(out, c)
		}
	}
	return out
}

// DerivedProgress computes a parent's progress from its subtasks: the mean of
// each child's effective progress (a complete child counts as 100), scaled
// into the 0..90 band — the final done/100% is always a human's call.
func DerivedProgress(children []Card) int {
	if len(children) == 0 {
		return 0
	}
	sum := 0
	for _, c := range children {
		if Complete(c.Stage, c.Progress) {
			sum += 100
		} else {
			sum += c.Progress
		}
	}
	return sum * 90 / (len(children) * 100)
}

// OpenChildren reports whether any of a card's subtasks is still unfinished.
func OpenChildren(b Board, itemID string) bool {
	for _, c := range b.Cards {
		if c.Parent == itemID && !Complete(c.Stage, c.Progress) {
			return true
		}
	}
	return false
}
