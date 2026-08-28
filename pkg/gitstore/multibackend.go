package gitstore

import (
	"context"
	"errors"
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
	// dmu guards the domain list: the configured domains are there from the
	// start, a personal domain is attached when its owner first shows up and
	// detached when they unlink it.
	dmu      sync.RWMutex
	domains  []Domain
	backends map[string]*Backend
	now      func() time.Time

	// issues is what the last merge had to resolve — duplicate roster names
	// and torn-move ghosts — for health to report; users are the primary's
	// links to personal repositories, for the server to attach.
	mu      sync.Mutex
	aliases []Alias
	ghosts  []Ghost
	users   []User
}

// domainList is a copy of the domains, primary first, safe to range over
// while another goroutine attaches or detaches one.
func (mb *MultiBackend) domainList() []Domain {
	mb.dmu.RLock()
	defer mb.dmu.RUnlock()
	return append([]Domain(nil), mb.domains...)
}

// Domains lists the board's domains, primary first.
func (mb *MultiBackend) Domains() []Domain { return mb.domainList() }

// AddDomain attaches a domain at run time — a personal repository, attached
// when its owner first arrives. A name the board already has is refused.
func (mb *MultiBackend) AddDomain(d Domain) error {
	if d.Name == "" || d.Repo == nil {
		return fmt.Errorf("%w: a domain needs a name and a repository", ErrUnknownDomain)
	}
	mb.dmu.Lock()
	defer mb.dmu.Unlock()
	if _, taken := mb.backends[d.Name]; taken {
		return fmt.Errorf("domain %q is already attached", d.Name)
	}
	mb.domains = append(mb.domains, d)
	mb.backends[d.Name] = NewBackend(d.Repo, BackendOptions{Now: mb.now})
	return nil
}

// RemoveDomain detaches a domain; its cards are no longer served. The
// primary stays.
func (mb *MultiBackend) RemoveDomain(name string) error {
	mb.dmu.Lock()
	defer mb.dmu.Unlock()
	if name == "" || name == mb.domains[0].Name {
		return errors.New("the primary domain cannot be removed")
	}
	if _, ok := mb.backends[name]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownDomain, name)
	}
	delete(mb.backends, name)
	for i, d := range mb.domains {
		if d.Name == name {
			mb.domains = append(mb.domains[:i:i], mb.domains[i+1:]...)
			break
		}
	}
	return nil
}

// Users are the primary's links to personal repositories as of the last
// load — nil before any.
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
	mb.dmu.RLock()
	b, ok := mb.backends[domain]
	mb.dmu.RUnlock()
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

// LoadBoard merges every domain into the board the service expects.
func (mb *MultiBackend) LoadBoard(_ context.Context, boardID string) (board.Board, error) {
	s, err := mb.snapshot()
	if err != nil {
		return board.Board{}, err
	}
	bd := boardFromSnapshot(s)
	bd.Board = boardID
	return bd, nil
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
		if c, ok := byID[id]; ok {
			ordered = append(ordered, c)
		}
	}
	return ordered, nil
}

// ---- resolving domains -----------------------------------------------------------------

// resolver answers DomainOf's questions from a merged snapshot.
type resolver struct {
	cards, projects, teams, processes map[string]string
	stubs                             map[string]string // roster id → domain
}

func newResolver(s Snapshot) resolver {
	r := resolver{cards: map[string]string{}, projects: map[string]string{}, teams: map[string]string{}, processes: map[string]string{}, stubs: map[string]string{}}
	for _, c := range s.Cards {
		r.cards[c.ItemID] = c.Domain
	}
	for _, t := range s.Teams {
		r.teams[t.Name] = t.Domain
		r.stubs[t.ID] = t.Domain
	}
	for _, p := range s.Projects {
		r.projects[p.Name] = p.Domain
		r.stubs[p.ID] = p.Domain
		for _, e := range p.Epics {
			r.stubs[e.ID] = e.Domain
		}
		for _, d := range p.Deadlines {
			r.stubs[d.ID] = d.Domain
		}
	}
	for _, p := range s.Processes {
		r.processes[p.Name] = p.Domain
		r.stubs[p.ID] = p.Domain
		for _, k := range p.Tasks {
			r.cards[k.ID] = k.Domain
		}
	}
	for _, a := range s.Aliases {
		r.stubs[a.ID] = a.Domain
	}
	return r
}

