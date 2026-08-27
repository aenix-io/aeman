package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The git backend behind the store. The cache and the coalescing queue stay
// exactly what they were — the user gets an answer in milliseconds — and
// behind them: the queue's executed writes become local commits, one per
// request (WithScope), a background push sends them, and a fetch on a timer
// brings other replicas' commits into the cache. A rejected push is
// re-applied on the remote's new tip and retried.

// coalesceWindow is how long a coalescable write (the progress slider) waits
// in the queue for the next value before it commits. A package variable so
// tests can shrink it.
var coalesceWindow = 500 * time.Millisecond

// gitOptions configures the sync.
type gitOptions struct {
	// PushDelay debounces the push after a drain; zero pushes at once.
	PushDelay time.Duration
	// SyncInterval is the fetch cadence; zero disables the ticker (tests
	// drive syncNow by hand).
	SyncInterval time.Duration
	Logger       *slog.Logger
}

// gitSync is the per-store sync state.
type gitSync struct {
	repo      *gitstore.Repo
	remote    gitstore.Remote
	pushDelay time.Duration
	log       *slog.Logger

	mu sync.Mutex
	// unpushed are the executed groups the remote has not confirmed, oldest
	// first — what a rebase re-applies. firstUnpushed is when the oldest
	// was committed.
	unpushed      []executedGroup
	firstUnpushed time.Time
	lastPushErr   error
	pushTimer     *time.Timer
	pushing       bool
}

// executedGroup is one action's ops after they committed.
type executedGroup struct {
	action gitstore.Action
	ops    []pendingOp
}

// newGitBackend builds the store's backend over a repository clone.
func newGitBackend(store *boardStore, repo *gitstore.Repo, remote gitstore.Remote, opts gitOptions) *storeBackend {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	be := &storeBackend{
		inner: gitstore.NewBackend(repo, gitstore.BackendOptions{}),
		store: store,
		git:   &gitSync{repo: repo, remote: remote, pushDelay: opts.PushDelay, log: opts.Logger},
	}
	if opts.SyncInterval > 0 {
		go be.runSync(context.Background(), opts.SyncInterval)
	}
	return be
}

// ---- the request's action -----------------------------------------------------------

type actionCtxKey struct{}

// actionRef is what a request is, for the commit it becomes.
type actionRef struct {
	ID   string
	Name string
}

// withAction stamps the request's action on the context: every write made
// with it belongs to one commit.
func withAction(ctx context.Context, id, name string) context.Context {
	return context.WithValue(ctx, actionCtxKey{}, actionRef{ID: id, Name: name})
}

func actionFrom(ctx context.Context) actionRef {
	a, _ := ctx.Value(actionCtxKey{}).(actionRef)
	return a
}

// scopeFor opens the git scope for a group of ops sharing an action.
func (g *gitSync) scopeFor(ctx context.Context, op pendingOp) (context.Context, func() (plumbing.Hash, error), gitstore.Action) {
	name := op.action.Name
	if name == "" {
		name = op.kind
	}
	if name == "" {
		name = "write"
	}
	act := gitstore.Action{Name: name, ID: op.action.ID, Actor: op.actor, At: time.Now(), Summary: op.desc}
	if act.Summary == "" {
		act.Summary = name
	}
	sctx, flush := gitstore.WithScope(ctx, act)
	return sctx, flush, act
}

// sameAction reports whether two ops belong to the same request.
func sameAction(a, b pendingOp) bool {
	return a.action.ID != "" && a.action.ID == b.action.ID
}

