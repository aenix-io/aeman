package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// A card's description may reference GitHub issues and pull requests; the
// board shows their live title and state (list_links, GET /cards/{uid}/links,
// the title a pasted URL gets). The store used to ask GitHub with the
// visitor's token; with the board in git the store has no per-visitor token,
// so the forge is asked with the SERVER credential — the same one that
// pushes — and the answer is cached for everyone.

// linksTTL is how long a resolved reference is trusted.
const linksTTL = 5 * time.Minute

// errUnresolvedLink is a reference the forge did not answer for: no
// credential, unknown or private repository, network trouble. The service
// swallows it and shows the reference as written.
var errUnresolvedLink = errors.New("link could not be resolved")

// forgeLinks resolves GitHub issue/PR references through the REST API.
type forgeLinks struct {
	api    string
	client *http.Client
	token  string
	// tokenFn, when set, mints the credential per lookup — the GitHub App
	// mode, where the server holds no static token at all.
	tokenFn func(context.Context) string
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedLink // by URL
}

type cachedLink struct {
	link board.Link
	at   time.Time
}

func newForgeLinks(api string, client *http.Client, token string) *forgeLinks {
	return &forgeLinks{api: api, client: client, token: token, now: time.Now, cache: map[string]cachedLink{}}
}

// credential is the token a lookup goes out with: the static one, or the
// minted one when the server credential is a GitHub App installation.
func (f *forgeLinks) credential(ctx context.Context) string {
	if f.tokenFn != nil {
		return f.tokenFn(ctx)
	}
	return f.token
}

// ResolveIssueRef fills the reference's title and state (open, closed, or
// merged for a pull request); a link that is not a GitHub reference passes
// through unchanged. It is boardservice.LinkResolver.
func (f *forgeLinks) ResolveIssueRef(ctx context.Context, link board.Link) (board.Link, error) {
	if !link.IsGitHubRef() {
		return link, nil
	}
	if f.credential(ctx) == "" {
		return link, fmt.Errorf("%w: no server credential", errUnresolvedLink)
	}
	f.mu.Lock()
	c, ok := f.cache[link.URL]
	f.mu.Unlock()
	if ok && f.now().Sub(c.at) < linksTTL {
		return c.link, nil
	}
	resolved, err := f.fetch(ctx, link)
	if err != nil {
		return link, err
	}
	f.mu.Lock()
	f.cache[link.URL] = cachedLink{link: resolved, at: f.now()}
	f.mu.Unlock()
	return resolved, nil
}

// fetch asks GET /repos/{owner}/{repo}/issues/{n}: GitHub answers it for
// pull requests too, with a pull_request block whose merged_at says merged.
func (f *forgeLinks) fetch(ctx context.Context, link board.Link) (board.Link, error) {
	// The path is the reference the user wrote — owner, repo, number — under
	// the forge's API base; never anything else the visitor supplied.
	target := f.api + "/repos/" + link.Owner + "/" + link.Repo + "/issues/" + strconv.Itoa(link.Number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) //nolint:gosec // the parsed reference, under the configured forge base
	if err != nil {
		return link, err
	}
	req.Header.Set("Authorization", "Bearer "+f.credential(ctx))
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := f.client.Do(req) //nolint:gosec // see above
	if err != nil {
		return link, fmt.Errorf("%w: %w", errUnresolvedLink, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return link, fmt.Errorf("%w: forge answered %s", errUnresolvedLink, resp.Status)
	}
	var body struct {
		Title       string `json:"title"`
		State       string `json:"state"`
		PullRequest *struct {
			MergedAt *string `json:"merged_at"`
		} `json:"pull_request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return link, fmt.Errorf("%w: %w", errUnresolvedLink, err)
	}
	link.Title = body.Title
	link.State = body.State
	if body.PullRequest != nil && body.PullRequest.MergedAt != nil && *body.PullRequest.MergedAt != "" {
		link.State = "merged"
	}
	return link, nil
}
