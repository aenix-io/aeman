package board

// Index is a board's cards by id and by parent, built ONCE for a reading
// that asks the hierarchy's questions of every card — a listing, a watch
// projection — so each answer costs a lookup and not a walk over the board.
// The free functions (Find, Lineage, LeafTasks, OnBacklog, …) give the
// same answers for one card at a time; a loop over a board of three
// thousand cards asking them of each card was three thousand walks of the
// board per question, and the listing paid it for every watcher. The index
// is a snapshot: a card added to the board after it was built is not in it.
type Index struct {
	cards    []Card // board order, state cards included
	byID     map[string]Card
	children map[string][]Card // parent id → the cards under it, board order
	planning map[string]bool
	// reviewedBy is the person on the open review card of an original, by
	// the original's id: the first such card in board order, as the
	// resource's own walk found it.
	reviewedBy map[string]string
	// customers is the board's customer roster by id: a card may name an id
	// the roster no longer holds (a delete that raced an edit, a replayed
	// write), and such an id is nobody's (EffectiveCustomers).
	customers map[string]bool
}

// NewIndex reads the board once.
func NewIndex(b Board) *Index {
	ix := &Index{
		cards:      b.Cards,
		byID:       make(map[string]Card, len(b.Cards)),
		children:   map[string][]Card{},
		planning:   map[string]bool{},
		reviewedBy: map[string]string{},
		customers:  make(map[string]bool, len(b.Customers)),
	}
	for _, cu := range b.Customers {
		ix.customers[cu.ID] = true
	}
	for _, c := range b.Cards {
		ix.byID[c.ItemID] = c
		if c.Parent != "" {
			ix.children[c.Parent] = append(ix.children[c.Parent], c)
		}
		if IsPlanning(c) {
			ix.planning[c.ItemID] = true
		}
		if c.ReviewOf != "" && len(c.Assignees) > 0 && !Complete(c.Stage, c.Progress) {
			if _, seen := ix.reviewedBy[c.ReviewOf]; !seen {
				ix.reviewedBy[c.ReviewOf] = c.Assignees[0]
			}
		}
	}
	return ix
}

// Find is a card by id.
func (ix *Index) Find(id string) (Card, bool) {
	if id == "" {
		return Card{}, false
	}
	c, ok := ix.byID[id]
	return c, ok
}

// ParentOf is the card a card's `parent` names, if it is on the board.
func (ix *Index) ParentOf(c Card) (Card, bool) { return ix.Find(c.Parent) }

// IsSubtask is board.IsSubtask: a task whose parent is a task, or is not
// here at all (which a read board never holds: the reading drops it).
func (ix *Index) IsSubtask(c Card) bool { return IsSubtaskIn(c, ix.planning) }

// Lineage is board.Lineage: every ancestor, root first, bounded.
func (ix *Index) Lineage(c Card) []Card {
	const maxHops = 6 // goal, initiative, epic, task, subtask — and one to spare
	var up []Card
	seen := map[string]bool{c.ItemID: true}
	for cur := c; cur.Parent != "" && len(up) < maxHops; {
		p, ok := ix.Find(cur.Parent)
		if !ok || seen[p.ItemID] {
			break
		}
		seen[p.ItemID] = true
		up = append(up, p)
		cur = p
	}
	// Collected bottom-up; the breadcrumb reads top-down.
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up
}

// EffectiveTeam is the team a card counts under (O3): its own, else the
// nearest ancestor's that has one. A task's own team always wins over an
// inherited one; nothing is written down the tree. A project card has none
// (H4), whatever an old file's `team:` says.
func (ix *Index) EffectiveTeam(c Card) string {
	if c.Scope != "" {
		return ""
	}
	if c.Team != "" {
		return c.Team
	}
	up := ix.Lineage(c)
	for i := len(up) - 1; i >= 0; i-- {
		if up[i].Team != "" {
			return up[i].Team
		}
	}
	return ""
}

