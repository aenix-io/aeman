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
