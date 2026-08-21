package board

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCardJSONTags checks the frontend-facing json tags on the enriched Card,
// Note and Board, so the REST API serialises exactly the camelCase names the
// frontend types.ts expects.
func TestCardJSONTags(t *testing.T) {
	card := Card{
		ItemID:       "I1",
		ContentID:    "C1",
		Title:        "t",
		IsDraft:      true,
		URL:          "https://x/1",
		Number:       42,
		Repository:   "acme/repo",
		State:        "OPEN",
		Assignees:    []string{"bob"},
		Author:       "erin",
		Zone:         ZoneRed,
		ZoneOptionID: "o_red",
		Progress:     40,
		Stage:        StageReview,
		Day:          "2026-06-25",
		StartDate:    "2026-06-20",
		SprintStart:  "2026-06-22",
		SprintTitle:  "Sprint 7",
		Status:       "In Progress",
		CreatedAt:    "2026-06-19T10:00:00Z",
		Description:  "desc",
		Notes: []Note{{
			ID:        "n1",
			Body:      "note",
			CreatedAt: "2026-06-21T09:00:00Z",
			Author:    "frank",
			Source:    "comment",
		}},
	}
	got := string(mustMarshal(t, card))
	for _, key := range []string{
		`"url":`, `"number":`, `"repository":`, `"state":`, `"author":`,
		`"zoneOptionId":`, `"day":`, `"sprintTitle":`, `"status":`,
		`"description":`, `"notes":`, `"createdAt":`, `"source":`,
	} {
		if !strings.Contains(got, key) {
			t.Errorf("card JSON missing %s: %s", key, got)
		}
	}

	b := Board{Title: "Domain", URL: "https://x/9"}
	bd := string(mustMarshal(t, b))
	for _, key := range []string{`"title":`, `"url":`} {
		if !strings.Contains(bd, key) {
			t.Errorf("board JSON missing %s: %s", key, bd)
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// NewBoard records the sprint-state cards' board positions as the shared team
// order (duplicates keep their first slot).
func TestNewBoardTeamOrder(t *testing.T) {
	b := NewBoard(nil, []Card{
		{ItemID: "s1", Title: SprintStateTitle, Team: "gamma", SprintStart: "2026-01-01"},
		{ItemID: "c1", Team: "alpha"},
		{ItemID: "s2", Title: SprintStateTitle, Team: "alpha", SprintStart: "2026-01-01"},
		{ItemID: "s3", Title: SprintStateTitle, Team: "gamma", SprintStart: "2026-01-02"},
	})
	want := []string{"gamma", "alpha"}
	if len(b.TeamOrder) != 2 || b.TeamOrder[0] != want[0] || b.TeamOrder[1] != want[1] {
		t.Fatalf("TeamOrder = %v, want %v", b.TeamOrder, want)
	}
}

// Duplicate sprint-state cards must resolve to a DETERMINISTIC winner — the
// oldest card — whatever their board positions; the pointer must not
// flip-flop between duplicates as positions churn.
func TestNewBoardDuplicateSprintStateWinner(t *testing.T) {
	dup := Card{ItemID: "s-new", Title: SprintStateTitle, Team: "alpha",
		SprintStart: "2026-07-23", StartDate: "2026-07-22", CreatedAt: "2026-07-21T14:25:44Z"}
	orig := Card{ItemID: "s-old", Title: SprintStateTitle, Team: "alpha",
		SprintStart: "2026-07-21", StartDate: "2026-07-20", CreatedAt: "2026-06-29T09:49:52Z"}

	// Whichever order the board lists them in, the older card wins.
	for name, cards := range map[string][]Card{
		"orig first": {orig, dup},
		"dup first":  {dup, orig},
	} {
		b := NewBoard(nil, cards)
		st := b.SprintStates["alpha"]
		if st.ItemID != "s-old" || st.Current != "2026-07-21" {
			t.Fatalf("%s: winner = %+v, want the oldest card s-old", name, st)
		}
		if len(b.TeamOrder) != 1 || b.TeamOrder[0] != "alpha" {
			t.Fatalf("%s: TeamOrder = %v", name, b.TeamOrder)
		}
	}
}

// A card naming an epic but no project is adopted by the only column of that
// name — the pair postdates such cards, and the GitHub UI can still write a
// bare Epic. Two columns sharing the name make it ambiguous: no guessing.
func TestCardsWithoutAProjectJoinTheOnlyColumnOfThatName(t *testing.T) {
	b := NewBoard(nil, []Card{
		{ItemID: "p1", Title: ProjectStateTitle, Project: "A"},
		{ItemID: "e1", Title: EpicStateTitle, Epic: "Infra", Project: "A"},
		{ItemID: "c1", Title: "legacy", Epic: "Infra"},
	})
	if got := b.Cards[0].Project; got != "A" {
		t.Fatalf("project = %q, want the only Infra column's", got)
	}

	amb := NewBoard(nil, []Card{
		{ItemID: "p1", Title: ProjectStateTitle, Project: "A"},
		{ItemID: "p2", Title: ProjectStateTitle, Project: "B"},
		{ItemID: "e1", Title: EpicStateTitle, Epic: "Infra", Project: "A"},
		{ItemID: "e2", Title: EpicStateTitle, Epic: "Infra", Project: "B"},
		{ItemID: "c1", Title: "legacy", Epic: "Infra"},
	})
	if got := amb.Cards[0].Project; got != "" {
		t.Fatalf("project = %q, want none — the name is ambiguous", got)
	}
}
