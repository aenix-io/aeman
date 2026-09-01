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
