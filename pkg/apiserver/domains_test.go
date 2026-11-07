package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A client cannot offer a team for a card without knowing which repository
// the team was declared in, so the board names the repository of the teams
// that live outside the primary — and only those, since the primary is the
// default and a single-repository board has none.
func TestTheBoardNamesTheRepositoryOfTeamsOutsideThePrimary(t *testing.T) {
	b := board.Board{
		TeamOrder: []string{"backoffice", "founders"},
		SprintStates: map[string]board.SprintState{
			"backoffice": {ItemID: "st-backoffice"},
			"founders":   {ItemID: "st-founders"},
		},
		Domains: map[string]string{"st-founders": "founders"},
	}
	got := BoardResourceWithPeople(b, nil).Metadata
	if got.TeamDomains["founders"] != "founders" {
		t.Fatalf("teamDomains = %v, want founders named", got.TeamDomains)
	}
	if _, named := got.TeamDomains["backoffice"]; named {
		t.Fatalf("a team of the primary needs no entry: %v", got.TeamDomains)
	}

	// A board of one repository records no domains at all, and says nothing.
	single := board.Board{
		TeamOrder:    []string{"backoffice"},
		SprintStates: map[string]board.SprintState{"backoffice": {ItemID: "st-backoffice"}},
	}
	if bare := BoardResourceWithPeople(single, nil).Metadata; bare.TeamDomains != nil {
		t.Fatalf("a single-repository board must name none: %v", bare.TeamDomains)
	}
}
