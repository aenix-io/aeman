package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/ghcli"
	"github.com/aenix-io/aeman/internal/glabcli"
	"github.com/aenix-io/aeman/internal/server"
	"github.com/aenix-io/aeman/internal/tokenstore"
)

// forgeTimeout bounds a call to the forge. `aeman login` has a person
// waiting at a prompt, so an unreachable forge must fail rather than hang
// until they interrupt it; the sources of the chain reuse it because they
// ask under a lock, and http.DefaultClient never gives up.
const forgeTimeout = 30 * time.Second

// forgeToolCLI is the forge's own command-line tool as the last source. A
// variable because it is the one source a test cannot supply: it execs
// whatever gh or glab the machine has, so the order of the chain cannot
// be asserted end to end without standing in for it.
var forgeToolCLI = func(f forge.Forge) forge.CLI {
	if f != nil && f.Kind() == forge.GitLab {
		return glabcli.New(f.Host())
	}
	return ghcli.NewTokenSource()
}

// cliFor is where a single-user run reads its credential and identity: the
// forge's token variables, the OS keychain that `aeman login` writes, then
// the forge's own command-line tool — gh for GitHub, glab for GitLab, the
// latter asked about the instance the board lives on. Every question of
// WHICH instance goes to the forge, so no repository URL is parsed here.
// A nil store leaves the keychain out, and so does a forge nobody named:
// a token's owner is read from the forge, which an unnamed one cannot
// answer.
func cliFor(f forge.Forge, store tokenstore.Store, env func(string) string, log *slog.Logger) forge.CLI {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	var sources []forge.CLI
	if f != nil {
		sources = append(sources, newEnvCLI(f, env, nil))
		if store != nil {
			sources = append(sources, tokenstore.NewCLI(store, f, nil))
		}
	}
	sources = append(sources, forgeToolCLI(f))
	return &chain{sources: sources, forge: f, log: log}
}

// envCLI is the forge's token variables as a source of the chain, so the
// credential the environment supplies and the person it belongs to are one
// answer. Without it the environment would decide the push while the
// keychain decided the name on the commits — which gh hid for as long as
// it was the only other source, because it hands GITHUB_TOKEN back
// unchanged. AEMAN_GIT_TOKEN is deliberately not here: it names the
// server's own credential and never named a person.
type envCLI struct {
	forge  forge.Forge
	env    func(string) string
	client *http.Client

	// now is the clock the unanswered window is judged by; tests replace
	// it, as the keychain's source does for its token window.
	now func() time.Time

	mu      sync.Mutex
	login   string
	refused string
	// unanswered is the token value the forge could not name an owner
	// for. A refusal stands until the value changes because it is an
	// answer; this is the absence of one, so it expires.
	unanswered    string
	unansweredAt  time.Time
	unansweredErr error
}

var (
	_ forge.CLI        = (*envCLI)(nil)
	_ forge.Credential = (*envCLI)(nil)
)

func newEnvCLI(f forge.Forge, env func(string) string, client *http.Client) *envCLI {
	if env == nil {
		env = osEnv
	}
	if client == nil {
		// Not http.DefaultClient: it waits forever, and the forge is asked
		// under the lock this source's Login holds.
		client = &http.Client{Timeout: forgeTimeout}
	}
	return &envCLI{forge: f, env: env, client: client, now: time.Now}
}

// Token is the first of the forge's variables that holds one. An empty
// environment is nothing to report, not a failure: the chain moves on.
func (c *envCLI) Token(context.Context) (string, error) { return c.token() }

// token is the lookup itself, taking no lock, so Login can call it under
// the one it already holds.
func (c *envCLI) token() (string, error) {
	for _, key := range tokenEnv(c.forge) {
		if v := strings.TrimSpace(c.env(key)); v != "" {
			return v, nil
		}
	}
	return "", nil
}

// Login is who the forge says the environment's token belongs to, asked
// once — the same question the keychain's source asks of its own token.
//
// Unlike that one it does not re-derive the name when the token changes,
// and does not need to: a process's environment does not change under it,
// so the variable read here is the one read at start-up. The keychain's
// can be replaced by `aeman login` in another terminal, which is why the
// two are asymmetric.
func (c *envCLI) Login(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginLocked(ctx)
}

