package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
// until they interrupt it.
const forgeTimeout = 30 * time.Second

// cliFor is where a single-user run reads its credential and identity: the
// OS keychain that `aeman login` writes, then the forge's own command-line
// tool — gh for GitHub, glab for GitLab, the latter asked about the
// instance the board lives on. A nil store leaves the keychain out, and so
// does a forge nobody named: the stored token's owner is read from the
// forge, which an unnamed one cannot answer.
func cliFor(f forge.Forge, repoURL string, store tokenstore.Store, log *slog.Logger) forge.CLI {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	var sources []forge.CLI
	if store != nil && f != nil {
		sources = append(sources, tokenstore.NewCLI(store, f, forgeHost(repoURL, ""), nil))
	}
	if f != nil && f.Kind() == forge.GitLab {
		sources = append(sources, glabcli.New(forge.HostOf(repoURL)))
	} else {
		sources = append(sources, ghcli.NewTokenSource())
	}
	return &chain{sources: sources, log: log}
}

// forgeHost is the keychain account for a board: the forge instance whose
// token it is. The repository's host, else a self-hosted GitLab named on
// its own, else github.com. One definition, so `aeman login` writes the
// item `aeman serve` and `aeman mcp` read.
func forgeHost(repoURL, gitlabURL string) string {
	if h := forge.HostOf(repoURL); h != "" {
		return h
	}
	if h := forge.HostOf(gitlabURL); h != "" {
		return h
	}
	return "github.com"
}

// chain asks its sources in order until one hands over a token, and
// remembers which one did. The election is sticky and has no expiry: the
// login has to come from whoever supplied the credential, or a process
// pushes with one account's token and attributes the commits to another's
// name. log must not be nil; cliFor is what fills it in.
type chain struct {
	mu      sync.Mutex
	sources []forge.CLI
	won     forge.CLI
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
func (c *chain) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	sources, won := c.sources, c.won
	c.mu.Unlock()
	if won != nil {
		return won.Token(ctx)
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
		return tok, nil
	}
	return "", nil
}

// Login is the elected source's. `aeman mcp` asks for it before anything
// asks for a token, so the election runs here too when it has not happened
// yet.
func (c *chain) Login(ctx context.Context) (string, error) {
	c.mu.Lock()
	won := c.won
	c.mu.Unlock()
	if won == nil {
		if _, err := c.Token(ctx); err != nil {
			return "", err
		}
		c.mu.Lock()
		won = c.won
		c.mu.Unlock()
	}
	if won == nil {
		return "", errors.New("no forge token, so no login to attribute the work to")
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
		return "", fmt.Errorf("no %s token; set %s or run `%s auth login`", labelOf(f), tokenEnv(f)[0], cliNameOf(f))
	}
	return tok, nil
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
