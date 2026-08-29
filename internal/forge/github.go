package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// DefaultScopes: `repo` is what an OAuth App needs to see a private board
// repository; a GitHub App ignores the parameter (its permissions come from
// the installation).
func (g *github) DefaultScopes() string { return "repo" }

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

// errNoListing is a credential that may read a repository but not list who
// else can. It is not an outage — the per-login question still answers.
var errNoListing = errors.New("the credential may not list collaborators")

// Readers is one question for everyone: GitHub lists a repository's
// collaborators with their permissions, a hundred to a page. Asking each
// login separately — what this did — cost a round trip per person, in a
// row, and a board of ten people across two domains spent seconds of a page
// load on it. A credential that may not list them falls back to that.
func (g *github) Readers(ctx context.Context, client *http.Client, serverToken, repoURL string, logins []string) (map[string]Person, error) {
	if serverToken == "" {
		return nil, fmt.Errorf("no server credential to ask the forge with")
	}
	slug, err := g.RepoRef(repoURL)
	if err != nil {
		return nil, err
	}
	switch out, seen, err := g.readersFromListing(ctx, client, serverToken, slug, logins); {
	case err == nil:
		// The listing does not see everyone every credential can vouch for:
		// an installation token without the app's Members permission omits
		// the people whose access comes through the organisation, while the
		// per-login question answers for them. Whoever the listing did not
		// name AT ALL is asked about one by one — someone it named without
		// read access was answered, and a full listing (a PAT) misses
		// nobody, so nothing extra is asked.
		missing := make([]string, 0)
		for _, login := range logins {
			if !seen[login] {
				missing = append(missing, login)
			}
		}
		if len(missing) == 0 {
			return out, nil
		}
		rest, err := g.readersOneByOne(ctx, client, serverToken, slug, missing)
		if err != nil {
			return nil, err
		}
		for login, p := range rest {
			out[login] = p
		}
		return out, nil
	case !errors.Is(err, errNoListing):
		return nil, err
	}
	return g.readersOneByOne(ctx, client, serverToken, slug, logins)
}

// readersFromListing pages through the collaborators, keeping the asked
// logins with any permission but none; seen is every asked login the
// listing named at all, whatever its permissions said.
func (g *github) readersFromListing(ctx context.Context, client *http.Client, serverToken, slug string, logins []string) (map[string]Person, map[string]bool, error) {
	wanted := map[string]bool{}
	for _, l := range logins {
		wanted[l] = true
	}
	out := map[string]Person{}
	seen := map[string]bool{}
	for page := 1; page <= 20; page++ {
		resp, err := g.get(ctx, client, serverToken, "/repos/"+slug+"/collaborators?per_page=100&page="+strconv.Itoa(page))
		if err != nil {
			return nil, nil, err
		}
		switch {
		case throttled(resp):
			_ = resp.Body.Close()
			return nil, nil, fmt.Errorf("%w: %s", ErrRateLimited, resp.Status)
		case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusNotFound:
			_ = resp.Body.Close()
			return nil, nil, errNoListing
		case resp.StatusCode/100 != 2:
			_ = resp.Body.Close()
			return nil, nil, fmt.Errorf("forge answered %s", resp.Status)
		}
		more := hasNextPage(resp.Header.Get("Link"))
		var collaborators []struct {
			Login       string `json:"login"`
			Permissions struct {
				Admin    bool `json:"admin"`
				Maintain bool `json:"maintain"`
				Push     bool `json:"push"`
				Triage   bool `json:"triage"`
				Pull     bool `json:"pull"`
			} `json:"permissions"`
		}
		if err := decodeJSON(resp, &collaborators); err != nil {
			return nil, nil, err
		}
		for _, c := range collaborators {
			if !wanted[c.Login] {
				continue
			}
			seen[c.Login] = true
			p := c.Permissions
			if p.Pull || p.Triage || p.Push || p.Maintain || p.Admin {
				out[c.Login] = Person{Login: c.Login, AvatarURL: githubAvatarURL(c.Login)}
			}
		}
		if !more || len(collaborators) == 0 {
			break
		}
	}
	return out, seen, nil
}

// hasNextPage reads GitHub's Link header: another page follows when it
// names one as `rel="next"`.
func hasNextPage(link string) bool {
	for _, part := range strings.Split(link, ",") {
		if strings.Contains(part, `rel="next"`) {
			return true
		}
	}
	return false
}

// readersOneByOne asks the collaborator permission of each login: any
// permission but "none" reads; a 404 is not a collaborator.
func (g *github) readersOneByOne(ctx context.Context, client *http.Client, serverToken, slug string, logins []string) (map[string]Person, error) {
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
