package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// github is GitHub: OAuth at github.com, REST at api.github.com, owner/repo
// slugs, a permissions block per repository, a collaborator permission per
// login, and avatars served off a CDN by login — no call needed.
type github struct {
	api string // REST base, https://api.github.com
}

const githubAPIBase = "https://api.github.com"

// NewGitHub is github.com.
func NewGitHub() Forge { return &github{api: githubAPIBase} }

// NewGitHubAt is GitHub with its REST base elsewhere — tests.
func NewGitHubAt(api string) Forge { return &github{api: strings.TrimRight(api, "/")} }

func (g *github) Kind() Kind    { return GitHub }
func (g *github) Label() string { return "GitHub" }
func (g *github) AuthorizeURL() string {
	return "https://github.com/login/oauth/authorize"
}
func (g *github) TokenURL() string {
	return "https://github.com/login/oauth/access_token" //nolint:gosec // OAuth endpoint, not a credential
}
func (g *github) DefaultScopes() string { return "repo project" }

func (g *github) ExchangeForm(clientID, secret, code, redirectURI string) url.Values {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	return form
}

// RefreshForm renews a GitHub App user token. The refresh token is
// single-use: the grant that comes back carries its replacement.
func (g *github) RefreshForm(clientID, secret, refresh, _ string) url.Values {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	return form
}

func (g *github) get(ctx context.Context, client *http.Client, token, path string) (*http.Response, error) {
	// The path is the operator's repository or a board member's login under
	// the forge's API base, never anything a visitor supplied verbatim.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.api+path, nil) //nolint:gosec // see above
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aeman")
	return client.Do(req) //nolint:gosec // see above
}

func (g *github) User(ctx context.Context, client *http.Client, token string) (User, error) {
	resp, err := g.get(ctx, client, token, "/user")
	if err != nil {
		return User{}, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		_ = resp.Body.Close()
		return User{}, ErrBadToken
	case resp.StatusCode != http.StatusOK:
		_ = resp.Body.Close()
		return User{}, fmt.Errorf("user endpoint returned %s", resp.Status)
	}
	var u struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := decodeJSON(resp, &u); err != nil {
		return User{}, err
	}
	return User{Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL}, nil
}

// RepoRef is owner/repo: the last two path segments.
func (g *github) RepoRef(repoURL string) (string, error) {
	parts := repoPath(repoURL)
	if len(parts) < 2 {
		return "", fmt.Errorf("repository url %q has no owner/repo", repoURL)
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1], nil
}

// Access asks GET /repos/{owner}/{repo} as the visitor: the permissions
// block says what they may do; a 404 is what GitHub says for a repository
// the visitor cannot see.
func (g *github) Access(ctx context.Context, client *http.Client, token, repoURL string) (read, write bool, err error) {
	slug, err := g.RepoRef(repoURL)
	if err != nil {
		return false, false, err
	}
	resp, err := g.get(ctx, client, token, "/repos/"+slug)
	if err != nil {
		return false, false, err
	}
	switch {
	case throttled(resp):
		_ = resp.Body.Close()
		return false, false, fmt.Errorf("%w: %s", ErrRateLimited, resp.Status)
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusForbidden:
		stale := staleScope(resp)
		_ = resp.Body.Close()
		if stale {
			// A token minted before this server asked for `repo` cannot see
			// a private repository, and GitHub says 404 either way. Read as
			// "no access" it strands the visitor — every write refused until
			// they happen to sign in again — so it is what it is: an
			// authorization that no longer covers what the board needs.
			return false, false, fmt.Errorf("%w: the sign-in predates the scopes this board needs", ErrBadToken)
		}
		return false, false, nil
	case resp.StatusCode == http.StatusUnauthorized:
		_ = resp.Body.Close()
		return false, false, ErrBadToken
	case resp.StatusCode/100 != 2:
		_ = resp.Body.Close()
		return false, false, fmt.Errorf("forge answered %s", resp.Status)
	}
	var body struct {
		Permissions struct {
			Admin    bool `json:"admin"`
			Maintain bool `json:"maintain"`
			Push     bool `json:"push"`
			Triage   bool `json:"triage"`
			Pull     bool `json:"pull"`
		} `json:"permissions"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		return false, false, err
	}
	p := body.Permissions
	return p.Pull || p.Triage || p.Push || p.Maintain || p.Admin, p.Push || p.Maintain || p.Admin, nil
}

// Readers asks the collaborator permission of each login with the server's
// token: any permission but "none" reads; a 404 is not a collaborator.
func (g *github) Readers(ctx context.Context, client *http.Client, serverToken, repoURL string, logins []string) (map[string]Person, error) {
	if serverToken == "" {
		return nil, fmt.Errorf("no server credential to ask the forge with")
	}
	slug, err := g.RepoRef(repoURL)
	if err != nil {
		return nil, err
	}
	out := map[string]Person{}
	for _, login := range logins {
		resp, err := g.get(ctx, client, serverToken, "/repos/"+slug+"/collaborators/"+url.PathEscape(login)+"/permission")
		if err != nil {
			return nil, err
		}
		switch {
		case throttled(resp):
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: %s", ErrRateLimited, resp.Status)
		case resp.StatusCode == http.StatusNotFound:
			_ = resp.Body.Close()
			continue
		case resp.StatusCode/100 != 2:
			_ = resp.Body.Close()
			return nil, fmt.Errorf("forge answered %s", resp.Status)
		}
		var body struct {
			Permission string `json:"permission"`
		}
		if err := decodeJSON(resp, &body); err != nil {
			return nil, err
		}
		if body.Permission != "" && body.Permission != "none" {
			out[login] = Person{Login: login, AvatarURL: githubAvatarURL(login)}
		}
	}
	return out, nil
}

// Lookup builds the person from the login alone: GitHub serves avatars off
// a CDN by login, no call needed, and the boards know no display names.
func (g *github) Lookup(_ context.Context, _ *http.Client, _, login string) (Person, error) {
	if login == "" {
		return Person{}, ErrNotFound
	}
	return Person{Login: login, AvatarURL: githubAvatarURL(login)}, nil
}

// staleScope reports whether a refusal is the token's scopes rather than
// the visitor's access: GitHub names an OAuth token's scopes in every
// answer, and one without `repo` cannot see a private repository at all. A
// forge that names no scopes (a fine-grained token, another server) tells
// us nothing, and the refusal is taken at face value.
func staleScope(resp *http.Response) bool {
	scopes, named := resp.Header["X-Oauth-Scopes"]
	if !named {
		return false
	}
	for _, s := range strings.Split(strings.Join(scopes, ","), ",") {
		if strings.TrimSpace(s) == "repo" {
			return false
		}
	}
	return true
}

// githubAvatarURL is GitHub's avatar image for a login — the CDN, sized
// for the boards.
func githubAvatarURL(login string) string {
	if login == "" {
		return ""
	}
	return "https://avatars.githubusercontent.com/" + url.PathEscape(login) + "?size=48"
}

func (g *github) GitAuth(token string) *githttp.BasicAuth {
	return &githttp.BasicAuth{Username: "x-access-token", Password: token}
}

func jsonDecode(r io.Reader, v any) error { return json.NewDecoder(r).Decode(v) }
