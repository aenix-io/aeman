package board

import "slices"

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
	if !IsPlanning(c) {
		return Complete(c.Stage, c.Progress)
	}
	// A plan item's roll-up walks its subtree; many cards ask it of the
	// same few targets, so it is worked out once per index.
	ix.memo.Lock()
	d, ok := ix.done[c.ItemID]
	ix.memo.Unlock()
	if ok {
		return d
	}
	st, _, _ := ix.HierarchyStatus(c)
	d = st == HierarchyDone
	ix.memo.Lock()
	if ix.done == nil {
		ix.done = map[string]bool{}
	}
	ix.done[c.ItemID] = d
	ix.memo.Unlock()
	return d
}

// HasDependencies reports a board on which some card waits for another.
func (ix *Index) HasDependencies() bool { return ix.hasDeps }

// Blockers are a card's unsatisfied dependencies: its own first, then each
// ancestor's, nearest first. A target the board does not hold is dangling
// and blocks nothing (N6).
func (ix *Index) Blockers(c Card) []Blocker {
	if !ix.waitsInLine(c) {
		return nil
	}
	// The card's own first, then each ancestor's, nearest first — walked up
	// the parents in place. What one holder contributes is the same for
	// every card under it, so it is worked out once and shared; a card whose
	// blockers all come from one holder gets that holder's list as it is
	// (clipped, so an append by the caller copies).
	var out []Blocker
	parts := 0
	seen := [8]string{}
	cur := c
	for hops := 0; hops < 7; hops++ {
		if own := ix.holderBlockers(cur); len(own) > 0 {
			switch parts {
			case 0:
				out = own
			case 1:
				out = append(append([]Blocker(nil), out...), own...)
			default:
				out = append(out, own...)
			}
			parts++
		}
		seen[hops] = cur.ItemID
		i, ok := ix.pos[cur.Parent]
		if !ok || cur.Parent == "" {
			break
		}
		loop := false
		for _, s := range seen[:hops+1] {
			if s == ix.cards[i].ItemID {
				loop = true
			}
		}
		if loop {
			break
		}
		cur = ix.cards[i]
	}
	return out
}

// holderBlockers is what one card's own dependsOn blocks the cards in its
// line with: the targets that are on the board and not done, once per card
// for the index's life.
func (ix *Index) holderBlockers(h Card) []Blocker {
	if len(h.DependsOn) == 0 {
		return nil
	}
	ix.memo.Lock()
	got, ok := ix.held[h.ItemID]
	ix.memo.Unlock()
	if ok {
		return got
	}
	var out []Blocker
	for _, id := range h.DependsOn {
		t, ok := ix.Find(id)
		if !ok || ix.Done(t) {
			continue
		}
		out = append(out, Blocker{Target: t, Via: h})
	}
	out = slices.Clip(out)
	ix.memo.Lock()
	if ix.held == nil {
		ix.held = map[string][]Blocker{}
	}
	ix.held[h.ItemID] = out
	ix.memo.Unlock()
	return out
}

// waitsInLine reports a card that, or an ancestor of which, waits for
// anything — the only cards that can have a blocker. Walked up the parents
// without building the lineage, as bounded as Lineage is, so the cards that
// wait for nothing (nearly all of them) cost a few lookups.
func (ix *Index) waitsInLine(c Card) bool {
	if !ix.hasDeps {
		return false
	}
	cur := c
	for range 7 { // the card and Lineage's six hops
		if len(cur.DependsOn) > 0 {
			return true
		}
		p, ok := ix.Find(cur.Parent)
		if !ok {
			return false
		}
		cur = p
	}
	return false
}

// Locked reports a card with at least one blocker (N3). Independent of the
// manual `stage: locked`.
func (ix *Index) Locked(c Card) bool { return len(ix.Blockers(c)) > 0 }

