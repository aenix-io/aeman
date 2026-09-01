package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	forgepkg "github.com/aenix-io/aeman/internal/forge"
)

// Coming back to a board after a break used to cost a second of waiting for
// nothing: the visitor's rights had expired (a minute), so the first request
// — and each of the four the page fires beside it — asked the forge again,
// two sequential round trips each. The answer is served from what we know
// while the check runs behind it, and one check runs at a time per visitor.
func TestRightsAreServedWhileTheyRevalidate(t *testing.T) {
	const probeTime = 300 * time.Millisecond // a forge round trip, slowed down
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(probeTime)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"permissions":{"admin":true,"push":true,"pull":true}}`))
	}))
	t.Cleanup(srv.Close)

	fa := newForgeAccess(forgepkg.NewGitHubAt(srv.URL), srv.Client(),
		[]RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared.git"}}, "srv-token", nil)
	clock := &fakeClock{at: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}
	fa.now = clock.now

	// The first answer is paid for: nothing is known about this visitor yet.
	first, err := fa.rights(context.Background(), "tok", "kvaps")
	if err != nil || !first.canWrite("shared") {
		t.Fatalf("first answer: %+v %v", first, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d probes for the first answer, want 1", calls.Load())
	}

	// A break: the answer is stale now. The visitor must not wait for the
	// forge again — the known answer stands while the check runs behind it.
	clock.wind(accessTTL + time.Second)
	started := time.Now()
	stale, err := fa.rights(context.Background(), "tok", "kvaps")
	took := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if !stale.canWrite("shared") {
		t.Fatalf("the stale answer must be the last one known: %+v", stale)
	}
	if took > probeTime/3 {
		t.Fatalf("the stale answer took %s — it waited for the forge instead of being served at once", took)
	}

	// The four requests a page load fires beside it do not each start their
	// own check — they share the one already running.
	for i := 0; i < 4; i++ {
		started := time.Now()
		if _, err := fa.rights(context.Background(), "tok", "kvaps"); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(started); took > probeTime/3 {
			t.Fatalf("request %d waited %s for the forge", i, took)
		}
	}

	// Once the check lands, the fresh answer is what the next request gets,
	// and the whole burst cost exactly one revalidation.
	deadline := time.Now().Add(2 * time.Second)
	for {
		fa.mu.Lock()
		at := fa.cache["kvaps"].at
		fa.mu.Unlock()
		if at.Equal(clock.now()) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the background check never landed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := fa.rights(context.Background(), "tok", "kvaps"); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("%d probes in total, want 2 (the first and one revalidation)", n)
	}
}

// A visitor nobody knows anything about still waits for a real answer —
// serving "no rights" or "all rights" from nothing would be a guess.
func TestAnUnknownVisitorStillWaitsForTheForge(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"permissions":{"pull":true}}`))
	}))
	t.Cleanup(srv.Close)
	fa := newForgeAccess(forgepkg.NewGitHubAt(srv.URL), srv.Client(),
		[]RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared.git"}}, "srv-token", nil)
	r, err := fa.rights(context.Background(), "tok", "newcomer")
	if err != nil || !r.canRead("shared") || r.canWrite("shared") {
		t.Fatalf("newcomer = %+v, %v", r, err)
	}
	if calls.Load() == 0 {
		t.Fatal("an unknown visitor must be checked, not guessed")
	}
}

// The board cache ages by the clock, but a git board's freshness is not a
// matter of time: the sync fetches every few seconds and knows whether the
// remote moved. A tick that finds nothing new means the cache is current —
// so it says so, and a board nobody has touched for an hour is not re-read
// from scratch when someone finally opens it.
func TestAQuietSyncKeepsTheCacheCurrent(t *testing.T) {
	remote := gitRemoteN(t, "quiet")
	seedGitRemote(t, remote)
	srv := gitModeServerOver(t, fakeAccess{}, remote)
	ctx := context.Background()
	key := storeKey(srv.gitBoard())
	if _, err := srv.gitBE.LoadBoard(ctx, key); err != nil {
		t.Fatal(err)
	}
	e := srv.gitBE.store.entry(key)
	e.mu.Lock()
	before := e.loadedAt
	e.mu.Unlock()
	if before.IsZero() {
		t.Fatal("the board must be loaded to begin with")
	}

	time.Sleep(5 * time.Millisecond)
	if err := srv.gitBE.syncNow(ctx, key); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	after := e.loadedAt
	loaded := e.loaded
	e.mu.Unlock()
	if !loaded {
		t.Fatal("a quiet sync must not drop the cache")
	}
	if !after.After(before) {
		t.Fatalf("a sync that found nothing new must mark the cache current: %s vs %s", after, before)
	}
}

// The health endpoint tells an operator when the cache was last known
// current — the number that explains a slow first request after a break.
func TestHealthzNamesWhenTheCacheWasVerified(t *testing.T) {
	remote := gitRemoteN(t, "quiet")
	seedGitRemote(t, remote)
	srv := gitModeServerOver(t, fakeAccess{}, remote)
	if _, err := srv.gitBE.LoadBoard(context.Background(), storeKey(srv.gitBoard())); err != nil {
		t.Fatal(err)
	}
	rec := doAs(t, srv, "kvaps", "GET", "/api/healthz", "")
	if !strings.Contains(rec.Body.String(), "cacheAgeSeconds") {
		t.Fatalf("healthz = %s; want the cache's age", rec.Body.String())
	}
}

