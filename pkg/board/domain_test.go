package board

import "testing"

// G14 — a card's domain (the repository it lives in) follows one rule,
// linked cards first: a review card lives with the card it reviews, a
// subtask with its parent, an iteration with its task; only an unlinked card
// follows its project, and only one without a project follows its team.

// mapResolver answers from three maps; missing keys are unknown.
type mapResolver struct {
	cards, projects, teams map[string]string
}

func (m mapResolver) CardDomain(id string) (string, bool) {
	d, ok := m.cards[id]
	return d, ok
}

func (m mapResolver) ProjectDomain(name string) (string, bool) {
	d, ok := m.projects[name]
	return d, ok
}

func (m mapResolver) TeamDomain(name string) (string, bool) {
	d, ok := m.teams[name]
	return d, ok
}

var world = mapResolver{
	cards:    map[string]string{"closedCard": "closed", "sharedCard": "shared", "task1": "closed"},
	projects: map[string]string{"secret": "closed", "portal": "shared"},
	teams:    map[string]string{"portal": "shared", "ops": "shared"},
}

func TestDomainOfLinkedCardsFirst(t *testing.T) {
	// The edge that makes the order matter: a review card carries the
	// ORIGINAL's team (shared) and no project. The team rule would leak it
	// into shared; the review rule keeps it with the closed original.
	review := Card{ItemID: "r", ReviewOf: "closedCard", Team: "portal"}
	if got := DomainOf(review, world); got != "closed" {
		t.Fatalf("review card domain = %q, want closed (the reviewed card's)", got)
	}
	// A subtask with its own column still follows its parent.
	sub := Card{ItemID: "s", Parent: "closedCard", Project: "portal", Epic: "Docs", Team: "portal"}
	if got := DomainOf(sub, world); got != "closed" {
		t.Fatalf("subtask domain = %q, want closed (the parent's)", got)
	}
	// An iteration follows its task.
	iter := Card{ItemID: "i", Task: "task1", Team: "ops"}
	if got := DomainOf(iter, world); got != "closed" {
		t.Fatalf("iteration domain = %q, want closed (the task's)", got)
	}
}

func TestDomainOfProjectThenTeam(t *testing.T) {
	// A project card lives with its project, whatever its team.
	if got := DomainOf(Card{ItemID: "p", Project: "secret", Epic: "Bugs", Team: "portal"}, world); got != "closed" {
		t.Fatalf("project card domain = %q, want closed", got)
	}
	// A card that names a project without a column still follows it.
	if got := DomainOf(Card{ItemID: "p2", Project: "secret", Team: "portal"}, world); got != "closed" {
		t.Fatalf("project-only card domain = %q, want closed", got)
	}
	// No project: the team decides.
	if got := DomainOf(Card{ItemID: "t", Team: "ops"}, world); got != "shared" {
		t.Fatalf("team card domain = %q, want shared", got)
	}
	// Nothing at all: the primary domain ("").
	if got := DomainOf(Card{ItemID: "n"}, world); got != "" {
		t.Fatalf("bare card domain = %q, want the primary", got)
	}
}

// An unknown reference does not decide: a dangling parent falls through to
// the project and team rules, and an unknown project or team to the primary.
func TestDomainOfUnknownReferencesFallThrough(t *testing.T) {
	if got := DomainOf(Card{ItemID: "d", Parent: "gone", Project: "secret"}, world); got != "closed" {
		t.Fatalf("dangling parent: domain = %q, want the project's", got)
	}
	if got := DomainOf(Card{ItemID: "d2", Parent: "gone", Team: "ops"}, world); got != "shared" {
		t.Fatalf("dangling parent, no project: domain = %q, want the team's", got)
	}
	if got := DomainOf(Card{ItemID: "u", Project: "nope", Team: "nope"}, world); got != "" {
		t.Fatalf("unknown project and team: domain = %q, want the primary", got)
	}
}

// A card's project decides where it lives and its team does not get a say
// (DomainOf, rule 4). So a card filed under a project of one repository and
// handed to a team of another is a card that says one thing and lives
// somewhere else: the team's people cannot see it, and the project's people
// can. The pair is refused rather than resolved — the board tells the caller
// which two repositories disagree.
func TestATeamAndAProjectFromDifferentRepositoriesAreARefusal(t *testing.T) {
	b := Board{
		SprintStates: map[string]SprintState{
			"backoffice": {ItemID: "team-backoffice"},
			"founders":   {ItemID: "team-founders"},
			"cozystack":  {ItemID: "team-cozystack"},
		},
		ProjectStates: map[string]string{
			"backoffice": "project-backoffice",
			"strategy":   "project-strategy",
		},
		Domains: map[string]string{
			"team-founders":    "founders",
			"project-strategy": "founders",
			// The primary's own entries record no domain at all.
		},
	}
	cases := []struct {
		name            string
		team, project   string
		wantTeamDomain  string
		wantOtherDomain string
		wantConflict    bool
	}{
		{name: "both in the primary", team: "backoffice", project: "backoffice"},
		{name: "both in the same secondary", team: "founders", project: "strategy", wantTeamDomain: "founders", wantOtherDomain: "founders"},
		{name: "a team elsewhere than its project", team: "founders", project: "backoffice",
			wantTeamDomain: "founders", wantConflict: true},
		{name: "a project elsewhere than its team", team: "backoffice", project: "strategy",
			wantOtherDomain: "founders", wantConflict: true},
		{name: "no project at all constrains nothing", team: "founders", wantTeamDomain: "founders"},
		{name: "no team at all constrains nothing", project: "strategy", wantOtherDomain: "founders"},
		{name: "a team the roster does not declare yet decides nothing", team: "brand-new", project: "strategy",
			wantOtherDomain: "founders"},
		{name: "a project the roster does not declare yet decides nothing", team: "founders", project: "brand-new",
			wantTeamDomain: "founders"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TeamDomain(b, tc.team); got != tc.wantTeamDomain {
				t.Errorf("TeamDomain(%q) = %q, want %q", tc.team, got, tc.wantTeamDomain)
			}
			if got := ProjectDomain(b, tc.project); got != tc.wantOtherDomain {
				t.Errorf("ProjectDomain(%q) = %q, want %q", tc.project, got, tc.wantOtherDomain)
			}
			team, project, conflict := RosterConflict(b, tc.team, tc.project)
			if conflict != tc.wantConflict {
				t.Fatalf("RosterConflict(%q, %q) = %v, want %v", tc.team, tc.project, conflict, tc.wantConflict)
			}
			if conflict && (team != tc.wantTeamDomain || project != tc.wantOtherDomain) {
				t.Fatalf("the refusal must name both sides: team %q, project %q", team, project)
			}
		})
	}
	// A board of one repository records no domains at all: nothing can
	// conflict there, and the guard must not invent a refusal.
	single := Board{SprintStates: map[string]SprintState{"backoffice": {ItemID: "team-backoffice"}},
		ProjectStates: map[string]string{"strategy": "project-strategy"}}
	if _, _, conflict := RosterConflict(single, "backoffice", "strategy"); conflict {
		t.Fatal("a single-repository board has nothing to conflict")
	}
}
