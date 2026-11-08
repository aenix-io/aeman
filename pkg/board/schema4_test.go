package board

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// assertSchema4Preserves checks every order a person reads on a schema-3
// board reads the same once the plan's keys are written: each chain of
// Schema4Orders (every backlog and any mix, every parent's children, every
// level's roots, the flat epic order) stands in the same order in the new
// board order — and the new BacklogOrder of every team is the old one.
func assertSchema4Preserves(t *testing.T, b Board) []RankChange {
	t.Helper()
	changes, _, err := Schema4Plan(b)
	if err != nil {
		t.Fatal(err)
	}
	old := schema3Order(b)
	oldIx := NewIndex(Board{Cards: old})
	after := ApplyRankChanges(Board{Cards: append([]Card(nil), old...)}, changes)
	for _, c := range after.Cards {
		if !ValidRank(c.Rank) {
			t.Fatalf("%s has no valid key after the migration: %q", c.ItemID, c.Rank)
		}
	}
	for i := 1; i < len(after.Cards); i++ {
		if after.Cards[i-1].Rank >= after.Cards[i].Rank {
			t.Fatalf("keys not strictly increasing: %s %q, %s %q", after.Cards[i-1].ItemID, after.Cards[i-1].Rank, after.Cards[i].ItemID, after.Cards[i].Rank)
		}
	}
	pos := map[string]int{}
	for i, c := range after.Cards {
		pos[c.ItemID] = i
	}
	for _, chain := range Schema4Orders(oldIx, old) {
		for i := 1; i < len(chain); i++ {
			if pos[chain[i-1]] > pos[chain[i]] {
				t.Fatalf("order lost: %s now after %s (chain %v)", chain[i-1], chain[i], chain)
			}
		}
	}
	// Every team's backlog, and all teams mixed, read as they did.
	newIx := NewIndex(after)
	teams := map[string]bool{"": true}
	for _, c := range old {
		teams[oldIx.PlacedTeam(c)] = true
	}
	read := func(ix *Index, cards []Card, keepTeam func(string) bool, order func(*Index, []Card) []Card) string {
		var listed []Card
		for _, c := range cards {
			if ix.OnBacklog(c) && keepTeam(ix.PlacedTeam(c)) {
				listed = append(listed, c)
			}
		}
		return strings.Join(ids(order(ix, listed)), " ")
	}
	for team := range teams {
		one := func(t2 string) bool { return t2 == team }
		if was, now := read(oldIx, old, one, Schema3BacklogOrder), read(newIx, after.Cards, one, BacklogOrder); was != now {
			t.Fatalf("team %q backlog\n was %s\n now %s", team, was, now)
		}
	}
	all := func(string) bool { return true }
	if was, now := read(oldIx, old, all, Schema3BacklogOrder), read(newIx, after.Cards, all, BacklogOrder); was != now {
		t.Fatalf("all teams mixed\n was %s\n now %s", was, now)
	}
	return changes
}

// Q14 — examples: mixed initiative children, loose tasks last, reviews first,
// empty epics, and keys with ties, blanks and invalid ones.
func TestSchema4PreservesTheOldOrdersOnExamples(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "loose", Parked: true, Rank: "a"},
		{ItemID: "t-e2", Parent: "e2", Parked: true, Rank: "b", Team: "T"},
		{ItemID: "g2", Type: TypeGoal, Rank: "c"},
		{ItemID: "e2", Type: TypeEpic, Parent: "i1", Rank: "d"},
		{ItemID: "r", ReviewOf: "t-e1", Parked: true, Rank: "e"},
		{ItemID: "g1", Type: TypeGoal, Rank: "f"},
		{ItemID: "i1", Type: TypeInitiative, Parent: "g1", Rank: ""},
		{ItemID: "e1", Type: TypeEpic, Parent: "i1", Rank: "g"},
		{ItemID: "t-i1", Parent: "i1", Parked: true, Rank: "g", Team: "T"}, // a tie
		{ItemID: "t-e1", Parent: "e1", Parked: true, Rank: "BAD", Team: "U"},
		{ItemID: "e-empty", Type: TypeEpic, Parent: "g2", Rank: "h"},
		{ItemID: "w", Parent: "e1", StartDate: "2026-11-02", Day: "2026-11-02", Team: "T", Rank: "i"},
	})
	changes := assertSchema4Preserves(t, b)
	if len(changes) == 0 {
		t.Fatal("the tree order differs from the key order: something must be re-keyed")
	}
}

