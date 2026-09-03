package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// twoRepoBoard is a board of two repositories: the primary, where the team
// "backoffice" and the project of the same name are declared, and
// "founders", where the team "founders" and the project "strategy" are.
func twoRepoBoard(cards []board.Card) *fakeBackend {
	roster := []board.Card{
		{ItemID: "pr-backoffice", Title: board.ProjectStateTitle, Project: "backoffice"},
		{ItemID: "pr-strategy", Title: board.ProjectStateTitle, Project: "strategy", Domain: "founders"},
		{ItemID: "ep-contracts", Title: board.EpicStateTitle, Epic: "Contracts", Project: "backoffice"},
		{ItemID: "ep-fundraising", Title: board.EpicStateTitle, Epic: "Fundraising", Project: "strategy", Domain: "founders"},
	}
	fake := newFake(append(roster, cards...), map[string]board.SprintState{
		"backoffice": {Current: "2026-08-24", ItemID: "st-backoffice"},
		"founders":   {Current: "2026-08-24", ItemID: "st-founders"},
	})
	// The sprint-state cards are seeded as pointers, so their repository is
	// seeded beside them.
	fake.b.Domains = map[string]string{"st-founders": "founders"}
	return fake
}

// Handing a project card to a team of another repository used to be
// accepted and then quietly ignored: the project decides where the card
// lives, so the card stayed in the primary carrying the name of a team
// whose people cannot even read it. The pair is refused where it is made.
func TestATeamFromAnotherRepositoryIsRefusedOnAProjectCard(t *testing.T) {
	fake := twoRepoBoard([]board.Card{
		{ItemID: "c1", Title: "SAFE #2", Team: "backoffice", Project: "backoffice", Epic: "Contracts"},
	})
	svc := New(fake)
	err := svc.SetTeam(t.Context(), "o", "c1", "founders", "2026-08-28")
	if !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("SetTeam across repositories = %v, want ErrDomainConflict", err)
	}
	// The refusal has to say which two repositories disagree — "no" without
	// a reason is what sends people to the log.
	if !strings.Contains(err.Error(), "founders") {
		t.Fatalf("the refusal must name the repositories: %v", err)
	}
	if c, _ := findCard(fake.b, "c1"); c.Team != "backoffice" {
		t.Fatalf("the card was changed anyway: team = %q", c.Team)
	}
	if fake.count("SetTeam") != 0 {
		t.Fatalf("a refused assignment wrote to the store: %v", fake.log)
	}
}

// The same pair the other way round: a card of the founders team must not be
// filed under a project of the primary repository, which would move it out
// from under the team that owns it.
func TestAProjectFromAnotherRepositoryIsRefusedOnATeamCard(t *testing.T) {
	fake := twoRepoBoard([]board.Card{
		{ItemID: "c1", Title: "Стратегия", Team: "founders"},
	})
	svc := New(fake)
	project := "backoffice"
	err := svc.SetEpic(t.Context(), "o", "c1", "Contracts", &project)
	if !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("SetEpic across repositories = %v, want ErrDomainConflict", err)
	}
	if c, _ := findCard(fake.b, "c1"); c.Project != "" || c.Epic != "" {
		t.Fatalf("the card was filed anyway: project %q epic %q", c.Project, c.Epic)
	}
}

// Both sides in the same repository stay allowed — the guard is about two
// repositories disagreeing, not about secondary repositories being special.
func TestATeamAndAProjectInTheSameRepositoryArePermitted(t *testing.T) {
	fake := twoRepoBoard([]board.Card{
		{ItemID: "c1", Title: "Стратегия", Team: "founders", Project: "strategy", Epic: "Fundraising"},
		{ItemID: "c2", Title: "no project", Team: "backoffice"},
	})
	svc := New(fake)
	if err := svc.SetTeam(t.Context(), "o", "c1", "founders", "2026-08-28"); err != nil {
		t.Fatalf("the same repository on both sides: %v", err)
	}
	// A card with no project at all is constrained by nothing: any team may
	// take it, and it follows that team's repository.
	if err := svc.SetTeam(t.Context(), "o", "c2", "founders", "2026-08-28"); err != nil {
		t.Fatalf("a card without a project: %v", err)
	}
	if c, _ := findCard(fake.b, "c2"); c.Team != "founders" {
		t.Fatalf("c2 team = %q, want founders", c.Team)
	}
}

// A create is a door too: the pair must not come into existence through it.
func TestCreatingACardWithATeamAndProjectFromDifferentRepositoriesIsRefused(t *testing.T) {
	fake := twoRepoBoard(nil)
	svc := New(fake)
	_, err := svc.CreateCard(t.Context(), "o", CreateCardArgs{
		Title: "new", Team: "founders", Project: "backoffice", Epic: "Contracts", Week: "2026-08-24",
	})
	if !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("create across repositories = %v, want ErrDomainConflict", err)
	}
	if len(fake.creates) != 0 {
		t.Fatalf("a refused create wrote a card: %+v", fake.creates)
	}
}

// A team the roster has never heard of decides nothing: it is declared on
// the way, in the card's own repository, and must not be refused as a
// conflict with a project that lives elsewhere.
func TestANewTeamNameIsNotAConflict(t *testing.T) {
	fake := twoRepoBoard([]board.Card{
		{ItemID: "c1", Title: "Стратегия", Team: "founders", Project: "strategy"},
	})
	svc := New(fake)
	if err := svc.SetTeam(t.Context(), "o", "c1", "brand-new", "2026-08-28"); err != nil {
		t.Fatalf("a team the roster does not declare yet: %v", err)
	}
}

// A card that already carries a conflicting pair — written before the guard,
// or by a tool that bypasses this server — must stay editable in every other
// way. Only a write that would leave the pair conflicting is refused.
func TestALegacyConflictDoesNotFreezeTheCard(t *testing.T) {
	fake := twoRepoBoard([]board.Card{
		{ItemID: "c1", Title: "SAFE #2", Team: "founders", Project: "backoffice", Epic: "Contracts", Progress: 40},
	})
	svc := New(fake)
	if err := svc.SetProgress(t.Context(), "o", "c1", 60); err != nil {
		t.Fatalf("an untouched pair must not block other writes: %v", err)
	}
	// And it can be resolved by fixing either side.
	if err := svc.SetTeam(t.Context(), "o", "c1", "backoffice", "2026-08-28"); err != nil {
		t.Fatalf("moving the team to the project's repository: %v", err)
	}
}
