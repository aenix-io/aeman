package apiserver

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/aenix-org/aeman/pkg/board"
)

func testBoard() board.Board {
	return board.Board{
		ID: "B1", Number: 1, Owner: "acme", Title: "board", URL: "https://x",
		Cards: []board.Card{
			{ItemID: "c1", ContentID: "D1", IsDraft: true, Title: "Wire the API",
				Team: "alpha", Zone: board.ZoneRed, Progress: 40, Stage: board.StageReview,
				StartDate: "2026-01-10", SprintStart: "2026-01-10", Day: "2026-01-12",
				Assignees: []string{"octocat"}, Author: "octocat",
				CreatedAt: "2026-01-10T08:00:00Z", Description: "details",
				Notes: []board.Note{{ID: "n1", Body: "hi", CreatedAt: "t", Author: "a", Source: "comment"}}},
			{ItemID: "rev", Title: "review: Wire the API", Team: "alpha",
				ReviewOf: "c1", Progress: 40, Assignees: []string{"lllamnyp"},
				StartDate: "2026-01-10", SprintStart: "2026-01-10"},
			{ItemID: "p1", Title: "plan it", Team: "alpha", Plan: board.PlanWed,
				Week: "2026-01-05", Progress: 50},
			{ItemID: "p2", Title: "recurring", Team: "alpha", Plan: board.PlanFri,
				Week: "2026-01-05", Stage: board.StageRecurrent, Progress: 100},
			{ItemID: "z1", Title: "done thing", Team: "alpha", Zone: board.ZoneGreen,
				Progress: 100, StartDate: "2026-01-10", SprintStart: "2026-01-10"},
		},
		SprintStates: map[string]board.SprintState{
			"alpha": {Current: "2026-01-10", Previous: "2026-01-03"},
		},
	}
}

