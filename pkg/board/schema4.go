package board

import (
	"container/heap"
	"sort"
	"strings"
)

// SCHEMA 4 — the ordering model's migration (docs/design/ordering.md, Q14).
// Schema 3 read a backlog in the Strategy board's tree order (K14); schema 4
// reads it in the board's order. The migration re-keys the board ONCE so that
// every order a person reads stays exactly as it was:
//
//   - every backlog, and any mix of them: the backlog cards in the old
//     backlog order (Schema3BacklogOrder — the reviews first, then the tree
//     walk);
//   - every Strategy range: each parent's children, and each level's roots,
//     in their old order;
//   - the flat epic order: the epics in the old tree walk.
//
// All of them are restrictions of one tree walk, so they agree; a stable
// topological sort with the old (rank, id) as its priority keeps every other
// card as close to where it stood as they allow. The cards already in that
// order with strictly increasing valid keys — the longest such run — keep
// their keys; the rest get evenly spaced keys between them. The order of
// tasks IN WORK inside a Team cell is the one thing one key per card cannot
// always keep (Appendix D): those cells are reported.

// CellChange is a Team cell — a team, a person, a zone — whose tasks in work
// read in a different order after the migration.
type CellChange struct {
	Team, Person, Zone string
	Before, After      []string
}

// Schema4Plan is the migration's plan over a board: the rank changes, and
// the Team cells whose in-work order they change. It is a pure function of
// the board, so a resumed run computes the same plan.
func Schema4Plan(b Board) ([]RankChange, []CellChange, error) {
	old := schema3Order(b)
	ix := NewIndex(Board{Cards: old, Customers: b.Customers})
	pos := make(map[string]int, len(old))
	for i, c := range old {
		pos[c.ItemID] = i
	}
	g := newConstraints(len(old))
	chain := func(ids []string) {
		for i := 1; i < len(ids); i++ {
			g.edge(pos[ids[i-1]], pos[ids[i]])
		}
	}
	for _, chainIDs := range Schema4Orders(ix, old) {
		chain(chainIDs)
	}
	order := g.sort()
	newOrder := make([]Card, len(order))
	for i, p := range order {
		newOrder[i] = old[p]
	}
	changes, err := keysFor(newOrder)
	if err != nil {
		return nil, nil, err
	}
	after := ApplyRankChanges(Board{Cards: append([]Card(nil), old...), Customers: b.Customers}, changes)
	return changes, cellChanges(ix, old, after.Cards), nil
}

// schema3Order is the board as schema 3 read it: by key, ties by id, a card
// with no key FIRST (the smallest string) — the order every client drew.
func schema3Order(b Board) []Card {
	var out []Card
	for _, c := range b.Cards {
		if !IsStateTitle(c.Title) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		return out[i].ItemID < out[j].ItemID
	})
	return out
}

// Schema4Orders is every order the migration keeps, as chains of ids over a
// board in its old order: the backlog (every backlog card, the old backlog
// order), each parent's children, each level's roots, and the flat epic
// order (the old tree walk). The tests read them back after the migration.
func Schema4Orders(ix *Index, old []Card) [][]string {
	var chains [][]string
	ids := func(cs []Card) []string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = c.ItemID
		}
		return out
	}
	var backlog []Card
	for _, c := range old {
		if ix.OnBacklog(c) {
			backlog = append(backlog, c)
		}
	}
	// In priority order: an order a later chain would contradict gives way
	// to an earlier one (Schema4Plan drops such an edge). The backlog first,
	// then the flat epic order, each parent's children, each level's roots.
	chains = append(chains, ids(Schema3BacklogOrder(ix, backlog)), flatEpics(ix, old))
	roots := map[string][]Card{}
	for _, c := range old {
		_, held := ix.Find(c.Parent)
		switch {
		case c.Parent != "" && held:
		case IsPlanning(c):
			roots[c.Type] = append(roots[c.Type], c)
		case !ix.OnBacklog(c):
			// The Strategy board's loose work in hand, in listing order;
			// a loose BACKLOG card is the backlog chain's.
			roots["work"] = append(roots["work"], c)
		}
		var kids []Card
		for _, k := range ix.ChildrenOf(c.ItemID) {
			// A review nobody has taken that a direct writer gave a parent
			// stands with the reviews on top of the backlog (K10), not
			// among that parent's children.
			if !NeedsReviewer(k) {
				kids = append(kids, k)
			}
		}
		if len(kids) > 1 {
			chains = append(chains, ids(kids))
		}
	}
	for _, k := range []string{TypeGoal, TypeInitiative, TypeEpic, "work"} {
		chains = append(chains, ids(roots[k]))
	}
	return chains
}

// flatEpics is the epics in the old Strategy board's flat epic range: the
// tree walk — roots level by level (goals, initiatives, epics), each node's
// children in board order — collecting the epics.
func flatEpics(ix *Index, old []Card) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(c Card)
	walk = func(c Card) {
		if seen[c.ItemID] {
			return
		}
		seen[c.ItemID] = true
		if c.Type == TypeEpic {
			out = append(out, c.ItemID)
		}
		for _, k := range ix.ChildrenOf(c.ItemID) {
			walk(k)
		}
	}
	for _, level := range []string{TypeGoal, TypeInitiative, TypeEpic} {
		for _, c := range old {
			if c.Type != level {
				continue
			}
			if _, ok := ix.Find(c.Parent); c.Parent == "" || !ok {
				walk(c)
			}
		}
	}
	return out
}

// constraints is a precedence graph over positions, sorted by Kahn's
// algorithm with the old position as priority.
type constraints struct {
	out [][]int
	in  []int
	has map[[2]int]bool
}

