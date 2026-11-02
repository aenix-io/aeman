// Package board holds aeman's pure board logic, ported from the web frontend:
// date helpers, the stage model, per-team sprint pointers, the board views
// (Team grid, Me, Triage) and the status/progress transition rules. Every
// function works on an in-memory Board snapshot with no network access, so the
// HTTP API and the MCP server can later expose the same views and actions.
package board

// SprintStateTitle marks the hidden per-team card that stores a team's sprint
// pointer (current/previous start dates). It mirrors SPRINT_STATE_TITLE in
// web/src/providers/github/githubProvider.ts; such cards never render on a board.
const SprintStateTitle = "aeman:sprint-state"

// ScopeStateTitle marks the hidden card that declares an scope column of the
// Project board. One card per scope: its Scope field is the scope's name, its
// Project field is the project the scope belongs to, and its position on the
// board is the column order (exactly the team-roster mechanism of sprint-state
// cards). The card exists so an empty scope — just added, or emptied out —
// still has a column.
const ScopeStateTitle = "aeman:scope-state"

// ProjectStateTitle marks the hidden card that declares a project — the
// Project board's top-level grouping, one step above scopes. One card per
// project: its Project field is the name and its board position is the order
// the chips appear in. Like an scope, a project exists in its own right, so an
// empty one can be created first and filled with scopes afterwards.
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