// TokenAndLogin answers both under one lock, so the pair cannot span two
// reads of the environment.
func (c *envCLI) TokenAndLogin(ctx context.Context) (string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tok, err := c.token()
	if err != nil {
		return "", "", err
	}
	login, err := c.loginLocked(ctx)
	switch {
	case errors.Is(err, forge.ErrBadToken):
		// A refused token is a dead credential and says so; the chain
		// ends this source's reign on it.
		return "", "", err
	case err != nil:
		// An unreachable forge is not a missing credential — the same
		// rule the chain applies, kept here so both levels agree.
		return tok, "", nil //nolint:nilerr // the token is the answer
	}
	return tok, login, nil
}

// loginLocked is the owner lookup with c.mu already held, and holds the
// answer both entry points share. The cache belongs here and not in
// Login: the chain asks a source that implements forge.Credential through
// TokenAndLogin, so a guard in Login alone is one production never
// reaches, and every request would put a /user call on the wire while
// this lock is held. A refusal is remembered against the token value, so
// a stale exported variable costs one call to the forge rather than one
// per request.
func (c *envCLI) loginLocked(ctx context.Context) (string, error) {
	if c.login != "" {
		return c.login, nil
	}
	tok, err := c.token()
	if err != nil {
		return "", err
	}
	if tok == "" {
		return "", errors.New("no token in the environment to name a person")
	}
	if tok == c.refused {
		return "", forge.ErrBadToken
	}
	if tok == c.unanswered && c.now().Sub(c.unansweredAt) < forge.UnansweredTTL {
		return "", c.unansweredErr
	}
	user, err := c.forge.User(ctx, c.client, tok)
	if err != nil {
		if errors.Is(err, forge.ErrBadToken) {
			c.refused = tok
			return "", err
		}
		// Only what the FORGE did is remembered. An error that came
		// from the caller's own context says nothing about the forge,
		// and the window is shared: recording it would hand the next
		// caller, with a live context, a stale error and no call. The
		// source's own client timeout still lands here, which is the
		// case the window is for.
		if ctx.Err() == nil {
			c.unanswered, c.unansweredAt, c.unansweredErr = tok, c.now(), err
		}
		return "", err
	}
	c.unanswered, c.unansweredErr = "", nil
	c.login = user.Login
	return c.login, nil
}

// chain asks its sources in order until one hands over a token, and
// remembers which one did. The election is sticky and has no expiry: the
// login has to come from whoever supplied the credential, or a process
// pushes with one account's token and attributes the commits to another's
// name. log must not be nil; cliFor is what fills it in, along with the
// forge the "no token anywhere" message is worded for.
type chain struct {
	mu      sync.Mutex
	sources []forge.CLI
	won     forge.CLI
	forge   forge.Forge
	log     *slog.Logger
}

var _ forge.Credential = (*chain)(nil)

// Token is the elected source's credential, decided by the same path
// that answers for its owner — so a token the forge refuses ends that
// source's reign here as well as everywhere else. All three entry points
// share ONE election, or the push credential comes from a source the
// identity has already moved on from: the board then reads, the page
// names the right person, and every push fails with nothing saying why.
//
// The owner lookup that decides it is cached per token value, so this
// costs one call to the forge and not one per ask.
func (c *chain) Token(ctx context.Context) (string, error) {
	tok, _, err := c.TokenAndLogin(ctx)
	return tok, err
}

// electExcept is elect, passing over the sources a caller has already
// found wanting in this attempt.
func (c *chain) electExcept(ctx context.Context, skip map[forge.CLI]bool) (string, forge.CLI, error) {
	c.mu.Lock()
	sources, won := c.sources, c.won
	c.mu.Unlock()
	if won != nil && skip[won] {
		won = nil
	}
	if won != nil {
		// The winner answers as long as it has something to answer with.
		// When it stops — `aeman logout` emptied the store it read, gh was
		// signed out — the election runs again rather than this reporting
		// "no token" for the life of the process: remembering a source
		// keeps the token and the login on ONE source, it does not stop
		// the chain looking. An empty answer ends a reign as surely as an
		// error, which is the same guard the election below applies.
		if tok, err := won.Token(ctx); err == nil && strings.TrimSpace(tok) != "" {
			return tok, won, nil
		}
		// Compare-and-clear, not a bare clear: the lock was released to
		// ask, and another goroutine may have elected since.
		c.retire(won)
	}
	for _, src := range sources {
		if skip[src] {
			continue
		}
		tok, err := src.Token(ctx)
		if err != nil {
			c.log.Debug("no token from this source", "err", err)
			continue
		}
		if strings.TrimSpace(tok) == "" {
			continue
		}
		c.mu.Lock()
		c.won = src
		c.mu.Unlock()
		return tok, src, nil
	}
	return "", nil, noTokenError(c.forge)
}

