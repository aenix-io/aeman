package gitstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Backend is boardservice.Backend over one domain's repository. The service
// speaks the state-card protocol it always has — a stub card whose Title is
// an aeman:*-state marker stands for a team, epic, project, deadline or
// process — and the backend routes those onto roster files. Every mutation
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
	ErrProjectNotFound = errors.New("gitstore: project not found")
	ErrProcessNotFound = errors.New("gitstore: process not found")
	ErrNoteNotFound    = errors.New("gitstore: note not found")
	ErrCardNotFound    = errors.New("gitstore: card not found")
)

// ---- scope: one action, one commit ------------------------------------------

type scopeKey struct{}

// scope collects an action's writes so they commit together.
type scope struct {
	mu      sync.Mutex
	action  Action
	staged  map[string][]byte // path → content; nil = delete
	order   []string
	changes []Change
	cards   map[string]bool
	repo    *Repo
	// roster names created inside the scope, so a later write in the same
	// action can find them before anything is committed.
	projects  map[string]string
	processes map[string]string
	teams     map[string]string
}

// WithScope opens an action on the context: every backend write made with
// the returned context is staged, and flush commits them as one commit
// carrying the action's trailers. A flush with nothing staged makes no
// commit and returns the zero hash.
func WithScope(ctx context.Context, a Action) (context.Context, func() (plumbing.Hash, error)) {
	sc := &scope{action: a, staged: map[string][]byte{}, cards: map[string]bool{},
		projects: map[string]string{}, processes: map[string]string{}, teams: map[string]string{}}
	for _, id := range a.Cards {
		sc.cards[id] = true
	}
	flush := func() (plumbing.Hash, error) {
		sc.mu.Lock()
		defer sc.mu.Unlock()
		if sc.repo == nil || len(sc.order) == 0 {
			return plumbing.ZeroHash, nil
		}
		writes := make([]FileWrite, 0, len(sc.order))
		for _, p := range sc.order {
			writes = append(writes, FileWrite{Path: p, Data: sc.staged[p]})
		}
		act := sc.action
		act.Changes = append(act.Changes, sc.changes...)
		act.Cards = append([]string(nil), a.Cards...)
		for id := range sc.cards {
			if !contains(act.Cards, id) {
				act.Cards = append(act.Cards, id)
			}
		}
		if act.Actor == "" {
			act.Actor = board.ActorFrom(ctx)
		}
		return sc.repo.Commit(act, writes)
	}
	return context.WithValue(ctx, scopeKey{}, sc), flush
}

func scopeOf(ctx context.Context) *scope {
	sc, _ := ctx.Value(scopeKey{}).(*scope)
	return sc
}

