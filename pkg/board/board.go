// Package board holds aeman's pure board logic, ported from the web frontend:
// date helpers, the stage model, per-team sprint pointers, the board views
// (Team grid, Me, Backlog) and the status/progress transition rules. Every
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
	// on the Strategy board and on no day board (docs/design/hierarchy.md). Fixed
	// at create; a card never changes level.
	Type string `json:"type,omitempty"`
	// Assignees are logins; Author is the login that created the card.
	Assignees []string `json:"assignees"`
	Author    string   `json:"author,omitempty"`
	// Team is the card's team label ("" = the no-team group). On a planning
	// card it is the team that carries the plan item out (O1): it places
	// nothing — the card lives in the primary (H3) — and the cards below
	// inherit it as their effective team when they have none (O3).
	Team string `json:"team,omitempty"`
	// Owner is the one person who answers for a PLANNING card — breaks it
	// down, keeps its status honest (O1). Never inherited, and it puts
	// nothing on any day board (O2); "" on every other card.
	Owner string `json:"owner,omitempty"`
	// Customers are the ids of the board's customers (Board.Customers) the
	// card is for. Any card may name several; the cards
	// below add to them (O4).
	Customers []string `json:"customers,omitempty"`
	// DependsOn are the uids of the cards this one waits for
	// (docs/design/dependencies.md): it is LOCKED while one of them, or one
	// an ancestor waits for, is not done — derived, never written — and the
	// targets are for this card's customers too (N1–N6).
	DependsOn []string `json:"dependsOn,omitempty"`
	Zone      ZoneKey  `json:"zone,omitempty"`
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
	// Parked says the task is in its team's BACKLOG rather than in work. A
	// task is in work (a date or a sprint) or parked, never both and never
	// neither (K1). Read it through board.InBacklog.
	Parked bool `json:"parked,omitempty"`
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
	// Domain, on a roster stub (a team's state card), is
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
	// Parked puts the card straight on its team's SHELF: born there rather
	// than born somewhere else and moved, which is one commit instead of two
	// and spares every reader the instant in between.
	Parked     bool   `json:"parked,omitempty"`
	Recurrence string `json:"recurrence,omitempty"`
	// Body is the draft's description, written with the create rather than
	// after it: a card that appears without its text, then fills in a second
	// later, reads as a card the board got wrong.
	Body string `json:"body,omitempty"`
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
	// Primary names the repository a board's own entries belong to — the
	// first of its domains. The store STAMPS every roster entry with its
	// domain's name, the primary included, while a card that nothing
	// places resolves to "" ("no repository decides this"); the two are
	// different namespaces, and comparing them directly answered "another
	// repository" for every card of the primary that nothing placed.
	// Boards built by hand (fixtures, older callers) leave it empty, which
	// is the same thing said the other way.
	Primary string `json:"primary,omitempty"`
	// Domains maps a roster entry's id — a team's sprint-state card — to the domain
	// (repository) it was declared in; cards carry their own Domain. Nil on
	// a single-domain board, where nothing records one.
	Domains map[string]string `json:"domains,omitempty"`
	// Customers is the board's customer roster in board order (O5): cards
	// name a customer by its id, so a rename reaches every card at once.
	Customers []Customer `json:"customers,omitempty"`
	// DroppedParents names the parents the reading dropped, by the card
	// that named them: a parent the board does not hold is read as no
	// parent (NewBoardIn), and a tool that must REPORT such links — the
	// importer's dangling references, M4 — reads them here.
	DroppedParents map[string]string `json:"-"`
	// BlockerTotals is how many blockers each card has on the WHOLE board,
	// recorded by Visible when the projection hides something: the
	// visitor's resource counts the ones it cannot name (N4). Nil when
	// nothing was hidden.
	BlockerTotals map[string]int `json:"-"`
	// UndoneTargets are the cards something waits for that are NOT done on
	// the WHOLE board, recorded by Visible beside BlockerTotals: a plan item
	// is done by its whole roll-up, not by the part of it the visitor's board
	// holds, so the visitor's listing judges a blocker the way a watch frame
	// does (N4). Nil when nothing was hidden.
	UndoneTargets map[string]bool `json:"-"`
	// JudgedWhole says Visible hid something on a board where something
	// waits: the visitor's dependencies are judged on the WHOLE board
	// (BlockerTotals, UndoneTargets), even when those maps are empty —
	// an empty map is "nothing undone on the whole board", not "judge on the
	// part you can see" (N4).
	JudgedWhole bool `json:"-"`
}

