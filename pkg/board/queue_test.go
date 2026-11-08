package board

import (
	"strings"
	"testing"
)

// qboard builds an ordering fixture: epics and plan items by id, tasks
// under them, every card with a key, read in board order. A task is
// "id:parent:team" and parked unless the id starts with "w" (in work).
type qboard struct{ cards []Card }

func (q *qboard) plan(id, typ, parent, rank string) *qboard {
	q.cards = append(q.cards, Card{ItemID: id, Title: id, Type: typ, Parent: parent, Rank: rank})
	return q
}

func (q *qboard) epic(id, rank string) *qboard { return q.plan(id, TypeEpic, "", rank) }

func (q *qboard) task(id, parent, team, rank string) *qboard {
	c := Card{ItemID: id, Title: id, Parent: parent, Team: team, Rank: rank, Parked: true}
	if strings.HasPrefix(id, "w") {
		c.Parked, c.StartDate, c.Day = false, "2026-11-02", "2026-11-02"
	}
	q.cards = append(q.cards, c)
	return q
}

func (q *qboard) board() Board {
	cards := append([]Card(nil), q.cards...)
	SortCards(cards)
	return Board{Board: "b", Cards: cards}
}

func queueOf(b Board, team string) string {
	var ids []string
	for _, c := range NewIndex(b).Queue(team) {
		ids = append(ids, c.ItemID)
	}
	return strings.Join(ids, " ")
}

func mustApply(t *testing.T, b Board, changes []RankChange, err error) Board {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return ApplyRankChanges(b, changes)
}

// create adds a parked task and places it by Place, as every door does.
func create(t *testing.T, b Board, id, parent, team string) Board {
	t.Helper()
	b.Cards = append(b.Cards, Card{ItemID: id, Title: id, Parent: parent, Team: team, Parked: true})
	c, _ := Find(b, id)
	changes, err := Place(b, c, team)
	return mustApply(t, b, changes, err)
}

// Appendix A's examples, the epics A < B < C and neutral tasks L.
func TestBestGapContradictsTheEpicOrderLeast(t *testing.T) {
	order := EpicOrder{"A": 0, "B": 1, "C": 2}
	epicOf := func(c Card) string {
		if c.ItemID[0] == 'L' {
			return ""
		}
		return c.ItemID[:1]
	}
	q := func(ids string) []Card {
		var out []Card
		for _, id := range strings.Fields(ids) {
			out = append(out, Card{ItemID: id})
		}
		return out
	}
	for _, tc := range []struct {
		queue string
		want  int
	}{
		{"L1 L2", 2},       // nothing counted: the end of the queue
		{"A1 L1 L2 C1", 1}, // right after the earlier epic
		{"L1 L2 C1", 2},    // before the later epic, after the neutral run
		{"C9 A1 A2 C1", 3}, // the least contradiction
		{"A1 C1 A9", 1},    // the earliest minimal run
		{"", 0},            // an empty queue
	} {
		if got := BestGap(q(tc.queue), "B", order, epicOf); got != tc.want {
			t.Errorf("BestGap(%q, B) = %d, want %d", tc.queue, got, tc.want)
		}
	}
}

