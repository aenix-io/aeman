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

// ProcessStateTitle marks the hidden card that declares a process — recurring
// work the team keeps doing and wants to see itself doing: its Process field
// is the name, its Project the plan it is part of. A process groups tasks
// the way a project groups epic columns.
const ProcessStateTitle = "aeman:process-state"

// ProcessTaskTitle marks the hidden card an iteration is copied FROM. Its
// Process names the process, its Title and Description are what the iteration
// will say, Recurrence is its cycle, StartDate the calendar anchor the cycle
// is counted from, Team the plan the iterations land in, and Assignees the
// standing owner. Unlike a recurrent card's reseed, which copies the previous
// iteration (so a rename propagates forever), every iteration comes from
// here — the live card may be renamed or described freely.
const ProcessTaskTitle = "aeman:process-task"

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

// Card is a single board item with the well-known field values the boards
// orient by. It mirrors the Card interface in web/src/providers/types.ts.
type Card struct {
	ItemID string `json:"itemId"`
	Title  string `json:"title"`
	// Assignees are logins; Author is the login that created the card.
	Assignees []string `json:"assignees"`
	Author    string   `json:"author,omitempty"`
	// Team is the card's team label ("" = the no-team group).
	Team string  `json:"team,omitempty"`
	Zone ZoneKey `json:"zone,omitempty"`
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
	// Plan/Week place the card in the founders' weekly plan (Week is a Monday).
	Plan PlanBand `json:"plan,omitempty"`
	Week string   `json:"week,omitempty"`
	// Epic names the Project-board column this card belongs to ("" = none). An
	// epic card's row is its Week; StartDate..Day span the weeks its slot
	// covers when it stretches over more than one.
	Epic string `json:"epic,omitempty"`
	// Process, on the two process state cards, is the process's name (on its
	// own card) or the process a task belongs to. On an ordinary card it
	// is empty — an iteration points at its task instead.
	Process string `json:"process,omitempty"`
	// Task, on an iteration, is the item id of the process task it
	// was spawned from ("" = not an iteration). The link is what makes the
	// process's history: its iterations are the cards that name it.
	Task string `json:"task,omitempty"`
	// Paused, on a process state card, stops it spawning: the tasks and
	// their history stay exactly as they are, and nothing new is filed until
	// it is resumed. Work that stops for a month is not work that was
	// deleted, and a process nobody can pause gets deleted instead.
	Paused bool `json:"paused,omitempty"`
	// Accumulate, on a task, spawns an iteration on every due date even
	// while the previous one is still open — unpaid months pile up as
	// separate cards. Off (the default), an open iteration simply goes
	// overdue and the next one waits for it.
	Accumulate bool `json:"accumulate,omitempty"`
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
	// Rank is the card's ordering key within its list (see RankBetween): a
	// plain string compared bytewise, so a reorder rewrites this card alone.
	Rank string `json:"rank,omitempty"`
	// DoneFrom is the progress the card had when a write took it to 100; a
	// reopen restores it. Stored, not derived from history, because an
	// action must never depend on a log that may be cut at a horizon.
	DoneFrom int `json:"doneFrom,omitempty"`
	// DoneAt is the board day (yyyy-mm-dd) a write took the card to 100,
	// cleared when it drops below again — what lets a board show a card the
	// day it was done and drop it the next without reading history.
	DoneAt string `json:"doneAt,omitempty"`
	// LeftAt is the board day (yyyy-mm-dd) the × took the card off.
	//
	// On a PERSONAL card it is a live rule: the board shows it that day and
	// before, off it from the next, and re-dating the card (calendar, defer)
	// clears it — the card is on a day again.
	//
	// On a TEAM card the × demotes into the previous sprint instead, dates
	// and all, and this records the day that happened. Nothing live reads it
	// there: today's board drops the card, which is what the × is for. It is
	// what a RECORD of that day gives back (G60) — the card was worked and
	// finished on it — and a fact about a day gone by is not undone by
	// re-dating the card, so it stands.
	LeftAt string `json:"leftAt,omitempty"`
	// Link is a URL the card points at — the only trace an issue-backed
	// card keeps of its issue. Nothing is fetched through it.
	Link string `json:"link,omitempty"`
	// GitHubID is the Projects v2 item id a migrated card came from, kept so
	// the id can still be resolved for a while; empty on cards born later.
	GitHubID string `json:"githubId,omitempty"`
	// Mirrors are additional Project-board columns the card stands in — the
	// same card, one file and one log, shown in more than one project. The
	// (Project, Epic) pair above stays its home: DomainOf reads it, mirrors
	// never move the card. See mirrors.go.
	Mirrors []Placement `json:"mirrors,omitempty"`
	// Domain names the repository the card lives in ("" = the primary or a
	// single-domain board). MovedFrom/MovedAt record a cross-domain move,
	// so a torn move resolves from the tree alone.
	Domain    string `json:"domain,omitempty"`
	MovedFrom string `json:"movedFrom,omitempty"`
	MovedAt   string `json:"movedAt,omitempty"`
	// Description is the card's free-form details (a draft body minus its appended
	// action log, or an issue/PR body). Notes are the card's dated work notes.
	Description string `json:"description,omitempty"`
	Notes       []Note `json:"notes,omitempty"`
}

