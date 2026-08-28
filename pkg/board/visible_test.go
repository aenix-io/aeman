package board

import (
	"reflect"
	"testing"
)

// G17 — a visitor's board is the union of the domains they can read: a
// domain they cannot read is absent, not empty — no cards, teams, projects,
// columns, deadlines, processes or tasks from it. A card whose team lives in
// an unreadable domain is still served, under its team name, with no sprint
// pointer for that team. Entries with no recorded domain belong to the
// primary.

func twoDomainBoard() Board {
	return NewBoard([]Card{
		{ItemID: "T_", Title: SprintStateTitle, Team: "", Domain: "shared"},
		{ItemID: "T_PORTAL", Title: SprintStateTitle, Team: "portal", SprintStart: "2026-08-24", Domain: "shared"},
		{ItemID: "T_OPS", Title: SprintStateTitle, Team: "ops", SprintStart: "2026-08-24", Domain: "closed"},
		{ItemID: "P_PORTAL", Title: ProjectStateTitle, Project: "portal", Domain: "shared"},
		{ItemID: "P_SECRET", Title: ProjectStateTitle, Project: "secret", Domain: "closed"},
		{ItemID: "E_BUGS", Title: EpicStateTitle, Project: "portal", Epic: "Bugs", Domain: "shared"},
		{ItemID: "E_RISK", Title: EpicStateTitle, Project: "secret", Epic: "Risk", Domain: "closed"},
		{ItemID: "D_1", Title: DeadlineStateTitle, Project: "secret", Week: "2026-09-07", Domain: "closed"},
		{ItemID: "PR_WEEKLY", Title: ProcessStateTitle, Process: "weekly", Project: "secret", Domain: "closed"},
		{ItemID: "K_1", Title: ProcessTaskTitle, Process: "weekly", Domain: "closed"},
		{ItemID: "C_SHARED", Title: "shared card", Team: "portal", Domain: "shared", Assignees: []string{"kvaps"}},
		{ItemID: "C_CLOSED", Title: "closed card", Team: "portal", Project: "secret", Epic: "Risk", Domain: "closed", Assignees: []string{"timur"}},
		{ItemID: "C_OPS", Title: "ops card in shared", Team: "ops", Domain: "shared"},
		{ItemID: "C_LEGACY", Title: "no domain recorded"},
	})
}

func cardIDs(cards []Card) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.ItemID)
	}
	return out
}

func TestNewBoardRecordsRosterDomains(t *testing.T) {
	b := twoDomainBoard()
	want := map[string]string{"T_": "shared", "T_PORTAL": "shared", "T_OPS": "closed", "P_PORTAL": "shared", "P_SECRET": "closed",
		"E_BUGS": "shared", "E_RISK": "closed", "D_1": "closed", "PR_WEEKLY": "closed", "K_1": "closed"}
	if !reflect.DeepEqual(b.Domains, want) {
		t.Fatalf("Domains = %v\nwant %v", b.Domains, want)
	}
	// A single-domain board records nothing.
	if one := NewBoard([]Card{{ItemID: "T", Title: SprintStateTitle, Team: "x"}}); one.Domains != nil {
		t.Fatalf("Domains on a single-domain board = %v", one.Domains)
	}
}

func TestVisibleDropsUnreadableDomainsEntirely(t *testing.T) {
	b := twoDomainBoard()
	v := Visible(b, "shared", func(d string) bool { return d == "shared" })
	if got := cardIDs(v.Cards); !reflect.DeepEqual(got, []string{"C_SHARED", "C_OPS", "C_LEGACY"}) {
		t.Fatalf("cards = %v", got)
	}
	if !reflect.DeepEqual(v.Projects, []string{"portal"}) || v.ProjectStates["secret"] != "" {
		t.Fatalf("projects = %v / %v", v.Projects, v.ProjectStates)
	}
	if len(v.Epics) != 1 || v.Epics[0].Name != "Bugs" {
		t.Fatalf("epics = %v", v.Epics)
	}
	if len(v.Deadlines) != 0 || len(v.Processes) != 0 || len(v.Tasks) != 0 {
		t.Fatalf("closed roster leaked: deadlines %v processes %v tasks %d", v.Deadlines, v.Processes, len(v.Tasks))
	}
	if _, ok := v.Domains["P_SECRET"]; ok {
		t.Fatal("Domains still names a closed entry")
	}
}

// The team "ops" is declared in the closed domain; its card in the shared
// domain is still served under team "ops", and the team itself — its order
// slot and sprint pointer — is absent.
func TestVisibleKeepsCardWhoseTeamIsUnreadable(t *testing.T) {
	b := twoDomainBoard()
	v := Visible(b, "shared", func(d string) bool { return d == "shared" })
	var ops *Card
	for i := range v.Cards {
		if v.Cards[i].ItemID == "C_OPS" {
			ops = &v.Cards[i]
		}
	}
	if ops == nil || ops.Team != "ops" {
		t.Fatalf("ops card = %+v, want served under its team name", ops)
	}
	if _, has := v.SprintStates["ops"]; has {
		t.Fatal("unreadable team's sprint pointer leaked")
	}
	if !reflect.DeepEqual(v.TeamOrder, []string{"", "portal"}) {
		t.Fatalf("team order = %v", v.TeamOrder)
	}
}

// No recorded domain means the primary: readable with it, gone without.
func TestVisibleNoDomainFollowsThePrimary(t *testing.T) {
	b := twoDomainBoard()
	closedOnly := Visible(b, "shared", func(d string) bool { return d == "closed" })
	for _, c := range closedOnly.Cards {
		if c.ItemID == "C_LEGACY" || c.Domain == "shared" {
			t.Fatalf("card %s served to a visitor who cannot read the primary", c.ItemID)
		}
	}
	if got := cardIDs(closedOnly.Cards); !reflect.DeepEqual(got, []string{"C_CLOSED"}) {
		t.Fatalf("closed-only cards = %v", got)
	}
}

// Everything readable is the board itself, untouched.
func TestVisibleEverythingIsIdentity(t *testing.T) {
	b := twoDomainBoard()
	v := Visible(b, "shared", func(string) bool { return true })
	if !reflect.DeepEqual(v, b) {
		t.Fatalf("Visible(all) differs from the board:\n%+v\n%+v", v, b)
	}
	// The input is not mutated by a filtering call.
	_ = Visible(b, "shared", func(d string) bool { return d == "closed" })
	if !reflect.DeepEqual(b, twoDomainBoard()) {
		t.Fatal("Visible mutated its input")
	}
}
