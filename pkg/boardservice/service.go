package boardservice

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrCardNotFound is returned when an item id is not on the loaded board.
var ErrCardNotFound = errors.New("card not found")

// ErrForbidden is a write the caller may not make: the domain it targets —
// the card's repository, or the one a move would file it into — is not
// theirs to write.
var ErrForbidden = errors.New("forbidden: no write access to the card's domain")

// Log is a card's activity feed: the card (for its notes) and the backend's
// history of it, with the horizon that history is cut at, if any.
func (s *Service) Log(ctx context.Context, boardID string, id string) (board.Card, []board.Event, time.Time, error) {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return board.Card{}, nil, time.Time{}, err
	}
	card, ok := findCard(b, id)
	if !ok {
		return board.Card{}, nil, time.Time{}, ErrCardNotFound
	}
	events, truncated, err := s.backend.CardLog(ctx, b, id)
	if err != nil {
		return board.Card{}, nil, time.Time{}, err
	}
	return card, events, truncated, nil
}

// ErrNoteNotFound is returned when a note id is not on the loaded card.
var ErrNoteNotFound = errors.New("note not found")

// ErrInvalidStage is returned when a stage cannot apply to a card — a review
// card cannot be made recurrent (a review is one-off, not a repeating task).
var ErrInvalidStage = errors.New("invalid stage for this card")

// ErrTeamInUse is returned when deleting a team that still has cards on the
// board — the caller must reassign or delete them first.
var ErrTeamInUse = errors.New("team still has cards")

// ErrDomainConflict is a team and a project declared in different
// repositories put on one card. The project decides where the card lives
// (board.DomainOf, rule 4), so such a card sits in the project's repository
// carrying the name of a team whose people cannot read it. Neither side can
// be honoured, so the pair is refused where it is made instead of being
// accepted and quietly ignored.
var ErrDomainConflict = errors.New("the team and the project live in different repositories")

// guardRoster refuses a team/project pair from two repositories, naming
// both. A name the roster does not declare yet decides nothing: it will be
// declared in the card's own repository on the way.
func guardRoster(b board.Board, team, project string) error {
	td, pd, conflict := board.RosterConflict(b, team, project)
	if !conflict {
		return nil
	}
	return fmt.Errorf("%w: team %q lives in %s, project %q in %s",
		ErrDomainConflict, team, repoName(td), project, repoName(pd))
}

// repoName is a domain for a message: the primary has no name of its own.
func repoName(domain string) string {
	if domain == "" {
		return "the primary repository"
	}
	return strconv.Quote(domain)
}

// MaxDescriptionLen caps a card description (in runes). The description shares
// a draft card's body with the note and event logs, and GitHub caps the body
// at ~64K — an oversized description would break log appends.
const MaxDescriptionLen = 16384

// ErrDescriptionTooLong is returned when a description exceeds MaxDescriptionLen.
var ErrDescriptionTooLong = fmt.Errorf("description is too long (max %d characters)", MaxDescriptionLen)

// MaxNoteLen caps a single work note (in runes). A note is a short comment,
// not a document — and notes share the same ~64K draft body as the description
// and event log, so one oversized note would crowd out the rest.
const MaxNoteLen = 4096

// ErrNoteTooLong is returned when a note exceeds MaxNoteLen.
var ErrNoteTooLong = fmt.Errorf("note is too long (max %d characters)", MaxNoteLen)

// Service performs aeman's board actions. It is stateless: every method loads
// the board through the backend, computes the change with internal/board logic,
// then applies it through the backend setters.
type Service struct {
	backend Backend
}

// New builds a Service over a backend.
func New(backend Backend) *Service {
	return &Service{backend: backend}
}

// findCard returns the card with the given item id, or ok = false. A legacy
// Projects v2 item id (the `githubId` the migration kept) still names the
// card it became, for one major version — old links keep working; the ULID
// is the key, and wins when both would match.
func findCard(b board.Board, itemID string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ItemID == itemID {
			return c, true
		}
	}
	if !board.IsLegacyID(itemID) {
		return board.Card{}, false
	}
	for _, c := range b.Cards {
		if c.GitHubID == itemID {
			return c, true
		}
	}
	return board.Card{}, false
}

// findReviewCard returns the review card linked to an original (its ReviewOf is
// the original's item id), or ok = false.
func findReviewCard(b board.Board, originalItemID string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ReviewOf == originalItemID {
			return c, true
		}
	}
	return board.Card{}, false
}

// loadCard loads the board and resolves a card by item id.
func (s *Service) loadCard(ctx context.Context, boardID string, itemID string) (board.Board, board.Card, error) {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return board.Board{}, board.Card{}, err
	}
	card, ok := findCard(b, itemID)
	if !ok {
		return b, board.Card{}, fmt.Errorf("%w: %s", ErrCardNotFound, itemID)
	}
	return b, card, nil
}

// --- Snapshot --------------------------------------------------------------

// Board returns the full board snapshot: identity, fields, the visible cards and
// the per-team sprint pointers. The API/MCP board endpoint builds its meta
// response (fields + sprint states) from it.
func (s *Service) Board(ctx context.Context, boardID string) (board.Board, error) {
	return s.backend.LoadBoard(ctx, boardID)
}

// Card loads the board and returns a single card by item id (ErrCardNotFound
// when absent). The API/MCP layer uses it to return the card resulting from an
// action, mirroring the way the UI re-renders the card it just changed.
func (s *Service) Card(ctx context.Context, boardID string, itemID string) (board.Card, error) {
	_, card, err := s.loadCard(ctx, boardID, itemID)
	return card, err
}

// --- Views -----------------------------------------------------------------

// TeamView returns the Team board's grid cards for a team on a day (day = "" is
// today). It mirrors filteredCards/passesFilter in TeamBoard.tsx via board.TeamGrid.
func (s *Service) TeamView(ctx context.Context, boardID string, team, day string) ([]board.Card, error) {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return nil, err
	}
	if day == "" {
		day = board.TodayIso()
	}
	return board.TeamGrid(b, team, day), nil
}

// MeView returns the personal day board for a user on a day (user = "" is
// everyone, day = "" is today). It mirrors myCards in MeBoard.tsx via board.MeView.
func (s *Service) MeView(ctx context.Context, boardID string, user, day string) ([]board.Card, error) {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return nil, err
	}
	if day == "" {
		day = board.TodayIso()
	}
	return board.MeView(b, user, day), nil
}

// --- Create / sprints ------------------------------------------------------

// CreateCardArgs are the inputs to CreateCard. Day defaults to today.
// StartNewSprint forces the team's sprint pointer: nil = auto (record the day as
// the team's first sprint only when it has none), true = always (re)start the
// pointer on the day, false = same as auto (start one only when there is none).
type CreateCardArgs struct {
	Team     string
	Zone     board.ZoneKey
	Title    string
	Assignee string
	Day      string
	// Start and SprintStart override the scheduled day and the sprint the card
	// joins (defaults: the day, and the team's current sprint).
	Start       string
	SprintStart string
	// Week creates a card scheduled for a WEEK instead of a day card: no
	// dates are set and no sprint is joined or started.
	Week string
	// Epic + Project create a Project-board card: filed under the column that
	// pair identifies, its Week is the row (defaulting to the Monday of Start,
	// then of today), Start/Day may span several weeks, and no sprint is
	// joined — the card lives on the Project board until a team picks it up.
	Epic    string
	Project string
	// ReviewOf marks the new card as the review of the given item.
	ReviewOf string
	// Personal files the card in the actor's personal domain — a backlog item
	// of their own: no team, no sprint, no column, no plan band. The service
	// names the domain from the actor; the caller only asks for it.
	Personal bool
	// Parent groups the new card as a subtask of the given item on create.
	Parent         string
	StartNewSprint *bool
	// NoSprint schedules the card for its day without joining any sprint — a
	// "next sprint" create: the card lives on its own day only until the first
	// carry-over whose day has reached the card's start adopts it into the
	// sprint it opens.
	NoSprint bool
}

// CreateCard creates a card with two dates: StartDate (its scheduled day) is the
// requested day (today by default), and SprintStart (the sprint it joins) is the
// team's current sprint — so a card created on a later day of the sprint keeps the
// sprint's start. A team with no sprint yet records its first one off this card on
// its sprint-state card, anchoring the Me view; force-new (re)starts the pointer on
// the day and the card joins that fresh sprint. It mirrors handleCreate in
// TeamBoard.tsx / MeBoard.tsx.
func (s *Service) CreateCard(ctx context.Context, boardID string, args CreateCardArgs) (board.Card, error) {
	if err := planningYourOwnWork(ctx, args); err != nil {
		return board.Card{}, err
	}
	// A week is a Monday wherever one is given, this door included.
	if err := guardWeek(args.Week); err != nil {
		return board.Card{}, err
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return board.Card{}, err
	}
	// A title that is nothing but a GitHub issue/PR URL turns into that item's
	// real title, with the link moved into the description — a one-time
	// resolution at create, never re-synced. The URL is always filed in the
	// body, and the title defaults to a readable "Issue: owner/repo#N" /
	// "Pull: owner/repo#N": when resolution fails (no repo access on a private
	// repo, dead link) the card stays usable — a bare URL as the title, or a
	// content-less item that never renders, is what left cards invisible.
	// Resolving the ref costs a GitHub round trip (seconds), and blocking the
	// create on it left the raw URL sitting in the UI all that time. Create at
	// once under the readable fallback and fetch the real title in the
	// background (resolveTitleAsync), which renames the card — unless the
	// person retitled it first.
	var linkDescription string
	var pendingRef *board.Link
	if ref, ok := board.ParseGitHubRef(strings.TrimSpace(args.Title)); ok {
		linkDescription = ref.URL
		args.Title = ref.FallbackTitle()
		pendingRef = &ref
	}
	// A team and a project from two repositories cannot both be honoured, so
	// the card is not created carrying them.
	if err := guardRoster(b, args.Team, args.Project); err != nil {
		return board.Card{}, err
	}
	// A personal card lives in the actor's own repository, outside the team
	// board's placement: it has a zone, dates and a body, and none of team,
	// sprint, column or plan band — those are the team board's coordinates.
	if err := linksArePossible(b, args); err != nil {
		return board.Card{}, err
	}
	if args.Personal {
		return s.createPersonalCard(ctx, b, args, linkDescription, pendingRef)
	}

	// An epic card lives on the Project board: filed under its column,
	// anchored to its week row, optionally spanning several weeks via
	// start..day. It joins no sprint.
	if args.Epic != "" {
		return s.createEpicCard(ctx, b, args, linkDescription, pendingRef)
	}
	// A card given a WEEK and no day is scheduled for that week alone.
	if args.Week != "" && args.Start == "" && args.Day == "" && args.SprintStart == "" {
		return s.createWeekCard(ctx, b, args, linkDescription, pendingRef)
	}
	// Start and Day (the end/due date) default to each other so a create with
	// only one of them yields a one-day range — a backdated create must NOT get
	// day = today, or the [start…day] range would stretch it onto today's board.
	day := args.Day
	start := args.Start
	if start == "" {
		if day == "" {
			day = board.TodayIso()
		}
		start = day
	} else if day == "" {
		day = start
	}
	sprint := args.SprintStart
	// A card scheduled for a FUTURE day is a "next sprint" create by another
	// name: the sprint for that day does not exist yet (sprints are daily and
	// open as they start), so joining today's sprint would only park the card
	// in a sprint it will never be worked in — dragged forward by every daily
	// carry-over, its dates reading "current sprint" the whole time. Leave it
	// sprint-less and let the carry-over that reaches its day adopt it, which
	// is exactly what NoSprint does. An explicit sprint (or an explicit
	// start-a-new-sprint) still wins: the caller said what they wanted.
	futureDay := start != "" && start > board.TodayIso() &&
		args.SprintStart == "" && args.StartNewSprint == nil
	if args.NoSprint || futureDay {
		// A "next sprint" create: the card is scheduled for its day but joins
		// no sprint (and starts none) — the first carry-over whose day reaches
		// the card's start adopts it into the sprint it opens. The team it
		// names is declared all the same: a card on a team the board does not
		// list is on no roster and in no column.
		sprint = ""
		if err := s.declareTeam(ctx, b, args.Team); err != nil {
			return board.Card{}, err
		}
	} else {
		cur := board.CurrentSprint(b, args.Team)
		startNew := cur == ""
		if args.StartNewSprint != nil {
			startNew = *args.StartNewSprint || cur == ""
		}
		if startNew {
			// Record the new sprint and have the card join it (previous = the old
			// current, which is "" when the team had no sprint yet — matching the
			// frontend's setSprintState(team, day, null)).
			if sprint == "" {
				sprint = day
			}
			if err := s.backend.SetSprintState(ctx, b, args.Team, sprint, cur); err != nil {
				return board.Card{}, err
			}
		} else if sprint == "" {
			sprint = cur
		}
	}
	// A subtask create is validated BEFORE the card exists and the card is
	// born already parented — a create-then-group pair would broadcast (and
	// persist) a parentless instant that watchers and mid-sync reloads see as
	// a stray top-level card.
	// Start is the scheduled day; SprintStart is the sprint the card belongs to.
	card, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title: args.Title,
		Zone:  args.Zone,
		Day:   day,
		Start: start,
		// The week the caller asked for, when they asked for one: a card
		// started in the week being worked belongs to that week AND to
		// today, and the two are not in conflict.
		Week:        args.Week,
		SprintStart: sprint,
		Assignee:    args.Assignee,
		Team:        args.Team,
		ReviewOf:    args.ReviewOf,
		Parent:      args.Parent,
	})
	card, err = s.withLinkDescription(ctx, b, card, err, linkDescription)
	if err == nil {
		s.logEvent(ctx, b, card, board.EventCreated, "", "")
		s.resolveTitleAsync(ctx, b, card, pendingRef)
	}
	if err == nil && args.Parent != "" {
		if perr := s.groupOrUndo(ctx, b, card, args.Parent); perr != nil {
			return board.Card{}, perr
		}
		card.Parent = args.Parent
	}
	return card, err
}