func (r resolver) CardDomain(id string) (string, bool)      { d, ok := r.cards[id]; return d, ok }
func (r resolver) ProjectDomain(name string) (string, bool) { d, ok := r.projects[name]; return d, ok }
func (r resolver) TeamDomain(name string) (string, bool)    { d, ok := r.teams[name]; return d, ok }

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
	if isStub(c) || c.Title == board.ProcessTaskTitle {
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
// the caller asks (default the primary) — except that a column or deadline
// lives with its project, a task with its process, a process with its
// project when it has one.
func (mb *MultiBackend) CreateCard(ctx context.Context, bd board.Board, in board.CreateInput) (board.Card, error) {
	s, err := mb.snapshot()
	if err != nil {
		return board.Card{}, err
	}
	r := newResolver(s)
	// A declaration takes a name no domain has yet.
	switch in.Title {
	case board.ProjectStateTitle:
		if err := nameFree(r, "project", in.Project); err != nil {
			return board.Card{}, err
		}
	case board.SprintStateTitle:
		if err := nameFree(r, "team", in.Team); err != nil {
			return board.Card{}, err
		}
	case board.ProcessStateTitle:
		if err := nameFree(r, "process", in.Process); err != nil {
			return board.Card{}, err
		}
	}
	// The caller's choice — the input's, else the context's (board.WithDomain)
	// — counts only for what has no home by rule: a team, a project, a
	// process without a project.
	choice := in.Domain
	if choice == "" {
		choice = board.DomainFrom(ctx)
	}
	target := ""
	switch in.Title {
	case board.EpicStateTitle, board.DeadlineStateTitle:
		if d, ok := r.projects[in.Project]; ok {
			target = d
		}
	case board.ProcessTaskTitle:
		if d, ok := r.processes[in.Process]; ok {
			target = d
		}
	case board.ProcessStateTitle:
		if in.Project != "" {
			if d, ok := r.projects[in.Project]; ok {
				target = d
			}
		} else {
			target = choice
		}
	case board.ProjectStateTitle, board.SprintStateTitle:
		target = choice
	default:
		if in.Personal {
			// A personal card goes where the caller says — their own domain —
			// and nowhere else; the home rule does not apply to it.
			if in.Domain == "" {
				return board.Card{}, fmt.Errorf("%w: a personal card needs its domain", ErrUnknownDomain)
			}
			target = in.Domain
			break
		}
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
	if isStub(c) || c.Title == board.ProcessTaskTitle {
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
	after := f.Card
	change(&after)
	to := mb.homeOf(r, after)
	if board.IsPersonalDomain(from) {
		to = from // a personal card stays home whatever team or project it is given
	}
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
func (mb *MultiBackend) CardLog(_ context.Context, bd board.Board, id string) ([]board.Event, time.Time, error) {
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
		log, err := be.repo.CardLog(id, 0)
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

// stampedDomain is the domain a loaded board records for a card or task, or
// "" when the board does not carry the card (or was built without stamps).
func stampedDomain(bd board.Board, id string) string {
	for _, c := range bd.Cards {
		if c.ItemID == id {
			return c.Domain
		}
	}
	for _, c := range bd.Tasks {
		if c.ItemID == id {
			return c.Domain
		}
	}
	return ""
}

// cascade moves what the linked-card rules tie to a moved card — its review
// card and its subtasks, and theirs — into the same domain, in the same
// action. A linked card already there (or already staged there) is left.
func (mb *MultiBackend) cascade(ctx context.Context, op string, s Snapshot, r resolver, parentID, to string) error {
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

// MoveCard reorders within the card's domain.
func (mb *MultiBackend) MoveCard(ctx context.Context, bd board.Board, card board.Card, afterID string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
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

// SetDay writes in the card's domain.
func (mb *MultiBackend) SetDay(ctx context.Context, bd board.Board, card board.Card, day string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetDay(ctx, bd, card, day)
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

// SetPlan writes in the card's domain.
func (mb *MultiBackend) SetPlan(ctx context.Context, bd board.Board, card board.Card, plan board.PlanBand) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetPlan(ctx, bd, card, plan)
}

// SetWeek writes in the card's (or deadline's) domain.
func (mb *MultiBackend) SetWeek(ctx context.Context, bd board.Board, card board.Card, week string) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetWeek(ctx, bd, card, week)
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
	switch kind {
	case "team":
		_, taken = r.teams[name]
	case "project":
		_, taken = r.projects[name]
	case "process":
		_, taken = r.processes[name]
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

// SetEpic may move a card into another project's domain.
func (mb *MultiBackend) SetEpic(ctx context.Context, bd board.Board, card board.Card, epic string) error {
	return mb.refile(ctx, "epic", card, func(c *board.Card) { c.Epic = epic }, func(be *Backend) error { return be.SetEpic(ctx, bd, card, epic) })
}

// SetProcess writes in the stub's or task's domain.
func (mb *MultiBackend) SetProcess(ctx context.Context, bd board.Board, card board.Card, process string) error {
	if card.Title == board.ProcessStateTitle && process != card.Process {
		if err := mb.checkName("process", process); err != nil {
			return err
		}
	}
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetProcess(ctx, bd, card, process)
}

// SetTask may move an iteration to its task's domain.
func (mb *MultiBackend) SetTask(ctx context.Context, bd board.Board, card board.Card, task string) error {
	return mb.refile(ctx, "task", card, func(c *board.Card) { c.Task = task }, func(be *Backend) error { return be.SetTask(ctx, bd, card, task) })
}

// SetPaused writes in the process's domain.
func (mb *MultiBackend) SetPaused(ctx context.Context, bd board.Board, card board.Card, paused bool) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetPaused(ctx, bd, card, paused)
}

// SetAccumulate writes in the task's domain.
func (mb *MultiBackend) SetAccumulate(ctx context.Context, bd board.Board, card board.Card, on bool) error {
	be, err := mb.route(ctx, card)
	if err != nil {
		return err
	}
	return be.SetAccumulate(ctx, bd, card, on)
}

// SetProject may move a card into another project's domain.
func (mb *MultiBackend) SetProject(ctx context.Context, bd board.Board, card board.Card, project string) error {
	if card.Title == board.ProjectStateTitle && project != card.Project {
		if err := mb.checkName("project", project); err != nil {
			return err
		}
	}
	return mb.refile(ctx, "project", card, func(c *board.Card) { c.Project = project }, func(be *Backend) error { return be.SetProject(ctx, bd, card, project) })
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