// execGroup runs the popped op and every queued op of the same request
// under one git scope, so they land as one commit. On success the group is
// recorded for the push; the in-flight marker covers the whole group.
func (b *storeBackend) execGroup(ctx context.Context, e *boardEntry, first pendingOp) error {
	sctx, flush, act := b.git.scopeFor(ctx, first)
	group := executedGroup{action: act, ops: []pendingOp{first}}
	if err := execRetry(sctx, first); err != nil {
		return err
	}
	for {
		e.mu.Lock()
		if len(e.pending) == 0 || !sameAction(first, e.pending[0]) || time.Now().Before(e.pending[0].notBefore) {
			e.mu.Unlock()
			break
		}
		next := e.pending[0]
		e.pending = e.pending[1:]
		e.mu.Unlock()
		if err := execRetry(sctx, next); err != nil {
			return err
		}
		group.ops = append(group.ops, next)
	}
	hash, err := flush()
	if err != nil {
		return err
	}
	if !hash.IsZero() {
		b.committed(e, group)
	}
	return nil
}

// ---- push ------------------------------------------------------------------------------

// committed records a group that just committed and schedules a push.
func (b *storeBackend) committed(e *boardEntry, group executedGroup) {
	g := b.git
	g.mu.Lock()
	if len(g.unpushed) == 0 {
		g.firstUnpushed = time.Now()
	}
	g.unpushed = append(g.unpushed, group)
	if g.pushDelay > 0 || g.pushTimer != nil {
		if g.pushTimer == nil {
			g.pushTimer = time.AfterFunc(g.pushDelay, func() {
				_ = b.syncNow(context.Background(), storeKeyOf(b.store, e))
			})
		}
	}
	g.mu.Unlock()
}

// unpushedAge is how long the oldest unpushed commit has been waiting; zero
// when everything is on the remote.
func (b *storeBackend) unpushedAge(_ string) time.Duration {
	g := b.git
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.unpushed) == 0 {
		return 0
	}
	return time.Since(g.firstUnpushed)
}

// syncNow runs one sync cycle for the board: push what is unpushed; on a
// rejection fetch, re-apply on the new tip and push again; with nothing to
// push, fetch and adopt what others pushed.
func (b *storeBackend) syncNow(ctx context.Context, key string) error {
	g := b.git
	g.mu.Lock()
	if g.pushing {
		g.mu.Unlock()
		return nil
	}
	g.pushing = true
	if g.pushTimer != nil {
		g.pushTimer.Stop()
		g.pushTimer = nil
	}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.pushing = false
		g.mu.Unlock()
	}()

	e := b.store.entry(key)
	for attempt := 0; ; attempt++ {
		g.mu.Lock()
		hasUnpushed := len(g.unpushed) > 0
		g.mu.Unlock()
		if !hasUnpushed {
			return b.adoptRemote(ctx, e)
		}
		err := g.repo.Push(ctx, g.remote)
		if err == nil {
			g.mu.Lock()
			g.unpushed = nil
			g.lastPushErr = nil
			g.mu.Unlock()
			return nil
		}
		// Rejected or unreachable: the fetch decides which.
		tip, moved, ferr := g.repo.Fetch(ctx, g.remote)
		if ferr != nil || !moved {
			g.mu.Lock()
			g.lastPushErr = err
			g.mu.Unlock()
			if ferr != nil {
				return fmt.Errorf("push failed and the remote is unreachable: %w", err)
			}
			return fmt.Errorf("push failed with nothing new on the remote: %w", err)
		}
		if attempt >= 5 {
			return fmt.Errorf("push kept losing races: %w", err)
		}
		if err := b.rebaseOnto(ctx, e, tip); err != nil {
			return err
		}
		// Backoff with jitter so two replicas do not lock-step.
		time.Sleep(time.Duration(rand.IntN(50)+10*attempt) * time.Millisecond) //nolint:gosec // jitter, not security
	}
}

// adoptRemote fetches and, when the remote moved and nothing local is
// pending, resets onto its tip and reloads the cache — the diff becomes
// watch events for everyone.
func (b *storeBackend) adoptRemote(ctx context.Context, e *boardEntry) error {
	tip, moved, err := b.git.repo.Fetch(ctx, b.git.remote)
	if err != nil {
		return err
	}
	if !moved {
		return nil
	}
	e.mu.Lock()
	busy := len(e.pending) > 0 || e.inflight != nil
	e.mu.Unlock()
	if busy {
		return nil // the drain will bring its own push; the rebase there adopts the tip
	}
	if err := b.git.repo.ResetTo(tip); err != nil {
		return err
	}
	return b.reloadFromTip(ctx, e)
}

