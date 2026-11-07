package gitstore

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// MultiBackend is boardservice.Backend over a board of several domains.
// Reads merge every domain; a write goes to the domain the card belongs to
// (board.DomainOf); a write that changes where a card belongs is a move —
// create in the new domain, then delete in the old, same id, one action.

// MultiBackend implements boardservice.Backend.
type MultiBackend struct {
	domains  []Domain
	backends map[string]*Backend
	now      func() time.Time

	// issues is what the last merge had to resolve — duplicate roster names
	// and torn-move ghosts — for health to report; users are the primary's
	// roster entries for people.
	mu      sync.Mutex
	aliases []Alias
	ghosts  []Ghost
	users   []User
}

// domainList is a copy of the domains, primary first.
func (mb *MultiBackend) domainList() []Domain {
	return append([]Domain(nil), mb.domains...)
}

// Domains lists the board's domains, primary first.
func (mb *MultiBackend) Domains() []Domain { return mb.domainList() }

// Users are the primary's users/<login>.yaml entries as of the last load —
// nil before any.
func (mb *MultiBackend) Users() []User {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return append([]User(nil), mb.users...)
}

// ErrUnknownDomain names a domain that is not part of the board.
var ErrUnknownDomain = fmt.Errorf("gitstore: unknown domain")

// NewMultiBackend builds the backend; domains[0] is the primary.
func NewMultiBackend(domains []Domain, opts BackendOptions) *MultiBackend {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	mb := &MultiBackend{domains: domains, backends: map[string]*Backend{}, now: opts.Now}
	for _, d := range domains {
		mb.backends[d.Name] = NewBackend(d.Repo, opts)
	}
	return mb
}

func (mb *MultiBackend) primary() string { return mb.domainList()[0].Name }

// backend returns the domain's backend; "" is the primary.
func (mb *MultiBackend) backend(domain string) (*Backend, error) {
	if domain == "" {
		domain = mb.primary()
	}
	b, ok := mb.backends[domain]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownDomain, domain)
	}
	return b, nil
}

func (mb *MultiBackend) snapshot() (Snapshot, error) {
	s, err := LoadAll(mb.domainList())
	if err != nil {
		return s, err
	}
	mb.mu.Lock()
	mb.aliases, mb.ghosts, mb.users = s.Aliases, s.Ghosts, s.Users
	mb.mu.Unlock()
	return s, nil
}

// Issues reports what the last load had to resolve: duplicate roster names
// (G13) and torn-move ghosts (G22). Nil before any load.
func (mb *MultiBackend) Issues() ([]Alias, []Ghost) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return append([]Alias(nil), mb.aliases...), append([]Ghost(nil), mb.ghosts...)
}

// LoadBoard merges every domain into the board the service expects. Inside
// a scope each domain is read as the action has left it, staged writes
// included (loadOverlaid), so the doors one action goes through in turn
// agree on the card.
func (mb *MultiBackend) LoadBoard(ctx context.Context, boardID string) (board.Board, error) {
	var staged func(*Repo) map[string][]byte
	if scopeOf(ctx) != nil {
		staged = func(r *Repo) map[string][]byte { return stagedIn(ctx, r) }
	}
	s, err := loadAllOverlaid(mb.domainList(), staged)
	if err != nil {
		return board.Board{}, err
	}
	mb.mu.Lock()
	mb.aliases, mb.ghosts, mb.users = s.Aliases, s.Ghosts, s.Users
	mb.mu.Unlock()
	// The board's own repository, by NAME, and before the assembly runs:
	// the snapshot stamps every entry with its domain's name, the primary
	// included, and the rules that read the board (HomeDomain, ColumnDomain)
	// ask "the same repository?" in that one namespace — setting the name
	// on the finished board would answer them in the wrong namespace.
	bd := boardFromSnapshotIn(mb.primary(), s)
	bd.Board = boardID
	return bd, nil
}

// LoadBoardAsOf is LoadBoard at a past moment: the board every domain had
// when that day ended. ok is false when the clone's history does not reach
// back that far — the caller deepens and asks again, or tells the reader the
// history is cut.
func (mb *MultiBackend) LoadBoardAsOf(_ context.Context, boardID string, at time.Time) (board.Board, bool, error) {
	s, ok, err := LoadAllAsOf(mb.domainList(), at)
	if err != nil || !ok {
		return board.Board{}, ok, err
	}
	bd := boardFromSnapshotIn(mb.primary(), s)
	bd.Board = boardID
	return bd, true, nil
}

