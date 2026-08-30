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
	if board.MirrorAllowed(b, "engineering", "strategy") {
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
