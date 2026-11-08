package board

import (
	"fmt"
	"sort"
)

// The ORDERING MODEL (docs/design/ordering.md, Q1–Q14). A card keeps one
// `rank`, and what it means is the card's kind:
//
//   - on an EPIC it is the board-wide EPIC ORDER — independent of the goal
//     and the initiative above it;
//   - on a TASK it is its place in its team's QUEUE, Q(T): the open, parked,
//     own-place tasks standing under team T, in board order;
//   - goals and initiatives order among their own.
//
// Nothing inherits down the tree. An epic sets the base position of its
// tasks — a new task lands in its epic's AREA by Place — and a task moved
// by hand is a free exception the epic's next move gathers back.
//
// Keys are computed here, over the MERGED board, and handed to the backend
// to write as they are (RankChange): a backend never decides a place.

// EpicOf is a card's EPIC: its nearest epic ancestor, or "" — a NEUTRAL task.
func (ix *Index) EpicOf(c Card) string {
	up := ix.Lineage(c)
	for i := len(up) - 1; i >= 0; i-- {
		if up[i].Type == TypeEpic {
			return up[i].ItemID
		}
	}
	return ""
}

// PlanParentOf is a card's nearest PLANNING ancestor — its epic when it has
// one, else the initiative or goal it hangs under — or "".
func (ix *Index) PlanParentOf(c Card) string {
	up := ix.Lineage(c)
	for i := len(up) - 1; i >= 0; i-- {
		if IsPlanning(up[i]) {
			return up[i].ItemID
		}
	}
	return ""
}

// InQueue reports whether a card stands in a team queue: a backlog card
// (OnBacklog) that is not a review nobody has taken — those keep their own
// pinned group at the top of the backlog (K10).
func (ix *Index) InQueue(c Card) bool {
	return ix.OnBacklog(c) && !NeedsReviewer(c)
}

// Queue is Q(T): the queue of one team ("" is the no-team group), in board
// order.
func (ix *Index) Queue(team string) []Card {
	var out []Card
	for _, c := range ix.cards {
		if ix.InQueue(c) && ix.PlacedTeam(c) == team {
			out = append(out, c)
		}
	}
	return out
}

// EpicOrder is the epic order one team reads: each epic's place, by id.
// Every algorithm takes it as a parameter. Today it is the SAME global order
// for every team (the epics in board order); a per-team exception would be
// an `epicRanks` key in the team's file read here — the documented
// extension point, not built (ordering.md).
type EpicOrder map[string]int

// EpicOrderFor is the epic order team T reads.
func (ix *Index) EpicOrderFor(_ string) EpicOrder {
	out := EpicOrder{}
	for _, c := range ix.cards {
		if c.Type == TypeEpic {
			out[c.ItemID] = len(out)
		}
	}
	return out
}

// BestGap is where an epic's block — or the first task of an empty epic —
// goes in a queue q (Appendix A of ordering.md): the gap that contradicts
// the epic order least. Gap g costs the tasks before it whose epic comes
// AFTER e plus the tasks from it on whose epic comes BEFORE e; neutral tasks
// and e's own count nothing. Adjacent equal-cost gaps are a run; the
// earliest minimal run wins, and within it the first gap when the run
// starts right after a task of an earlier epic, else its last — with
// nothing counted at all, the end of the queue.
func BestGap(q []Card, e string, order EpicOrder, epicOf func(Card) string) int {
	me, ok := order[e]
	rel := make([]int, len(q)) // -1 earlier epic, +1 later epic, 0 neither
	for i, c := range q {
		ce := epicOf(c)
		if ce == "" || ce == e || !ok {
			continue
		}
		if p, known := order[ce]; known {
			switch {
			case p < me:
				rel[i] = -1
			case p > me:
				rel[i] = 1
			}
		}
	}
	cost := make([]int, len(q)+1)
	before := 0
	for _, r := range rel {
		if r < 0 {
			before++
		}
	}
	after := 0
	for g := 0; g <= len(q); g++ {
		cost[g] = after + before
		if g < len(q) {
			if rel[g] > 0 {
				after++
			}
			if rel[g] < 0 {
				before--
			}
		}
	}
	best := 0
	for g := range cost {
		if cost[g] < cost[best] {
			best = g
		}
	}
	end := best
	for end+1 < len(cost) && cost[end+1] == cost[best] {
		end++
	}
	if best > 0 && rel[best-1] < 0 {
		return best
	}
	return end
}

