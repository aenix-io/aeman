package gitstore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Backend is boardservice.Backend over one domain's repository. The service
// speaks the state-card protocol it always has — a stub card whose Title is
// an aeman:*-state marker stands for a team — and the backend routes it
// onto its roster file. Every mutation
// is a commit: on its own when called bare, or folded into the enclosing
// action's single commit when the context carries a scope (WithScope).

// BackendOptions configures a Backend.
type BackendOptions struct {
	// Now is the clock (tests pin it); nil means time.Now.
	Now func() time.Time
}

// Backend implements boardservice.Backend.
type Backend struct {
	repo *Repo
	now  func() time.Time
}

// NewBackend wraps a repository.
func NewBackend(repo *Repo, opts BackendOptions) *Backend {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Backend{repo: repo, now: opts.Now}
}

// Errors the backend reports for the roster protocol.
var (
	ErrNoteNotFound = errors.New("gitstore: note not found")
	ErrCardNotFound = errors.New("gitstore: card not found")
)

// ---- scope: one action, one commit ------------------------------------------

type scopeKey struct{}

// scope collects an action's writes so they commit together. An action may
// touch more than one repository (a move between domains): each gets its
// own part, and flush commits the parts in the order they were first
// written — the destination of a move before its source.
type scope struct {
	mu      sync.Mutex
	action  Action
	parts   map[*Repo]*scopePart
	repos   []*Repo // first-write order
	changes []Change
	cards   map[string]bool
	// roster names created inside the scope, so a later write in the same
	// action can find them before anything is committed.
	teams map[string]string
}

// scopePart is one repository's staged writes, and the trailers only its
// commit carries (a move's Aeman-Moved-From on the destination, -To on the
// source).
type scopePart struct {
	staged   map[string][]byte // path → content; nil = delete
	order    []string
	trailers map[string]string
}

// addTrailer records a per-repository trailer; a second, different value
// for the same key is appended rather than lost.
func (p *scopePart) addTrailer(key, val string) {
	if p.trailers == nil {
		p.trailers = map[string]string{}
	}
	cur, ok := p.trailers[key]
	switch {
	case !ok:
		p.trailers[key] = val
	case cur == val || contains(strings.Fields(cur), val):
	default:
		p.trailers[key] = cur + " " + val
	}
}

func (sc *scope) part(r *Repo) *scopePart {
	p, ok := sc.parts[r]
	if !ok {
		p = &scopePart{staged: map[string][]byte{}}
		sc.parts[r] = p
		sc.repos = append(sc.repos, r)
	}
	return p
}

// WithScope opens an action on the context: every backend write made with
// the returned context is staged, and flush commits them — one commit per
// repository touched, all carrying the action's trailers. A flush with
// nothing staged makes no commit and returns the zero hash; with several
// repositories it returns the last commit's hash.
func WithScope(ctx context.Context, a Action) (context.Context, func() (plumbing.Hash, error)) {
	sc := &scope{action: a, parts: map[*Repo]*scopePart{}, cards: map[string]bool{},
		teams: map[string]string{}}
	for _, id := range a.Cards {
		sc.cards[id] = true
	}
	flush := func() (plumbing.Hash, error) {
		sc.mu.Lock()
		defer sc.mu.Unlock()
		act := sc.action
		act.Changes = append(act.Changes, sc.changes...)
		act.Cards = append([]string(nil), a.Cards...)
		for id := range sc.cards {
			if !contains(act.Cards, id) {
				act.Cards = append(act.Cards, id)
			}
		}
		if act.Actor == "" {
			act.Actor = actorOf(ctx)
		}
		last := plumbing.ZeroHash
		for _, r := range sc.repos {
			part := sc.parts[r]
			if len(part.order) == 0 {
				continue
			}
			writes := make([]FileWrite, 0, len(part.order))
			for _, p := range part.order {
				writes = append(writes, FileWrite{Path: p, Data: part.staged[p]})
			}
			partAct := act
			if len(part.trailers) > 0 {
				partAct.Trailers = make(map[string]string, len(act.Trailers)+len(part.trailers))
				for k, v := range act.Trailers {
					partAct.Trailers[k] = v
				}
				for k, v := range part.trailers {
					partAct.Trailers[k] = v
				}
			}
			h, err := r.Commit(partAct, writes)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			if !h.IsZero() {
				last = h
			}
		}
		return last, nil
	}
	return context.WithValue(ctx, scopeKey{}, sc), flush
}

func scopeOf(ctx context.Context) *scope {
	sc, _ := ctx.Value(scopeKey{}).(*scope)
	return sc
}

// actorOf is the commit's author: the request's actor, or nobody — the
// server — when the context is marked Unattributed (G6): work the server
// does on its own behalf inside a user's request, a background title
// resolve, is not that user's doing.
func actorOf(ctx context.Context) string {
	if board.IsUnattributed(ctx) {
		return ""
	}
	return board.ActorFrom(ctx)
}

