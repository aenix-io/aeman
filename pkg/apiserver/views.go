package apiserver

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// Selector scopes a card LIST or watch subscription. View selectors reproduce
// exactly what the UI renders (the Team grid, the Me day board, the
// backlogs); the plain field selectors compose with no view.
type Selector struct {
	// View is "", "all", "team", "me", "backlog", "roadmap" or "project". ""
	// and "all" both list every card (the HTTP/MCP layer defaults an
	// unspecified view to the caller's "me").
	View string
	// Team is the team key for the team/backlog views ("" = the no-team
	// group); a comma-separated set lists several.
	Team string
	// Day is the viewed day for the team/me views (defaults to today).
	Day string
	// User is the person for the me view ("" = everyone).
	User string
	// Project filters the project view to one project's scope columns. Empty
	// means every project — the all-projects overview. Note this is the
	// planning entity, NOT the GitHub board (that is addressed by owner+board).
	Project string
	// RecordCards are the cards this listing is a RECORD of — what the day's
	// board took from that evening, by id (board.MergeAsOf). Only they give
	// back what the × took off (LeftOn): a card of a team still inside that
	// sprint is live, and the × took it off that day on purpose. Set together
	// with LeftOn.
	RecordCards map[string]bool
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
	// IncludeReviews brings review cards into a view. On me/team it APPENDS
	// each returned card's linked review, so a client rendering the reviewer
	// badge has it on hand without a second request. Off by default: agents
	// listing a Me board do not want them mixed in.
	IncludeReviews bool
	// ProjectCards brings the PROJECT cards — cards in a Scope column — into
	// the roadmap view, under the planning cards they are filed under. Off by
	// default: the Roadmap is the plan of goals, initiatives, epics and the
	// work under them, and the Project board's cards stay there unless asked
	// for (the Roadmap toolbar's "Project cards" toggle). It means nothing on
	// any other view.
	ProjectCards bool
	// Customer and Owner narrow the roadmap view (docs/design/ownership.md,
	// O7, O8), each a comma-separated set: Customer by effective customers
	// (customer ids), Owner by a planning card's own owner. On the roadmap
	// Team narrows by EFFECTIVE team the same way, the no-team group being
	// its empty member (alone, the set ","). Several values in one are OR,
	// different ones AND; every match comes with its ancestors. Customer
	// narrows the day boards' lists too — team, backlog, all, me — by
	// effective customers (O11).
	Customer string
	Owner    string
}

// ParseSelector reads a selector from query parameters. Unknown views error.
func ParseSelector(q url.Values) (Selector, error) {
	sel := Selector{
		View:           q.Get("view"),
		Team:           q.Get("team"),
		Project:        q.Get("project"),
		Day:            q.Get("day"),
		User:           q.Get("user"),
		Assignee:       q.Get("assignee"),
		Focus:          q.Get("focus") == "true" || q.Get("focus") == "1",
		Snapshot:       q.Get("snapshot") == "true" || q.Get("snapshot") == "1",
		IncludeReviews: q.Get("reviews") == "true" || q.Get("reviews") == "1",
		ProjectCards:   q.Get("projectCards") == "true" || q.Get("projectCards") == "1",
		Customer:       q.Get("customer"),
		Owner:          q.Get("owner"),
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
	case "", "all", "team", "me", "project", "backlog", "roadmap":
	default:
		return Selector{}, fmt.Errorf("unknown view %q", sel.View)
	}
	return sel, nil
}

// ParseViewSelector is the same parse for a door that takes the board from the
// PATH (`/api/v1/views/{view}/cards`): the view is the segment, and a `view=`
// in the query is refused rather than ignored, since a client sending one is
// asking for a board and would be answered from another.
func ParseViewSelector(view string, q url.Values) (Selector, error) {
	if q.Has("view") {
		return Selector{}, fmt.Errorf("view is a path segment now: /api/v1/views/%s/cards", q.Get("view"))
	}
	if !board.KnownView(view) {
		return Selector{}, fmt.Errorf("unknown view %q", view)
	}
	sel, err := ParseSelector(q)
	if err != nil {
		return Selector{}, err
	}
	sel.View = view
	return sel, nil
}

