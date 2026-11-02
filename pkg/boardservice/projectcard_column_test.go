package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A project card cannot exist without a project: it is born in a column and
// dies there. Taking its column AWAY would leave a card with no team, no
// owner, no band, no plan and no column — a card on no board anyone can
// open — so the write is refused and the person deletes the card instead.
// Moving it to ANOTHER column is what re-filing means and stays free; a task
// that stands in no column is unaffected by an empty scope, which asks for
// nothing. (The × that once "orphaned" a worked card into the working area
// went with this rule: the SPA deletes, and nothing else is offered.)
func TestAProjectCardLivesAndDiesInItsColumn(t *testing.T) {
	f := columnBoard([]board.Card{
		{ItemID: "pc", Title: "a plan", Project: "engineering", Scope: "Cozystack", Progress: 40},
		{ItemID: "task", Title: "a task", Team: "platform"},
	})
	svc := New(f)
	// Taking the column away is refused, however worked the card is.
	if err := svc.SetScope(ctx, "acme", "pc", "", nil); !errors.Is(err, ErrProjectCard) {
		t.Fatalf("a project card cannot leave its column — it is deleted instead: %v", err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if c, _ := findCard(b, "pc"); c.Scope != "Cozystack" || c.Project != "engineering" {
		t.Fatalf("the refusal must not have moved it: %+v", c)
	}
	// Re-filing into another column — here another project's, both in the
	// primary — is still free.
	freedom := "freedom"
	if err := svc.SetScope(ctx, "acme", "pc", "Launch", &freedom); err != nil {
		t.Fatalf("moving between columns is what re-filing means: %v", err)
	}
	b, _ = f.LoadBoard(ctx, "acme")
	if c, _ := findCard(b, "pc"); c.Scope != "Launch" || c.Project != "freedom" {
		t.Fatalf("moved: %+v", c)
	}
	// An empty scope on a card in no column asks for nothing and refuses nothing.
	if err := svc.SetScope(ctx, "acme", "task", "", nil); err != nil {
		t.Fatalf("a task outside every column is not a project card: %v", err)
	}
}