// ScopeCards lists the cards the action has touched so far, in id order —
// after flush, exactly what its commits name in Aeman-Cards (a move's
// cascade included). Nil without a scope.
func ScopeCards(ctx context.Context) []string {
	sc := scopeOf(ctx)
	if sc == nil {
		return nil
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	ids := make([]string, 0, len(sc.cards))
	for id := range sc.cards {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// read returns a file as the action currently sees it: staged content first,
// then the tree. ok is false when the file does not exist (or is staged for
// deletion).
func (b *Backend) read(ctx context.Context, p string) ([]byte, bool, error) {
	if sc := scopeOf(ctx); sc != nil {
		sc.mu.Lock()
		var data []byte
		staged := false
		if part, ok := sc.parts[b.repo]; ok {
			data, staged = part.staged[p]
		}
		sc.mu.Unlock()
		if staged {
			return data, data != nil, nil
		}
	}
	data, err := b.repo.ReadFile(p)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// write records one file change: staged under a scope, else committed on
// its own as action op.
func (b *Backend) write(ctx context.Context, op string, cards []string, p string, data []byte) error {
	return b.writeWith(ctx, op, cards, p, data, nil)
}

// writeWith is write with trailers that belong to this repository's commit
// only — under a scope they attach to its part, not to the action — and
// with Aeman-Change lines for what the write changed when the file diff
// cannot say it (a file that arrives whole from another domain).
func (b *Backend) writeWith(ctx context.Context, op string, cards []string, p string, data []byte, trailers map[string]string, changes ...Change) error {
	return b.writeSummed(ctx, op, "", cards, p, data, trailers, changes...)
}

// writeNamed is write with the commit's subject said outright — a roster
// entry carries no card id for the subject to name.
func (b *Backend) writeNamed(ctx context.Context, op, summary string, cards []string, p string, data []byte) error {
	return b.writeSummed(ctx, op, summary, cards, p, data, nil)
}

// writeSummed is writeWith with a subject; "" names the op and its one card.
// Under a scope the action's own subject stands and the summary is unused.
func (b *Backend) writeSummed(ctx context.Context, op, summary string, cards []string, p string, data []byte, trailers map[string]string, changes ...Change) error {
	if sc := scopeOf(ctx); sc != nil {
		sc.mu.Lock()
		defer sc.mu.Unlock()
		part := sc.part(b.repo)
		if _, seen := part.staged[p]; !seen {
			part.order = append(part.order, p)
		}
		part.staged[p] = data
		for _, id := range cards {
			sc.cards[id] = true
		}
		for k, v := range trailers {
			part.addTrailer(k, v)
		}
		sc.changes = append(sc.changes, changes...)
		return nil
	}
	if summary == "" {
		summary = op
		if len(cards) == 1 {
			summary = op + " " + cards[0]
		}
	}
	_, err := b.repo.Commit(Action{Name: op, Actor: actorOf(ctx), At: b.now(), Cards: cards, Summary: summary, Trailers: trailers, Changes: changes}, []FileWrite{{Path: p, Data: data}})
	return err
}

// ---- loading -----------------------------------------------------------------------

// stagedIn is what an action has written to a repository so far — the files
// a board read inside the action lays over the tip (loadOverlaid). Nil
// outside a scope, or when the action has not touched this repository.
func stagedIn(ctx context.Context, r *Repo) map[string][]byte {
	sc := scopeOf(ctx)
	if sc == nil {
		return nil
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	part, ok := sc.parts[r]
	if !ok || len(part.staged) == 0 {
		return nil
	}
	return maps.Clone(part.staged)
}

// LoadBoard reads the domain and hands the service the shape it expects:
// the roster files become the state cards NewBoard splits back out, so the
// duplicate and ordering rules stay in one place. Inside a scope the board
// is read as the action has left it, staged writes included.
func (b *Backend) LoadBoard(ctx context.Context, boardID string) (board.Board, error) {
	s, err := loadOverlaid(b.repo, stagedIn(ctx, b.repo))
	if err != nil {
		return board.Board{}, err
	}
	bd := boardFromSnapshot(s)
	bd.Board = boardID
	return bd, nil
}

// LoadBoardAsOf is LoadBoard at a past moment — the tree this domain had
// when that day ended (see LoadAsOf). ok is false behind the horizon.
func (b *Backend) LoadBoardAsOf(_ context.Context, boardID string, at time.Time) (board.Board, bool, error) {
	s, ok, err := LoadAsOf(b.repo, at)
	if err != nil || !ok {
		return board.Board{}, ok, err
	}
	bd := boardFromSnapshot(s)
	bd.Board = boardID
	return bd, true, nil
}

// boardFromSnapshot hands the service the shape it expects: the roster
// becomes the state cards NewBoard splits back out, so the duplicate and
// ordering rules stay in one place. Every synthesized card carries its
// domain.
func boardFromSnapshot(s Snapshot) board.Board { return boardFromSnapshotIn("", s) }

// boardFromSnapshotIn assembles the board for a store whose entries carry
// their domain's NAME — which is every stamped snapshot, the primary
// included — naming that primary so the assembly's own domain rules read
// one namespace rather than comparing a stamped column against an
// unstamped placement.
func boardFromSnapshotIn(primary string, s Snapshot) board.Board {
	cards := make([]board.Card, 0, len(s.Cards)+len(s.Teams))
	for _, t := range s.Teams {
		cards = append(cards, board.Card{ItemID: t.ID, Title: board.SprintStateTitle, Team: t.Name,
			SprintStart: t.Sprint.Current, StartDate: t.Sprint.Previous, Rank: t.Rank, CreatedAt: t.Created, Domain: t.Domain})
	}
	cards = append(cards, s.Cards...)
	bd := board.NewBoardIn(primary, cards)
	// The customer roster, in rank order (ties by creation, then id).
	bd.Customers = customersOf(s.Customers)
	// The people the roster knows: users/<login>.yaml in the primary — a
	// capacity a lead set travels here.
	for _, u := range s.Users {
		if u.Capacity == 0 {
			continue
		}
		if bd.People == nil {
			bd.People = map[string]board.Person{}
		}
		bd.People[u.Login] = board.Person{Capacity: u.Capacity}
	}
	// The team's capacity rides its state, not a state card: the card only
	// carries the sprint, and the number belongs beside it.
	// The team's BACKLOG LISTS ride there too, for the same reason: they are
	// the team's, they live in its roster file, and every board that draws a
	// list reads them from here. A team's state may not exist yet — a team
	// with lists but no sprint is perfectly ordinary — so it is created rather
	// than skipped, or the lists would be invisible until somebody started a
	// sprint.
	for _, t := range s.Teams {
		st, ok := bd.SprintStates[t.Name]
		if !ok {
			continue
		}
		if t.Capacity != (board.Capacity{}) {
			st.Capacity = t.Capacity
		}
		bd.SprintStates[t.Name] = st
	}
	bd.Title = s.Board.Title
	return bd
}

// LoadCards reads the asked-for cards, in the order asked; missing ones are
// omitted.
func (b *Backend) LoadCards(ctx context.Context, _ board.Board, ids []string) ([]board.Card, error) {
	out := make([]board.Card, 0, len(ids))
	for _, id := range ids {
		p, err := CardPath(id)
		if err != nil {
			continue
		}
		data, ok, err := b.read(ctx, p)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		f, err := DecodeCard(id, data)
		if err != nil {
			return nil, err
		}
		if f.ProjectCard {
			continue // not a card of the board (H15)
		}
		// Read by the same rules a read of the whole board applies to one
		// card, or the reload of a card a commit wrote would disagree with
		// the board about the same file.
		c := board.SettleCard(f.Card)
		if c.Parent != "" && b.holdsProjectCard(ctx, c.Parent) {
			c.Parent = "" // dangling on a whole read too (dropDanglingParents, H15)
		}
		out = append(out, c)
	}
	return out, nil
}

// holdsProjectCard reports that this repository keeps id as a project card —
// a file the board no longer reads (H15). A card it does not hold at all is
// not this repository's to answer for: the board may hold it in another.
func (b *Backend) holdsProjectCard(ctx context.Context, id string) bool {
	p, err := CardPath(id)
	if err != nil {
		return false
	}
	data, ok, err := b.read(ctx, p)
	if err != nil || !ok {
		return false
	}
	f, err := DecodeCard(id, data)
	return err == nil && f.ProjectCard
}

// ---- creating ------------------------------------------------------------------------

// CreateCard creates a card — or, for a marker title, the roster entry the
// stub stands for — and returns it with its new id.
func (b *Backend) CreateCard(ctx context.Context, _ board.Board, in board.CreateInput) (board.Card, error) {
	now := b.now()
	id := in.ItemID // the cache may have minted it already, so it can hand it out at once
	if id == "" {
		id = MintID(in, now)
	}
	created := now.UTC().Format(time.RFC3339)
	s, err := b.snapshot(ctx)
	if err != nil {
		return board.Card{}, err
	}
	c := cardFromInput(in, id, created, board.ActorFrom(ctx))
	c.Rank, _ = board.RankBetween(lastRank(len(s.Cards), func(i int) string { return s.Cards[i].Rank }), "")
	data, err := EncodeCard(CardFile{Card: c})
	if err != nil {
		return board.Card{}, err
	}
	p, _ := CardPath(id)
	return c, b.write(ctx, "create", []string{id}, p, data)
}

func cardFromInput(in board.CreateInput, id, created, author string) board.Card {
	c := board.Card{
		ItemID: id, Title: in.Title, Type: in.Type, Zone: in.Zone, Day: in.Day, StartDate: in.Start, SprintStart: in.SprintStart,
		Team: in.Team, ReviewOf: in.ReviewOf, Parent: in.Parent,
		Parked: in.Parked, Size: in.Size, Recurrence: in.Recurrence,
		Description: in.Body, CreatedAt: created, Author: author,
	}
	if in.Assignee != "" {
		c.Assignees = []string{in.Assignee}
	}
	return c
}

// lastRank is the largest rank in a list ("" for an empty one), so a new
// entry lands after everything.
func lastRank(n int, rank func(int) string) string {
	last := ""
	for i := 0; i < n; i++ {
		if r := rank(i); r > last {
			last = r
		}
	}
	return last
}

// ---- resolving names ---------------------------------------------------------------

func (b *Backend) snapshot(_ context.Context) (Snapshot, error) {
	s, err := Load(b.repo)
	if errors.Is(err, ErrEmptyRepository) {
		return Snapshot{}, nil
	}
	return s, err
}

// pathFor is where a card or stub lives.
func pathFor(c board.Card) (string, error) {
	if c.Title == board.SprintStateTitle {
		return TeamPath(c.ItemID), nil
	}
	return CardPath(c.ItemID)
}

// ---- deleting and moving -----------------------------------------------------------

// DeleteCard removes a card's file, or a stub's roster file.
func (b *Backend) DeleteCard(ctx context.Context, _ board.Board, card board.Card) error {
	p, err := pathFor(card)
	if err != nil {
		return err
	}
	if !isStub(card) {
		if err := b.refuseProjectCard(ctx, p, card.ItemID); err != nil {
			return err
		}
	}
	// A roster entry carries no card id to name, so its commit names the
	// entry itself — "delete team «features»", not a bare "delete".
	summary := ""
	if name := StubName(card); name != "" {
		summary = "delete " + name
	}
	return b.writeNamed(ctx, "delete", summary, cardIDs(card), p, nil)
}

// refuseProjectCard answers "not found" for the file of a project card:
// the board no longer reads one (H15), so no write may reach one either — an
// id handed in from outside the loaded board, by an embedder or a stale
// client, must not delete or rewrite a file the repository keeps as it is.
// A path with no file is left for the write to decide.
func (b *Backend) refuseProjectCard(ctx context.Context, p, id string) error {
	data, ok, err := b.read(ctx, p)
	if err != nil || !ok {
		return err
	}
	if f, err := DecodeCard(id, data); err == nil && f.ProjectCard {
		return fmt.Errorf("%w: %s", ErrCardNotFound, id)
	}
	return nil
}

// StubName is how a roster entry is named in a commit subject — "team
// «portal»" — or "" for a card.
func StubName(c board.Card) string {
	if c.Title == board.SprintStateTitle {
		return "team «" + c.Team + "»"
	}
	return ""
}

func cardIDs(c board.Card) []string {
	if isStub(c) {
		return nil
	}
	return []string{c.ItemID}
}

func isStub(c board.Card) bool {
	return c.Title == board.SprintStateTitle
}

// MoveCard gives the card (or stub) a rank between afterID and its
// successor in its own list — one file rewritten, nothing renumbered — or,
// when that gap is exhausted, renumbers a bounded window around the place
// in the same commit (board.PlaceAfter, G12).
func (b *Backend) MoveCard(ctx context.Context, _ board.Board, card board.Card, afterID string) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	var list []board.Ranked
	for _, r := range b.rankedList(s, card) {
		list = append(list, board.Ranked{ID: r.id, Rank: r.rank})
	}
	changes, err := board.PlaceAfter(list, card.ItemID, afterID)
	if err != nil {
		return err
	}
	return b.writeRanks(ctx, card, changes)
}

// setCardRank writes the card's rank as given — the MultiBackend's keys,
// worked out over every domain's cards.
func (b *Backend) setCardRank(ctx context.Context, card board.Card, rank string) error {
	return b.setRank(ctx, card, rank)
}

// writeRanks writes a move's keys: the mover's, and its window's when the
// gap was renumbered — in ONE commit (G12). Under the caller's scope the
// rewrites join its commit; bare, several get one of their own.
func (b *Backend) writeRanks(ctx context.Context, card board.Card, changes []board.RankChange) error {
	flush := func() (plumbing.Hash, error) { return plumbing.ZeroHash, nil }
	if scopeOf(ctx) == nil && len(changes) > 1 {
		ctx, flush = WithScope(ctx, Action{Name: "move", Actor: actorOf(ctx), At: b.now(), Cards: cardIDs(card),
			Summary: "move " + card.ItemID + " (run renumbered)"})
	}
	for _, ch := range changes {
		target := card
		if ch.ID != card.ItemID {
			target = board.Card{ItemID: ch.ID, Title: card.Title}
		}
		if err := b.setRank(ctx, target, ch.After); err != nil {
			return err
		}
	}
	_, err := flush()
	return err
}

type ranked struct{ id, rank string }

// rankedList is the list the card sorts in, in current order.
func (b *Backend) rankedList(s Snapshot, c board.Card) []ranked {
	var out []ranked
	switch c.Title {
	case board.SprintStateTitle:
		for _, t := range s.Teams {
			out = append(out, ranked{t.ID, t.Rank})
		}
	default:
		for _, x := range s.Cards {
			out = append(out, ranked{x.ItemID, x.Rank})
		}
	}
	return out
}

// setRank rewrites the one file that carries the rank.
func (b *Backend) setRank(ctx context.Context, c board.Card, rank string) error {
	p, err := pathFor(c)
	if err != nil {
		return err
	}
	if c.Title == board.SprintStateTitle {
		return b.editTeam(ctx, "move", p, func(f *TeamFile) { f.Rank = rank })
	}
	return b.editCardAt(ctx, "move", p, c.ItemID, func(f *CardFile) { f.Card.Rank = rank })
}

// ---- editing files -----------------------------------------------------------------------

func (b *Backend) editCardAt(ctx context.Context, op, p, id string, fn func(*CardFile)) error {
	data, ok, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrCardNotFound, id)
	}
	f, err := DecodeCard(id, data)
	if err != nil {
		return err
	}
	if f.ProjectCard {
		return fmt.Errorf("%w: %s", ErrCardNotFound, id)
	}
	fn(&f)
	out, err := EncodeCard(f)
	if err != nil {
		return err
	}
	return b.write(ctx, op, []string{id}, p, out)
}

// editCard edits a card or task by id.
func (b *Backend) editCard(ctx context.Context, op string, c board.Card, fn func(*CardFile)) error {
	p, err := pathFor(c)
	if err != nil {
		return err
	}
	return b.editCardAt(ctx, op, p, c.ItemID, fn)
}

func (b *Backend) editTeam(ctx context.Context, op, p string, fn func(*TeamFile)) error {
	data, _, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	f, err := DecodeTeam(data)
	if err != nil {
		return err
	}
	fn(&f)
	out, err := EncodeTeam(f)
	if err != nil {
		return err
	}
	return b.write(ctx, op, nil, p, out)
}

// editUser rewrites users/<login>.yaml, creating it when the person has no
// file yet: a capacity is set for anyone on the board, and most people have
// no file.
func (b *Backend) editUser(ctx context.Context, op, login string, fn func(*UserFile)) error {
	p := UserPath(login)
	data, ok, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	f := UserFile{Created: b.now().UTC().Format(time.RFC3339)}
	if ok {
		if f, err = DecodeUser(data); err != nil {
			return err
		}
	}
	fn(&f)
	out, err := EncodeUser(f)
	if err != nil {
		return err
	}
	return b.write(ctx, op, nil, p, out)
}

// SetPersonCapacity records the points a week a lead set for a person (0
// takes it back: the board then has no number for them at all).
func (b *Backend) SetPersonCapacity(ctx context.Context, _ board.Board, login string, points int) error {
	return b.editUser(ctx, "capacity", login, func(f *UserFile) { f.Capacity = points })
}

// SetTeamPoints records the points a week a lead set for a team, in the team's
// own file (0 takes it back). The team must exist:
// a capacity is said about something already declared.
func (b *Backend) SetTeamPoints(ctx context.Context, _ board.Board, team string, points int) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	id := ""
	for _, t := range s.Teams {
		if t.Name == team {
			id = t.ID
			break
		}
	}
	if id == "" {
		if sc := scopeOf(ctx); sc != nil {
			id = sc.teams[team]
		}
	}
	if id == "" && team == "" {
		id = "_"
	}
	if id == "" {
		return fmt.Errorf("%w: %s", ErrNotFound, team)
	}
	return b.editTeam(ctx, "capacity", TeamPath(id), func(f *TeamFile) { f.Capacity.Points = points })
}

// ---- notes and events ----------------------------------------------------------------

// AddNote appends a note with its own id, the actor and the time.
func (b *Backend) AddNote(ctx context.Context, _ board.Board, card board.Card, text string) error {
	now := b.now()
	return b.editCard(ctx, "note", card, func(f *CardFile) {
		f.Card.Notes = append(f.Card.Notes, board.Note{ID: NewID(now), Body: text, CreatedAt: now.UTC().Format(time.RFC3339), Author: board.ActorFrom(ctx), Source: "draft"})
	})
}

// EditNote rewrites one note's text.
func (b *Backend) EditNote(ctx context.Context, _ board.Board, card board.Card, note board.Note, text string) error {
	found := false
	err := b.editCard(ctx, "note-edit", card, func(f *CardFile) {
		for i := range f.Card.Notes {
			if f.Card.Notes[i].ID == note.ID {
				f.Card.Notes[i].Body = text
				found = true
			}
		}
	})
	if err == nil && !found {
		return fmt.Errorf("%w: %s", ErrNoteNotFound, note.ID)
	}
	return err
}

// DeleteNote removes one note.
func (b *Backend) DeleteNote(ctx context.Context, _ board.Board, card board.Card, note board.Note) error {
	found := false
	err := b.editCard(ctx, "note-delete", card, func(f *CardFile) {
		kept := f.Card.Notes[:0]
		for _, n := range f.Card.Notes {
			if n.ID == note.ID {
				found = true
				continue
			}
			kept = append(kept, n)
		}
		f.Card.Notes = kept
	})
	if err == nil && !found {
		return fmt.Errorf("%w: %s", ErrNoteNotFound, note.ID)
	}
	return err
}

// AppendEvent has no file to write: a commit is the event. Inside a scope
// the event becomes an Aeman-Change trailer — the way a payload that is not
// a field diff (a reviewer, a subtask's title) reaches the log. Outside a
// scope the field diff already says it, and this is a no-op.
func (b *Backend) AppendEvent(ctx context.Context, _ board.Board, card board.Card, e board.Event) error {
	if sc := scopeOf(ctx); sc != nil {
		sc.mu.Lock()
		defer sc.mu.Unlock()
		sc.part(b.repo) // the trailer rides this repository's commit
		sc.changes = append(sc.changes, Change{Card: card.ItemID, Kind: e.Kind, From: e.From, To: e.To})
		sc.cards[card.ItemID] = true
	}
	return nil
}

// CardLog is the card's feed read from this repository's commits — each
// field change an event with the commit's actor and time — and the horizon
// a shallow clone cuts it at (zero when whole). MultiBackend.CardLog is the
// cross-domain version that follows a move.
func (b *Backend) CardLog(_ context.Context, _ board.Board, id string) ([]board.Event, time.Time, error) {
	return eventsFrom(b.repo.CardLog(id, 0))
}

// CardLogSince is CardLog cut at a boundary — the day feed's read.
func (b *Backend) CardLogSince(_ context.Context, _ board.Board, id string, since time.Time) ([]board.Event, time.Time, error) {
	return eventsFrom(b.repo.CardLogSince(id, since))
}

// eventsFrom flattens a log's entries into events.
func eventsFrom(log Log, err error) ([]board.Event, time.Time, error) {
	if err != nil {
		return nil, time.Time{}, err
	}
	var events []board.Event
	for _, e := range log.Entries {
		events = append(events, eventsOf(e)...)
	}
	return events, log.TruncatedBefore, nil
}

// ---- setters ----------------------------------------------------------------------------

// SetDescription replaces the free-form description.
func (b *Backend) SetDescription(ctx context.Context, _ board.Board, card board.Card, description string) error {
	return b.editCard(ctx, "description", card, func(f *CardFile) { f.Card.Description = description })
}

// RenameCard changes the title.
func (b *Backend) RenameCard(ctx context.Context, _ board.Board, card board.Card, title string) error {
	return b.editCard(ctx, "rename", card, func(f *CardFile) { f.Card.Title = title })
}

// SetStage sets or clears the explicit stage.
func (b *Backend) SetStage(ctx context.Context, _ board.Board, card board.Card, stage board.StageKey) error {
	return b.editCard(ctx, "stage", card, func(f *CardFile) { f.Card.Stage = stage })
}

// SetProgress sets the readiness. Reaching 100 remembers where the card came
// from (doneFrom) and the board day it happened (doneAt); dropping below 100
// forgets both — that is what a reopen is.
func (b *Backend) SetProgress(ctx context.Context, _ board.Board, card board.Card, progress int) error {
	return b.editCard(ctx, "progress", card, func(f *CardFile) {
		switch {
		case progress >= 100 && f.Card.Progress < 100:
			f.Card.DoneFrom = f.Card.Progress
			f.Card.DoneAt = board.LocalDateIso(b.now().UTC().Format(time.RFC3339))
		case progress < 100:
			f.Card.DoneFrom = 0
			f.Card.DoneAt = ""
		}
		f.Card.Progress = progress
	})
}

// SetDoneAt writes the board day the card counts as finished on, without
// touching the progress that normally sets it.
func (b *Backend) SetDoneAt(ctx context.Context, _ board.Board, card board.Card, day string) error {
	return b.editCard(ctx, "done-at", card, func(f *CardFile) { f.Card.DoneAt = day })
}

// SetZone sets or clears the zone.
func (b *Backend) SetZone(ctx context.Context, _ board.Board, card board.Card, zone board.ZoneKey) error {
	return b.editCard(ctx, "zone", card, func(f *CardFile) { f.Card.Zone = zone })
}

// SetSize sets or clears the size.
func (b *Backend) SetSize(ctx context.Context, _ board.Board, card board.Card, size board.SizeKey) error {
	return b.editCard(ctx, "size", card, func(f *CardFile) { f.Card.Size = size })
}

// SetDay sets or clears the end day.
func (b *Backend) SetDay(ctx context.Context, _ board.Board, card board.Card, day string) error {
	return b.editCard(ctx, "day", card, func(f *CardFile) { f.Card.Day = day })
}

// SetStart sets or clears the start day.
func (b *Backend) SetStart(ctx context.Context, _ board.Board, card board.Card, date string) error {
	return b.editCard(ctx, "start", card, func(f *CardFile) { f.Card.StartDate = date })
}

// SetSprintStart sets or clears the sprint membership.
func (b *Backend) SetSprintStart(ctx context.Context, _ board.Board, card board.Card, date string) error {
	return b.editCard(ctx, "sprint", card, func(f *CardFile) { f.Card.SprintStart = date })
}

// SetTeam sets or clears the team.
func (b *Backend) SetTeam(ctx context.Context, _ board.Board, card board.Card, team string) error {
	if card.Title == board.SprintStateTitle {
		// The team's own stub: this is the rename of the team file.
		return b.editTeam(ctx, "rename-team", TeamPath(card.ItemID), func(f *TeamFile) { f.Name = team })
	}
	return b.editCard(ctx, "team", card, func(f *CardFile) { f.Card.Team = team })
}

// SetBacklog puts the card on its team's shelf, or takes it off.
func (b *Backend) SetBacklog(
	ctx context.Context, _ board.Board, card board.Card, parked bool,
) error {
	return b.editCard(ctx, "backlog", card, func(f *CardFile) { f.Card.Parked = parked })
}

// SetRecurrence sets a recurrent card's cycle.
func (b *Backend) SetRecurrence(ctx context.Context, _ board.Board, card board.Card, cycle string) error {
	return b.editCard(ctx, "recurrence", card, func(f *CardFile) { f.Card.Recurrence = cycle })
}

// SetAssignee sets the single assignee ("" clears).
func (b *Backend) SetAssignee(ctx context.Context, _ board.Board, card board.Card, login string) error {
	return b.editCard(ctx, "assignee", card, func(f *CardFile) {
		if login == "" {
			f.Card.Assignees = nil
		} else {
			f.Card.Assignees = []string{login}
		}
	})
}

// SetParent sets or clears the parent link.
func (b *Backend) SetParent(ctx context.Context, _ board.Board, card board.Card, parent string) error {
	return b.editCard(ctx, "parent", card, func(f *CardFile) { f.Card.Parent = parent })
}

// SetReviewOf sets or clears the review link.
func (b *Backend) SetReviewOf(ctx context.Context, _ board.Board, card board.Card, reviewOf string) error {
	return b.editCard(ctx, "review-of", card, func(f *CardFile) { f.Card.ReviewOf = reviewOf })
}

// SetReviewRound sets the review round.
func (b *Backend) SetReviewRound(ctx context.Context, _ board.Board, card board.Card, round int) error {
	return b.editCard(ctx, "review-round", card, func(f *CardFile) { f.Card.ReviewRound = round })
}

// SetSprintState writes a team's sprint pointer, creating the team file if
// the team is new. The no-team group is the file "_".

func (b *Backend) SetSprintState(ctx context.Context, _ board.Board, team, current, previous string) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	id := ""
	for _, t := range s.Teams {
		if t.Name == team {
			id = t.ID
			break
		}
	}
	if id == "" {
		if sc := scopeOf(ctx); sc != nil {
			id = sc.teams[team]
		}
	}
	if id == "" {
		if team == "" {
			id = "_"
		} else {
			id = NewID(b.now())
		}
		rank, _ := board.RankBetween(lastRank(len(s.Teams), func(i int) string { return s.Teams[i].Rank }), "")
		if sc := scopeOf(ctx); sc != nil {
			sc.teams[team] = id
		}
		data, err := EncodeTeam(TeamFile{Name: team, Rank: rank, Created: b.now().UTC().Format(time.RFC3339),
			Sprint: SprintPointer{Current: current, Previous: previous}})
		if err != nil {
			return err
		}
		return b.write(ctx, "sprint-state", nil, TeamPath(id), data)
	}
	return b.editTeam(ctx, "sprint-state", TeamPath(id), func(f *TeamFile) {
		f.Sprint = SprintPointer{Current: current, Previous: previous}
	})
}

