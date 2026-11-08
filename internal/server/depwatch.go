package server

import (
	"sort"
	"strings"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

// Dependencies on the watch (docs/design/dependencies.md, N4, N8). The lock,
// the blockers and the customers a target inherits are derived from OTHER
// cards, so no write touches the cards they change: a target finishing
// unlocks the cards that wait for it, and a card's dependency list moves
// what its targets block and whom they are for.

// depShown is what other cards derive from this one: whom it waits for,
// its customers and the chain it hangs from (whom it passes them on to, and
// whose waits it inherits), and its title and type (named in the blockers of
// what waits for it, and in the blocks and dependent customers of what it
// waits for). Done is recorded for the cards something waits for — the
// targets — alone: the only ones whose done unlocks anything.
type depShown struct {
	deps, customers, parent, title, typ string
	done                                bool
}

func shownDependency(c board.Card) depShown {
	return depShown{deps: strings.Join(c.DependsOn, ","), customers: strings.Join(c.Customers, ","),
		parent: c.Parent, title: c.Title, typ: c.Type}
}

// rememberDependencies records every card's depShown and every target's
// done, once some card depends on another. The caller holds e.mu.
func (e *boardEntry) rememberDependencies() {
	e.depOf = nil
	e.depTargets = nil
	e.anyDeps = false
	for _, c := range e.board.Cards {
		if len(c.DependsOn) > 0 {
			e.anyDeps = true
			break
		}
	}
	if !e.anyDeps {
		return
	}
	ix := board.NewIndex(e.board)
	e.depOf = make(map[string]depShown, len(e.board.Cards))
	e.depTargets = map[string]bool{}
	for _, c := range e.board.Cards {
		e.depOf[c.ItemID] = shownDependency(c)
		for _, t := range c.DependsOn {
			e.depTargets[t] = true
		}
	}
	for t := range e.depTargets {
		if tc, ok := ix.Find(t); ok {
			sh := e.depOf[t]
			sh.done = ix.Done(tc)
			e.depOf[t] = sh
		}
	}
}

// dependencyFanout is the ids of the cards to re-send after c changed (N8):
//
//   - c's own list, customers or parent moved — c arriving or leaving
//     included: the cards under it, and every target it — or a card under
//     it — waits for, before or after, with the cards under those and,
//     transitively, what they wait for (customers);
//   - c renamed or re-typed: what waits for it (their blockers name it) and
//     what it waits for (their blocks and dependent customers name it);
//   - a target became done or undone — c itself, or a plan item whose roll-up
//     c moved by finishing, reopening, arriving, leaving or moving out from
//     under it: every card that waits for it and the cards under those.
//
// The caller holds e.mu with the cache already updated.
func (e *boardEntry) dependencyFanout(c board.Card, verb string) []string {
	if !e.anyDeps {
		if verb == "DELETED" || len(c.DependsOn) == 0 {
			return nil
		}
		// The first dependency on the board: what it is measured against
		// from now on, with c's own list as it was before — none.
		e.rememberDependencies()
		sh := e.depOf[c.ItemID]
		sh.deps = ""
		e.depOf[c.ItemID] = sh
	}
	ix := board.NewIndex(e.board)
	out := map[string]bool{}
	subtree := func(id string) []string {
		var ids []string
		seen := map[string]bool{id: true}
		for level := []string{id}; len(level) > 0; {
			var next []string
			for _, p := range level {
				for _, k := range ix.ChildrenOf(p) {
					if !seen[k.ItemID] {
						seen[k.ItemID] = true
						next = append(next, k.ItemID)
					}
				}
			}
			ids = append(ids, next...)
			level = next
		}
		return ids
	}
	withSubtree := func(id string) {
		out[id] = true
		for _, k := range subtree(id) {
			out[k] = true
		}
	}
	reach := func(targets []string) {
		seen := map[string]bool{}
		for len(targets) > 0 {
			id := targets[0]
			targets = targets[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			t, ok := ix.Find(id)
			if !ok {
				continue
			}
			withSubtree(id)
			targets = append(targets, t.DependsOn...)
			for _, k := range subtree(id) {
				if kc, ok := ix.Find(k); ok {
					targets = append(targets, kc.DependsOn...)
				}
			}
		}
	}
	waiters := func(id string) {
		for _, d := range ix.Blocks(board.Card{ItemID: id}) {
			withSubtree(d.ItemID)
		}
	}
	was, known := e.depOf[c.ItemID]
	if verb == "DELETED" {
		delete(e.depOf, c.ItemID)
		// What it waited for no longer blocks it nor is for its customers;
		// what waited for it waits for nothing now.
		reach(c.DependsOn)
		waiters(c.ItemID)
	} else {
		now := shownDependency(c)
		now.done = was.done
		e.depOf[c.ItemID] = now
		for _, t := range c.DependsOn {
			e.depTargets[t] = true
		}
		// A card the cache has not seen — written, fetched, reloaded — moved
		// from waiting for nothing.
		if !known || was.deps != now.deps || was.customers != now.customers || was.parent != now.parent {
			targets := append(splitIDs(was.deps), c.DependsOn...)
			for _, k := range subtree(c.ItemID) {
				out[k] = true
				if kc, ok := ix.Find(k); ok {
					targets = append(targets, kc.DependsOn...)
				}
			}
			reach(targets)
		}
		if known && (was.title != now.title || was.typ != now.typ) {
			waiters(c.ItemID)
			reach(c.DependsOn)
		}
	}
	// Every target judged again: whatever this change did to a roll-up —
	// the chain c joined, the chain it left, a task deleted from under a
	// plan item — shows as a target's done moving.
	for t := range e.depTargets {
		tc, ok := ix.Find(t)
		if !ok {
			continue
		}
		sh, known := e.depOf[t]
		done := ix.Done(tc)
		if known && sh.done != done {
			waiters(t)
		}
		sh.done = done
		e.depOf[t] = sh
	}
	delete(out, c.ItemID)
	ids := make([]string, 0, len(out))
	for id := range out {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func splitIDs(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// seenDependencies is a frame's dependency fields as the subscriber's
// listing works them out on their visible board (N4, N5): the blockers they
// may not read counted, not named; the waiting cards they may not read left
// out of `blocks`; and the customers only readable waiting work passes on.
// It reports whether it worked them out — whether any dependency touches
// the card at all; its effectiveCustomers then stand as worked out here.
func seenDependencies(res *apiserver.Card, ix *board.Index, c board.Card, readable func(board.Card) bool) bool {
	st := &res.Status
	if !touchedByDependencies(ix, c) {
		return false
	}
	shown, hidden := ix.BlockersAs(c, readable)
	st.BlockedBy = nil
	for _, bl := range shown {
		ref := apiserver.BlockerRef{UID: bl.Target.ItemID, Type: bl.Target.Type, Title: bl.Target.Title}
		if bl.Via.ItemID != c.ItemID {
			ref.Via = bl.Via.ItemID
		}
		st.BlockedBy = append(st.BlockedBy, ref)
	}
	st.HiddenBlockers = hidden
	st.Locked = len(shown)+hidden > 0
	var blocks []apiserver.LineageRef
	for _, b := range st.Blocks {
		if d, ok := ix.Find(b.UID); ok && readable(d) {
			blocks = append(blocks, b)
		}
	}
	st.Blocks = blocks
	ids, from := ix.EffectiveCustomersAs(c, readable)
	st.EffectiveCustomers = ids
	st.DependentCustomers = nil
	for _, dc := range from {
		st.DependentCustomers = append(st.DependentCustomers, apiserver.DependentCustomerRef{Customer: dc.Customer,
			From: apiserver.LineageRef{UID: dc.From.ItemID, Type: dc.From.Type, Title: dc.From.Title}})
	}
	return true
}

// touchedByDependencies reports whether any dependency can move a card's
// derived fields: one the card or an ancestor names (its blockers), or one
// naming the card or an ancestor (blocks, and the customers waiting work
// passes down) — for a review card, its task's line too, whose customers it
// takes. When none does, the whole board and a visitor's board agree on
// them. Judged on the whole board: a dependency the subscriber cannot read
// can still change what their board says (N4, N5).
func touchedByDependencies(ix *board.Index, c board.Card) bool {
	line := func(c board.Card) bool {
		for _, h := range append(ix.Lineage(c), c) {
			if len(h.DependsOn) > 0 || len(ix.Blocks(h)) > 0 {
				return true
			}
		}
		return false
	}
	if line(c) {
		return true
	}
	if c.ReviewOf != "" {
		if task, ok := ix.Find(c.ReviewOf); ok && line(task) {
			return true
		}
	}
	return false
}
