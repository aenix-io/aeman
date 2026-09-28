package ghcli

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
)

// resetLoginCache isolates the package's default source and gh seam. Tests use
// their own TokenSource values, but the package helper must not leak state.
func resetLoginCache(t *testing.T) {
	t.Helper()
	prevRun, prevTokens := runGH, loginTokens
	loginTokens = NewTokenSource()
	t.Cleanup(func() {
		runGH, loginTokens = prevRun, prevTokens
	})
}

// fakeGH stands in for the gh binary. It answers `auth token` from a list of
// successive tokens (the last repeats) and `api user` with one reply, counting
// only the api-user calls, which is what the cache is meant to spare.
type fakeGH struct {
	mu         sync.Mutex
	tokens     []string
	tokenIdx   int
	tokenErr   error
	userReply  string
	userErr    error
	userCalls  atomic.Int32
	respectCtx bool // when set, a cancelled context fails the api-user lookup
}

func (f *fakeGH) run(ctx context.Context, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(args) >= 2 && args[0] == "auth" && args[1] == "token" {
		if f.tokenErr != nil {
			return "", f.tokenErr
		}
		if len(f.tokens) == 0 {
			return "", nil
		}
		tok := f.tokens[min(f.tokenIdx, len(f.tokens)-1)]
		f.tokenIdx++
		return tok, nil
	}
	// api user --jq .login
	f.userCalls.Add(1)
	if f.respectCtx && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return f.userReply, f.userErr
}

func (f *fakeGH) setUser(t *testing.T, reply string, err error) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.userReply, f.userErr = reply, err
}

// An error from `gh api user` is remembered for the window so an unreachable
// forge costs one subprocess per window rather than one per request.
func TestLoginCachesFailure(t *testing.T) {
	resetLoginCache(t)
	f := &fakeGH{tokens: []string{"tok"}, userErr: errors.New("403 from forge")}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return time.Unix(1000, 0) }

	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("first lookup must return the forge error")
	}
	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("second lookup, within the window, must still be an error")
	}
	if got := f.userCalls.Load(); got != 1 {
		t.Fatalf("gh api user ran %d times, want 1 (the failure is cached)", got)
	}
}

// The window is not a verdict: once it expires the lookup runs again, and a
// recovery fills the source's successful cache.
func TestLoginRetriesAtUnansweredTTLAndCachesRecovery(t *testing.T) {
	resetLoginCache(t)
	now := time.Unix(1000, 0)
	f := &fakeGH{tokens: []string{"tok"}, userErr: errors.New("transient 503")}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return now }

	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("first lookup fails and opens the window")
	}
	now = now.Add(forge.UnansweredTTL) // the window expires exactly at the TTL
	f.setUser(t, "octocat", nil)
	if got, err := src.Login(context.Background()); err != nil || got != "octocat" {
		t.Fatalf("after the window a retry must run and succeed: %q, %v", got, err)
	}
	if got := f.userCalls.Load(); got != 2 {
		t.Fatalf("gh api user ran %d times, want 2 (one per window)", got)
	}
	if got, err := src.Login(context.Background()); err != nil || got != "octocat" || f.userCalls.Load() != 2 {
		t.Fatalf("success must be cached for the source: %q, %v, calls=%d", got, err, f.userCalls.Load())
	}
}

// A repeated failure past the TTL opens a fresh window rather than retrying on
// every call.
func TestLoginRenewsUnansweredWindow(t *testing.T) {
	resetLoginCache(t)
	now := time.Unix(2000, 0)
	f := &fakeGH{tokens: []string{"tok"}, userErr: errors.New("still 503")}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return now }

	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("first failure opens a window")
	}
	now = now.Add(forge.UnansweredTTL) // window one expires
	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("second failure retries and opens a new window")
	}
	if got := f.userCalls.Load(); got != 2 {
		t.Fatalf("want 2 retries across two windows, got %d", got)
	}
	now = now.Add(forge.UnansweredTTL - time.Second) // inside the renewed window
	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("still failing")
	}
	if got := f.userCalls.Load(); got != 2 {
		t.Fatalf("the renewed window must cache: got %d calls, want 2", got)
	}
}

// The window is keyed by the token value, so a token that changed under the
// source bypasses it and is asked about at once.
func TestLoginTokenChangeBypassesUnansweredCache(t *testing.T) {
	resetLoginCache(t)
	now := time.Unix(3000, 0)
	f := &fakeGH{tokens: []string{"tok-a", "tok-b"}, userErr: errors.New("403")}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return now }

	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("token A fails and opens a window")
	}
	now = now.Add(tokenTTL)
	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("token B still errors")
	}
	if got := f.userCalls.Load(); got != 2 {
		t.Fatalf("a changed token must bypass the window: got %d calls, want 2", got)
	}
}

// A resolved login belongs only to its source. Another source may carry a
// different token and must ask for its own identity.
func TestLoginSuccessIsScopedToSource(t *testing.T) {
	resetLoginCache(t)
	f := &fakeGH{tokens: []string{"tok-alice", "tok-bob"}, userReply: "alice"}
	runGH = f.run
	alice := NewTokenSource()
	bob := NewTokenSource()

	if got, err := alice.Login(context.Background()); err != nil || got != "alice" {
		t.Fatalf("first login: %q, %v", got, err)
	}
	f.setUser(t, "bob", nil)
	if got, err := bob.Login(context.Background()); err != nil || got != "bob" {
		t.Fatalf("second source login: %q, %v", got, err)
	}
	if got := f.userCalls.Load(); got != 2 {
		t.Fatalf("gh api user ran %d times, want 2 (once per source)", got)
	}
}