// customersOf is the roster in board order: by rank, then by creation, then
// by id, so two entries a direct writer gave one rank still read the same
// way everywhere.
func customersOf(in []Customer) []board.Customer {
	sorted := slices.Clone(in)
	slices.SortStableFunc(sorted, func(a, b Customer) int {
		if c := strings.Compare(a.Rank, b.Rank); c != 0 {
			return c
		}
		if c := strings.Compare(a.Created, b.Created); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	var out []board.Customer
	for _, c := range sorted {
		out = append(out, board.Customer{ID: c.ID, Name: c.Name, Rank: c.Rank})
	}
	return out
}

// ---- ownership: docs/design/ownership.md ------------------------------------

// ErrCustomerNotFound is a customer id the roster does not hold.
var ErrCustomerNotFound = errors.New("gitstore: customer not found")

// SetOwner writes a planning card's owner ("" takes it off).
func (b *Backend) SetOwner(ctx context.Context, _ board.Board, card board.Card, login string) error {
	return b.editCard(ctx, "owner", card, func(f *CardFile) { f.Card.Owner = login })
}

// SetCustomers writes the customers a card names (ids; empty takes them
// all off).
func (b *Backend) SetCustomers(ctx context.Context, _ board.Board, card board.Card, ids []string) error {
	return b.editCard(ctx, "customers", card, func(f *CardFile) { f.Card.Customers = slices.Clone(ids) })
}

// SetDependsOn writes the cards a card waits for (ids; empty takes them
// all off).
func (b *Backend) SetDependsOn(ctx context.Context, _ board.Board, card board.Card, ids []string) error {
	return b.editCard(ctx, "depends-on", card, func(f *CardFile) { f.Card.DependsOn = slices.Clone(ids) })
}

// AddCustomer declares a customer, last in the roster.
func (b *Backend) AddCustomer(ctx context.Context, _ board.Board, name string) (board.Customer, error) {
	s, err := b.snapshot(ctx)
	if err != nil {
		return board.Customer{}, err
	}
	last := ""
	for _, c := range s.Customers {
		if c.Rank > last {
			last = c.Rank
		}
	}
	rank, err := board.RankBetween(last, "")
	if err != nil {
		return board.Customer{}, err
	}
	now := b.now()
	id := NewID(now)
	out, err := EncodeCustomer(CustomerFile{Name: name, Rank: rank, Created: now.UTC().Format(time.RFC3339)})
	if err != nil {
		return board.Customer{}, err
	}
	if err := b.writeNamed(ctx, "add-customer", "add customer «"+name+"»", nil, CustomerPath(id), out); err != nil {
		return board.Customer{}, err
	}
	return board.Customer{ID: id, Name: name, Rank: rank}, nil
}

// editCustomer rewrites one customer file; summary names the change from
// the file as it was and as it will be.
func (b *Backend) editCustomer(ctx context.Context, op, id string, fn func(*CustomerFile), summary func(was, now CustomerFile) string) error {
	p := CustomerPath(id)
	data, ok, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrCustomerNotFound, id)
	}
	f, err := DecodeCustomer(data)
	if err != nil {
		return err
	}
	was := f
	fn(&f)
	out, err := EncodeCustomer(f)
	if err != nil {
		return err
	}
	return b.writeNamed(ctx, op, summary(was, f), nil, p, out)
}