// TokenAndLogin is the elected source's token and its owner, decided in
// ONE election. The server asks for both on every request in the local
// mode, and asking twice let a re-election between the calls hand back a
// token from one source and a name from the next.
func (c *chain) TokenAndLogin(ctx context.Context) (string, string, error) {
	// A refusal ends a reign, so the loop runs at most once per source.
	var refused error
	skip := map[forge.CLI]bool{}
	c.mu.Lock()
	attempts := len(c.sources)
	c.mu.Unlock()
	for range attempts {
		tok, login, err, retry := c.pairOnce(ctx, skip)
		if !retry {
			if err != nil && refused != nil {
				// Nothing below answered either: the person needs to hear
				// that a token was REFUSED, not that there is none — the
				// second sends them to add one they already have.
				return "", "", refused
			}
			return tok, login, err
		}
		if errors.Is(err, forge.ErrBadToken) {
			// A source that went quiet says nothing the person can act
			// on; one whose token was REFUSED does, and it must survive
			// to the end — otherwise they are told to add a credential
			// they already have.
			refused = err
		}
	}
	if refused != nil {
		return "", "", refused
	}
	return "", "", noTokenError(c.forge)
}

// pairOnce elects a source outside skip and asks it both questions. retry
// says the source was refused by the forge and the next one should be
// tried: a token the forge will not accept is a source with nothing left
// to give, which is the same thing an empty answer means to the election.
func (c *chain) pairOnce(ctx context.Context, skip map[forge.CLI]bool) (tok, login string, err error, retry bool) {
	tok, won, err := c.electExcept(ctx, skip)
	if err != nil {
		return "", "", err, false
	}
	if pair, ok := won.(forge.Credential); ok {
		// One read inside the source, so its own two answers cannot come
		// from two different tokens.
		tok, login, err = pair.TokenAndLogin(ctx)
	} else {
		login, err = won.Login(ctx)
	}
	if errors.Is(err, forge.ErrBadToken) {
		skip[won] = true
		c.retire(won)
		return "", "", err, true
	}

	// A source that answers the pair with no token has nothing to give,
	// whatever it says about the owner or about why. Returning it would
	// be the one shape Token forbids — an empty answer with no error —
	// which the server reads as signed in, hiding the banner that would
	// name the missing credential. The source emptied between the
	// election and this call: electExcept will not elect an empty one, so
	// `aeman logout` in another terminal is how it happens.
	if strings.TrimSpace(tok) == "" {
		// Its reign ends here and the next source is asked, exactly as
		// when the forge refuses a token: a winner that emptied under us
		// must not take the request down while a source below it holds a
		// good credential.
		skip[won] = true
		c.retire(won)
		if err != nil {
			// The reason is not the answer — the person is told which
			// commands would give them a token — but it is the only
			// account of why the source went quiet, and dropping it
			// leaves `--verbose` with nothing to show. Logged where the
			// election logs the same class.
			c.log.Debug("the winner stopped answering", "err", err)
		}
		return "", "", err, true
	}
	if err != nil {
		// An unreachable forge is not a missing credential. The promise
		// here is that the two answers come from ONE source, not that
		// both arrive: the token is good, only the question of whose it
		// is went unanswered. Failing would take every read down on a
		// transient outage and tell the person to run `aeman login` for a
		// token they already have. Callers guard on an empty login, as
		// they did before the pair existed.
		c.log.Debug("the forge could not say who the token belongs to", "err", err)
		return tok, "", nil, false //nolint:nilerr // the token is the answer; an unreachable forge must not read as no credential
	}
	return tok, login, nil, false
}

// retire drops a source's reign so the next election reconsiders.
func (c *chain) retire(src forge.CLI) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.won == src {
		c.won = nil
	}
}