// BlockersAs splits a card's blockers by what a reader may read: shown are
// the ones whose target, and every card from this one up to the card that
// names it, the reader can read; hidden counts the rest (N4).
func (ix *Index) BlockersAs(c Card, can func(Card) bool) (shown []Blocker, hidden int) {
	all := ix.Blockers(c)
	if len(all) == 0 {
		return nil, 0
	}
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
	for _, b := range all {
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
// customers on once, so a cycle a direct writer left ends. can, when set,
// is what the reader may read: the chain of ancestors ends at the first one
// they cannot (O4's readable-ancestors rule), a task they cannot read gives
// a review card nothing, and a card they cannot read passes nothing on.
func (ix *Index) effectiveCustomers(c Card, visited map[string]bool, can func(Card) bool) ([]string, []DependentCustomer) {
	if can == nil {
		can = func(Card) bool { return true }
	}
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
		if task, ok := ix.Find(c.ReviewOf); ok && task.ReviewOf == "" && can(task) && !visited[task.ItemID] {
			visited[task.ItemID] = true
			ids, _ := ix.effectiveCustomers(task, visited, can)
			add(ids)
		}
	}
	all := ix.Lineage(c)
	start := 0
	for i := len(all) - 1; i >= 0; i-- {
		if !can(all[i]) {
			start = i + 1
			break
		}
	}
	up := all[start:]
	for _, a := range up {
		add(a.Customers)
	}
	add(c.Customers)
	// What waits for this card, or for an ancestor, is for its customers
	// too — after the card's own line, so O4's order stands.
	holders := append(append([]Card(nil), up...), c)
	for _, h := range holders {
		for _, d := range ix.dependents[h.ItemID] {
			if visited[d.ItemID] || !can(d) {
				continue
			}
			visited[d.ItemID] = true
			ids, _ := ix.effectiveCustomers(d, visited, can)
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
	return ix.customersOf(c).from
}

// EffectiveCustomersAs is EffectiveCustomers and DependentCustomers as a
// reader who can read only some cards works them out on their visible board
// (N5): a watch frame counted again for its subscriber.
func (ix *Index) EffectiveCustomersAs(c Card, can func(Card) bool) ([]string, []DependentCustomer) {
	return ix.effectiveCustomers(c, map[string]bool{c.ItemID: true}, can)
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

// WouldCycle reports whether c waiting for target closes a cycle (N2): c —
// or any card under c, which inherits c's wait — is reachable from target
// through dependsOn, the waits a card inherits from its ancestors, and a
// plan item's cards under it (its roll-up).
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
		up := ix.Lineage(x)
		for _, a := range up {
			if a.ItemID == c.ItemID {
				return true // x is under c, and inherits the wait being added
			}
		}
		queue = append(queue, x.DependsOn...)
		for _, a := range up {
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

// DependencyProblem is why a dependency list cannot be written.
type DependencyProblem int

// The answers CheckDependsOn gives.
const (
	DependencyFine DependencyProblem = iota
	// DependencyBadTarget is a target N1 refuses.
	DependencyBadTarget
	// DependencyClosesCycle is a target that would close a cycle (N2).
	DependencyClosesCycle
)

// CheckDependsOn judges writing ids as the list of the card with id itemID
// on b: each id the card's list on b does not hold yet must be a lawful
// target (N1) and close no cycle (N2), judged on the board as the write would
// leave it, so two new ids that close a cycle only together are found. It
// answers the first problem and the id it is about. The board must be the
// WHOLE board: a visitor's breaks a lineage at a card they cannot read and
// ends a cycle's walk there.
func CheckDependsOn(b Board, itemID string, ids []string) (DependencyProblem, string) {
	ix := NewIndex(b)
	card, ok := ix.Find(itemID)
	if !ok {
		return DependencyFine, ""
	}
	if len(ids) > 0 && card.ReviewOf != "" {
		return DependencyBadTarget, ""
	}
	after := b
	after.Cards = make([]Card, len(b.Cards))
	copy(after.Cards, b.Cards)
	for i := range after.Cards {
		if after.Cards[i].ItemID == itemID {
			after.Cards[i].DependsOn = ids
		}
	}
	tentative := NewIndex(after)
	self, _ := tentative.Find(itemID)
	had := map[string]bool{}
	for _, id := range card.DependsOn {
		had[id] = true
	}
	for _, id := range ids {
		if had[id] {
			continue // on the list already: lawful when it was written
		}
		if !ix.DependencyTargetOK(card, id) {
			return DependencyBadTarget, id
		}
		if tentative.WouldCycle(self, id) {
			return DependencyClosesCycle, id
		}
	}
	return DependencyFine, ""
}
