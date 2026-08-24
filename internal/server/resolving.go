package server

import (
	"context"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// resolvingBackend sits between the store and the real backend and swaps
// every provisional "local-…" id — the card's own, and the ids a write can
// carry (a parent, a review target, a task link, a move anchor, a create's
// references) — for the id GitHub answered with. Creation answers from the
// cache before GitHub has spoken, so anything a client does next may name an
// id GitHub has never heard of; by the time a queued write executes, the
// create ahead of it in the FIFO has published the real id here.
type resolvingBackend struct {
	inner boardservice.Backend
	store *boardStore
}

func (r *resolvingBackend) entryOf(b board.Board) *boardEntry {
	return r.store.entry(storeKey(b.Owner, b.Number))
}

func (r *resolvingBackend) card(b board.Board, c board.Card) board.Card {
	e := r.entryOf(b)
	e.mu.Lock()
	defer e.mu.Unlock()
	if real, ok := e.locals[c.ItemID]; ok && real.ItemID != "" {
		c.ItemID, c.ContentID = real.ItemID, real.ContentID
	}
	return c
}

func (r *resolvingBackend) id(b board.Board, id string) string {
	e := r.entryOf(b)
	e.mu.Lock()
	defer e.mu.Unlock()
	if real, ok := e.locals[id]; ok && real.ItemID != "" {
		return real.ItemID
	}
	return id
}

func (r *resolvingBackend) LoadBoard(ctx context.Context, owner string, project int) (board.Board, error) {
	return r.inner.LoadBoard(ctx, owner, project)
}

func (r *resolvingBackend) LoadCards(ctx context.Context, b board.Board, ids []string) ([]board.Card, error) {
	return r.inner.LoadCards(ctx, b, ids)
}

func (r *resolvingBackend) CreateCard(ctx context.Context, b board.Board, in board.CreateInput) (board.Card, error) {
	in.Parent = r.id(b, in.Parent)
	in.ReviewOf = r.id(b, in.ReviewOf)
	in.Task = r.id(b, in.Task)
	return r.inner.CreateCard(ctx, b, in)
}

func (r *resolvingBackend) DeleteCard(ctx context.Context, b board.Board, card board.Card) error {
	return r.inner.DeleteCard(ctx, b, r.card(b, card))
}

func (r *resolvingBackend) MoveCard(ctx context.Context, b board.Board, card board.Card, afterID string) error {
	return r.inner.MoveCard(ctx, b, r.card(b, card), r.id(b, afterID))
}

func (r *resolvingBackend) AddNote(ctx context.Context, b board.Board, card board.Card, text string) error {
	return r.inner.AddNote(ctx, b, r.card(b, card), text)
}

func (r *resolvingBackend) AppendEvent(ctx context.Context, b board.Board, card board.Card, e board.Event) error {
	return r.inner.AppendEvent(ctx, b, r.card(b, card), e)
}

func (r *resolvingBackend) EditNote(ctx context.Context, b board.Board, card board.Card, note board.Note, text string) error {
	return r.inner.EditNote(ctx, b, r.card(b, card), note, text)
}

func (r *resolvingBackend) DeleteNote(ctx context.Context, b board.Board, card board.Card, note board.Note) error {
	return r.inner.DeleteNote(ctx, b, r.card(b, card), note)
}

func (r *resolvingBackend) SetDescription(ctx context.Context, b board.Board, card board.Card, description string) error {
	return r.inner.SetDescription(ctx, b, r.card(b, card), description)
}

func (r *resolvingBackend) RenameCard(ctx context.Context, b board.Board, card board.Card, title string) error {
	return r.inner.RenameCard(ctx, b, r.card(b, card), title)
}

func (r *resolvingBackend) SetStage(ctx context.Context, b board.Board, card board.Card, stage board.StageKey) error {
	return r.inner.SetStage(ctx, b, r.card(b, card), stage)
}

func (r *resolvingBackend) SetProgress(ctx context.Context, b board.Board, card board.Card, progress int) error {
	return r.inner.SetProgress(ctx, b, r.card(b, card), progress)
}

func (r *resolvingBackend) SetZone(ctx context.Context, b board.Board, card board.Card, zone board.ZoneKey) error {
	return r.inner.SetZone(ctx, b, r.card(b, card), zone)
}

func (r *resolvingBackend) SetDay(ctx context.Context, b board.Board, card board.Card, day string) error {
	return r.inner.SetDay(ctx, b, r.card(b, card), day)
}

func (r *resolvingBackend) SetStart(ctx context.Context, b board.Board, card board.Card, date string) error {
	return r.inner.SetStart(ctx, b, r.card(b, card), date)
}

func (r *resolvingBackend) SetSprintStart(ctx context.Context, b board.Board, card board.Card, date string) error {
	return r.inner.SetSprintStart(ctx, b, r.card(b, card), date)
}

func (r *resolvingBackend) SetPlan(ctx context.Context, b board.Board, card board.Card, plan board.PlanBand) error {
	return r.inner.SetPlan(ctx, b, r.card(b, card), plan)
}

func (r *resolvingBackend) SetWeek(ctx context.Context, b board.Board, card board.Card, week string) error {
	return r.inner.SetWeek(ctx, b, r.card(b, card), week)
}

func (r *resolvingBackend) SetTeam(ctx context.Context, b board.Board, card board.Card, team string) error {
	return r.inner.SetTeam(ctx, b, r.card(b, card), team)
}

func (r *resolvingBackend) SetEpic(ctx context.Context, b board.Board, card board.Card, epic string) error {
	return r.inner.SetEpic(ctx, b, r.card(b, card), epic)
}

func (r *resolvingBackend) SetProcess(ctx context.Context, b board.Board, card board.Card, process string) error {
	return r.inner.SetProcess(ctx, b, r.card(b, card), process)
}

func (r *resolvingBackend) SetTask(ctx context.Context, b board.Board, card board.Card, task string) error {
	return r.inner.SetTask(ctx, b, r.card(b, card), r.id(b, task))
}

func (r *resolvingBackend) SetPaused(ctx context.Context, b board.Board, card board.Card, paused bool) error {
	return r.inner.SetPaused(ctx, b, r.card(b, card), paused)
}

func (r *resolvingBackend) SetAccumulate(ctx context.Context, b board.Board, card board.Card, on bool) error {
	return r.inner.SetAccumulate(ctx, b, r.card(b, card), on)
}

func (r *resolvingBackend) SetProject(ctx context.Context, b board.Board, card board.Card, project string) error {
	return r.inner.SetProject(ctx, b, r.card(b, card), project)
}

func (r *resolvingBackend) SetRecurrence(ctx context.Context, b board.Board, card board.Card, cycle string) error {
	return r.inner.SetRecurrence(ctx, b, r.card(b, card), cycle)
}

func (r *resolvingBackend) SetAssignee(ctx context.Context, b board.Board, card board.Card, login string) error {
	return r.inner.SetAssignee(ctx, b, r.card(b, card), login)
}

func (r *resolvingBackend) SetParent(ctx context.Context, b board.Board, card board.Card, parent string) error {
	return r.inner.SetParent(ctx, b, r.card(b, card), r.id(b, parent))
}

func (r *resolvingBackend) SetReviewOf(ctx context.Context, b board.Board, card board.Card, reviewOf string) error {
	return r.inner.SetReviewOf(ctx, b, r.card(b, card), r.id(b, reviewOf))
}

func (r *resolvingBackend) SetReviewRound(ctx context.Context, b board.Board, card board.Card, round int) error {
	return r.inner.SetReviewRound(ctx, b, r.card(b, card), round)
}

func (r *resolvingBackend) SetSprintState(ctx context.Context, b board.Board, team, current, previous string) error {
	return r.inner.SetSprintState(ctx, b, team, current, previous)
}

var _ boardservice.Backend = (*resolvingBackend)(nil)

// ---- optional capabilities ------------------------------------------------
// The wrapper must not HIDE what its inner backend can do: the store probes
// b.inner with type assertions for these, and when the resolving layer landed
// in between, every assertion silently failed — the cheap access probe died
// (every returning user paid the full board load), draft-body sync died, and
// GitHub link resolution died. Each forward delegates when the inner backend
// offers the capability and reports its absence otherwise.

func (r *resolvingBackend) CheckBoardAccess(ctx context.Context, owner string, project int) error {
	if p, ok := r.inner.(interface {
		CheckBoardAccess(ctx context.Context, owner string, project int) error
	}); ok {
		return p.CheckBoardAccess(ctx, owner, project)
	}
	return fmt.Errorf("backend offers no access probe")
}

func (r *resolvingBackend) SyncDraftBody(ctx context.Context, card board.Card, description string, notes []board.Note, events []board.Event) ([]board.Note, []board.Event, error) {
	if s, ok := r.inner.(interface {
		SyncDraftBody(ctx context.Context, card board.Card, description string, notes []board.Note, events []board.Event) ([]board.Note, []board.Event, error)
	}); ok {
		return s.SyncDraftBody(ctx, card, description, notes, events)
	}
	return nil, nil, fmt.Errorf("backend cannot sync draft bodies")
}

func (r *resolvingBackend) ResolveIssueRef(ctx context.Context, link board.Link) (board.Link, error) {
	if l, ok := r.inner.(boardservice.LinkResolver); ok {
		return l.ResolveIssueRef(ctx, link)
	}
	return link, fmt.Errorf("backend cannot resolve github refs")
}