// normalized fills the selector's day default against the wall clock.
func (s Selector) normalized() Selector {
	if (s.View == "team" || s.View == "me") && s.Day == "" {
		s.Day = board.TodayIso()
	}
	return s
}

// roadmapCards is the Roadmap's listing (H9): the hierarchy — every goal,
// initiative and epic, and every card with one of those above it: a task,
// its subtasks, a project card (docs/design/hierarchy.md) — and, below it,
// every OPEN task that has no planning card above it yet: the work in hand
// on the Team and Me boards and the cards parked in a backlog. The
// loose work is listed because the Roadmap is where a lead drags it INTO the
// tree, and a board cannot offer a drag of a card it does not draw.
//
// What stays out: a task already FINISHED outside the hierarchy — there is
// nothing left to plan on it, and a roadmap of every card ever closed is a
// log; a project card in no hierarchy — the Project board's alone (H4); a
// project card IN the hierarchy too, unless the reader asked for them
// (projectCards); and a subtask whose parent task is out, since a subtask
// rides its parent. The
// tree is the client's to assemble from `parent`; the listing is the cards
// that make it up.
func roadmapCards(b board.Board, ix *board.Index, projectCards bool) []board.Card {
	in := map[string]bool{}
	for _, c := range b.Cards {
		if board.IsStateTitle(c.Title) {
			continue
		}
		if c.Scope != "" && !projectCards {
			continue
		}
		switch {
		case board.IsPlanning(c) || ix.HasPlanningAncestor(c):
			in[c.ItemID] = true
		// Open work with no planning ancestor: a task, parked or on a day
		// board. Not a review card — it follows the card it
		// reviews, which is the work, and cannot be filed under anything.
		case c.Scope == "" && c.ReviewOf == "" && !ix.IsSubtask(c) && !board.Complete(c.Stage, c.Progress):
			in[c.ItemID] = true
		}
	}
	// A subtask rides its parent task: in when it is, out when it is not.
	for _, c := range b.Cards {
		if ix.IsSubtask(c) && in[c.Parent] {
			in[c.ItemID] = true
		}
	}
	var out []board.Card
	for _, c := range b.Cards {
		if in[c.ItemID] {
			out = append(out, c)
		}
	}
	return out
}

// Mirrored reports whether a client can work out this selection's membership
// itself — mirror it — so a watch may spare it the ADDED and DELETED of its
// own changes. The Roadmap's team, customer and owner filters are the
// server's alone (roadmapNarrowed): a board filtered by them draws what it
// was sent, and must be sent what its own change put in or took out.
func (sel Selector) Mirrored() bool {
	if sel.View != "roadmap" {
		return true
	}
	return teamSetOf(sel.Team) == nil && setOf(sel.Customer) == nil && setOf(sel.Owner) == nil
}

