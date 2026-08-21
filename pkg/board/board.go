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

// EpicStateTitle marks the hidden card that declares an epic column of the
// Project board. One card per epic: its Epic field is the epic's name, its
// Project field is the project the epic belongs to, and its position on the
// board is the column order (exactly the team-roster mechanism of sprint-state
// cards). The card exists so an empty epic — just added, or emptied out —
// still has a column.
const EpicStateTitle = "aeman:epic-state"

// ProjectStateTitle marks the hidden card that declares a project — the
// Project board's top-level grouping, one step above epics. One card per
// project: its Project field is the name and its board position is the order
// the chips appear in. Like an epic, a project exists in its own right, so an
// empty one can be created first and filled with epics afterwards.
const ProjectStateTitle = "aeman:project-state"

// DeadlineStateTitle marks the hidden card that puts a deadline on a week of
// the Project board — the line across the grid. One card per deadline: its
// Week is the week the line sits on and its Project is whose deadline it is.
// A project holds at most one deadline per week, so two of ITS OWN lines
// dragged onto the same week merge into one; two projects can of course both
// have something due that week.
const DeadlineStateTitle = "aeman:deadline-state"

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
	// Epic names the Project-board column this card belongs to ("" = none). An
	// epic card's row is its Week; StartDate..Day span the weeks its slot
	// covers when it stretches over more than one.
	Epic string `json:"epic,omitempty"`
	// Project is the project side of the card's column. A column is
	// identified by the PAIR (project, epic), because epic names are unique
	// only within a project — "Docs" or "Auth" belong to every project — so a
	// card filed under a column must name both. On a project-state card it is
	// the project's own name; on an epic-state card, the project owning it.
	Project string `json:"project,omitempty"`
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
	Epic        string   `json:"epic,omitempty"`
	Project     string   `json:"project,omitempty"`
}

// Deadline is a date marked on the Project board: the week its line sits on,
// the project it belongs to ("" = none), and the hidden card that declares it.
// A project holds at most one deadline per week.
type Deadline struct {
	Week    string `json:"week"`
	Project string `json:"project,omitempty"`
	ItemID  string `json:"itemId,omitempty"`
}