// Customer is one entry of the board's customer roster: who the work is for.
type Customer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Rank string `json:"rank,omitempty"`
}

// CustomerName is the roster name of a customer id, or "" for one the
// roster does not hold.
func (b Board) CustomerName(id string) string {
	for _, c := range b.Customers {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

// NewBoard builds a Board snapshot from the full card list, splitting the hidden sprint-state cards out of Cards into SprintStates (keyed
// by team, "" = the no-team group). A sprint-state
// card's Team is its key, its SprintStart is the team's current sprint and its
// StartDate (the "Start" field) is the previous sprint.
func NewBoard(cards []Card) Board { return NewBoardIn("", cards) }

// NewBoardIn is NewBoard for a board whose entries are STAMPED with their
// domain's name — which is every board the store hands over, the primary
// included. The rules that read the board (TeamDomain, HomeDomain) ask
// "the same repository?" in that one namespace, so the primary's name
// arrives with the cards rather than being set on the result afterwards.
func NewBoardIn(primary string, cards []Card) Board {
	b := Board{Primary: primary,
		Cards:        make([]Card, 0, len(cards)),
		SprintStates: map[string]SprintState{},
	}
	seen := map[string]bool{}
	winners := map[string]Card{}
	for _, c := range cards {
		if c.Domain != "" && IsStateTitle(c.Title) {
			if b.Domains == nil {
				b.Domains = map[string]string{}
			}
			b.Domains[c.ItemID] = c.Domain
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
	for i := range b.Cards {
		b.Cards[i] = SettleCard(b.Cards[i])
	}
	dropDanglingParents(&b)
	for team, c := range winners {
		b.SprintStates[team] = SprintState{
			Current:  c.SprintStart,
			Previous: c.StartDate,
			ItemID:   c.ItemID,
		}
	}
	return b
}

// SettleCard applies the reading rules that hold for one card on its own,
// whatever the file says — the same on a full read of the board and on the
// reload of a single card, or the two would disagree about one file.
//
// A planning card has no band, no size and no sprint (H3): the Strategy board draws
// levels and bars, and a weight belongs to work somebody carries. A file that
// still says otherwise — a repository migrated before the rule, a direct
// writer — is read without them, and the card's next rewrite drops the keys;
// nothing is rewritten for the reading alone. A planning card is nobody's
// work besides: the people are on the tasks under it, and its owner and team
// (O1) answer for the plan item without putting it on any day board. Only a
// planning card has an owner.
func SettleCard(c Card) Card {
	if IsPlanning(c) {
		c.Zone, c.Size = ZoneNone, ""
		c.SprintStart = ""
		c.Assignees = nil
	} else {
		c.Owner = ""
	}
	return c
}

// dropDanglingParents reads a parent the board does not hold as no parent,
// and names what it dropped in DroppedParents. Every rule reads a card with
// a parent that is not a planning card as a subtask — hidden from every
// board — so an epic deleted by a direct writer, or a torn move, would
// otherwise take its whole subtree off Me, Team, the backlogs and the Strategy board with
// nothing to bring it back. Read as a root, the card stands where its own
// dates and team put it; its next rewrite drops the key. It needs the whole
// board, which is why it is not part of SettleCard.
func dropDanglingParents(b *Board) {
	held := make(map[string]bool, len(b.Cards))
	for _, c := range b.Cards {
		held[c.ItemID] = true
	}
	for i := range b.Cards {
		if p := b.Cards[i].Parent; p != "" && !held[p] {
			if b.DroppedParents == nil {
				b.DroppedParents = map[string]string{}
			}
			b.DroppedParents[b.Cards[i].ItemID] = p
			b.Cards[i].Parent = ""
		}
	}
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
