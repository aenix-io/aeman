// Package ghprojects is a Go-native client for GitHub Projects v2. It ports the
// GraphQL queries and field mapping that previously lived only in the aeman
// frontend, so the same logic can back both the HTTP API and the MCP server.
package ghprojects

// ZoneKey is the colour zone a card belongs to, in the Ford sense.
type ZoneKey string

// The four Ford zones. They mirror the single-select option colours on the
// board's zone field (see [zoneFromColor]).
const (
	ZoneGray   ZoneKey = "gray"
	ZoneGreen  ZoneKey = "green"
	ZoneYellow ZoneKey = "yellow"
	ZoneRed    ZoneKey = "red"
)

// SingleSelectOption is one choice of a single-select project field.
type SingleSelectOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ProjectField is a column/field defined on a project board.
type ProjectField struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	DataType string               `json:"dataType"`
	Options  []SingleSelectOption `json:"options,omitempty"`
}

// BoardSummary is the lightweight description returned when listing projects.
type BoardSummary struct {
	ID               string `json:"id"`
	Number           int    `json:"number"`
	Title            string `json:"title"`
	URL              string `json:"url"`
	ShortDescription string `json:"shortDescription,omitempty"`
}

// Note is a dated work note attached to a card: an issue/PR comment, or a line
// stored in a draft issue's body when the card has no comment thread.
type Note struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	Author    string `json:"author,omitempty"`
	// Source is "comment" or "draft".
	Source string `json:"source"`
}

// Card is a single project item (issue, pull request or draft issue) together
// with the well-known field values aeman understands.
type Card struct {
	ItemID string `json:"itemId"`
	// ContentID is the node id of the underlying issue/PR/draft, used for
	// comments and assignee mutations.
	ContentID    string   `json:"contentId,omitempty"`
	Title        string   `json:"title"`
	IsDraft      bool     `json:"isDraft"`
	URL          string   `json:"url,omitempty"`
	Number       int      `json:"number,omitempty"`
	Repository   string   `json:"repository,omitempty"`
	State        string   `json:"state,omitempty"`
	Assignees    []string `json:"assignees"`
	Zone         ZoneKey  `json:"zone,omitempty"`
	ZoneOptionID string   `json:"zoneOptionId,omitempty"`
	// Progress is the readiness percentage (0..100) when the board has such a
	// field; nil means unset.
	Progress *float64 `json:"progress,omitempty"`
	Day      string   `json:"day,omitempty"`
	// StartDate is the day the card starts on; SprintStart is the start day of
	// the sprint it belongs to (what the boards orient by).
	StartDate   string `json:"startDate,omitempty"`
	SprintStart string `json:"sprintStart,omitempty"`
	SprintTitle string `json:"sprintTitle,omitempty"`
	Status      string `json:"status,omitempty"`
	Team        string `json:"team,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Notes       []Note `json:"notes"`
	// Fields holds every field value keyed by its (verbatim) field name, so
	// callers can read board-specific fields aeman has no dedicated role for.
	Fields map[string]string `json:"fields,omitempty"`
}

// Board is a fully-loaded project board with its fields and cards.
type Board struct {
	ID     string         `json:"id"`
	Number int            `json:"number"`
	Title  string         `json:"title"`
	URL    string         `json:"url"`
	Owner  string         `json:"owner"`
	Fields []ProjectField `json:"fields"`
	Cards  []Card         `json:"cards"`
}

// FieldRoles maps well-known roles onto the board's actual fields by name.
type FieldRoles struct {
	Zone        *ProjectField
	Progress    *ProjectField
	Day         *ProjectField
	Start       *ProjectField
	SprintStart *ProjectField
	Sprint      *ProjectField
	Status      *ProjectField
	Team        *ProjectField
}

// CreateCardInput describes a draft-issue card to create on a board. Only Title
// is required; the remaining fields are applied when the board has a matching
// role.
type CreateCardInput struct {
	Title       string            `json:"title"`
	Zone        ZoneKey           `json:"zone,omitempty"`
	Assignee    string            `json:"assignee,omitempty"`
	Day         string            `json:"day,omitempty"`
	Start       string            `json:"start,omitempty"`
	SprintStart string            `json:"sprintStart,omitempty"`
	Status      string            `json:"status,omitempty"`
	Team        string            `json:"team,omitempty"`
	Progress    *float64          `json:"progress,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
	// StartNewSprint controls how the card joins a sprint when it has a team (or
	// the flag is set explicitly): nil = auto (join the team's running sprint, or
	// start a new one today when it is all done / absent), true = force a new
	// sprint today, false = force-join the current sprint (today if none). When
	// engaged it sets startDate = sprintStart and defaults day to today.
	StartNewSprint *bool `json:"startNewSprint,omitempty"`
}