// GapCost is the cost BestGap gives gap g — what ValidReturn compares a
// returning task's gap against.
func GapCost(q []Card, g int, e string, order EpicOrder, epicOf func(Card) string) int {
	me, ok := order[e]
	if !ok {
		return 0
	}
	n := 0
	for i, c := range q {
		ce := epicOf(c)
		if ce == "" || ce == e {
			continue
		}
		p, known := order[ce]
		if !known {
			continue
		}
		if i < g && p > me || i >= g && p < me {
			n++
		}
	}
	return n
}

// Spot is a place in the board's order named by a neighbour: right after
// After, or — with After empty — right before Before; both empty is the end
// of the board.
type Spot struct {
	After  string
	Before string
}

// PlaceSpot is THE default position of a task c in team T's queue (Q3) —
// create, a team handover, a drop into another epic, a return whose old key
// is no longer valid, normalisation. c is the card as it will be (its new
// parent, its new team); the queue is read without it.
//
//	epic E has parked tasks in Q(T)          → right after the LAST of them
//	epic E with none in Q(T)                 → BestGap(Q(T), E)
//	neutral, plan parent P has tasks in Q(T) → right after the last of them
//	otherwise                                → right after the last task of Q(T);
//	                                           an empty queue → the end of the board
//
// An explicit place always wins over it; the callers apply that.
func (ix *Index) PlaceSpot(c Card, team string) Spot {
	q := ix.queueWithout(team, c.ItemID)
	epic := ix.EpicOf(c)
	if epic != "" {
		if last := lastOf(q, func(t Card) bool { return ix.EpicOf(t) == epic }); last != "" {
			return Spot{After: last}
		}
		return spotAtGap(q, BestGap(q, epic, ix.EpicOrderFor(team), ix.EpicOf))
	}
	if p := ix.PlanParentOf(c); p != "" {
		if last := lastOf(q, func(t Card) bool { return ix.PlanParentOf(t) == p }); last != "" {
			return Spot{After: last}
		}
	}
	if len(q) > 0 {
		return Spot{After: q[len(q)-1].ItemID}
	}
	return Spot{}
}

func (ix *Index) queueWithout(team, id string) []Card {
	var q []Card
	for _, t := range ix.Queue(team) {
		if t.ItemID != id {
			q = append(q, t)
		}
	}
	return q
}

func lastOf(q []Card, match func(Card) bool) string {
	for i := len(q) - 1; i >= 0; i-- {
		if match(q[i]) {
			return q[i].ItemID
		}
	}
	return ""
}

// spotAtGap is gap g of a queue as a spot: right after the task before it,
// right before the first task at g = 0, the end of the board in an empty
// queue.
func spotAtGap(q []Card, g int) Spot {
	switch {
	case g > 0:
		return Spot{After: q[g-1].ItemID}
	case len(q) > 0:
		return Spot{Before: q[0].ItemID}
	}
	return Spot{}
}

// SpotChanges is the keys that put card id at a spot in the board's order.
// The board's cards are its order (rank, then id).
func SpotChanges(b Board, id string, s Spot) ([]RankChange, error) {
	list := RankedOfBoard(b)
	return PlaceAfter(list, id, afterFor(list, id, s))
}

// afterFor is a spot as the card it follows in a list without id: Before is
// "after the card before it" ("" — the top — when it is first), and the
// empty spot is after the last card.
func afterFor(list []Ranked, id string, s Spot) string {
	if s.After != "" {
		return s.After
	}
	prev := ""
	for _, r := range list {
		if r.ID == id {
			continue
		}
		if s.Before != "" && r.ID == s.Before {
			return prev
		}
		prev = r.ID
	}
	if s.Before != "" {
		return "" // a spot before a card the list lacks: nothing is known; the top
	}
	return prev
}

// Place is PlaceSpot as keys: where c goes in team T's queue by default.
func Place(b Board, c Card, team string) ([]RankChange, error) {
	return SpotChanges(b, c.ItemID, NewIndex(b).PlaceSpot(c, team))
}

// ValidReturn reports whether a task coming back to its team's queue — the
// ×, unassign, park — may keep its old key (Q8): only while the key is
// still inside its epic's AREA in Q(T) (strictly after the task before the
// epic's first task and strictly before the task after its last); with no
// other task of the epic there, while the gap it falls in costs no more
// than the best one; a neutral task by its plan parent's area. No plan item
// above it at all, or a key no server mints, is never kept.
func (ix *Index) ValidReturn(c Card, team string) bool {
	if !ValidRank(c.Rank) {
		return false
	}
	q := ix.queueWithout(team, c.ItemID)
	at := 0 // the gap the old key falls in
	for at < len(q) && rankedLess(q[at], c) {
		at++
	}
	epic := ix.EpicOf(c)
	group := func(t Card) bool { return epic != "" && ix.EpicOf(t) == epic }
	if epic == "" {
		p := ix.PlanParentOf(c)
		if p == "" {
			return false
		}
		group = func(t Card) bool { return ix.PlanParentOf(t) == p }
	}
	first, last := -1, -1
	for i, t := range q {
		if group(t) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		if epic == "" {
			return false
		}
		order := ix.EpicOrderFor(team)
		min := GapCost(q, BestGap(q, epic, order, ix.EpicOf), epic, order, ix.EpicOf)
		return GapCost(q, at, epic, order, ix.EpicOf) <= min
	}
	return at >= first && at <= last+1
}

