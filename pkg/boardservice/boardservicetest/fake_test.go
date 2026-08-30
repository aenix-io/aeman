package boardservicetest

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The fake this package publishes is what an external tool tests its own
// board logic against (docs/embedding.md), so it has to reproduce the
// board the service sees — domains included. Without them every
// cross-repository rule passes silently here while the real service
// refuses, which is worse than having no fake at all.
func TestTheFakeRecordsWhichRepositoryDeclaredEachEntry(t *testing.T) {
	f := New([]board.Card{
		{ItemID: "pr-e", Title: board.ProjectStateTitle, Project: "engineering"},
		{ItemID: "pr-s", Title: board.ProjectStateTitle, Project: "strategy", Domain: "founders"},
		{ItemID: "ep-f", Title: board.EpicStateTitle, Epic: "Fundraising", Project: "strategy", Domain: "founders"},
	}, nil)
	b, err := f.LoadBoard(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got := board.ProjectDomain(b, "strategy"); got != "founders" {
		t.Fatalf("the closed project keeps its repository: %q", got)
	}
	if got := board.ProjectDomain(b, "engineering"); got != "" {
		t.Fatalf("the primary is the default and names none: %q", got)
	}
	if cd, ok := board.ColumnDomain(b, "strategy", "Fundraising"); !ok || cd != "founders" {
		t.Fatalf("a column carries the repository it was declared in: %q %v", cd, ok)
	}
	if d, _ := board.ColumnDomain(b, "strategy", "Fundraising"); d != "founders" {
		t.Fatal("and the domain rules can finally be exercised through this fake")
	}
}

// A team's repository is recorded on its sprint-state card, and the domain
// rule that pairs a card's team with its project (G46) is the one an
// external tool is most likely to trip over — so the fake has to split
// those cards the way board.NewBoard does, or TeamDomain answers "" for
// every team and the rule passes silently here while the service refuses.
func TestTheFakeRecordsATeamsRepository(t *testing.T) {
	f := New([]board.Card{
		{ItemID: "st-p", Title: board.SprintStateTitle, Team: "platform", SprintStart: "2026-08-24"},
		{ItemID: "st-f", Title: board.SprintStateTitle, Team: "founders", SprintStart: "2026-08-24", Domain: "founders"},
	}, nil)
	b, err := f.LoadBoard(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got := board.TeamDomain(b, "founders"); got != "founders" {
		t.Fatalf("the closed team keeps its repository: %q", got)
	}
	if got := board.TeamDomain(b, "platform"); got != "" {
		t.Fatalf("the primary is the default and names none: %q", got)
	}
	if st, ok := b.SprintStates["platform"]; !ok || st.Current != "2026-08-24" {
		t.Fatalf("a sprint-state card seeds the team's sprint: %+v", b.SprintStates)
	}
	for _, c := range b.Cards {
		if board.IsStateTitle(c.Title) {
			t.Fatalf("a state card is not a board row: %+v", c)
		}
	}
}

// The no-team group is a real group ("" is a team key like any other), and
// board order comes from the state cards' positions — a fake that drops
// either answers differently from the board it stands in for.
func TestTheFakeKeepsTheNoTeamGroupAndTheBoardOrder(t *testing.T) {
	f := New([]board.Card{
		{ItemID: "st-none", Title: board.SprintStateTitle, SprintStart: "2026-08-24"},
		{ItemID: "st-p", Title: board.SprintStateTitle, Team: "platform", SprintStart: "2026-08-24"},
	}, nil)
	b, err := f.LoadBoard(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := b.SprintStates[""]; !ok || st.Current != "2026-08-24" {
		t.Fatalf("the no-team group keeps its sprint: %+v", b.SprintStates)
	}
	if len(b.TeamOrder) != 2 || b.TeamOrder[0] != "" || b.TeamOrder[1] != "platform" {
		t.Fatalf("board order is the state cards' order: %+v", b.TeamOrder)
	}
}

// The same seed must give the same board, call after call. The sprint
// states are a MAP, and turning them into cards in map order made the
// board's team order — and therefore every rank derived from it — differ
// between runs: a migration over this fake produced two different trees
// from one source, which is exactly what its determinism test forbids.
func TestTheFakeHandsOutTheSameBoardEveryTime(t *testing.T) {
	f := New(nil, map[string]board.SprintState{
		"platform":   {Current: "2026-08-24", ItemID: "st-p"},
		"founders":   {Current: "2026-08-24", ItemID: "st-f"},
		"backoffice": {Current: "2026-08-24", ItemID: "st-b"},
		"":           {Current: "2026-08-24", ItemID: "st-none"},
	})
	first, err := f.LoadBoard(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		again, err := f.LoadBoard(t.Context(), "acme")
		if err != nil {
			t.Fatal(err)
		}
		if len(again.TeamOrder) != len(first.TeamOrder) {
			t.Fatalf("team count changed: %v vs %v", first.TeamOrder, again.TeamOrder)
		}
		for j := range first.TeamOrder {
			if first.TeamOrder[j] != again.TeamOrder[j] {
				t.Fatalf("board order is not stable: %v vs %v", first.TeamOrder, again.TeamOrder)
			}
		}
	}
}

// The fake can model the board a real store hands over: primary named,
// every entry stamped with its domain's name. Without that an external
// tool's test of the column rules answers in a namespace the server never
// uses, and passes where production refuses.
func TestTheFakeCanModelAStampedBoard(t *testing.T) {
	f := New([]board.Card{
		{ItemID: "ep-inbox", Title: board.EpicStateTitle, Epic: "Inbox"},
	}, nil).InRepository("aeman-db", map[string]string{"ep-inbox": "aeman-db"})
	b, err := f.LoadBoard(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if b.Primary != "aeman-db" {
		t.Fatalf("the board names its primary: %q", b.Primary)
	}
	cd, ok := board.ColumnDomain(b, "", "Inbox")
	if !ok || cd != "aeman-db" {
		t.Fatalf("the column carries the stamp: %q %v", cd, ok)
	}
	if got := board.HomeDomain(b, board.Card{Epic: "Inbox"}); got != cd {
		t.Fatalf("and a card nothing places belongs to the same repository: %q vs %q", got, cd)
	}
}

// The stamps have to be there BEFORE the assembly runs, not after it: the
// assembly's own rules read them. declaredMirrors is the sharpest of them
// — it keeps a mirror only while the column it names is in the repository
// that holds the card — so a fake that stamped afterwards judged every
// mirror in an empty namespace, where all columns agree, and kept the very
// placement the store drops on the next load.
func TestTheFakeAppliesItsStampsBeforeTheAssembly(t *testing.T) {
	f := New([]board.Card{
		{ItemID: "ep-inbox", Title: board.EpicStateTitle, Epic: "Inbox"},
		{ItemID: "ep-fund", Title: board.EpicStateTitle, Epic: "Fundraising", Project: "strategy"},
		{ItemID: "c1", Title: "shared", Epic: "Inbox", Mirrors: []board.Placement{
			{Project: "strategy", Epic: "Fundraising"},
		}},
	}, nil).InRepository("aeman-db", map[string]string{
		"ep-inbox": "aeman-db",
		"ep-fund":  "founders",
	})
	b, err := f.LoadBoard(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := findCard(b, "c1")
	if !ok {
		t.Fatal("the card is on the board")
	}
	if board.Mirrored(c, "strategy", "Fundraising") {
		t.Fatalf("a mirror onto another repository's column is not a placement this card can have: %+v", c.Mirrors)
	}
}

// The store stamps CARDS with their repository's name too, the primary's
// included (gitstore.stamp), and rules that ask where a card's own file
// lives read that stamp raw — the process tie is one. A fake that left the
// cards unstamped beside a named primary answered "" to those rules and
// refused what the store accepts.
func TestTheFakeStampsCardsWithTheirRepository(t *testing.T) {
	f := New([]board.Card{
		{ItemID: "c1", Title: "in the primary"},
		{ItemID: "c2", Title: "in the closed one", Domain: "founders"},
	}, nil).InRepository("aeman-db", nil)
	b, err := f.LoadBoard(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	c1, _ := findCard(b, "c1")
	if c1.Domain != "aeman-db" {
		t.Fatalf("a card of the primary carries its name: %q", c1.Domain)
	}
	c2, _ := findCard(b, "c2")
	if c2.Domain != "founders" {
		t.Fatalf("and a card of another repository keeps its own: %q", c2.Domain)
	}
}

func findCard(b board.Board, id string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ItemID == id {
			return c, true
		}
	}
	return board.Card{}, false
}