// LoadBoardOfDay is LoadBoardAsOf for a DAY: the board every domain ended it
// with, plus what the day itself removed (boardservice.DayReader).
func (mb *MultiBackend) LoadBoardOfDay(_ context.Context, boardID string, from, to time.Time) (board.Board, bool, error) {
	s, ok, err := LoadAllAsOfDay(mb.domainList(), from, to)
	if err != nil || !ok {
		return board.Board{}, ok, err
	}
	bd := boardFromSnapshotIn(mb.primary(), s)
	bd.Board = boardID
	return bd, true, nil
}

// LoadCards reads cards by id from whichever domain holds them.
func (mb *MultiBackend) LoadCards(ctx context.Context, bd board.Board, ids []string) ([]board.Card, error) {
	var out []board.Card
	for _, d := range mb.domainList() {
		got, err := mb.backends[d.Name].LoadCards(ctx, bd, ids)
		if err != nil {
			return nil, err
		}
		for i := range got {
			got[i].Domain = d.Name
		}
		out = append(out, got...)
	}
	// Keep the asked-for order; a card caught mid-move (G22) is the copy
	// whose movedFrom names the other domain.
	byID := map[string]board.Card{}
	for _, c := range out {
		prev, seen := byID[c.ItemID]
		if !seen || c.MovedFrom == prev.Domain {
			byID[c.ItemID] = c
		}
	}
	ordered := make([]board.Card, 0, len(byID))
	for _, id := range ids {
		c, ok := byID[id]
		if !ok {
			continue
		}
		// Each repository dropped a project-card parent it holds itself;
		// one kept in ANOTHER repository is dropped here (H15).
		for _, d := range mb.domainList() {
			if c.Parent != "" && mb.backends[d.Name].holdsProjectCard(ctx, c.Parent) {
				c.Parent = ""
			}
		}
		ordered = append(ordered, c)
	}
	return ordered, nil
}

// ---- resolving domains -----------------------------------------------------------------

// resolver answers DomainOf's questions from a merged snapshot.
type resolver struct {
	cards, teams map[string]string
	stubs        map[string]string // roster id → domain
	// planning is the goals, initiatives and epics: the parents a link to
	// does not place the child (board.PlanningResolver, H6).
	planning map[string]bool
}

func newResolver(s Snapshot) resolver {
	r := resolver{cards: map[string]string{}, teams: map[string]string{},
		stubs: map[string]string{}, planning: map[string]bool{}}
	for _, c := range s.Cards {
		r.cards[c.ItemID] = c.Domain
		if board.IsPlanning(c) {
			r.planning[c.ItemID] = true
		}
	}
	for _, t := range s.Teams {
		r.teams[t.Name] = t.Domain
		r.stubs[t.ID] = t.Domain
	}
	for _, a := range s.Aliases {
		r.stubs[a.ID] = a.Domain
	}
	return r
}

func (r resolver) CardDomain(id string) (string, bool)   { d, ok := r.cards[id]; return d, ok }
func (r resolver) CardIsPlanning(id string) bool         { return r.planning[id] }
func (r resolver) TeamDomain(name string) (string, bool) { d, ok := r.teams[name]; return d, ok }

// domainOf is where a card or stub lives now. Inside an action that already
// moved the card, "now" is where the action staged it — the card the caller
// holds still says where it was.
func (mb *MultiBackend) domainOf(ctx context.Context, r resolver, c board.Card) string {
	if sc := scopeOf(ctx); sc != nil && !isStub(c) {
		if p, err := CardPath(c.ItemID); err == nil {
			sc.mu.Lock()
			staged := ""
			for _, d := range mb.domainList() {
				if part, ok := sc.parts[d.Repo]; ok {
					if data, ok := part.staged[p]; ok && data != nil {
						staged = d.Name
					}
				}
			}
			sc.mu.Unlock()
			if staged != "" {
				return staged
			}
		}
	}
	if c.Domain != "" {
		return c.Domain
	}
	if isStub(c) {
		if d, ok := r.stubs[c.ItemID]; ok {
			return d
		}
		if d, ok := r.cards[c.ItemID]; ok {
			return d
		}
		return mb.primary()
	}
	if d, ok := r.cards[c.ItemID]; ok {
		return d
	}
	return mb.primary()
}

