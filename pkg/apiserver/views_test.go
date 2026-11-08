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

	// Every list the team has, in the board's order (BacklogOrder: no plan
	// item above either, so their own places decide) — and each parked
	// card's subtasks ride along, as they do in every other view: the client
	// nests them under their parent and needs the whole set for its
	// derived-progress math.
	if got := ids(Selector{View: "backlog", Team: "alpha"}); !slices.Equal(got, []string{"now1", "kid", "ice"}) {
		t.Fatalf("view=backlog&team=alpha = %v; want the parked cards in board order, each with its pieces", got)
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

// The reviews NOBODY HAS TAKEN lead a backlog listing (K10, BacklogOrder):
// before every task, in their tasks' Strategy order — one dragged below a
// task is still above it.
func TestTheReviewsNobodyHasTakenLeadABacklogListing(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "t1", Team: "alpha", Parked: true, Rank: "b", CreatedAt: "2026-01-01T00:00:00Z"},
		{ItemID: "o", Team: "alpha", Stage: board.StageReview, SprintStart: "2026-11-03"},
		{ItemID: "r-top", Team: "alpha", ReviewOf: "o", Parked: true, Rank: "a", CreatedAt: "2026-11-03T10:00:00Z"},
		{ItemID: "t2", Team: "alpha", Parked: true, Rank: "c", CreatedAt: "2026-01-02T00:00:00Z"},
		{ItemID: "o2", Team: "alpha", Stage: board.StageReview, SprintStart: "2026-11-03"},
		{ItemID: "r-dragged", Team: "alpha", ReviewOf: "o2", Parked: true, Rank: "d", CreatedAt: "2026-11-02T10:00:00Z"},
	})
	var got []string
	for _, c := range FilterCards(b, Selector{View: "backlog", Team: "alpha"}) {
		got = append(got, c.ItemID)
	}
	if want := []string{"r-top", "r-dragged", "t1", "t2"}; !slices.Equal(got, want) {
		t.Fatalf("backlog = %v, want %v", got, want)
	}
}

// A backlog listing carries the WHOLE board's order (K14) — what every
// Ordering frame carries too — so a client places a card the watch brings in
// (one under a plan item no listed card stands under, say) and re-sorts on a
// frame by the same key the server sorts by.
func TestABacklogListingCarriesTheOrderItIsReadFrom(t *testing.T) {
	b := board.NewBoard([]board.Card{
		{ItemID: "elsewhere", Type: board.TypeGoal},
		{ItemID: "t2", Team: "alpha", Parent: "e", Parked: true},
		{ItemID: "g", Type: board.TypeGoal},
		{ItemID: "inwork", Team: "alpha", Parent: "e", SprintStart: "2026-11-02"},
		{ItemID: "e", Type: board.TypeEpic, Parent: "g"},
		{ItemID: "t1", Team: "alpha", Parent: "e", Parked: true},
		{ItemID: "loose", Team: "alpha", Parked: true},
	})
	list := ListCards(b, Selector{View: "backlog", Team: "alpha"})
	if want := []string{"elsewhere", "t2", "g", "inwork", "e", "t1", "loose"}; !slices.Equal(list.BoardOrder, want) {
		t.Fatalf("boardOrder = %v, want %v", list.BoardOrder, want)
	}
	if other := ListCards(b, Selector{View: "team", Team: "alpha"}); other.BoardOrder != nil {
		t.Fatalf("only a backlog listing carries it: %v", other.BoardOrder)
	}
}
