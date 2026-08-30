package apiserver

import (
	"slices"
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
		if it.Spec.Description != nil {
			t.Fatalf("summary must not carry bodies, got %q on %s", *it.Spec.Description, it.Metadata.UID)
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
	if full.Items[0].Spec.Description == nil || *full.Items[0].Spec.Description == "" {
		t.Fatal("fields=full must carry bodies")
	}
	if full.Items[1].Spec.Description == nil {
		t.Fatal("a full resource carries the description even when empty — nil means NOT LOADED, and only summaries may say that")
	}
	if len(full.Items[0].Status.Links) != 2 {
		t.Fatal("the full shape carries the derived refs too")
	}
}

// The Project board draws a subtask that carries its own column (G57), so
// the view has to deliver it — and deliver it on its OWN merit, not as a
// rider of a delivered parent: the case the rule exists for is a parent
// that lives elsewhere entirely (the weekly plan, the working area) and is
// therefore in no project view at all. Filtering every parented card out
// left the whole group visible nowhere.
func TestProjectViewDeliversSubtasksThatCarryAColumn(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "freedom"},
		{ItemID: "ep", Title: board.EpicStateTitle, Epic: "Redis App", Project: "freedom"},
		// The parent is a plan card: no column, so no project view holds it.
		{ItemID: "parent", Title: "TLS for DBaaS", Plan: board.PlanWed, Week: "2026-08-24"},
		{ItemID: "child", Title: "TLS for Redis", Parent: "parent",
			Project: "freedom", Epic: "Redis App"},
		{ItemID: "loose", Title: "no column", Parent: "parent"},
	})
	got := FilterCards(b, Selector{View: "project"})
	var seen []string
	for _, c := range got {
		seen = append(seen, c.ItemID)
	}
	if !slices.Contains(seen, "child") {
		t.Fatalf("the subtask carries the column and must be delivered: %v", seen)
	}
	if slices.Contains(seen, "loose") {
		t.Fatalf("a subtask without a column belongs to no Project board: %v", seen)
	}
	// The parent RIDES ALONG even without a column of its own: the slot is
	// marked with its title, and the client has no other query to find it
	// in. It is not drawn — the grid draws what carries a column.
	if !slices.Contains(seen, "parent") {
		t.Fatalf("the parent of a delivered subtask names the slot: %v", seen)
	}
}

// The subtask rider must not smuggle columnless children in through a
// parent that DOES have a column: they belong to no Project board, and
// the client would count them into a progress bar that draws nothing.
func TestProjectViewDropsColumnlessChildrenOfAColumnedParent(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "freedom"},
		{ItemID: "ep", Title: board.EpicStateTitle, Epic: "Redis App", Project: "freedom"},
		{ItemID: "parent", Title: "parent in a column", Project: "freedom", Epic: "Redis App"},
		{ItemID: "loose", Title: "child without one", Parent: "parent"},
	})
	for _, c := range FilterCards(b, Selector{View: "project"}) {
		if c.ItemID == "loose" {
			t.Fatal("a columnless subtask is on no Project board, rider or not")
		}
	}
}

// ?view=project&project=X is ONE project's columns. A subtask is delivered
// on its own merit — its epic — so a child filed under another project is
// no more welcome than any other card of it, however its parent is filed.
func TestProjectViewKeepsToTheProjectItWasAskedFor(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "pr-e", Title: board.ProjectStateTitle, Project: "engineering"},
		{ItemID: "pr-m", Title: board.ProjectStateTitle, Project: "marketing"},
		{ItemID: "ep-c", Title: board.EpicStateTitle, Epic: "Cozy", Project: "engineering"},
		{ItemID: "ep-l", Title: board.EpicStateTitle, Epic: "Launch", Project: "marketing"},
		{ItemID: "p", Title: "parent", Project: "engineering", Epic: "Cozy"},
		{ItemID: "kid", Title: "child elsewhere", Parent: "p", Project: "marketing", Epic: "Launch"},
	})
	for _, c := range FilterCards(b, Selector{View: "project", Project: "engineering"}) {
		if c.ItemID == "kid" {
			t.Fatal("another project's card rides in on nobody's ticket")
		}
	}
}

// The riding parent is placed in BOARD ORDER, not appended as a tail: a
// listing whose rows reshuffle on every refetch is what withSubtasks goes
// out of its way to avoid for children, and the same must hold here.
func TestTheProjectViewKeepsBoardOrderWithARidingParent(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "freedom"},
		{ItemID: "ep", Title: board.EpicStateTitle, Epic: "Redis App", Project: "freedom"},
		{ItemID: "first", Title: "a columned card", Project: "freedom", Epic: "Redis App"},
		{ItemID: "parent", Title: "the parent, no column of its own"},
		{ItemID: "child", Title: "a subtask with one", Parent: "parent",
			Project: "freedom", Epic: "Redis App"},
		{ItemID: "last", Title: "another columned card", Project: "freedom", Epic: "Redis App"},
	})
	var got []string
	for _, c := range FilterCards(b, Selector{View: "project"}) {
		got = append(got, c.ItemID)
	}
	want := []string{"first", "parent", "child", "last"}
	if len(got) != len(want) {
		t.Fatalf("delivered %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("board order: delivered %v, want %v", got, want)
		}
	}
}
