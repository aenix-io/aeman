package glabcli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// recorder stands in for the glab binary: it hands back a scripted reply and
// remembers every invocation, so a test can see which commands ran and how
// many times.
type recorder struct {
	calls [][]string
	out   string
	err   error
}

func (r *recorder) run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, args)
	return r.out, r.err
}

func newTestCLI(host string, r *recorder) *CLI {
	c := New(host)
	c.run = r.run
	return c
}

// The token is what glab has stored for the host, with the trailing newline
// the CLI prints stripped, and it is read once: a second call within the TTL
// is answered from the cache without touching glab again.
func TestTokenIsTrimmedAndCachedWithinTTL(t *testing.T) {
	r := &recorder{out: "  glpat-fake-token\n"}
	c := newTestCLI("gitlab.com", r)
	ctx := context.Background()

	for i := range 2 {
		got, err := c.Token(ctx)
		if err != nil {
			t.Fatalf("Token() call %d: %v", i+1, err)
		}
		if got != "glpat-fake-token" {
			t.Fatalf("Token() call %d = %q, want the trimmed token", i+1, got)
		}
	}
	if len(r.calls) != 1 {
		t.Fatalf("glab was invoked %d times for two Token calls, want 1 (the second is cached)", len(r.calls))
	}
}

// The cache is good for five minutes, not forever: once the TTL has passed
// the token is read from glab again, so a re-login on the machine is picked
// up by a running server.
func TestTokenIsReReadOnceTheTTLHasPassed(t *testing.T) {
	r := &recorder{out: "glpat-fake-token\n"}
	c := newTestCLI("gitlab.com", r)
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	if _, err := c.Token(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(tokenTTL - time.Second)
	if _, err := c.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("glab was invoked %d times just inside the TTL, want 1", len(r.calls))
	}
	now = now.Add(2 * time.Second)
	if _, err := c.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("glab was invoked %d times once the TTL passed, want 2", len(r.calls))
	}
}

// glab prints nothing when no token is stored for the host; that is not a
// token, it is "not logged in", and the error says how to fix it.
func TestEmptyTokenNamesTheLoginCommand(t *testing.T) {
	r := &recorder{out: "\n"}
	c := newTestCLI("gitlab.com", r)

	_, err := c.Token(context.Background())
	if err == nil {
		t.Fatal("an empty token must be an error")
	}
	const want = "glab returned an empty token; run `glab auth login`"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	// Nothing is cached from a failed read: the next call asks glab again.
	if _, err := c.Token(context.Background()); err == nil {
		t.Fatal("a failed read must not be cached as a token")
	}
	if len(r.calls) != 2 {
		t.Fatalf("glab was invoked %d times, want 2 (failures are not cached)", len(r.calls))
	}
}

// The token is read for the instance the board lives on, not for whatever
// glab considers its default host: the host goes to `config get token`, and
// an empty host means gitlab.com.
func TestTokenIsReadForTheConfiguredHost(t *testing.T) {
	for host, want := range map[string]string{
		"gitlab.example.org": "gitlab.example.org",
		"":                   "gitlab.com",
	} {
		r := &recorder{out: "glpat-fake-token\n"}
		c := newTestCLI(host, r)
		if _, err := c.Token(context.Background()); err != nil {
			t.Fatalf("New(%q).Token(): %v", host, err)
		}
		got := strings.Join(r.calls[0], " ")
		if got != "config get token --host "+want {
			t.Errorf("New(%q) ran %q, want %q", host, got, "config get token --host "+want)
		}
	}
}

// Login is the username field of `glab api user` — the login the forge knows
// the person by, not their display name — and it is asked for once per CLI
// value: the identity does not change under a running server.
func TestLoginIsTheUsernameFieldAndIsCachedForTheProcess(t *testing.T) {
	r := &recorder{out: `{"id":7,"username":"kvaps","name":"K"}` + "\n"}
	c := newTestCLI("gitlab.com", r)
	ctx := context.Background()

	for i := range 2 {
		got, err := c.Login(ctx)
		if err != nil {
			t.Fatalf("Login() call %d: %v", i+1, err)
		}
		if got != "kvaps" {
			t.Fatalf("Login() call %d = %q, want %q", i+1, got, "kvaps")
		}
	}
	if len(r.calls) != 1 {
		t.Fatalf("glab was invoked %d times for two Login calls, want 1 (the second is cached)", len(r.calls))
	}
}

// The login must belong to the same instance the token does: `glab api`
// defaults to gitlab.com (or the host of the current git directory), so the
// configured host is passed explicitly.
func TestLoginIsAskedFromTheConfiguredHost(t *testing.T) {
	r := &recorder{out: `{"username":"kvaps"}`}
	c := newTestCLI("gitlab.example.org", r)
	if _, err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.calls[0], " ")
	if got != "api user --hostname gitlab.example.org" {
		t.Fatalf("Login ran %q, want %q", got, "api user --hostname gitlab.example.org")
	}
}

// A reply without a username — an error page, an empty object — is not a
// login; it is refused rather than cached as "".
func TestLoginRefusesAReplyWithoutAUsername(t *testing.T) {
	for _, out := range []string{`{"name":"K"}`, `not json`, `{"username":""}`} {
		r := &recorder{out: out}
		c := newTestCLI("gitlab.com", r)
		if got, err := c.Login(context.Background()); err == nil {
			t.Errorf("Login() on %q = %q, want an error", out, got)
		}
		if _, err := c.Login(context.Background()); err == nil {
			t.Errorf("Login() on %q succeeded on retry, a failure must not be cached", out)
		}
		if len(r.calls) != 2 {
			t.Errorf("Login() on %q invoked glab %d times, want 2", out, len(r.calls))
		}
	}
}

// When glab itself fails, its stderr is what tells the person what went
// wrong (not logged in, unknown host) — it must survive into the error
// returned by Token and Login rather than be swallowed.
func TestRunnerErrorSurfacesWithItsText(t *testing.T) {
	r := &recorder{err: errors.New("glab config get token --host gitlab.com: no token found for gitlab.com: exit status 1")}
	c := newTestCLI("gitlab.com", r)

	_, err := c.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no token found for gitlab.com") {
		t.Fatalf("Token() error = %v, want the glab stderr text in it", err)
	}
	if !errors.Is(err, r.err) {
		t.Fatalf("Token() error does not wrap the runner's error: %v", err)
	}
	_, err = c.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no token found for gitlab.com") {
		t.Fatalf("Login() error = %v, want the glab stderr text in it", err)
	}
}

// Run is the real exec: a failing command's stderr is folded into the error
// together with the command line, and a clean run returns stdout untouched.
// The folding is exercised through /bin/sh so the test needs no glab binary.
func TestRunFoldsStderrIntoTheError(t *testing.T) {
	ctx := context.Background()
	out, err := execBinary(ctx, "sh", "-c", "printf 'hello\\n'")
	if err != nil || out != "hello\n" {
		t.Fatalf("execBinary(sh echo) = %q, %v; want %q, nil", out, err, "hello\n")
	}

	_, err = execBinary(ctx, "sh", "-c", "echo 'no token found' >&2; exit 3")
	if err == nil {
		t.Fatal("a failing command must return an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no token found") {
		t.Errorf("error %q does not carry the command's stderr", msg)
	}
	if !strings.HasPrefix(msg, "sh -c ") {
		t.Errorf("error %q does not name the command that failed", msg)
	}

	_, err = execBinary(ctx, "sh", "-c", "exit 2")
	if err == nil || !strings.HasPrefix(err.Error(), "sh -c exit 2: ") {
		t.Errorf("a silent failure must still name the command: %v", err)
	}
}