// spawn runs background work. Tests replace it to run inline so the
// asynchronous title resolve is deterministic.
var spawn = func(fn func()) { go fn() }

// asyncResolveTimeout bounds a background title resolve; GitHub occasionally
// takes seconds, but a create must never leave work hanging indefinitely.
const asyncResolveTimeout = 30 * time.Second

// resolveTitleAsync fetches a create-by-URL card's real title in the
// background and renames the card to it. The create itself already returned
// under the readable "Issue: owner/repo#N" fallback, so nobody waits on
// GitHub. Two guards: the rename is skipped when the person (or an agent)
// retitled the card meanwhile — their words win over ours — and the work is
// marked unattributed so the watch frame reaches the creating tab too,
// instead of being echo-suppressed into invisibility.
func (s *Service) resolveTitleAsync(ctx context.Context, b board.Board, card board.Card, ref *board.Link) {
	if ref == nil {
		return
	}
	resolver, ok := s.backend.(LinkResolver)
	if !ok {
		return
	}
	fallback := ref.FallbackTitle()
	link := *ref
	itemID := card.ItemID
	// Detached from the request: the response is already on its way out, but
	// the caller's values (token, actor) must survive.
	bg := board.Unattributed(context.WithoutCancel(ctx))
	spawn(func() {
		ctx, cancel := context.WithTimeout(bg, asyncResolveTimeout)
		defer cancel()
		resolved, err := resolver.ResolveIssueRef(ctx, link)
		if err != nil || resolved.Title == "" || resolved.Title == fallback {
			return
		}
		fresh, cerr := s.backend.LoadBoard(ctx, b.Board)
		if cerr != nil {
			return
		}
		current, found := findCard(fresh, itemID)
		// Gone, or retitled by a human while we were away: leave it alone.
		if !found || current.Title != fallback {
			return
		}
		_ = s.backend.RenameCard(ctx, fresh, current, resolved.Title)
	})
}

// createEpicCard files a new card on the Project board: under an existing
// column — the (project, epic) pair — anchored to its week row and optionally
// spanning several weeks. No sprint, no plan band: a team picking it up later
// is what schedules it.
// createWeekCard files a card scheduled for a WEEK and nothing else — the
// Triage board's own create, where the week IS the decision. It takes no band
// (that is a promise on the weekly panel, and nobody makes one by scheduling
// work) and no dates: defaulting a start to today would put the card on
// today's board, which is what choosing a week ahead was for (B1). Place says
// the same thing from the other side.
func (s *Service) createWeekCard(ctx context.Context, b board.Board, args CreateCardArgs, linkDescription string, pendingRef *board.Link) (board.Card, error) {
	if err := s.declareTeam(ctx, b, args.Team); err != nil {
		return board.Card{}, err
	}
	card, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:    args.Title,
		Zone:     args.Zone,
		Week:     args.Week,
		Assignee: args.Assignee,
		Team:     args.Team,
		ReviewOf: args.ReviewOf,
		Parent:   args.Parent,
	})
	card, err = s.withLinkDescription(ctx, b, card, err, linkDescription)
	if err == nil {
		s.resolveTitleAsync(ctx, b, card, pendingRef)
		s.logEvent(ctx, b, card, board.EventCreated, "", "")
	}
	return card, err
}

func (s *Service) createEpicCard(ctx context.Context, b board.Board, args CreateCardArgs, linkDescription string, pendingRef *board.Link) (board.Card, error) {
	if _, ok := board.FindEpic(b, args.Project, args.Epic); !ok {
		return board.Card{}, fmt.Errorf("%w %q in project %q — add it first (add_epic / POST /epics)",
			ErrEpicNotFound, args.Epic, args.Project)
	}
	start := args.Start
	if start == "" {
		start = board.TodayIso()
	}
	day := args.Day
	if day == "" {
		day = start
	}
	// The row is the start's week, full stop: an explicit week here would be
	// a second value free to disagree with the dates the moment either moves.
	week := board.MondayOf(start)
	card, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:   args.Title,
		Zone:    args.Zone,
		Epic:    args.Epic,
		Project: args.Project,
		// Born parented and born LINKED, like the other create doors: a
		// create-then-group pair broadcasts a parentless instant that
		// watchers and mid-sync reloads read as a stray top-level card,
		// and a dropped reviewOf answered a request for a review card with
		// an ordinary one — 201, no error, no link. SetParent below still
		// runs, for the grouping's own side effects.
		Parent:   args.Parent,
		ReviewOf: args.ReviewOf,
		Week:     week,
		Start:    start,
		Day:      day,
		Team:     args.Team,
		Assignee: args.Assignee,
	})
	card, err = s.withLinkDescription(ctx, b, card, err, linkDescription)
	if err == nil {
		s.logEvent(ctx, b, card, board.EventCreated, "", "")
		s.resolveTitleAsync(ctx, b, card, pendingRef)
	}
	// A subtask may carry a column of its own (G14), and the Project board
	// draws it there (G57) — so the pair is a state to produce, not one to
	// drop on the floor: dropping it answered a request for a child with a
	// top-level card standing in someone's planner. Grouping goes through
	// SetParent like every other, guards and riders included.
	if err == nil && args.Parent != "" {
		if perr := s.groupOrUndo(ctx, b, card, args.Parent); perr != nil {
			return board.Card{}, perr
		}
		card.Parent = args.Parent
	}
	return card, err
}

// withLinkDescription moves the source URL of a create-by-URL card into its
// description. A failure to write the description is not fatal — the card
// exists with the right title; the link is just not filed.
func (s *Service) withLinkDescription(ctx context.Context, b board.Board, card board.Card, err error, url string) (board.Card, error) {
	if err != nil || url == "" {
		return card, err
	}
	if setErr := s.backend.SetDescription(ctx, b, card, url); setErr == nil {
		card.Description = url
	}
	return card, nil
}

// CarryReport summarizes what a carry pass did — or would do, on a dry run
// (which backs the UI's confirm dialog counts).
type CarryReport struct {
	// Carried counts the open plan cards from earlier weeks — the debts now
	// showing on the target week's panel. They are not moved.
	Carried  int `json:"carried"`
	Reseeded int `json:"reseeded"`
}

// ReorderTeams applies a shared team order by moving the hidden sprint-state
// cards into the given sequence on the project — the board position of those
// cards IS the team order every client reads back (Board.TeamOrder). Teams
// without a sprint pointer are skipped; teams missing from the list keep
// their positions after the reordered ones.
func (s *Service) ReorderTeams(ctx context.Context, boardID string, teams []string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	prev := ""
	for _, team := range teams {
		st, ok := b.SprintStates[team]
		if !ok || st.ItemID == "" {
			continue
		}
		stub := board.Card{ItemID: st.ItemID, Title: board.SprintStateTitle, Team: team}
		if err := s.backend.MoveCard(ctx, b, stub, prev); err != nil {
			return err
		}
		prev = st.ItemID
	}
	return nil
}

// DeleteTeam removes a team from the board by deleting its hidden sprint-state
// card. A team still referenced by cards is protected (ErrTeamInUse): deleting
// its pointer would orphan them silently. A team with no pointer has nothing
// server-side to delete — that is a no-op success, the client just drops its
// local entry.
func (s *Service) DeleteTeam(ctx context.Context, boardID string, team string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	inUse := 0
	for _, c := range b.Cards {
		if c.Team == team {
			inUse++
		}
	}
	if inUse > 0 {
		return fmt.Errorf("%w: %d card(s) still on %q — reassign or delete them first", ErrTeamInUse, inUse, team)
	}
	st, ok := b.SprintStates[team]
	if !ok || st.ItemID == "" {
		return nil
	}
	stub := board.Card{ItemID: st.ItemID, Title: board.SprintStateTitle, Team: team}
	return s.backend.DeleteCard(ctx, b, stub)
}

// selectCarry partitions a team's cards for a carry-over closing sprint old
// and opening today: cards to carry forward, finished recurrent cards to
// reseed, and completed review cards to pin back to the closing sprint.
func selectCarry(b board.Board, team, old, today string) (carry, reseed, relocate []board.Card) {
	// Carry only the unfinished cards of the sprint being closed (sprintStart ==
	// the old current pointer). A card that is NOT on today's sprint — demoted
	// back to an earlier one, or simply old — stays where it is, so removing a
	// card from the current sprint is final and it never boomerangs back. A
	// finished recurrent card stays behind and reseeds a fresh copy instead.
	for _, c := range b.Cards {
		// A subtask is never carried on its own - it rides with its parent
		// (below), whatever team either of them belongs to.
		if c.Parent != "" {
			continue
		}
		if c.Team != team {
			continue
		}
		// Adopt sprint-less day cards whose scheduled day has arrived (a "next
		// sprint" create): they join the sprint this carry-over opens, which is
		// what "the next sprint" meant when they were created. Only cards
		// scheduled PAST the closing sprint qualify — an old sprint-less stray
		// (a report card, a pre-sprint legacy card) is not this sprint's work
		// and must not be dragged in.
		if c.SprintStart == "" && c.StartDate != "" &&
			old != "" && c.StartDate > old &&
			c.StartDate <= today && !board.Complete(c.Stage, c.Progress) {
			carry = append(carry, c)
			continue
		}
		// A weekly/monthly recurrent card RESTS outside the current cycle: a
		// finished (or deliberately backdated) one sits in a past sprint until
		// the interval elapses, then the carry-over that reaches its due day
		// reseeds it. A newer copy (same team+title, later sprint) means it
		// was already reseeded — skip, so reruns never duplicate.
		if c.Recurrence != "" && c.Stage == board.StageRecurrent && c.SprintStart != old {
			if board.RecurrenceDue(c, today) && !hasNewerRecurrent(b, c) {
				reseed = append(reseed, c)
			}
			continue
		}
		if old == "" || c.SprintStart != old {
			continue
		}
		if c.Stage == board.StageRecurrent && c.Progress >= 100 {
			// A cycle card finished in the closing sprint reseeds only when
			// its week/month has elapsed; until then it stays behind, resting.
			if board.RecurrenceDue(c, today) {
				reseed = append(reseed, c)
			}
			continue
		}
		if board.Complete(c.Stage, c.Progress) {
			// A completed review card created on a later day of the closing
			// sprint keeps its own start-day (often today), so it would linger on
			// the new sprint's day even though it is not carried. Pull its dates
			// back to the closing sprint so it stays behind; a re-review in the
			// new sprint relocates it forward.
			if c.ReviewOf != "" && (c.StartDate != old || c.Day != old) {
				relocate = append(relocate, c)
			}
			continue
		}
		// A review card crosses sprints only while its review is still on: the
		// original must still sit on the review stage unfinished, and the card
		// must still be the assigned reviewer's. Stale review work (original
		// done, reviewer swapped, link severed) stays behind.
		if c.ReviewOf != "" && !reviewStillRequired(b, c) {
			continue
		}
		carry = append(carry, c)
	}
	return carry, reseed, relocate
}

