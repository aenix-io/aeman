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
