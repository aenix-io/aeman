package apiserver

import (
	"fmt"
	"net/url"

	"github.com/aenix-org/aeman/internal/board"
)

// Selector scopes a card LIST or watch subscription. View selectors reproduce
// exactly what the UI renders (the Team grid, the Me day board, the Weekly
// plan); the plain field selectors compose with no view.
type Selector struct {
	// View is "", "team", "me" or "weekly".
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
	// Plain field selectors, applied on top of the view (or of all cards).
	Stage    *string
	Zone     *string
	Assignee string
}

// ParseSelector reads a selector from query parameters. Unknown views error.
func ParseSelector(q url.Values) (Selector, error) {
	sel := Selector{
		View:     q.Get("view"),
		Team:     q.Get("team"),
		Day:      q.Get("day"),
		User:     q.Get("user"),
		Week:     q.Get("week"),
		Assignee: q.Get("assignee"),
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
	case "", "team", "me", "weekly":
	default:
		return Selector{}, fmt.Errorf("unknown view %q", sel.View)
	}
	return sel, nil
}

// normalized fills the selector's day/week defaults against the wall clock.
func (s Selector) normalized() Selector {
	if s.View == "team" || s.View == "me" {
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
		base = board.TeamGrid(b, sel.Team, sel.Day)
	case "me":
		base = board.MeView(b, sel.User, sel.Day)
	case "weekly":
		bands := board.WeeklyPlan(b, sel.Team, sel.Week)
		base = append(append([]board.Card{}, bands.Wed...), bands.Fri...)
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
		out = append(out, c)
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
	items := make([]Card, 0, len(cards))
	for _, c := range cards {
		items = append(items, CardResource(b, c))
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

func contains(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}