// CarryOver advances a team's sprint to today (the old current becomes previous)
// and pulls its unfinished cards forward. It mirrors startSprint in TeamBoard.tsx:
// idempotent (a no-op when the sprint is already today's), and it always advances
// even with nothing to carry. team = "" is the no-team group. With dryRun the
// would-be counts are reported and nothing is written.
func (s *Service) CarryOver(ctx context.Context, boardID string, team string, dryRun bool) (CarryReport, error) {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return CarryReport{}, err
	}
	old := board.CurrentSprint(b, team)
	today := board.TodayIso()
	if old == today {
		// Already on today's sprint: re-advancing would overwrite previous.
		return CarryReport{}, nil
	}
	carry, reseed, relocate := selectCarry(b, team, old, today)
	// Subtasks ride with their carried parent - even completed ones, and
	// whatever team they belong to; the parent's team drives the sprint.
	followers := carryFollowers(b, carry)
	carry = append(carry, followers...)
	rep := CarryReport{Carried: len(carry) - len(followers), Reseeded: len(reseed)}
	if dryRun {
		return rep, nil
	}
	if err := s.backend.SetSprintState(ctx, b, team, today, old); err != nil {
		return CarryReport{}, err
	}
	// Pin completed review cards to the closing sprint's day so they do not
	// linger on the new sprint (best-effort; failures are non-fatal).
	for _, c := range relocate {
		_ = s.backend.SetStart(ctx, b, c, old)
		_ = s.backend.SetDay(ctx, b, c, old)
		_ = s.backend.SetSprintStart(ctx, b, c, old)
	}
	// The per-card writes are independent, but a burst of concurrent Projects v2
	// mutations trips GitHub's secondary rate limit, so run them at a modest
	// concurrency, retry transient failures, and — crucially — do NOT stop at the
	// first error: a partial carry-over that silently drops a card is worse than
	// a slow one. Only cards that still fail after retries surface as an error.
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed int
	var firstErr error
	for _, c := range carry {
		wg.Add(1)
		sem <- struct{}{}
		go func(c board.Card) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.setSprintStartRetry(ctx, b, c, today); err != nil {
				mu.Lock()
				failed++
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			// Same kind as a manual sprint move, so the day-state replay reads
			// carries and hand moves uniformly. Best-effort, inside the worker.
			s.logEvent(ctx, b, c, board.EventSprint, c.SprintStart, today)
		}(c)
	}
	wg.Wait()
	// Seed the new sprint with fresh copies of the finished recurrent cards:
	// same title/description/team/zone/assignee at 0%, without the old notes.
	for _, c := range reseed {
		if err := s.reseedRecurrent(ctx, b, c, board.CreateInput{
			Title:       c.Title,
			Zone:        c.Zone,
			Day:         today,
			Start:       today,
			SprintStart: today,
			Team:        c.Team,
		}); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return rep, fmt.Errorf("carried over %d of %d cards, %d failed: %w",
			len(carry)-failed, len(carry), failed, firstErr)
	}
	return rep, nil
}

// reseedRecurrent creates the fresh copy of a finished recurrent card: the given
// create input plus the original's first assignee, the recurrent stage and the
// original's description (notes are deliberately not copied).
func (s *Service) reseedRecurrent(ctx context.Context, b board.Board, c board.Card, in board.CreateInput) error {
	if len(c.Assignees) > 0 {
		in.Assignee = c.Assignees[0]
	}
	created, err := s.backend.CreateCard(ctx, b, in)
	if err != nil {
		return err
	}
	s.logEvent(ctx, b, created, board.EventCreated, "", "")
	if err := s.backend.SetStage(ctx, b, created, board.StageRecurrent); err != nil {
		return err
	}
	if c.Recurrence != "" {
		if err := s.backend.SetRecurrence(ctx, b, created, c.Recurrence); err != nil {
			return err
		}
	}
	if desc := board.StripEventLines(c.Description); desc != "" {
		return s.backend.SetDescription(ctx, b, created, desc)
	}
	return nil
}

// SetRecurrence sets a recurrent card's reseed cycle: "" (every sprint, the
// default), "week" or "month". Only cards on the recurrent stage carry a
// cycle — the stage menu is the only path here, and applyStage sheds the
// cycle when the stage changes.
func (s *Service) SetRecurrence(ctx context.Context, boardID string, itemID, cycle string) error {
	if !board.ValidRecurrence(cycle) {
		return fmt.Errorf("%w: unknown recurrence %q (\"\" | week | month)", ErrInvalidStage, cycle)
	}
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if c.Recurrence == cycle {
		return nil
	}
	if cycle != "" && c.Stage != board.StageRecurrent {
		return fmt.Errorf("%w: a recurrence cycle needs the recurrent stage", ErrInvalidStage)
	}
	if err := s.backend.SetRecurrence(ctx, b, c, cycle); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventRecurrence, c.Recurrence, cycle)
	return nil
}

// hasNewerRecurrent reports whether the board holds a fresher copy of a
// resting weekly/monthly recurrent card: same team and title, still on the
// recurrent stage, bound to a later sprint. Its presence means a past
// carry-over already reseeded this card — the old one is history.
func hasNewerRecurrent(b board.Board, c board.Card) bool {
	for _, o := range b.Cards {
		if o.ItemID == c.ItemID {
			continue
		}
		if o.Team == c.Team && o.Title == c.Title &&
			o.Stage == board.StageRecurrent && o.SprintStart > c.SprintStart {
			return true
		}
	}
	return false
}

// setSprintStartRetry sets a card's Sprint Start, retrying a few times with
// backoff. Carry Over touches many cards at once, and a transient GitHub hiccup
// (secondary rate limit, 502, a token refreshed mid-flight) on one of them must
// not silently leave it behind. The write is idempotent, so retrying is safe.
func (s *Service) setSprintStartRetry(ctx context.Context, b board.Board, c board.Card, date string) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		if err = s.backend.SetSprintStart(ctx, b, c, date); err == nil {
			return nil
		}
	}
	return err
}

// --- Defer / dates / remove (the frontend's date logic, moved server-side) --

// Defer pushes a card's scheduled day N days ahead of today — or ahead of its
// already-deferred slot, so presses stack. An old card keeps its HISTORY: the
// sprint it was worked in stays, so its past sprint day still holds it. A card
// created today has no history to keep and relocates fully.
//
// An end date is never left behind the new start, whatever the card's age. A
// card due before it begins is overdue for ever: it came straight back to the
// week it had just been sent out of, which is what "+1 week" is meant to get
// it out of. It mirrors moveStart in Card.tsx + handleDefer in TeamBoard.tsx.
func (s *Service) Defer(ctx context.Context, boardID string, itemID string, days int) error {
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	today := board.TodayIso()
	base := today
	if c.StartDate != "" && c.StartDate > today {
		base = c.StartDate
	}
	target := board.AddDays(base, days)
	if err := s.bringBack(ctx, b, c); err != nil {
		return err
	}
	if err := s.backend.SetStart(ctx, b, c, target); err != nil {
		return err
	}
	// A slot's row is its start's week, so deferring one moves the row too —
	// otherwise the cache says one week and the card's own dates another,
	// and the next full load jumps it.
	if err := s.syncSlotWeek(ctx, b, c, target); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventDates,
		board.DateRange(c.StartDate, c.Day), board.DateRange(target, c.Day))
	// A card created today has no history to keep: its sprint moves with it.
	// A personal card has no sprint to relocate at all.
	if board.LocalDateIso(c.CreatedAt) == today && !board.IsPersonalDomain(c.Domain) {
		if err := s.backend.SetSprintStart(ctx, b, c, target); err != nil {
			return err
		}
	}
	if c.Day != "" && c.Day < target {
		return s.backend.SetDay(ctx, b, c, target)
	}
	return nil
}

// SetDates applies the calendar: an explicit start…end relocation. The card
// joins the sprint that was active on the start day (falling back to the start
// day itself when no tracked sprint covers it); empty values clear the dates.
// It mirrors handleSetDates in TeamBoard.tsx.
func (s *Service) SetDates(ctx context.Context, boardID string, itemID, start, end string) error {
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	sprint := ""
	if start != "" {
		// Moving a card to a FUTURE day parks it: no sprint covers that day
		// yet, and pinning it to today's sprint would keep it riding every
		// daily carry-over while it waits. Sprint-less is the state the
		// carry-over that reaches its day knows how to adopt.
		if start > board.TodayIso() {
			sprint = ""
		} else {
			sprint = board.ActiveSprint(b, c.Team, start)
			if sprint == "" {
				// The day is older than the team can reach: its sprints have
				// moved past it. Pinning the card to a sprint STARTING there
				// pins it to one that closed — the day grid draws the current
				// and the previous, the Me board gates on them, and a
				// carry-over moves only the closing sprint's own cards — so
				// the card was on no board at all, findable by id alone.
				// Three went that way in one working day on the production
				// board. The dates are the person's to choose; the sprint is
				// where the work stands, which is the team's current one.
				sprint = board.CurrentSprint(b, c.Team)
			}
			if sprint == "" {
				// A team with no sprint pointer at all: the day seeds it.
				sprint = start
			}
		}
	}
	// Re-dating a slot is planning, whatever state it is in: one never taken
	// into work stays out of the sprints, and one somebody is working on
	// keeps the sprint they are working in. Recomputing it from the new date
	// silently dropped a started slot off its team's day board — and its
	// subtasks with it.
	if c.Epic != "" {
		sprint = c.SprintStart
	}
	// A personal board has no sprints: planning there is dates alone, and
	// the card must not be pinned to the no-team group's sprint pointer or
	// to its own start day the way a team card is.
	if board.IsPersonalDomain(c.Domain) {
		sprint = ""
	}
	if err := s.bringBack(ctx, b, c); err != nil {
		return err
	}
	if err := s.backend.SetStart(ctx, b, c, start); err != nil {
		return err
	}
	if err := s.backend.SetSprintStart(ctx, b, c, sprint); err != nil {
		return err
	}
	if err := s.backend.SetDay(ctx, b, c, end); err != nil {
		return err
	}
	if err := s.syncSlotWeek(ctx, b, c, start); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventDates,
		board.DateRange(c.StartDate, c.Day), board.DateRange(start, end))
	s.logEvent(ctx, b, c, board.EventSprint, c.SprintStart, sprint)
	return s.syncChildrenSprint(ctx, b, c.ItemID, sprint)
}

// syncChildrenSprint drags a card's subtasks onto its (re)assigned sprint: a
// subtask rides its parent, so a parent moved to another sprint must not
// leave its children stranded in the old one.
func (s *Service) syncChildrenSprint(ctx context.Context, b board.Board, parentID, sprint string) error {
	for _, c := range board.Children(b, parentID) {
		if c.SprintStart == sprint {
			continue
		}
		if err := s.backend.SetSprintStart(ctx, b, c, sprint); err != nil {
			return err
		}
		s.logEvent(ctx, b, c, board.EventSprint, c.SprintStart, sprint)
	}
	return nil
}

