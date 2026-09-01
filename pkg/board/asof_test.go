package board

import "testing"

// Whether a day is over is each TEAM's own answer: a team whose sprint has
// moved on past it is done with the day, a team still inside that sprint is
// working it — its own day is where a sprint lays itself out, and where a
// card created today lands with a pointer days old.
func TestADayIsOverPerTeam(t *testing.T) {
	live := Board{SprintStates: map[string]SprintState{
		"portal":     {Current: "2026-09-01", Previous: "2026-08-31"},
		"backoffice": {Current: "2026-08-31", Previous: "2026-08-28"},
		"sales":      {Current: "2026-07-06"},
		"nosprint":   {},
	}}
	past := TeamsPast(live, "2026-08-31")
	if !past["portal"] {
		t.Fatal("portal opened a new sprint since the 31st: the day is over for it")
	}
	for _, team := range []string{"backoffice", "sales", "nosprint"} {
		if past[team] {
			t.Fatalf("%s is still in the sprint that covers the 31st (or has none): the day is theirs", team)
		}
	}
	// Nothing is over on today or later.
	if len(TeamsPast(live, "2026-09-01")) != 0 {
		t.Fatalf("the day the newest sprint opened is over for nobody: %v", TeamsPast(live, "2026-09-01"))
	}
}

// The merge is one screen with two moments in it.
func TestMergeAsOfTakesEachTeamFromItsOwnMoment(t *testing.T) {
	live := Board{
		SprintStates: map[string]SprintState{
			"portal":     {Current: "2026-09-01", Previous: "2026-08-20"},
			"backoffice": {Current: "2026-08-20"},
		},
		Cards: []Card{
			{ItemID: "p1", Team: "portal", Progress: 90, Rank: "a"},
			{ItemID: "b1", Team: "backoffice", Progress: 90, Rank: "b"},
			{ItemID: "p2", Team: "portal", Progress: 0, Rank: "c"}, // created since
		},
	}
	then := Board{
		SprintStates: map[string]SprintState{
			"portal":     {Current: "2026-08-20"},
			"backoffice": {Current: "2026-08-20"},
		},
		Cards: []Card{
			{ItemID: "p1", Team: "portal", Progress: 20, Rank: "a"},
			{ItemID: "b1", Team: "backoffice", Progress: 20, Rank: "b"},
		},
	}
	merged, fromPast := MergeAsOf(live, then, TeamsPast(live, "2026-08-20"))

	by := map[string]Card{}
	for _, c := range merged.Cards {
		by[c.ItemID] = c
	}
	if got := by["p1"].Progress; got != 20 {
		t.Fatalf("portal has moved on, so its card is the day's own: %d%%", got)
	}
	if got := by["b1"].Progress; got != 90 {
		t.Fatalf("backoffice is still in that sprint, so its card is today's: %d%%", got)
	}
	if _, there := by["p2"]; there {
		t.Fatal("a card created after that day was not on the board then")
	}
	if !fromPast["p1"] || fromPast["b1"] {
		t.Fatalf("what came from the past = %v; only the settled team's cards are a record", fromPast)
	}
	// The pointers split the same way — the view rules place a card by its
	// team's pointer, so a past team's cards need the one they were placed
	// by, and a live team's need today's.
	if merged.SprintStates["portal"].Current != "2026-08-20" {
		t.Fatalf("portal's pointer = %q, want the day's own", merged.SprintStates["portal"].Current)
	}
	if merged.SprintStates["backoffice"].Current != "2026-08-20" {
		t.Fatalf("backoffice's pointer = %q, want its live one", merged.SprintStates["backoffice"].Current)
	}
	// Board order survives the merge: a listing is served in rank order.
	if len(merged.Cards) != 2 || merged.Cards[0].ItemID != "p1" || merged.Cards[1].ItemID != "b1" {
		t.Fatalf("merged order = %+v", merged.Cards)
	}
}

// A card that changed team between the two moments belongs to the team that
// is still working: taken once, from the live side.
func TestACardThatChangedTeamIsTakenOnce(t *testing.T) {
	live := Board{
		SprintStates: map[string]SprintState{
			"portal":     {Current: "2026-09-01"},
			"backoffice": {Current: "2026-08-20"},
		},
		Cards: []Card{{ItemID: "c1", Team: "backoffice", Progress: 90, Rank: "a"}},
	}
	then := Board{
		SprintStates: map[string]SprintState{"portal": {Current: "2026-08-20"}},
		Cards:        []Card{{ItemID: "c1", Team: "portal", Progress: 20, Rank: "a"}},
	}
	merged, fromPast := MergeAsOf(live, then, TeamsPast(live, "2026-08-20"))
	if len(merged.Cards) != 1 {
		t.Fatalf("the card appears %d times", len(merged.Cards))
	}
	if merged.Cards[0].Progress != 90 || fromPast["c1"] {
		t.Fatalf("card = %+v, fromPast=%v; the team still working owns it", merged.Cards[0], fromPast)
	}
}