// Capacity is a team's throughput: the POINTS a week it gets through, which
// is what a week of its plan is weighed against. Never derived — 0 means
// nobody has said (PointsAWeekOf).
//
// It carried three more numbers once: a limit in CARDS a week, derived from
// what the team had been finishing, and the shares of it that client and
// internal work might take. Nothing ever wrote them — not one team file on
// the production board had the block — and no client read them back: the bar
// they were drawn for is long gone from the boards. A derived number nobody
// looks at is a rule that can only rot.
type Capacity struct {
	Points int `json:"points,omitempty"`
}

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
	// Type says what the card is: "" is a TASK, the day boards' own kind;
	// "goal", "initiative" and "epic" are the PLANNING cards above it, drawn
	// on the Roadmap and on no day board (docs/design/hierarchy.md). Fixed
	// at create; a card never changes level.
	Type string `json:"type,omitempty"`
	// Assignees are logins; Author is the login that created the card.
	Assignees []string `json:"assignees"`
	Author    string   `json:"author,omitempty"`
	// Team is the card's team label ("" = the no-team group).
	Team string  `json:"team,omitempty"`
	Zone ZoneKey `json:"zone,omitempty"`
	// Size is what somebody said the card weighs — S, M, L or XL — set on a
	// daily sync or by a sizing tool; "" is unsized. The points a board sums
	// are derived from it (Points, PointsOf), never stored.
	Size SizeKey `json:"size,omitempty"`
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
	// Week is the week the card is scheduled for, a Monday: the row it stands
	// in on the Triage board. A Week AHEAD of the current one is a backlog
	// placement — the card is on no day board until that Monday (B1).
	Week string `json:"week,omitempty"`
	// Parked says the card is on its team's SHELF rather than in the plan.
	// EXCLUSIVE with Week — a card is on the shelf or in a week, never both,
	// or the board draws the same work twice and counts it twice against the
	// week. Read it through board.InBacklog.
	Parked bool `json:"parked,omitempty"`
	// Scope names the Project-board column this card belongs to ("" = none). An
	// scope card's row is its Week; StartDate..Day span the weeks its slot
	// covers when it stretches over more than one.
	Scope string `json:"scope,omitempty"`
	// Project is the project side of the card's column. A column is
	// identified by the PAIR (project, scope), because scope names are unique
	// only within a project — "Docs" or "Auth" belong to every project — so a
	// card filed under a column must name both. On a project-state card it is
	// the project's own name; on an scope-state card, the project owning it.
	Project string `json:"project,omitempty"`
	// ReviewOf, on a review card, is the itemId of the original card it reviews.
	ReviewOf string `json:"reviewOf,omitempty"`
	// Parent is the ONE link of the hierarchy: the itemId of the card this
	// one sits under ("" = a root). The parent's TYPE says what the link
	// means — read off the parent card, never off this field (H1): under a
	// task it makes this card a SUBTASK, one level deep (a parent cannot
	// itself be a subtask, and a card with subtasks cannot become one);
	// under an epic, an initiative or a goal it files the card in the
	// hierarchy and changes nothing else about it. A parent is always of a
	// higher level than its child (H2).
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
	// LeftAt is the board day (yyyy-mm-dd) the older × took the card off:
	// it demoted into the previous sprint, dates and all, and this recorded
	// the day that happened. The × deletes now and nothing writes the field;
	// the cards that carry it are still read. Nothing live reads it: today's
	// board drops the card, which is what the × was for. It is what a RECORD
	// of that day gives back (G60) — the card was worked and finished on it —
	// and a fact about a day gone by is not undone by re-dating the card, so
	// it stands.
	LeftAt string `json:"leftAt,omitempty"`
	// Link is a URL the card points at — the only trace an issue-backed
	// card keeps of its issue. Nothing is fetched through it.
	Link string `json:"link,omitempty"`
	// GitHubID is the Projects v2 item id a migrated card came from, kept so
	// the id can still be resolved for a while; empty on cards born later.
	GitHubID string `json:"githubId,omitempty"`
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
	Title  string `json:"title"`
	// Type is the card's level: "" (a task), "goal", "initiative" or "epic".
	Type        string  `json:"type,omitempty"`
	Zone        ZoneKey `json:"zone,omitempty"`
	Size        SizeKey `json:"size,omitempty"`
	Day         string  `json:"day,omitempty"`
	Start       string  `json:"start,omitempty"`
	SprintStart string  `json:"sprintStart,omitempty"`
	Assignee    string  `json:"assignee,omitempty"`
	Team        string  `json:"team,omitempty"`
	ReviewOf    string  `json:"reviewOf,omitempty"`
	Parent      string  `json:"parent,omitempty"`
	Week        string  `json:"week,omitempty"`
	// Parked puts the card straight on its team's SHELF: born there rather
	// than born in the strip and moved, which is one commit instead of two
	// and spares every reader the instant in between.
	Parked     bool   `json:"parked,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Project    string `json:"project,omitempty"`
	Recurrence string `json:"recurrence,omitempty"`
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

// ScopeCol is one column of the Project board: its name, the project that owns
// it, and the hidden scope-state card that declares it. The (Project, Name)
// pair is the column's identity — scope names repeat across projects, so a name
// alone does not identify a column.
type ScopeCol struct {
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
	// Capacity is the team's, from its roster file; zero when nothing is set.
	Capacity Capacity `json:"capacity,omitempty"`
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
	// People is what the roster says about each login — the primary
	// repository's users/<login>.yaml: a capacity a lead set, when one is.
	// A login absent here is a person the board only knows from cards.
	People map[string]Person `json:"people,omitempty"`
	// TeamOrder lists the SprintStates keys in board order — the position of
	// each team's hidden sprint-state card on the project. That position IS
	// the team order every client shares (reordering teams moves the card).
	TeamOrder []string `json:"teamOrder,omitempty"`
	// Scopes lists the Project board's columns in board order (the positions
	// of the hidden scope-state cards that declare them).
	Scopes []ScopeCol `json:"scopes,omitempty"`
	// Deadlines are the weeks carrying a deadline line, in board order.
	Deadlines []Deadline `json:"deadlines,omitempty"`
	// Projects lists the project roster in board order (the positions of the
	// hidden project-state cards) and ProjectStates maps each project to the
	// card that declares it. A project groups scopes: the Project board shows
	// one project's scopes as its columns.
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
	// project, an scope column, a deadline — to the domain
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
// included. The rules that read the board (HomeDomain, ColumnDomain) ask
// "the same repository?" in that one namespace, so the primary's name
// arrives with the cards rather than being set on the result afterwards.
func NewBoardIn(primary string, cards []Card) Board {
	b := Board{Primary: primary,
		Cards:        make([]Card, 0, len(cards)),
		SprintStates: map[string]SprintState{},
	}
	seen := map[string]bool{}
	winners := map[string]Card{}
	scopeSeen := map[string]bool{}
	projectSeen := map[string]bool{}
	deadlineSeen := map[string]bool{}
	scopeKey := func(project, name string) string { return project + "\x00" + name }
	for _, c := range cards {
		if c.Domain != "" && IsStateTitle(c.Title) {
			if b.Domains == nil {
				b.Domains = map[string]string{}
			}
			b.Domains[c.ItemID] = c.Domain
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
			// Same duplicate rule as scope-state: the first position wins.
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
		if c.Title == ScopeStateTitle {
			// Same duplicate rule as sprint-state: the first position wins
			// the order, the oldest card wins the state slot.
			if c.Scope == "" {
				continue
			}
			if k := scopeKey(c.Project, c.Scope); !scopeSeen[k] {
				scopeSeen[k] = true
				b.Scopes = append(b.Scopes, ScopeCol{
					Name: c.Scope, Project: c.Project, ItemID: c.ItemID,
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
	// A card can name an scope without naming a project: it predates the pair,
	// or someone set the Scope field straight in the GitHub UI. When exactly one
	// column bears that name it can only be that one, so it is adopted on READ
	// — no rewriting of anyone's data, and the next write settles the pair.
	// An ambiguous name (two projects, one column name) is left unattached
	// rather than guessed at.
	for i := range b.Cards {
		if b.Cards[i].Scope == "" || b.Cards[i].Project != "" {
			continue
		}
		match, count := "", 0
		for _, col := range b.Scopes {
			if col.Name == b.Cards[i].Scope {
				match, count = col.Project, count+1
			}
		}
		if count == 1 {
			b.Cards[i].Project = match
		}
	}
	// A card in a column and a planning card have no band and no size (H4,
	// H3): the Project board draws columns and date rows, the Roadmap draws
	// levels and bars, and a weight belongs to work somebody carries. A file
	// that still says otherwise — a repository migrated before the rule, a
	// direct writer — is read without them, and the card's next rewrite
	// drops the keys; nothing is rewritten for the reading alone.
	// A planning card is nobody's and no team's besides (H3): the people
	// and the teams are on the tasks under it, and the Roadmap reads them
	// off the tree.
	// Nor a sprint or a week: no day board draws either kind, so a sprint
	// left on one (a team had picked the card up before the rule) is a
	// sprint a carry-over would move for nothing and nothing else reads.
	for i := range b.Cards {
		if b.Cards[i].Scope != "" || IsPlanning(b.Cards[i]) {
			b.Cards[i].Zone, b.Cards[i].Size = ZoneNone, ""
			b.Cards[i].SprintStart, b.Cards[i].Week = "", ""
		}
		if IsPlanning(b.Cards[i]) {
			b.Cards[i].Team, b.Cards[i].Assignees = "", nil
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

// ScopesOf lists a project's scope columns in board order. A column whose
// project is unset belongs to none and shows only in the all-projects view —
// it is never silently adopted.
func ScopesOf(b Board, project string) []ScopeCol {
	var out []ScopeCol
	for _, e := range b.Scopes {
		if e.Project == project {
			out = append(out, e)
		}
	}
	return out
}

// FindScope looks a column up by its identity — the (project, scope) pair.
func FindScope(b Board, project, name string) (ScopeCol, bool) {
	for _, e := range b.Scopes {
		if e.Project == project && e.Name == name {
			return e, true
		}
	}
	return ScopeCol{}, false
}

// InScope reports whether a card is filed under a column. Both halves have to
// match: the same scope name in another project is a different column.
func InScope(c Card, project, name string) bool {
	return c.Scope == name && c.Project == project
}

// InProject reports whether a card stands in one project's columns.
func InProject(c Card, project string) bool {
	return c.Project == project
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