// Remove is the smart × — one method, the backend decides the outcome. A
// card has two homes — the working area (a sprint and its days) and the
// weekly plan (a band and its week) — and each × empties one of them. It
// RemoveIntent is which of the two things an × means. The gesture used to
// decide for itself — a card with a week was ALWAYS handed back to that week,
// so taking one off the board took two presses, the second landing on a card
// that no longer looked like the one the person meant to remove. The board
// asks now (RemoveChoiceDialog offers what the card allows), so the request
// carries the answer.
type RemoveIntent string

const (
	// RemoveAuto is the gesture deciding for itself, as it always did: the
	// card leaves the working area for whatever home it has left, and is
	// deleted when it had none. It is the ZERO value, so a caller that does
	// not care — an agent, an embedder, the API with no intent in the body —
	// gets the old behaviour by saying nothing.
	RemoveAuto RemoveIntent = ""
	// Unassign empties the WORKING AREA and leaves the card where it still
	// belongs: its week, or its Project-board column. It destroys nothing.
	Unassign RemoveIntent = "unassign"
	// OffBoard takes the card away, with the subtasks that were pieces of it.
	OffBoard RemoveIntent = "off-board"
)

// mirrors handleGridDelete in TeamBoard.tsx: the card leaves the working area
// — assignee, sprint and dates cleared (a slot keeps its dates: they are its
// row) — and stays wherever else it is: the WEEK it is scheduled for, or its
// Project-board column. With neither it was nowhere else, and it is deleted
// (cascading its review card; subtasks are freed into standalone cards). What
// it carries changes nothing here — the UI asks first when there is work to
// lose.
func (s *Service) Remove(ctx context.Context, boardID string, itemID string, intent RemoveIntent) error {
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if err := removingSomebodyElsesCard(ctx, c); err != nil {
		return err
	}
	if board.IsPersonalDomain(c.Domain) {
		return s.removePersonal(ctx, b, c)
	}
	// OFF THE BOARD is taken at its word: the card goes even though its week
	// or its column would have kept it, and the subtasks that were pieces of
	// it go along. Only what this board put on itself, though — a slot and a
	// process turn belong to another board's plan (ErrNotYoursToDestroy).
	// A SUBTASK is not answered here: the × on one means "out of the group",
	// which the arm below still decides.
	if intent == OffBoard && c.Parent == "" {
		if hasColumn(c) || c.Task != "" {
			return fmt.Errorf("%w: %q", ErrNotYoursToDestroy, c.Title)
		}
		return s.deleteGroup(ctx, b, c)
	}
	// UNASSIGN needs somewhere to leave the card. With no week and no column
	// emptying the working area would leave it with no person, no dates and
	// no home at all — the state this rule exists to prevent — so the caller
	// is told to say OffBoard if that is what they meant.
	if intent == Unassign && c.Parent == "" && !hasColumn(c) && c.Week == "" {
		return fmt.Errorf("%w: %q", ErrNowhereToLeaveIt, c.Title)
	}
	// UNASSIGN destroys nothing, and it says so on the button. It is answered
	// here rather than left to the gesture below, which asks a different
	// question: the gesture hands a card back to its week only while the card
	// is IN the working area, because a card that is nothing but its week has
	// no second home for the × to hand it to — and so, having nowhere to be
	// put, it is deleted. That is the right answer to "×" and the wrong one
	// to "leave it in its week": a card dragged into a week ahead IS nothing
	// but its week (Place clears the dates and the sprint), so the safe
	// option deleted the very cards the Triage board makes.
	if intent == Unassign && c.Week != "" && !hasColumn(c) {
		if len(c.Assignees) > 0 {
			if err := s.backend.SetAssignee(ctx, b, c, ""); err != nil {
				return err
			}
			s.logEvent(ctx, b, c, board.EventAssignee, c.Assignees[0], "")
		}
		// A subtask is pulled out of its group first: standing under a parent
		// is a home of its own, and leaving it there would make the × look
		// like it had done nothing.
		if c.Parent != "" {
			if err := s.ungroupWith(ctx, b, c, false, false); err != nil {
				return err
			}
			c.Parent = ""
		}
		return s.leaveWorkingArea(ctx, b, c)
	}
	// A subtask has no sprint history of its own: it rides its parent, so
	// demoting it alone would split the family across two sprints — the very
	// thing syncChildrenSprint prevents everywhere else — and a subtask left
	// in an earlier sprint still renders under its parent, so the × would
	// look like it had done nothing. It is deleted — UNLESS it stands in a
	// COLUMN, which is a home of its own (G57). Then the × takes it OUT OF
	// THE GROUP and leaves it there: releasing it while still a subtask
	// would either break the person/sprint pair its parent owns (S9) or be
	// undone by the next carry-over, which takes every open child along.
	// Leaving the family is what makes the gesture mean something, and the
	// work stays planned in its column.
	if c.Parent != "" {
		if !hasColumn(c) {
			return s.deleteWithCascade(ctx, b, c)
		}
		// EVERY refusal fires before anything is written. Ungrouping strands
		// a column of the PARENT's repository — the gesture's own doing, so
		// the column is repaired rather than refused over — but whatever
		// ELSE the pull-out would strand (a follower's column, a process
		// tie) is asked FIRST, or a refusal lands after a write the commit
		// never carries and the cache keeps.
		if err := s.ungroupWith(ctx, b, c, false, false); err != nil {
			return err
		}
		// The card in hand, not a re-read: a staged write is invisible to a
		// bare gitstore until the scope flushes (only the server's cache
		// answers mid-request), so re-loading here would return the card
		// as it was for an embedder and as it is for the server — the same
		// call, two answers.
		c.Parent = ""
		if err := s.dropAStrandedColumn(ctx, b, c); err != nil {
			return err
		}
		if columnFollows(b, c) {
			return s.releaseToColumn(ctx, b, c)
		}
		// The column could not come along, so the card is an ordinary one
		// now and the × means for it what it means for any other: demote,
		// or delete when the working area was its only place. Releasing it
		// as if it still had a column left it with no sprint, no dates, no
		// band and no column — alive, and on no board anyone can open.
		// The card in hand again, for the same reason: a re-read answers
		// differently for an embedder than for the server.
		c.Epic, c.Project, c.Week = "", "", ""
		return s.removeFromGrid(ctx, b, c)
	}
	return s.removeFromGrid(ctx, b, c)
}

// removeFromGrid is the day grid's × on a card that stands on its own: the
// card leaves the working area, and where it goes is decided by what it has
// left. Split out of Remove so the former subtask above, whose column could
// not follow it out of the group, is answered by the SAME law rather than a
// second copy of it.
func (s *Service) removeFromGrid(ctx context.Context, b board.Board, c board.Card) error {
	// A card scheduled for a WEEK has the Triage board to go back to, so this
	// × takes it out of the working area and leaves it there — whatever it
	// carries. Only while it IS in the working area: a card that is nothing
	// but its week has no second home to be handed to, and an × that always
	// answered "back to your week" could never remove one at all. A PROCESS
	// TURN is the exception, and never runs out of homes: the week it was
	// filed into is its process's record of what that week was owed, and the
	// turn is how the board remembers it — so the × empties the working area
	// and, pressed again, does nothing. A slot's week is derived from its
	// start date rather than stored, so it is answered by the column arm
	// below instead.
	if c.Week != "" && !hasColumn(c) && (inWorkingArea(c) || c.Task != "") {
		if len(c.Assignees) > 0 {
			if err := s.backend.SetAssignee(ctx, b, c, ""); err != nil {
				return err
			}
			s.logEvent(ctx, b, c, board.EventAssignee, c.Assignees[0], "")
		}
		return s.leaveWorkingArea(ctx, b, c)
	}
	// A card has two homes — the working area (a sprint and its days) and the
	// weekly plan (a band and its week) — and this × empties the first one.
	// With a Project-board column to fall back on the card simply goes there;
	// with none, it was nowhere else, and removing it from the only place it
	// was is deletion.
	//
	// It used to be a DEMOTE for a card of the current sprint: the dates and
	// the sprint moved back one, and the card lived on in the sprint before
	// this one, out of today's way but still there to be found by stepping
	// back. That is not where it ended up. No live view reaches such a card —
	// not the day grid (its sprint is neither the current one nor the
	// previous), not the Me board (the sprint gate), and no carry-over ever
	// takes it, since a carry-over moves the CLOSING sprint's own cards — so
	// the board grew a pile of open work nobody could see: on the production
	// board, three hundred cards, thirty-six of them with progress on them.
	//
	// The demote existed to keep the card's history. The history is git now:
	// the day the card was worked on holds it whole, in the state it went in
	// (G60, LoadAsOfDay), so there is nothing left for the demote to save.
	//
	// Nothing is spared here on account of what it carries: work and
	// subtasks make a delete worth CONFIRMING, not worth turning into a
	// move somewhere nobody asked for — the UI asks before sending this
	// (deleteWarning in removal.ts).
	if !hasColumn(c) {
		return s.deleteGroup(ctx, b, c)
	}
	return s.releaseToColumn(ctx, b, c)
}

// deleteGroup is the grid ×'s delete: the card and the subtasks that were
// only ever a part of it. A plain delete FREES a card's subtasks — they keep
// their own lives where the parent stood, which is right when one card is
// deleted out of a group — but the × is the gesture for a piece of work that
// is over, and the pieces of that work go with it; the demote it replaces
// carried them along too. A subtask standing in a COLUMN of its own is the
// exception: it has a home the × does not empty, so it is freed into it
// (G57), exactly as a top-level card in a column is.
func (s *Service) deleteGroup(ctx context.Context, b board.Board, card board.Card) error {
	for _, c := range board.Children(b, card.ItemID) {
		if !hasColumn(c) {
			if err := s.deleteWithCascade(ctx, b, c); err != nil {
				return err
			}
			continue
		}
		if err := s.backend.SetParent(ctx, b, c, ""); err != nil {
			return err
		}
		c.Parent = ""
		if err := s.dropAStrandedColumn(ctx, b, c); err != nil {
			return err
		}
		if err := s.releaseToColumn(ctx, b, c); err != nil {
			return err
		}
	}
	return s.deleteWithCascade(ctx, b, card)
}

// A card is somewhere when a board shows it: the working area (a sprint or
// its dates, on the day grids), the weekly plan (a band), or the Project
// board (a column). Emptying its last one is what deletion means here.

// inWorkingArea reports whether the card is in the working area — in a
// sprint, or on the day grid by its dates.
func inWorkingArea(c board.Card) bool {
	return c.SprintStart != "" || c.StartDate != "" || c.Day != ""
}

// childrenLeaveWorkingArea takes a card's subtasks out of the working area
// with it: the sprint through syncChildrenSprint, and the dates that would
// otherwise keep them listed on a day the parent has left.
func (s *Service) childrenLeaveWorkingArea(ctx context.Context, b board.Board, parent board.Card) error {
	if err := s.syncChildrenSprint(ctx, b, parent.ItemID, ""); err != nil {
		return err
	}
	for _, k := range board.Children(b, parent.ItemID) {
		if k.StartDate == "" && k.Day == "" {
			continue
		}
		if k.StartDate != "" {
			if err := s.backend.SetStart(ctx, b, k, ""); err != nil {
				return err
			}
		}
		if k.Day != "" {
			if err := s.backend.SetDay(ctx, b, k, ""); err != nil {
				return err
			}
		}
		s.logEvent(ctx, b, k, board.EventDates, board.DateRange(k.StartDate, k.Day), "")
	}
	return nil
}

// hasColumn reports whether the card sits in a Project-board column. The
// column is the pair (project, epic) and the EPIC side is what puts the card
// in one: the Project board renders columns by epic and the weekly plan
// derives a slot's band by epic, so a card carrying only a project name is
// on no board — a label, not a home.
func hasColumn(c board.Card) bool { return c.Epic != "" }