// Login is the elected source's, taken through the same path the pair
// uses — so a token the forge refuses ends that source's reign here as
// well. `aeman mcp` asks for a login and never for a token, so a rule
// that lived only on the pair would leave that process with no identity
// at all while a working source sat underneath the refused one.
func (c *chain) Login(ctx context.Context) (string, error) {
	_, login, err := c.TokenAndLogin(ctx)
	return login, err
}

// tokenEnv is the environment variables a forge's token is taken from, in
// order, before the CLI is asked.
func tokenEnv(f forge.Forge) []string {
	if f != nil && f.Kind() == forge.GitLab {
		return []string{"GITLAB_TOKEN"}
	}
	return []string{"GITHUB_TOKEN", "GH_TOKEN"}
}

// resolveForgeToken is the credential for the board's forge, asked of the
// chain and of nothing else. The error names the login command that would
// fix it.
//
// It reads no variables of its own. Doing that ahead of the chain gave
// the environment two turns in the order, and the second one was not the
// chain's: a revoked GITHUB_TOKEN in front of a good keychain item became
// the push credential while the identity came from the keychain, so the
// commits carried a name the push did not belong to.
func resolveForgeToken(ctx context.Context, f forge.Forge, cli forge.CLI) (string, error) {
	tok, err := cli.Token(ctx)
	if err != nil {
		return "", err
	}
	if tok = strings.TrimSpace(tok); tok == "" {
		return "", noTokenError(f)
	}
	return tok, nil
}

// noTokenError is what a caller with no credential anywhere is told, in
// one place: the chain returns it when no source answered, and this
// function returns it when a source handed back nothing but whitespace.
// A second wording would mean the same dead end reads differently
// depending on which layer noticed it first.
func noTokenError(f forge.Forge) error {
	return fmt.Errorf("no %s token; set %s, run `aeman login`, or run `%s auth login`", labelOf(f), tokenEnv(f)[0], cliNameOf(f))
}

func labelOf(f forge.Forge) string {
	if f == nil {
		return "GitHub"
	}
	return f.Label()
}

func cliNameOf(f forge.Forge) string {
	if f != nil && f.Kind() == forge.GitLab {
		return "glab"
	}
	return "gh"
}

// oauthPair is the client credential pair of one forge's OAuth application,
// read from the environment. Exactly one forge may be configured, and it
// must be the forge the board lives on — a GitHub sign-in cannot vouch for
// a GitLab repository.
func oauthPair(f forge.Forge, env func(string) string) (id, secret string, err error) {
	gh := env("AEMAN_GITHUB_CLIENT_ID") != "" && env("AEMAN_GITHUB_CLIENT_SECRET") != ""
	gl := env("AEMAN_GITLAB_CLIENT_ID") != "" && env("AEMAN_GITLAB_CLIENT_SECRET") != ""
	switch {
	case gh && gl:
		return "", "", fmt.Errorf("both AEMAN_GITHUB_CLIENT_* and AEMAN_GITLAB_CLIENT_* are set; a board signs in with one forge")
	case gh:
		if f.Kind() != forge.GitHub {
			return "", "", fmt.Errorf("AEMAN_GITHUB_CLIENT_* is set but the board's forge is %s (set --forge github, or the GitLab pair)", f.Kind())
		}
		return env("AEMAN_GITHUB_CLIENT_ID"), env("AEMAN_GITHUB_CLIENT_SECRET"), nil
	case gl:
		if f.Kind() != forge.GitLab {
			return "", "", fmt.Errorf("AEMAN_GITLAB_CLIENT_* is set but the board's forge is %s (set --forge gitlab or --gitlab-url)", f.Kind())
		}
		return env("AEMAN_GITLAB_CLIENT_ID"), env("AEMAN_GITLAB_CLIENT_SECRET"), nil
	}
	return "", "", nil
}

// missingTokens names the board's repositories that have no credential —
// each with the variable that would give it one. In the OAuth mode the
// server needs one per repository: it pushes them and asks the forge who
// may read them with its own. Empty when every repository is covered,
// whether by its own token or by the shared one.
func missingTokens(cfg *server.GitConfig) []string {
	var out []string
	for _, r := range cfg.Repos {
		if r.Token == "" {
			out = append(out, r.Name+" ("+tokenEnvFor(r.Name)+")")
		}
	}
	return out
}

// osEnv is os.Getenv with the surrounding whitespace dropped.
func osEnv(key string) string { return strings.TrimSpace(os.Getenv(key)) }