func newConstraints(n int) *constraints {
	return &constraints{out: make([][]int, n), in: make([]int, n), has: map[[2]int]bool{}}
}

// edge adds "a before b" unless it is there already or would close a cycle
// with the edges added before it — the earlier chains win.
func (g *constraints) edge(a, b int) {
	if a == b || g.has[[2]int{a, b}] || g.reaches(b, a) {
		return
	}
	g.has[[2]int{a, b}] = true
	g.out[a] = append(g.out[a], b)
	g.in[b]++
}

// reaches reports a path from a to b.
func (g *constraints) reaches(a, b int) bool {
	if len(g.out[a]) == 0 || g.in[b] == 0 {
		return false
	}
	seen := map[int]bool{a: true}
	stack := []int{a}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, y := range g.out[x] {
			if y == b {
				return true
			}
			if !seen[y] {
				seen[y] = true
				stack = append(stack, y)
			}
		}
	}
	return false
}

type minHeap []int

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(int)) }
func (h *minHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// sort is the topological order closest to the old one. A cycle — which the
// constraints, all restrictions of one tree walk, never make, but a
// repository a direct writer broke might — is broken at its earliest card.
func (g *constraints) sort() []int {
	n := len(g.in)
	in := append([]int(nil), g.in...)
	done := make([]bool, n)
	h := &minHeap{}
	for i := 0; i < n; i++ {
		if in[i] == 0 {
			heap.Push(h, i)
		}
	}
	out := make([]int, 0, n)
	next := 0
	for len(out) < n {
		if h.Len() == 0 {
			for done[next] {
				next++
			}
			in[next] = 0
			heap.Push(h, next)
		}
		p := heap.Pop(h).(int)
		if done[p] {
			continue
		}
		done[p] = true
		out = append(out, p)
		for _, q := range g.out[p] {
			if in[q]--; in[q] == 0 && !done[q] {
				heap.Push(h, q)
			}
		}
	}
	return out
}

// keysFor gives cards in their new order strictly increasing keys, keeping
// the longest run of valid, strictly increasing old keys as they are.
func keysFor(cards []Card) ([]RankChange, error) {
	cards = append([]Card(nil), cards...)
	keep := lisKeep(cards)
	var changes []RankChange
	for i := 0; i < len(cards); {
		if keep[i] {
			i++
			continue
		}
		j := i
		for j < len(cards) && !keep[j] {
			j++
		}
		// The run [i, j) between the kept neighbours; widened over a kept
		// neighbour when the gap between them has no room.
		lo, hi := i, j
		for {
			floor, ceil := "", ""
			if lo > 0 {
				floor = cards[lo-1].Rank
			}
			if hi < len(cards) {
				ceil = cards[hi].Rank
			}
			keys, err := RankRebalance(floor, ceil, hi-lo)
			if err == nil && shortEnough(keys) || err == nil && lo == 0 && hi == len(cards) {
				var run []RankChange
				for k := lo; k < hi; k++ {
					if cards[k].Rank != keys[k-lo] {
						run = append(run, RankChange{ID: cards[k].ItemID, Before: cards[k].Rank, After: keys[k-lo]})
						cards[k].Rank = keys[k-lo]
					}
				}
				changes = merge(changes, run)
				break
			}
			if lo == 0 && hi == len(cards) {
				return nil, errNoRoom
			}
			if lo > 0 {
				lo--
			}
			if hi < len(cards) {
				hi++
			}
		}
		i = hi
	}
	return changes, nil
}

// lisKeep marks the longest subsequence of cards whose old keys are valid
// and strictly increasing in the new order: those keep their keys.
func lisKeep(cards []Card) []bool {
	tails := []int{} // index of the smallest tail of each length
	prev := make([]int, len(cards))
	for i, c := range cards {
		prev[i] = -1
		if !ValidRank(c.Rank) {
			continue
		}
		at := sort.Search(len(tails), func(k int) bool { return cards[tails[k]].Rank >= c.Rank })
		if at > 0 {
			prev[i] = tails[at-1]
		}
		if at == len(tails) {
			tails = append(tails, i)
		} else {
			tails[at] = i
		}
	}
	keep := make([]bool, len(cards))
	if len(tails) == 0 {
		return keep
	}
	for i := tails[len(tails)-1]; i >= 0; i = prev[i] {
		keep[i] = true
	}
	return keep
}

// cellChanges compares the Team cells — open tasks in work, by the team
// they stand under, their first person and their zone — before and after.
func cellChanges(ix *Index, before, after []Card) []CellChange {
	cellOf := func(c Card) (string, bool) {
		if !InWork(c) || c.Parked || Complete(c.Stage, c.Progress) || IsPlanning(c) || IsStateTitle(c.Title) {
			return "", false
		}
		person := ""
		if len(c.Assignees) > 0 {
			person = c.Assignees[0]
		}
		return ix.PlacedTeam(c) + "\x00" + person + "\x00" + string(c.Zone), true
	}
	group := func(cards []Card) map[string][]string {
		out := map[string][]string{}
		for _, c := range cards {
			if k, ok := cellOf(c); ok {
				out[k] = append(out[k], c.ItemID)
			}
		}
		return out
	}
	was, now := group(before), group(after)
	keys := make([]string, 0, len(was))
	for k := range was {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []CellChange
	for _, k := range keys {
		if strings.Join(was[k], " ") == strings.Join(now[k], " ") {
			continue
		}
		parts := strings.SplitN(k, "\x00", 3)
		out = append(out, CellChange{Team: parts[0], Person: parts[1], Zone: parts[2], Before: was[k], After: now[k]})
	}
	return out
}