// homeOf is where a card BELONGS by the rule — where a write must put it.
func (mb *MultiBackend) homeOf(r resolver, c board.Card) string {
	if d := board.DomainOf(c, r); d != "" {
		return d
	}
	return mb.primary()
}

// ---- creating -----------------------------------------------------------------------------

// CreateCard places a new card by the inheritance rule, a roster stub where
// the caller asks (default the primary).
func (mb *MultiBackend) CreateCard(ctx context.Context, bd board.Board, in board.CreateInput) (board.Card, error) {
	s, err := mb.snapshot()
	if err != nil {
		return board.Card{}, err
	}
	r := newResolver(s)
	// A declaration takes a name no domain has yet.
	if in.Title == board.SprintStateTitle {
		if err := nameFree(r, "team", in.Team); err != nil {
			return board.Card{}, err
		}
	}
	// The caller's choice — the input's, else the context's (board.WithDomain)
	// — counts only for what has no home by rule: a team.
	choice := in.Domain
	if choice == "" {
		choice = board.DomainFrom(ctx)
	}
	target := choice
	if in.Title != board.SprintStateTitle {
		probe := cardFromInput(in, in.ItemID, "", "")
		target = mb.homeOf(r, probe)
	}
	be, err := mb.backend(target)
	if err != nil {
		return board.Card{}, err
	}
	c, err := be.CreateCard(ctx, bd, in)
	if err != nil {
		return c, err
	}
	if target == "" {
		target = mb.primary()
	}
	c.Domain = target
	return c, nil
}

// ---- routing -----------------------------------------------------------------------------

// route returns the backend of the domain the card lives in.
func (mb *MultiBackend) route(ctx context.Context, c board.Card) (*Backend, error) {
	s, err := mb.snapshot()
	if err != nil {
		return nil, err
	}
	return mb.backend(mb.domainOf(ctx, newResolver(s), c))
}

// refile applies a change that may alter where the card belongs. If the
// home stays, the card's own domain backend takes the write; if it moves,
// the card is written into the new domain first — with movedFrom — and
// deleted from the old one, same id, in the same action.
func (mb *MultiBackend) refile(ctx context.Context, op string, c board.Card, change func(*board.Card), apply func(*Backend) error) error {
	s, err := mb.snapshot()
	if err != nil {
		return err
	}
	r := newResolver(s)
	from := mb.domainOf(ctx, r, c)
	src, err := mb.backend(from)
	if err != nil {
		return err
	}
	if isStub(c) {
		return apply(src)
	}
	// The home is decided from the card as the action currently sees it — an
	// earlier setter in the same scope may already have re-filed it — never
	// from the caller's copy.
	p, err := CardPath(c.ItemID)
	if err != nil {
		return err
	}
	data, ok, err := src.read(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrCardNotFound, c.ItemID)
	}
	f, err := DecodeCard(c.ItemID, data)
	if err != nil {
		return err
	}
	if f.ProjectCard {
		return fmt.Errorf("%w: %s", ErrCardNotFound, c.ItemID)
	}
	after := f.Card
	change(&after)
	to := mb.homeOf(r, after)
	if to == from {
		return apply(src)
	}
	f.Card = after
	if err := mb.place(ctx, op, f, p, from, to); err != nil {
		return err
	}
	return mb.cascade(ctx, op, s, r, c.ItemID, to)
}

// place moves one decoded card from one domain to another: the copy in the
// new domain is written first — with movedFrom/movedAt and Aeman-Moved-From
// on its commit — then the old one is deleted with Aeman-Moved-To. Create
// before delete: a crash between the two leaves a duplicate the reader
// resolves, never a missing card.
func (mb *MultiBackend) place(ctx context.Context, op string, f CardFile, p, from, to string) error {
	src, err := mb.backend(from)
	if err != nil {
		return err
	}
	dst, err := mb.backend(to)
	if err != nil {
		return err
	}
	id := f.Card.ItemID
	// What the move changed, as trailers: the destination commit's file diff
	// is the whole card, so the log reads the change from these instead.
	var changes []Change
	if was, ok, rerr := src.read(ctx, p); rerr == nil && ok {
		f.Card.MovedFrom, f.Card.MovedAt = "", ""
		if now, eerr := EncodeCard(f); eerr == nil {
			changes = diffCard(id, was, now)
		}
	}
	f.Card.MovedFrom = from
	f.Card.MovedAt = mb.now().UTC().Format(time.RFC3339)
	out, err := EncodeCard(f)
	if err != nil {
		return err
	}
	if err := dst.writeWith(ctx, op, []string{id}, p, out, map[string]string{"Aeman-Moved-From": from}, changes...); err != nil {
		return err
	}
	return src.writeWith(ctx, op, []string{id}, p, nil, map[string]string{"Aeman-Moved-To": to})
}

