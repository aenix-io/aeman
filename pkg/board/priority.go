package board

// PriorityKey is how much a card matters now, said by a person — P1, P2, P3
// or nothing (docs/design/backlog.md, K15). Any card carries one: a task, a
// goal, an initiative, an epic. What a card counts under is its EFFECTIVE
// priority — its own, else the nearest ancestor's (EffectivePriority) — the
// way a card counts under its effective team (O3). Nothing clears it on its
// own: taking a task into work, finishing it or reopening it keeps it.
type PriorityKey string

const (
	PriorityNone PriorityKey = ""
	PriorityP1   PriorityKey = "P1"
	PriorityP2   PriorityKey = "P2"
	PriorityP3   PriorityKey = "P3"
)

// ValidPriority reports a value a card may carry: P1, P2, P3, or none.
func ValidPriority(p string) bool {
	switch PriorityKey(p) {
	case PriorityNone, PriorityP1, PriorityP2, PriorityP3:
		return true
	}
	return false
}

// priorityRank orders the priorities for a backlog: P1 first, none last.
func priorityRank(p PriorityKey) int {
	switch p {
	case PriorityP1:
		return 1
	case PriorityP2:
		return 2
	case PriorityP3:
		return 3
	}
	return 4
}

// EffectivePriority is the priority a card counts under (K15): its own when
// it has one, else the nearest ancestor's that has one, else none — and the
// card it came from (the zero Card when it is the card's own, or nobody's).
// A review card counts under its task's. Computed on reading, never written
// down: changing a plan item's priority changes every card under it that has
// none of its own.
func (ix *Index) EffectivePriority(c Card) (PriorityKey, Card) {
	if c.Priority != PriorityNone {
		return c.Priority, Card{}
	}
	if c.ReviewOf != "" {
		if task, ok := ix.Find(c.ReviewOf); ok && task.ReviewOf == "" {
			if task.Priority != PriorityNone {
				return task.Priority, task
			}
			return ix.EffectivePriority(task)
		}
	}
	up := ix.Lineage(c)
	for i := len(up) - 1; i >= 0; i-- {
		if up[i].Priority != PriorityNone {
			return up[i].Priority, up[i]
		}
	}
	return PriorityNone, Card{}
}
