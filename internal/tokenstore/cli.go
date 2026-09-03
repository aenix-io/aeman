package tokenstore

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
)

// tokenTTL is how long a token read from the store is reused before it is
// read again — the window gh and glab use, so a credential replaced on the
// machine reaches a running server as quickly as it did before.
const tokenTTL = 5 * time.Minute

// forgeTimeout bounds the one call this package makes to the forge. It
// happens under the lock Token also takes, so a connection that never
// answers would hold every later token read behind it; http.DefaultClient
// waits forever, which is why it is not the fallback. The server bounds
// its own forge requests the same way.
const forgeTimeout = 30 * time.Second

// CLI is the secret store standing in for the forge's command-line tool:
// the token `aeman login` put there, and the person the forge says that
// token belongs to. It is the peer of ghcli.TokenSource and glabcli.CLI,
// and is asked before either of them.
type CLI struct {
	store  Store
	forge  forge.Forge
	host   string
	client *http.Client

	// now is the clock the token window is judged by; tests replace it.
	now func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
	login  string
}

var _ forge.CLI = (*CLI)(nil)

// NewCLI returns the store's forge.CLI for the token held under host. A nil
// client is one bounded by forgeTimeout.
func NewCLI(store Store, f forge.Forge, host string, client *http.Client) *CLI {
	if client == nil {
		client = &http.Client{Timeout: forgeTimeout}
	}
	return &CLI{store: store, forge: f, host: host, client: client, now: time.Now}
}

// Token is the stored token, with the store's own error untouched — the
// caller decides what a failure means, and the only distinction the store
// can offer it is ErrNotFound against everything else.
//
// It is cached for tokenTTL because the server asks for it on every request
// in the local mode, and reading it runs the platform's secret tool — tens
// of milliseconds, behind a process-wide lock. A read that found nothing is
// not cached, so a token stored while the process runs is picked up at once
// rather than after the window.
func (c *CLI) Token(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokenLocked()
}

// tokenLocked is Token with c.mu already held, so Login can read the token
// without dropping the lock it holds to fill the identity in once.
func (c *CLI) tokenLocked() (string, error) {
	if c.token != "" && c.now().Before(c.expiry) {
		return c.token, nil
	}
	tok, err := c.store.Get(c.host)
	if err != nil {
		return "", err
	}
	if tok != c.token {
		// A different token is a different person. The identity cached for
		// the one before it would otherwise put that person's name on
		// pushes made with this one, which is the exact failure the
		// election exists to prevent.
		c.login = ""
	}
	c.token, c.expiry = tok, c.now().Add(tokenTTL)
	return tok, nil
}

// Login is who the forge says the stored token belongs to, which need not
// be whoever gh or glab is signed in as on this machine — a stored bot
// token belongs to the bot, and that is who its commits are by. It is asked
// once per token, not once per process: this is on the path of every
// request in the local mode, but `aeman login` in another terminal
// replaces the token under a running server, and the owner asked about
// has to be the owner of the token now in use.
func (c *CLI) Login(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// The token is read FIRST, before the cached login is trusted: reading
	// it is what notices a replacement and drops the name that came with
	// the old one. Asking the other way round would freeze the identity
	// for any caller that never asks for a token — which is `aeman mcp`,
	// the case this whole path exists for.
	tok, err := c.tokenLocked()
	if err != nil {
		return "", err
	}
	if c.login != "" {
		return c.login, nil
	}
	user, err := c.forge.User(ctx, c.client, tok)
	if err != nil {
		return "", err
	}
	c.login = user.Login
	return c.login, nil
}
