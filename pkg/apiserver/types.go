// Package apiserver is the Kubernetes-style resource layer of the aeman API:
// it translates the internal board domain into Card/Sprint/Note/Ordering
// resources (metadata/spec/status), evaluates LIST selectors for the Team, Me
// and Weekly views, and derives the fields the UI would otherwise compute.
// It is backend-agnostic: everything works on the domain board snapshot.
package apiserver

import (
	"sort"

	"github.com/aenix-org/aeman/pkg/board"
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
	UID        string `json:"uid"`
	ContentID  string `json:"contentId,omitempty"`
	IsDraft    bool   `json:"isDraft,omitempty"`
	URL        string `json:"url,omitempty"`
	Number     int    `json:"number,omitempty"`
	Repository string `json:"repository,omitempty"`
	Author     string `json:"author,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

// CardDates is the card's date model: its scheduled start, the end of its
// visible range, and the sprint it belongs to (see docs/dates.md).
type CardDates struct {
	Start  string `json:"start,omitempty"`
	End    string `json:"end,omitempty"`
	Sprint string `json:"sprint,omitempty"`
}

// CardPlan places the card in the founders' weekly plan.
type CardPlan struct {
	Band string `json:"band"`
	Week string `json:"week,omitempty"`
}

// CardSpec is the user's intent — everything an edit can change.
type CardSpec struct {
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Team        string    `json:"team,omitempty"`
	Zone        string    `json:"zone,omitempty"`
	Assignees   []string  `json:"assignees"`
	Progress    int       `json:"progress"`
	Stage       string    `json:"stage,omitempty"`
	Dates       CardDates `json:"dates"`
	Plan        *CardPlan `json:"plan,omitempty"`
	ReviewOf    string    `json:"reviewOf,omitempty"`
}

// CardStatus is derived by the server, never written by clients.
type CardStatus struct {
	Complete    bool   `json:"complete"`
	InProgress  bool   `json:"inProgress"`
	ReviewedBy  string `json:"reviewedBy,omitempty"`
	ReviewRound int    `json:"reviewRound,omitempty"`
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
	// Members is every distinct assignee on the board — the people roster for
	// pickers (assign, review, view-as) now that clients load one view at a
	// time and cannot derive it from the cards they hold.
	Members []string `json:"members"`
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
func CardResource(b board.Board, c board.Card) Card {
	spec := CardSpec{
		Title:       c.Title,
		Description: c.Description,
		Team:        c.Team,
		Zone:        SemanticZone(c.Zone),
		Assignees:   append([]string{}, c.Assignees...),
		Progress:    c.Progress,
		Stage:       string(c.Stage),
		Dates:       CardDates{Start: c.StartDate, End: c.Day, Sprint: c.SprintStart},
		ReviewOf:    c.ReviewOf,
	}
	if c.Plan != board.PlanNone {
		spec.Plan = &CardPlan{Band: string(c.Plan), Week: c.Week}
	}
	status := CardStatus{
		Complete:    board.Complete(c.Stage, c.Progress),
		InProgress:  board.IsInProgress(c),
		ReviewRound: c.ReviewRound,
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
			UID:        c.ItemID,
			ContentID:  c.ContentID,
			IsDraft:    c.IsDraft,
			URL:        c.URL,
			Number:     c.Number,
			Repository: c.Repository,
			Author:     c.Author,
			CreatedAt:  c.CreatedAt,
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

// BoardResource is the read-only board identity resource.
func BoardResource(b board.Board) BoardInfo {
	teams := make([]string, 0, len(b.SprintStates))
	for t := range b.SprintStates {
		teams = append(teams, t)
	}
	sortStrings(teams)
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
	return BoardInfo{
		Kind:     "Board",
		Metadata: BoardMetadata{Title: b.Title, URL: b.URL, Teams: teams, Members: members},
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
}

// CardLog merges a card's events and notes into one chronological feed.
func CardLog(c board.Card) LogList {
	items := make([]LogEntry, 0, len(c.Events)+len(c.Notes))
	for _, e := range c.Events {
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
	return LogList{Kind: "LogList", Items: items}
}
