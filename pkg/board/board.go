// Package board holds aeman's pure board logic, ported from the web frontend:
// date helpers, the stage model, per-team sprint pointers, the three board views
// (Team grid, Me, Weekly plan) and the status/progress transition rules. Every
// function works on an in-memory Board snapshot with no network access, so the
// HTTP API and the MCP server can later expose the same views and actions.
package board

// SprintStateTitle marks the hidden per-team card that stores a team's sprint
// pointer (current/previous start dates). It mirrors SPRINT_STATE_TITLE in
// web/src/providers/github/githubProvider.ts; such cards never render on a board.
const SprintStateTitle = "aeman:sprint-state"

// ZoneKey is the colour zone a card belongs to, in the Ford sense. It mirrors
// the ZoneKey union in web/src/providers/types.ts ("" means no zone).
type ZoneKey string

// The four Ford zones, mirroring web/src/zones.ts. ZoneNone ("") is no zone.
const (
	ZoneNone   ZoneKey = ""
	ZoneGray   ZoneKey = "gray"
	ZoneGreen  ZoneKey = "green"
	ZoneYellow ZoneKey = "yellow"
	ZoneRed    ZoneKey = "red"
)

// PlanBand is a card's weekly-plan band. It mirrors the `plan?: "wed" | "fri"`
// field in web/src/providers/types.ts; PlanNone ("") means the card is not a
// weekly-plan card.
type PlanBand string

// The weekly-plan bands. PlanNone ("") means the card is not in the weekly plan.
const (
	PlanNone PlanBand = ""
	PlanWed  PlanBand = "wed"
	PlanFri  PlanBand = "fri"
)

// Note is a dated work note attached to a card: an issue/PR comment, or a line
// stored in a draft issue's body when the card has no comment thread. It mirrors
// the Note interface in web/src/providers/types.ts.
type Note struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	Author    string `json:"author,omitempty"`
	// Source is "comment" or "draft".
	Source string `json:"source"`
}

// SingleSelectOption is one choice of a single-select project field. It mirrors
// the SingleSelectOption interface in web/src/providers/types.ts.
type SingleSelectOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ProjectField is a column/field defined on a project board. It mirrors the
// ProjectField interface in web/src/providers/types.ts.
type ProjectField struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	DataType string               `json:"dataType"`
	Options  []SingleSelectOption `json:"options,omitempty"`
}