// M1: the resource carries every field, zones are semantic, and the JSON keys
// follow the metadata/spec/status shape.
func TestCardResourceMapsEveryField(t *testing.T) {
	b := testBoard()
	r := CardResource(b, b.Cards[0])
	if r.Kind != "Card" || r.Metadata.UID != "c1" || r.Metadata.ContentID != "D1" ||
		!r.Metadata.IsDraft || r.Metadata.Author != "octocat" ||
		r.Metadata.CreatedAt != "2026-01-10T08:00:00Z" {
		t.Fatalf("metadata = %+v", r.Metadata)
	}
	if r.Spec.Title != "Wire the API" || r.Spec.Team != "alpha" ||
		r.Spec.Zone != "urgent" || r.Spec.Progress != 40 || r.Spec.Stage != "review" ||
		r.Spec.Description != "details" {
		t.Fatalf("spec = %+v", r.Spec)
	}
	if r.Spec.Dates != (CardDates{Start: "2026-01-10", End: "2026-01-12", Sprint: "2026-01-10"}) {
		t.Fatalf("dates = %+v", r.Spec.Dates)
	}
	raw, _ := json.Marshal(r)
	for _, key := range []string{`"kind":"Card"`, `"uid":"c1"`, `"zone":"urgent"`, `"start":"2026-01-10"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("marshalled card missing %s: %s", key, raw)
		}
	}
}

// M1: zone names round-trip.
func TestZoneMapping(t *testing.T) {
	pairs := map[board.ZoneKey]string{
		board.ZoneRed: "urgent", board.ZoneYellow: "unplanned",
		board.ZoneGray: "planned", board.ZoneGreen: "niceToHave",
	}
	for z, name := range pairs {
		if SemanticZone(z) != name || DomainZone(name) != z {
			t.Fatalf("zone %s <-> %s does not round-trip", z, name)
		}
	}
	if SemanticZone("") != "" || DomainZone("") != "" {
		t.Fatal("empty zone must stay empty")
	}
}

// M2: derived status.
func TestCardStatusDerived(t *testing.T) {
	b := testBoard()
	// c1 is on review with an unfinished linked review card assigned to lllamnyp.
	c1 := CardResource(b, b.Cards[0])
	if c1.Status.Complete || c1.Status.ReviewedBy != "lllamnyp" {
		t.Fatalf("c1 status = %+v", c1.Status)
	}
	// z1 is 100% with no stage: derived done.
	z1 := CardResource(b, b.Cards[4])
	if !z1.Status.Complete || z1.Status.InProgress {
		t.Fatalf("z1 status = %+v", z1.Status)
	}
	// p1 at 50% with no stage is the implicit In Progress.
	p1 := CardResource(b, b.Cards[2])
	if !p1.Status.InProgress {
		t.Fatalf("p1 status = %+v", p1.Status)
	}
}

// V1: view selectors reproduce the domain views; field selectors compose.
func TestListSelectors(t *testing.T) {
	b := testBoard()
	team := FilterCards(b, Selector{View: "team", Team: "alpha", Day: "2026-01-10"})
	ids := func(cs []board.Card) []string {
		out := []string{}
		for _, c := range cs {
			out = append(out, c.ItemID)
		}
		return out
	}
	if !reflect.DeepEqual(ids(team), []string{"c1", "rev", "z1"}) {
		t.Fatalf("team view = %v", ids(team))
	}
	me := FilterCards(b, Selector{View: "me", User: "octocat", Day: "2026-01-10"})
	if !reflect.DeepEqual(ids(me), []string{"c1"}) {
		t.Fatalf("me view = %v", ids(me))
	}
	stage := "review"
	filtered := FilterCards(b, Selector{Stage: &stage})
	if !reflect.DeepEqual(ids(filtered), []string{"c1"}) {
		t.Fatalf("stage selector = %v", ids(filtered))
	}
	zone := "niceToHave"
	byZone := FilterCards(b, Selector{Zone: &zone})
	if !reflect.DeepEqual(ids(byZone), []string{"z1"}) {
		t.Fatalf("zone selector = %v", ids(byZone))
	}
	byAssignee := FilterCards(b, Selector{Assignee: "lllamnyp"})
	if !reflect.DeepEqual(ids(byAssignee), []string{"rev"}) {
		t.Fatalf("assignee selector = %v", ids(byAssignee))
	}
}

// V2: the weekly view computes the plan progress, excluding recurrent cards.
func TestWeeklyViewProgressExcludesRecurrent(t *testing.T) {
	b := testBoard()
	list := ListCards(b, Selector{View: "weekly", Team: "alpha", Week: "2026-01-05"})
	if len(list.Items) != 2 {
		t.Fatalf("weekly items = %d", len(list.Items))
	}
	if list.Weekly == nil || list.Weekly.Progress != 50 {
		t.Fatalf("weekly summary = %+v (recurrent p2 must not count)", list.Weekly)
	}
}

// V4 support: Matches agrees with FilterCards.
func TestSelectorMatches(t *testing.T) {
	b := testBoard()
	sel := Selector{View: "team", Team: "alpha", Day: "2026-01-10"}
	if !sel.Matches(b, b.Cards[0]) {
		t.Fatal("c1 is in the team view")
	}
	if sel.Matches(b, b.Cards[2]) {
		t.Fatal("p1 (a plan card) is not on the day grid")
	}
}

func TestSelectorParsing(t *testing.T) {
	q, _ := url.ParseQuery("view=team&team=alpha&day=2026-01-10&stage=review")
	sel, err := ParseSelector(q)
	if err != nil || sel.View != "team" || sel.Team != "alpha" || sel.Stage == nil || *sel.Stage != "review" {
		t.Fatalf("sel = %+v err = %v", sel, err)
	}
	if _, err := ParseSelector(url.Values{"view": {"nope"}}); err == nil {
		t.Fatal("unknown view must error")
	}
	if _, err := ParseSelector(url.Values{"view": {"all"}}); err != nil {
		t.Fatalf("view=all must be accepted: %v", err)
	}
}

// view=all lists every card (like the empty view) and still honours the team
// set and field filters — it is the explicit whole-board request.
func TestViewAllListsEverythingWithTeamFilter(t *testing.T) {
	b := board.Board{Cards: []board.Card{
		{ItemID: "a1", Team: "alpha", Progress: 40},
		{ItemID: "a2", Team: "alpha", Progress: 100},
		{ItemID: "b1", Team: "beta", Progress: 40},
	}}
	ids := func(sel Selector) []string {
		out := []string{}
		for _, c := range FilterCards(b, sel) {
			out = append(out, c.ItemID)
		}
		return out
	}
	if got := ids(Selector{View: "all"}); !reflect.DeepEqual(got, []string{"a1", "a2", "b1"}) {
		t.Fatalf("view=all = %v, want all three", got)
	}
	if got := ids(Selector{View: "all", Team: "beta"}); !reflect.DeepEqual(got, []string{"b1"}) {
		t.Fatalf("view=all&team=beta = %v, want [b1]", got)
	}
}

func TestOrderingResource(t *testing.T) {
	b := testBoard()
	o := OrderingResource(b)
	if o.Kind != "Ordering" || len(o.Spec.UIDs) != len(b.Cards) || o.Spec.UIDs[0] != "c1" {
		t.Fatalf("ordering = %+v", o)
	}
}

// Focus keeps only workable cards; the me view's team accepts a comma set.
func TestSelectorFocusAndMultiTeam(t *testing.T) {
	b := board.Board{Cards: []board.Card{
		{ItemID: "wip", Team: "alpha", Progress: 40},
		{ItemID: "todo", Team: "alpha"},
		{ItemID: "rev", Team: "alpha", Stage: board.StageReview, Progress: 50},
		{ItemID: "locked", Team: "beta", Stage: board.StageLocked, Progress: 50},
		{ItemID: "done", Team: "beta", Progress: 100},
		{ItemID: "recur", Team: "gamma", Stage: board.StageRecurrent, Progress: 30},
	}}
	ids := func(sel Selector) []string {
		out := []string{}
		for _, c := range FilterCards(b, sel) {
			out = append(out, c.ItemID)
		}
		return out
	}
	// Focus drops review/locked/done, keeps wip/todo/recurrent.
	got := ids(Selector{Focus: true})
	want := []string{"wip", "todo", "recur"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("focus = %v, want %v", got, want)
	}
	// Comma-separated team set on the me view (no sprint gating here: default view).
	got = ids(Selector{Team: "alpha,beta"})
	if !reflect.DeepEqual(got, []string{"wip", "todo", "rev", "locked", "done"}) {
		t.Fatalf("multi-team = %v", got)
	}
	// Focus + one team.
	if got := ids(Selector{Team: "alpha", Focus: true}); !reflect.DeepEqual(got, []string{"wip", "todo"}) {
		t.Fatalf("team+focus = %v", got)
	}
	// focus= query parsing.
	sel, _ := ParseSelector(map[string][]string{"focus": {"true"}})
	if !sel.Focus {
		t.Fatal("focus=true must parse")
	}
}

// reviews=true appends a me/team card's linked review card so the view is
// self-contained for the badge; off by default it is not mixed in.
func TestViewIncludeReviews(t *testing.T) {
	today := board.TodayIso()
	b := board.Board{
		Cards: []board.Card{
			{ItemID: "mine", Team: "alpha", Assignees: []string{"bob"}, Progress: 40, SprintStart: today},
			{ItemID: "rev", Team: "alpha", Assignees: []string{"carol"}, ReviewOf: "mine", Progress: 50, SprintStart: today},
		},
		SprintStates: map[string]board.SprintState{"alpha": {Current: today}},
	}
	has := func(sel Selector, id string) bool {
		for _, c := range FilterCards(b, sel) {
			if c.ItemID == id {
				return true
			}
		}
		return false
	}
	base := Selector{View: "me", User: "bob", Day: today}
	if !has(base, "mine") || has(base, "rev") {
		t.Fatal("plain me view holds only the user's own card")
	}
	withRev := Selector{View: "me", User: "bob", Day: today, IncludeReviews: true}
	if !has(withRev, "mine") || !has(withRev, "rev") {
		t.Fatal("reviews=true must append the linked review card")
	}
}

// view=team accepts a comma set: the Team board fetches every team it shows in
// one request (union of the per-team grids).
func TestTeamViewMultiTeam(t *testing.T) {
	day := "2026-01-10"
	b := board.Board{
		Cards: []board.Card{
			{ItemID: "a1", Team: "alpha", Progress: 40, StartDate: day, SprintStart: day},
			{ItemID: "b1", Team: "beta", Progress: 40, StartDate: day, SprintStart: day},
			{ItemID: "g1", Team: "gamma", Progress: 40, StartDate: day, SprintStart: day},
		},
		SprintStates: map[string]board.SprintState{
			"alpha": {Current: day}, "beta": {Current: day}, "gamma": {Current: day},
		},
	}
	ids := map[string]bool{}
	for _, c := range FilterCards(b, Selector{View: "team", Team: "alpha,beta", Day: day}) {
		ids[c.ItemID] = true
	}
	if !ids["a1"] || !ids["b1"] || ids["g1"] {
		t.Fatalf("team=alpha,beta = %v, want alpha+beta only", ids)
	}
}

// view=weekly accepts a comma set too: the Team board's weekly-plan panel
// fetches every team it shows in one request.
func TestWeeklyViewMultiTeam(t *testing.T) {
	week := "2026-01-05"
	b := board.Board{Cards: []board.Card{
		{ItemID: "aw", Team: "alpha", Plan: board.PlanWed, Week: week},
		{ItemID: "bf", Team: "beta", Plan: board.PlanFri, Week: week},
		{ItemID: "gw", Team: "gamma", Plan: board.PlanWed, Week: week},
	}}
	ids := map[string]bool{}
	for _, c := range FilterCards(b, Selector{View: "weekly", Team: "alpha,beta", Week: week}) {
		ids[c.ItemID] = true
	}
	if !ids["aw"] || !ids["bf"] || ids["gw"] {
		t.Fatalf("weekly team=alpha,beta = %v, want alpha+beta only", ids)
	}
}

// The board resource carries the people roster (every distinct assignee), so
// pickers work even though clients load one view at a time.
func TestBoardResourceMembers(t *testing.T) {
	b := testBoard()
	info := BoardResource(b)
	got := info.Metadata.Members
	if !reflect.DeepEqual(got, []string{"lllamnyp", "octocat"}) {
		t.Fatalf("members = %v, want [lllamnyp octocat]", got)
	}
}

// A worked plan card carried forward keeps showing in every past week it was
// worked in (week history, mirroring the day grid's sprint history); a pure
// never-started plan card moves with its week and leaves no history.
func TestWeeklyHistoryForWorkedCards(t *testing.T) {
	b := board.Board{Cards: []board.Card{
		{ItemID: "worked", Team: "alpha", Plan: board.PlanWed, Week: "2026-07-06",
			StartDate: "2026-07-01", Assignees: []string{"bob"}, Progress: 40},
		{ItemID: "pure", Team: "alpha", Plan: board.PlanFri, Week: "2026-07-06"},
		{ItemID: "later", Team: "alpha", Plan: board.PlanWed, Week: "2026-07-13",
			StartDate: "2026-07-08", Progress: 20},
		// Taken into the sprint without a start date (take-into-plan sets only
		// the sprint): the sprint join anchors its week history.
		{ItemID: "sprintOnly", Team: "alpha", Plan: board.PlanFri, Week: "2026-07-06",
			SprintStart: "2026-07-03", Assignees: []string{"dan"}},
	}}
	ids := func(week string) map[string]bool {
		out := map[string]bool{}
		for _, c := range FilterCards(b, Selector{View: "weekly", Team: "alpha", Week: week}) {
			out[c.ItemID] = true
		}
		return out
	}
	prev := ids("2026-06-29")
	if !prev["worked"] || !prev["sprintOnly"] || prev["pure"] || prev["later"] {
		t.Fatalf("week 06-29 = %v, want worked+sprintOnly as history", prev)
	}
	cur := ids("2026-07-06")
	// "later" started on 07-08 (inside week 07-06) and was carried to 07-13,
	// so week 07-06 keeps it as history alongside its own members.
	if !cur["worked"] || !cur["pure"] || !cur["later"] {
		t.Fatalf("week 07-06 = %v, want worked+pure+later", cur)
	}
}