// Appendix D — the cycle one key cannot break: the backlog, two Strategy
// parents and a Team cell. Backlog and Strategy are kept; the Team cell
// changes, and is reported.
func TestSchema4ReportsATeamCellItMustChange(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "A", Type: TypeEpic, Rank: "1"},
		{ItemID: "B", Type: TypeEpic, Rank: "2"},
		{ItemID: "xb", Parent: "B", Team: "T", Parked: true, Rank: "a"},                                                         // x′, parked B
		{ItemID: "yb", Parent: "B", Team: "T", Assignees: []string{"p"}, Rank: "b", StartDate: "2026-11-02", Day: "2026-11-02"}, // y, B in work
		{ItemID: "ya", Parent: "A", Team: "T", Assignees: []string{"p"}, Rank: "c", StartDate: "2026-11-02", Day: "2026-11-02"}, // y′, A in work
		{ItemID: "xa", Parent: "A", Team: "T", Parked: true, Rank: "d"},                                                         // x, parked A
	})
	assertSchema4Preserves(t, b)
	_, cells, err := Schema4Plan(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 1 || cells[0].Team != "T" || cells[0].Person != "p" {
		t.Fatalf("cells = %+v, want the one Team cell the cycle forces", cells)
	}
}

// Q14 — the generated property test: random forests, teams, states and keys
// (ties, blanks, invalid ones); every order is kept; and where the
// constraints let them, the tasks in work keep their order.
func TestSchema4PreservesOrderOnGeneratedBoards(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		b := generatedBoard(rand.New(rand.NewSource(seed)))
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			changes := assertSchema4Preserves(t, b)
			again, _, _ := Schema4Plan(b)
			if fmt.Sprint(changes) != fmt.Sprint(again) {
				t.Fatal("the plan is not a function of the board")
			}
		})
	}
}

func generatedBoard(r *rand.Rand) Board {
	keys := []string{"", "", "a", "b", "b", "c", "d", "e", "f", "g", "h", "i", "BAD", "x0", "m", "n", "q"}
	key := func() string { return keys[r.Intn(len(keys))] + fmt.Sprint(r.Intn(3)+1) }
	teams := []string{"", "T", "U", "V"}
	var cards []Card
	add := func(c Card) string {
		c.ItemID = fmt.Sprintf("c%03d", len(cards))
		switch {
		case r.Intn(6) == 0:
			c.Rank = "" // a direct writer's card with no key
		case r.Intn(3) == 0:
			c.Rank = longKey(r, 22) // the "zzzz…" keys a real board grows
		default:
			c.Rank = key()
		}
		cards = append(cards, c)
		return c.ItemID
	}
	var goals, inits, epics, tasks []string
	for i := 0; i < 1+r.Intn(3); i++ {
		goals = append(goals, add(Card{Type: TypeGoal}))
	}
	pick := func(xs []string) string {
		if len(xs) == 0 || r.Intn(4) == 0 {
			return ""
		}
		return xs[r.Intn(len(xs))]
	}
	for i := 0; i < 1+r.Intn(4); i++ {
		inits = append(inits, add(Card{Type: TypeInitiative, Parent: pick(goals)}))
	}
	for i := 0; i < 2+r.Intn(5); i++ {
		parent := pick(inits)
		if parent == "" {
			parent = pick(goals)
		}
		epics = append(epics, add(Card{Type: TypeEpic, Parent: parent, Team: teams[r.Intn(len(teams))]}))
	}
	for i := 0; i < 5+r.Intn(25); i++ {
		parent := pick(epics)
		if parent == "" {
			parent = pick(inits)
		}
		c := Card{Parent: parent, Team: teams[r.Intn(len(teams))]}
		switch r.Intn(4) {
		case 0, 1:
			c.Parked = true
		case 2:
			c.StartDate, c.Day = "2026-11-02", "2026-11-02"
			c.Assignees = []string{[]string{"p", "q"}[r.Intn(2)]}
			c.Zone = ZoneKey([]string{"yellow", "green"}[r.Intn(2)])
		default:
			c.StartDate, c.Day, c.Progress = "2026-10-02", "2026-10-02", 100
		}
		tasks = append(tasks, add(c))
	}
	for i := 0; i < r.Intn(4); i++ {
		if len(tasks) == 0 {
			break
		}
		add(Card{ReviewOf: tasks[r.Intn(len(tasks))], Parked: true})
	}
	for i := 0; i < r.Intn(3); i++ {
		if len(tasks) == 0 {
			break
		}
		add(Card{Parent: tasks[r.Intn(len(tasks))], StartDate: "2026-11-02", Day: "2026-11-02"})
	}
	return NewBoard(cards)
}
