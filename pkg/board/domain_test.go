package board

import "testing"

// G14 — a card's domain (the repository it lives in) follows one rule,
// linked cards first: a review card lives with the card it reviews, a
// subtask with its parent; only an unlinked card follows its team.

// mapResolver answers from two maps; missing keys are unknown.
type mapResolver struct {
	cards, teams map[string]string
}

func (m mapResolver) CardDomain(id string) (string, bool) {
	d, ok := m.cards[id]
	return d, ok
}

func (m mapResolver) TeamDomain(name string) (string, bool) {
	d, ok := m.teams[name]
	return d, ok
}

var world = mapResolver{
	cards: map[string]string{"closedCard": "closed", "sharedCard": "shared"},
	teams: map[string]string{"portal": "shared", "ops": "shared"},
}

func TestDomainOfLinkedCardsFirst(t *testing.T) {
	// The edge that makes the order matter: a review card carries the
	// ORIGINAL's team (shared). The team rule would leak it into shared;
	// the review rule keeps it with the closed original.
	review := Card{ItemID: "r", ReviewOf: "closedCard", Team: "portal"}
	if got := DomainOf(review, world); got != "closed" {
		t.Fatalf("review card domain = %q, want closed (the reviewed card's)", got)
	}
	// A subtask with a team of its own still follows its parent.
	sub := Card{ItemID: "s", Parent: "closedCard", Team: "portal"}
	if got := DomainOf(sub, world); got != "closed" {
		t.Fatalf("subtask domain = %q, want closed (the parent's)", got)
	}
}

func TestDomainOfTeamThenPrimary(t *testing.T) {
	// No link: the team decides.
	if got := DomainOf(Card{ItemID: "t", Team: "ops"}, world); got != "shared" {
		t.Fatalf("team card domain = %q, want shared", got)
	}
	// Nothing at all: the primary domain ("").
	if got := DomainOf(Card{ItemID: "n"}, world); got != "" {
		t.Fatalf("bare card domain = %q, want the primary", got)
	}
	// A PLANNING card is placed by nothing — the people are on the tasks
	// under it — so it lives in the primary, on a board of two repositories
	// like any other; a stray value a direct writer put on it does not move
	// it into a closed repository.
	for _, c := range []Card{
		{ItemID: "g", Type: TypeGoal},
		{ItemID: "g2", Type: TypeGoal, Team: "portal"},
		{ItemID: "e", Type: TypeEpic, Parent: "closedCard"},
	} {
		if got := DomainOf(c, world); got != "" {
			t.Fatalf("planning card %s domain = %q, want the primary", c.ItemID, got)
		}
	}
}

// An unknown reference does not decide: a dangling parent falls through to
// the team rule, and an unknown team to the primary.
func TestDomainOfUnknownReferencesFallThrough(t *testing.T) {
	if got := DomainOf(Card{ItemID: "d2", Parent: "gone", Team: "ops"}, world); got != "shared" {
		t.Fatalf("dangling parent: domain = %q, want the team's", got)
	}
	if got := DomainOf(Card{ItemID: "u", Team: "nope"}, world); got != "" {
		t.Fatalf("unknown team: domain = %q, want the primary", got)
	}
}

// The board's ONE namespace. A real store hands over a board whose primary
// is NAMED and whose entries carry that name (gitstore.stamp), but a file
// may leave a stamp out and a test may not set one — so every reader has to
// answer "unstamped" and "the primary" with the same string. Compared raw,
// "aeman-db" and "" are two repositories where there is one, and the rules
// that ask "the same repository?" refuse what they should allow.
func TestEveryDomainReaderAnswersInThePrimarysNamespace(t *testing.T) {
	b := NewBoardIn("aeman-db", []Card{
		{ItemID: "st-p", Title: SprintStateTitle, Team: "platform"},
		{ItemID: "st-f", Title: SprintStateTitle, Team: "founders", Domain: "founders"},
	})
	for _, tc := range []struct{ what, got, want string }{
		{"an unstamped team", TeamDomain(b, "platform"), "aeman-db"},
		{"a stamped team", TeamDomain(b, "founders"), "founders"},
		{"a card its team places", HomeDomain(b, Card{Team: "platform"}), "aeman-db"},
		{"a card its team places in a secondary", HomeDomain(b, Card{Team: "founders"}), "founders"},
		{"a card NOTHING places", HomeDomain(b, Card{}), "aeman-db"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s answers %q, want %q", tc.what, tc.got, tc.want)
		}
	}
	// Where the card's FILE is, in the same namespace: the stamp, or the
	// primary when the file carries none. A rule that reads a card's own
	// stamp raw compares it against answers that ARE normalized.
	for _, tc := range []struct{ what, got, want string }{
		{"an unstamped card", FileDomain(b, Card{}), "aeman-db"},
		{"a stamped card", FileDomain(b, Card{Domain: "founders"}), "founders"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s answers %q, want %q", tc.what, tc.got, tc.want)
		}
	}
	// "Nothing declares this" stays its own answer: it decides nothing,
	// and reading it as the primary would place a card by a team that
	// does not exist.
	if got := TeamDomain(b, "ghosts"); got != "" {
		t.Errorf("an undeclared team answers %q, want the empty non-answer", got)
	}
}
