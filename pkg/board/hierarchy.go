package board

// The hierarchy above the day boards — goal, initiative, epic — is made of
// CARDS, told apart by one field, `type`, and linked by the ONE link the
// board already had, `parent` (docs/design/hierarchy.md). A task whose parent
// is a task is a subtask, exactly as before; a task whose parent is an epic
// is simply a task under an epic — not hidden, inheriting nothing, riding
// nowhere. Every rule that used to read "a card with a parent" reads "a task
// whose parent is a task" now, and the helpers here are how it asks.

// The card types. A task is the absence of one: the day boards' own kind,
// and the only kind the boards before this one ever drew.
const (
	TypeTask       = ""
	TypeEpic       = "epic"
	TypeInitiative = "initiative"
	TypeGoal       = "goal"
)

// The derived status of a planning card (HierarchyStatus): never stored.
// Four words. A card with NOTHING under it is not waiting for work, it has
// none (no items); work under it in hand is in progress; open work under it
// all waiting in backlogs is BACKLOG; done when every task is.
const (
	HierarchyNoItems    = "no-items"
	HierarchyBacklog    = "backlog"
	HierarchyInProgress = "in-progress"
	HierarchyDone       = "done"
)

// Level orders the types: a parent is always of a HIGHER level than its
// child, which is what makes the link point up and a cycle impossible to
// write. An unknown type is read as a task — the lowest level — so a value a
// direct writer invented can be a parent of nothing but a subtask.
func Level(typ string) int {
	switch typ {
	case TypeGoal:
		return 4
	case TypeInitiative:
		return 3
	case TypeEpic:
		return 2
	}
	return 1
}

// ValidType reports a type a card may be created with: a task, or one of
// the three planning levels.
func ValidType(typ string) bool {
	switch typ {
	case TypeTask, TypeEpic, TypeInitiative, TypeGoal:
		return true
	}
	return false
}

// IsPlanning reports a goal, an initiative or an epic — a card the Strategy board
// draws and no day board does (H3).
func IsPlanning(c Card) bool { return c.Type != TypeTask }

// Find is a card by id. Linear: the board is a slice, and a lookup that
// happens once per request is not the place for an index (PlanningIDs is
// what a loop over the board uses).
func Find(b Board, id string) (Card, bool) {
	if id == "" {
		return Card{}, false
	}
	for _, c := range b.Cards {
		if c.ItemID == id {
			return c, true
		}
	}
	return Card{}, false
}

// ParentOf is the card a card's `parent` names, if it is on the board.
func ParentOf(b Board, c Card) (Card, bool) {
	return Find(b, c.Parent)
}

// PlanningIDs is the set of planning cards, for the loops that ask of every
// card whether it is a subtask: one pass to build, and the question costs
// nothing after it. Planning cards are few — dozens on a board of
// thousands — so the set stays small.
func PlanningIDs(b Board) map[string]bool {
	out := map[string]bool{}
	for _, c := range b.Cards {
		if IsPlanning(c) {
			out[c.ItemID] = true
		}
	}
	return out
}

// IsSubtaskIn answers IsSubtask against a set PlanningIDs built: a card with
// a parent that is NOT a planning card. A read board holds no DANGLING
// parent — the reading drops one (dropDanglingParents) — so an unknown
// parent is met only on a board nobody read: a cache between a delete and
// its reload, a hand-built board. There it reads as a subtask, which is what
// it always read as.
func IsSubtaskIn(c Card, planning map[string]bool) bool {
	return c.Parent != "" && !planning[c.Parent]
}

// IsSubtask reports a task whose parent is a task (or is not here): the one
// kind of card that rides another — hidden from the boards, drawn under its
// parent, inheriting its team, person and sprint.
func IsSubtask(b Board, c Card) bool {
	if c.Parent == "" {
		return false
	}
	p, ok := Find(b, c.Parent)
	return !ok || !IsPlanning(p)
}

// Lineage is every ancestor of a card, ROOT FIRST — the breadcrumb a detail
// pane draws above it, a task parent included. The walk is bounded: it stops
// at a parent that is not here, on a repeat (a cycle written straight into
// the repository must not hang a reader), and after more hops than the
// levels allow.
//
// The one-card form of Index.Lineage: it reads the board once for the
// answer. A loop over a board asking it of every card builds an Index first.
func Lineage(b Board, c Card) []Card { return NewIndex(b).Lineage(c) }

// HasPlanningAncestor reports a card with a goal, an initiative or an epic
// somewhere above it — which is what puts a task or a subtask on the
// Strategy.
func HasPlanningAncestor(b Board, c Card) bool { return NewIndex(b).HasPlanningAncestor(c) }

// ChildrenOf is every card whose parent is the given one, planning cards and
// tasks alike, in board order. (Children, in subtasks.go, is the narrower
// question: the SUBTASKS of a task.)
func ChildrenOf(b Board, id string) []Card { return NewIndex(b).ChildrenOf(id) }

// LeafTasks is every task under a planning card, through every level: a
// task whose parent is a planning card in the subtree. A subtask is NOT one
// of them — its parent task answers for it through its own derived progress
// — and neither is anything under a task, nor a review card (a copy of the
// card it reviews, made for the asking; the original answers for the work).
// A task has no leaf tasks: the question is a planning card's. The list is
// in BOARD order, whatever level a task hangs off.
func LeafTasks(b Board, id string) []Card { return NewIndex(b).LeafTasks(id) }

// HierarchyStatus is a planning card's standing, derived from its leaf tasks
// and never stored (H5): NO ITEMS when nothing is under it; DONE when every
// leaf task is complete; IN PROGRESS when any open one is in work (a date or
// a sprint, not parked); BACKLOG when there are open ones and none is in
// work (amended with K16: progress, a person or a stage alone do not count).
// The counts are what
// the Strategy board shows beside the word. A task answers "" — it has a stage and
// a progress of its own.
func HierarchyStatus(b Board, c Card) (status string, done, total int) {
	return NewIndex(b).HierarchyStatus(c)
}

// PlanningResolver is the optional half of a DomainResolver: whether a card
// is a planning card. The domain rule needs it — a subtask lives where its
// parent lives, a task under an epic where its own team does —
// and a resolver that cannot answer is read as "every parent is a task",
// which is what every parent was before this field existed.
type PlanningResolver interface {
	CardIsPlanning(id string) bool
}

func (r boardResolver) CardIsPlanning(id string) bool {
	c, ok := Find(r.b, id)
	return ok && IsPlanning(c)
}