// leaveWorkingArea takes a card out of the day grid: the sprint goes, and so
// do the dates that would keep the grid listing it (a card whose start is
// the viewed day shows there whatever its sprint says — which is how a card
// handed back to the plan stayed on the board as well, in two places at
// once). A Project-board slot keeps its dates: they are its row.
func (s *Service) leaveWorkingArea(ctx context.Context, b board.Board, c board.Card) error {
	if c.SprintStart != "" {
		if err := s.backend.SetSprintStart(ctx, b, c, ""); err != nil {
			return err
		}
		// The departure is RECORDED, the way a demote records its move: a
		// card that leaves today's board without a word in its own history is
		// a card nobody can account for afterwards (W6).
		s.logEvent(ctx, b, c, board.EventSprint, c.SprintStart, "")
	}
	// The subtasks ride with their parent: left in the sprint and on the days
	// their parent has left, they stand under a card no board shows — and a
	// stale date is what kept the parent itself in two places.
	if err := s.childrenLeaveWorkingArea(ctx, b, c); err != nil {
		return err
	}
	if hasColumn(c) {
		return nil // its dates are its row on the Project board
	}
	if c.StartDate == "" && c.Day == "" {
		return nil
	}
	if c.StartDate != "" {
		if err := s.backend.SetStart(ctx, b, c, ""); err != nil {
			return err
		}
	}
	if c.Day != "" {
		if err := s.backend.SetDay(ctx, b, c, ""); err != nil {
			return err
		}
	}
	s.logEvent(ctx, b, c, board.EventDates, board.DateRange(c.StartDate, c.Day), "")
	return nil
}

// removePersonal is the × on a personal board, which has no sprint to demote
// into: a worked-on card is left behind on yesterday's board — leftAt set on
// it and on its subtasks, which ride with it — and an untouched one, or one
// that started today, is deleted for real (mirrors personalRemovalKind in
// removal.ts; the UI asks first when there is progress to lose).
func (s *Service) removePersonal(ctx context.Context, b board.Board, c board.Card) error {
	today := board.TodayIso()
	if !board.PersonalLeaves(c, today) {
		return s.deleteWithCascade(ctx, b, c)
	}
	return s.setLeftAt(ctx, b, c, board.AddDays(today, -1))
}

// setLeftAt writes a personal card's left-behind day — "" brings it back —
// on the card and its subtasks, recording the move on each.
func (s *Service) setLeftAt(ctx context.Context, b board.Board, c board.Card, day string) error {
	for _, k := range append([]board.Card{c}, board.Children(b, c.ItemID)...) {
		if k.LeftAt == day {
			continue
		}
		if err := s.backend.SetLeftAt(ctx, b, k, day); err != nil {
			return err
		}
		s.logEvent(ctx, b, k, board.EventLeft, k.LeftAt, day)
	}
	return nil
}

// bringBack clears a left-behind personal card's leftAt when it is re-dated:
// the calendar and the defer put it on a day again, so it is on the board
// again. A no-op for every other card.
func (s *Service) bringBack(ctx context.Context, b board.Board, c board.Card) error {
	if !board.IsPersonalDomain(c.Domain) || c.LeftAt == "" {
		return nil
	}
	return s.setLeftAt(ctx, b, c, "")
}

// releaseToColumn gives a slot back: it loses its person and leaves the
// working area, and its Project-board column is where it still is. Only a
// card with a column reaches this — one without is deleted by Remove
// instead of being filed somewhere nobody put it.
func (s *Service) releaseToColumn(ctx context.Context, b board.Board, c board.Card) error {
	if len(c.Assignees) > 0 {
		if err := s.backend.SetAssignee(ctx, b, c, ""); err != nil {
			return err
		}
		s.logEvent(ctx, b, c, board.EventAssignee, c.Assignees[0], "")
	}
	return s.leaveWorkingArea(ctx, b, c)
}

// carryFollowers collects the subtasks that ride a carried parent into the
// new sprint: the OPEN ones. A completed subtask stays in the sprint it was
// finished in — the parent's derived bar still counts it (DerivedProgress
// scans ALL children), so the progress carries even though the done row
// stays behind on the old day.
func carryFollowers(b board.Board, carry []board.Card) []board.Card {
	followers := make([]board.Card, 0)
	for _, p := range carry {
		for _, c := range board.Children(b, p.ItemID) {
			if board.Complete(c.Stage, c.Progress) {
				continue
			}
			followers = append(followers, c)
		}
	}
	return followers
}

// LinkResolver resolves a GitHub issue/PR link to its live title and state.
// Backends that can talk to GitHub implement it; the service degrades to
// unresolved links otherwise.
type LinkResolver interface {
	ResolveIssueRef(ctx context.Context, link board.Link) (board.Link, error)
}

// CardLinks extracts every URL from a card's description — GitHub issue/PR
// references first, plain links after — and resolves the references to their
// titles when the backend can. A reference that fails to resolve is returned
// as-is rather than dropped.
func (s *Service) CardLinks(ctx context.Context, boardID string, itemID string) ([]board.Link, error) {
	_, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return nil, err
	}
	links := board.ExtractLinks(card.Description)
	resolver, ok := s.backend.(LinkResolver)
	if !ok {
		return links, nil
	}
	for i, link := range links {
		if !link.IsGitHubRef() {
			continue
		}
		if resolved, err := resolver.ResolveIssueRef(ctx, link); err == nil {
			links[i] = resolved
		}
	}
	return links, nil
}

// SetReviewOf sets or clears (reviewOf = "") the link marking a card as the
// review of another.
func (s *Service) SetReviewOf(ctx context.Context, boardID string, itemID, reviewOf string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	// The review link decides the card's repository before its project
	// (linked cards first, G14), so setting or clearing it is a re-file in
	// disguise — and a re-file that carries a mirrored card into another
	// repository leaves the mirrors naming columns of the one it left:
	// G15's forbidden state. Refused while mirrors stand, symmetric with
	// SetEpic: unmirror first.
	if len(card.Mirrors) > 0 {
		after := card
		after.ReviewOf = reviewOf
		// Through HomeDomain, which reads BOTH sides in the board's one
		// namespace. A resolver of its own answered "" for an unstamped
		// team and the primary's NAME for a card the store stamped — two
		// names for one repository inside a single comparison, so a link
		// that moved nothing looked like a move and was refused.
		if board.HomeDomain(b, after) != board.HomeDomain(b, card) {
			return fmt.Errorf("%w: the review link would move the card — unmirror it first", ErrCrossDomain)
		}
	}
	// The tie is pinned the same way the mirrors are: a link that re-files
	// the card into another repository would strand it.
	if err := refileGuard(b, card, func(a *board.Card) { a.ReviewOf = reviewOf }); err != nil {
		return err
	}
	return s.backend.SetReviewOf(ctx, b, card, reviewOf)
}

// cancelLinkedReview demotes or deletes the unfinished review card linked to an
// original that just left the review stage, mirroring the × logic; the demote
// also breaks the reviewOf link, or the original would keep showing "On review".
// A finished review card stays as a record of the work. It mirrors
// cancelLinkedReview in TeamBoard.tsx / MeBoard.tsx.
func (s *Service) cancelLinkedReview(ctx context.Context, b board.Board, original board.Card) error {
	review, ok := findReviewCard(b, original.ItemID)
	if !ok || board.Complete(review.Stage, review.Progress) {
		return nil
	}
	// The reviewer already put work into it (progress > 0): never auto-remove
	// that work. The card stays in place, link intact — the next carry-over
	// decides its fate (it only travels while the review is still required).
	if review.Progress > 0 {
		return nil
	}
	cur := board.CurrentSprint(b, review.Team)
	prev := board.PreviousSprint(b, review.Team)
	if review.SprintStart != "" && cur != "" && review.SprintStart == cur && prev != "" && prev < cur {
		if err := s.backend.SetStart(ctx, b, review, prev); err != nil {
			return err
		}
		if err := s.backend.SetSprintStart(ctx, b, review, prev); err != nil {
			return err
		}
		if err := s.backend.SetReviewOf(ctx, b, review, ""); err != nil {
			return err
		}
		if review.Day != "" && review.Day != prev {
			if err := s.backend.SetDay(ctx, b, review, prev); err != nil {
				return err
			}
		}
		s.logReviewCancelled(ctx, b, original, review)
		return nil
	}
	if err := s.backend.DeleteCard(ctx, b, review); err != nil {
		return err
	}
	s.logReviewCancelled(ctx, b, original, review)
	return nil
}

// logReviewCancelled records on the original that leaving the review stage
// cancelled its linked review card (the review card's own log dies with it
// when deleted, so the original carries the trace).
func (s *Service) logReviewCancelled(ctx context.Context, b board.Board, original, review board.Card) {
	reviewer := ""
	if len(review.Assignees) > 0 {
		reviewer = review.Assignees[0]
	}
	s.logEvent(ctx, b, original, board.EventReviewerRemoved, reviewer, "")
}

// --- Stage / progress ------------------------------------------------------

// SetStage moves a card to a stage (stage = "" clears it). It mirrors handleStage
// in TeamBoard.tsx: board.ApplyStage computes the resulting (stage, progress) and
// both are persisted (done fills 100%, review/locked knock a full card to 90%).
// Taking a card off review cancels its unfinished linked review card.
func (s *Service) SetStage(ctx context.Context, boardID string, itemID string, stage board.StageKey) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	// A stage the board has no such thing as is refused before anything is
	// written. Nothing checked the NAME, so a caller could file a card under a
	// stage that exists nowhere: it comes back wearing something the frontend
	// drops on the way in, and reads as ordinary work in progress — a green
	// bar, no clamp, and none of the rules the real stage carries.
	if _, known := board.Stages[stage]; !known && stage != board.StageNone {
		return fmt.Errorf("%w: no such stage %q", ErrInvalidStage, stage)
	}
	// A review card is auxiliary and one-off: it cannot be made recurrent.
	if stage == board.StageRecurrent && card.ReviewOf != "" {
		return fmt.Errorf("%w: a review card cannot be recurrent", ErrInvalidStage)
	}
	// REFUSE is the answer of the person carrying the work, and of nobody
	// else. The Me board is where the stage is offered, but an agent reaches
	// the same door, so the rule lives here.
	if stage == board.StageRefuse && !slices.Contains(card.Assignees, board.ActorFrom(ctx)) {
		return ErrNotYoursToRefuse
	}
	// Closing the parent is the human's final call - made only once every
	// subtask is done.
	if stage == board.StageDone && board.OpenChildren(b, card.ItemID) {
		return ErrOpenSubtasks
	}
	// Done on a RECURRENT card completes this iteration without shedding the
	// recurrence: progress fills to 100 (complete for carry-over/reseed) and
	// the recurrent marker stays for the next round.
	if stage == board.StageDone && card.Stage == board.StageRecurrent {
		if card.Progress != 100 {
			if err := s.backend.SetProgress(ctx, b, card, 100); err != nil {
				return err
			}
			s.logEvent(ctx, b, card, board.EventProgress,
				strconv.Itoa(card.Progress), "100")
		}
		if card.Parent != "" {
			child := card
			child.Progress = 100
			return s.syncParentProgress(ctx, b, card.Parent, &child, "")
		}
		return nil
	}
	if err := s.applyStage(ctx, b, card, stage); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventStage, string(card.Stage), string(stage))
	// A subtask's completion state feeds its parent's derived progress.
	if card.Parent != "" {
		child := card
		child.Stage, child.Progress = board.ApplyStage(stage, card.Progress)
		if err := s.syncParentProgress(ctx, b, card.Parent, &child, ""); err != nil {
			return err
		}
	}
	// A REVIEW card's own stage drives its original the same way its progress
	// does: marking it done passes the review, reopening it sends the original
	// back. Only the progress paths synced the link, so "mark as done" on the
	// review card left the original stuck on review — with nothing in its log
	// to say why (seen on the live board: review card done at 13:18, original
	// still on review).
	if card.ReviewOf != "" {
		_, newProgress := board.ApplyStage(stage, card.Progress)
		return s.syncReviewLink(ctx, b, card, newProgress)
	}
	// Leaving review cancels the unfinished linked review card.
	if card.Stage == board.StageReview && stage != board.StageReview {
		return s.cancelLinkedReview(ctx, b, card)
	}
	// Entering review re-review: a passed card put back on review reactivates
	// its completed review card for a fresh round (the reverse of the review
	// done → off-review forward sync).
	if stage == board.StageReview && card.Stage != board.StageReview {
		return s.reactivateReviewCard(ctx, b, card)
	}
	return nil
}