// RenameCustomer renames a customer: one file, and every card that names it
// reads the new name at once (cards hold the id).
func (b *Backend) RenameCustomer(ctx context.Context, _ board.Board, id, name string) error {
	return b.editCustomer(ctx, "rename-customer", id, func(f *CustomerFile) { f.Name = name }, func(was, now CustomerFile) string {
		return "rename customer «" + was.Name + "» to «" + now.Name + "»"
	})
}

// MoveCustomer puts a customer right after afterID in the roster ("" = first).
func (b *Backend) MoveCustomer(ctx context.Context, _ board.Board, id, afterID string) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	order := customersOf(s.Customers)
	prev, next := "", ""
	if afterID == "" {
		for _, c := range order {
			if c.ID != id {
				next = c.Rank
				break
			}
		}
	} else {
		for i, c := range order {
			if c.ID != afterID {
				continue
			}
			prev = c.Rank
			for _, n := range order[i+1:] {
				if n.ID != id {
					next = n.Rank
					break
				}
			}
			break
		}
	}
	if prev != "" && next != "" && prev >= next {
		next = ""
	}
	rank, err := board.RankBetween(prev, next)
	if err != nil {
		return err
	}
	return b.editCustomer(ctx, "move-customer", id, func(f *CustomerFile) { f.Rank = rank }, func(_, now CustomerFile) string {
		return "move customer «" + now.Name + "»"
	})
}

