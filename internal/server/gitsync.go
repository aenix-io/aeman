package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"reflect"
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
	// MaintainEvery is the maintenance cadence — repack, and the removal of
	// torn-move ghosts whose destination has landed; zero disables it.
	MaintainEvery time.Duration
	Logger        *slog.Logger
}

// gitDomain is one of the board's repositories and where it pushes.
type gitDomain struct {
	gitstore.Domain
	remote gitstore.Remote
}

// gitSync is the per-store sync state.
type gitSync struct {
	domains   []gitDomain // primary first
	mb        *gitstore.MultiBackend
	pushDelay time.Duration
	log       *slog.Logger

	// applyMu serializes the queue's commits with the sync's resets and
	// replays: a group in flight finishes its commit before a rebase moves
	// the branch, so the replay carries it instead of a stale flush
	// clobbering the other replica's fields.
	applyMu sync.Mutex

	mu          sync.Mutex
	lastPushErr error
	pushTimer   *time.Timer
	pushing     bool
}

// primary is the repository that names the board.
func (g *gitSync) primary() *gitstore.Repo { return g.domains[0].Repo }

// newGitBackend builds the store's backend over the board's clones.
func newGitBackend(store *boardStore, domains []gitDomain, opts gitOptions) *storeBackend {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	ds := make([]gitstore.Domain, len(domains))
	for i, d := range domains {
		ds[i] = d.Domain
	}
	mb := gitstore.NewMultiBackend(ds, gitstore.BackendOptions{})
	be := &storeBackend{
		inner: mb,
		store: store,
		git:   &gitSync{domains: domains, mb: mb, pushDelay: opts.PushDelay, log: opts.Logger},
	}
	if opts.SyncInterval > 0 {
		go be.runSync(context.Background(), opts.SyncInterval)
	}
	if opts.MaintainEvery > 0 {
		go be.runMaintain(context.Background(), opts.MaintainEvery)
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
	b.git.applyMu.Lock()
	defer b.git.applyMu.Unlock()
	sctx, flush, _ := b.git.scopeFor(ctx, first)
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
	}
	hash, err := flush()
	if err != nil {
		return err
	}
	if !hash.IsZero() {
		b.refreshCards(sctx, e, gitstore.ScopeCards(sctx))
		b.committed(e)
	}
	return nil
}

// refreshCards brings the cards a commit named up to date from the tree —
// what the optimistic apply could not know: a moved card's new domain, the
// cascade that followed it, doneFrom on reaching 100 — and tells the
// watchers about the ones that actually differ. Refresh, never resurrect: a
// card no longer in the cache was deleted by this very request.
func (b *storeBackend) refreshCards(ctx context.Context, e *boardEntry, ids []string) {
	if len(ids) == 0 {
		return
	}
	fresh, err := b.inner.LoadCards(ctx, board.Board{}, ids)
	if err != nil {
		b.git.log.Warn("refresh after commit failed", "err", err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var changed []string
	for _, c := range fresh {
		for i := range e.board.Cards {
			if e.board.Cards[i].ItemID != c.ItemID {
				continue
			}
			if !reflect.DeepEqual(e.board.Cards[i], c) {
				e.board.Cards[i] = c
				changed = append(changed, c.ItemID)
			}
			break
		}
	}
	if len(changed) == 0 {
		return
	}
	// Later queued writes to these cards predate nothing on the tree yet;
	// replay them so the cache keeps saying what the user asked.
	if e.inflight != nil {
		e.inflight.apply(&e.board)
	}
	for _, op := range e.pending {
		op.apply(&e.board)
	}
	for _, id := range changed {
		for i := range e.board.Cards {
			if e.board.Cards[i].ItemID == id {
				e.cardChanged("", e.board.Cards[i], "MODIFIED")
				break
			}
		}
	}
}

// ---- push ------------------------------------------------------------------------------

// committed schedules a push after a group committed.
func (b *storeBackend) committed(e *boardEntry) {
	g := b.git
	g.mu.Lock()
	if (g.pushDelay > 0 || g.pushTimer != nil) && g.pushTimer == nil {
		g.pushTimer = time.AfterFunc(g.pushDelay, func() {
			_ = b.syncNow(context.Background(), storeKeyOf(b.store, e))
		})
	}
	g.mu.Unlock()
}

// unpushedAge is how long the oldest unpushed commit, in any domain, has
// been waiting; zero when everything is on the remotes. The commits are
// read from the clones, so what an earlier run left behind counts too.
func (b *storeBackend) unpushedAge(_ string) time.Duration {
	var oldest time.Time
	for _, d := range b.git.domains {
		cs, err := d.Repo.UnpushedCommits()
		if err != nil || len(cs) == 0 {
			continue
		}
		if w := cs[0].Committer.When; oldest.IsZero() || w.Before(oldest) {
			oldest = w
		}
	}
	if oldest.IsZero() {
		return 0
	}
	return time.Since(oldest)
}

// syncNow runs one sync cycle for the board: push every domain with
// unpushed commits; on a rejection re-apply that domain on the new tip and
// push again; then fetch and adopt what others pushed.
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
		rejected, tip, err := g.pushAll(ctx)
		if err != nil {
			return err
		}
		if rejected == nil {
			break
		}
		if attempt >= 5 {
			return fmt.Errorf("push kept losing races on %s", rejected.Name)
		}
		if err := b.rebaseDomain(ctx, e, rejected, tip); err != nil {
			return err
		}
		// Backoff with jitter so two replicas do not lock-step.
		time.Sleep(time.Duration(rand.IntN(50)+10*attempt) * time.Millisecond) //nolint:gosec // jitter, not security
	}
	return b.adoptRemote(ctx, e)
}

