// Package apiserver is the Kubernetes-style resource layer of the aeman API:
// it translates the internal board domain into Card/Sprint/Note/Ordering
// resources (metadata/spec/status), evaluates LIST selectors for the Team, Me
// and Weekly views, and derives the fields the UI would otherwise compute.
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

// CardPlan places the card in the founders' weekly plan — or, for an epic
// card, anchors its week row (Band empty: the card is on the Plan board, not
// in a team's wed/fri bands).
type CardPlan struct {
	Band string `json:"band,omitempty"`
	Week string `json:"week,omitempty"`
}

// CardSpec is the user's intent — everything an edit can change.
type CardSpec struct {
	Title string `json:"title"`
	// Description is nil in a summary listing ("body not included" — the
	// row shape) and always present, possibly empty, on a full resource.
	// The distinction is the client's "loaded" marker: only nil means "go
	// fetch the body", so an empty body never reads as a missing one.
	Description *string  `json:"description,omitempty"`
	Team        string   `json:"team,omitempty"`
	Zone        string   `json:"zone,omitempty"`
	Assignees   []string `json:"assignees"`
	Progress    int      `json:"progress"`
	Stage       string   `json:"stage,omitempty"`
	// Recurrence, on a recurrent card, is its reseed cycle: "" = every
	// sprint, "week" / "month" = once the interval has elapsed.
	Recurrence string    `json:"recurrence,omitempty"`
	Dates      CardDates `json:"dates"`
	Plan       *CardPlan `json:"plan,omitempty"`
	// Epic and Project are the column the card is filed under ("" = none) —
	// the pair, since epic names repeat across projects. The card's Week is
	// the row, and Dates span the weeks its slot stretches over.
	Epic    string `json:"epic,omitempty"`
	Project string `json:"project,omitempty"`
	// Mirrors are the additional columns the same card stands in — one
	// file, one log, one set of dates, shown in every listed project too.
	// Always the card's own repository (the service admits nothing else).
	Mirrors []board.Placement `json:"mirrors,omitempty"`
	// Process and Task, on a process turn, name the process this card is a
	// turn of and the task it was copied from. A card on the Me or Team
	// board carries them so a person can see what it belongs to without
	// going looking for it.
	Process  string `json:"process,omitempty"`
	Task     string `json:"task,omitempty"`
	ReviewOf string `json:"reviewOf,omitempty"`
	// Parent, on a subtask, is the uid of the card it is grouped under.
	Parent string `json:"parent,omitempty"`
}

