// Package apiserver is the Kubernetes-style resource layer of the aeman API:
// it translates the internal board domain into Card/Sprint/Note/Ordering
// resources (metadata/spec/status), evaluates LIST selectors for the Team, Me
// and Backlog views, and derives the fields the UI would otherwise compute.
// It is backend-agnostic: everything works on the domain board snapshot.
package apiserver

import (
	"sort"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// Zones are addressed by meaning in the API, not colour. The domain (and the
// GitHub backend's option matching) keeps the colour keys.
var zoneToSemantic = map[board.ZoneKey]string{
	board.ZoneRed:    "urgent",
	board.ZoneYellow: "unplanned",
	board.ZoneGray:   "planned",
	board.ZoneGreen:  "niceToHave",
}

var semanticToZone = map[string]board.ZoneKey{
	"urgent":     board.ZoneRed,
	"unplanned":  board.ZoneYellow,
	"planned":    board.ZoneGray,
	"niceToHave": board.ZoneGreen,
}

// SemanticZone maps a domain zone onto its API name ("" stays "").
func SemanticZone(z board.ZoneKey) string { return zoneToSemantic[z] }

// DomainZone maps an API zone name onto the domain key ("" stays "").
func DomainZone(name string) board.ZoneKey { return semanticToZone[name] }

// Card is a project item as an API resource.
type Card struct {
	Kind     string       `json:"kind"`
	Metadata CardMetadata `json:"metadata"`
	Spec     CardSpec     `json:"spec"`
	Status   CardStatus   `json:"status"`
}

// CardMetadata is the card's identity and immutable facts.
type CardMetadata struct {
	UID       string `json:"uid"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// CardDates is the card's date model: its scheduled start, the end of its
// visible range, and the sprint it belongs to (see docs/dates.md).
type CardDates struct {
	Start  string `json:"start,omitempty"`
	End    string `json:"end,omitempty"`
	Sprint string `json:"sprint,omitempty"`
}

// CardSpec is the user's intent — everything an edit can change.
type CardSpec struct {
	Title string `json:"title"`
	// Description is nil in a summary listing ("body not included" — the
	// row shape) and always present, possibly empty, on a full resource.
	// The distinction is the client's "loaded" marker: only nil means "go
	// fetch the body", so an empty body never reads as a missing one.
	Description *string `json:"description,omitempty"`
	Team        string  `json:"team,omitempty"`
	// Owner is the one person who answers for a goal, an initiative or an
	// epic (docs/design/ownership.md, O1); absent on every other card.
	Owner string `json:"owner,omitempty"`
	// Customers are the ids of the board's customers this card is for, as
	// written on the card itself; status.effectiveCustomers adds the
	// ancestors'.
	Customers []string `json:"customers,omitempty"`
	Zone      string   `json:"zone,omitempty"`
	// Size is what somebody said the card weighs: S, M, L or XL, or absent.
	Size      string   `json:"size,omitempty"`
	Assignees []string `json:"assignees"`
	Progress  int      `json:"progress"`
	Stage     string   `json:"stage,omitempty"`
	// Recurrence, on a recurrent card, is its reseed cycle: "" = every
	// sprint, "week" / "month" = once the interval has elapsed.
	Recurrence string    `json:"recurrence,omitempty"`
	Dates      CardDates `json:"dates"`
	// Parked says the task is in its team's BACKLOG rather than in work.
	// Every team has one and nothing declares it. A task is in work (a date
	// or a sprint) or parked, never both and never neither
	// (docs/design/backlog.md, K1). A task has no week (K7).
	Parked bool `json:"parked,omitempty"`
	// Priority is the card's OWN priority — P0 to P4, or none (K15); what
	// it counts under is status.effectivePriority.
	Priority string `json:"priority,omitempty"`
	ReviewOf string `json:"reviewOf,omitempty"`
	// Parent is the uid of the card this one sits under — the ONE link of
	// the hierarchy. What it means is the parent's type: under a task this
	// card is a subtask; under a goal, an initiative or an epic it is filed
	// in the hierarchy and nothing else about it changes
	// (docs/design/hierarchy.md).
	Parent string `json:"parent,omitempty"`
	// Type is the card's level: absent for a task, else "goal",
	// "initiative" or "epic" — a PLANNING card, drawn on the Roadmap and on
	// no day board. Fixed at create.
	Type string `json:"type,omitempty"`
}

// LineageRef is one ancestor of a card, in status.lineage: root first, so a
// client draws the breadcrumb straight from the list.
type LineageRef struct {
	UID   string `json:"uid"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title"`
	// End is the ancestor's end date, when it has one: a planning card's
	// bar ends there on the Roadmap, and the Backlog shows the nearest
	// planning card's end as the due date of the task under it.
	End string `json:"end,omitempty"`
}

// ReviewedRef is the card a review card reviews, in status.reviewed: enough
// to name it, open it and draw whose it is (the small counterpart avatar)
// without a lookup.
type ReviewedRef struct {
	UID       string   `json:"uid"`
	Type      string   `json:"type,omitempty"`
	Title     string   `json:"title"`
	Assignees []string `json:"assignees,omitempty"`
}

// Hierarchy is a planning card's derived standing (status.hierarchy): where
// the tasks under it stand, through every level, and how many of them there
// are. Never stored — read off the tree on every listing.
type Hierarchy struct {
	// Status is no-items, backlog, in-progress or done (board.HierarchyStatus).
	Status string `json:"status"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
}

// CardStatus is derived by the server, never written by clients.
type CardStatus struct {
	Complete    bool   `json:"complete"`
	InProgress  bool   `json:"inProgress"`
	ReviewedBy  string `json:"reviewedBy,omitempty"`
	ReviewRound int    `json:"reviewRound,omitempty"`
	// AsOf marks a card served from the PAST: its team's sprint has moved on
	// past the day asked for, so this is what the card was that evening, not
	// what it is. A record — a client shows it as one and does not offer to
	// change it. Absent on every live card, including on the same listing:
	// whether a day is over is each team's own answer (G60).
	AsOf string `json:"asOf,omitempty"`
	// Domain is the repository the card lives in — the store's decision by
	// the inheritance rule, never a client's choice. The store stamps every
	// card, the primary's included, so this normally carries a NAME on a
	// multi-repository board and the client compares it against the primary
	// (cardDomainBadge) rather than reading an empty string as "primary".
	// Empty only where nothing stamped it at all — a board assembled by
	// hand; the git server stamps every card, one repository or many.
	Domain string `json:"domain,omitempty"`
	// DoneAt is the board day the card reached 100 (cleared on reopen) — the
	// day boards show a done card that day and drop it the next.
	DoneAt string `json:"doneAt,omitempty"`
	// LeftAt is the board day the older × demoted the card into the previous
	// sprint, which a RECORD of that day gives back (G60). Nothing writes it
	// any more; the cards that carry it still stand in the history.
	LeftAt string `json:"leftAt,omitempty"`
	// Links are the references extracted from the card's description —
	// unresolved (no titles or states; GET /cards/{uid}/links resolves those).
	// They ride the status so a summary listing, which omits the description
	// itself, still tells a row it has links to show.
	Links []CardLinkRef `json:"links,omitempty"`
	// Lineage is every ancestor of the card, root first — the breadcrumb a
	// detail pane draws above it. Absent on a root.
	Lineage []LineageRef `json:"lineage,omitempty"`
	// Reviewed is, on a REVIEW card, the card it reviews — its uid, title and
	// people — so a client shows "Reviews: «task»", opens it and draws whose
	// it is without a lookup.
	// Absent on every other card, and when the reviewed card is gone.
	Reviewed *ReviewedRef `json:"reviewed,omitempty"`
	// EffectiveTeam is the team the card counts under (O3): its own, else
	// the nearest ancestor's that has one. Computed, never written.
	EffectiveTeam string `json:"effectiveTeam,omitempty"`
	// EffectivePriority is the priority the card counts under (K15): its
	// own, else the nearest ancestor's that has one; a review card's is its
	// task's. Computed, never written. EffectivePriorityFrom names the
	// ancestor it is taken from — absent when it is the card's own.
	EffectivePriority     string      `json:"effectivePriority,omitempty"`
	EffectivePriorityFrom *LineageRef `json:"effectivePriorityFrom,omitempty"`
	// EffectiveCustomers is who the card is for (O4): every ancestor's
	// customers and its own, the ancestors' first; a review card's are its
	// task's (O6). Computed, never written.
	EffectiveCustomers []string `json:"effectiveCustomers,omitempty"`
	// DueDate is a task's due date (named apart from the retired Triage
	// status.due, a list of weeks): the end of its NEAREST planning card — the
	// epic it is for — never one borrowed from a farther ancestor. Absent
	// with no planning card above it, when the nearest has no end, and on a
	// planning card (its own end is its date).
	DueDate string `json:"dueDate,omitempty"`
	// Hierarchy is a PLANNING card's derived standing, from the tasks under
	// it. Absent on a task, whose stage and progress are its own.
	Hierarchy *Hierarchy `json:"hierarchy,omitempty"`
}

// maxStatusLinks bounds the derived refs a status carries; the full resolved
// list is always available from GET /cards/{uid}/links.
const maxStatusLinks = 50

// CardLinkRef is one extracted reference: a GitHub issue/PR or a plain URL.
type CardLinkRef struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Owner  string `json:"owner,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Number int    `json:"number,omitempty"`
}

// Sprint is a team's sprint pointer as an API resource (name = the team key,
// "" is the no-team group).
type Sprint struct {
	Kind     string         `json:"kind"`
	Metadata SprintMetadata `json:"metadata"`
	Spec     SprintSpec     `json:"spec"`
}

type SprintMetadata struct {
	Team string `json:"team"`
}

type SprintSpec struct {
	Current  string `json:"current,omitempty"`
	Previous string `json:"previous,omitempty"`
	// Capacity is what a week of the team's plan is weighed against.
	Capacity *SprintCapacity `json:"capacity,omitempty"`
}

// SprintCapacity is a team's capacity as the API states it: the POINTS a week
// it gets through, a number somebody SET and never derived — absent when
// nobody has said. No board draws it at the moment (the Triage board's load
// panel went with that board); it is kept for the backlog to show load again.
type SprintCapacity struct {
	Points int `json:"points,omitempty"`
}

// Note is a work note as an API resource, a subresource of a card.
type Note struct {
	Kind     string       `json:"kind"`
	Metadata NoteMetadata `json:"metadata"`
	Spec     NoteSpec     `json:"spec"`
}

type NoteMetadata struct {
	ID        string `json:"id"`
	CardUID   string `json:"cardUid"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	Source    string `json:"source"`
}

type NoteSpec struct {
	Text string `json:"text"`
}

// Ordering is the board-level manual order: the uid list clients sort by.
type Ordering struct {
	Kind string       `json:"kind"`
	Spec OrderingSpec `json:"spec"`
}

type OrderingSpec struct {
	UIDs []string `json:"uids"`
}

// BoardInfo is the read-only board resource: identity plus the team roster
// (the teams that have sprint pointers).
type BoardInfo struct {
	Kind     string        `json:"kind"`
	Metadata BoardMetadata `json:"metadata"`
}

// CustomerRef is one entry of the board's customer roster.
type CustomerRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type BoardMetadata struct {
	Title string   `json:"title,omitempty"`
	URL   string   `json:"url,omitempty"`
	Teams []string `json:"teams"`
	// Members is every distinct assignee on the board — the people roster for
	// pickers (assign, review, view-as) now that clients load one view at a
	// time and cannot derive it from the cards they hold — with the avatar
	// the server's forge adapter resolves for each, so a client never
	// assembles a forge URL of its own.
	Members []Member `json:"members"`
	// Domains are the repositories the visitor can read, primary first: which
	// they may write — a team is declared in one they pick when
	// more than one is writable — and which members can read each,
	// so a reviewer picker offers only people who will see the card. Absent
	// primary first. The board lists its repositories whatever it spans
	// (G59), so a client compares the stamps it carries against this list
	// and reads a single entry as "no boundaries", never an absent one.
	Domains []DomainInfo `json:"domains,omitempty"`
	// Customers is the board's customer roster in board order: the ids the
	// cards name and the names to show (docs/design/ownership.md, O5).
	Customers []CustomerRef `json:"customers,omitempty"`
	// TeamDomains names the repository a team was declared in, for the
	// teams that live OUTSIDE the primary — the primary is the default and
	// needs no entry, and a single-repository board names none at all.
	TeamDomains map[string]string `json:"teamDomains,omitempty"`
}

// Member is one person on the board: a login and, when the server knows the
// forge, an avatar image URL and — on a forge that has them (GitLab) — a
// display name. The login is the identity everywhere; the name is for eyes.
type Member struct {
	Login     string `json:"login"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	// Carrying is how much open work is this person's right now, counted
	// across EVERY team — a board is read through a filter and a person is
	// not, so handing somebody a card without the whole number in front of
	// you is a decision made in the dark (board.CarryingNow).
	Carrying int `json:"carrying,omitempty"`
	// Load is the same work WEIGHED — the points of what the person is
	// carrying (board.LoadNow) — and Capacity the points a week they get
	// through: the roster's number, absent when nobody has set one
	// (board.CapacityOfPerson). A day board draws load/capacity beside the
	// person and goes red past it; with no capacity it draws the load alone.
	Load     int `json:"load,omitempty"`
	Capacity int `json:"capacity,omitempty"`
}

// DomainInfo is one readable domain of the visitor's board.
type DomainInfo struct {
	Name     string   `json:"name"`
	Writable bool     `json:"writable"`
	Members  []string `json:"members"`
}

// CardList is the LIST response envelope.
type CardList struct {
	Kind  string `json:"kind"`
	Items []Card `json:"items"`
	// AsOf is the moment a snapshot listing reflects (RFC3339) — the end of
	// the day it was asked for. Empty on a live listing; a day the history
	// no longer reaches is refused (410) rather than answered.
	AsOf string `json:"asOf,omitempty"`
	// BoardOrder is, on a backlog listing, the whole board's order — what
	// every Ordering frame carries, and what the listing's order is read
	// from (K14). A client places a card the watch brings in, and re-sorts
	// on an Ordering frame, by the same key (board.BacklogKey). Absent on
	// every other listing.
	BoardOrder []string `json:"boardOrder,omitempty"`
}

// CardResource maps a domain card onto the API resource, deriving status from
// the board (reviewedBy needs the linked review card's assignee).
// CardSummaryResource is CardResource without the card body: the shape of a
// board row. The description — often the bulk of a card's bytes, and unused
// by row rendering — stays behind for GET /cards/{uid}; the derived link refs
// in status keep the row's links indicator honest without it.
func CardSummaryResource(b board.Board, c board.Card) Card {
	return CardSummaryResourceIn(b, board.NewIndex(b), c)
}

// CardSummaryResourceIn is CardSummaryResource over an index the caller
// built once for a whole listing.
func CardSummaryResourceIn(b board.Board, ix *board.Index, c board.Card) Card {
	res := CardResourceIn(b, ix, c)
	res.Spec.Description = nil
	return res
}

// CardResource is the resource for one card. It reads the board once (an
// Index) for the questions that walk it — the lineage, a planning card's
// standing, the reviewer. A LISTING builds the index once
// and asks CardResourceIn of every card: three thousand cards walking the
// board for each of those questions was the listing's whole cost, paid again
// for every watcher's projection.
func CardResource(b board.Board, c board.Card) Card {
	return CardResourceIn(b, board.NewIndex(b), c)
}

// CardResourceIn is CardResource over an index built once for a listing.
func CardResourceIn(b board.Board, ix *board.Index, c board.Card) Card {
	description := c.Description
	spec := CardSpec{
		Title:       c.Title,
		Description: &description,
		Team:        c.Team,
		Owner:       c.Owner,
		Customers:   ix.OnRoster(c.Customers),
		Zone:        SemanticZone(board.ZoneOf(c)),
		Size:        sizeOut(c.Size),
		Assignees:   append([]string{}, c.Assignees...),
		Progress:    c.Progress,
		Stage:       string(c.Stage),
		Recurrence:  c.Recurrence,
		Dates:       CardDates{Start: c.StartDate, End: c.Day, Sprint: c.SprintStart},
		Parked:      c.Parked,
		Priority:    string(c.Priority),
		ReviewOf:    c.ReviewOf,
		Parent:      c.Parent,
		Type:        c.Type,
	}
	status := CardStatus{
		Complete:    board.Complete(c.Stage, c.Progress),
		InProgress:  board.IsInProgress(c),
		ReviewRound: c.ReviewRound,
		Domain:      c.Domain,
		DoneAt:      c.DoneAt,
		LeftAt:      c.LeftAt,
	}
	if !board.IsStateTitle(c.Title) {
		status.EffectiveTeam = ix.EffectiveTeam(c)
		if p, from := ix.EffectivePriority(c); p != board.PriorityNone {
			status.EffectivePriority = string(p)
			if from.ItemID != "" {
				status.EffectivePriorityFrom = &LineageRef{UID: from.ItemID, Type: from.Type, Title: from.Title}
			}
		}
		status.EffectiveCustomers = ix.EffectiveCustomers(c)
	}
	up := ix.Lineage(c)
	// A REVIEW card hangs under nothing of its own; its chain is the
	// planning chain of the task it reviews (K10: the Backlog draws a review
	// nobody has taken with it), never the task itself, which would read as
	// a task parent. A parent of its own — a direct writer's stray, which no
	// door gives a review card — is not on it either: the backlog places the
	// review by the task's chain (board.BacklogKey), and the client reads
	// that place off this lineage.
	if c.ReviewOf != "" {
		if original, ok := ix.Find(c.ReviewOf); ok {
			status.Reviewed = &ReviewedRef{UID: original.ItemID, Type: original.Type, Title: original.Title,
				Assignees: original.Assignees}
		}
	}
	if c.ReviewOf != "" {
		up = nil
		if original, ok := ix.Find(c.ReviewOf); ok {
			for _, a := range ix.Lineage(original) {
				if board.IsPlanning(a) {
					up = append(up, a)
				}
			}
		}
	}
	for _, a := range up {
		status.Lineage = append(status.Lineage, LineageRef{UID: a.ItemID, Type: a.Type, Title: a.Title, End: a.Day})
	}
	if !board.IsPlanning(c) {
		for i := len(up) - 1; i >= 0; i-- {
			if board.IsPlanning(up[i]) {
				status.DueDate = up[i].Day
				break
			}
		}
	}
	if board.IsPlanning(c) {
		st, done, total := ix.HierarchyStatus(c)
		status.Hierarchy = &Hierarchy{Status: st, Done: done, Total: total}
	}
	for _, l := range board.ExtractLinks(c.Description) {
		// A row needs an indicator and a menu, not an inventory: a
		// pathological body of hundreds of URLs must not make the "light"
		// row heavier than the description it replaced.
		if len(status.Links) == maxStatusLinks {
			break
		}
		status.Links = append(status.Links, CardLinkRef{
			Kind: l.Kind, URL: l.URL, Owner: l.Owner, Repo: l.Repo, Number: l.Number,
		})
	}
	status.ReviewedBy = ix.ReviewedBy(c)
	return Card{
		Kind: "Card",
		Metadata: CardMetadata{
			UID:       c.ItemID,
			Author:    c.Author,
			CreatedAt: c.CreatedAt,
		},
		Spec:   spec,
		Status: status,
	}
}

// SprintResources maps the board's per-team pointers onto Sprint resources,
// sorted by team for stable output.
func SprintResources(b board.Board) []Sprint {
	teams := make([]string, 0, len(b.SprintStates))
	for t := range b.SprintStates {
		teams = append(teams, t)
	}
	sortStrings(teams)
	out := make([]Sprint, 0, len(teams))
	for _, t := range teams {
		out = append(out, SprintResourceOf(b, t))
	}
	return out
}

// SprintResourceOf shapes ONE team's sprint resource. The watch frame for a
// team and the listing go through it together, so a frame cannot carry less
// than the listing does — which is how a client that merges frames in place
// came to lose the team's capacity every time a sprint pointer moved.
func SprintResourceOf(b board.Board, team string) Sprint {
	st := b.SprintStates[team]
	return Sprint{
		Kind:     "Sprint",
		Metadata: SprintMetadata{Team: team},
		Spec: SprintSpec{Current: st.Current, Previous: st.Previous,
			Capacity: &SprintCapacity{Points: board.PointsAWeekOf(b, team)}},
	}
}

// sizeOut is the size a card resource states: one of the four letters, or
// nothing. The storage is open, so a card can carry a size nothing knows
// (`size: large`); the board weighs such a card as unsized, and the API has
// to say the same thing rather than echo a value its own PATCH would refuse.
func sizeOut(s board.SizeKey) string {
	if board.Points(s) > 0 {
		return string(s)
	}
	return ""
}

// NoteResources maps a card's notes onto Note resources.
func NoteResources(c board.Card) []Note {
	out := make([]Note, 0, len(c.Notes))
	for _, n := range c.Notes {
		out = append(out, Note{
			Kind: "Note",
			Metadata: NoteMetadata{
				ID:        n.ID,
				CardUID:   c.ItemID,
				Author:    n.Author,
				CreatedAt: n.CreatedAt,
				Source:    n.Source,
			},
			Spec: NoteSpec{Text: n.Body},
		})
	}
	return out
}

// OrderingResource is the board's manual order as a resource.
func OrderingResource(b board.Board) Ordering {
	uids := make([]string, 0, len(b.Cards))
	for _, c := range b.Cards {
		uids = append(uids, c.ItemID)
	}
	return Ordering{Kind: "Ordering", Spec: OrderingSpec{UIDs: uids}}
}

// BoardResource is the read-only board identity resource, members without
// avatars.
func BoardResource(b board.Board) BoardInfo {
	return BoardResourceWith(b, nil)
}

// BoardResourceWith is BoardResource with the members' avatars resolved by
// the given hook (nil leaves them empty).
func BoardResourceWith(b board.Board, avatar func(login string) string) BoardInfo {
	if avatar == nil {
		return BoardResourceWithPeople(b, nil)
	}
	return BoardResourceWithPeople(b, func(login string) Member {
		return Member{Login: login, AvatarURL: avatar(login)}
	})
}

// MembersOf is the board's people with the numbers beside each: everyone who
// is assigned anything, in login order, with what they are carrying (cards),
// what it weighs (points) and how many points a week they get through.
//
// It is separate from the board resource because those numbers change on
// every card write while the rest of the metadata — teams, columns,
// processes — changes rarely: a watcher can be told the people alone, which
// is a frame of a couple of kilobytes rather than the whole roster.
func MembersOf(b board.Board, person func(login string) Member) []Member {
	seen := map[string]bool{}
	logins := []string{}
	for _, c := range b.Cards {
		for _, a := range c.Assignees {
			if a != "" && !seen[a] {
				seen[a] = true
				logins = append(logins, a)
			}
		}
	}
	sortStrings(logins)
	today := board.TodayIso()
	carrying := board.CarryingNow(b, today)
	load := board.LoadNow(b, today)
	people := make([]Member, 0, len(logins))
	for _, login := range logins {
		m := Member{Login: login, Carrying: carrying[login], Load: load[login],
			Capacity: board.CapacityOfPerson(b, login)}
		if person != nil {
			p := person(login)
			m.Name, m.AvatarURL = p.Name, p.AvatarURL
		}
		people = append(people, m)
	}
	return people
}

// BoardResourceWithPeople is BoardResource with the members resolved by the
// given hook — the forge's avatar and display name for a login (nil leaves
// both empty). The hook's Login is ignored: the board's login is the identity.
func BoardResourceWithPeople(b board.Board, person func(login string) Member) BoardInfo {
	// Teams come out in BOARD order (the sprint-state cards' positions — the
	// shared, server-side team order); stragglers the order does not know
	// (defensive: map entries without an order slot) append sorted.
	teams := make([]string, 0, len(b.SprintStates))
	inOrder := map[string]bool{}
	for _, t := range b.TeamOrder {
		if _, ok := b.SprintStates[t]; ok && !inOrder[t] {
			inOrder[t] = true
			teams = append(teams, t)
		}
	}
	rest := make([]string, 0)
	for t := range b.SprintStates {
		if !inOrder[t] {
			rest = append(rest, t)
		}
	}
	sortStrings(rest)
	teams = append(teams, rest...)
	people := MembersOf(b, person)
	return BoardInfo{
		Kind: "Board",
		Metadata: BoardMetadata{Title: b.Title, URL: b.URL, Teams: teams,
			Members: people, Customers: customerRefs(b),
			TeamDomains: rosterDomains(teams, func(name string) string { return board.TeamDomain(b, name) })},
	}
}

func customerRefs(b board.Board) []CustomerRef {
	var out []CustomerRef
	for _, c := range b.Customers {
		out = append(out, CustomerRef{ID: c.ID, Name: c.Name})
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// LogEntry is one item of a card's unified activity feed: a recorded event
// (stage/progress/review/plan change) or a work note, in one timeline.
type LogEntry struct {
	// Type is "event" or "note".
	Type string `json:"type"`
	ID   string `json:"id"`
	At   string `json:"at,omitempty"`
	// Actor is the event's actor or the note's author.
	Actor string `json:"actor,omitempty"`
	// Event fields (Type == "event").
	EventKind string `json:"kind,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	// Note body (Type == "note").
	Text string `json:"text,omitempty"`
}

// LogList is the GET /cards/{uid}/log response envelope.
type LogList struct {
	Kind  string     `json:"kind"`
	Items []LogEntry `json:"items"`
	// TruncatedBefore, when set, is the time the loaded history is cut at:
	// older entries exist on the remote but are not here (a shallow clone).
	TruncatedBefore string `json:"truncatedBefore,omitempty"`
}

// DayLogList is the GET /logs response: one day's feed, per card. The day
// board asks it once for every card it shows — a card's own whole history
// is the other question (GET /cards/{uid}/log).
type DayLogList struct {
	Kind string `json:"kind"`
	Day  string `json:"day"`
	// Cards maps a card's uid to its entries on that day, oldest first. A
	// card that was quiet is present with an empty list; a card the visitor
	// cannot see is absent.
	Cards map[string][]LogEntry `json:"cards"`
}

// DayLogsFrom builds the day feed's response from what the service found.
func DayLogsFrom(day string, per map[string]DayEntries) DayLogList {
	out := DayLogList{Kind: "DayLogList", Day: day, Cards: make(map[string][]LogEntry, len(per))}
	for uid, d := range per {
		items := make([]LogEntry, 0, len(d.Events)+len(d.Notes))
		for _, e := range d.Events {
			items = append(items, LogEntry{
				Type: "event", ID: e.ID, At: e.At, Actor: e.Actor,
				EventKind: e.Kind, From: e.From, To: e.To,
			})
		}
		for _, n := range d.Notes {
			items = append(items, LogEntry{
				Type: "note", ID: n.ID, At: n.CreatedAt, Actor: n.Author, Text: n.Body,
			})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].At < items[j].At })
		out.Cards[uid] = items
	}
	return out
}

// DayEntries is one card's notes and events on a day — the service's answer
// shape, kept here so the resource layer does not import the service.
type DayEntries struct {
	Notes  []board.Note
	Events []board.Event
}

// CardLogFrom merges the given events — a backend's history — with the
// card's notes into one chronological feed, naming the horizon the history
// is cut at when there is one.
func CardLogFrom(c board.Card, events []board.Event, truncatedBefore time.Time) LogList {
	items := make([]LogEntry, 0, len(events)+len(c.Notes))
	for _, e := range events {
		items = append(items, LogEntry{
			Type: "event", ID: e.ID, At: e.At, Actor: e.Actor,
			EventKind: e.Kind, From: e.From, To: e.To,
		})
	}
	for _, n := range c.Notes {
		items = append(items, LogEntry{
			Type: "note", ID: n.ID, At: n.CreatedAt, Actor: n.Author, Text: n.Body,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At < items[j].At })
	out := LogList{Kind: "LogList", Items: items}
	if !truncatedBefore.IsZero() {
		out.TruncatedBefore = truncatedBefore.UTC().Format(time.RFC3339)
	}
	return out
}

// rosterDomains names the repository of every entry whose domain is known,
// in the board's own namespace — which, on a board whose primary has a
// name, is every declared entry, the primary's included (board.TeamDomain
// and friends answer with that name, not ""). The git store always names
// its domains, so a served board normally fills these maps whatever it
// spans; nil is for a board that names nothing at all, which is what a
// hand-built one does. The client normalizes the same way (domains.ts:
// primaryDomain/inPrimary), so an entry with no row here and one naming
// the primary mean the same thing to it.
func rosterDomains(names []string, domainOf func(string) string) map[string]string {
	var out map[string]string
	for _, name := range names {
		d := domainOf(name)
		if d == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name] = d
	}
	return out
}