// roadmapNarrowed applies the Roadmap's team, customer and owner filters
// (O7, O8) to its listing, each judged on its own and AND across them:
//
//   - a PLANNING card passes team (customer) when its own effective team
//     (customers) or any task's under it does, and owner when its OWN owner
//     (never inherited) or a task's under it assignee is one of them;
//   - a TASK or subtask passes team and customer by its effective values,
//     and owner when one of its assignees is one of them;
//
// and every card listed comes with its ancestors. With no filter the
// listing is as it was.
func roadmapNarrowed(ix *board.Index, cards []board.Card, sel Selector) []board.Card {
	teams, customers, owners := teamSetOf(sel.Team), setOf(sel.Customer), setOf(sel.Owner)
	if teams == nil && customers == nil && owners == nil {
		return cards
	}
	teamOK := func(c board.Card) bool { return teams == nil || teams[ix.EffectiveTeam(c)] }
	customerOK := func(c board.Card) bool { return customerOK(ix, c, customers) }
	// A person matches a plan item they OWN (never inherited) and a task or
	// subtask they are ASSIGNED to (O8).
	ownerOK := func(c board.Card) bool {
		if owners == nil {
			return true
		}
		if board.IsPlanning(c) {
			return owners[c.Owner]
		}
		for _, a := range c.Assignees {
			if owners[a] {
				return true
			}
		}
		return false
	}
	listed := map[string]bool{}
	for _, c := range cards {
		listed[c.ItemID] = true
	}
	// The tasks under each planning card, through every level, as listed.
	tasksUnder := func(p board.Card) []board.Card {
		var out []board.Card
		seen := map[string]bool{p.ItemID: true}
		for frontier := []string{p.ItemID}; len(frontier) > 0; {
			var next []string
			for _, id := range frontier {
				for _, c := range ix.ChildrenOf(id) {
					if seen[c.ItemID] || !listed[c.ItemID] {
						continue
					}
					seen[c.ItemID] = true
					next = append(next, c.ItemID)
					if !board.IsPlanning(c) {
						out = append(out, c)
					}
				}
			}
			frontier = next
		}
		return out
	}
	planPasses := func(p board.Card) bool {
		under := tasksUnder(p)
		anyUnder := func(ok func(board.Card) bool) bool {
			for _, t := range under {
				if ok(t) {
					return true
				}
			}
			return false
		}
		return (teamOK(p) || anyUnder(teamOK)) && (customerOK(p) || anyUnder(customerOK)) &&
			(ownerOK(p) || anyUnder(ownerOK))
	}
	passedPlans := map[string]bool{}
	for _, c := range cards {
		if board.IsPlanning(c) && planPasses(c) {
			passedPlans[c.ItemID] = true
		}
	}
	keep := map[string]bool{}
	include := func(c board.Card) {
		keep[c.ItemID] = true
		for _, a := range ix.Lineage(c) {
			keep[a.ItemID] = true
		}
	}
	for _, c := range cards {
		if board.IsPlanning(c) {
			if passedPlans[c.ItemID] {
				include(c)
			}
			continue
		}
		if !teamOK(c) || !customerOK(c) || !ownerOK(c) {
			continue
		}
		include(c)
	}
	var out []board.Card
	for _, c := range cards {
		if keep[c.ItemID] && listed[c.ItemID] {
			out = append(out, c)
		}
	}
	return out
}

// setOf reads a comma-separated selector value as a set; nil when empty.
// teamSetOf is a team selector's set, the no-team group being its empty
// member: "portal," is portal and the no-team group, "," the no-team group
// alone — as teamInSet reads it on every other board. An empty selector is
// no filter (nil).
func teamSetOf(v string) map[string]bool {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	out := map[string]bool{}
	for _, x := range strings.Split(v, ",") {
		out[strings.TrimSpace(x)] = true
	}
	return out
}

// customerOK reports whether a card's effective customers (O4) include one
// of the set; a nil set passes everything.
func customerOK(ix *board.Index, c board.Card, set map[string]bool) bool {
	if set == nil {
		return true
	}
	for _, id := range ix.EffectiveCustomers(c) {
		if set[id] {
			return true
		}
	}
	return false
}

