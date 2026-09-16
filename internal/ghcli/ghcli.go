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
	token  string
	expiry time.Time
}

var _ forge.CLI = (*TokenSource)(nil)

// NewTokenSource returns a TokenSource backed by `gh auth token`.
func NewTokenSource() *TokenSource {
	return &TokenSource{}
}

// Token returns a cached GitHub token, fetching a fresh one when the cache has
// expired. It returns an error if gh is missing or the user is not logged in.
func (t *TokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.token != "" && time.Now().Before(t.expiry) {
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
	t.token = tok
	t.expiry = time.Now().Add(tokenTTL)
	return tok, nil
}

// Login returns the login of the currently authenticated GitHub user. The gh
// identity is one per machine, not per TokenSource, so this is the package
// Login and shares its process-wide cache.
func (t *TokenSource) Login(ctx context.Context) (string, error) {
	return login(ctx, t)
}

var (
	loginMu     sync.Mutex
	cachedLogin string
	// Unanswered results share the successful cache's lock, but expire and
	// only apply to the token that was read when the lookup failed.
	unanswered    string
	unansweredAt  time.Time
	unansweredErr error
	loginTokens   = NewTokenSource()
	loginNow      = time.Now // tests advance the unanswered window without sleeping
)

// Login returns the login of the currently authenticated GitHub user, cached
// for the lifetime of the process (it is read on every API request in local
// mode, and the gh identity does not change under a running server).
// Unanswered lookups are cached per token for forge.UnansweredTTL instead.
func Login(ctx context.Context) (string, error) {
	return login(ctx, loginTokens)
}

func login(ctx context.Context, tokens *TokenSource) (string, error) {
	loginMu.Lock()
	defer loginMu.Unlock()
	if cachedLogin != "" {
		return cachedLogin, nil
	}
	// Reuse the source's normal token window rather than running auth token
	// on every request. A newly read value bypasses the unanswered window.
	tok, tokenErr := tokens.Token(ctx)
	if tokenErr == nil && tok == unanswered && loginNow().Sub(unansweredAt) < forge.UnansweredTTL {
		return "", unansweredErr
	}
	out, err := Run(ctx, "api", "user", "--jq", ".login")
	if err != nil || strings.TrimSpace(out) == "" {
		// Preserve both the underlying error and an empty, nil-error reply.
		// Without a token there is no safe cache key. The caller's own
		// cancellation is not an answer about the shared credential either.
		if tokenErr == nil && ctx.Err() == nil {
			unanswered, unansweredAt, unansweredErr = tok, loginNow(), err
		}
		return "", err
	}
	unanswered, unansweredErr = "", nil
	cachedLogin = strings.TrimSpace(out)
	return cachedLogin, nil
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
