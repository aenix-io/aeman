package apiserver

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// Selector scopes a card LIST or watch subscription. View selectors reproduce
// exactly what the UI renders (the Team grid, the Me day board, the Weekly
// plan); the plain field selectors compose with no view.
type Selector struct {
	// View is "", "all", "team", "me", "personal", "weekly" or "project". "" and "all" both list every
	// card (the HTTP/MCP layer defaults an unspecified view to the caller's "me").
	View string
	// Team is the team key for the team/weekly views ("" = the no-team group).
	Team string
	// Day is the viewed day for the team/me views (defaults to today).
	Day string
	// User is the person for the me view ("" = everyone).
	User string
	// Week is the plan week (a Monday) for the weekly view (defaults to the
	// current week).
	Week string
	// Project filters the project view to one project's epic columns. Empty
	// means every project — the all-projects overview. Note this is the
	// planning entity, NOT the GitHub board (that is addressed by owner+board).
	Project string
	// Fields picks the resource shape a listing delivers. The default is the
	// board row — no description, with the derived link refs in status
	// standing in for it; a card's body is one GET /cards/{uid} away.
	// "full" opts a genuine bulk reader into complete Cards.
	Fields string
	// Plain field selectors, applied on top of the view (or of all cards).
	Stage    *string
	Zone     *string
	Assignee string
	// Focus keeps only workable cards — the "what can I act on now" filter
	// (drops done, on-review and locked). It mirrors the Me view's focus toggle.
	Focus bool
	// IncludeReviews appends each returned card's linked review card to a me/team
	// view, so a client rendering the reviewer badge has it on hand without a
	// second request. It is a UI convenience, off by default (agents listing a
	// Me board do not want the review cards mixed in).
	IncludeReviews bool
}

// ParseSelector reads a selector from query parameters. Unknown views error.
func ParseSelector(q url.Values) (Selector, error) {
	sel := Selector{
		View:           q.Get("view"),
		Team:           q.Get("team"),
		Project:        q.Get("project"),
		Day:            q.Get("day"),
		User:           q.Get("user"),
		Week:           q.Get("week"),
		Assignee:       q.Get("assignee"),
		Focus:          q.Get("focus") == "true" || q.Get("focus") == "1",
		IncludeReviews: q.Get("reviews") == "true" || q.Get("reviews") == "1",
		Fields:         q.Get("fields"),
	}
	switch sel.Fields {
	case "", "full", "summary":
	default:
		return Selector{}, fmt.Errorf("unknown fields %q (want summary or full)", sel.Fields)
	}
	if sel.Fields == "" {
		sel.Fields = "summary"
	}
	if q.Has("stage") {
		v := q.Get("stage")
		sel.Stage = &v
	}
	if q.Has("zone") {
		v := q.Get("zone")
		sel.Zone = &v
	}
	switch sel.View {
	case "", "all", "team", "me", "personal", "weekly", "project":
	default:
		return Selector{}, fmt.Errorf("unknown view %q", sel.View)
	}
	return sel, nil
}

// normalized fills the selector's day/week defaults against the wall clock.
func (s Selector) normalized() Selector {
	if s.View == "team" || s.View == "me" || s.View == "personal" {
		if s.Day == "" {
			s.Day = board.TodayIso()
		}
	}
	if s.View == "weekly" && s.Week == "" {
		s.Week = board.MondayOf(board.TodayIso())
	}
	return s
}

// FilterCards returns the cards matching the selector, in board order. The
// view rules delegate to the same domain filters the views always used.
func FilterCards(b board.Board, sel Selector) []board.Card {
	sel = sel.normalized()
	var base []board.Card
	switch sel.View {
	case "team":
		// team accepts a comma-separated set so the Team board fetches every team
		// it shows in one request; the grids are unioned, deduped by item id.
		seen := map[string]bool{}
		for _, t := range strings.Split(sel.Team, ",") {
			for _, c := range board.TeamGrid(b, strings.TrimSpace(t), sel.Day) {
				if !seen[c.ItemID] {
					seen[c.ItemID] = true
					base = append(base, c)
				}
			}
		}
	case "me":
		base = board.MeView(b, sel.User, sel.Day)
	case "personal":
		// The caller's personal repository as a backlog: open cards and the
		// ones done today; the user is who-am-i, filled in by the handler.
		base = board.PersonalView(b, sel.User, sel.Day)
	case "project":
		// The Project board: every card filed under an epic, whatever its week
		// — the client lays rows (weeks) and columns (epics) out itself. The
		// selector filters by PROJECT (an epic's owner), not by team: a project
		// spans teams, and a card is assigned to a team from this very board.
		for _, c := range b.Cards {
			if c.Epic == "" || c.Parent != "" {
				continue
			}
			if sel.Project != "" && c.Project != sel.Project {
				continue
			}
			base = append(base, c)
		}
	case "weekly":
		// weekly accepts a comma-separated team set too, so the Team board's
		// weekly-plan panel fetches every team it shows in one request.
		seen := map[string]bool{}
		for _, t := range strings.Split(sel.Team, ",") {
			bands := board.WeeklyPlan(b, strings.TrimSpace(t), sel.Week)
			for _, c := range append(append([]board.Card{}, bands.Wed...), bands.Fri...) {
				if !seen[c.ItemID] {
					seen[c.ItemID] = true
					base = append(base, c)
				}
			}
		}
	default:
		base = b.Cards
	}
	out := make([]board.Card, 0, len(base))
	for _, c := range base {
		if sel.Stage != nil && string(c.Stage) != *sel.Stage {
			continue
		}
		if sel.Zone != nil && SemanticZone(c.Zone) != *sel.Zone {
			continue
		}
		if sel.Assignee != "" && !contains(c.Assignees, sel.Assignee) {
			continue
		}
		// team filters the views that are not already scoped by it (the me, all
		// and default lists). It accepts a comma-separated set, so
		// ?view=me&team=marketing,portal narrows the personal board to those
		// teams — the Me view's team-focus toggle over the selected chips.
		if (sel.View == "me" || sel.View == "" || sel.View == "all") && !teamInSet(c.Team, sel.Team) {
			continue
		}
		if sel.Focus && !board.Workable(c) {
			continue
		}
		out = append(out, c)
	}
	out = withSubtasks(b, out, sel)
	if sel.IncludeReviews && (sel.View == "me" || sel.View == "team") {
		out = withLinkedReviews(b, out)
	}
	return out
}