// pushAll pushes every domain with unpushed commits. It returns the first
// domain whose push was rejected because its remote moved, with the tip
// fetched; a push that fails with nothing new on the remote, or an
// unreachable remote, is an error (G10) and the commits stay.
func (g *gitSync) pushAll(ctx context.Context) (*gitDomain, plumbing.Hash, error) {
	for i := range g.domains {
		d := &g.domains[i]
		n, err := d.Repo.Unpushed()
		if err != nil {
			return nil, plumbing.ZeroHash, err
		}
		if n == 0 {
			continue
		}
		err = d.Repo.Push(ctx, d.remote)
		if err == nil {
			continue
		}
		// Rejected or unreachable: the fetch decides which.
		tip, moved, ferr := d.Repo.Fetch(ctx, d.remote)
		if ferr != nil || !moved {
			g.mu.Lock()
			g.lastPushErr = err
			g.mu.Unlock()
			if ferr != nil {
				return nil, plumbing.ZeroHash, fmt.Errorf("push %s failed and the remote is unreachable: %w", d.Name, err)
			}
			return nil, plumbing.ZeroHash, fmt.Errorf("push %s failed with nothing new on the remote: %w", d.Name, err)
		}
		return d, tip, nil
	}
	g.mu.Lock()
	g.lastPushErr = nil
	g.mu.Unlock()
	return nil, plumbing.ZeroHash, nil
}

// rebaseDomain re-applies one domain's unpushed commits on its remote's new
// tip and refreshes the cache from the result.
func (b *storeBackend) rebaseDomain(ctx context.Context, e *boardEntry, d *gitDomain, tip plumbing.Hash) error {
	g := b.git
	g.applyMu.Lock()
	defer g.applyMu.Unlock()
	res, err := d.Repo.Rebase(tip)
	if err != nil {
		return err
	}
	g.log.Info("re-applied on the remote's tip", "domain", d.Name, "replayed", res.Replayed, "dropped", res.Dropped)
	return b.reloadFromTip(ctx, e)
}

// adoptRemote fetches every domain and, when a remote moved and nothing
// local is pending, moves onto its tip — a plain reset, or a replay when the
// domain still holds unpushed commits — and reloads the cache once; the
// diff becomes watch events for everyone.
func (b *storeBackend) adoptRemote(ctx context.Context, e *boardEntry) error {
	g := b.git
	g.applyMu.Lock()
	defer g.applyMu.Unlock()
	moved := false
	for i := range g.domains {
		d := &g.domains[i]
		tip, m, err := d.Repo.Fetch(ctx, d.remote)
		if err != nil {
			return err
		}
		if !m {
			continue
		}
		e.mu.Lock()
		busy := len(e.pending) > 0 || e.inflight != nil
		e.mu.Unlock()
		if busy {
			return nil // the drain will bring its own push; the rebase there adopts the tip
		}
		if n, _ := d.Repo.Unpushed(); n > 0 {
			if _, err := d.Repo.Rebase(tip); err != nil {
				return err
			}
		} else if err := d.Repo.ResetTo(tip); err != nil {
			return err
		}
		moved = true
	}
	if !moved {
		return nil
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

// runMaintain is the maintenance ticker: every interval, one pass per known
// board.
func (b *storeBackend) runMaintain(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
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
				if _, err := b.maintainNow(ctx, k); err != nil && !errors.Is(err, context.Canceled) {
					b.git.log.Warn("maintenance", "board", k, "err", err)
				}
			}
		}
	}
}

// maintainNow is one maintenance pass: the stale copies of torn moves whose
// destination has landed are removed (G22) — one commit per domain, pushed
// at once — and the clones are repacked. It returns how many ghosts went.
func (b *storeBackend) maintainNow(ctx context.Context, key string) (int, error) {
	g := b.git
	g.applyMu.Lock()
	landed := func(domain string) bool {
		for _, d := range g.domains {
			if d.Name == domain {
				n, err := d.Repo.Unpushed()
				return err == nil && n == 0
			}
		}
		return false
	}
	swept, err := g.mb.SweepGhosts(ctx, landed)
	if err == nil {
		for _, d := range g.domains {
			if merr := d.Repo.Maintain(); merr != nil {
				g.log.Warn("repack failed", "domain", d.Name, "err", merr)
			}
		}
	}
	g.applyMu.Unlock()
	if err != nil {
		return 0, err
	}
	if swept == 0 {
		return 0, nil
	}
	g.log.Info("maintenance removed torn-move ghosts", "count", swept)
	if err := b.reloadFromTip(ctx, b.store.entry(key)); err != nil {
		return swept, err
	}
	return swept, b.syncNow(ctx, key)
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
