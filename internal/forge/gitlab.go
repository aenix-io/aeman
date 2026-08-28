package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// gitlab is one GitLab instance (gitlab.com or self-hosted): OAuth and REST
// under one base URL, projects named by their full path (subgroups and all,
// URL-encoded as one segment), a numeric access level per visitor, a member
// list that carries names and avatars, and avatars that are uploads — found
// through the user directory, never built from a login.
type gitlab struct {
	base string // https://gitlab.com
}

// GitLab access levels (the ones the board cares about).
const (
	gitlabReporter  = 20 // reads the repository
	gitlabDeveloper = 30 // pushes to it
)

// NewGitLab is the GitLab instance at base.
func NewGitLab(base string) Forge { return &gitlab{base: strings.TrimRight(base, "/")} }

func (g *gitlab) Kind() Kind           { return GitLab }
func (g *gitlab) Label() string        { return "GitLab" }
func (g *gitlab) AuthorizeURL() string { return g.base + "/oauth/authorize" }
func (g *gitlab) TokenURL() string     { return g.base + "/oauth/token" }
func (g *gitlab) api() string          { return g.base + "/api/v4" }

// DefaultScopes: read_user for who-am-i, read_api for projects and members,
// write_repository for git over HTTPS with the token (a personal board is
// pushed with its owner's token).
func (g *gitlab) DefaultScopes() string { return "read_user read_api write_repository" }

func (g *gitlab) ExchangeForm(clientID, secret, code, redirectURI string) url.Values {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURI)
	return form
}

// RefreshForm renews an access token (GitLab's live two hours). GitLab
// requires the redirect URI on the refresh grant as well.
func (g *gitlab) RefreshForm(clientID, secret, refresh, redirectURI string) url.Values {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	form.Set("redirect_uri", redirectURI)
	return form
}

func (g *gitlab) get(ctx context.Context, client *http.Client, token, path string) (*http.Response, error) {
	// The path is the operator's repository (URL-encoded as one segment) or
	// a board member's login, under the instance's API base.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.api()+path, nil) //nolint:gosec // see above
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aeman")
	return client.Do(req) //nolint:gosec // see above
}

// gitlabUser is the user shape GitLab returns everywhere.
type gitlabUser struct {
	Username    string `json:"username"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url"`
	AccessLevel int    `json:"access_level"`
}

func (g *gitlab) User(ctx context.Context, client *http.Client, token string) (User, error) {
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
	var u gitlabUser
	if err := decodeJSON(resp, &u); err != nil {
		return User{}, err
	}
	return User{Login: u.Username, Name: u.Name, AvatarURL: u.AvatarURL}, nil
}

// RepoRef is the project's full path — group/subgroup/project — encoded as
// one URL segment, the way the projects API takes it.
func (g *gitlab) RepoRef(repoURL string) (string, error) {
	parts := repoPath(repoURL)
	if len(parts) < 2 {
		return "", fmt.Errorf("repository url %q has no group/project path", repoURL)
	}
	return url.PathEscape(strings.Join(parts, "/")), nil
}

// Access asks GET /projects/{path} as the visitor. The answer's access
// level — the project's own or inherited from the group, whichever is
// higher — says what they may do: Reporter reads, Developer pushes; a Guest
// sees the project but not its code. A public project reads for anyone. A
// 404 is what GitLab says for a project the visitor cannot see.
func (g *gitlab) Access(ctx context.Context, client *http.Client, token, repoURL string) (read, write bool, err error) {
	ref, err := g.RepoRef(repoURL)
	if err != nil {
		return false, false, err
	}
	resp, err := g.get(ctx, client, token, "/projects/"+ref)
	if err != nil {
		return false, false, err
	}
	switch {
	case throttled(resp):
		_ = resp.Body.Close()
		return false, false, fmt.Errorf("%w: %s", ErrRateLimited, resp.Status)
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusForbidden:
		_ = resp.Body.Close()
		return false, false, nil
	case resp.StatusCode == http.StatusUnauthorized:
		_ = resp.Body.Close()
		return false, false, ErrBadToken
	case resp.StatusCode/100 != 2:
		_ = resp.Body.Close()
		return false, false, fmt.Errorf("forge answered %s", resp.Status)
	}
	var body struct {
		Visibility  string `json:"visibility"`
		Permissions struct {
			Project *struct {
				AccessLevel int `json:"access_level"`
			} `json:"project_access"`
			Group *struct {
				AccessLevel int `json:"access_level"`
			} `json:"group_access"`
		} `json:"permissions"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		return false, false, err
	}
	level := 0
	if p := body.Permissions.Project; p != nil && p.AccessLevel > level {
		level = p.AccessLevel
	}
	if gr := body.Permissions.Group; gr != nil && gr.AccessLevel > level {
		level = gr.AccessLevel
	}
	read = level >= gitlabReporter || body.Visibility == "public" || body.Visibility == "internal"
	return read, level >= gitlabDeveloper, nil
}

// Readers is the project's member list — inherited members included, every
// page — narrowed to the logins asked about, Reporter and up. The list says
// who each member is, so the names and avatars come back with the answer.
func (g *gitlab) Readers(ctx context.Context, client *http.Client, serverToken, repoURL string, logins []string) (map[string]Person, error) {
	if serverToken == "" {
		return nil, fmt.Errorf("no server credential to ask the forge with")
	}
	ref, err := g.RepoRef(repoURL)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, l := range logins {
		wanted[l] = true
	}
	out := map[string]Person{}
	for page := 1; page <= 20; page++ {
		resp, err := g.get(ctx, client, serverToken, "/projects/"+ref+"/members/all?per_page=100&page="+strconv.Itoa(page))
		if err != nil {
			return nil, err
		}
		if throttled(resp) {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: %s", ErrRateLimited, resp.Status)
		}
		if resp.StatusCode/100 != 2 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("forge answered %s", resp.Status)
		}
		next := resp.Header.Get("X-Next-Page")
		var members []gitlabUser
		if err := decodeJSON(resp, &members); err != nil {
			return nil, err
		}
		for _, m := range members {
			if wanted[m.Username] && m.AccessLevel >= gitlabReporter {
				out[m.Username] = Person{Login: m.Username, Name: m.Name, AvatarURL: m.AvatarURL}
			}
		}
		if next == "" || len(members) == 0 {
			break
		}
	}
	return out, nil
}

// Lookup asks the user directory for a login: an avatar on GitLab is an
// upload, there is no URL to build from the login.
func (g *gitlab) Lookup(ctx context.Context, client *http.Client, token, login string) (Person, error) {
	if login == "" {
		return Person{}, ErrNotFound
	}
	resp, err := g.get(ctx, client, token, "/users?username="+url.QueryEscape(login))
	if err != nil {
		return Person{}, err
	}
	if resp.StatusCode/100 != 2 {
		_ = resp.Body.Close()
		return Person{}, fmt.Errorf("forge answered %s", resp.Status)
	}
	var users []gitlabUser
	if err := decodeJSON(resp, &users); err != nil {
		return Person{}, err
	}
	for _, u := range users {
		if strings.EqualFold(u.Username, login) {
			return Person{Login: u.Username, Name: u.Name, AvatarURL: u.AvatarURL}, nil
		}
	}
	return Person{}, ErrNotFound
}

// GitAuth: GitLab takes an OAuth token over HTTPS only as the password of
// the user "oauth2".
func (g *gitlab) GitAuth(token string) *githttp.BasicAuth {
	return &githttp.BasicAuth{Username: "oauth2", Password: token}
}