// A changed token invalidates the successful login cached on the same source.
func TestLoginRefreshesWhenTokenChanges(t *testing.T) {
	resetLoginCache(t)
	now := time.Unix(4000, 0)
	f := &fakeGH{tokens: []string{"tok-alice", "tok-bob"}, userReply: "alice"}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return now }

	if got, err := src.Login(context.Background()); err != nil || got != "alice" {
		t.Fatalf("first login: %q, %v", got, err)
	}
	now = now.Add(tokenTTL)
	f.setUser(t, "bob", nil)
	if got, err := src.Login(context.Background()); err != nil || got != "bob" {
		t.Fatalf("login after token replacement: %q, %v", got, err)
	}
	if got := f.userCalls.Load(); got != 2 {
		t.Fatalf("gh api user ran %d times, want 2 (once per token)", got)
	}
}

func TestTokenAndLoginRefreshTogether(t *testing.T) {
	resetLoginCache(t)
	now := time.Unix(4500, 0)
	f := &fakeGH{tokens: []string{"tok-alice", "tok-bob"}, userReply: "alice"}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return now }

	tok, login, err := src.TokenAndLogin(context.Background())
	if err != nil || tok != "tok-alice" || login != "alice" {
		t.Fatalf("first pair = %q, %q, %v", tok, login, err)
	}
	now = now.Add(tokenTTL)
	f.setUser(t, "bob", nil)
	tok, login, err = src.TokenAndLogin(context.Background())
	if err != nil || tok != "tok-bob" || login != "bob" {
		t.Fatalf("refreshed pair = %q, %q, %v", tok, login, err)
	}
}

// An empty reply with no error is a real answer — no login yet — and is cached
// as such, with nil kept as the error rather than a fabricated one.
func TestLoginCachesEmptyReplyWithoutInventingError(t *testing.T) {
	resetLoginCache(t)
	f := &fakeGH{tokens: []string{"tok"}, userReply: "", userErr: nil}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return time.Unix(5000, 0) }

	if got, err := src.Login(context.Background()); got != "" || err != nil {
		t.Fatalf("an empty reply is an empty login and no error: %q, %v", got, err)
	}
	if got, err := src.Login(context.Background()); got != "" || err != nil {
		t.Fatalf("the empty reply is cached as-is, no invented error: %q, %v", got, err)
	}
	if got := f.userCalls.Load(); got != 1 {
		t.Fatalf("gh api user ran %d times, want 1 (empty reply cached)", got)
	}
}

// The caller's own cancellation says nothing about the shared credential, so it
// is never cached: the next live call asks again.
func TestLoginDoesNotCacheCallerCancellation(t *testing.T) {
	resetLoginCache(t)
	f := &fakeGH{tokens: []string{"tok"}, respectCtx: true, userErr: errors.New("unused")}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return time.Unix(6000, 0) }
	// Warm the token cache so the cancelled call still reaches api-user rather
	// than failing on the token read.
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Login(ctx); err == nil {
		t.Fatal("a cancelled lookup is an error")
	}
	if _, err := src.Login(context.Background()); err == nil {
		t.Fatal("the forge still errors")
	}
	if got := f.userCalls.Load(); got != 2 {
		t.Fatalf("cancellation must not be cached: got %d calls, want 2", got)
	}
}

// The source mutex covers the subprocess, so a burst of callers arriving on an
// unanswered token share one attempt.
func TestLoginConcurrentCallersShareUnansweredWindow(t *testing.T) {
	resetLoginCache(t)
	f := &fakeGH{tokens: []string{"tok"}, userErr: errors.New("503")}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return time.Unix(7000, 0) }

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_, _ = src.Login(context.Background())
		}()
	}
	wg.Wait()
	if got := f.userCalls.Load(); got != 1 {
		t.Fatalf("concurrent callers ran gh api user %d times, want 1 (they share the window)", got)
	}
}

// Without a token there is no safe cache key, so the lookup still runs, its own
// result is returned, and nothing is remembered.
func TestLoginWithoutTokenPreservesLookupResult(t *testing.T) {
	resetLoginCache(t)
	lookupErr := errors.New("gh api user: not found")
	f := &fakeGH{tokenErr: errors.New("not logged in"), userErr: lookupErr}
	runGH = f.run
	src := NewTokenSource()
	src.now = func() time.Time { return time.Unix(8000, 0) }

	if _, err := src.Login(context.Background()); !errors.Is(err, lookupErr) {
		t.Fatalf("the lookup's own error must be preserved, got %v", err)
	}
	if _, err := src.Login(context.Background()); !errors.Is(err, lookupErr) {
		t.Fatalf("a keyless failure is not cached, got %v", err)
	}
	if got := f.userCalls.Load(); got != 2 {
		t.Fatalf("a failure without a token key must not be cached: got %d, want 2", got)
	}
}
