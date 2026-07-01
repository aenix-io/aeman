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
