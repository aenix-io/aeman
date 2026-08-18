package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func TestWithSubtasksDeliversAllChildren(t *testing.T) {
	today := board.TodayIso()
	old := board.AddDays(today, -5)
	future := board.AddDays(today, 3)
	b := board.Board{
		Cards: []board.Card{
			// Carried from the old sprint: startDate keeps its history day.
			{ItemID: "p", Team: "alpha", StartDate: old, SprintStart: today},
			{ItemID: "open", Team: "alpha", Parent: "p", SprintStart: today},
			// Done subtask left behind in the previous sprint by carry-over,
			// and a deferred one: BOTH still ride with the parent — per-day
			// visibility is the client's rendering rule, and the client needs
			// the full set for its derived-progress math.
			{ItemID: "done", Team: "alpha", Parent: "p", SprintStart: old,
				Progress: 100},
			{ItemID: "later", Team: "alpha", Parent: "p", SprintStart: today,
				StartDate: future},
		},
		SprintStates: map[string]board.SprintState{
			"alpha": {Current: today, Previous: old},
		},
	}
	got := map[string]bool{}
	for _, c := range FilterCards(b, Selector{View: "team", Team: "alpha", Day: today}) {
		got[c.ItemID] = true
	}
	for _, id := range []string{"p", "open", "done", "later"} {
		if !got[id] {
			t.Fatalf("%s missing from the view: %v", id, got)
		}
	}
}

// A summary listing is the board-row shape: no card bodies, with the derived
// link refs standing in so a row still knows it has links to show. This is
// what both the SPA and MCP list by default — descriptions measured 76-84% of
// day-view payloads and were unused by row rendering.
func TestListCardsSummaryOmitsBodies(t *testing.T) {
	b := board.Board{Cards: []board.Card{
		{ItemID: "c1", Team: "alpha", Title: "with body",
			Description: "see **https://github.com/acme/repo/pull/7** and https://example.com/doc"},
		{ItemID: "c2", Team: "alpha", Title: "bare"},
	}}
	list := ListCards(b, Selector{View: "all"})
	if len(list.Items) != 2 {
		t.Fatalf("items = %d", len(list.Items))
	}
	for _, it := range list.Items {
		if it.Spec.Description != "" {
			t.Fatalf("summary must not carry bodies, got %q on %s", it.Spec.Description, it.Metadata.UID)
		}
	}
	links := list.Items[0].Status.Links
	if len(links) != 2 {
		t.Fatalf("derived links = %+v, want the PR ref and the plain URL", links)
	}
	if links[0].Kind != "pull" || links[0].Number != 7 || links[0].Repo != "repo" {
		t.Fatalf("first ref = %+v, want the resolved PR shorthand fields", links[0])
	}
	if links[1].Kind != "link" || links[1].URL != "https://example.com/doc" {
		t.Fatalf("second ref = %+v", links[1])
	}

	// fields=full opts a genuine bulk reader into complete cards.
	full := ListCards(b, Selector{View: "all", Fields: "full"})
	if full.Items[0].Spec.Description == "" {
		t.Fatal("fields=full must carry bodies")
	}
	if len(full.Items[0].Status.Links) != 2 {
		t.Fatal("the full shape carries the derived refs too")
	}
}
