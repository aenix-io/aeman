// Package forge is the code host behind a board: the identity provider the
// server signs people in with, the authority on who may read or write a
// repository, the directory of names and avatars, and the credential git
// pushes with. GitHub was the first; GitLab is the second. Everything the
// server does with a forge goes through the Forge interface, and the server
// never assembles a forge URL of its own.
package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// UnansweredTTL is how long a source remembers that the forge could not
// say who a token belongs to — a 403 for a token with no user behind it,
// a 5xx, a timeout, a DNS failure. Without it the question is asked again
// on every request, under the lock the source holds while it asks, so one
// unreachable forge serialises every caller behind a full timeout each.
//
// It matches the ceiling those sources put on the call, so a window holds
// at most one attempt that runs to the end; and half a minute is short
// enough that a forge coming back — a VPN reconnecting, a rate limit
// lifting — names the person again without a restart. A refusal is NOT
// this: a 401 is an answer, and it is remembered until the token changes.
const UnansweredTTL = 30 * time.Second

// Kind names a forge.
type Kind string

// The forges aeman knows.
const (
	GitHub Kind = "github"
	GitLab Kind = "gitlab"
)

// User is the signed-in person as the forge describes them.
type User struct {
	Login     string
	Name      string
	AvatarURL string
}

// Person is what the forge knows about a login: the name and the avatar.
// A forge that cannot say (GitHub's permission check names nobody) leaves
// Name empty and builds the avatar from the login.
type Person struct {
	Login     string
	Name      string
	AvatarURL string
}

// Errors a forge reports.
var (
	// ErrBadToken is the forge refusing the token it was given (401).
	ErrBadToken = errors.New("forge: the token was rejected")
	// ErrNotFound is a login the forge does not know.
	ErrNotFound = errors.New("forge: no such person")
	// ErrRateLimited is the forge throttling us. It shares the 403 of "you
	// may not see this" and must never be read as one: a moment of
	// throttling would otherwise be remembered as a visitor's lack of
	// access to their own board.
	ErrRateLimited = errors.New("forge: rate limited")
)

// throttled reports whether a refusal is the forge throttling rather than
// answering: 429, or a 403 that names a spent budget or a retry delay.
// GitHub uses 403 for both its primary and secondary rate limits; GitLab
// answers 429 and, behind some proxies, a 403 with Retry-After.
func throttled(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	return resp.Header.Get("Retry-After") != "" ||
		resp.Header.Get("X-RateLimit-Remaining") == "0" ||
		resp.Header.Get("RateLimit-Remaining") == "0"
}

// Forge is one code host.
type Forge interface {
	// Kind names the forge; Label is its display name for UI copy.
	Kind() Kind
	Label() string
	// Host is the instance this forge is: github.com, or the host of a
	// GitLab's base URL. It is what a credential belongs to, so a caller
	// keeping one token per forge keys it by this rather than parsing a
	// repository URL of its own.
	Host() string

	// AuthorizeURL and TokenURL are the OAuth endpoints the sign-in flow
	// uses; DefaultScopes the scope string a board needs when the operator
	// sets none.
	AuthorizeURL() string
	TokenURL() string
	DefaultScopes() string
	// ExchangeForm and RefreshForm are the token endpoint's request bodies
	// in the forge's dialect (GitLab names the grant type and wants the
	// redirect URI on a refresh too).
	ExchangeForm(clientID, secret, code, redirectURI string) url.Values
	RefreshForm(clientID, secret, refresh, redirectURI string) url.Values

	// User is who a token belongs to. A rejected token is ErrBadToken.
	User(ctx context.Context, client *http.Client, token string) (User, error)

	// RepoRef is a repository URL as the forge's API names the repository —
	// and the check that the URL names one at all.
	RepoRef(repoURL string) (string, error)
	// Access asks, as the visitor, what they may do with the repository:
	// read it, write it. A repository they cannot see is (false, false, nil);
	// a rejected token is ErrBadToken.
	Access(ctx context.Context, client *http.Client, token, repoURL string) (read, write bool, err error)
	// Readers is the subset of logins that may read the repository, asked
	// with the server's credential, keyed by login — with whatever the forge
	// said about each on the way (a member list carries names and avatars).
	Readers(ctx context.Context, client *http.Client, serverToken, repoURL string, logins []string) (map[string]Person, error)
	// Lookup finds a person by login. It may or may not call the forge.
	Lookup(ctx context.Context, client *http.Client, token, login string) (Person, error)

	// GitAuth is the HTTPS credential git operations use with a token.
	GitAuth(token string) *githttp.BasicAuth
}

// Detect names the forge for a repository: the explicit kind when given,
// else by the URL's host — github.com is GitHub, a host that says gitlab is
// that GitLab instance — else GitLab when a base URL for one is configured,
// else GitHub, the forge this code base grew up on.
func Detect(repoURL string, explicit Kind, gitlabBase string) (Forge, error) {
	host := repoHost(repoURL)
	base := strings.TrimRight(gitlabBase, "/")
	if base == "" && host != "" {
		base = "https://" + host
	}
	switch explicit {
	case GitHub:
		return NewGitHub(), nil
	case GitLab:
		if base == "" {
			return nil, errors.New("forge: gitlab needs a base URL (the repository host or --gitlab-url)")
		}
		return NewGitLab(base), nil
	case "":
	default:
		return nil, fmt.Errorf("forge: unknown kind %q (github or gitlab)", explicit)
	}
	switch {
	case host == "github.com":
		return NewGitHub(), nil
	case strings.Contains(host, "gitlab"):
		return NewGitLab(base), nil
	case gitlabBase != "":
		return NewGitLab(base), nil
	}
	return NewGitHub(), nil
}

// HostOf is the host of a repository URL — https://host/…, git@host:… —
// or "" for a bare owner/repo.
func HostOf(repoURL string) string { return repoHost(repoURL) }

// repoHost is the host of a repository URL — https://host/…, git@host:…,
// or "" for a bare owner/repo.
func repoHost(repoURL string) string {
	s := repoURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.IndexAny(s, "/"); j >= 0 {
			s = s[:j]
		}
		if at := strings.LastIndex(s, "@"); at >= 0 {
			s = s[at+1:]
		}
		return strings.ToLower(s)
	}
	if i := strings.Index(s, ":"); i >= 0 && !strings.Contains(s[:i], "/") {
		h := s[:i]
		if at := strings.LastIndex(h, "@"); at >= 0 {
			h = h[at+1:]
		}
		return strings.ToLower(h)
	}
	return ""
}

// repoPath is the path of a repository URL, .git dropped, as its segments:
// https://host/a/b/c.git → [a b c]; git@host:a/b.git → [a b]; a/b → [a b].
func repoPath(repoURL string) []string {
	s := strings.TrimSuffix(strings.TrimSuffix(strings.TrimRight(repoURL, "/"), ".git"), "/")
	hosted := false
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		hosted = true
	}
	if i := strings.Index(s, ":"); i >= 0 && !strings.Contains(s[:i], "/") {
		s = s[i+1:]
		hosted = false
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if hosted && len(parts) > 0 {
		parts = parts[1:]
	}
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// decodeJSON reads a JSON body, closing it.
func decodeJSON(resp *http.Response, v any) error {
	defer func() { _ = resp.Body.Close() }()
	return jsonDecode(resp.Body, v)
}
