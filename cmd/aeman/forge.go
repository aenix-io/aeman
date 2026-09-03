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
	if f != nil && f.Kind() == forge.GitLab {
		sources = append(sources, glabcli.New(f.Host()))
	} else {
		sources = append(sources, ghcli.NewTokenSource())
	}
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

	mu    sync.Mutex
	login string
}

var _ forge.CLI = (*envCLI)(nil)

func newEnvCLI(f forge.Forge, env func(string) string, client *http.Client) *envCLI {
	if env == nil {
		env = osEnv
	}
	if client == nil {
		// Not http.DefaultClient: it waits forever, and the forge is asked
		// under the lock this source's Login holds.
		client = &http.Client{Timeout: forgeTimeout}
	}
	return &envCLI{forge: f, env: env, client: client}
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
func (c *envCLI) Login(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
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
	user, err := c.forge.User(ctx, c.client, tok)
	if err != nil {
		return "", err
	}
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

// Token is the first non-empty answer.
//
// A source that cannot produce one is skipped at debug level, whatever it
// says about why, and its error is not returned. No keychain item, no
// secret store, no gh on the PATH, a gh that is not signed in — on a
// machine that uses one source these are the ordinary state of the
// others, not faults, and each tool words its own refusal differently. A
// machine without gh would otherwise get exec's "file not found" where
// the caller has a message naming the commands that would fix it.
//
// There is deliberately no louder case. Telling "the store has your token
// and will not give it to me" apart from "the store has no token" needs a
// classification the store cannot make: on macOS every operation runs
// /usr/bin/security, whose one classified outcome is a missing item.
//
// Nothing anywhere is an ERROR, never an empty string with no error: the
// server asks this directly to decide whether it has a credential at all
// (`/api/config`), and a silent empty answer reads there as "signed in",
// suppressing the very banner that would say what is missing.
func (c *chain) Token(ctx context.Context) (string, error) {
	tok, _, err := c.elect(ctx)
	return tok, err
}

// elect is Token, also handing back the source that answered, so a caller
// that needs both cannot read the winner out of the field afterwards and
// find a different one there — a re-election between the two reads would
// otherwise pair one source's token with another's login.
func (c *chain) elect(ctx context.Context) (string, forge.CLI, error) {
	c.mu.Lock()
	sources, won := c.sources, c.won
	c.mu.Unlock()
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
		c.mu.Lock()
		c.won = nil
		c.mu.Unlock()
	}
	for _, src := range sources {
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

// Login is the elected source's. `aeman mcp` asks for it before anything
// asks for a token, so the election runs here too when it has not happened
// yet.
func (c *chain) Login(ctx context.Context) (string, error) {
	// The token is asked for first every time, not only when nothing has
	// won yet: it is what elects a source, and what re-elects one whose
	// token has gone. Asking the old winner for a login after that would
	// name the account the push is no longer made with. The sources cache
	// their own tokens, so this costs a map lookup or nothing at all.
	_, won, err := c.elect(ctx)
	if err != nil {
		return "", err
	}
	return won.Login(ctx)
}

// tokenEnv is the environment variables a forge's token is taken from, in
// order, before the CLI is asked.
func tokenEnv(f forge.Forge) []string {
	if f != nil && f.Kind() == forge.GitLab {
		return []string{"GITLAB_TOKEN"}
	}
	return []string{"GITHUB_TOKEN", "GH_TOKEN"}
}

// resolveForgeToken is the credential for the board's forge: the forge's
// token variables first, then the CLI's stored token. The error names the
// login command that would fix it.
//
// The variables are read here and again by the chain's own first source,
// which is redundant only when cli is that chain: any other forge.CLI —
// the tests use plain ones — still needs the environment consulted before
// it.
func resolveForgeToken(ctx context.Context, f forge.Forge, cli forge.CLI, env func(string) string) (string, error) {
	for _, key := range tokenEnv(f) {
		if v := strings.TrimSpace(env(key)); v != "" {
			return v, nil
		}
	}
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