// read returns a file as the action currently sees it: staged content first,
// then the tree. ok is false when the file does not exist (or is staged for
// deletion).
func (b *Backend) read(ctx context.Context, p string) ([]byte, bool, error) {
	if sc := scopeOf(ctx); sc != nil {
		sc.mu.Lock()
		data, staged := sc.staged[p]
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
	if sc := scopeOf(ctx); sc != nil {
		sc.mu.Lock()
		defer sc.mu.Unlock()
		sc.repo = b.repo
		if _, seen := sc.staged[p]; !seen {
			sc.order = append(sc.order, p)
		}
		sc.staged[p] = data
		for _, id := range cards {
			sc.cards[id] = true
		}
		return nil
	}
	summary := op
	if len(cards) == 1 {
		summary = op + " " + cards[0]
	}
	_, err := b.repo.Commit(Action{Name: op, Actor: board.ActorFrom(ctx), At: b.now(), Cards: cards, Summary: summary}, []FileWrite{{Path: p, Data: data}})
	return err
}

// ---- loading -----------------------------------------------------------------------

// LoadBoard reads the domain and hands the service the shape it expects:
// the roster files become the state cards NewBoard splits back out, so the
// duplicate and ordering rules stay in one place.
func (b *Backend) LoadBoard(_ context.Context, owner string, project int) (board.Board, error) {
	s, err := Load(b.repo)
	if err != nil {
		return board.Board{}, err
	}
	cards := make([]board.Card, 0, len(s.Cards)+len(s.Teams)+len(s.Projects)*4)
	for _, t := range s.Teams {
		cards = append(cards, board.Card{ItemID: t.ID, Title: board.SprintStateTitle, Team: t.Name,
			SprintStart: t.Sprint.Current, StartDate: t.Sprint.Previous, Rank: t.Rank, CreatedAt: t.Created})
	}
	for _, p := range s.Projects {
		cards = append(cards, board.Card{ItemID: p.ID, Title: board.ProjectStateTitle, Project: p.Name, Rank: p.Rank, CreatedAt: p.Created})
	}
	for _, p := range s.Projects {
		for _, e := range p.Epics {
			cards = append(cards, board.Card{ItemID: e.ID, Title: board.EpicStateTitle, Epic: e.Name, Project: p.Name, Rank: e.Rank, CreatedAt: e.Created})
		}
		for _, d := range p.Deadlines {
			cards = append(cards, board.Card{ItemID: d.ID, Title: board.DeadlineStateTitle, Week: d.Week, Project: p.Name, CreatedAt: d.Created})
		}
	}
	for _, pr := range s.Processes {
		cards = append(cards, board.Card{ItemID: pr.ID, Title: board.ProcessStateTitle, Process: pr.Name, Project: pr.Project, Paused: pr.Paused, Rank: pr.Rank, CreatedAt: pr.Created})
		for _, k := range pr.Tasks {
			c := k.Card
			c.ItemID = k.ID
			c.Title = board.ProcessTaskTitle
			c.Process = pr.Name
			cards = append(cards, c)
		}
	}
	cards = append(cards, s.Cards...)
	bd := board.NewBoard(nil, cards)
	bd.Owner = owner
	bd.Number = project
	bd.Title = s.Board.Title
	return bd, nil
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
		out = append(out, f.Card)
	}
	return out, nil
}

// ---- creating ------------------------------------------------------------------------