// CreateInput is the payload for creating a card on a board: the fields a create
// can set. It mirrors NewCardInput in web/src/providers/types.ts. It lives in the
// board package (not boardservice) so a backend can implement the create method
// without importing boardservice. An empty field is left unset.
type CreateInput struct {
	// ItemID, when set, is the id the card is created WITH — a backend that
	// mints its own ids (the git store) honours it, so the cache can hand out
	// the final id before the write lands; a backend that cannot ignores it.
	ItemID string `json:"itemId,omitempty"`
	// Domain, on a roster stub (a team, project or process state card), is
	// the repository to declare it in; "" is the primary. Cards never carry
	// one — their domain is inherited (see DomainOf).
	Domain string `json:"domain,omitempty"`
	// Personal files the card in the caller's personal domain (Domain names
	// it) instead of where the home rule would put it; such a card stays
	// there whatever team or project it is later given.
	Personal    bool     `json:"personal,omitempty"`
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
	Process     string   `json:"process,omitempty"`
	Task        string   `json:"task,omitempty"`
	Recurrence  string   `json:"recurrence,omitempty"`
	Paused      bool     `json:"paused,omitempty"`
	// Body is the draft's description, written with the create rather than
	// after it: a card that appears without its text, then fills in a second
	// later, reads as a card the board got wrong.
	Body string `json:"body,omitempty"`
}

// Deadline is a date marked on the Project board: the week its line sits on,
// the project it belongs to ("" = none), and the hidden card that declares it.
// A project holds at most one deadline per week.
type Deadline struct {
	Week    string `json:"week"`
	Project string `json:"project,omitempty"`
	ItemID  string `json:"itemId,omitempty"`
}

// Process is recurring work the team keeps doing: its name, the project it
// is part of, and the hidden card that declares it.
type Process struct {
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
	Paused  bool   `json:"paused,omitempty"`
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
	// Board names the board this snapshot came from — the name of its primary
	// repository — so a backend can apply mutations against it. Empty on
	// hand-built snapshots that never get persisted.
	Board string `json:"board,omitempty"`
	// Title/URL identify the board for display, mirroring the frontend Board.
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
	Cards []Card `json:"cards"`
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
	// Processes lists the board's processes in board order, and Tasks the
	// cards iterations are copied from (also in board order). Both are split
	// out of Cards, like every other state card.
	Processes []Process `json:"processes,omitempty"`
	Tasks     []Card    `json:"tasks,omitempty"`
	// Projects lists the project roster in board order (the positions of the
	// hidden project-state cards) and ProjectStates maps each project to the
	// card that declares it. A project groups epics: the Project board shows
	// one project's epics as its columns.
	Projects      []string          `json:"projects,omitempty"`
	ProjectStates map[string]string `json:"projectStates,omitempty"`
	// Primary names the repository a board's own entries belong to — the
	// first of its domains. The store STAMPS every roster entry with its
	// domain's name, the primary included, while a card that nothing
	// places resolves to "" ("no repository decides this"); the two are
	// different namespaces, and comparing them directly answered "another
	// repository" for every card of the primary that nothing placed.
	// Boards built by hand (fixtures, older callers) leave it empty, which
	// is the same thing said the other way.
	Primary string `json:"primary,omitempty"`
	// Domains maps a roster entry's id — a team's sprint-state card, a
	// project, an epic column, a deadline, a process, a task — to the domain
	// (repository) it was declared in; cards carry their own Domain. Nil on
	// a single-domain board, where nothing records one.
	Domains map[string]string `json:"domains,omitempty"`
}

// NewBoard builds a Board snapshot from the full card list, splitting the hidden sprint-state cards out of Cards into SprintStates (keyed
// by team, "" = the no-team group). It mirrors mapProject's split: a sprint-state
// card's Team is its key, its SprintStart is the team's current sprint and its
// StartDate (the "Start" field) is the previous sprint.
func NewBoard(cards []Card) Board { return NewBoardIn("", cards) }