// CardStatus is derived by the server, never written by clients.
type CardStatus struct {
	Complete   bool `json:"complete"`
	InProgress bool `json:"inProgress"`
	// Overdue: a card that came from a plan — a slot, a process turn, a
	// weekly-plan card — still open past the day it was owed by. Derived on
	// read from the card's own dates (board.Overdue); never stored.
	Overdue     bool   `json:"overdue,omitempty"`
	ReviewedBy  string `json:"reviewedBy,omitempty"`
	ReviewRound int    `json:"reviewRound,omitempty"`
	// Domain is the repository the card lives in — the store's decision by
	// the inheritance rule, never a client's choice; empty when the store did
	// not stamp it (a card in the primary is shown without a badge).
	Domain string `json:"domain,omitempty"`
	// DoneAt is the board day the card reached 100 (cleared on reopen) — the
	// personal board shows a done card that day and drops it the next.
	DoneAt string `json:"doneAt,omitempty"`
	// LeftAt is the board day a personal card was left behind on by the ×
	// (remove): the personal board shows it that day and before, not after.
	LeftAt string `json:"leftAt,omitempty"`
	// Links are the references extracted from the card's description —
	// unresolved (no titles or states; GET /cards/{uid}/links resolves those).
	// They ride the status so a summary listing, which omits the description
	// itself, still tells a row it has links to show.
	Links []CardLinkRef `json:"links,omitempty"`
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

type BoardMetadata struct {
	Title string   `json:"title,omitempty"`
	URL   string   `json:"url,omitempty"`
	Teams []string `json:"teams"`
	// Projects lists the Project board's projects, in board order — the top
	// grouping: a project owns epic columns. (Not the GitHub board, which the
	// caller addresses with owner+board.)
	Projects []string `json:"projects,omitempty"`
	// Processes lists the board's processes in board order, each naming its
	// project. The Process tab reads the full structure from /processes; this
	// is the roster, so a client can tell when it changed.
	Processes []ProcessRef `json:"processes,omitempty"`
	// Deadlines are the deadline lines in board order: the week each one sits
	// on and the project it belongs to. A project holds at most one per week.
	Deadlines []DeadlineRef `json:"deadlines,omitempty"`
	// Epics lists the Project board's columns in board order, each naming the
	// project that owns it. An epic with an empty project belongs to none and
	// shows only in the all-projects view.
	Epics []EpicRef `json:"epics,omitempty"`
	// Members is every distinct assignee on the board — the people roster for
	// pickers (assign, review, view-as) now that clients load one view at a
	// time and cannot derive it from the cards they hold — with the avatar
	// the server's forge adapter resolves for each, so a client never
	// assembles a forge URL of its own.
	Members []Member `json:"members"`
	// Domains are the repositories the visitor can read, primary first: which
	// they may write — a team, project or process is declared in one they
	// pick when more than one is writable — and which members can read each,
	// so a reviewer picker offers only people who will see the card. Absent
	// on a single-domain board.
	Domains []DomainInfo `json:"domains,omitempty"`
	// TeamDomains and ProjectDomains name the repository a team or a project
	// was declared in, for the entries that live OUTSIDE the primary — the
	// primary is the default and needs no entry, and a single-repository
	// board names none at all. A client needs them to keep a card's team and
	// project in one repository: the pair cannot both be honoured, so the
	// pickers must not offer it (boardservice.ErrDomainConflict).
	TeamDomains    map[string]string `json:"teamDomains,omitempty"`
	ProjectDomains map[string]string `json:"projectDomains,omitempty"`
	// Personal is the visitor's own repository when they linked one: the
	// personal board lives there (view=personal, create with personal=true).
	Personal *PersonalInfo `json:"personal,omitempty"`
}

// Member is one person on the board: a login and, when the server knows the
// forge, an avatar image URL and — on a forge that has them (GitLab) — a
// display name. The login is the identity everywhere; the name is for eyes.
type Member struct {
	Login     string `json:"login"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// DomainInfo is one readable domain of the visitor's board.
type DomainInfo struct {
	Name     string   `json:"name"`
	Writable bool     `json:"writable"`
	Members  []string `json:"members"`
	// Personal marks the visitor's own repository, attached for them alone.
	Personal bool `json:"personal,omitempty"`
}

// PersonalInfo is the visitor's personal repository as the board knows it:
// the domain it is served as and the repository it is.
type PersonalInfo struct {
	Domain string `json:"domain"`
	URL    string `json:"url"`
	// Problem says why the linked repository is not attached — the server
	// cannot reach it — and ActionURL is what fixes it (installing the
	// board's GitHub App on the repository). Both empty when the board is
	// attached and well.
	Problem   string `json:"problem,omitempty"`
	ActionURL string `json:"actionUrl,omitempty"`
}

// EpicRef is one Project-board column: its name and the project that owns it.
// The pair travels together because a column is meaningless without knowing
// which project's grid it belongs in.
type EpicRef struct {
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
}

// ProcessRef is one process: its name and project.
type ProcessRef struct {
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
}

// DeadlineRef is one deadline line: its week and the project it belongs to.
type DeadlineRef struct {
	Week    string `json:"week"`
	Project string `json:"project,omitempty"`
}

// processRefs lists the processes, in board order.
func processRefs(b board.Board) []ProcessRef {
	out := make([]ProcessRef, 0, len(b.Processes))
	for _, p := range b.Processes {
		out = append(out, ProcessRef{Name: p.Name, Project: p.Project})
	}
	return out
}

// processOf names the process a card is a turn of, by way of the task it was
// copied from. Empty for every card that is not a turn.
func processOf(b board.Board, c board.Card) string {
	if c.Task == "" {
		return ""
	}
	for _, t := range b.Tasks {
		if t.ItemID == c.Task {
			return t.Process
		}
	}
	return ""
}

// deadlineWeeks lists the deadline lines, in board order.
func deadlineWeeks(b board.Board) []DeadlineRef {
	out := make([]DeadlineRef, 0, len(b.Deadlines))
	for _, d := range b.Deadlines {
		out = append(out, DeadlineRef{Week: d.Week, Project: d.Project})
	}
	return out
}

// epicRefs projects the column roster onto the wire, preserving board order.
func epicRefs(b board.Board) []EpicRef {
	out := make([]EpicRef, 0, len(b.Epics))
	for _, e := range b.Epics {
		out = append(out, EpicRef{Name: e.Name, Project: e.Project})
	}
	return out
}

// CardList is the LIST response envelope; Weekly carries the view's computed
// extras when the weekly view was selected.
type CardList struct {
	Kind   string         `json:"kind"`
	Items  []Card         `json:"items"`
	Weekly *WeeklySummary `json:"weekly,omitempty"`
}

// WeeklySummary is the weekly view's computed plan progress (recurrent cards
// excluded — they restart every week and would skew the bar).
type WeeklySummary struct {
	Progress int `json:"progress"`
}

// CardResource maps a domain card onto the API resource, deriving status from
// the board (reviewedBy needs the linked review card's assignee).
// CardSummaryResource is CardResource without the card body: the shape of a
// board row. The description — often the bulk of a card's bytes, and unused
// by row rendering — stays behind for GET /cards/{uid}; the derived link refs
// in status keep the row's links indicator honest without it.
func CardSummaryResource(b board.Board, c board.Card) Card {
	res := CardResource(b, c)
	res.Spec.Description = nil
	return res
}

func CardResource(b board.Board, c board.Card) Card {
	description := c.Description
	spec := CardSpec{
		Title:       c.Title,
		Description: &description,
		Team:        c.Team,
		Zone:        SemanticZone(c.Zone),
		Assignees:   append([]string{}, c.Assignees...),
		Progress:    c.Progress,
		Stage:       string(c.Stage),
		Recurrence:  c.Recurrence,
		Dates:       CardDates{Start: c.StartDate, End: c.Day, Sprint: c.SprintStart},
		Epic:        c.Epic,
		Project:     c.Project,
		Mirrors:     append([]board.Placement{}, c.Mirrors...),
		Process:     processOf(b, c),
		Task:        c.Task,
		ReviewOf:    c.ReviewOf,
		Parent:      c.Parent,
	}
	if c.Plan != board.PlanNone || c.Week != "" {
		spec.Plan = &CardPlan{Band: string(c.Plan), Week: c.Week}
	}
	status := CardStatus{
		Complete:    board.Complete(c.Stage, c.Progress),
		InProgress:  board.IsInProgress(c),
		Overdue:     board.Overdue(c, board.TodayIso()),
		ReviewRound: c.ReviewRound,
		Domain:      c.Domain,
		DoneAt:      c.DoneAt,
		LeftAt:      c.LeftAt,
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
	for _, r := range b.Cards {
		if r.ReviewOf == c.ItemID && len(r.Assignees) > 0 &&
			!board.Complete(r.Stage, r.Progress) {
			status.ReviewedBy = r.Assignees[0]
			break
		}
	}
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
		st := b.SprintStates[t]
		out = append(out, Sprint{
			Kind:     "Sprint",
			Metadata: SprintMetadata{Team: t},
			Spec:     SprintSpec{Current: st.Current, Previous: st.Previous},
		})
	}
	return out
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
	seen := map[string]bool{}
	members := []string{}
	for _, c := range b.Cards {
		for _, a := range c.Assignees {
			if a != "" && !seen[a] {
				seen[a] = true
				members = append(members, a)
			}
		}
	}
	sortStrings(members)
	people := make([]Member, 0, len(members))
	for _, login := range members {
		m := Member{Login: login}
		if person != nil {
			p := person(login)
			m.Name, m.AvatarURL = p.Name, p.AvatarURL
		}
		people = append(people, m)
	}
	return BoardInfo{
		Kind: "Board",
		Metadata: BoardMetadata{Title: b.Title, URL: b.URL, Teams: teams,
			Projects: append([]string{}, b.Projects...), Epics: epicRefs(b),
			Deadlines: deadlineWeeks(b), Processes: processRefs(b), Members: people,
			TeamDomains: rosterDomains(teams, func(name string) string { return board.TeamDomain(b, name) }),
			ProjectDomains: rosterDomains(b.Projects, func(name string) string {
				return board.ProjectDomain(b, name)
			})},
	}
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

// rosterDomains names the repository of the entries that live outside the
// primary; nil when they all live in it.
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