// reactivateReviewCard resets a completed linked review card for a new review
// round: its progress back to 0 (no longer done) and the round counter bumped.
// A no-op when the original has no linked review card, or the review is still
// in progress. It is called whenever an already-passed original is put back on
// review — from the stage menu or by re-sending it to the same reviewer.
func (s *Service) reactivateReviewCard(ctx context.Context, b board.Board, original board.Card) error {
	review, ok := findReviewCard(b, original.ItemID)
	if !ok || !board.Complete(review.Stage, review.Progress) {
		return nil
	}
	// Bring the review card into the original's current sprint and onto today,
	// so a re-review in a NEW sprint makes it appear there (it may have been
	// left behind in the closing sprint by carry-over).
	today := board.TodayIso()
	if review.SprintStart != original.SprintStart {
		if err := s.backend.SetSprintStart(ctx, b, review, original.SprintStart); err != nil {
			return err
		}
	}
	if review.StartDate != today {
		if err := s.backend.SetStart(ctx, b, review, today); err != nil {
			return err
		}
	}
	if review.Day != today {
		if err := s.backend.SetDay(ctx, b, review, today); err != nil {
			return err
		}
	}
	if err := s.backend.SetProgress(ctx, b, review, 0); err != nil {
		return err
	}
	round := review.ReviewRound
	if round < 1 {
		round = 1
	}
	if err := s.backend.SetReviewRound(ctx, b, review, round+1); err != nil {
		return err
	}
	// The original re-entering review reset this review card for a fresh
	// round — record it on the review card itself.
	s.logEvent(ctx, b, review, board.EventReviewRound,
		strconv.Itoa(round), strconv.Itoa(round+1))
	return nil
}

// applyStage persists a stage change and any coupled progress change.
// keepsItsMarker refuses to move a turn of a process off the recurrent stage,
// whichever door the change came through — the stage menu, "in progress", or
// being sent to review. Its task owns the repeat, and a turn that shed the
// marker would still be replaced next cycle while pretending to be a one-off.
// Done is the exception: that is what finishing a turn IS.
func (s *Service) keepsItsMarker(card board.Card, newStage board.StageKey) error {
	if card.Task == "" || newStage == board.StageRecurrent || newStage == board.StageDone {
		return nil
	}
	return fmt.Errorf("%w: this card is a turn of a process — its recurrence is the process's", ErrInvalidStage)
}

func (s *Service) applyStage(ctx context.Context, b board.Board, card board.Card, stage board.StageKey) error {
	newStage, newProgress := board.ApplyStage(stage, card.Progress)
	if err := s.keepsItsMarker(card, newStage); err != nil {
		return err
	}
	if err := s.backend.SetStage(ctx, b, card, newStage); err != nil {
		return err
	}
	// Leaving the recurrent stage sheds the cycle: a stale hidden "monthly"
	// marker on a now-ordinary card would resurrect it at some future
	// carry-over.
	if newStage != board.StageRecurrent && card.Recurrence != "" {
		if err := s.backend.SetRecurrence(ctx, b, card, ""); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventRecurrence, card.Recurrence, "")
	}
	if newProgress != card.Progress {
		if err := s.backend.SetProgress(ctx, b, card, newProgress); err != nil {
			return err
		}
		// The jump is recorded so Reopen can RESTORE it: undoing an
		// accidental done must give back the progress the card had, not an
		// invented number.
		s.logEvent(ctx, b, card, board.EventProgress,
			strconv.Itoa(card.Progress), strconv.Itoa(newProgress))
	}
	return nil
}

// Reopen undoes a done mark: the stage clears and the progress RETURNS to
// what the card had when done was set — read from its own activity log (the
// done write records the jump). A card with no recorded jump falls back to
// the in-progress nudge, the old behaviour.
func (s *Service) Reopen(ctx context.Context, boardID string, itemID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	// The write that took the card to 100 stored where it came from
	// (DoneFrom): that is the value owed back, and it does not depend on a
	// history that a horizon may have cut. A card without one (done before
	// the field existed, and the migration found no recorded jump to seed it
	// from) goes back to the implicit In Progress.
	restored := -1
	if card.DoneFrom > 0 {
		restored = card.DoneFrom
	}
	if restored < 0 {
		return s.SetInProgress(ctx, boardID, itemID)
	}
	newStage, _ := board.ApplyInProgress(card.Stage, card.Progress)
	if err := s.keepsItsMarker(card, newStage); err != nil {
		return err
	}
	if err := s.backend.SetStage(ctx, b, card, newStage); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventStage, string(card.Stage), "reopened")
	if restored != card.Progress {
		if err := s.backend.SetProgress(ctx, b, card, restored); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventProgress,
			strconv.Itoa(card.Progress), strconv.Itoa(restored))
	}
	return nil
}

// SetProgress sets a card's progress. It mirrors handleProgress in TeamBoard.tsx:
// board.ApplyProgress clamps the value and runs the done auto-link, both are
// persisted, and a review card's progress drives its original's review stage.
func (s *Service) SetProgress(ctx context.Context, boardID string, itemID string, raw int) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	// 100% completes a stageless card; the parent's final 100 waits for
	// every subtask to be done first.
	if raw >= 100 && board.OpenChildren(b, card.ItemID) {
		return ErrOpenSubtasks
	}
	newStage, newProgress := board.ApplyProgress(card.Stage, raw)
	if err := s.backend.SetProgress(ctx, b, card, newProgress); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventProgress,
		strconv.Itoa(card.Progress), strconv.Itoa(newProgress))
	if newStage != card.Stage {
		if err := s.backend.SetStage(ctx, b, card, newStage); err != nil {
			return err
		}
	}
	// A subtask's progress feeds its parent's derived progress.
	if card.Parent != "" {
		child := card
		child.Stage, child.Progress = newStage, newProgress
		if err := s.syncParentProgress(ctx, b, card.Parent, &child, ""); err != nil {
			return err
		}
	}
	return s.syncReviewLink(ctx, b, card, newProgress)
}

// SetInProgress moves a card to the implicit "In Progress" status (no stage,
// progress nudged into [10, 90]). It mirrors handleInProgress in TeamBoard.tsx
// via board.ApplyInProgress, keeping a review card's review-link in sync.
func (s *Service) SetInProgress(ctx context.Context, boardID string, itemID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	newStage, newProgress := board.ApplyInProgress(card.Stage, card.Progress)
	if err := s.keepsItsMarker(card, newStage); err != nil {
		return err
	}
	if err := s.backend.SetStage(ctx, b, card, newStage); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventStage, string(card.Stage), "in-progress")
	// In Progress leaves the review stage too: cancel the linked review card.
	if card.Stage == board.StageReview {
		if err := s.cancelLinkedReview(ctx, b, card); err != nil {
			return err
		}
	}
	if newProgress != card.Progress {
		if err := s.backend.SetProgress(ctx, b, card, newProgress); err != nil {
			return err
		}
	}
	// A subtask's state feeds its parent's derived progress (and reopens a
	// done parent — in-progress means the group has open work again).
	if card.Parent != "" {
		child := card
		child.Stage, child.Progress = newStage, newProgress
		if err := s.syncParentProgress(ctx, b, card.Parent, &child, ""); err != nil {
			return err
		}
	}
	if newProgress == card.Progress {
		return nil
	}
	return s.syncReviewLink(ctx, b, card, newProgress)
}

// SetZone sets a card's colour zone (zone = "" clears it).
func (s *Service) SetZone(ctx context.Context, boardID string, itemID string, zone board.ZoneKey) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if err := s.backend.SetZone(ctx, b, card, zone); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventZone, string(card.Zone), string(zone))
	return nil
}

// SetDay sets a card's scheduled day (day = "" clears it).
func (s *Service) SetDay(ctx context.Context, boardID string, itemID, day string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if err := s.backend.SetDay(ctx, b, card, day); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventDates,
		board.DateRange(card.StartDate, card.Day), board.DateRange(card.StartDate, day))
	return nil
}

// syncSlotWeek keeps a Project-board slot's row under its start date. The week
// is not a second thing to set: it IS the start's week, and a card whose dates
// moved while its week stayed behind sat in a row its own dates contradicted.
func (s *Service) syncSlotWeek(ctx context.Context, b board.Board, c board.Card, start string) error {
	if c.Epic == "" || start == "" {
		return nil
	}
	week := board.MondayOf(start)
	if week == c.Week {
		return nil
	}
	if err := s.backend.SetWeek(ctx, b, c, week); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventWeek, c.Week, week)
	return nil
}

// SetStart sets a card's start date (date = "" clears it).
func (s *Service) SetStart(ctx context.Context, boardID string, itemID, date string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if err := s.backend.SetStart(ctx, b, card, date); err != nil {
		return err
	}
	if err := s.syncSlotWeek(ctx, b, card, date); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventDates,
		board.DateRange(card.StartDate, card.Day), board.DateRange(date, card.Day))
	return nil
}

// SetSprintStart sets the start day of the sprint a card belongs to (date = ""
// clears it).
func (s *Service) SetSprintStart(ctx context.Context, boardID string, itemID, date string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if err := s.backend.SetSprintStart(ctx, b, card, date); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventSprint, card.SprintStart, date)
	return s.syncChildrenSprint(ctx, b, card.ItemID, date)
}

// SetWeek sets a card's plan week, a Monday (week = "" clears it).
// SetWeek moves a WEEKLY-PLAN card to another week. A Project-board slot is
// refused: its week comes from its start date, and accepting a second value
// here is exactly how the two came to disagree.
func (s *Service) SetWeek(ctx context.Context, boardID string, itemID, week string) error {
	if err := guardWeek(week); err != nil {
		return err
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if card.Epic != "" {
		// Re-asserting the week the slot already derives from its start date
		// is a harmless no-op — the SPA and API writers echo the visible week
		// back (ungrouping a slot subtask used to die here on the parent's
		// week). Only a CONFLICTING week is refused: accepting it is exactly
		// how a slot's week and dates came to disagree.
		derived := card.Week
		if card.StartDate != "" {
			derived = board.MondayOf(card.StartDate)
		}
		if week == derived {
			return nil
		}
		return fmt.Errorf("%w: a slot's week follows its start date — move the dates instead", ErrWeekDerived)
	}
	// A card is a subtask or a card of its own week, never both (G58) — and a
	// WEEK given to a standing subtask is how a person says "take it out of
	// the group and schedule it". A refusal here would leave them with no
	// gesture at all: grouping has already taken the card's week, and the ×
	// deletes it, so a card dropped under the wrong parent could not be
	// freed. Clearing a week is not that request and ungroups nothing.
	if week != "" && card.Parent != "" {
		if err := s.ungroup(ctx, b, card); err != nil {
			return err
		}
		card.Parent = ""
	}
	if err := s.backend.SetWeek(ctx, b, card, week); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventWeek, card.Week, week)
	return nil
}

// SetSprintState sets a team's sprint pointer directly (current/previous sprint
// start dates; "" clears them). team = "" is the no-team group. It backs the
// frontend's client-side Carry Over, which advances the pointer then re-dates the
// unfinished cards.
func (s *Service) SetSprintState(ctx context.Context, boardID string, team, current, previous string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	return s.backend.SetSprintState(ctx, b, team, current, previous)
}

// syncReviewLink, when card is a review card, drives its original's review stage
// from the review card's progress. It mirrors syncOriginalReview.
func (s *Service) syncReviewLink(ctx context.Context, b board.Board, card board.Card, reviewProgress int) error {
	if card.ReviewOf == "" {
		return nil
	}
	original, ok := findCard(b, card.ReviewOf)
	if !ok {
		return nil
	}
	var next board.StageKey
	switch {
	case reviewProgress == 100 && original.Stage == board.StageReview:
		next = board.StageNone
	case reviewProgress < 100 && original.Stage != board.StageReview:
		next = board.StageReview
	default:
		return nil
	}
	if err := s.backend.SetStage(ctx, b, original, next); err != nil {
		return err
	}
	reviewer := ""
	if len(card.Assignees) > 0 {
		reviewer = card.Assignees[0]
	}
	if next == board.StageNone {
		s.logEvent(ctx, b, original, board.EventReviewPassed, reviewer, "")
	} else {
		s.logEvent(ctx, b, original, board.EventStage, string(original.Stage), string(board.StageReview))
	}
	return nil
}