// NewBoardIn is NewBoard for a board whose entries are STAMPED with their
// domain's name — which is every board the store hands over, the primary
// included. The assembly's own rules ask "the same repository?" while it
// runs (declaredMirrors), so the primary's name has to arrive with the
// cards rather than be set on the result afterwards.
func NewBoardIn(primary string, cards []Card) Board {
	b := Board{Primary: primary,
		Cards:        make([]Card, 0, len(cards)),
		SprintStates: map[string]SprintState{},
	}
	seen := map[string]bool{}
	winners := map[string]Card{}
	epicSeen := map[string]bool{}
	projectSeen := map[string]bool{}
	deadlineSeen := map[string]bool{}
	processSeen := map[string]bool{}
	epicKey := func(project, name string) string { return project + "\x00" + name }
	for _, c := range cards {
		if c.Domain != "" && IsStateTitle(c.Title) {
			if b.Domains == nil {
				b.Domains = map[string]string{}
			}
			b.Domains[c.ItemID] = c.Domain
		}
		if c.Title == ProcessStateTitle {
			if c.Process != "" && !processSeen[c.Process] {
				processSeen[c.Process] = true
				b.Processes = append(b.Processes, Process{
					Name: c.Process, Project: c.Project, Paused: c.Paused, ItemID: c.ItemID,
				})
			}
			continue
		}
		if c.Title == ProcessTaskTitle {
			// A task is a whole card — title, description, cycle, owner —
			// so it is kept as one, just out of the board's card rows.
			b.Tasks = append(b.Tasks, c)
			continue
		}
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

	// The decoder drops what one file can prove wrong (half pairs,
	// duplicates, home twins, subtask mirrors); a mirror into another
	// repository or onto a column nobody declared needs the ROSTER, so it
	// is dropped here — a writer producing one is silently corrected, not
	// honoured (G15), and the x's promotion never inherits a home the
	// service would have refused to mirror to.
	for i := range b.Cards {
		b.Cards[i].Mirrors = declaredMirrors(b, b.Cards[i])
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
	if c.Epic == name && c.Project == project {
		return true
	}
	// A mirrored card stands in every one of its columns alike; only the
	// home pair decides anything beyond being shown (DomainOf, promotion).
	return Mirrored(c, project, name)
}

// InProject reports whether a card stands in one project's columns — its
// home pair, or ANY mirror it carries. A mirror is the same card standing
// in a second column (G15), so a project's listing that read only the home
// pair answered with less than the board it draws.
func InProject(c Card, project string) bool {
	if c.Project == project {
		return true
	}
	for _, m := range c.Mirrors {
		if m.Project == project {
			return true
		}
	}
	return false
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

// FindProcess looks a process up by name.
func FindProcess(b Board, name string) (Process, bool) {
	for _, p := range b.Processes {
		if p.Name == name {
			return p, true
		}
	}
	return Process{}, false
}

// TasksOf lists a process's tasks in board order.
func TasksOf(b Board, process string) []Card {
	var out []Card
	for _, t := range b.Tasks {
		if t.Process == process {
			out = append(out, t)
		}
	}
	return out
}

// Iterations lists the cards spawned from a task, in board order.
func Iterations(b Board, taskID string) []Card {
	var out []Card
	for _, c := range b.Cards {
		if c.Task == taskID {
			out = append(out, c)
		}
	}
	return out
}

// declaredMirrors keeps the mirror entries the service itself would have
// admitted: the column exists on the roster and belongs to the repository
// that holds this card. It asks the COLUMN, exactly as Mirror does — a
// project name may be declared in two repositories with its columns merged
// (G13), and the no-project bucket has no project to ask at all, so the
// project-based answer dropped placements the service had just written.
// Everything else is a hand edit the decoder could not judge without the
// roster.
func declaredMirrors(b Board, c Card) []Placement {
	if len(c.Mirrors) == 0 {
		return c.Mirrors
	}
	// A fresh slice on purpose: filtering into c.Mirrors[:0] would write
	// through to the caller's backing array, and NewBoard does not own
	// the cards it is handed.
	// Which repository holds this CARD — the question Mirror asks, and the
	// only one that is the same on both sides. The home COLUMN is a
	// different question wherever the two disagree (an older write, an
	// outside writer), and answering it here kept mirrors Mirror refuses.
	mine := HomeDomain(b, c)
	kept := make([]Placement, 0, len(c.Mirrors))
	for _, m := range c.Mirrors {
		if d, ok := ColumnDomain(b, m.Project, m.Epic); !ok || d != mine {
			continue
		}
		kept = append(kept, m)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
