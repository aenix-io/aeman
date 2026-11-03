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

// The Project board is the ONE board that draws a project card, and it draws
// what stands in a column and nothing else (H4). A card in a column is
// nobody's subtask and has none, so nothing rides in with it: a child the
// subtask rider appended — a group written before schema 2 — belongs to no
// Project board, and the day boards no longer draw a card in a column either.
func TestProjectViewDeliversWhatStandsInAColumnAndNothingElse(t *testing.T) {
	today := board.TodayIso()
	b := board.NewBoard([]board.Card{
		{ItemID: "pr", Title: board.ProjectStateTitle, Project: "freedom"},
		{ItemID: "ep", Title: board.ScopeStateTitle, Scope: "Redis App", Project: "freedom"},
		{ItemID: "plan", Title: "TLS for DBaaS", Project: "freedom", Scope: "Redis App",
			StartDate: today, Day: today},
		// Written before schema 2: a task grouped under a card in a column.
		{ItemID: "loose", Title: "a piece of it", Parent: "plan", Team: "alpha"},
		// A project NAME alone is no column.
		{ItemID: "named", Title: "merely of the project", Project: "freedom", Team: "alpha",
			StartDate: today, Day: today, SprintStart: today},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today}}
	var seen []string
	for _, c := range FilterCards(b, Selector{View: "project"}) {
		seen = append(seen, c.ItemID)
	}
	if !slices.Equal(seen, []string{"plan"}) {
		t.Fatalf("the Project board delivers the card in the column alone, got %v", seen)
	}
	// And the day boards leave it to the Project board.
	for _, sel := range []Selector{
		{View: "team", Team: "alpha", Day: today},
		{View: "me", Day: today},
		{View: "backlog", Team: "alpha"},
	} {
		for _, c := range FilterCards(b, sel) {
			if c.ItemID == "plan" {
				t.Fatalf("view=%s draws a project card; it is the Project board's alone", sel.View)
			}
		}
	}
	if got := FilterCards(b, Selector{View: "team", Team: "alpha", Day: today}); len(got) != 1 || got[0].ItemID != "named" {
		t.Fatalf("a card that merely names a project is an ordinary task: %v", got)
	}
}

// ?view=project&project=X is ONE project's columns: a card standing in
// another project's column is not delivered, whatever else is true of it.
func TestProjectViewKeepsToTheProjectItWasAskedFor(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "pr-e", Title: board.ProjectStateTitle, Project: "engineering"},
		{ItemID: "pr-m", Title: board.ProjectStateTitle, Project: "marketing"},
		{ItemID: "ep-c", Title: board.ScopeStateTitle, Scope: "Cozy", Project: "engineering"},
		{ItemID: "ep-l", Title: board.ScopeStateTitle, Scope: "Launch", Project: "marketing"},
		{ItemID: "ours", Title: "in our column", Project: "engineering", Scope: "Cozy"},
		{ItemID: "theirs", Title: "in theirs", Project: "marketing", Scope: "Launch"},
	})
	var seen []string
	for _, c := range FilterCards(b, Selector{View: "project", Project: "engineering"}) {
		seen = append(seen, c.ItemID)
	}
	if !slices.Equal(seen, []string{"ours"}) {
		t.Fatalf("one project's columns, got %v", seen)
	}
}

// A PARKED card takes its subtasks off the view with it.
//
// The pieces of a parked card are not separate work waiting to be picked up:
// they are parts of something nobody is doing yet, and leaving them behind
// would scatter a card's insides across a board its parent has left.
//
// It costs nothing to arrange, which is the point. The board filters drop the
// parked parent, and children are only ever appended to a parent that is
// PRESENT — so the rule is derived rather than written on each child. Nothing
// has to be undone to bring them back: planning the parent again returns the
// whole group, whole, in one write.
func TestAParkedParentTakesItsSubtasksOffTheView(t *testing.T) {
	today := board.TodayIso()
	park := func(parked bool) board.Board {
		return board.Board{
			Cards: []board.Card{
				{ItemID: "p", Team: "alpha", Parked: parked,
					StartDate: today, Day: today, SprintStart: today},
				{ItemID: "kid", Team: "alpha", Parent: "p",
					StartDate: today, Day: today, SprintStart: today},
			},
			SprintStates: map[string]board.SprintState{"alpha": {Current: today}},
		}
	}
	ids := func(b board.Board) []string {
		out := []string{}
		for _, c := range FilterCards(b, Selector{View: "team", Team: "alpha", Day: today}) {
			out = append(out, c.ItemID)
		}
		return out
	}

	if got := ids(park(true)); len(got) != 0 {
		t.Fatalf("view = %v; a parked card and its pieces are off the day board", got)
	}
	// Planned again — both are back, and neither had to be touched.
	if got := ids(park(false)); len(got) != 2 {
		t.Fatalf("view = %v; the card comes back with its piece", got)
	}
}

