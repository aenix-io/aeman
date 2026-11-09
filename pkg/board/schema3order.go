package board

import "sort"

// Schema3BacklogKey is a backlog card's place in the list as schema 3 read
// it (K14, retired by docs/design/ordering.md): kept for the schema-4
// migration, which preserves exactly this order, and for nothing else.
//
//   - Review: a review nobody has taken (K10) stands first, before every
//     task, in its own group.
//   - Loose: the card has no plan item above it; the loose cards come after
//     every planned one.
//   - Path: the places in the board's order from the root of the card's tree
//     down to the card itself (K14) — the Strategy board's draw order, which lists a
//     parent's children in board order. The first step is the root's level
//     ahead of its place, as the Strategy board draws its roots level by level (goals,
//     then initiatives with no goal, then epics with no initiative). A loose
//     card's path is its own place alone; a review's is the reviewed task's,
//     so the reviews read in their tasks' Strategy order.
type Schema3BacklogKey struct {
	Review bool
	Loose  bool
	Path   []int
}

// Less reports whether k stands before o.
func (k Schema3BacklogKey) Less(o Schema3BacklogKey) bool {
	if k.Review != o.Review {
		return k.Review
	}
	if k.Loose != o.Loose {
		return !k.Loose
	}
	for i := 0; i < len(k.Path) && i < len(o.Path); i++ {
		if k.Path[i] != o.Path[i] {
			return k.Path[i] < o.Path[i]
		}
	}
	return len(k.Path) < len(o.Path)
}

// Schema3BacklogKey is the card's place in its backlog (Schema3BacklogKey).
func (ix *Index) Schema3BacklogKey(c Card) Schema3BacklogKey {
	if NeedsReviewer(c) {
		k := Schema3BacklogKey{Loose: true, Path: []int{ix.pos[c.ItemID]}}
		// Placed by the card it reviews — a review of a review (a direct
		// writer's) by the review it reviews — as the client reads it off
		// status.lineage.
		if task, ok := ix.Find(c.ReviewOf); ok {
			k = ix.reviewedKey(task)
		}
		k.Review = true
		return k
	}
	return ix.taskKey(c)
}

// reviewedKey is where the task a review stands by is placed for that
// review: its PLANNING chain and its own place — a subtask's parent task is
// not on the path, as the client reads it off the review card's
// status.lineage, which carries the planning chain alone.
func (ix *Index) reviewedKey(task Card) Schema3BacklogKey {
	var up []Card
	for _, a := range ix.Lineage(task) {
		if IsPlanning(a) {
			up = append(up, a)
		}
	}
	if len(up) == 0 {
		return Schema3BacklogKey{Loose: true, Path: []int{ix.pos[task.ItemID]}}
	}
	path := []int{Level(TypeGoal) - Level(up[0].Type)}
	for _, a := range up {
		path = append(path, ix.pos[a.ItemID])
	}
	return Schema3BacklogKey{Path: append(path, ix.pos[task.ItemID])}
}

// taskKey is a task's place by its tree.
func (ix *Index) taskKey(c Card) Schema3BacklogKey {
	own := ix.pos[c.ItemID]
	up := ix.Lineage(c)
	planned := false
	for _, a := range up {
		if IsPlanning(a) {
			planned = true
			break
		}
	}
	if !planned {
		return Schema3BacklogKey{Loose: true, Path: []int{own}}
	}
	path := make([]int, 0, len(up)+2)
	path = append(path, Level(TypeGoal)-Level(up[0].Type))
	for _, a := range up {
		path = append(path, ix.pos[a.ItemID])
	}
	return Schema3BacklogKey{Path: append(path, own)}
}

// Schema3BacklogOrder sorts a backlog's cards the way it is read: the reviews
// nobody has taken (K10) first, in their tasks' Strategy order, then the
// tasks in the STRATEGY BOARD's order across the whole hierarchy (K14), the tasks
// with no plan item above them after — see Schema3BacklogKey.
//
// One order, decided by the tree: dragging a task in the backlog moves it
// among its siblings under the same parent, which
// moves it on the Strategy board too, and a task's place is never something the
// backlog keeps apart from the Strategy board's. The board's order (rank, then id)
// is where the siblings' order comes from, as on every board.
func Schema3BacklogOrder(ix *Index, cards []Card) []Card {
	out := append([]Card(nil), cards...)
	keys := make(map[string]Schema3BacklogKey, len(out))
	for _, c := range out {
		keys[c.ItemID] = ix.Schema3BacklogKey(c)
	}
	sort.SliceStable(out, func(i, j int) bool { return keys[out[i].ItemID].Less(keys[out[j].ItemID]) })
	return out
}