// DeleteCustomer takes a customer out of the roster and off every card of
// this repository that names it — the roster file and the cards in ONE
// commit, so no reader ever sees a card naming a customer that is gone.
func (b *Backend) DeleteCustomer(ctx context.Context, _ board.Board, id string) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	var writes []FileWrite
	var cards []string
	for _, c := range s.Cards {
		if !slices.Contains(c.Customers, id) {
			continue
		}
		p, err := CardPath(c.ItemID)
		if err != nil {
			return err
		}
		data, ok, err := b.read(ctx, p)
		if err != nil || !ok {
			continue
		}
		f, err := DecodeCard(c.ItemID, data)
		if err != nil {
			return err
		}
		f.Card.Customers = slices.DeleteFunc(f.Card.Customers, func(x string) bool { return x == id })
		out, err := EncodeCard(f)
		if err != nil {
			return err
		}
		writes = append(writes, FileWrite{Path: p, Data: out})
		cards = append(cards, c.ItemID)
	}
	summary := "delete customer " + id
	if data, ok, err := b.read(ctx, CustomerPath(id)); err != nil {
		return err
	} else if ok {
		if f, err := DecodeCustomer(data); err == nil && f.Name != "" {
			summary = "delete customer «" + f.Name + "»"
		}
		writes = append(writes, FileWrite{Path: CustomerPath(id), Data: nil})
	}
	return b.writeAll(ctx, "delete-customer", summary, cards, writes)
}

// writeAll records several file changes as one: staged under a scope, else
// committed together as action op.
func (b *Backend) writeAll(ctx context.Context, op, summary string, cards []string, writes []FileWrite) error {
	if len(writes) == 0 {
		return nil
	}
	if scopeOf(ctx) != nil {
		for _, w := range writes {
			if err := b.write(ctx, op, cards, w.Path, w.Data); err != nil {
				return err
			}
		}
		return nil
	}
	if summary == "" {
		summary = op
	}
	_, err := b.repo.Commit(Action{Name: op, Actor: actorOf(ctx), At: b.now(), Cards: cards, Summary: summary}, writes)
	return err
}