// CardLog is the card's feed read from the commits: in the domain it lives
// in and, following Aeman-Moved-From, in the domain it came from — one
// continuous list, newest first, each field change an event with the
// commit's actor and time. truncatedBefore is the horizon of the oldest
// domain walked when a shallow clone cuts the history; zero otherwise.
func (mb *MultiBackend) CardLog(ctx context.Context, bd board.Board, id string) ([]board.Event, time.Time, error) {
	return mb.cardLog(ctx, bd, id, time.Time{})
}

// CardLogSince is CardLog cut at a boundary — the day feed's read, which
// follows a move the same way.
func (mb *MultiBackend) CardLogSince(ctx context.Context, bd board.Board, id string, since time.Time) ([]board.Event, time.Time, error) {
	return mb.cardLog(ctx, bd, id, since)
}

func (mb *MultiBackend) cardLog(_ context.Context, bd board.Board, id string, since time.Time) ([]board.Event, time.Time, error) {
	// The board handed in already says where the card lives (the store
	// stamps every card); decoding the whole tree again for that one fact
	// is what made a log read cost as much as a board load.
	domain := stampedDomain(bd, id)
	if domain == "" {
		s, err := mb.snapshot()
		if err != nil {
			return nil, time.Time{}, err
		}
		var ok bool
		if domain, ok = newResolver(s).cards[id]; !ok {
			return nil, time.Time{}, fmt.Errorf("%w: %s", ErrCardNotFound, id)
		}
	}
	var events []board.Event
	var truncated time.Time
	visited := map[string]bool{}
	for domain != "" && !visited[domain] {
		visited[domain] = true
		be, err := mb.backend(domain)
		if err != nil {
			return nil, time.Time{}, err
		}
		log, err := be.repo.CardLogSince(id, since)
		if err != nil {
			return nil, time.Time{}, err
		}
		next := ""
		for _, e := range log.Entries {
			events = append(events, eventsOf(e)...)
			if e.MovedFrom != "" {
				next = e.MovedFrom // the oldest move-in decides where the feed continues
			}
		}
		truncated = log.TruncatedBefore
		domain = next
	}
	return events, truncated, nil
}

// eventsOf turns one commit's entry into events, one per change; a commit
// that names the card without a field change is one event of its action.
func eventsOf(e LogEntry) []board.Event {
	at := e.At.UTC().Format(time.RFC3339)
	hash := e.Hash.String()[:12]
	if len(e.Changes) == 0 {
		return []board.Event{{ID: hash, Kind: e.Action, Actor: e.Actor, At: at}}
	}
	out := make([]board.Event, 0, len(e.Changes))
	for _, ch := range e.Changes {
		out = append(out, board.Event{ID: hash + ":" + ch.Kind, Kind: ch.Kind, From: ch.From, To: ch.To, Actor: e.Actor, At: at})
	}
	return out
}

// stampedDomain is the domain a loaded board records for a card, or "" when
// the board does not carry the card (or was built without stamps).
func stampedDomain(bd board.Board, id string) string {
	for _, c := range bd.Cards {
		if c.ItemID == id {
			return c.Domain
		}
	}
	return ""
}

// cascade moves what the linked-card rules tie to a moved card — its review
// card and its subtasks, and theirs — into the same domain, in the same
// action. A linked card already there (or already staged there) is left. A
// hierarchy link is not a tie: nothing under a goal, an initiative or an epic
// follows it (H6), and a task under one keeps the repository its own team
// gives it.
func (mb *MultiBackend) cascade(ctx context.Context, op string, s Snapshot, r resolver, parentID, to string) error {
	if r.planning[parentID] {
		return nil
	}
	for _, child := range s.Cards {
		if child.ReviewOf != parentID && child.Parent != parentID {
			continue
		}
		from := mb.domainOf(ctx, r, child)
		if from == to {
			continue
		}
		src, err := mb.backend(from)
		if err != nil {
			return err
		}
		p, err := CardPath(child.ItemID)
		if err != nil {
			return err
		}
		data, ok, err := src.read(ctx, p)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		f, err := DecodeCard(child.ItemID, data)
		if err != nil {
			return err
		}
		if f.ProjectCard {
			continue
		}
		if err := mb.place(ctx, op, f, p, from, to); err != nil {
			return err
		}
		if err := mb.cascade(ctx, op, s, r, child.ItemID, to); err != nil {
			return err
		}
	}
	return nil
}

