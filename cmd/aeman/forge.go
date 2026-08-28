package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/ghcli"
	"github.com/aenix-io/aeman/internal/glabcli"
	"github.com/aenix-io/aeman/internal/server"
)

// cliFor is the forge's command-line tool — gh for GitHub, glab for GitLab
// — the thing a single-user server reads its identity and credential from.
// A GitLab CLI is asked about the instance the board lives on.
func cliFor(f forge.Forge, repoURL string) forge.CLI {
	if f != nil && f.Kind() == forge.GitLab {
		return glabcli.New(forge.HostOf(repoURL))
	}
	return ghcli.NewTokenSource()
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