// CreateCard creates a card — or, for a marker title, the roster entry the
// stub stands for — and returns it with its new id.
func (b *Backend) CreateCard(ctx context.Context, _ board.Board, in board.CreateInput) (board.Card, error) {
	now := b.now()
	id := NewID(now)
	created := now.UTC().Format(time.RFC3339)
	s, err := b.snapshot(ctx)
	if err != nil {
		return board.Card{}, err
	}
	switch in.Title {
	case board.ProjectStateTitle:
		rank, _ := board.RankBetween(lastRank(len(s.Projects), func(i int) string { return s.Projects[i].Rank }), "")
		data, err := EncodeProject(ProjectFile{Name: in.Project, Rank: rank, Created: created})
		if err != nil {
			return board.Card{}, err
		}
		if sc := scopeOf(ctx); sc != nil {
			sc.projects[in.Project] = id
		}
		return board.Card{ItemID: id, Title: in.Title, Project: in.Project, Rank: rank, CreatedAt: created},
			b.write(ctx, "add-project", nil, ProjectPath(id), data)
	case board.EpicStateTitle:
		pid, pr, err := b.projectByName(ctx, s, in.Project)
		if err != nil {
			return board.Card{}, err
		}
		rank, _ := board.RankBetween(lastRank(len(pr.Epics), func(i int) string { return pr.Epics[i].Rank }), "")
		data, err := EncodeEpic(EpicFile{Name: in.Epic, Rank: rank, Created: created})
		if err != nil {
			return board.Card{}, err
		}
		return board.Card{ItemID: id, Title: in.Title, Epic: in.Epic, Project: in.Project, Rank: rank, CreatedAt: created},
			b.write(ctx, "add-epic", nil, EpicPath(pid, id), data)
	case board.DeadlineStateTitle:
		pid, _, err := b.projectByName(ctx, s, in.Project)
		if err != nil {
			return board.Card{}, err
		}
		data, err := EncodeDeadline(DeadlineFile{Week: in.Week, Created: created})
		if err != nil {
			return board.Card{}, err
		}
		return board.Card{ItemID: id, Title: in.Title, Week: in.Week, Project: in.Project, CreatedAt: created},
			b.write(ctx, "add-deadline", nil, DeadlinePath(pid, id), data)
	case board.ProcessStateTitle:
		rank, _ := board.RankBetween(lastRank(len(s.Processes), func(i int) string { return s.Processes[i].Rank }), "")
		data, err := EncodeProcess(ProcessFile{Name: in.Process, Project: in.Project, Paused: in.Paused, Rank: rank, Created: created})
		if err != nil {
			return board.Card{}, err
		}
		if sc := scopeOf(ctx); sc != nil {
			sc.processes[in.Process] = id
		}
		return board.Card{ItemID: id, Title: in.Title, Process: in.Process, Project: in.Project, Paused: in.Paused, Rank: rank, CreatedAt: created},
			b.write(ctx, "add-process", nil, ProcessPath(id), data)
	case board.ProcessTaskTitle:
		pid, pr, err := b.processByName(ctx, s, in.Process)
		if err != nil {
			return board.Card{}, err
		}
		c := cardFromInput(in, id, created, board.ActorFrom(ctx))
		c.Rank, _ = board.RankBetween(lastRank(len(pr.Tasks), func(i int) string { return pr.Tasks[i].Card.Rank }), "")
		data, err := EncodeCard(CardFile{Card: c})
		if err != nil {
			return board.Card{}, err
		}
		return c, b.write(ctx, "add-task", []string{id}, TaskPath(pid, id), data)
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
		ItemID: id, Title: in.Title, Zone: in.Zone, Day: in.Day, StartDate: in.Start, SprintStart: in.SprintStart,
		Team: in.Team, ReviewOf: in.ReviewOf, Parent: in.Parent, Plan: in.Plan, Week: in.Week, Epic: in.Epic,
		Project: in.Project, Process: in.Process, Task: in.Task, Recurrence: in.Recurrence, Paused: in.Paused,
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

func (b *Backend) projectByName(ctx context.Context, s Snapshot, name string) (string, Project, error) {
	for _, p := range s.Projects {
		if p.Name == name {
			return p.ID, p, nil
		}
	}
	if sc := scopeOf(ctx); sc != nil {
		if id, ok := sc.projects[name]; ok {
			return id, Project{ID: id}, nil
		}
	}
	return "", Project{}, fmt.Errorf("%w: %q", ErrProjectNotFound, name)
}

func (b *Backend) processByName(ctx context.Context, s Snapshot, name string) (string, Process, error) {
	for _, p := range s.Processes {
		if p.Name == name {
			return p.ID, p, nil
		}
	}
	if sc := scopeOf(ctx); sc != nil {
		if id, ok := sc.processes[name]; ok {
			return id, Process{ID: id}, nil
		}
	}
	return "", Process{}, fmt.Errorf("%w: %q", ErrProcessNotFound, name)
}

// pathFor is where a card or stub lives.
func (b *Backend) pathFor(ctx context.Context, s Snapshot, c board.Card) (string, error) {
	switch c.Title {
	case board.SprintStateTitle:
		return TeamPath(c.ItemID), nil
	case board.ProjectStateTitle:
		return ProjectPath(c.ItemID), nil
	case board.EpicStateTitle:
		pid, err := b.projectOfChild(ctx, s, c, func(p Project) bool {
			for _, e := range p.Epics {
				if e.ID == c.ItemID {
					return true
				}
			}
			return false
		})
		if err != nil {
			return "", err
		}
		return EpicPath(pid, c.ItemID), nil
	case board.DeadlineStateTitle:
		pid, err := b.projectOfChild(ctx, s, c, func(p Project) bool {
			for _, d := range p.Deadlines {
				if d.ID == c.ItemID {
					return true
				}
			}
			return false
		})
		if err != nil {
			return "", err
		}
		return DeadlinePath(pid, c.ItemID), nil
	case board.ProcessStateTitle:
		return ProcessPath(c.ItemID), nil
	case board.ProcessTaskTitle:
		for _, p := range s.Processes {
			for _, k := range p.Tasks {
				if k.ID == c.ItemID {
					return TaskPath(p.ID, c.ItemID), nil
				}
			}
		}
		pid, _, err := b.processByName(ctx, s, c.Process)
		if err != nil {
			return "", err
		}
		return TaskPath(pid, c.ItemID), nil
	}
	return CardPath(c.ItemID)
}

// projectOfChild finds the project holding a child by id, falling back to
// the stub's project name.
func (b *Backend) projectOfChild(ctx context.Context, s Snapshot, c board.Card, holds func(Project) bool) (string, error) {
	for _, p := range s.Projects {
		if holds(p) {
			return p.ID, nil
		}
	}
	id, _, err := b.projectByName(ctx, s, c.Project)
	return id, err
}

// ---- deleting and moving -----------------------------------------------------------

// DeleteCard removes a card's file, or a stub's roster file.
func (b *Backend) DeleteCard(ctx context.Context, _ board.Board, card board.Card) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	p, err := b.pathFor(ctx, s, card)
	if err != nil {
		return err
	}
	return b.write(ctx, "delete", cardIDs(card), p, nil)
}

func cardIDs(c board.Card) []string {
	if isStub(c) {
		return nil
	}
	return []string{c.ItemID}
}

func isStub(c board.Card) bool {
	switch c.Title {
	case board.SprintStateTitle, board.ProjectStateTitle, board.EpicStateTitle, board.DeadlineStateTitle, board.ProcessStateTitle:
		return true
	}
	return false
}

// MoveCard gives the card (or stub) a rank between afterID and its
// successor in its own list — one file rewritten, nothing renumbered.
func (b *Backend) MoveCard(ctx context.Context, _ board.Board, card board.Card, afterID string) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	ranks := b.rankedList(s, card)
	prev, next := "", ""
	if afterID == "" {
		if len(ranks) > 0 && ranks[0].id != card.ItemID {
			next = ranks[0].rank
		} else if len(ranks) > 1 {
			next = ranks[1].rank
		}
	} else {
		for i, r := range ranks {
			if r.id != afterID {
				continue
			}
			prev = r.rank
			for j := i + 1; j < len(ranks); j++ {
				if ranks[j].id != card.ItemID {
					next = ranks[j].rank
					break
				}
			}
			break
		}
	}
	if prev != "" && next != "" && prev >= next {
		next = "" // the list's own ranks are out of order; append after prev
	}
	rank, err := board.RankBetween(prev, next)
	if err != nil {
		return err
	}
	return b.setRank(ctx, s, card, rank)
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
	case board.ProjectStateTitle:
		for _, p := range s.Projects {
			out = append(out, ranked{p.ID, p.Rank})
		}
	case board.EpicStateTitle:
		for _, p := range s.Projects {
			for _, e := range p.Epics {
				if e.ID == c.ItemID {
					for _, x := range p.Epics {
						out = append(out, ranked{x.ID, x.Rank})
					}
					return out
				}
			}
		}
	case board.ProcessStateTitle:
		for _, p := range s.Processes {
			out = append(out, ranked{p.ID, p.Rank})
		}
	case board.ProcessTaskTitle:
		for _, p := range s.Processes {
			for _, k := range p.Tasks {
				if k.ID == c.ItemID {
					for _, x := range p.Tasks {
						out = append(out, ranked{x.ID, x.Card.Rank})
					}
					return out
				}
			}
		}
	default:
		for _, x := range s.Cards {
			out = append(out, ranked{x.ItemID, x.Rank})
		}
	}
	return out
}

