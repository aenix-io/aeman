package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// RenameTeam renames the team where it is declared and rewrites every card
// and task that names it — one action, so the roster and the cards never
// disagree. The old UI relabelled a chip and patched cards one by one,
// which left the declared team behind under its old name.
func TestRenameTeamRewritesPointerAndCards(t *testing.T) {
	fake := newFake([]board.Card{
		{ItemID: "c1", Title: "one", Team: "test", Progress: 40},
		{ItemID: "c2", Title: "two", Team: "portal"},
		{ItemID: "t1", Title: board.ProcessTaskTitle, Process: "Invoicing", Team: "test", Recurrence: "month", StartDate: "2026-06-01"},
	}, map[string]board.SprintState{
		"test":   {Current: "2026-08-24", Previous: "2026-08-17", ItemID: "st-test"},
		"portal": {Current: "2026-08-24", ItemID: "st-portal"},
	})
	svc := New(fake)
	if err := svc.RenameTeam(t.Context(), "o", "test", "platform"); err != nil {
		t.Fatal(err)
	}
	b, _ := fake.LoadBoard(t.Context(), "o")
	if _, old := b.SprintStates["test"]; old {
		t.Fatal("the old name still has a sprint pointer")
	}
	st, ok := b.SprintStates["platform"]
	if !ok || st.Current != "2026-08-24" || st.Previous != "2026-08-17" || st.ItemID != "st-test" {
		t.Fatalf("renamed team's pointer = %+v, want the same pointer under the new name", st)
	}
	if c, _ := findCard(b, "c1"); c.Team != "platform" {
		t.Fatalf("c1 team = %q, want platform", c.Team)
	}
	if c, _ := findCard(b, "c2"); c.Team != "portal" {
		t.Fatalf("another team's card was touched: %q", c.Team)
	}
	if !fake.saw("SetTeam t1 platform") {
		t.Fatalf("the process task that names the team follows it; log = %v", fake.log)
	}
}

// The name is the team's identity: a rename into a name another team has —
// in any case — is refused, as is renaming into or out of the no-team group.
func TestRenameTeamRefusesTakenEmptyOrNoTeam(t *testing.T) {
	fake := newFake(nil, map[string]board.SprintState{
		"test":   {ItemID: "st-test"},
		"portal": {ItemID: "st-portal"},
	})
	svc := New(fake)
	if err := svc.RenameTeam(t.Context(), "o", "test", "Portal"); !errors.Is(err, ErrTeamExists) {
		t.Fatalf("rename into a taken name: err = %v, want ErrTeamExists", err)
	}
	if err := svc.RenameTeam(t.Context(), "o", "test", "  "); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if err := svc.RenameTeam(t.Context(), "o", "", "x"); err == nil {
		t.Fatal("the no-team group cannot be renamed")
	}
	if err := svc.RenameTeam(t.Context(), "o", "ghost", "x"); !errors.Is(err, ErrTeamNotFound) {
		t.Fatalf("unknown team: err = %v, want ErrTeamNotFound", err)
	}
	if err := svc.RenameTeam(t.Context(), "o", "test", "test"); err != nil {
		t.Fatalf("renaming onto itself is a no-op, got %v", err)
	}
	if fake.count("SetTeam") != 0 {
		t.Fatalf("refused renames must write nothing; log = %v", fake.log)
	}
}
