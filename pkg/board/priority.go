package board

// PriorityKey is how much a card matters now, said by a person — P0 (the
// highest) to P4, or nothing (docs/design/backlog.md, K15), the scale Jira
// uses. Any card carries one: a task, a
// goal, an initiative, an epic. What a card counts under is its EFFECTIVE
// priority — its own, else the nearest ancestor's (EffectivePriority) — the
// way a card counts under its effective team (O3). Nothing clears it on its
// own: taking a task into work, finishing it or reopening it keeps it.
type PriorityKey string

const (
	PriorityNone PriorityKey = ""
	PriorityP0   PriorityKey = "P0"
	PriorityP1   PriorityKey = "P1"
	PriorityP2   PriorityKey = "P2"
	PriorityP3   PriorityKey = "P3"
	PriorityP4   PriorityKey = "P4"
)

// ValidPriority reports a value a card may carry: P0 to P4, or none.
func ValidPriority(p string) bool {
	switch PriorityKey(p) {
	case PriorityNone, PriorityP0, PriorityP1, PriorityP2, PriorityP3, PriorityP4:
		return true
	}
	return false
}

// priorityRank orders the priorities for a backlog: P0 first, none last.
func priorityRank(p PriorityKey) int {
	switch p {
	case PriorityP0:
		return 1
	case PriorityP1:
		return 2
	case PriorityP2:
		return 3
	case PriorityP3:
		return 4
	case PriorityP4:
		return 5
	}
	return 6
}

// EffectivePriority is the priority a card counts under (K15): its own when
// it has one, else the nearest ancestor's that has one, else none — and the
// card it came from (the zero Card when it is the card's own, or nobody's).
// A review card counts under its task's, and a review of a review (a direct
// writer's) under the review it reviews — so beside it in a backlog; a cycle
// of reviews ends under nobody. Computed on reading, never written down:
// changing a plan item's priority changes every card under it that has none
// of its own.
func (ix *Index) EffectivePriority(c Card) (PriorityKey, Card) {
	if c.Priority != PriorityNone {
		return c.Priority, Card{}
	}
	seen := map[string]bool{c.ItemID: true}
	for c.ReviewOf != "" {
		task, ok := ix.Find(c.ReviewOf)
		if !ok {
			break
		}
		if seen[task.ItemID] {
			return PriorityNone, Card{}
		}
		seen[task.ItemID] = true
		if task.Priority != PriorityNone {
			return task.Priority, task
		}
		c = task
	}
	up := ix.Lineage(c)
	for i := len(up) - 1; i >= 0; i-- {
		if up[i].Priority != PriorityNone {
			return up[i].Priority, up[i]
		}
	}
	return PriorityNone, Card{}
}