// setRank rewrites the one file that carries the rank.
func (b *Backend) setRank(ctx context.Context, s Snapshot, c board.Card, rank string) error {
	p, err := b.pathFor(ctx, s, c)
	if err != nil {
		return err
	}
	switch c.Title {
	case board.SprintStateTitle:
		return b.editTeam(ctx, "move", p, func(f *TeamFile) { f.Rank = rank })
	case board.ProjectStateTitle:
		return b.editProject(ctx, "move", p, func(f *ProjectFile) { f.Rank = rank })
	case board.EpicStateTitle:
		return b.editEpic(ctx, "move", p, func(f *EpicFile) { f.Rank = rank })
	case board.ProcessStateTitle:
		return b.editProcess(ctx, "move", p, func(f *ProcessFile) { f.Rank = rank })
	case board.DeadlineStateTitle:
		return nil // deadlines order by week
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
	fn(&f)
	out, err := EncodeCard(f)
	if err != nil {
		return err
	}
	return b.write(ctx, op, []string{id}, p, out)
}

// editCard edits a card or task by id.
func (b *Backend) editCard(ctx context.Context, op string, c board.Card, fn func(*CardFile)) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	p, err := b.pathFor(ctx, s, c)
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

func (b *Backend) editProject(ctx context.Context, op, p string, fn func(*ProjectFile)) error {
	data, ok, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrProjectNotFound, p)
	}
	f, err := DecodeProject(data)
	if err != nil {
		return err
	}
	fn(&f)
	out, err := EncodeProject(f)
	if err != nil {
		return err
	}
	return b.write(ctx, op, nil, p, out)
}