// Serving the last known answer while it revalidates must not outlive the
// authorization itself. A token the forge no longer honours — revoked, or
// minted before the scopes this board needs — is not a slow forge: the
// cached rights are forgotten, so the next request asks, fails, and the
// visitor is sent to sign in again instead of being told "no write access"
// for as long as the process lives.
func TestADeadTokenIsForgottenRatherThanServedStale(t *testing.T) {
	var dead atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if dead.Load() {
			// What GitHub answers a token whose scopes predate the board:
			// the private repository is simply not there.
			w.Header().Set("X-OAuth-Scopes", "project")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"permissions":{"admin":true,"push":true,"pull":true}}`))
	}))
	t.Cleanup(srv.Close)

	fa := newForgeAccess(forgepkg.NewGitHubAt(srv.URL), srv.Client(),
		[]RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared.git"}}, "srv-token", nil)
	clock := &fakeClock{at: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}
	fa.now = clock.now
	if _, err := fa.rights(context.Background(), "tok", "kvaps"); err != nil {
		t.Fatal(err)
	}

	dead.Store(true)
	clock.wind(accessTTL + time.Second)
	// The request in flight is still answered from what was true a minute
	// ago — the check runs behind it.
	if r, err := fa.rights(context.Background(), "tok", "kvaps"); err != nil || !r.canWrite("shared") {
		t.Fatalf("the answer during the check: %+v, %v", r, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		fa.mu.Lock()
		_, held := fa.cache["kvaps"]
		fa.mu.Unlock()
		if !held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the rights of a token the forge rejects were kept")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := fa.rights(context.Background(), "tok", "kvaps"); !errors.Is(err, errBadVisitorToken) {
		t.Fatalf("after the token died: %v, want a bad-token error", err)
	}
}

// memberForge answers the collaborator listing, slowly, and counts how often
// it was asked.
func memberForge(t *testing.T, calls *atomic.Int32, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"login":"kvaps","permissions":{"pull":true,"push":true}},{"login":"lex","permissions":{"pull":true}}]`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Who else may read a domain is asked with the server's credential and
// trusted for five minutes — so the first board request after a five-minute
// break paid for it, and with one question per person across two domains
// that was seconds of a page load. The list a page needs is the one already
// known; the check runs behind it.
func TestTheMemberListIsServedWhileItRefreshes(t *testing.T) {
	const probeTime = 300 * time.Millisecond
	var calls atomic.Int32
	srv := memberForge(t, &calls, probeTime)
	fa := newForgeAccess(forgepkg.NewGitHubAt(srv.URL), srv.Client(),
		[]RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared.git"}}, "srv-token", nil)
	clock := &fakeClock{at: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}
	fa.now = clock.now
	ctx := context.Background()

	// Nobody has been asked about yet: this one is paid for, or the picker
	// would silently lose people it has never heard of.
	first, err := fa.readers(ctx, "shared", []string{"kvaps", "lex"})
	if err != nil || len(first) != 2 {
		t.Fatalf("first answer: %v, %v", first, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d listings for the first answer, want 1", calls.Load())
	}

	// A break longer than the member TTL. The page must not wait again.
	clock.wind(membersTTL + time.Second)
	started := time.Now()
	stale, err := fa.readers(ctx, "shared", []string{"kvaps", "lex"})
	took := time.Since(started)
	if err != nil || len(stale) != 2 {
		t.Fatalf("the stale answer: %v, %v", stale, err)
	}
	if took > probeTime/3 {
		t.Fatalf("the member list took %s — it waited for the forge", took)
	}
	for i := 0; i < 3; i++ {
		started := time.Now()
		if _, err := fa.readers(ctx, "shared", []string{"kvaps", "lex"}); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(started); took > probeTime/3 {
			t.Fatalf("request %d waited %s for the member list", i, took)
		}
	}
	waitForCalls(t, &calls, 2)
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 2 {
		t.Fatalf("%d listings in total, want 2 (the first and one refresh)", n)
	}
}

// A login the server has never asked about is asked about — an answer of
// "not a reader" from nothing would quietly shrink the reviewer picker.
func TestAPersonNobodyHasAskedAboutIsAskedAbout(t *testing.T) {
	var calls atomic.Int32
	srv := memberForge(t, &calls, 0)
	fa := newForgeAccess(forgepkg.NewGitHubAt(srv.URL), srv.Client(),
		[]RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared.git"}}, "srv-token", nil)
	ctx := context.Background()
	if _, err := fa.readers(ctx, "shared", []string{"kvaps"}); err != nil {
		t.Fatal(err)
	}
	got, err := fa.readers(ctx, "shared", []string{"kvaps", "lex"})
	if err != nil {
		t.Fatal(err)
	}
	// The newcomer is offered at once — a login nobody has asked about yet
	// is not "cannot read", and the picker must not lose the person a card
	// was just assigned to — and asked about BEHIND the answer: waiting for
	// the forge here is what put seconds between a person and their board
	// every time somebody new appeared on it.
	if len(got) != 2 {
		t.Fatalf("readers = %v; the newcomer must not be assumed away", got)
	}
	waitForCalls(t, &calls, 2)
	if calls.Load() != 2 {
		t.Fatalf("%d listings, want 2 — the second for the login never seen", calls.Load())
	}
	// …and once the forge has ANSWERED, the answer stands. The listing call
	// is counted when it is made, and the answer is recorded when it comes
	// back: in between, the login is one the server has asked about and has
	// no answer for, which is deliberately not offered — so settle first.
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := fa.readers(ctx, "shared", []string{"kvaps", "lex"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the settled answer: %v", got)
		}
		time.Sleep(time.Millisecond)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("%d listings; settling must not have asked again", n)
	}
}
