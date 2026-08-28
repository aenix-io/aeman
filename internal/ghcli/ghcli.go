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
	return Login(ctx)
}

var (
	loginMu     sync.Mutex
	cachedLogin string
)

// Login returns the login of the currently authenticated GitHub user, cached
// for the lifetime of the process (it is read on every API request in local
// mode, and the gh identity does not change under a running server).
func Login(ctx context.Context) (string, error) {
	loginMu.Lock()
	defer loginMu.Unlock()
	if cachedLogin != "" {
		return cachedLogin, nil
	}
	out, err := Run(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	cachedLogin = strings.TrimSpace(out)
	return cachedLogin, nil
}

// Run executes `gh` with the given arguments and returns its stdout. The gh
// binary name is fixed and arguments are supplied by aeman itself, never by
// untrusted input.
func Run(ctx context.Context, args ...string) (string, error) {
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
