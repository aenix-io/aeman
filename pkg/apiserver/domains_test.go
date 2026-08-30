package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A client cannot offer a team for a card without knowing which repository
// the team was declared in: a project card of the primary must not be
// offered a team that lives in another repository, because the pair cannot
// both be honoured (boardservice refuses it). So the board names the
// repository of the entries that live outside the primary — and only those,
// since the primary is the default and a single-repository board has none.
func TestTheBoardNamesTheRepositoryOfTeamsAndProjectsOutsideThePrimary(t *testing.T) {
	b := board.Board{
		TeamOrder: []string{"backoffice", "founders"},
		SprintStates: map[string]board.SprintState{
			"backoffice": {ItemID: "st-backoffice"},
			"founders":   {ItemID: "st-founders"},
		},
		Projects:      []string{"backoffice", "strategy"},
		ProjectStates: map[string]string{"backoffice": "pr-backoffice", "strategy": "pr-strategy"},
		Epics: []board.EpicCol{
			{Name: "Cozystack", Project: "backoffice", ItemID: "ep-cozy"},
			{Name: "Fundraising", Project: "strategy", ItemID: "ep-fund"},
		},
		Processes: []board.Process{
			{Name: "Payroll", ItemID: "proc-pay"},
			{Name: "Fundraising ops", ItemID: "proc-fund"},
		},
		Domains: map[string]string{
			"st-founders": "founders", "pr-strategy": "founders", "proc-fund": "founders",
			"ep-fund": "founders",
		},
	}
	got := BoardResourceWithPeople(b, nil).Metadata
	if got.TeamDomains["founders"] != "founders" {
		t.Fatalf("teamDomains = %v, want founders named", got.TeamDomains)
	}
	if _, named := got.TeamDomains["backoffice"]; named {
		t.Fatalf("a team of the primary needs no entry: %v", got.TeamDomains)
	}
	if got.ProjectDomains["strategy"] != "founders" {
		t.Fatalf("projectDomains = %v, want strategy named", got.ProjectDomains)
	}
	if _, named := got.ProjectDomains["backoffice"]; named {
		t.Fatalf("a project of the primary needs no entry: %v", got.ProjectDomains)
	}
	// Processes the same way: a card is only tied to a process of its own
	// repository, so the picker needs to know where each was declared.
	if got.ProcessDomains["Fundraising ops"] != "founders" {
		t.Fatalf("processDomains = %v, want Fundraising ops named", got.ProcessDomains)
	}
	if _, named := got.ProcessDomains["Payroll"]; named {
		t.Fatalf("a process of the primary needs no entry: %v", got.ProcessDomains)
	}

	// A column carries the repository it was declared in, which a client
	// cannot compute from the project: the same project NAME may be
	// declared in two repositories with its columns merged (G13), and the
	// column is what the server asks about.
	var closed, open bool
	for _, e := range got.Epics {
		if e.Name == "Fundraising" && e.Domain == "founders" {
			closed = true
		}
		if e.Name == "Cozystack" && e.Domain == "" {
			open = true
		}
	}
	if !closed || !open {
		t.Fatalf("every column names its own repository, the primary by omission: %+v", got.Epics)
	}

	// A board of one repository records no domains at all, and says nothing.
	single := board.Board{
		TeamOrder:     []string{"backoffice"},
		SprintStates:  map[string]board.SprintState{"backoffice": {ItemID: "st-backoffice"}},
		Projects:      []string{"backoffice"},
		ProjectStates: map[string]string{"backoffice": "pr-backoffice"},
	}
	bare := BoardResourceWithPeople(single, nil).Metadata
	if bare.TeamDomains != nil || bare.ProjectDomains != nil || bare.ProcessDomains != nil {
		t.Fatalf("a single-repository board must name none: %v %v %v",
			bare.TeamDomains, bare.ProjectDomains, bare.ProcessDomains)
	}
}