// EpicCol is one column of the Project board: its name, the project that owns
// it, and the hidden epic-state card that declares it. The (Project, Name)
// pair is the column's identity — epic names repeat across projects, so a name
// alone does not identify a column.
type EpicCol struct {
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
	ItemID  string `json:"itemId,omitempty"`
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
	// TeamOrder lists the SprintStates keys in board order — the position of
	// each team's hidden sprint-state card on the project. That position IS
	// the team order every client shares (reordering teams moves the card).
	TeamOrder []string `json:"teamOrder,omitempty"`
	// Epics lists the Project board's columns in board order (the positions
	// of the hidden epic-state cards that declare them).
	Epics []EpicCol `json:"epics,omitempty"`
	// Deadlines are the weeks carrying a deadline line, in board order.
	Deadlines []Deadline `json:"deadlines,omitempty"`
	// Projects lists the project roster in board order (the positions of the
	// hidden project-state cards) and ProjectStates maps each project to the
	// card that declares it. A project groups epics: the Project board shows
	// one project's epics as its columns.
	Projects      []string          `json:"projects,omitempty"`
	ProjectStates map[string]string `json:"projectStates,omitempty"`
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
	seen := map[string]bool{}
	winners := map[string]Card{}
	epicSeen := map[string]bool{}
	projectSeen := map[string]bool{}
	deadlineSeen := map[string]bool{}
	epicKey := func(project, name string) string { return project + "\x00" + name }
	for _, c := range cards {
		if c.Title == DeadlineStateTitle {
			// One line per week: a duplicate is a merge that already happened
			// (or a torn write) and the first position wins, as everywhere.
			if k := c.Project + "\x00" + c.Week; c.Week != "" && !deadlineSeen[k] {
				deadlineSeen[k] = true
				b.Deadlines = append(b.Deadlines,
					Deadline{Week: c.Week, Project: c.Project, ItemID: c.ItemID})
			}
			continue
		}
		if c.Title == ProjectStateTitle {
			// Same duplicate rule as epic-state: the first position wins.
			if c.Project == "" || projectSeen[c.Project] {
				continue
			}
			projectSeen[c.Project] = true
			b.Projects = append(b.Projects, c.Project)
			if b.ProjectStates == nil {
				b.ProjectStates = map[string]string{}
			}
			b.ProjectStates[c.Project] = c.ItemID
			continue
		}
		if c.Title == EpicStateTitle {
			// Same duplicate rule as sprint-state: the first position wins
			// the order, the oldest card wins the state slot.
			if c.Epic == "" {
				continue
			}
			if k := epicKey(c.Project, c.Epic); !epicSeen[k] {
				epicSeen[k] = true
				b.Epics = append(b.Epics, EpicCol{
					Name: c.Epic, Project: c.Project, ItemID: c.ItemID,
				})
			}
			continue
		}
		if c.Title == SprintStateTitle {
			// Duplicate sprint-state cards happen (a bootstrap raced a stale
			// snapshot); the winner must be DETERMINISTIC or the pointer
			// flip-flops between duplicates as board positions churn — the
			// oldest card wins, ties broken by item id.
			if prev, ok := winners[c.Team]; !ok || olderSprintState(c, prev) {
				winners[c.Team] = c
			}
			// Duplicates keep the first position they appeared at.
			if !seen[c.Team] {
				seen[c.Team] = true
				b.TeamOrder = append(b.TeamOrder, c.Team)
			}
			continue
		}
		b.Cards = append(b.Cards, c)
	}
	// A slot's row is its START date's week, never a week stored beside it.
	// The two used to be written separately, so editing a card's dates left it
	// sitting in the week it was created in — the board said one thing and the
	// card's own dates said another. Deriving here makes every reader agree at
	// once, and the stored field converges as cards are written.
	for i := range b.Cards {
		if b.Cards[i].Epic != "" && b.Cards[i].StartDate != "" {
			b.Cards[i].Week = MondayOf(b.Cards[i].StartDate)
		}
	}
	// A card can name an epic without naming a project: it predates the pair,
	// or someone set the Epic field straight in the GitHub UI. When exactly one
	// column bears that name it can only be that one, so it is adopted on READ
	// — no rewriting of anyone's data, and the next write settles the pair.
	// An ambiguous name (two projects, one column name) is left unattached
	// rather than guessed at.
	for i := range b.Cards {
		if b.Cards[i].Epic == "" || b.Cards[i].Project != "" {
			continue
		}
		match, count := "", 0
		for _, col := range b.Epics {
			if col.Name == b.Cards[i].Epic {
				match, count = col.Project, count+1
			}
		}
		if count == 1 {
			b.Cards[i].Project = match
		}
	}
	for team, c := range winners {
		b.SprintStates[team] = SprintState{
			Current:  c.SprintStart,
			Previous: c.StartDate,
			ItemID:   c.ItemID,
		}
	}
	return b
}

// olderSprintState reports whether a is the older sprint-state card: earlier
// CreatedAt, ties (and hand-built snapshots without timestamps) broken by the
// smaller item id.
func olderSprintState(a, b Card) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt < b.CreatedAt
	}
	return a.ItemID < b.ItemID
}

// EpicsOf lists a project's epic columns in board order. A column whose
// project is unset belongs to none and shows only in the all-projects view —
// it is never silently adopted.
func EpicsOf(b Board, project string) []EpicCol {
	var out []EpicCol
	for _, e := range b.Epics {
		if e.Project == project {
			out = append(out, e)
		}
	}
	return out
}

// FindEpic looks a column up by its identity — the (project, epic) pair.
func FindEpic(b Board, project, name string) (EpicCol, bool) {
	for _, e := range b.Epics {
		if e.Project == project && e.Name == name {
			return e, true
		}
	}
	return EpicCol{}, false
}

// InEpic reports whether a card is filed under a column. Both halves have to
// match: the same epic name in another project is a different column.
func InEpic(c Card, project, name string) bool {
	return c.Epic == name && c.Project == project
}

// FindDeadline looks a deadline up by its identity — the (project, week) pair.
func FindDeadline(b Board, project, week string) (Deadline, bool) {
	for _, d := range b.Deadlines {
		if d.Project == project && d.Week == week {
			return d, true
		}
	}
	return Deadline{}, false
}