// A PERSONAL card is never a record. It belongs to no team and no sprint —
// its day comes from its own dates, and it lives in its owner's repository —
// so the no-team group's sprint moving on must not freeze somebody's personal
// column: the two share nothing but an empty team name.
func TestAPersonalCardIsNeverARecord(t *testing.T) {
	live := Board{
		// The no-team group HAS moved on past the day.
		SprintStates: map[string]SprintState{"": {Current: "2026-09-01", Previous: "2026-08-20"}},
		Cards: []Card{
			{ItemID: "team-less", Progress: 90, Rank: "a"},
			{ItemID: "mine", Domain: "~kvaps", Progress: 90, Rank: "b"},
		},
	}
	then := Board{
		SprintStates: map[string]SprintState{"": {Current: "2026-08-20"}},
		Cards: []Card{
			{ItemID: "team-less", Progress: 20, Rank: "a"},
			{ItemID: "mine", Domain: "~kvaps", Progress: 20, Rank: "b"},
		},
	}
	merged, fromPast := MergeAsOf(live, then, TeamsPast(live, "2026-08-20"))
	by := map[string]Card{}
	for _, c := range merged.Cards {
		by[c.ItemID] = c
	}
	// The no-team group's own card is a record of that day, like any team's.
	if by["team-less"].Progress != 20 || !fromPast["team-less"] {
		t.Fatalf("the no-team card = %+v, fromPast=%v", by["team-less"], fromPast)
	}
	// The personal card is the owner's own, live and editable.
	if by["mine"].Progress != 90 || fromPast["mine"] {
		t.Fatalf("the personal card = %+v, fromPast=%v — a personal board has no sprint to be past", by["mine"], fromPast)
	}
}

// A card that has MOVED between teams since is still on the day it stood on.
// The merge asks "is the day over for this card's team?" and the two boards
// answer with different teams: the live one says portal (moved on, so the
// live copy is dropped), the evening one says backoffice (still working, so
// the record copy is dropped too) — and the card fell through both.
func TestACardThatMovedIntoASettledTeamStaysOnTheDay(t *testing.T) {
	live := Board{
		SprintStates: map[string]SprintState{
			"portal":     {Current: "2026-09-01", Previous: "2026-08-20"},
			"backoffice": {Current: "2026-08-20"},
		},
		Cards: []Card{{ItemID: "c1", Team: "portal", Progress: 90, Rank: "a"}},
	}
	then := Board{
		SprintStates: map[string]SprintState{
			"portal":     {Current: "2026-08-20"},
			"backoffice": {Current: "2026-08-20"},
		},
		Cards: []Card{{ItemID: "c1", Team: "backoffice", Progress: 20, Rank: "a"}},
	}
	merged, fromPast := MergeAsOf(live, then, TeamsPast(live, "2026-08-20"))
	if len(merged.Cards) != 1 {
		t.Fatalf("the card appears %d times: %+v", len(merged.Cards), merged.Cards)
	}
	// Its team TODAY has moved on, so the day is over for it and the record
	// is what that evening held.
	if merged.Cards[0].Progress != 20 || !fromPast["c1"] {
		t.Fatalf("card = %+v, fromPast=%v", merged.Cards[0], fromPast)
	}
}

// A team RENAMED since is the same team: the evening's cards carry the old
// name and the sprint pointers the new one, so asking the old name whether
// the day is over answers "no" for every card of it — and the day's record
// came back empty.
func TestARenamedTeamStillHasItsRecord(t *testing.T) {
	live := Board{
		SprintStates: map[string]SprintState{"platform": {Current: "2026-09-01", Previous: "2026-08-20"}},
		Cards:        []Card{{ItemID: "c1", Team: "platform", Progress: 90, Rank: "a"}},
	}
	then := Board{
		SprintStates: map[string]SprintState{"portal": {Current: "2026-08-20"}},
		Cards:        []Card{{ItemID: "c1", Team: "portal", Progress: 20, Rank: "a"}},
	}
	merged, fromPast := MergeAsOf(live, then, TeamsPast(live, "2026-08-20"))
	if len(merged.Cards) != 1 || merged.Cards[0].Progress != 20 || !fromPast["c1"] {
		t.Fatalf("the renamed team's record = %+v, fromPast=%v", merged.Cards, fromPast)
	}
}