func (b *Backend) editEpic(ctx context.Context, op, p string, fn func(*EpicFile)) error {
	data, ok, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, p)
	}
	f, err := DecodeEpic(data)
	if err != nil {
		return err
	}
	fn(&f)
	out, err := EncodeEpic(f)
	if err != nil {
		return err
	}
	return b.write(ctx, op, nil, p, out)
}

func (b *Backend) editDeadline(ctx context.Context, op, p string, fn func(*DeadlineFile)) error {
	data, ok, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, p)
	}
	f, err := DecodeDeadline(data)
	if err != nil {
		return err
	}
	fn(&f)
	out, err := EncodeDeadline(f)
	if err != nil {
		return err
	}
	return b.write(ctx, op, nil, p, out)
}

func (b *Backend) editProcess(ctx context.Context, op, p string, fn func(*ProcessFile)) error {
	data, ok, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrProcessNotFound, p)
	}
	f, err := DecodeProcess(data)
	if err != nil {
		return err
	}
	fn(&f)
	out, err := EncodeProcess(f)
	if err != nil {
		return err
	}
	return b.write(ctx, op, nil, p, out)
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
		sc.repo = b.repo
		sc.changes = append(sc.changes, Change{Card: card.ItemID, Kind: e.Kind, From: e.From, To: e.To})
		sc.cards[card.ItemID] = true
	}
	return nil
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
// from (doneFrom); dropping below 100 forgets it — that is what a reopen is.
func (b *Backend) SetProgress(ctx context.Context, _ board.Board, card board.Card, progress int) error {
	return b.editCard(ctx, "progress", card, func(f *CardFile) {
		switch {
		case progress >= 100 && f.Card.Progress < 100:
			f.Card.DoneFrom = f.Card.Progress
		case progress < 100:
			f.Card.DoneFrom = 0
		}
		f.Card.Progress = progress
	})
}

