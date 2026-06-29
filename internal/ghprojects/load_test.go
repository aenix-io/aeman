package ghprojects

import (
	"encoding/json"
	"testing"

	"github.com/aenix-org/aeman/internal/board"
)

// domainBoardJSON is a raw project exercising the domain mapping: a normal card
// with every typed role set, a linked review card, and a hidden sprint-state
// card that must be split out into SprintStates (not Cards).
const domainBoardJSON = `{
  "id":"PVT_D","number":9,"title":"Domain","url":"https://example/9",
  "fields":{"nodes":[
    {"__typename":"ProjectV2SingleSelectField","id":"F_ZONE","name":"Zone","dataType":"SINGLE_SELECT","options":[
       {"id":"o_gray","name":"Planned","color":"GRAY"},
       {"id":"o_red","name":"Critical","color":"RED"}]},
    {"__typename":"ProjectV2Field","id":"F_PROG","name":"Progress","dataType":"NUMBER"},
    {"__typename":"ProjectV2SingleSelectField","id":"F_STAGE","name":"Stage","dataType":"SINGLE_SELECT","options":[
       {"id":"s_locked","name":"Locked","color":"RED"},
       {"id":"s_review","name":"Review","color":"YELLOW"},
       {"id":"s_done","name":"Done","color":"GREEN"}]},
    {"__typename":"ProjectV2Field","id":"F_START","name":"Start","dataType":"DATE"},
    {"__typename":"ProjectV2Field","id":"F_SS","name":"Sprint Start","dataType":"DATE"},
    {"__typename":"ProjectV2Field","id":"F_TEAM","name":"Team","dataType":"TEXT"},
    {"__typename":"ProjectV2SingleSelectField","id":"F_PLAN","name":"Plan","dataType":"SINGLE_SELECT","options":[
       {"id":"p_wed","name":"Wed","color":"BLUE"},
       {"id":"p_fri","name":"Fri","color":"PURPLE"}]},
    {"__typename":"ProjectV2Field","id":"F_WEEK","name":"Week","dataType":"DATE"},
    {"__typename":"ProjectV2Field","id":"F_REV","name":"Review Of","dataType":"TEXT"}
  ]},
  "items":{"nodes":[
    {"id":"I_ORIG","type":"DRAFT_ISSUE","createdAt":"2026-06-20T10:00:00Z",
     "content":{"__typename":"DraftIssue","id":"DI_ORIG","title":"Build feature","assignees":{"nodes":[{"login":"bob"}]}},
     "fieldValues":{"nodes":[
        {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"o_red","name":"Critical","field":{"id":"F_ZONE","name":"Zone"}},
        {"__typename":"ProjectV2ItemFieldNumberValue","number":40,"field":{"id":"F_PROG","name":"Progress"}},
        {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"s_review","name":"Review","field":{"id":"F_STAGE","name":"Stage"}},
        {"__typename":"ProjectV2ItemFieldDateValue","date":"2026-06-20","field":{"id":"F_START","name":"Start"}},
        {"__typename":"ProjectV2ItemFieldDateValue","date":"2026-06-22","field":{"id":"F_SS","name":"Sprint Start"}},
        {"__typename":"ProjectV2ItemFieldTextValue","text":"alpha","field":{"id":"F_TEAM","name":"Team"}},
        {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"p_wed","name":"Wed","field":{"id":"F_PLAN","name":"Plan"}},
        {"__typename":"ProjectV2ItemFieldDateValue","date":"2026-06-22","field":{"id":"F_WEEK","name":"Week"}}
     ]}},
    {"id":"I_REV","type":"DRAFT_ISSUE","createdAt":"2026-06-21T10:00:00Z",
     "content":{"__typename":"DraftIssue","id":"DI_REV","title":"review: Build feature","assignees":{"nodes":[{"login":"carol"}]}},
     "fieldValues":{"nodes":[
        {"__typename":"ProjectV2ItemFieldTextValue","text":"I_ORIG","field":{"id":"F_REV","name":"Review Of"}},
        {"__typename":"ProjectV2ItemFieldTextValue","text":"alpha","field":{"id":"F_TEAM","name":"Team"}}
     ]}},
    {"id":"I_STATE","type":"DRAFT_ISSUE","createdAt":"2026-06-08T10:00:00Z",
     "content":{"__typename":"DraftIssue","id":"DI_STATE","title":"aeman:sprint-state","assignees":{"nodes":[]}},
     "fieldValues":{"nodes":[
        {"__typename":"ProjectV2ItemFieldTextValue","text":"alpha","field":{"id":"F_TEAM","name":"Team"}},
        {"__typename":"ProjectV2ItemFieldDateValue","date":"2026-06-22","field":{"id":"F_SS","name":"Sprint Start"}},
        {"__typename":"ProjectV2ItemFieldDateValue","date":"2026-06-08","field":{"id":"F_START","name":"Start"}}
     ]}}
  ]}
}`

func TestMapDomainBoard(t *testing.T) {
	var raw rawProject
	if err := json.Unmarshal([]byte(domainBoardJSON), &raw); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	b := mapDomainBoard("acme", &raw)

	if b.ID != "PVT_D" || b.Number != 9 || b.Owner != "acme" {
		t.Fatalf("board identity = %+v", b)
	}
	if len(b.Cards) != 2 {
		t.Fatalf("want 2 visible cards (state card split out), got %d", len(b.Cards))
	}
	if len(b.Fields) != 9 {
		t.Fatalf("want 9 fields, got %d", len(b.Fields))
	}

	// The sprint-state card is split into SprintStates, not Cards.
	st, ok := b.SprintStates["alpha"]
	if !ok || st.Current != "2026-06-22" || st.Previous != "2026-06-08" || st.ItemID != "I_STATE" {
		t.Fatalf("sprint state = %+v (ok=%v)", st, ok)
	}
	for _, c := range b.Cards {
		if c.ItemID == "I_STATE" {
			t.Fatal("sprint-state card must not appear in Cards")
		}
	}

	orig := cardOf(t, b, "I_ORIG")
	if !orig.IsDraft || orig.ContentID != "DI_ORIG" || orig.Title != "Build feature" {
		t.Fatalf("orig identity = %+v", orig)
	}
	if orig.Zone != board.ZoneRed {
		t.Errorf("orig zone = %q, want red", orig.Zone)
	}
	if orig.Progress != 40 {
		t.Errorf("orig progress = %d, want 40", orig.Progress)
	}
	if orig.Stage != board.StageReview {
		t.Errorf("orig stage = %q, want review", orig.Stage)
	}
	if orig.StartDate != "2026-06-20" || orig.SprintStart != "2026-06-22" {
		t.Errorf("orig dates = %q / %q", orig.StartDate, orig.SprintStart)
	}
	if orig.Team != "alpha" || orig.Plan != board.PlanWed || orig.Week != "2026-06-22" {
		t.Errorf("orig team/plan/week = %q / %q / %q", orig.Team, orig.Plan, orig.Week)
	}
	if len(orig.Assignees) != 1 || orig.Assignees[0] != "bob" {
		t.Errorf("orig assignees = %v", orig.Assignees)
	}

	rev := cardOf(t, b, "I_REV")
	if rev.ReviewOf != "I_ORIG" {
		t.Errorf("review card ReviewOf = %q, want I_ORIG", rev.ReviewOf)
	}
}

func cardOf(t *testing.T, b board.Board, itemID string) board.Card {
	t.Helper()
	for _, c := range b.Cards {
		if c.ItemID == itemID {
			return c
		}
	}
	t.Fatalf("card %q not found", itemID)
	return board.Card{}
}