// SweepGhosts is the maintenance half of a move: it deletes the stale copy
// of every torn move whose destination has landed — landed(domain) says
// whether that domain's commits are pushed — one "maintenance" commit per
// domain, and reports how many files it removed. A duplicate that is not a
// move (neither copy says movedFrom) is a maintainer's to resolve and is
// never touched.
func (mb *MultiBackend) SweepGhosts(_ context.Context, landed func(domain string) bool) (int, error) {
	s, err := mb.snapshot()
	if err != nil {
		return 0, err
	}
	byDomain := map[string][]string{}
	for _, g := range s.Ghosts {
		if g.Current == "" || !landed(g.Current) {
			continue
		}
		byDomain[g.Domain] = append(byDomain[g.Domain], g.ID)
	}
	n := 0
	for _, d := range mb.domainList() {
		ids := byDomain[d.Name]
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		writes := make([]FileWrite, 0, len(ids))
		for _, id := range ids {
			p, err := CardPath(id)
			if err != nil {
				return n, err
			}
			writes = append(writes, FileWrite{Path: p})
		}
		be := mb.backends[d.Name]
		if _, err := be.repo.Commit(Action{Name: "maintenance", At: be.now(), Cards: ids,
			Summary: fmt.Sprintf("maintenance: remove %d torn-move ghost(s)", len(ids))}, writes); err != nil {
			return n, err
		}
		n += len(ids)
	}
	return n, nil
}

// ---- Backend methods -----------------------------------------------------------------

// DeleteCard removes the card from its domain.
func (mb *MultiBackend) DeleteCard(ctx context.Context, bd board.Board, card board.Card) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.DeleteCard(ctx, bd, card)
}

// MoveCard reorders within the card's domain. The TOP of the board (afterID
// "") is the top of the merged board, not of the card's own repository: the
// board's order is one list across the domains, and a card put first — a
// review nobody has taken being made (K10), a card parked at the top of its
// backlog — must rank before every domain's first card.
func (mb *MultiBackend) MoveCard(ctx context.Context, bd board.Board, card board.Card, afterID string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	if afterID == "" && len(mb.domains) > 1 && !board.IsStateTitle(card.Title) {
		first := ""
		for _, c := range bd.Cards {
			if c.ItemID == card.ItemID || c.Rank == "" || board.IsStateTitle(c.Title) {
				continue
			}
			if first == "" || c.Rank < first {
				first = c.Rank
			}
		}
		if first != "" {
			rank, err := board.RankBetween("", first)
			if err == nil && !board.RankTooLong(rank) {
				return be.setCardRank(ctx, card, rank)
			}
		}
	}
	return be.MoveCard(ctx, bd, card, afterID)
}

// AddNote appends a note in the card's domain.
func (mb *MultiBackend) AddNote(ctx context.Context, bd board.Board, card board.Card, text string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.AddNote(ctx, bd, card, text)
}

// AppendEvent records the event on the card's domain commit.
func (mb *MultiBackend) AppendEvent(ctx context.Context, bd board.Board, card board.Card, e board.Event) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.AppendEvent(ctx, bd, card, e)
}

// EditNote rewrites a note in the card's domain.
func (mb *MultiBackend) EditNote(ctx context.Context, bd board.Board, card board.Card, note board.Note, text string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.EditNote(ctx, bd, card, note, text)
}

// DeleteNote removes a note in the card's domain.
func (mb *MultiBackend) DeleteNote(ctx context.Context, bd board.Board, card board.Card, note board.Note) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.DeleteNote(ctx, bd, card, note)
}

// SetDescription writes in the card's domain.
func (mb *MultiBackend) SetDescription(ctx context.Context, bd board.Board, card board.Card, description string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetDescription(ctx, bd, card, description)
}