func setOf(v string) map[string]bool {
	var out map[string]bool
	for _, x := range strings.Split(v, ",") {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[x] = true
	}
	return out
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
	case "project":
		// The Project board: every card filed under an scope, whatever its week
		// — the client lays rows (weeks) and columns (scopes) out itself. The
		// selector filters by PROJECT (an scope's owner), not by team: a project
		// spans teams, and a card is assigned to a team from this very board.
		for _, c := range b.Cards {
			// A card in a column is a PROJECT CARD, and this is the one board
			// that draws it (H4): no team, no owner, no parent — it stands in
			// its column on its own merit.
			if c.Scope == "" {
				continue
			}
			if sel.Project != "" && !board.InProject(c, sel.Project) {
				continue
			}
			base = append(base, c)
		}
	case "backlog":
		base = backlogCards(b, board.NewIndex(b), sel)
	case "roadmap":
		ix := board.NewIndex(b)
		base = roadmapNarrowed(ix, roadmapCards(b, ix, sel.ProjectCards), sel)
	default:
		base = b.Cards
	}
	out := make([]board.Card, 0, len(base))
	// The day boards' Customer filter (O11): the roadmap applies its own
	// (roadmapNarrowed), and the Project board's cards carry none (H4).
	var dayCustomers map[string]bool
	var dayIx *board.Index
	if sel.View != "roadmap" && sel.View != "project" {
		if dayCustomers = setOf(sel.Customer); dayCustomers != nil {
			dayIx = board.NewIndex(b)
		}
	}
	for _, c := range base {
		if dayCustomers != nil && !customerOK(dayIx, c, dayCustomers) {
			continue
		}
		// "done" is not a stored stage — it is DERIVED from progress, so a
		// card that is finished has no stage at all — and comparing it
		// against the stored one asked for a file that says `stage: done`,
		// which no door on this board writes. It is a value the tools
		// advertise, so it has to ask the board's own question.
		if sel.Stage != nil {
			if *sel.Stage == string(board.StageDone) {
				if !board.Complete(c.Stage, c.Progress) {
					continue
				}
			} else if string(c.Stage) != *sel.Stage {
				continue
			}
		}
		if sel.Zone != nil && SemanticZone(board.ZoneOf(c)) != *sel.Zone {
			continue
		}
		if sel.Assignee != "" && !contains(c.Assignees, sel.Assignee) {
			continue
		}
		// team filters the views that are not already scoped by it (the me, all
		// and default lists). It accepts a comma-separated set, so
		// ?view=me&team=marketing,portal narrows the Me board to those
		// teams — the Me view's team-focus toggle over the selected chips.
		if offTheTeamList(c, sel) {
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
		if back := leftBehindOn(b, sel, out); len(back) > 0 {
			out = append(out, back...)
			// A listing is served in BOARD order; appending to the tail put
			// the given-back cards after everything regardless of rank.
			sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
		}
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

// projectViewCards settles what the Project board's query carries: the cards
// that stand in a column, and nothing that rode in with them. The grid draws
// a card by its COLUMN, and a project card is nobody's subtask and has none
// (H4), so a parented card the subtask rider appended — one written before
// schema 2 — belongs to no Project board and would only be counted by a
// progress bar that draws nothing.
func projectViewCards(_ board.Board, out []board.Card, project string) []board.Card {
	kept := make([]board.Card, 0, len(out))
	for _, c := range out {
		if c.Scope == "" {
			continue
		}
		if project != "" && !board.InProject(c, project) {
			continue
		}
		kept = append(kept, c)
	}
	return kept
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
	planning := map[string]bool{}
	for _, c := range out {
		present[c.ItemID] = true
		if board.IsPlanning(c) {
			planning[c.ItemID] = true
		}
	}
	extra := map[string][]board.Card{}
	for _, c := range b.Cards {
		if c.Parent == "" || !present[c.Parent] || present[c.ItemID] {
			continue
		}
		// Only a SUBTASK rides its parent: what stands under a planning card
		// is that card's child, listed — or left out by a filter — on its
		// own merit (O7).
		if planning[c.Parent] {
			continue
		}
		// The rider does not bring back a project card the roadmap left out
		// on purpose: one filed under a planning card is its CHILD, not a
		// piece of it.
		if sel.View == "roadmap" && c.Scope != "" && !sel.ProjectCards {
			continue
		}
		if offTheTeamList(c, sel) {
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
	customers := setOf(sel.Customer)
	var ix *board.Index
	if customers != nil {
		ix = board.NewIndex(b)
	}
	for _, c := range b.Cards {
		if c.LeftAt != sel.LeftOn || seen[c.ItemID] {
			continue
		}
		if !customerOK(ix, c, customers) {
			continue
		}
		// Only what this listing is a record OF: elsewhere the day is still
		// being worked, and the × is what takes a card off it there.
		if !sel.RecordCards[c.ItemID] {
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
	if sel.Zone != nil && SemanticZone(board.ZoneOf(c)) != *sel.Zone {
		return false
	}
	if sel.Assignee != "" && !contains(c.Assignees, sel.Assignee) {
		return false
	}
	return !sel.Focus || board.Workable(c)
}

// MarkRecords stamps the cards that came FROM the past with the moment they
// are from: everything else in the same listing is today's and stays
// workable, so the mark is per card.
//
// records is what the day's board took from that evening (board.MergeAsOf's
// own answer), by card id. Deriving it again from team names loses the cards
// whose team differs between the two moments — one moved between teams, one
// of a team renamed since — and such a card comes through LIVE: draggable,
// editable, and refused by every write door. Which is the refusal G60 names
// as the thing not to do.
func MarkRecords(list *CardList, records map[string]bool, asOf string) {
	if asOf == "" || len(records) == 0 {
		return
	}
	list.AsOf = asOf
	for i := range list.Items {
		item := &list.Items[i]
		if records[item.Metadata.UID] {
			item.Status.AsOf = asOf
		}
	}
}

// ListCards builds the LIST response for a selector: resources in board order.
func ListCards(b board.Board, sel Selector) CardList {
	cards := FilterCards(b, sel)
	// One index for the whole listing: every resource asks the board the
	// same questions (lineage, standing, backlog, reviewer), and asking them
	// afresh per card was a walk of the board per card.
	ix := board.NewIndex(b)
	resource := CardSummaryResourceIn
	if sel.Fields == "full" {
		resource = CardResourceIn
	}
	items := make([]Card, 0, len(cards))
	for _, c := range cards {
		items = append(items, resource(b, ix, c))
	}
	return CardList{Kind: "CardList", Items: items}
}

// offTheTeamList is a card the team selector leaves off the views it
// narrows (me, all and the default list). A planning card is never on one
// narrowed by a team: its team says who carries it out (O1) and puts it on
// no day board (H3), so these lists hold tasks under a team, as they did
// before planning cards had one.
func offTheTeamList(c board.Card, sel Selector) bool {
	if sel.View != "me" && sel.View != "" && sel.View != "all" {
		return false
	}
	if strings.TrimSpace(sel.Team) == "" {
		return false
	}
	return board.IsPlanning(c) || !teamInSet(c.Team, sel.Team)
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

// backlogCards is the view=backlog listing (docs/design/backlog.md, K2):
// each named team's parked open tasks — no planning card, no project card,
// no subtask, no review card, nothing finished (board.OnBacklog) — in the
// order a backlog is read: by hand first, then oldest first
// (board.BacklogOrder). `team` names one backlog or a comma-separated set of
// them; the Backlog board draws them as one list, in this same order.
//
// A review NOBODY HAS TAKEN (board.NeedsReviewer) stands in the same order
// as the tasks: it is made at the top of the board's order, so it shows
// first until somebody drags it (K10).
func backlogCards(b board.Board, ix *board.Index, sel Selector) []board.Card {
	var out []board.Card
	for _, c := range b.Cards {
		if !ix.OnBacklog(c) || !teamInSet(c.Team, sel.Team) {
			continue
		}
		out = append(out, c)
	}
	return board.BacklogOrder(out)
}

// Drawn reports whether a card is on the board the selector describes — the
// question a view-scoped GESTURE asks before it acts.
//
// A person cannot press × on a card they cannot see, and an agent standing on
// the Me board should not be able to either: it has to say `view=team&team=X`
// (or `view=all`), and then it has said which board it is acting from. The
// answer comes from the LISTING itself rather than from a second set of rules
// beside it, because a gate that drifted from the listing would be a card the
// board draws and refuses to act on — or, worse, one it acts on and never
// shows.
//
// It is asked of the card as it stands BEFORE the write, never after: sending
// work off today's board is the point of half these gestures, and a gate on
// the result would forbid exactly the ones that move it.
func Drawn(b board.Board, sel Selector, uid string) bool {
	for _, c := range FilterCards(b, sel) {
		if c.ItemID == uid {
			return true
		}
	}
	return false
}
