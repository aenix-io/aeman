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
	// LeftOn gives a day back what the × took off it: a card finished that
	// day and tidied away carries the day it was LEFT on (board.Card.LeftAt)
	// while its dates have moved into the previous sprint, so nothing else
	// remembers where it was worked. Set only when a day is read as a RECORD
	// — today's board must not show it, since taking the card off today is
	// what the × is for (G60).
	LeftOn string
	// Snapshot asks for the board OF the day rather than today's board
	// filtered by it: every card as it stood when that day ended. Only a
	// PAST day has one — today is the live board — and only storage that
	// keeps history can answer (git does).
	Snapshot bool
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
		Snapshot:       q.Get("snapshot") == "true" || q.Get("snapshot") == "1",
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
			// A SUBTASK that carries its own column belongs here on its own
			// merit (G57), not as a rider of a delivered parent: the case
			// the rule exists for is a parent that lives elsewhere — the
			// weekly plan, the working area — and is in no project view at
			// all, which left the whole group visible nowhere.
			if c.Epic == "" {
				continue
			}
			// A MIRROR is the same card standing in a second column, so a
			// project's view holds every card that stands in one of ITS
			// columns, home pair or mirror (G15) — the home-only reading
			// answered an agent asking about a project with less than the
			// board draws there.
			if sel.Project != "" && !board.InProject(c, sel.Project) {
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
	// What the × took off this day, given back to the record of it. The
	// field filters above apply to these too — a card of another team, or
	// somebody else's on a Me board, was not on THIS board that day.
	if sel.LeftOn != "" {
		out = append(out, leftBehindOn(b, sel, out)...)
	}
	out = withSubtasks(b, out, sel)
	if sel.View == "project" {
		out = projectViewCards(b, out, sel.Project)
	}
	if sel.IncludeReviews && (sel.View == "me" || sel.View == "team") {
		out = withLinkedReviews(b, out)
	}
	return out
}

// projectViewCards settles what the Project board's query carries. The grid
// draws a card by its COLUMN, so a subtask without one belongs to no
// Project board — and the subtask rider would otherwise smuggle in every
// child of a columned parent, counted by the client's progress bars and
// drawn by nothing. The PARENT of a delivered subtask rides along instead,
// column or not: the slot is marked with its title, and the client has no
// other query to find it in (the motivating parent — a plan card, a
// working-area card — never carries a column of its own).
func projectViewCards(b board.Board, out []board.Card, project string) []board.Card {
	kept := make([]board.Card, 0, len(out))
	wanted := map[string]bool{}
	for _, c := range out {
		if c.Epic == "" {
			continue
		}
		// The subtask rider ignores the selector, so a child filed under
		// ANOTHER project came in on its parent's ticket: one project's
		// columns are what this view is.
		if project != "" && !board.InProject(c, project) {
			continue
		}
		kept = append(kept, c)
		if c.Parent != "" {
			wanted[c.Parent] = true
		}
	}
	if len(wanted) == 0 {
		return kept
	}
	have := map[string]bool{}
	for _, c := range kept {
		have[c.ItemID] = true
	}
	// In BOARD ORDER, like every other listing: a tail of riders would
	// reshuffle rows on every refetch, which is what withSubtasks goes out
	// of its way to avoid for children.
	ordered := make([]board.Card, 0, len(kept)+len(wanted))
	for _, c := range b.Cards {
		if have[c.ItemID] || wanted[c.ItemID] {
			ordered = append(ordered, c)
		}
	}
	return ordered
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

// leftBehindOn is the cards the × took off the day being read as a record:
// they carry that day in LeftAt, and their dates have since moved into the
// previous sprint. Each is given in the state it has now — which, on the
// board of that evening, is the state it ended the day with.
func leftBehindOn(b board.Board, sel Selector, have []board.Card) []board.Card {
	seen := make(map[string]bool, len(have))
	for _, c := range have {
		seen[c.ItemID] = true
	}
	var out []board.Card
	for _, c := range b.Cards {
		if c.LeftAt != sel.LeftOn || seen[c.ItemID] {
			continue
		}
		if !onThisBoard(c, sel) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// onThisBoard is the scope a view is read in — whose board it is — applied to
// a card the view's own placement rules never saw.
func onThisBoard(c board.Card, sel Selector) bool {
	switch sel.View {
	case "team":
		if !teamInSet(c.Team, sel.Team) {
			return false
		}
	case "me":
		if sel.User != "" && !contains(c.Assignees, sel.User) {
			return false
		}
		if !teamInSet(c.Team, sel.Team) {
			return false
		}
	default:
		return false // only the day boards have a day to give back
	}
	if sel.Stage != nil && string(c.Stage) != *sel.Stage {
		return false
	}
	if sel.Zone != nil && SemanticZone(c.Zone) != *sel.Zone {
		return false
	}
	if sel.Assignee != "" && !contains(c.Assignees, sel.Assignee) {
		return false
	}
	return !sel.Focus || board.Workable(c)
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