// UpdateCardInput describes a partial update to a card. A nil pointer leaves the
// corresponding field untouched. For Zone, Day, Assignee, Status and Team an
// empty string clears the value.
type UpdateCardInput struct {
	Title       *string           `json:"title,omitempty"`
	Zone        *ZoneKey          `json:"zone,omitempty"`
	Progress    *float64          `json:"progress,omitempty"`
	Day         *string           `json:"day,omitempty"`
	Start       *string           `json:"start,omitempty"`
	SprintStart *string           `json:"sprintStart,omitempty"`
	Assignee    *string           `json:"assignee,omitempty"`
	Status      *string           `json:"status,omitempty"`
	Team        *string           `json:"team,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
}

// cardByItemID returns a pointer to the card with the given item id, or nil.
func (b *Board) cardByItemID(itemID string) *Card {
	for i := range b.Cards {
		if b.Cards[i].ItemID == itemID {
			return &b.Cards[i]
		}
	}
	return nil
}

// fieldByName returns the field whose name matches (case-insensitively), or nil.
func (b *Board) fieldByName(name string) *ProjectField {
	for i := range b.Fields {
		if equalFold(b.Fields[i].Name, name) {
			return &b.Fields[i]
		}
	}
	return nil
}

// stageField returns the board's "stage" field (the locked/review/done status),
// matched by name the way the frontend does, or nil. It is distinct from the
// default GitHub "Status" field.
func (b *Board) stageField() *ProjectField {
	for i := range b.Fields {
		if equalFold(b.Fields[i].Name, "stage") {
			return &b.Fields[i]
		}
	}
	return nil
}

// cardDone reports whether the card sits in the "done" stage. It reads the stage
// field value off the card's verbatim Fields map, mirroring the frontend's
// stage === "done" check.
func (b *Board) cardDone(card *Card) bool {
	f := b.stageField()
	if f == nil {
		return false
	}
	return equalFold(card.Fields[f.Name], "done")
}

// currentSprint returns the team's current sprint start (the latest SprintStart
// on or before today) and whether it is still running (any card of that sprint
// is not done). It mirrors web/src/sprint.ts. An empty cur means the team has no
// sprint on or before today.
func (b *Board) currentSprint(team, today string) (cur string, running bool) {
	for i := range b.Cards {
		c := &b.Cards[i]
		if c.Team != team || c.SprintStart == "" || c.SprintStart > today {
			continue
		}
		if c.SprintStart > cur {
			cur = c.SprintStart
		}
	}
	if cur == "" {
		return "", false
	}
	for i := range b.Cards {
		c := &b.Cards[i]
		if c.Team == team && c.SprintStart == cur && !b.cardDone(c) {
			return cur, true
		}
	}
	return cur, false
}

// sprintStartForNew resolves the sprint a new card should belong to, honouring
// the startNewSprint preference (nil = auto). It mirrors the Team board's create
// rule: auto joins the running sprint or starts a new one today; true forces a
// new sprint today; false force-joins the current sprint (today if none).
func (b *Board) sprintStartForNew(team string, startNewSprint *bool, today string) string {
	cur, running := b.currentSprint(team, today)
	switch {
	case startNewSprint != nil && *startNewSprint:
		return today
	case startNewSprint != nil && !*startNewSprint:
		if cur != "" {
			return cur
		}
		return today
	default:
		if running {
			return cur
		}
		return today
	}
}