// withSubtasks appends the subtasks of every delivered parent, so a view is
// self-contained for a client nesting them under their cards. The me/all team
// gate and the focus filter apply to subtasks the same way they apply to
// top-level cards. ALL of a parent's subtasks are delivered — per-day
// visibility (a deferred subtask hiding until its day, a done one staying on
// its old sprint's days) is the CLIENT's rendering rule: the client needs the
// full set anyway, or its optimistic derived-progress math diverges from the
// server's and the parent's bar jumps on every reload.
func withSubtasks(b board.Board, out []board.Card, sel Selector) []board.Card {
	present := make(map[string]bool, len(out))
	for _, c := range out {
		present[c.ItemID] = true
	}
	extra := map[string][]board.Card{}
	for _, c := range b.Cards {
		if c.Parent == "" || !present[c.Parent] || present[c.ItemID] {
			continue
		}
		if (sel.View == "me" || sel.View == "" || sel.View == "all") && !teamInSet(c.Team, sel.Team) {
			continue
		}
		if sel.Focus && !board.Workable(c) {
			continue
		}
		extra[c.Parent] = append(extra[c.Parent], c)
		present[c.ItemID] = true
	}
	if len(extra) == 0 {
		return out
	}
	// Children slot in right AFTER their parent, so the list order a client
	// installs wholesale still matches the board order — appended tails would
	// reshuffle rows on every refetch.
	merged := make([]board.Card, 0, len(out))
	for _, c := range out {
		merged = append(merged, c)
		merged = append(merged, extra[c.ItemID]...)
	}
	return merged
}

// withLinkedReviews appends each card's linked review card (reviewOf == its id),
// skipping any already present, so a me/team view is self-contained for a client
// that renders the reviewer badge from the same card set.
func withLinkedReviews(b board.Board, out []board.Card) []board.Card {
	present := make(map[string]bool, len(out))
	ids := make(map[string]bool, len(out))
	for _, c := range out {
		present[c.ItemID] = true
		ids[c.ItemID] = true
	}
	for _, c := range b.Cards {
		if c.ReviewOf != "" && ids[c.ReviewOf] && !present[c.ItemID] {
			out = append(out, c)
			present[c.ItemID] = true
		}
	}
	return out
}

// Matches reports whether a single card is in the selector's scope — the watch
// hub uses it to compute per-subscription membership deltas. It must agree
// with FilterCards.
func (s Selector) Matches(b board.Board, c board.Card) bool {
	for _, m := range FilterCards(b, s) {
		if m.ItemID == c.ItemID {
			return true
		}
	}
	return false
}

// ListCards builds the LIST response for a selector: resources in board order,
// plus the weekly summary when the weekly view was selected.
func ListCards(b board.Board, sel Selector) CardList {
	cards := FilterCards(b, sel)
	resource := CardSummaryResource
	if sel.Fields == "full" {
		resource = CardResource
	}
	items := make([]Card, 0, len(cards))
	for _, c := range cards {
		items = append(items, resource(b, c))
	}
	list := CardList{Kind: "CardList", Items: items}
	if sel.View == "weekly" {
		list.Weekly = &WeeklySummary{Progress: planProgress(cards)}
	}
	return list
}

// planProgress averages the week's completion over its one-off cards: done
// counts as 100, recurrent cards are excluded (they restart every week and
// would skew the bar). It mirrors planProgress in TeamBoard.tsx.
func planProgress(cards []board.Card) int {
	sum, n := 0, 0
	for _, c := range cards {
		if c.Stage == board.StageRecurrent {
			continue
		}
		n++
		if c.Stage == board.StageDone {
			sum += 100
			continue
		}
		sum += c.Progress
	}
	if n == 0 {
		return 0
	}
	return (sum + n/2) / n
}

// teamInSet reports whether a card's team is in a comma-separated selector
// set. An empty set matches every team (no team filter).
func teamInSet(team, set string) bool {
	set = strings.TrimSpace(set)
	if set == "" {
		return true
	}
	for _, want := range strings.Split(set, ",") {
		if strings.TrimSpace(want) == team {
			return true
		}
	}
	return false
}

func contains(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}