// RenameCard writes in the card's domain.
func (mb *MultiBackend) RenameCard(ctx context.Context, bd board.Board, card board.Card, title string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.RenameCard(ctx, bd, card, title)
}

// SetStage writes in the card's domain.
func (mb *MultiBackend) SetStage(ctx context.Context, bd board.Board, card board.Card, stage board.StageKey) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetStage(ctx, bd, card, stage)
}

// SetProgress writes in the card's domain.
func (mb *MultiBackend) SetProgress(ctx context.Context, bd board.Board, card board.Card, progress int) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetProgress(ctx, bd, card, progress)
}

// SetZone writes in the card's domain.
func (mb *MultiBackend) SetZone(ctx context.Context, bd board.Board, card board.Card, zone board.ZoneKey) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetZone(ctx, bd, card, zone)
}

// SetPersonCapacity writes in the PRIMARY: people are the board's, not a
// domain's, and users/<login>.yaml lives where the board is named.
func (mb *MultiBackend) SetPersonCapacity(ctx context.Context, bd board.Board, login string, points int) error {
	be, err := mb.backend(mb.primary())
	if err != nil {
		return err
	}
	return be.SetPersonCapacity(ctx, bd, login, points)
}

// SetTeamPoints writes where the TEAM is declared: its capacity is one line
// of the same file its sprint pointer lives in.
func (mb *MultiBackend) SetTeamPoints(ctx context.Context, bd board.Board, team string, points int) error {
	s, err := mb.snapshot()
	if err != nil {
		return err
	}
	d, ok := newResolver(s).teams[team]
	if !ok {
		d = board.DomainFrom(ctx)
	}
	be, err := mb.backend(d)
	if err != nil {
		return err
	}
	return be.SetTeamPoints(ctx, bd, team, points)
}

// SetSize writes in the card's domain.
func (mb *MultiBackend) SetSize(ctx context.Context, bd board.Board, card board.Card, size board.SizeKey) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetSize(ctx, bd, card, size)
}

// SetDay writes in the card's domain.
func (mb *MultiBackend) SetDay(ctx context.Context, bd board.Board, card board.Card, day string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetDay(ctx, bd, card, day)
}

// SetDoneAt writes in the card's domain.
func (mb *MultiBackend) SetDoneAt(ctx context.Context, bd board.Board, card board.Card, day string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetDoneAt(ctx, bd, card, day)
}

// SetStart writes in the card's domain.
func (mb *MultiBackend) SetStart(ctx context.Context, bd board.Board, card board.Card, date string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetStart(ctx, bd, card, date)
}

// SetSprintStart writes in the card's domain.
func (mb *MultiBackend) SetSprintStart(ctx context.Context, bd board.Board, card board.Card, date string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetSprintStart(ctx, bd, card, date)
}

// SetBacklog writes in the card's own domain: a list is the team's, and the
// card is already in the repository its team names.
func (mb *MultiBackend) SetBacklog(
	ctx context.Context, bd board.Board, card board.Card, parked bool,
) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetBacklog(ctx, bd, card, parked)
}

// SetTodo writes in the card's own domain, as SetBacklog does.
func (mb *MultiBackend) SetTodo(ctx context.Context, bd board.Board, card board.Card, todo bool) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetTodo(ctx, bd, card, todo)
}

// SetTeam may move a team card to another domain.
func (mb *MultiBackend) SetTeam(ctx context.Context, bd board.Board, card board.Card, team string) error {
	if card.Title == board.SprintStateTitle && team != card.Team {
		if err := mb.checkName("team", team); err != nil {
			return err
		}
	}
	return mb.refile(ctx, "team", card, func(c *board.Card) { c.Team = team }, func(be *Backend) error { return be.SetTeam(ctx, bd, card, team) })
}

// nameFree refuses a roster name some domain already declares (ErrNameTaken).
func nameFree(r resolver, kind, name string) error {
	var taken bool
	if kind == "team" {
		_, taken = r.teams[name]
	}
	if taken {
		return fmt.Errorf("%w: %s %q", ErrNameTaken, kind, name)
	}
	return nil
}

// checkName is nameFree against the current snapshot — for a rename, which
// has no snapshot of its own yet.
func (mb *MultiBackend) checkName(kind, name string) error {
	s, err := mb.snapshot()
	if err != nil {
		return err
	}
	return nameFree(newResolver(s), kind, name)
}