// EffectiveCustomers is the customers a card is for (O4): every ancestor's
// and its own, the ancestors' first, each once — a lower card adds to them
// and never overwrites them. A review card is for its task's (O6), and a
// project card for none (H4). An id the board's roster does not hold is left
// out: it names nobody (O5).
func (ix *Index) EffectiveCustomers(c Card) []string {
	if c.Scope != "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(ids []string) {
		for _, id := range ids {
			if id != "" && !seen[id] && ix.customers[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	if c.ReviewOf != "" {
		if task, ok := ix.Find(c.ReviewOf); ok && task.ReviewOf == "" {
			add(ix.EffectiveCustomers(task))
		}
	}
	for _, a := range ix.Lineage(c) {
		add(a.Customers)
	}
	add(c.Customers)
	return out
}

// OnRoster is the ids of the given ones the board's customer roster holds,
// in order: what a card's own customers read as (O5).
func (ix *Index) OnRoster(ids []string) []string {
	var out []string
	for _, id := range ids {
		if ix.customers[id] {
			out = append(out, id)
		}
	}
	return out
}

// HasPlanningAncestor is board.HasPlanningAncestor.
func (ix *Index) HasPlanningAncestor(c Card) bool {
	for _, a := range ix.Lineage(c) {
		if IsPlanning(a) {
			return true
		}
	}
	return false
}

// ChildrenOf is board.ChildrenOf: every card under the given one, planning
// cards and tasks alike, in board order.
func (ix *Index) ChildrenOf(id string) []Card {
	if id == "" {
		return nil
	}
	return ix.children[id]
}

// LeafTasks is board.LeafTasks: the tasks under a planning card through
// every level, a subtask and a review card not among them, in board order.
func (ix *Index) LeafTasks(id string) []Card {
	root, ok := ix.Find(id)
	if !ok || !IsPlanning(root) {
		return nil
	}
	subtree := map[string]bool{id: true}
	for frontier := []string{id}; len(frontier) > 0; {
		var next []string
		for _, pid := range frontier {
			for _, c := range ix.ChildrenOf(pid) {
				if IsPlanning(c) && !subtree[c.ItemID] {
					subtree[c.ItemID] = true
					next = append(next, c.ItemID)
				}
			}
		}
		frontier = next
	}
	var out []Card
	for _, c := range ix.cards {
		if !IsPlanning(c) && c.ReviewOf == "" && subtree[c.Parent] {
			out = append(out, c)
		}
	}
	return out
}

// HierarchyStatus is board.HierarchyStatus.
func (ix *Index) HierarchyStatus(c Card) (status string, done, total int) {
	if !IsPlanning(c) {
		return "", 0, 0
	}
	return hierarchyStatus(ix.LeafTasks(c.ItemID))
}

// OnBacklog is board.OnBacklog with the subtask question answered by the
// index.
func (ix *Index) OnBacklog(c Card) bool { return OnBacklog(c, ix.IsSubtask) }

// ReviewedBy is the person on the card's open review card, or "" — what
// `status.reviewedBy` carries.
func (ix *Index) ReviewedBy(c Card) string { return ix.reviewedBy[c.ItemID] }

// hierarchyStatus is the standing a set of leaf tasks gives their planning
// card (H5): NO ITEMS when there are none; DONE when every one is complete;
// IN PROGRESS when any of them is somebody's, has started — any progress,
// or held in review or locked — or is finished — a person named on a task
// is work being done before the first percent is typed; TO DO otherwise.
func hierarchyStatus(leaves []Card) (status string, done, total int) {
	working := false
	for _, t := range leaves {
		total++
		switch {
		case Complete(t.Stage, t.Progress):
			done++
		case len(t.Assignees) > 0 || IsInProgress(t) || t.Progress > 0 ||
			t.Stage == StageReview || t.Stage == StageLocked:
			// Somebody's, in progress — or plainly under way without either:
			// any progress at all, or held in review or locked (#287). A
			// project card has nobody on it by design (H4).
			working = true
		}
	}
	switch {
	case total == 0:
		return HierarchyNoItems, 0, 0
	case done == total:
		return HierarchyDone, done, total
	case done > 0 || working:
		return HierarchyInProgress, done, total
	}
	return HierarchyTodo, done, total
}