// rankedLess is board order: by key, ties by id.
func rankedLess(a, b Card) bool {
	if a.Rank != b.Rank {
		return a.Rank < b.Rank
	}
	return a.ItemID < b.ItemID
}

// ReturnChanges is the keys a task coming back to team T's queue gets: none
// when its old key is still valid (ValidReturn), else Place.
func ReturnChanges(b Board, c Card, team string) ([]RankChange, error) {
	ix := NewIndex(b)
	if ix.ValidReturn(c, team) {
		return nil, nil
	}
	return SpotChanges(b, c.ItemID, ix.PlaceSpot(c, team))
}

// InheritedEpic is the epic a task made in a gap of a list inherits (Q10):
// the epic of the card above it — the same as below's when the two agree, the
// card above's at a boundary — or, at the top of the list, the card below's;
// nothing in an empty list. The epics are the neighbours' EpicOf, reviews
// already skipped by the caller.
func InheritedEpic(above, below string, hasAbove, hasBelow bool) string {
	switch {
	case hasAbove:
		return above
	case hasBelow:
		return below
	}
	return ""
}

// Split reports an epic whose parked tasks in team T's queue do not stand
// together: another task stands inside its area.
func (ix *Index) Split(epic, team string) bool {
	q := ix.Queue(team)
	first, last, n := -1, -1, 0
	for i, t := range q {
		if ix.EpicOf(t) == epic {
			if first < 0 {
				first = i
			}
			last = i
			n++
		}
	}
	return n > 0 && last-first+1 != n
}

// applyChanges is the board with the changes written and the cards back in
// board order — the state the next step of a plan computes over.
func applyChanges(b Board, changes []RankChange) Board {
	if len(changes) == 0 {
		return b
	}
	keys := make(map[string]string, len(changes))
	for _, ch := range changes {
		keys[ch.ID] = ch.After
	}
	cards := make([]Card, len(b.Cards))
	copy(cards, b.Cards)
	for i := range cards {
		if k, ok := keys[cards[i].ItemID]; ok {
			cards[i].Rank = k
		}
	}
	SortCards(cards)
	b.Cards = cards
	return b
}

// ApplyRankChanges is applyChanges for the callers that keep a board of
// their own in step with a plan (the server's cache, a test's fake).
func ApplyRankChanges(b Board, changes []RankChange) Board { return applyChanges(b, changes) }

// SortCards puts cards in board order: by key, ties by id; a card with no
// valid key after every card that has one (normalisation gives it a place),
// the teams' state cards where they stand relative to each other.
func SortCards(cards []Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		vi, vj := ValidRank(cards[i].Rank), ValidRank(cards[j].Rank)
		if vi != vj {
			return vi
		}
		return rankedLess(cards[i], cards[j])
	})
}

// merge folds a later step's changes into a plan: one entry per card, its
// first Before and its last After.
func merge(plan, more []RankChange) []RankChange {
	at := make(map[string]int, len(plan))
	for i, ch := range plan {
		at[ch.ID] = i
	}
	for _, ch := range more {
		if i, ok := at[ch.ID]; ok {
			plan[i].After = ch.After
			continue
		}
		at[ch.ID] = len(plan)
		plan = append(plan, ch)
	}
	out := plan[:0]
	for _, ch := range plan {
		if ch.Before != ch.After {
			out = append(out, ch)
		}
	}
	return out
}

// MoveEpicPlan is the epic move (Q5), the same operation from the Strategy
// board and the Backlog: the epic takes a key right after `after` ("" = the
// top of the board) — its new place in the epic order — and then, in EVERY
// team holding its parked tasks, those tasks are gathered into one block,
// their order kept, at the BestGap of the queue without them under the new
// epic order. A block already standing there writes nothing; tasks in work
// and other epics' tasks never move.
func MoveEpicPlan(b Board, epic, after string) ([]RankChange, error) {
	ix := NewIndex(b)
	e, ok := ix.Find(epic)
	if !ok || e.Type != TypeEpic {
		return nil, fmt.Errorf("move epic: %q is no epic on the board", epic)
	}
	plan, err := PlaceAfter(RankedOfBoard(b), epic, after)
	if err != nil {
		return nil, err
	}
	b = applyChanges(b, plan)
	more, err := GatherPlan(b, epic)
	if err != nil {
		return nil, err
	}
	return merge(plan, more), nil
}

