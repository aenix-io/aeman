// Package glabcli provides access to the local GitLab CLI (glab) for
// authentication and lightweight command execution — the GitLab counterpart
// of ghcli, standing in for a signed-in person on a single-user server.
package glabcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
)

// tokenTTL is how long a token fetched from glab is reused before re-reading it.
const tokenTTL = 5 * time.Minute

// defaultHost is the instance glab talks to when the board does not name one.
const defaultHost = "gitlab.com"

// CLI resolves a GitLab token and the signed-in login from the local glab CLI
// for one GitLab instance, caching both. Unlike gh, glab keeps a credential
// per host, so the host is part of every question asked of it.
type CLI struct {
	host string

	// run executes glab; tests swap it for a scripted reply so the package is
	// exercised without the binary. now is the clock the token TTL is judged by.
	run func(ctx context.Context, args ...string) (string, error)
	now func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
	login  string
}

var _ forge.CLI = (*CLI)(nil)

// New returns a CLI for the GitLab instance at host ("gitlab.com" when empty),
// backed by `glab config get token` and `glab api user`.
func New(host string) *CLI {
	if host == "" {
		host = defaultHost
	}
	return &CLI{host: host, run: Run, now: time.Now}
}

// Token returns a cached GitLab token for the host, fetching a fresh one when
// the cache has expired. It returns an error if glab is missing or no token is
// stored for the host.
func (c *CLI) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && c.now().Before(c.expiry) {
		return c.token, nil
	}

	out, err := c.run(ctx, "config", "get", "token", "--host", c.host)
	if err != nil {
		return "", fmt.Errorf("read token from glab: %w", err)
	}
	tok := strings.TrimSpace(out)
	if tok == "" {
		return "", errors.New("glab returned an empty token; run `glab auth login`")
	}
	c.token = tok
	c.expiry = c.now().Add(tokenTTL)
	return tok, nil
}

// Login returns the username glab is signed in as on the host, cached for the
// lifetime of the CLI value (it is read on every API request in local mode,
// and the glab identity does not change under a running server). The host is
// passed explicitly: `glab api` would otherwise default to gitlab.com or to
// the instance of the current git directory, which need not be the board's.
func (c *CLI) Login(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.login != "" {
		return c.login, nil
	}

	out, err := c.run(ctx, "api", "user", "--hostname", c.host)
	if err != nil {
		return "", err
	}
	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(out), &user); err != nil {
		return "", fmt.Errorf("decode glab api user: %w", err)
	}
	if user.Username == "" {
		return "", errors.New("glab api user returned no username; run `glab auth login`")
	}
	c.login = user.Username
	return c.login, nil
}

// Run executes `glab` with the given arguments and returns its stdout. The
// glab binary name is fixed and arguments are supplied by aeman itself, never
// by untrusted input.
func Run(ctx context.Context, args ...string) (string, error) {
	return execBinary(ctx, "glab", args...)
}

// execBinary runs one command and folds its stderr into the error, so the
// tool's own explanation (not logged in, unknown host) reaches the person
// instead of a bare exit status.
func execBinary(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // fixed binary, internal args
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		line := name + " " + strings.Join(args, " ")
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s: %s: %w", line, msg, err)
		}
		return "", fmt.Errorf("%s: %w", line, err)
	}
	return stdout.String(), nil
}