// --- Review linkage --------------------------------------------------------

// SendToReview creates a linked review card for a reviewer (in the original's
// team) and puts the original on the review stage, returning the review card.
// day = "" is today. zone places the review card explicitly — the Me board
// sends the reviewer's copy to their unplanned zone — while "" keeps the
// original's zone (the Team board's and MCP's behaviour). It mirrors
// handleSendToReview in TeamBoard.tsx / MeBoard.tsx.
func (s *Service) SendToReview(ctx context.Context, boardID string, itemID, reviewer, day string, zone board.ZoneKey) (board.Card, error) {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return board.Card{}, err
	}
	return s.sendToReview(ctx, b, card, reviewer, day, zone)
}

// sendToReview is the create-review-card half of SendToReview over a loaded board.
func (s *Service) sendToReview(ctx context.Context, b board.Board, card board.Card, reviewer, day string, zone board.ZoneKey) (board.Card, error) {
	if day == "" {
		day = board.TodayIso()
	}
	if zone == "" {
		zone = card.Zone
	}
	if zone == "" {
		zone = board.ZoneGray
	}
	// A review card belongs to the SAME sprint as the card it reviews, not
	// merely the team's current pointer — otherwise a card being reviewed in an
	// older, not-yet-carried sprint would get a review card in a different
	// sprint, and carry-over would split them.
	sprintStart := card.SprintStart
	if sprintStart == "" {
		sprintStart = board.CurrentSprint(b, card.Team)
	}
	if sprintStart == "" {
		sprintStart = day
	}
	created, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:       "review: " + card.Title,
		Zone:        zone,
		Day:         day,
		Start:       day,
		SprintStart: sprintStart,
		Assignee:    reviewer,
		Team:        card.Team,
		ReviewOf:    card.ItemID,
	})
	if err != nil {
		return board.Card{}, err
	}
	// The review card carries a copy of the original's description, so the
	// reviewer sees the same context (and its links). A one-time copy at
	// create — the reviewer may edit their own copy freely afterwards. Event
	// lines that leaked into the original's description are not context.
	if desc := board.StripEventLines(card.Description); desc != "" {
		if setErr := s.backend.SetDescription(ctx, b, created, desc); setErr == nil {
			created.Description = desc
		}
	}
	// Put the original on review (ApplyStage also drops a 100% card to 90%).
	if err := s.applyStage(ctx, b, card, board.StageReview); err != nil {
		return created, err
	}
	s.logEvent(ctx, b, created, board.EventCreated, "", "")
	s.logEvent(ctx, b, card, board.EventReviewSent, "", reviewer)
	return created, nil
}

// ReassignReviewer points a card's linked review card at another reviewer, or
// sends the card to review when it has none yet (day = "" is today). zone
// places a newly created review card like SendToReview's ("" = the original's
// zone); an existing review card is never moved. It mirrors
// handleSetReviewAssignee (non-null login) in TeamBoard.tsx.
func (s *Service) ReassignReviewer(ctx context.Context, boardID string, itemID, reviewer, day string, zone board.ZoneKey) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	reviewCard, ok := findReviewCard(b, card.ItemID)
	if !ok {
		_, err := s.sendToReview(ctx, b, card, reviewer, day, zone)
		return err
	}
	// Re-review: sending an already-passed card back to the SAME reviewer
	// reactivates their finished review card instead of orphaning it — the
	// reverse of "review done → off review". The original goes back on the
	// review stage (which clamps its progress off 100%), the review card's
	// progress resets to 0 for the fresh round, and its round counter ticks
	// up (the first review is round 1, implicit; the first re-review is 2).
	sameReviewer := len(reviewCard.Assignees) > 0 && reviewCard.Assignees[0] == reviewer
	if sameReviewer && board.Complete(reviewCard.Stage, reviewCard.Progress) {
		if err := s.applyStage(ctx, b, card, board.StageReview); err != nil {
			return err
		}
		if err := s.reactivateReviewCard(ctx, b, card); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventReviewSent, "", reviewer)
		return nil
	}
	// An untouched review card is simply handed to the new reviewer. One the
	// old reviewer already worked on (progress > 0) keeps their work: it is
	// released from the link (a standalone card now — the next carry-over
	// leaves it behind) and a fresh review card is created for the new
	// reviewer.
	if reviewCard.Progress == 0 {
		prev := ""
		if len(reviewCard.Assignees) > 0 {
			prev = reviewCard.Assignees[0]
		}
		if err := s.backend.SetAssignee(ctx, b, reviewCard, reviewer); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventReviewSent, prev, reviewer)
		return nil
	}
	if err := s.backend.SetReviewOf(ctx, b, reviewCard, ""); err != nil {
		return err
	}
	_, err = s.sendToReview(ctx, b, card, reviewer, day, zone)
	return err
}

// reviewStillRequired reports whether a review card's review is still on: its
// original is on the board, unfinished, on the review stage, and the review
// card still has a reviewer assigned.
func reviewStillRequired(b board.Board, review board.Card) bool {
	if len(review.Assignees) == 0 {
		return false
	}
	for _, c := range b.Cards {
		if c.ItemID == review.ReviewOf {
			return c.Stage == board.StageReview && !board.Complete(c.Stage, c.Progress)
		}
	}
	return false
}

// RemoveReviewer deletes a card's linked review card (a no-op when there is
// none). It mirrors handleSetReviewAssignee(null) in TeamBoard.tsx.
func (s *Service) RemoveReviewer(ctx context.Context, boardID string, itemID string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	reviewCard, ok := findReviewCard(b, itemID)
	if !ok {
		return nil
	}
	if err := s.backend.DeleteCard(ctx, b, reviewCard); err != nil {
		return err
	}
	if original, found := findCard(b, itemID); found {
		reviewer := ""
		if len(reviewCard.Assignees) > 0 {
			reviewer = reviewCard.Assignees[0]
		}
		s.logEvent(ctx, b, original, board.EventReviewerRemoved, reviewer, "")
	}
	return nil
}

// --- Card field actions ----------------------------------------------------

// SetAssignee replaces a card's assignee (login = "" unassigns). It mirrors
// handleSetAssignee in TeamBoard.tsx.
func (s *Service) SetAssignee(ctx context.Context, boardID string, itemID, login string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	// A subtask always belongs to its parent's PERSON, the way it always
	// belongs to the parent's team: a direct change follows the parent
	// instead of drifting away from it. A family that drifts apart lands on
	// two personal boards — the Me view admits a card when you own one of its
	// subtasks, so one stray child drags the parent and every sibling onto a
	// board they are not part of.
	if card.Parent != "" {
		if p, ok := findCard(b, card.Parent); ok {
			login = ""
			if len(p.Assignees) > 0 {
				login = p.Assignees[0]
			}
		}
	}
	if err := s.backend.SetAssignee(ctx, b, card, login); err != nil {
		return err
	}
	prev := ""
	if len(card.Assignees) > 0 {
		prev = card.Assignees[0]
	}
	if prev != login {
		s.logEvent(ctx, b, card, board.EventAssignee, prev, login)
	}
	// Re-assigning a parent hands its whole family over.
	return s.syncChildrenAssignee(ctx, b, card, login)
}

// syncChildrenAssignee puts every subtask on the person its parent is on. It
// is a no-op for a card without subtasks.
func (s *Service) syncChildrenAssignee(ctx context.Context, b board.Board, parent board.Card, login string) error {
	if parent.Parent != "" {
		return nil // a subtask has no subtasks of its own
	}
	for _, c := range board.Children(b, parent.ItemID) {
		cur := ""
		if len(c.Assignees) > 0 {
			cur = c.Assignees[0]
		}
		if cur == login {
			continue
		}
		if err := s.backend.SetAssignee(ctx, b, c, login); err != nil {
			return err
		}
		s.logEvent(ctx, b, c, board.EventAssignee, cur, login)
	}
	return nil
}

// SetTeam moves a card to a team and joins that team's current sprint (team = ""
// is the no-team group, day = "" is today). It mirrors handleSetTeam in
// TeamBoard.tsx.
func (s *Service) SetTeam(ctx context.Context, boardID string, itemID, team, day string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	// A subtask always belongs to its parent's team: a direct change follows
	// the parent instead of drifting away from it.
	if card.Parent != "" {
		if p, ok := findCard(b, card.Parent); ok {
			team = p.Team
		}
	}
	if day == "" {
		day = board.TodayIso()
	}
	if err := guardRoster(b, team, card.Project); err != nil {
		return err
	}
	// The everyday door for a tied card to slip repositories: a recurrent
	// card without a project follows its TEAM, so re-teaming it re-files
	// the card and would strand the tie. Refused before anything is
	// declared or written.
	if err := refileGuard(b, card, func(a *board.Card) { a.Team = team }); err != nil {
		return err
	}
	// A team the board does not declare is declared by the assignment: over
	// the API and MCP a name the roster lacks would otherwise sit on the card
	// alone — on no roster, in no column.
	if err := s.declareTeam(ctx, b, team); err != nil {
		return err
	}
	sprintStart := board.CurrentSprint(b, team)
	if sprintStart == "" {
		sprintStart = day
	}
	// An epic card that is not in work yet stays plan-level: handing it to a
	// team files it into that team's WEEKLY plan (band + week do that), not
	// into today's sprint — joining the sprint would smear its multi-week
	// span across the team's day grid.
	if card.Epic != "" && card.SprintStart == "" {
		sprintStart = ""
	}
	if err := s.setTeamOne(ctx, b, card, team, sprintStart); err != nil {
		return err
	}
	// The team travels with the whole group: subtasks follow their parent.
	for _, c := range board.Children(b, card.ItemID) {
		if err := s.setTeamOne(ctx, b, c, team, sprintStart); err != nil {
			return err
		}
	}
	return nil
}

// setTeamOne moves a single card to a team + sprint, logging what changed.
// declareTeam puts a team the board does not list yet on its roster — a
// sprint pointer with no sprint — so no card ever names a team the board
// lacks. A team already declared keeps the pointer it has; the no-team
// group needs no declaring.
func (s *Service) declareTeam(ctx context.Context, b board.Board, team string) error {
	if team == "" {
		return nil
	}
	if _, known := b.SprintStates[team]; known {
		return nil
	}
	return s.backend.SetSprintState(ctx, b, team, "", "")
}

func (s *Service) setTeamOne(ctx context.Context, b board.Board, card board.Card, team, sprintStart string) error {
	if card.Team != team {
		if err := s.backend.SetTeam(ctx, b, card, team); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventTeam, card.Team, team)
	}
	if sprintStart != card.SprintStart {
		if err := s.backend.SetSprintStart(ctx, b, card, sprintStart); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventSprint, card.SprintStart, sprintStart)
	}
	return nil
}

// Rename changes a card's title. It mirrors handleRename in TeamBoard.tsx.
func (s *Service) Rename(ctx context.Context, boardID string, itemID, title string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	return s.backend.RenameCard(ctx, b, card, title)
}