// reloadFromTip re-reads the board from the local branch and installs it.
// The recent-card guards exist for a store that answers stale reads; the
// branch tip is never stale — it holds every write we made — so they are
// dropped first, or a card we just re-applied would keep its pre-rebase
// shape in the cache.
func (b *storeBackend) reloadFromTip(ctx context.Context, e *boardEntry) error {
	fresh, err := b.inner.LoadBoard(ctx, e.board.Owner, e.board.Number)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.recentCards = map[string]time.Time{}
	e.recentGone = map[string]time.Time{}
	e.recentMove = time.Time{}
	e.mu.Unlock()
	b.install(e, fresh, "")
	return nil
}

// rebaseOnto moves the local branch to the remote's tip and re-runs every
// unpushed group on top of it — each op re-reads its file, so the result is
// field-level: the other replica's changes to other fields survive.
func (b *storeBackend) rebaseOnto(ctx context.Context, e *boardEntry, tip plumbing.Hash) error {
	g := b.git
	g.mu.Lock()
	groups := g.unpushed
	g.unpushed = nil
	g.mu.Unlock()
	if err := g.repo.ResetTo(tip); err != nil {
		return err
	}
	if err := b.reloadFromTip(ctx, e); err != nil {
		return err
	}
	var kept []executedGroup
	for _, grp := range groups {
		sctx, flush := gitstore.WithScope(context.WithoutCancel(ctx), grp.action)
		for _, op := range grp.ops {
			if err := op.exec(sctx); err != nil {
				g.log.Warn("re-apply dropped a write", "op", op.desc, "err", err)
			}
		}
		if _, err := flush(); err != nil {
			return err
		}
		kept = append(kept, grp)
	}
	// The re-applied groups are unpushed again; the cache is refreshed from
	// the new tip so it agrees with what was just committed.
	g.mu.Lock()
	g.unpushed = append(kept, g.unpushed...)
	if len(g.unpushed) > 0 && g.firstUnpushed.IsZero() {
		g.firstUnpushed = time.Now()
	}
	g.mu.Unlock()
	return b.reloadFromTip(ctx, e)
}

// runSync is the fetch ticker: every interval, one sync cycle per known
// board, until ctx ends.
func (b *storeBackend) runSync(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.store.mu.Lock()
			keys := make([]string, 0, len(b.store.entries))
			for k := range b.store.entries {
				keys = append(keys, k)
			}
			b.store.mu.Unlock()
			for _, k := range keys {
				if err := b.syncNow(ctx, k); err != nil && !errors.Is(err, context.Canceled) {
					b.git.log.Warn("sync", "board", k, "err", err)
				}
			}
		}
	}
}

// mintID is the id a create hands out before its write lands.
func (b *storeBackend) mintID() string {
	return gitstore.NewID(time.Now())
}

// createMinted is the git path of CreateCard: the store mints the final id,
// installs the card under it at once and queues the write — no provisional
// id, nothing to alias later. The write joins its request's commit.
func (b *storeBackend) createMinted(ctx context.Context, bd board.Board, e *boardEntry, in board.CreateInput) (board.Card, error) {
	in.ItemID = b.mintID()
	card := cardFromInput(in, in.ItemID)
	card.Author = board.ActorFrom(ctx)
	card.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	b.installCreated(ctx, e, card)
	b.enqueue(ctx, e, pendingOp{
		itemID:  in.ItemID,
		kind:    "create",
		desc:    "create «" + card.Title + "»",
		noRetry: true,
		apply: func(target *board.Board) {
			for _, c := range target.Cards {
				if c.ItemID == in.ItemID {
					return
				}
			}
			target.Cards = append(target.Cards, card)
		},
		exec: func(ctx context.Context) error {
			_, err := b.inner.CreateCard(ctx, bd, in)
			return err
		},
	})
	return card, nil
}