// Card is a single project item with the well-known field values the boards
// orient by. It mirrors the Card interface in web/src/providers/types.ts (kept
// distinct from ghprojects.Card, which has no typed Stage/Plan/Week/ReviewOf and
// stores them only in a generic map).
type Card struct {
	ItemID string `json:"itemId"`
	// ContentID is the node id of the underlying issue/PR/draft and IsDraft marks
	// a draft-issue card. They mirror the contentId/isDraft fields on the frontend
	// Card; a backend needs them to rename, reassign or note on the card (the pure
	// views never read them).
	ContentID string `json:"contentId,omitempty"`
	IsDraft   bool   `json:"isDraft,omitempty"`
	Title     string `json:"title"`
	// URL/Number/Repository/State describe the underlying issue or PR (empty on a
	// draft card). Author is the card's creator (draft-issue creator or issue
	// author). They mirror the url/number/repository/state/author fields on the
	// frontend Card.
	URL        string   `json:"url,omitempty"`
	Number     int      `json:"number,omitempty"`
	Repository string   `json:"repository,omitempty"`
	State      string   `json:"state,omitempty"`
	Assignees  []string `json:"assignees"`
	Author     string   `json:"author,omitempty"`
	// Team is the card's team label ("" = the no-team group).
	Team string  `json:"team,omitempty"`
	Zone ZoneKey `json:"zone,omitempty"`
	// ZoneOptionID is the raw single-select option id backing Zone, so a backend
	// can round-trip the value. It mirrors zoneOptionId on the frontend Card.
	ZoneOptionID string `json:"zoneOptionId,omitempty"`
	// Progress is the readiness percentage (0..100); 0 also stands for unset,
	// matching the frontend's `progress ?? 0`.
	Progress int      `json:"progress"`
	Stage    StageKey `json:"stage,omitempty"`
	// Day is the ISO date (yyyy-mm-dd) the card is planned to finish/be due on.
	Day string `json:"day,omitempty"`
	// StartDate is the day the card starts on; SprintStart is the start day of the
	// sprint it belongs to (what the day boards orient by).
	StartDate   string `json:"startDate,omitempty"`
	SprintStart string `json:"sprintStart,omitempty"`
	// SprintTitle/Status are the board's Sprint (iteration) title and Status
	// single-select value, kept for the frontend (distinct from Stage).
	SprintTitle string `json:"sprintTitle,omitempty"`
	Status      string `json:"status,omitempty"`
	// Plan/Week place the card in the founders' weekly plan (Week is a Monday).
	Plan PlanBand `json:"plan,omitempty"`
	Week string   `json:"week,omitempty"`
	// ReviewOf, on a review card, is the itemId of the original card it reviews.
	ReviewOf string `json:"reviewOf,omitempty"`
	// Parent, on a subtask, is the itemId of the card it belongs to ("" = a
	// top-level card). Subtasks are one level deep: a parent cannot itself be
	// a subtask, and a card with subtasks cannot become one.
	Parent string `json:"parent,omitempty"`
	// Recurrence is a recurrent card's reseed cycle: "" = every sprint (the
	// default), "week" / "month" = only once that interval has elapsed since
	// the sprint the card is bound to (see RecurrenceDue).
	Recurrence string `json:"recurrence,omitempty"`
	// ReviewRound counts a review card's review rounds. A card sent back for
	// another review after already passing starts round 2, 3, … The first
	// review is implicit and left at 0 (round 1 is not shown).
	ReviewRound int    `json:"reviewRound,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	// Description is the card's free-form details (a draft body minus its appended
	// action log, or an issue/PR body). Notes are the card's dated work notes.
	Description string `json:"description,omitempty"`
	Notes       []Note `json:"notes,omitempty"`
	// Events is the card's recorded activity log (stage/progress/review/plan
	// changes), stored alongside the notes — see Event.
	Events []Event `json:"events,omitempty"`
	// EventLogID is the id of the dedicated log comment holding an issue/PR
	// card's events ("" when none exists yet, or on draft cards, whose events
	// live in the body log). Internal plumbing for the event writer.
	EventLogID string `json:"-"`
}

// CreateInput is the payload for creating a card on a board: the fields a create
// can set. It mirrors NewCardInput in web/src/providers/types.ts. It lives in the
// board package (not boardservice) so a backend can implement the create method
// without importing boardservice. An empty field is left unset.
type CreateInput struct {
	Title       string   `json:"title"`
	Zone        ZoneKey  `json:"zone,omitempty"`
	Day         string   `json:"day,omitempty"`
	Start       string   `json:"start,omitempty"`
	SprintStart string   `json:"sprintStart,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	Team        string   `json:"team,omitempty"`
	ReviewOf    string   `json:"reviewOf,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Plan        PlanBand `json:"plan,omitempty"`
	Week        string   `json:"week,omitempty"`
}

// SprintState is a team's explicit sprint pointer, read from its hidden
// sprint-state card: the current and previous sprint start dates (and the card's
// item id). It mirrors the SprintState interface in web/src/providers/types.ts;
// "" means the team has no such sprint yet.
type SprintState struct {
	Current  string `json:"current"`
	Previous string `json:"previous"`
	ItemID   string `json:"itemId"`
}

// Board is an in-memory snapshot of a project board: its fields, the visible
// cards (sprint-state cards excluded) and the per-team sprint pointers. It
// mirrors the Board interface in web/src/providers/types.ts.
type Board struct {
	// ID/Number/Owner identify the project this snapshot came from, so a backend
	// can apply mutations against it. They mirror the id/number/owner fields on the
	// frontend Board and are empty on hand-built snapshots that never get persisted.
	ID     string `json:"id,omitempty"`
	Number int    `json:"number,omitempty"`
	// Title/URL identify the project for display, mirroring the frontend Board.
	Title  string         `json:"title,omitempty"`
	URL    string         `json:"url,omitempty"`
	Owner  string         `json:"owner,omitempty"`
	Fields []ProjectField `json:"fields"`
	Cards  []Card         `json:"cards"`
	// SprintStates maps each team key ("" = the no-team group) to its pointer.
	SprintStates map[string]SprintState `json:"sprintStates"`
}

// NewBoard builds a Board snapshot from a board's fields and full card list,
// splitting the hidden sprint-state cards out of Cards into SprintStates (keyed
// by team, "" = the no-team group). It mirrors mapProject's split: a sprint-state
// card's Team is its key, its SprintStart is the team's current sprint and its
// StartDate (the "Start" field) is the previous sprint.
func NewBoard(fields []ProjectField, cards []Card) Board {
	b := Board{
		Fields:       fields,
		Cards:        make([]Card, 0, len(cards)),
		SprintStates: map[string]SprintState{},
	}
	for _, c := range cards {
		if c.Title == SprintStateTitle {
			b.SprintStates[c.Team] = SprintState{
				Current:  c.SprintStart,
				Previous: c.StartDate,
				ItemID:   c.ItemID,
			}
			continue
		}
		b.Cards = append(b.Cards, c)
	}
	return b
}