// SetDescription replaces a card's free-form description (the body text above
// the note log).
// SetDescription sets a card's description. The description live-syncs with
// the linked counterpart — editing the original updates its review card and
// vice versa, so both always show the same context. Notes stay per-card.
func (s *Service) SetDescription(ctx context.Context, boardID string, itemID, description string) error {
	// Machine event-log lines are never legitimate description prose; strip
	// them so a copied-back visible text cannot bake the log into the body.
	description = board.StripEventLines(description)
	if utf8.RuneCountInString(description) > MaxDescriptionLen {
		return ErrDescriptionTooLong
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if err := s.backend.SetDescription(ctx, b, card, description); err != nil {
		return err
	}
	if other, ok := reviewCounterpart(b, card); ok {
		return s.backend.SetDescription(ctx, b, other, description)
	}
	return nil
}

// reviewCounterpart finds the card linked to this one through the review
// relation: the original for a review card, or the review card of an original.
func reviewCounterpart(b board.Board, card board.Card) (board.Card, bool) {
	for _, c := range b.Cards {
		if card.ReviewOf != "" && c.ItemID == card.ReviewOf {
			return c, true
		}
		if card.ReviewOf == "" && c.ReviewOf == card.ItemID {
			return c, true
		}
	}
	return board.Card{}, false
}

// findNote resolves a note on a card by its id.
func findNote(card board.Card, noteID string) (board.Note, bool) {
	for _, n := range card.Notes {
		if n.ID == noteID {
			return n, true
		}
	}
	return board.Note{}, false
}

// EditNote rewrites one of a card's work notes (ErrNoteNotFound when the note id
// is not on the card).
func (s *Service) EditNote(ctx context.Context, boardID string, itemID, noteID, text string) error {
	if utf8.RuneCountInString(text) > MaxNoteLen {
		return ErrNoteTooLong
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	note, ok := findNote(card, noteID)
	if !ok {
		return fmt.Errorf("%w: %s on %s", ErrNoteNotFound, noteID, itemID)
	}
	return s.backend.EditNote(ctx, b, card, note, text)
}

// DeleteNote removes one of a card's work notes (ErrNoteNotFound when the note
// id is not on the card).
func (s *Service) DeleteNote(ctx context.Context, boardID string, itemID, noteID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	note, ok := findNote(card, noteID)
	if !ok {
		return fmt.Errorf("%w: %s on %s", ErrNoteNotFound, noteID, itemID)
	}
	return s.backend.DeleteNote(ctx, b, card, note)
}

// AddNote appends a work note to a card.
func (s *Service) AddNote(ctx context.Context, boardID string, itemID, text string) error {
	if utf8.RuneCountInString(text) > MaxNoteLen {
		return ErrNoteTooLong
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	return s.backend.AddNote(ctx, b, card, text)
}

// MoveCard reorders a card to sit after afterID ("" = top of the board). It
// mirrors the moveCard calls behind drag-and-drop in TeamBoard.tsx.
func (s *Service) MoveCard(ctx context.Context, boardID string, itemID, afterID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	return s.backend.MoveCard(ctx, b, card, afterID)
}

// MoveCardBefore reorders a card to sit right before beforeID. Clients that
// render a filtered slice of the board (a weekly-plan band) cannot name the
// global predecessor for "move to the top of my group" — but the server knows
// the full order, so it resolves the card just before beforeID (skipping the
// moved card itself) and anchors there ("" = top of the board).
func (s *Service) MoveCardBefore(ctx context.Context, boardID string, itemID, beforeID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	afterID := ""
	for _, c := range b.Cards {
		if c.ItemID == beforeID {
			break
		}
		if c.ItemID != itemID {
			afterID = c.ItemID
		}
	}
	return s.backend.MoveCard(ctx, b, card, afterID)
}

// DeleteCard deletes a card, cascading to its linked review card. It mirrors
// handleDelete in TeamBoard.tsx (deleting a reviewed card removes both).
func (s *Service) DeleteCard(ctx context.Context, boardID string, itemID string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if err := removingSomebodyElsesCard(ctx, card); err != nil {
		return err
	}
	return s.deleteWithCascade(ctx, b, card)
}

// planningYourOwnWork refuses a create that files work for the ACTOR
// THEMSELVES into a planned zone. The Me board offers its add form in the
// unplanned zone alone, and the rule lives here because an agent reaches the
// same door (web/src/meboard.ts is the mirror).
//
// Untouched: a create for somebody else (the lead planning the team's week),
// and one whose card is placed by the thing it belongs to — a column, a
// parent, a review or a process turn — whose zone nobody chose here.
func planningYourOwnWork(ctx context.Context, args CreateCardArgs) error {
	actor := board.ActorFrom(ctx)
	if actor == "" || args.Assignee != actor {
		return nil
	}
	if args.Epic != "" || args.Parent != "" || args.ReviewOf != "" {
		return nil
	}
	if args.Zone == "" || args.Zone == board.ZoneYellow {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrNotYoursToPlan, args.Zone)
}

// removingSomebodyElsesCard refuses an × on a card the actor is CARRYING but
// did not create: work planned for them is not theirs to take off the board,
// and their answer to it is the refused stage. A card on somebody else is
// the lead's to remove, and a SUBTASK is a piece of its parent rather than
// work assigned to anyone.
func removingSomebodyElsesCard(ctx context.Context, card board.Card) error {
	actor := board.ActorFrom(ctx)
	if actor == "" || card.Parent != "" || card.Author == "" || card.Author == actor {
		return nil
	}
	if !slices.Contains(card.Assignees, actor) {
		return nil
	}
	return fmt.Errorf("%w: %q created it", ErrNotYoursToRemove, card.Author)
}

// deleteWithCascade deletes a card and any review card linked to it. The
// card's subtasks are RELEASED, not deleted: they are work items in their own
// right, so they return to the board as standalone cards (keeping their team,
// sprint and dates) instead of vanishing as orphans of a gone parent.
func (s *Service) deleteWithCascade(ctx context.Context, b board.Board, card board.Card) error {
	if reviewCard, ok := findReviewCard(b, card.ItemID); ok {
		// The review card's OWN subtasks are freed first: deleting it out
		// from under them left a parent id pointing at a card that is gone
		// — a group nobody can see and nothing can dissolve.
		if err := s.freeChildren(ctx, b, reviewCard); err != nil {
			return err
		}
		if err := s.backend.DeleteCard(ctx, b, reviewCard); err != nil {
			return err
		}
	}
	if err := s.freeChildren(ctx, b, card); err != nil {
		return err
	}
	return s.backend.DeleteCard(ctx, b, card)
}

// freeChildren releases a card's subtasks into standalone cards before the
// card itself goes: they keep their own lives, their columns when those
// still name the repository that holds them, and the parent's person when
// they had none — so a freed child lands where its parent stood instead of
// falling into Unassigned.
func (s *Service) freeChildren(ctx context.Context, b board.Board, card board.Card) error {
	anchor := card.ItemID
	for _, c := range board.Children(b, card.ItemID) {
		if err := s.backend.SetParent(ctx, b, c, ""); err != nil {
			return err
		}
		// The child keeps the one column it carries (G57) — but only while
		// that column still names the repository that holds it. A parent
		// whose own domain came from a LINK (a review card takes its
		// original's repository, G14) drops its children into the primary
		// when it goes, and a column left behind there is the state every
		// door refuses, reached by the one release that cannot ask the
		// guard: its parent is being deleted.
		if err := s.dropAStrandedColumn(ctx, b, c); err != nil {
			return err
		}
		// An unassigned subtask takes the parent's person, so on the Team
		// grid it surfaces in the cell the parent stood in, not in Unassigned.
		if len(c.Assignees) == 0 && len(card.Assignees) > 0 {
			if err := s.backend.SetAssignee(ctx, b, c, card.Assignees[0]); err != nil {
				return err
			}
			s.logEvent(ctx, b, c, board.EventAssignee, "", card.Assignees[0])
		}
		// Slide the freed card into the parent's slot (a subtask grouped by
		// drag kept its old project position, which may be anywhere); chained
		// anchors keep the children in their nested order. Best-effort: a
		// misplaced row beats a failed delete.
		if err := s.backend.MoveCard(ctx, b, c, anchor); err == nil {
			anchor = c.ItemID
		}
		s.logEvent(ctx, b, c, board.EventParent, card.Title, "")
	}
	return nil
}

// --- Weekly plan -----------------------------------------------------------

// groupOrUndo finishes a create by grouping the new card, and undoes the
// create when it cannot: a half-grouped twin left standing is a stray
// top-level card in somebody's column. If the undo itself fails, both
// reasons travel — a card nobody was told about is the worse outcome, and
// one of these two paths used to swallow the second error and hand back
// the card it had just deleted.
func (s *Service) groupOrUndo(ctx context.Context, b board.Board, card board.Card, parent string) error {
	// The card in hand, never re-read: it was written a moment ago, and
	// inside a scope that write is staged — invisible to a bare store until
	// the scope flushes. A re-read there fails and takes the undo with it,
	// deleting the card the caller asked for (setParentOf).
	if err := s.setParentOf(ctx, b, card, parent); err != nil {
		if derr := s.deleteWithCascade(ctx, b, card); derr != nil {
			return errors.Join(err, derr)
		}
		return err
	}
	return nil
}

// linksArePossible answers, before anything is written, whether the card
// being created may be born as asked. A named PARENT must exist and may
// not be a subtask itself; and since a LINK decides which repository holds
// a card — reviewOf first, then parent, then the project (G14) — the
// repository the new card lands in must be able to hold the column asked
// for (G57). Refusing after the create instead leaves a stray ADDED
// broadcast and a created event for a card that never was, which is why
// the probe models every link the request carries: reading only the parent
// answered for a different card than the one about to be written.
func linksArePossible(b board.Board, args CreateCardArgs) error {
	if args.Parent != "" {
		p, ok := findCard(b, args.Parent)
		if !ok || p.Title == board.SprintStateTitle {
			return ErrParentNotFound
		}
		if p.Parent != "" {
			return ErrSubtaskDepth
		}
		if args.Week != "" {
			return ErrSubtaskWeek
		}
	}
	// A COLUMN is what G57 is about, so a request that names one is asked
	// even when it names no link: the column's repository must be the one
	// that will hold the card. Only a request with neither has nothing to
	// check — there the project decides, and it decides for itself.
	if args.Epic == "" && args.Parent == "" && args.ReviewOf == "" {
		return nil
	}
	probe := board.Card{
		Parent: args.Parent, ReviewOf: args.ReviewOf, Team: args.Team,
		Project: args.Project, Epic: args.Epic,
	}
	return refileGuard(b, probe, func(*board.Card) {})
}

// columnFollows reports whether a card's column can come with it out of a
// group. Judged on the card WITHOUT its parent: every caller is releasing
// it — a deleted parent frees its children, the grid's × takes a card out
// of its group — so the question is which repository will hold the card
// once the link is gone. An UNKNOWN column is left alone: no roster
// declares it, so nothing says it belongs to another repository.
//
// The grid's × asks this to know what the card has left; dropAStrandedColumn
// acts on the same answer. One function, or the two drift and the × decides
// a card is columnless while its column is still written on it.
func columnFollows(b board.Board, c board.Card) bool {
	if c.Epic == "" {
		return true
	}
	after := c
	after.Parent = ""
	cd, known := board.ColumnDomain(b, c.Project, c.Epic)
	return !known || cd == board.HomeDomain(b, after)
}

// dropAStrandedColumn takes a card's column away when the card no longer
// lives in that column's repository — the only correction of its kind,
// for the only re-files that cannot be refused instead (a parent being
// deleted releases its children whatever else is true; the grid's × on a
// subtask strands the column by the very gesture it answers). Everywhere
// else this state is prevented; here it is repaired, and recorded.
func (s *Service) dropAStrandedColumn(ctx context.Context, b board.Board, c board.Card) error {
	if columnFollows(b, c) {
		return nil
	}
	was := c.Project + " / " + c.Epic
	if err := s.backend.SetEpic(ctx, b, c, ""); err != nil {
		return err
	}
	if c.Project != "" {
		if err := s.backend.SetProject(ctx, b, c, ""); err != nil {
			return err
		}
	}
	// The ROW goes with the column: a slot's week is derived from its start
	// date (board.NewBoard), so a card that keeps it after losing the column
	// carries a week nobody placed it in — and the × then reads that week as
	// a home worth sparing the card for. RemoveFromProject says the same for
	// the same reason.
	if c.Week != "" {
		if err := s.backend.SetWeek(ctx, b, c, ""); err != nil {
			return err
		}
	}
	s.logEvent(ctx, b, c, board.EventEpic, was, "")
	return nil
}
