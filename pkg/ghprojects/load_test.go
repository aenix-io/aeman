package ghprojects

import (
	"encoding/json"
	"testing"

	"github.com/aenix-org/aeman/pkg/board"
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
	if orig.ZoneOptionID != "o_red" {
		t.Errorf("orig zoneOptionId = %q, want o_red", orig.ZoneOptionID)
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

// contentBoardJSON exercises the frontend-facing content mapping: a draft card
// with a creator, a zone, a Day/Sprint/Status value and a body carrying a
// description plus a log-marked note thread, and an issue card with an author,
// url/number/repository/state, a body description and a comment thread.
const contentBoardJSON = `{
  "id":"PVT_C","number":11,"title":"Content","url":"https://example/11",
  "fields":{"nodes":[
    {"__typename":"ProjectV2SingleSelectField","id":"F_ZONE","name":"Zone","dataType":"SINGLE_SELECT","options":[
       {"id":"o_gray","name":"Planned","color":"GRAY"},
       {"id":"o_red","name":"Critical","color":"RED"}]},
    {"__typename":"ProjectV2Field","id":"F_DAY","name":"Day","dataType":"DATE"},
    {"__typename":"ProjectV2IterationField","id":"F_SPRINT","name":"Sprint","dataType":"ITERATION"},
    {"__typename":"ProjectV2SingleSelectField","id":"F_STATUS","name":"Status","dataType":"SINGLE_SELECT","options":[
       {"id":"st_todo","name":"Todo","color":"GRAY"},
       {"id":"st_prog","name":"In Progress","color":"YELLOW"}]}
  ]},
  "items":{"nodes":[
    {"id":"I_DRAFT","type":"DRAFT_ISSUE","createdAt":"2026-06-20T10:00:00Z",
     "content":{"__typename":"DraftIssue","id":"DI_DRAFT","title":"Draft card",
        "body":"Ship the thing\n\n<!-- aeman:log -->\n- [2026-06-21T09:00:00Z] started\n- [2026-06-22T09:00:00Z] almost",
        "creator":{"login":"dave"},"assignees":{"nodes":[{"login":"bob"}]}},
     "fieldValues":{"nodes":[
        {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"o_red","name":"Critical","field":{"id":"F_ZONE","name":"Zone"}},
        {"__typename":"ProjectV2ItemFieldDateValue","date":"2026-06-25","field":{"id":"F_DAY","name":"Day"}},
        {"__typename":"ProjectV2ItemFieldIterationValue","title":"Sprint 7","field":{"id":"F_SPRINT","name":"Sprint"}},
        {"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"st_prog","name":"In Progress","field":{"id":"F_STATUS","name":"Status"}}
     ]}},
    {"id":"I_ISSUE","type":"ISSUE","createdAt":"2026-06-19T10:00:00Z",
     "content":{"__typename":"Issue","id":"ISS_1","number":42,"title":"Fix bug",
        "url":"https://github.com/acme/repo/issues/42","state":"OPEN","author":{"login":"erin"},
        "repository":{"nameWithOwner":"acme/repo"},"assignees":{"nodes":[{"login":"carol"}]},
        "body":"The bug is real.",
        "comments":{"nodes":[{"id":"C1","body":"looking into it","createdAt":"2026-06-19T11:00:00Z","author":{"login":"frank"}}]}},
     "fieldValues":{"nodes":[]}}
  ]}
}`

func TestMapDomainItemContent(t *testing.T) {
	var raw rawProject
	if err := json.Unmarshal([]byte(contentBoardJSON), &raw); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	b := mapDomainBoard("acme", &raw)
	if b.Title != "Content" || b.URL != "https://example/11" {
		t.Fatalf("board title/url = %q / %q", b.Title, b.URL)
	}

	draft := cardOf(t, b, "I_DRAFT")
	if !draft.IsDraft {
		t.Errorf("draft.IsDraft = false, want true")
	}
	if draft.Author != "dave" {
		t.Errorf("draft author = %q, want dave (creator)", draft.Author)
	}
	if draft.Day != "2026-06-25" {
		t.Errorf("draft day = %q, want 2026-06-25", draft.Day)
	}
	if draft.ZoneOptionID != "o_red" || draft.Zone != board.ZoneRed {
		t.Errorf("draft zone/optionId = %q / %q", draft.Zone, draft.ZoneOptionID)
	}
	if draft.SprintTitle != "Sprint 7" {
		t.Errorf("draft sprintTitle = %q, want Sprint 7", draft.SprintTitle)
	}
	if draft.Status != "In Progress" {
		t.Errorf("draft status = %q, want In Progress", draft.Status)
	}
	if draft.Description != "Ship the thing" {
		t.Errorf("draft description = %q, want %q", draft.Description, "Ship the thing")
	}
	if len(draft.Notes) != 2 {
		t.Fatalf("draft notes = %+v, want 2", draft.Notes)
	}
	if draft.Notes[0].Body != "started" || draft.Notes[0].CreatedAt != "2026-06-21T09:00:00Z" ||
		draft.Notes[0].Source != "draft" || draft.Notes[0].ID != "I_DRAFT:1" {
		t.Errorf("draft note[0] = %+v", draft.Notes[0])
	}
	if draft.Notes[1].Body != "almost" || draft.Notes[1].ID != "I_DRAFT:2" {
		t.Errorf("draft note[1] = %+v", draft.Notes[1])
	}

	issue := cardOf(t, b, "I_ISSUE")
	if issue.IsDraft {
		t.Errorf("issue.IsDraft = true, want false")
	}
	if issue.Author != "erin" {
		t.Errorf("issue author = %q, want erin", issue.Author)
	}
	if issue.URL != "https://github.com/acme/repo/issues/42" || issue.Number != 42 ||
		issue.Repository != "acme/repo" || issue.State != "OPEN" {
		t.Errorf("issue url/number/repo/state = %q / %d / %q / %q",
			issue.URL, issue.Number, issue.Repository, issue.State)
	}
	if issue.Description != "The bug is real." {
		t.Errorf("issue description = %q", issue.Description)
	}
	if len(issue.Notes) != 1 {
		t.Fatalf("issue notes = %+v, want 1", issue.Notes)
	}
	if issue.Notes[0].ID != "C1" || issue.Notes[0].Body != "looking into it" ||
		issue.Notes[0].Author != "frank" || issue.Notes[0].Source != "comment" {
		t.Errorf("issue note[0] = %+v", issue.Notes[0])
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

// A draft-log note may span multiple lines: everything up to the next
// "- [timestamp]" header belongs to its body, and the edit/delete rebuild
// round-trips it unchanged.
func TestDraftLogMultilineNotes(t *testing.T) {
	body := "context\n\n<!-- aeman:log -->\n" +
		"- [2026-07-02T18:46:58Z] Review list (day one).\n" +
		"\nMerged:\n- repo#1011 — roles catalog\n- repo#1013 — subject matching\n" +
		"- [2026-07-03T07:52:38Z] Second note.\nWith one continuation line."
	desc, notes := domainParseDraftBody(body, "item1")
	if desc != "context" {
		t.Fatalf("description = %q", desc)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %d, want 2", len(notes))
	}
	want0 := "Review list (day one).\n\nMerged:\n- repo#1011 — roles catalog\n- repo#1013 — subject matching"
	if notes[0].Body != want0 {
		t.Fatalf("note[0] = %q", notes[0].Body)
	}
	if notes[1].Body != "Second note.\nWith one continuation line." {
		t.Fatalf("note[1] = %q", notes[1].Body)
	}
	// The rebuild (what EditNote/DeleteNote write back) must keep both bodies.
	rebuilt := domainBuildDraftBody(desc, notes)
	desc2, notes2 := domainParseDraftBody(rebuilt, "item1")
	if desc2 != desc || len(notes2) != 2 || notes2[0].Body != want0 {
		t.Fatalf("round-trip lost content: %q %+v", desc2, notes2)
	}
}
