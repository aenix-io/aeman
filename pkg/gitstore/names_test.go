package gitstore

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Roster names — teams, projects, processes — are one namespace across every
// domain of the board: a name is what cards refer to and what the domain
// rule resolves, so two "portal"s in two repositories would leave a card's
// home undecidable. The store refuses a create or a rename into a name any
// domain already declares, whether or not the caller can read that domain;
// the read-side alias merge (G13) stays only for collisions made behind the
// server's back.
func TestMultiBackendRefusesRosterNamesTakenInAnotherDomain(t *testing.T) {
	mb, _, _ := twoDomains(t) // shared: team portal, project portal; closed: project secret
	ctx := ctxAs("kvaps")
	b, _ := mb.LoadBoard(ctx, "x")

	// Creating in closed what shared already has.
	if _, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProjectStateTitle, Project: "portal", Domain: "closed"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("project portal in closed: err = %v, want ErrNameTaken", err)
	}
	if _, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.SprintStateTitle, Team: "portal", Domain: "closed"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("team portal in closed: err = %v, want ErrNameTaken", err)
	}
	if _, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProcessStateTitle, Process: "Invoicing", Domain: "closed"}); err != nil {
		t.Fatalf("a fresh process name is fine: %v", err)
	}
	if _, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProcessStateTitle, Process: "Invoicing"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("the same process in the primary: err = %v, want ErrNameTaken", err)
	}

	// Renaming into a taken name, across domains, on each roster kind.
	b, _ = mb.LoadBoard(ctx, "x")
	secret := board.Card{ItemID: b.ProjectStates["secret"], Title: board.ProjectStateTitle, Project: "secret"}
	if err := mb.SetProject(ctx, b, secret, "portal"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("rename project secret → portal: err = %v, want ErrNameTaken", err)
	}
	if err := mb.SetSprintState(ctx, b, "ops", "2026-08-31", ""); err != nil { // a second team, in the primary
		t.Fatal(err)
	}
	b, _ = mb.LoadBoard(ctx, "x")
	ops := board.Card{ItemID: b.SprintStates["ops"].ItemID, Title: board.SprintStateTitle, Team: "ops"}
	if err := mb.SetTeam(ctx, b, ops, "portal"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("rename team ops → portal: err = %v, want ErrNameTaken", err)
	}
	var invoicing string
	for _, p := range b.Processes {
		if p.Name == "Invoicing" {
			invoicing = p.ItemID
		}
	}
	if _, err := mb.CreateCard(ctx, b, board.CreateInput{Title: board.ProcessStateTitle, Process: "Runway"}); err != nil {
		t.Fatal(err)
	}
	inv := board.Card{ItemID: invoicing, Title: board.ProcessStateTitle, Process: "Invoicing"}
	if err := mb.SetProcess(ctx, b, inv, "Runway"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("rename process Invoicing → Runway: err = %v, want ErrNameTaken", err)
	}
	// A rename to a free name, and a rename onto itself, still work.
	if err := mb.SetTeam(ctx, b, ops, "operations"); err != nil {
		t.Fatalf("rename to a free name: %v", err)
	}
	if err := mb.SetProject(ctx, b, secret, "secret"); err != nil {
		t.Fatalf("rename onto itself: %v", err)
	}
	// An ordinary card may still be given any team or project the board has.
	var card board.Card
	for _, c := range b.Cards {
		if c.Title == "closed card" {
			card = c
		}
	}
	if err := mb.SetTeam(ctx, b, card, "operations"); err != nil {
		t.Fatalf("a card's team is a reference, not a declaration: %v", err)
	}
}
