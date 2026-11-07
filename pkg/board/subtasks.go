package board

// Subtasks: a TASK may group child tasks one level deep. A child carries its
// parent's item id in Parent; it works like any card (own description, notes,
// log, stage, progress) but rides with its parent through Carry Over and is
// rendered nested under it.
//
// `parent` is also the hierarchy's link (hierarchy.go): a task under an epic
// carries one too, and is NOT a subtask. Everything here answers for tasks
// under tasks and stops at a planning card — a goal's children are not its
// subtasks, do not drive its progress, and do not follow it between
// repositories.

// Children returns the subtasks of a task, in board order: the cards whose
// parent it is. A planning card has none — what sits under it is the
// hierarchy's (ChildrenOf), and never its subtasks.
func Children(b Board, itemID string) []Card {
	if itemID == "" {
		return nil
	}
	if p, ok := Find(b, itemID); ok && IsPlanning(p) {
		return nil
	}
	var out []Card
	for _, c := range b.Cards {
		if c.Parent == itemID && !IsPlanning(c) {
			out = append(out, c)
		}
	}
	return out
}

// Followers lists the cards whose FILE moves with this one: its subtasks
// and its review card, and THEIRS — the store's cascade recurses, so a
// walk that stopped at the first ring went blind exactly where a
// child hides its own review card. What follows a card
// cannot be left behind by a move: neither its file nor the rules that
// read it.
//
// A hierarchy link is not one of them: a task under an epic lives where its
// own team lives (H6), so nothing follows a planning card.
//
// Cost: one pass over the board's cards per ring of the walk, and the
// rings are shallow (a card, its subtasks and its review card). A caller
// that asks it for many cards pays that per card; at board scale it is
// nothing, and a card index would be the
// answer if a board ever made it one.
func Followers(b Board, itemID string) []Card {
	// Nothing follows nobody. Every root card has an empty parent and an
	// empty reviewOf, so an unset id used to adopt the whole board — and
	// this is exported for tools that may well pass one.
	if itemID == "" {
		return nil
	}
	if p, ok := Find(b, itemID); ok && IsPlanning(p) {
		return nil
	}
	var out []Card
	seen := map[string]bool{itemID: true}
	for frontier := []string{itemID}; len(frontier) > 0; {
		var next []string
		for _, id := range frontier {
			for _, c := range b.Cards {
				// A card with no id of its own would join the frontier as
				// "" and adopt every root card on the next pass.
				if c.ItemID == "" || seen[c.ItemID] || (c.Parent != id && c.ReviewOf != id) {
					continue
				}
				seen[c.ItemID] = true
				out = append(out, c)
				next = append(next, c.ItemID)
			}
		}
		frontier = next
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

// OpenChildren reports whether any of a task's subtasks is still unfinished.
func OpenChildren(b Board, itemID string) bool {
	for _, c := range Children(b, itemID) {
		if !Complete(c.Stage, c.Progress) {
			return true
		}
	}
	return false
}