// SetRecurrence writes in the card's domain.
func (mb *MultiBackend) SetRecurrence(ctx context.Context, bd board.Board, card board.Card, cycle string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetRecurrence(ctx, bd, card, cycle)
}

// SetAssignee writes in the card's domain.
func (mb *MultiBackend) SetAssignee(ctx context.Context, bd board.Board, card board.Card, login string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetAssignee(ctx, bd, card, login)
}

// SetParent may move a subtask to its parent's domain.
func (mb *MultiBackend) SetParent(ctx context.Context, bd board.Board, card board.Card, parent string) error {
	return mb.refile(ctx, "parent", card, func(c *board.Card) { c.Parent = parent }, func(be *Backend) error { return be.SetParent(ctx, bd, card, parent) })
}

// SetReviewOf may move a review card to its original's domain.
func (mb *MultiBackend) SetReviewOf(ctx context.Context, bd board.Board, card board.Card, reviewOf string) error {
	return mb.refile(ctx, "review-of", card, func(c *board.Card) { c.ReviewOf = reviewOf }, func(be *Backend) error { return be.SetReviewOf(ctx, bd, card, reviewOf) })
}

// SetReviewRound writes in the card's domain.
func (mb *MultiBackend) SetReviewRound(ctx context.Context, bd board.Board, card board.Card, round int) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetReviewRound(ctx, bd, card, round)
}

// SetSprintState writes the team's pointer where the team is declared; a
// new team is declared in the primary.

func (mb *MultiBackend) SetSprintState(ctx context.Context, bd board.Board, team, current, previous string) error {
	s, err := mb.snapshot()
	if err != nil {
		return err
	}
	// An existing team's pointer stays where the team is declared; a new
	// team is declared where the caller chose (board.WithDomain), default
	// the primary.
	d, ok := newResolver(s).teams[team]
	if !ok {
		d = board.DomainFrom(ctx)
	}
	be, err := mb.backend(d)
	if err != nil {
		return err
	}
	return be.SetSprintState(ctx, bd, team, current, previous)
}

// ---- ownership: docs/design/ownership.md ------------------------------------

// SetOwner writes a planning card's owner where the card lives (the primary).
func (mb *MultiBackend) SetOwner(ctx context.Context, bd board.Board, card board.Card, login string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetOwner(ctx, bd, card, login)
}

// SetCustomers writes a card's customers where the card lives: a customer
// id places nothing.
func (mb *MultiBackend) SetCustomers(ctx context.Context, bd board.Board, card board.Card, ids []string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetCustomers(ctx, bd, card, ids)
}

// AddCustomer declares a customer in the primary, where the roster lives.
func (mb *MultiBackend) AddCustomer(ctx context.Context, bd board.Board, name string) (board.Customer, error) {
	be, err := mb.backend(mb.primary())
	if err != nil {
		return board.Customer{}, err
	}
	return be.AddCustomer(ctx, bd, name)
}

// RenameCustomer renames a customer in the primary's roster.
func (mb *MultiBackend) RenameCustomer(ctx context.Context, bd board.Board, id, name string) error {
	be, err := mb.backend(mb.primary())
	if err != nil {
		return err
	}
	return be.RenameCustomer(ctx, bd, id, name)
}

// MoveCustomer reorders the primary's roster.
func (mb *MultiBackend) MoveCustomer(ctx context.Context, bd board.Board, id, afterID string) error {
	be, err := mb.backend(mb.primary())
	if err != nil {
		return err
	}
	return be.MoveCustomer(ctx, bd, id, afterID)
}

// DeleteCustomer takes the customer off every card of EVERY domain that
// names it and out of the primary's roster. Each repository's part is one
// commit; under a scope the action is one commit per repository it touched.
// The other domains go first and the roster last, so a reader never sees a
// card naming a customer the roster no longer has.
func (mb *MultiBackend) DeleteCustomer(ctx context.Context, bd board.Board, id string) error {
	for _, d := range mb.domainList()[1:] {
		be, err := mb.backend(d.Name)
		if err != nil {
			return err
		}
		if err := be.DeleteCustomer(ctx, bd, id); err != nil {
			return err
		}
	}
	be, err := mb.backend(mb.primary())
	if err != nil {
		return err
	}
	return be.DeleteCustomer(ctx, bd, id)
}