// GatherPlan gathers an epic's parked tasks into one block per team at the
// best gap of the queue without them — MoveEpicPlan's second half, on a
// board whose epic order is already the new one.
func GatherPlan(b Board, epic string) ([]RankChange, error) {
	ix := NewIndex(b)
	var teams []string
	seen := map[string]bool{}
	for _, c := range ix.cards {
		if ix.InQueue(c) && ix.EpicOf(c) == epic {
			if t := ix.PlacedTeam(c); !seen[t] {
				seen[t] = true
				teams = append(teams, t)
			}
		}
	}
	var plan []RankChange
	for _, team := range teams {
		ix = NewIndex(b)
		q := ix.Queue(team)
		var block, rest []Card
		for _, t := range q {
			// A task the mover may not write stays where it stands — an
			// exception, like any — and the block gathers around it.
			if ix.EpicOf(t) == epic && !b.Pinned[t.ItemID] {
				block = append(block, t)
			} else {
				rest = append(rest, t)
			}
		}
		g := BestGap(rest, epic, ix.EpicOrderFor(team), ix.EpicOf)
		if contiguousAt(q, block, rest, g) {
			continue
		}
		changes, err := blockChanges(b, block, spotAtGap(rest, g))
		if err != nil {
			return nil, err
		}
		plan = merge(plan, changes)
		b = applyChanges(b, changes)
	}
	return plan, nil
}

// contiguousAt reports a queue that already reads rest[:g] + block + rest[g:].
func contiguousAt(q, block, rest []Card, g int) bool {
	want := make([]string, 0, len(q))
	for _, c := range rest[:g] {
		want = append(want, c.ItemID)
	}
	for _, c := range block {
		want = append(want, c.ItemID)
	}
	for _, c := range rest[g:] {
		want = append(want, c.ItemID)
	}
	for i, c := range q {
		if want[i] != c.ItemID {
			return false
		}
	}
	return true
}

// blockChanges is the keys that put a block of cards, in the order given,
// at a spot of the board's order.
func blockChanges(b Board, block []Card, s Spot) ([]RankChange, error) {
	in := make(map[string]bool, len(block))
	movers := make([]Ranked, 0, len(block))
	for _, c := range block {
		in[c.ItemID] = true
		movers = append(movers, Ranked{ID: c.ItemID, Rank: c.Rank})
	}
	var rest []Ranked
	for _, r := range RankedOfBoard(b) {
		if !in[r.ID] && ValidRank(r.Rank) {
			rest = append(rest, r)
		}
	}
	at := len(rest) // the empty spot, or an anchor the list lacks: the end
	for i, r := range rest {
		if s.After != "" && r.ID == s.After {
			at = i + 1
			break
		}
		if s.After == "" && s.Before != "" && r.ID == s.Before {
			at = i
			break
		}
	}
	return PlaceRunAt(rest, movers, at)
}