// SetZone sets or clears the zone.
func (b *Backend) SetZone(ctx context.Context, _ board.Board, card board.Card, zone board.ZoneKey) error {
	return b.editCard(ctx, "zone", card, func(f *CardFile) { f.Card.Zone = zone })
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

// SetPlan sets or clears the weekly-plan band.
func (b *Backend) SetPlan(ctx context.Context, _ board.Board, card board.Card, plan board.PlanBand) error {
	return b.editCard(ctx, "plan", card, func(f *CardFile) { f.Card.Plan = plan })
}

// SetWeek sets the week — of a card, or of a deadline stub.
func (b *Backend) SetWeek(ctx context.Context, _ board.Board, card board.Card, week string) error {
	if card.Title == board.DeadlineStateTitle {
		s, err := b.snapshot(ctx)
		if err != nil {
			return err
		}
		p, err := b.pathFor(ctx, s, card)
		if err != nil {
			return err
		}
		return b.editDeadline(ctx, "move-deadline", p, func(f *DeadlineFile) { f.Week = week })
	}
	return b.editCard(ctx, "week", card, func(f *CardFile) { f.Card.Week = week })
}

// SetTeam sets or clears the team.
func (b *Backend) SetTeam(ctx context.Context, _ board.Board, card board.Card, team string) error {
	return b.editCard(ctx, "team", card, func(f *CardFile) { f.Card.Team = team })
}

// SetEpic files the card under a column, or renames an epic stub.
func (b *Backend) SetEpic(ctx context.Context, _ board.Board, card board.Card, epic string) error {
	if card.Title == board.EpicStateTitle {
		s, err := b.snapshot(ctx)
		if err != nil {
			return err
		}
		p, err := b.pathFor(ctx, s, card)
		if err != nil {
			return err
		}
		return b.editEpic(ctx, "rename-epic", p, func(f *EpicFile) { f.Name = epic })
	}
	return b.editCard(ctx, "epic", card, func(f *CardFile) { f.Card.Epic = epic })
}

// SetProcess names a process on a stub (rename) or a task (its process).
func (b *Backend) SetProcess(ctx context.Context, _ board.Board, card board.Card, process string) error {
	if card.Title == board.ProcessStateTitle {
		return b.editProcess(ctx, "rename-process", ProcessPath(card.ItemID), func(f *ProcessFile) { f.Name = process })
	}
	return b.editCard(ctx, "process", card, func(f *CardFile) { f.Card.Process = process })
}

// SetTask links an iteration to its task.
func (b *Backend) SetTask(ctx context.Context, _ board.Board, card board.Card, task string) error {
	return b.editCard(ctx, "task", card, func(f *CardFile) { f.Card.Task = task })
}

// SetPaused pauses or resumes a process.
func (b *Backend) SetPaused(ctx context.Context, _ board.Board, card board.Card, paused bool) error {
	if card.Title == board.ProcessStateTitle {
		return b.editProcess(ctx, "pause-process", ProcessPath(card.ItemID), func(f *ProcessFile) { f.Paused = paused })
	}
	return b.editCard(ctx, "paused", card, func(f *CardFile) { f.Card.Paused = paused })
}

// SetAccumulate sets a task's accumulate flag.
func (b *Backend) SetAccumulate(ctx context.Context, _ board.Board, card board.Card, on bool) error {
	return b.editCard(ctx, "accumulate", card, func(f *CardFile) { f.Card.Accumulate = on })
}

// SetProject renames a project stub, re-parents an epic stub, sets a
// process's project, or files a card under a project.
func (b *Backend) SetProject(ctx context.Context, _ board.Board, card board.Card, project string) error {
	switch card.Title {
	case board.ProjectStateTitle:
		return b.editProject(ctx, "rename-project", ProjectPath(card.ItemID), func(f *ProjectFile) { f.Name = project })
	case board.EpicStateTitle:
		return b.moveEpic(ctx, card, project)
	case board.ProcessStateTitle:
		return b.editProcess(ctx, "process-project", ProcessPath(card.ItemID), func(f *ProcessFile) { f.Project = project })
	}
	return b.editCard(ctx, "project", card, func(f *CardFile) { f.Card.Project = project })
}

// moveEpic moves a column to another project: the epic file's path names
// its project, so this is a delete and a create with the same id.
func (b *Backend) moveEpic(ctx context.Context, card board.Card, project string) error {
	s, err := b.snapshot(ctx)
	if err != nil {
		return err
	}
	from, err := b.pathFor(ctx, s, card)
	if err != nil {
		return err
	}
	toPID, _, err := b.projectByName(ctx, s, project)
	if err != nil {
		return err
	}
	data, ok, err := b.read(ctx, from)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, from)
	}
	if err := b.write(ctx, "epic-project", nil, from, nil); err != nil {
		return err
	}
	return b.write(ctx, "epic-project", nil, EpicPath(toPID, card.ItemID), data)
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
