package tokenstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/nonet"
)

// fakeGitHub answers /user with the login a token belongs to and 401 for a
// token it does not know, counting the calls so a test can see how often
// the forge was asked at all.
func fakeGitHub(t *testing.T, logins map[string]string, calls *atomic.Int32) (forge.Forge, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		login, ok := logins[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"login":"`+login+`","name":"","avatar_url":""}`)
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()
	// The guard the default transport carries, on the client the code
	// under test is handed: this one never consults the default, so
	// without this a case pointed at a real host would reach it through
	// the client this helper supplied.
	client.Transport = nonet.Guard(client.Transport)
	return forge.NewGitHubAt(srv.URL), client
}

// The actor behind a stored token is whoever the forge says the token
// belongs to — not whoever gh or glab happens to be signed in as on the
// machine. The two differ whenever the stored token is a bot's, or a second
// account's, and attributing that process's commits to the wrong person is
// the failure this rules out.
func TestKeychainCLILoginIsTheTokensOwnerNotTheCLIs(t *testing.T) {
	var calls atomic.Int32
	f, client := fakeGitHub(t, map[string]string{"ghp_bot": "aeman-bot"}, &calls)
	cli := NewCLI(newFake().Put("github.com", "ghp_bot"), f, client)

	if tok, err := cli.Token(context.Background()); err != nil || tok != "ghp_bot" {
		t.Fatalf("Token = %q, %v", tok, err)
	}
	login, err := cli.Login(context.Background())
	if err != nil || login != "aeman-bot" {
		t.Fatalf("Login = %q, %v; want the token's owner", login, err)
	}
}

// One token, one question to the forge. Login is on the path of every
// MCP request, so within the window the answer comes from memory — and
// it is the TOKEN that bounds that, not the process: see the two tests
// below for what happens when the token underneath it changes.
func TestKeychainCLILoginIsAskedOncePerToken(t *testing.T) {
	var calls atomic.Int32
	f, client := fakeGitHub(t, map[string]string{"ghp_alice": "alice"}, &calls)
	store := newFake().Put("github.com", "ghp_alice")
	cli := NewCLI(store, f, client)

	for i := range 3 {
		if login, err := cli.Login(context.Background()); err != nil || login != "alice" {
			t.Fatalf("Login call %d = %q, %v", i+1, login, err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times for three logins, want 1", n)
	}
	if store.Gets != 1 {
		t.Fatalf("the store was read %d times for three logins, want 1", store.Gets)
	}
}

// The token is read once and reused, because the server asks for it on
// every request in the local mode and reading it here is a fork+exec of
// the platform's secret tool — tens of milliseconds, serialised behind the
// library's own lock. The window is the one gh and glab use, so a token
// replaced on the machine is picked up by a running server within it.
func TestKeychainCLITokenIsReadOnceWithinTheTTL(t *testing.T) {
	ctx := context.Background()
	f, client := fakeGitHub(t, map[string]string{"ghp_alice": "alice"}, new(atomic.Int32))
	store := newFake().Put("github.com", "ghp_alice")
	cli := NewCLI(store, f, client)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cli.now = func() time.Time { return now }

	for i := range 3 {
		if tok, err := cli.Token(ctx); err != nil || tok != "ghp_alice" {
			t.Fatalf("Token call %d = %q, %v", i+1, tok, err)
		}
	}
	if store.Gets != 1 {
		t.Fatalf("the store was read %d times for three tokens, want 1", store.Gets)
	}

	now = now.Add(tokenTTL - time.Second)
	if _, err := cli.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if store.Gets != 1 {
		t.Fatalf("the store was read %d times just inside the window, want 1", store.Gets)
	}

	now = now.Add(2 * time.Second)
	if _, err := cli.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if store.Gets != 2 {
		t.Fatalf("the store was read %d times once the window passed, want 2", store.Gets)
	}
}

// `aeman login` in another terminal replaces the token under a running
// server, and the identity has to follow it. Caching the token for a
// window and the login for the whole process would push with the new
// token and sign the commits with the old token's owner — the divergence
// the election exists to prevent, arriving by the back door.
func TestKeychainCLIForgetsTheLoginWhenTheTokenChanges(t *testing.T) {
	ctx := context.Background()
	f, client := fakeGitHub(t, map[string]string{"ghp_alice": "alice", "ghp_bot": "aeman-bot"}, new(atomic.Int32))
	store := newFake().Put("github.com", "ghp_alice")
	cli := NewCLI(store, f, client)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cli.now = func() time.Time { return now }

	if login, err := cli.Login(ctx); err != nil || login != "alice" {
		t.Fatalf("Login = %q, %v", login, err)
	}

	store.Put("github.com", "ghp_bot")
	now = now.Add(tokenTTL + time.Second)

	tok, err := cli.Token(ctx)
	if err != nil || tok != "ghp_bot" {
		t.Fatalf("Token after the replacement = %q, %v", tok, err)
	}
	if login, err := cli.Login(ctx); err != nil || login != "aeman-bot" {
		t.Fatalf("Login after the replacement = %q, %v; want the new token's owner", login, err)
	}
}

// The same replacement seen by a caller that only ever asks for a login.
// `aeman mcp` is that caller — its middleware and its personal-board
// lookup call Login and nothing else — so a cached login that is only
// invalidated as a side effect of someone asking for a token would stay
// frozen for the whole process on the very path this feature is for.
func TestKeychainCLIForgetsTheLoginWhenOnlyLoginIsEverAsked(t *testing.T) {
	ctx := context.Background()
	f, client := fakeGitHub(t, map[string]string{"ghp_alice": "alice", "ghp_bot": "aeman-bot"}, new(atomic.Int32))
	store := newFake().Put("github.com", "ghp_alice")
	cli := NewCLI(store, f, client)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cli.now = func() time.Time { return now }

	if login, err := cli.Login(ctx); err != nil || login != "alice" {
		t.Fatalf("Login = %q, %v", login, err)
	}

	store.Put("github.com", "ghp_bot")
	now = now.Add(tokenTTL + time.Second)

	if login, err := cli.Login(ctx); err != nil || login != "aeman-bot" {
		t.Fatalf("Login after the replacement = %q, %v; want the new token's owner", login, err)
	}
}

// A read that found nothing is not an answer worth keeping: the next call
// asks again, so `aeman login` in one terminal is seen by a process that
// already looked and came up empty.
func TestKeychainCLIDoesNotCacheAMiss(t *testing.T) {
	ctx := context.Background()
	f, client := fakeGitHub(t, map[string]string{"ghp_alice": "alice"}, new(atomic.Int32))
	store := newFake()
	cli := NewCLI(store, f, client)

	for range 2 {
		if _, err := cli.Token(ctx); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Token = %v, want ErrNotFound", err)
		}
	}
	if store.Gets != 2 {
		t.Fatalf("the store was read %d times after a miss, want 2", store.Gets)
	}

	store.Put("github.com", "ghp_alice")
	if tok, err := cli.Token(ctx); err != nil || tok != "ghp_alice" {
		t.Fatalf("Token after a login = %q, %v; want the token that has appeared", tok, err)
	}
}

// A token the forge refuses is ErrBadToken and stays one through Login, so
// the chain above tells "this credential is wrong" apart from "there is no
// credential here" and does not remember a login it never got.
func TestKeychainCLIRejectedTokenIsBadToken(t *testing.T) {
	var calls atomic.Int32
	f, client := fakeGitHub(t, map[string]string{"ghp_good": "alice"}, &calls)
	cli := NewCLI(newFake().Put("github.com", "ghp_revoked"), f, client)

	if _, err := cli.Login(context.Background()); !errors.Is(err, forge.ErrBadToken) {
		t.Fatalf("Login with a revoked token = %v, want forge.ErrBadToken", err)
	}
	if _, err := cli.Login(context.Background()); !errors.Is(err, forge.ErrBadToken) {
		t.Fatalf("a failed login must not be cached as an identity: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times, want 1: a refusal is remembered for that token", n)
	}
}

// The refusal is remembered against the token VALUE, not the process. A
// stored PAT expiring is the ordinary end of a stored credential, and
// asking the forge again on every request would put a round trip under
// this source's lock for as long as the bad token sits there. Replacing
// it — `aeman login` with a fresh one — is a different value, so the
// forge is asked again without anything to expire first.
func TestKeychainCLIRemembersARefusalPerTokenValue(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	f, client := fakeGitHub(t, map[string]string{"ghp_fresh": "alice"}, &calls)
	store := newFake().Put("github.com", "ghp_revoked")
	cli := NewCLI(store, f, client)

	for range 5 {
		if _, err := cli.Login(ctx); !errors.Is(err, forge.ErrBadToken) {
			t.Fatalf("Login = %v, want forge.ErrBadToken", err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times for one refused token, want 1", n)
	}

	// A refusal does not expire the way an unanswered call does: it is an
	// answer, and it stands until the token changes. Well past the
	// unanswered window, the forge is still not asked.
	cli.now = func() time.Time { return time.Now().Add(4 * forge.UnansweredTTL) }
	if _, err := cli.Login(ctx); !errors.Is(err, forge.ErrBadToken) {
		t.Fatalf("Login well after the unanswered window = %v, want the refusal to stand", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times; a refusal must not expire", n)
	}

	// A new token is a new question.
	store.Put("github.com", "ghp_fresh")
	cli.now = func() time.Time { return time.Now().Add(2 * tokenTTL) }
	if login, err := cli.Login(ctx); err != nil || login != "alice" {
		t.Fatalf("Login after the token was replaced = %q, %v; want the new token's owner", login, err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("the forge was asked %d times, want 2: the new token was asked about", n)
	}
}

// A host nobody has run `aeman login` for holds nothing: both questions
// report ErrNotFound, which is what the chain falls through on.
func TestKeychainCLIWithoutAnItemIsNotFound(t *testing.T) {
	var calls atomic.Int32
	f, client := fakeGitHub(t, map[string]string{"ghp_alice": "alice"}, &calls)
	cli := NewCLI(newFake(), f, client)

	_, err := cli.Token(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Token = %v, want ErrNotFound", err)
	}
	if _, err := cli.Login(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Login = %v, want ErrNotFound", err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the forge was asked %d times without a token, want 0", n)
	}
}

// A caller that names no HTTP client gets one that gives up. The forge is
// asked under the same lock Token takes, so a connection that never
// answers would otherwise hold every later token read behind it — and
// http.DefaultClient waits forever by design.
func TestKeychainCLIWithoutAClientBoundsTheForgeCall(t *testing.T) {
	cli := NewCLI(newFake(), forge.NewGitHub(), nil)
	if cli.client == http.DefaultClient {
		t.Fatal("http.DefaultClient has no timeout; a wedged forge would pin the lock")
	}
	if cli.client.Timeout <= 0 {
		t.Fatalf("client timeout = %v, want a bound", cli.client.Timeout)
	}
}

// fake is this package's own in-memory Store. The exported one lives in
// tokenstoretest, for the packages that consume this one; a package cannot
// import its own test helper without a cycle, and these tests reach into
// unexported fields anyway.
type fake struct {
	items   map[string]string
	Err     error
	Gets    int
	Deletes int
}

func newFake() *fake { return &fake{items: map[string]string{}} }

func (f *fake) Put(host, token string) *fake { f.items[host] = token; return f }

func (f *fake) Get(host string) (string, error) {
	f.Gets++
	if f.Err != nil {
		return "", f.Err
	}
	tok, ok := f.items[host]
	if !ok {
		return "", ErrNotFound
	}
	// The rules osStore.Get puts on a raw item, so a case here cannot
	// pass against a shape the real store never hands back. This double
	// exists only because these tests drive the CLI's clock seam, which
	// is unexported; tokenstoretest.Fake is the one every other package
	// uses.
	if tok = strings.TrimSpace(tok); tok == "" {
		return "", ErrNotFound
	}
	return tok, nil
}

func (f *fake) Set(host, token string) error {
	if f.Err != nil {
		return f.Err
	}
	f.items[host] = token
	return nil
}

func (f *fake) Delete(host string) error {
	f.Deletes++
	if f.Err != nil {
		return f.Err
	}
	delete(f.items, host)
	return nil
}

// This package's tests run with the network shut off: the default
// transport refuses anything but loopback, and so do the clients the fake
// forges hand out, which carry their own and would otherwise go around
// it. A case that builds a forge against the real host fails on the
// request rather than reaching it with whatever token the machine
// exports. What the guard cannot see is a client assembled by hand with
// its own Transport.
func TestMain(m *testing.M) {
	restore := nonet.Block()
	code := m.Run()
	restore()
	os.Exit(code)
}

// forgeThatCannotAnswer answers /user with a status that is neither the
// login nor a refusal — 403 is what a GitHub App installation token gets,
// and a 5xx or a timeout arrives the same way.
func forgeThatCannotAnswer(t *testing.T, calls *atomic.Int32, delay time.Duration) (forge.Forge, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(delay)
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()
	// The guard the default transport carries, on the client the code
	// under test is handed: this one never consults the default, so
	// without this a case pointed at a real host would reach it through
	// the client this helper supplied.
	client.Transport = nonet.Guard(client.Transport)
	return forge.NewGitHubAt(srv.URL), client
}

// A forge that cannot say who the token belongs to costs ONE call per
// window, not one per request. The answer is not cacheable as a login and
// the token is not refused, so without a window every request repeats the
// call — under this source's lock, where the next request waits for it.
func TestKeychainCLIRemembersAForgeThatCouldNotAnswer(t *testing.T) {
	var calls atomic.Int32
	f, client := forgeThatCannotAnswer(t, &calls, 0)
	store := newFake().Put("github.com", "ghp_app")
	cli := NewCLI(store, f, client)
	now := time.Now()
	cli.now = func() time.Time { return now }

	for range 20 {
		if _, err := cli.Login(context.Background()); err == nil {
			t.Fatal("a forge that will not answer is not a login")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times for twenty logins, want 1", n)
	}

	// The window is not a verdict: once it passes the forge is asked
	// again, so a token that starts working is named without a restart.
	now = now.Add(forge.UnansweredTTL + time.Second)
	if _, err := cli.Login(context.Background()); err == nil {
		t.Fatal("still not a login")
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("the forge was asked %d times after the window, want 2", n)
	}
}

// A token replaced in the store is a new question. What this pins is the
// token TTL, not the value key: the item is only re-read once that window
// passes, and by then the shorter unanswered window has expired anyway.
// The value key underneath is defensive here and is exercised where it
// can fire, on the environment's source, whose value can change under a
// running process.
func TestKeychainCLIAsksAgainForATokenReplacedInTheStore(t *testing.T) {
	var calls atomic.Int32
	f, client := forgeThatCannotAnswer(t, &calls, 0)
	store := newFake().Put("github.com", "ghp_app")
	cli := NewCLI(store, f, client)
	base := time.Now()
	cli.now = func() time.Time { return base }

	if _, err := cli.Login(context.Background()); err == nil {
		t.Fatal("want a failure")
	}
	store.Put("github.com", "ghp_replaced")
	base = base.Add(tokenTTL + time.Second) // let the token be re-read
	if _, err := cli.Login(context.Background()); err == nil {
		t.Fatal("want a failure")
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("the forge was asked %d times, want 2: a replaced token is a new question", n)
	}
}

// Callers do not queue behind a forge that cannot answer. The first call
// pays for it; the rest read the window.
func TestKeychainCLIConcurrentCallersDoNotQueueBehindADeadForge(t *testing.T) {
	const callers = 8
	const latency = 100 * time.Millisecond
	var calls atomic.Int32
	f, client := forgeThatCannotAnswer(t, &calls, latency)
	cli := NewCLI(newFake().Put("github.com", "ghp_app"), f, client)

	start := time.Now()
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cli.Login(context.Background())
		}()
	}
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times by %d callers, want 1", n, callers)
	}
	if elapsed := time.Since(start); elapsed > latency*4 {
		t.Fatalf("%d callers took %v against a %v forge; they queued behind it", callers, elapsed, latency)
	}
}

// The same guard on the client this package's fakes hand out.
func TestTheFakeForgesClientRefusesTheRealHost(t *testing.T) {
	var calls atomic.Int32
	_, client := fakeGitHub(t, nil, &calls)
	off := forge.NewGitHubAt("http://192.0.2.1")
	if _, err := off.User(context.Background(), client, "not-a-token"); err == nil {
		t.Fatal("the helper's client left the machine")
	} else if !strings.Contains(err.Error(), "tried to reach") {
		t.Fatalf("error = %v; want the guard's refusal, not a network answer", err)
	}
}

// One caller giving up must not answer for the next. The window exists
// for a forge that could not answer; a context cancelled by the caller
// says nothing about the forge, and recording it hands the next caller —
// with a live context and a forge that is fine — a stale error and no
// call. A browser abandoning a request while the login cache is cold is
// enough to blank the identity for the whole window.
func TestKeychainCLIDoesNotRememberTheCallersOwnCancellation(t *testing.T) {
	var calls atomic.Int32
	f, client := fakeGitHub(t, map[string]string{"ghp_alice": "alice"}, &calls)
	cli := NewCLI(newFake().Put("github.com", "ghp_alice"), f, client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cli.Login(ctx); err == nil {
		t.Fatal("a cancelled caller gets no login")
	}

	// The next caller asks for itself.
	login, err := cli.Login(context.Background())
	if err != nil || login != "alice" {
		t.Fatalf("second caller: login=%q err=%v; want the owner, asked afresh", login, err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the forge was asked %d times; the second caller was served the first's cancellation", n)
	}
}