// ReorderInEpicPlan is a task moved WITHIN its epic on the Strategy board
// (Q6): a slot permutation. The epic's parked tasks of the task's team keep
// the keys they hold between them — the slots of the queue they stand in —
// and the keys are handed out in the new order, the task put right after
// `after` (or right before `before`) among the epic's children as drawn.
// Other epics' tasks never move. A task in work or done is no slot: it
// takes an ordinary key between its drawn neighbours.
func ReorderInEpicPlan(b Board, task, after, before string) ([]RankChange, error) {
	ix := NewIndex(b)
	c, ok := ix.Find(task)
	if !ok {
		return nil, fmt.Errorf("reorder in epic: %q is not on the board", task)
	}
	epic := ix.EpicOf(c)
	if epic == "" {
		return nil, fmt.Errorf("reorder in epic: %q is under no epic", task)
	}
	if !ix.InQueue(c) {
		s := Spot{After: after, Before: before}
		if after == "" && before == "" {
			return nil, nil
		}
		return SpotChanges(b, task, s)
	}
	team := ix.PlacedTeam(c)
	// The epic's children as drawn (board order), the task moved.
	var drawn []string
	for _, k := range ix.cards {
		if k.ItemID != task && ix.EpicOf(k) == epic && k.ReviewOf == "" && !IsPlanning(k) && !ix.IsSubtask(k) {
			drawn = append(drawn, k.ItemID)
		}
	}
	at := len(drawn)
	switch {
	case after != "":
		for i, id := range drawn {
			if id == after {
				at = i + 1
			}
		}
	case before != "":
		for i, id := range drawn {
			if id == before {
				at = i
			}
		}
	default:
		at = 0
	}
	order := append(append(append([]string{}, drawn[:at]...), task), drawn[at:]...)
	slot := map[string]bool{}
	var keys []string
	for _, t := range ix.Queue(team) {
		if ix.EpicOf(t) == epic && !b.Pinned[t.ItemID] {
			slot[t.ItemID] = true
			keys = append(keys, t.Rank)
		}
	}
	sort.Strings(keys)
	var plan []RankChange
	i := 0
	for _, id := range order {
		if !slot[id] {
			continue
		}
		t, _ := ix.Find(id)
		if t.Rank != keys[i] {
			plan = append(plan, RankChange{ID: id, Before: t.Rank, After: keys[i]})
		}
		i++
	}
	if keysTied(keys) {
		// Two slots with one key cannot be told apart: renumber the run
		// in the new order between the slots' neighbours instead.
		var block []Card
		for _, id := range order {
			if slot[id] {
				t, _ := ix.Find(id)
				block = append(block, t)
			}
		}
		q := ix.Queue(team)
		var rest []Card
		first := -1
		for _, t := range q {
			if slot[t.ItemID] {
				if first < 0 {
					first = len(rest)
				}
				continue
			}
			rest = append(rest, t)
		}
		return blockChanges(b, block, spotAtGap(rest, first))
	}
	return plan, nil
}

func keysTied(keys []string) bool {
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] || !ValidRank(keys[i]) {
			return true
		}
	}
	return len(keys) > 0 && !ValidRank(keys[0])
}

// Normalize gives every open task in a queue that has NO valid key — a
// direct writer's file without a `rank`, or with one no server mints — its
// default place (Q11): Place, task by task in (created, id) order, each on
// the board the previous ones left. Deterministic over a board, so two
// replicas write the same keys. A valid key a writer chose is kept.
func Normalize(b Board) ([]RankChange, error) {
	ix := NewIndex(b)
	var todo []Card
	for _, c := range ix.cards {
		if ix.InQueue(c) && !ValidRank(c.Rank) && !b.Pinned[c.ItemID] {
			todo = append(todo, c)
		}
	}
	sort.SliceStable(todo, func(i, j int) bool {
		if todo[i].CreatedAt != todo[j].CreatedAt {
			return todo[i].CreatedAt < todo[j].CreatedAt
		}
		return todo[i].ItemID < todo[j].ItemID
	})
	var plan []RankChange
	for _, c := range todo {
		ix := NewIndex(b)
		cur, _ := ix.Find(c.ItemID)
		changes, err := SpotChanges(b, c.ItemID, ix.PlaceSpot(cur, ix.PlacedTeam(cur)))
		if err != nil {
			return nil, err
		}
		plan = merge(plan, changes)
		b = applyChanges(b, changes)
	}
	return plan, nil
}

// PlaceInOrder places the given open parked tasks one by one, in the order
// given, by Place on the board the previous ones left — the cards of a
// repository that joins a board already at schema 4 (their keys meant the
// old tree order). Cards in no queue are left alone.
func PlaceInOrder(b Board, ids []string) ([]RankChange, error) {
	// Their old keys mean nothing now: no neighbour may be read off them.
	moving := map[string]bool{}
	for _, id := range ids {
		moving[id] = true
	}
	cur := b
	cur.Cards = make([]Card, len(b.Cards))
	copy(cur.Cards, b.Cards)
	for i := range cur.Cards {
		if moving[cur.Cards[i].ItemID] {
			cur.Cards[i].Rank = ""
		}
	}
	SortCards(cur.Cards)
	var plan []RankChange
	before := map[string]string{}
	for _, c := range b.Cards {
		if moving[c.ItemID] {
			before[c.ItemID] = c.Rank
		}
	}
	for _, id := range ids {
		ix := NewIndex(cur)
		c, ok := ix.Find(id)
		if !ok || !ix.InQueue(c) {
			continue
		}
		changes, err := SpotChanges(cur, id, ix.PlaceSpot(c, ix.PlacedTeam(c)))
		if err != nil {
			return nil, err
		}
		plan = merge(plan, changes)
		cur = applyChanges(cur, changes)
	}
	for i := range plan {
		if b, ok := before[plan[i].ID]; ok {
			plan[i].Before = b
		}
	}
	return plan, nil
}