// Q3 — EVERY default position is Place: the case table of the rule.
func TestPlaceIsTheOnlyDefaultPosition(t *testing.T) {
	base := func() *qboard {
		return (&qboard{}).epic("A", "a").epic("B", "b").epic("C", "c").
			plan("I", TypeInitiative, "", "d").
			task("A1", "A", "T", "e").task("B1", "B", "T", "f").task("L1", "I", "T", "g").task("N1", "", "T", "h")
	}
	t.Run("after the epic's last parked task", func(t *testing.T) {
		b := create(t, base().board(), "A2", "A", "T")
		if got := queueOf(b, "T"); got != "A1 A2 B1 L1 N1" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("an empty epic by the epic order", func(t *testing.T) {
		b := create(t, base().board(), "C1", "C", "T")
		if got := queueOf(b, "T"); got != "A1 B1 C1 L1 N1" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("a neutral task after its plan parent's last", func(t *testing.T) {
		b := create(t, base().board(), "L2", "I", "T")
		if got := queueOf(b, "T"); got != "A1 B1 L1 L2 N1" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("otherwise the end of the queue", func(t *testing.T) {
		b := create(t, base().board(), "N2", "", "T")
		if got := queueOf(b, "T"); got != "A1 B1 L1 N1 N2" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("an empty queue is the end of the board", func(t *testing.T) {
		b := create(t, base().board(), "X1", "", "U")
		if got := queueOf(b, "U"); got != "X1" {
			t.Fatalf("queue = %s", got)
		}
		if last := b.Cards[len(b.Cards)-1].ItemID; last != "X1" {
			t.Fatalf("the last card of the board is %s", last)
		}
	})
}

// The brief's main scenario: epics prioritised while EMPTY, decomposed the
// next day — every task lands in its epic's area, in creation order, with
// nobody ordering anything by hand. A neutral task already there stays
// ahead: with nothing to contradict, an empty epic's area starts after the
// neutral run (Appendix A).
func TestTheFirstTaskOfAnEmptyEpicLandsByTheEpicOrder(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").epic("C", "c").task("N1", "", "T", "d").board()
	for _, step := range []struct{ id, epic string }{{"C1", "C"}, {"A1", "A"}, {"B1", "B"}, {"A2", "A"}, {"C2", "C"}, {"B2", "B"}} {
		b = create(t, b, step.id, step.epic, "T")
	}
	if got := queueOf(b, "T"); got != "N1 A1 A2 B1 B2 C1 C2" {
		t.Fatalf("queue = %s", got)
	}
}

// §6 of the brief: A1 B1 A2 C1, A3 made → A1 B1 A2 A3 C1.
func TestANewTaskLandsRightAfterTheEpicsLastBacklogTask(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").epic("C", "c").
		task("A1", "A", "T", "d").task("B1", "B", "T", "e").task("A2", "A", "T", "f").task("C1", "C", "T", "g").board()
	b = create(t, b, "A3", "A", "T")
	if got := queueOf(b, "T"); got != "A1 B1 A2 A3 C1" {
		t.Fatalf("queue = %s", got)
	}
}

// §4: B2 moved between A1 and A2 — any neighbours; the epic's key stays.
func TestMovingATaskBetweenEpicsLeavesTheEpicOrderAlone(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("A2", "A", "T", "d").task("B1", "B", "T", "e").task("B2", "B", "T", "f").board()
	changes, err := PlaceAfter(RankedOf(b.Cards), "B2", "A1")
	b = mustApply(t, b, changes, err)
	if got := queueOf(b, "T"); got != "A1 B2 A2 B1" {
		t.Fatalf("queue = %s", got)
	}
	if len(changes) != 1 || changes[0].ID != "B2" {
		t.Fatalf("changes = %+v, want B2's key alone", changes)
	}
}

// §3/§4: A1 B2 A2 B1, B moved above A → B2 B1 A1 A2; and a scattered epic
// is gathered with its tasks' order kept.
func TestMovingAnEpicGathersItsScatteredTasksKeepingTheirOrder(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("B2", "B", "T", "d").task("A2", "A", "T", "e").task("B1", "B", "T", "f").board()
	changes, err := MoveEpicPlan(b, "B", "")
	b = mustApply(t, b, changes, err)
	if got := queueOf(b, "T"); got != "B2 B1 A1 A2" {
		t.Fatalf("queue = %s", got)
	}
	again, err := MoveEpicPlan(b, "B", "")
	if err != nil || len(again) != 0 {
		t.Fatalf("a repeated move wrote %+v (%v), want nothing", again, err)
	}
}

// C4 — an epic move erases only its OWN tasks' exceptions: A's exception
// in the middle of C's tasks stays where it is when B moves.
func TestOtherEpicsExceptionsStayPut(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").epic("C", "c").
		task("C1", "C", "T", "d").task("A9", "A", "T", "e").task("C2", "C", "T", "f").task("B1", "B", "T", "g").task("A1", "A", "T", "h").board()
	changes, err := MoveEpicPlan(b, "B", "")
	b = mustApply(t, b, changes, err)
	q := queueOf(b, "T")
	if !strings.Contains(q, "C1 A9 C2") {
		t.Fatalf("queue = %s, A's exception moved", q)
	}
	for _, ch := range changes {
		if ch.ID != "B" && ch.ID != "B1" {
			t.Fatalf("%s was rewritten by B's move: %+v", ch.ID, changes)
		}
	}
}

func TestTasksInWorkDoNotMoveWithTheEpic(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("wB", "B", "T", "d").task("B1", "B", "T", "e").board()
	changes, err := MoveEpicPlan(b, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range changes {
		if ch.ID == "wB" {
			t.Fatalf("the task in work was re-keyed: %+v", changes)
		}
	}
}

// §5: A1 B2 A2 B1 A3, A3 moved first inside A on the Strategy board → A3 B2
// A1 B1 A2: A's tasks take the slots they held.
func TestReorderingInsideAnEpicKeepsTheQueueSlots(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("B2", "B", "T", "d").task("A2", "A", "T", "e").task("B1", "B", "T", "f").task("A3", "A", "T", "g").board()
	changes, err := ReorderInEpicPlan(b, "A3", "", "A1")
	b = mustApply(t, b, changes, err)
	if got := queueOf(b, "T"); got != "A3 B2 A1 B1 A2" {
		t.Fatalf("queue = %s", got)
	}
}

// C2 — the permutation is within ONE team: another team's tasks of the epic
// keep their keys.
func TestReorderingInsideAnEpicStaysInsideTheTeam(t *testing.T) {
	b := (&qboard{}).epic("A", "a").
		task("A1", "A", "T", "c").task("U1", "A", "U", "d").task("A2", "A", "T", "e").board()
	changes, err := ReorderInEpicPlan(b, "A2", "", "A1")
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range changes {
		if ch.ID == "U1" {
			t.Fatalf("the other team's task moved: %+v", changes)
		}
	}
	b = ApplyRankChanges(b, changes)
	if got := queueOf(b, "T"); got != "A2 A1" {
		t.Fatalf("queue = %s", got)
	}
}

// Q6 — a task dropped into another epic is re-parented and placed in the
// new epic's area.
func TestDroppingATaskIntoAnotherEpicPlacesItInThatEpicsArea(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("B1", "B", "T", "d").task("A2", "A", "T", "e").task("N1", "", "T", "f").board()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "A2" {
			b.Cards[i].Parent = "B"
		}
	}
	c, _ := Find(b, "A2")
	changes, err := Place(b, c, "T")
	b = mustApply(t, b, changes, err)
	if got := queueOf(b, "T"); got != "A1 B1 A2 N1" {
		t.Fatalf("queue = %s", got)
	}
}

// Q8 — the four examples of ordering.md, with A < B < C.
func TestAReturnedTaskKeepsItsKeyOnlyInsideItsEpicsArea(t *testing.T) {
	ret := func(b Board, id string) string {
		t.Helper()
		for i := range b.Cards {
			if b.Cards[i].ItemID == id {
				b.Cards[i].Parked, b.Cards[i].StartDate, b.Cards[i].Day = true, "", ""
			}
		}
		c, _ := Find(b, id)
		changes, err := ReturnChanges(b, c, "T")
		return queueOf(mustApply(t, b, changes, err), "T")
	}
	t.Run("returned an hour later to an unchanged queue", func(t *testing.T) {
		b := (&qboard{}).epic("A", "a").epic("B", "b").
			task("wA1", "A", "T", "c").task("A2", "A", "T", "d").task("A3", "A", "T", "e").task("B1", "B", "T", "f").board()
		if got := ret(b, "wA1"); got != "wA1 A2 A3 B1" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("B has since moved above A", func(t *testing.T) {
		b := (&qboard{}).epic("B", "a").epic("A", "b").
			task("wA1", "A", "T", "c").task("B1", "B", "T", "d").task("B2", "B", "T", "e").task("A2", "A", "T", "f").task("A3", "A", "T", "g").board()
		if got := ret(b, "wA1"); got != "B1 B2 A2 A3 wA1" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("inside A's area between A2 and B1", func(t *testing.T) {
		b := (&qboard{}).epic("A", "a").epic("B", "b").
			task("A2", "A", "T", "c").task("wA1", "A", "T", "d").task("B1", "B", "T", "e").task("A3", "A", "T", "f").board()
		if got := ret(b, "wA1"); got != "A2 wA1 B1 A3" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("no other A task, a minimal-cost gap is kept", func(t *testing.T) {
		b := (&qboard{}).epic("A", "a").epic("B", "b").epic("C", "c").
			task("wA1", "A", "T", "d").task("B1", "B", "T", "e").task("C1", "C", "T", "f").board()
		if got := ret(b, "wA1"); got != "wA1 B1 C1" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("no other A task, and the epic order changed: bestGap", func(t *testing.T) {
		b := (&qboard{}).epic("B", "a").epic("A", "b").epic("C", "c").
			task("wA1", "A", "T", "d").task("B1", "B", "T", "e").task("C1", "C", "T", "f").board()
		if got := ret(b, "wA1"); got != "B1 wA1 C1" {
			t.Fatalf("queue = %s", got)
		}
	})
}

// Q9 — a parked task handed to another team lands after its epic's last
// task in THAT team's queue.
func TestATaskHandedToAnotherTeamLandsAfterItsEpicsLastTaskThere(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A9", "A", "T", "c").task("A1", "A", "U", "d").task("B1", "B", "U", "e").board()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "A9" {
			b.Cards[i].Team = "U"
		}
	}
	c, _ := Find(b, "A9")
	changes, err := Place(b, c, "U")
	b = mustApply(t, b, changes, err)
	if got := queueOf(b, "U"); got != "A1 A9 B1" {
		t.Fatalf("queue = %s", got)
	}
}

// Q10 — the "+" inherits the epic of its context: four cases.
func TestThePlusInheritsTheEpicOfItsContext(t *testing.T) {
	for _, tc := range []struct {
		name               string
		above, below       string
		hasAbove, hasBelow bool
		want               string
	}{
		{"the same epic above and below", "A", "A", true, true, "A"},
		{"a boundary: the card above's", "", "B", true, true, ""},
		{"the top: the card below's", "", "B", false, true, "B"},
		{"an empty list: none", "", "", false, false, ""},
	} {
		if got := InheritedEpic(tc.above, tc.below, tc.hasAbove, tc.hasBelow); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Q7 — an initiative's move rewrites its own key only.
func TestMovingAnInitiativeRewritesNoEpicOrTask(t *testing.T) {
	b := (&qboard{}).plan("I1", TypeInitiative, "", "a").plan("I2", TypeInitiative, "", "b").
		plan("A", TypeEpic, "I1", "c").plan("B", TypeEpic, "I2", "d").
		task("A1", "A", "T", "e").task("B1", "B", "T", "f").board()
	changes, err := PlaceAfter(RankedOf(b.Cards), "I2", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].ID != "I2" {
		t.Fatalf("changes = %+v", changes)
	}
	if got := queueOf(ApplyRankChanges(b, changes), "T"); got != "A1 B1" {
		t.Fatalf("queue = %s", got)
	}
}

// Dependencies never move a card: Place reads no lock.
func TestALockNeverChangesTheOrder(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("B1", "B", "T", "d").board()
	for i := range b.Cards {
		if b.Cards[i].ItemID == "A1" {
			b.Cards[i].DependsOn = []string{"B1"}
		}
	}
	b = create(t, b, "A2", "A", "T")
	if got := queueOf(b, "T"); got != "A1 A2 B1" {
		t.Fatalf("queue = %s", got)
	}
}

// A multi-team epic gathers in EACH team's queue.
func TestAMultiTeamEpicGathersInEachTeamsQueue(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("B1", "B", "T", "d").task("A2", "A", "T", "e").
		task("B9", "B", "U", "f").task("A8", "A", "U", "g").task("B8", "B", "U", "h").task("A9", "A", "U", "i").board()
	changes, err := MoveEpicPlan(b, "B", "")
	b = mustApply(t, b, changes, err)
	if got := queueOf(b, "T"); got != "B1 A1 A2" {
		t.Fatalf("T = %s", got)
	}
	if got := queueOf(b, "U"); got != "B9 B8 A8 A9" {
		t.Fatalf("U = %s", got)
	}
}

// The epic order is a parameter per team (the extension point): today
// every team reads the same global order.
func TestEpicOrderIsAParameterPerTeam(t *testing.T) {
	b := (&qboard{}).epic("B", "a").epic("A", "b").board()
	ix := NewIndex(b)
	for _, team := range []string{"T", "U", ""} {
		o := ix.EpicOrderFor(team)
		if o["B"] != 0 || o["A"] != 1 {
			t.Fatalf("team %q reads %v", team, o)
		}
	}
}

// Q11 — normalisation: a task with no key, or one no server mints, is
// placed by Place, in (created, id) order; a valid key a writer chose is
// kept; the same board always gives the same keys.
func TestNormalizePlacesUnrankedTasksDeterministically(t *testing.T) {
	seed := func() Board {
		b := (&qboard{}).epic("A", "a").epic("B", "b").
			task("A1", "A", "T", "c").task("B1", "B", "T", "d").board()
		b.Cards = append(b.Cards,
			Card{ItemID: "A2", Parent: "A", Team: "T", Parked: true, CreatedAt: "2026-11-01T00:00:00Z"},
			Card{ItemID: "B2", Parent: "B", Team: "T", Parked: true, Rank: "BAD", CreatedAt: "2026-11-02T00:00:00Z"},
			Card{ItemID: "A3", Parent: "A", Team: "T", Parked: true, Rank: "z", CreatedAt: "2026-11-03T00:00:00Z"})
		SortCards(b.Cards)
		return b
	}
	one, err := Normalize(seed())
	if err != nil {
		t.Fatal(err)
	}
	two, _ := Normalize(seed())
	if len(one) != 2 || len(two) != 2 || one[0] != two[0] || one[1] != two[1] {
		t.Fatalf("normalisation = %+v then %+v, want two identical changes", one, two)
	}
	b := ApplyRankChanges(seed(), one)
	if got := queueOf(b, "T"); got != "A1 B1 B2 A3 A2" {
		t.Fatalf("queue = %s", got)
	}
	if again, _ := Normalize(b); len(again) != 0 {
		t.Fatalf("a second pass wrote %+v", again)
	}
}

// Split reports an epic with another task inside its area.
func TestSplitIsAnotherTaskInsideTheEpicsArea(t *testing.T) {
	b := (&qboard{}).epic("A", "a").epic("B", "b").
		task("A1", "A", "T", "c").task("B1", "B", "T", "d").task("A2", "A", "T", "e").board()
	ix := NewIndex(b)
	if !ix.Split("A", "T") || ix.Split("B", "T") || ix.Split("A", "U") {
		t.Fatalf("split: A %v, B %v, A on U %v", ix.Split("A", "T"), ix.Split("B", "T"), ix.Split("A", "U"))
	}
}
