package board

// Card dependencies (docs/design/dependencies.md, N1–N6): a card waits for
// other cards. The lock is derived on reading, never written; the targets
// are for the waiting work's customers too.

// Blocker is one unsatisfied dependency of a card: the card it waits for,
// and the card whose dependsOn names it — the card itself, or an ancestor
// whose wait it inherits.
type Blocker struct {
	Target Card
	Via    Card
}

// Done reports a dependency target as satisfied: a task complete, a plan
// item whose roll-up is done. A plan item with no tasks is not done.
func (ix *Index) Done(c Card) bool {
	if IsPlanning(c) {
		st, _, _ := ix.HierarchyStatus(c)
		return st == HierarchyDone
	}
	return Complete(c.Stage, c.Progress)
}

// Blockers are a card's unsatisfied dependencies: its own first, then each
// ancestor's, nearest first. A target the board does not hold is dangling
// and blocks nothing (N6).
func (ix *Index) Blockers(c Card) []Blocker {
	var out []Blocker
	holders := []Card{c}
	up := ix.Lineage(c)
	for i := len(up) - 1; i >= 0; i-- {
		holders = append(holders, up[i])
	}
	for _, h := range holders {
		for _, id := range h.DependsOn {
			t, ok := ix.Find(id)
			if !ok || ix.Done(t) {
				continue
			}
			out = append(out, Blocker{Target: t, Via: h})
		}
	}
	return out
}

// Locked reports a card with at least one blocker (N3). Independent of the
// manual `stage: locked`.
func (ix *Index) Locked(c Card) bool { return len(ix.Blockers(c)) > 0 }

// BlockersAs splits a card's blockers by what a reader may read: shown are
// the ones whose target, and every card from this one up to the card that
// names it, the reader can read; hidden counts the rest (N4).
func (ix *Index) BlockersAs(c Card, can func(Card) bool) (shown []Blocker, hidden int) {
	up := ix.Lineage(c) // root first
	pathReadable := func(via Card) bool {
		if via.ItemID == c.ItemID {
			return true
		}
		for i := len(up) - 1; i >= 0; i-- {
			if !can(up[i]) {
				return false
			}
			if up[i].ItemID == via.ItemID {
				return true
			}
		}
		return false
	}
	for _, b := range ix.Blockers(c) {
		if can(b.Target) && pathReadable(b.Via) {
			shown = append(shown, b)
		} else {
			hidden++
		}
	}
	return shown, hidden
}

// Blocks are the cards whose own dependsOn names this one, board order.
func (ix *Index) Blocks(c Card) []Card { return ix.dependents[c.ItemID] }

// DependentCustomer is a customer a card is for only because work that
// waits for it is (N5), with the nearest such card.
type DependentCustomer struct {
	Customer string
	From     Card
}

// effectiveCustomers is EffectiveCustomers with the cards already walked:
// the dependency inheritance is transitive, and each card passes its
// customers on once, so a cycle a direct writer left ends.
func (ix *Index) effectiveCustomers(c Card, visited map[string]bool) ([]string, []DependentCustomer) {
	var out []string
	var from []DependentCustomer
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
		if task, ok := ix.Find(c.ReviewOf); ok && task.ReviewOf == "" && !visited[task.ItemID] {
			visited[task.ItemID] = true
			ids, _ := ix.effectiveCustomers(task, visited)
			add(ids)
		}
	}
	up := ix.Lineage(c)
	for _, a := range up {
		add(a.Customers)
	}
	add(c.Customers)
	// What waits for this card, or for an ancestor, is for its customers
	// too — after the card's own line, so O4's order stands.
	holders := append(append([]Card(nil), up...), c)
	for _, h := range holders {
		for _, d := range ix.dependents[h.ItemID] {
			if visited[d.ItemID] {
				continue
			}
			visited[d.ItemID] = true
			ids, _ := ix.effectiveCustomers(d, visited)
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
					from = append(from, DependentCustomer{Customer: id, From: d})
				}
			}
		}
	}
	return out, from
}

// DependentCustomers are the customers a card is for only through the work
// that waits for it (N5), each with the nearest waiting card.
func (ix *Index) DependentCustomers(c Card) []DependentCustomer {
	_, from := ix.effectiveCustomers(c, map[string]bool{c.ItemID: true})
	return from
}

// DependencyTargetOK reports whether card c may wait for the card with id
// target (N1): it must be on the board, not c, neither a review card, and
// neither an ancestor nor a descendant of the other.
func (ix *Index) DependencyTargetOK(c Card, target string) bool {
	t, ok := ix.Find(target)
	if !ok || t.ItemID == c.ItemID || t.ReviewOf != "" || c.ReviewOf != "" {
		return false
	}
	for _, a := range ix.Lineage(c) {
		if a.ItemID == t.ItemID {
			return false
		}
	}
	for _, a := range ix.Lineage(t) {
		if a.ItemID == c.ItemID {
			return false
		}
	}
	return true
}

// WouldCycle reports whether c waiting for target closes a cycle (N2): c is
// reachable from target through dependsOn, the waits a card inherits from
// its ancestors, and a plan item's cards under it (its roll-up).
func (ix *Index) WouldCycle(c Card, target string) bool {
	visited := map[string]bool{}
	queue := []string{target}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == c.ItemID {
			return true
		}
		if visited[id] {
			continue
		}
		visited[id] = true
		x, ok := ix.Find(id)
		if !ok {
			continue
		}
		queue = append(queue, x.DependsOn...)
		for _, a := range ix.Lineage(x) {
			queue = append(queue, a.DependsOn...)
		}
		if IsPlanning(x) {
			for _, k := range ix.children[x.ItemID] {
				queue = append(queue, k.ItemID)
			}
		}
	}
	return false
}