// view=backlog is each team's ranked list of parked open tasks (K2): no
// planning card, no project card, no subtask on its own, no review card,
// nothing finished, and nothing in work — taken into work, a task drops out
// of the backlog at once (K4, K6).
func TestTheBacklogViewListsWhatATeamHasParked(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "now1", Team: "alpha", Parked: true, CreatedAt: "2026-01-02T00:00:00Z"},
		{ItemID: "ice", Team: "alpha", Parked: true, CreatedAt: "2026-01-01T00:00:00Z"},
		{ItemID: "inwork", Team: "alpha", SprintStart: "2026-08-31"},
		{ItemID: "unplaced", Team: "alpha"},
		{ItemID: "theirs", Team: "beta", Parked: true},
		{ItemID: "done", Team: "alpha", Parked: true, Progress: 100},
		{ItemID: "epic", Type: board.TypeEpic},
		{ItemID: "review", Team: "alpha", Parked: true, ReviewOf: "now1", Assignees: []string{"bob"}},
		// A subtask rides its parent and is never listed on its own.
		{ItemID: "kid", Team: "alpha", Parent: "now1", Parked: true},
	})
	ids := func(sel Selector) []string {
		out := []string{}
		for _, c := range FilterCards(b, sel) {
			out = append(out, c.ItemID)
		}
		return out
	}

	// Every list the team has, oldest first where nobody arranged them — and
	// each parked card's subtasks ride along, as they do in every other view:
	// the client nests them under their parent and needs the whole set for its
	// derived-progress math.
	if got := ids(Selector{View: "backlog", Team: "alpha"}); !slices.Equal(got, []string{"ice", "now1", "kid"}) {
		t.Fatalf("view=backlog&team=alpha = %v; want the parked cards, oldest first, pieces after", got)
	}
	// Another team's backlog is another team's business — and a set of
	// teams lists each, which is what the Backlog board's columns read.
	if got := ids(Selector{View: "backlog", Team: "beta"}); !slices.Equal(got, []string{"theirs"}) {
		t.Fatalf("view=backlog&team=beta = %v", got)
	}
	if got := ids(Selector{View: "backlog", Team: "alpha,beta"}); len(got) != 4 {
		t.Fatalf("view=backlog&team=alpha,beta = %v, want both teams' backlogs", got)
	}
}

// The reviews NOBODY HAS TAKEN are PINNED at the top of a backlog listing,
// above the hand-ranked tasks, oldest first among themselves — whatever rank
// they hold; the tasks follow in BacklogOrder (K10).
func TestTheBacklogPinsTheReviewsNobodyHasTakenOnTop(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "t1", Team: "alpha", Parked: true, Rank: "a", CreatedAt: "2026-01-01T00:00:00Z"},
		{ItemID: "t2", Team: "alpha", Parked: true, Rank: "b", CreatedAt: "2026-01-02T00:00:00Z"},
		{ItemID: "o", Team: "alpha", Stage: board.StageReview, SprintStart: "2026-11-03"},
		{ItemID: "o2", Team: "alpha", Stage: board.StageReview, SprintStart: "2026-11-03"},
		{ItemID: "r-new", Team: "alpha", ReviewOf: "o", Parked: true, Rank: "c", CreatedAt: "2026-11-03T10:00:00Z"},
		{ItemID: "r-old", Team: "alpha", ReviewOf: "o2", Parked: true, Rank: "d", CreatedAt: "2026-11-02T10:00:00Z"},
	})
	var got []string
	for _, c := range FilterCards(b, Selector{View: "backlog", Team: "alpha"}) {
		got = append(got, c.ItemID)
	}
	if want := []string{"r-old", "r-new", "t1", "t2"}; !slices.Equal(got, want) {
		t.Fatalf("backlog = %v, want %v", got, want)
	}
}
