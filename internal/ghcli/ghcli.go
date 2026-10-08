// Package ghcli provides access to the local GitHub CLI (gh) for
// authentication and lightweight command execution.
package ghcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
)

// tokenTTL is how long a token fetched from gh is reused before re-reading it.
const tokenTTL = 5 * time.Minute

// TokenSource resolves a GitHub token from the local gh CLI and caches it. It
// is also the GitHub side of forge.CLI: the token plus the signed-in login.
type TokenSource struct {
	mu     sync.Mutex
	now    func() time.Time
	token  string
	expiry time.Time
	login  string

	// Unanswered lookups are cached briefly per token. A changed token is a
	// different credential and must be asked about immediately.
	unanswered    string
	unansweredAt  time.Time
	unansweredErr error
}

var (
	_ forge.CLI        = (*TokenSource)(nil)
	_ forge.Credential = (*TokenSource)(nil)
)

// NewTokenSource returns a TokenSource backed by `gh auth token`.
func NewTokenSource() *TokenSource {
	return &TokenSource{now: time.Now}
}

func (t *TokenSource) timeNow() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// Token returns a cached GitHub token, fetching a fresh one when the cache has
// expired. It returns an error if gh is missing or the user is not logged in.
func (t *TokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tokenLocked(ctx)
}

func (t *TokenSource) tokenLocked(ctx context.Context) (string, error) {
	if t.token != "" && t.timeNow().Before(t.expiry) {
		return t.token, nil
	}

	out, err := Run(ctx, "auth", "token")
	if err != nil {
		return "", fmt.Errorf("read token from gh: %w", err)
	}
	tok := strings.TrimSpace(out)
	if tok == "" {
		return "", errors.New("gh returned an empty token; run `gh auth login`")
	}
	if tok != t.token {
		// The login belongs to the token, not to the gh process. Re-reading a
		// different credential must invalidate the identity cached with the old
		// one before either can be used for a commit.
		t.login = ""
	}
	t.token = tok
	t.expiry = t.timeNow().Add(tokenTTL)
	return tok, nil
}

// Login returns the login belonging to the source's current token. It is
// cached per TokenSource and per token so replacing the gh credential makes a
// running process refresh the identity on the same schedule as the token.
func (t *TokenSource) Login(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tok, tokenErr := t.tokenLocked(ctx)
	return t.loginLocked(ctx, tok, tokenErr)
}

// TokenAndLogin returns a token and its owner from one locked view of this
// source, so a refresh cannot split the pair across two credentials.
func (t *TokenSource) TokenAndLogin(ctx context.Context) (string, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tok, err := t.tokenLocked(ctx)
	if err != nil {
		return "", "", err
	}
	login, err := t.loginLocked(ctx, tok, nil)
	return tok, login, err
}

// loginLocked resolves the identity with t.mu held. tokenErr is kept separate
// because Login historically still asks gh for its own answer when `gh auth
// token` fails, but such an answer cannot safely populate a token-keyed cache.
func (t *TokenSource) loginLocked(ctx context.Context, tok string, tokenErr error) (string, error) {
	if tokenErr == nil {
		if t.login != "" {
			return t.login, nil
		}
		if tok == t.unanswered && t.timeNow().Sub(t.unansweredAt) < forge.UnansweredTTL {
			return "", t.unansweredErr
		}
	}

	out, err := Run(ctx, "api", "user", "--jq", ".login")
	login := strings.TrimSpace(out)
	if err != nil || login == "" {
		// Without a token there is no safe cache key. The caller's own
		// cancellation is not an answer about the shared credential either.
		if tokenErr == nil && ctx.Err() == nil {
			t.unanswered, t.unansweredAt, t.unansweredErr = tok, t.timeNow(), err
		}
		return "", err
	}
	if tokenErr == nil {
		t.unanswered, t.unansweredErr = "", nil
		t.login = login
	}
	return login, nil
}

var loginTokens = NewTokenSource()

// Login returns the login of the currently authenticated GitHub user using the
// package's default token source. Callers that need an independent credential
// source should create a TokenSource and use its Login method.
func Login(ctx context.Context) (string, error) {
	return loginTokens.Login(ctx)
}

// runGH is the seam every gh invocation goes through. Tests replace it so the
// CLI is never executed and the login cache can be exercised without a machine
// that has gh installed and signed in.
var runGH = runCLI

// Run executes `gh` with the given arguments and returns its stdout. The gh
// binary name is fixed and arguments are supplied by aeman itself, never by
// untrusted input.
func Run(ctx context.Context, args ...string) (string, error) {
	return runGH(ctx, args...)
}

func runCLI(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...) //nolint:gosec // fixed binary, internal args
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("gh %s: %s: %w", strings.Join(args, " "), msg, err)
		}
		return "", fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
