package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Who sees, who writes (G17, G25). A board is an ordered list of domains —
// repositories — and a visitor's board is the union of the ones they can
// read; a write needs write access to the domain it targets, both domains
// of a move, whatever the server's own push credential can do. The forge
// answers both questions from its permission API with the visitor's token,
// one request per domain, cached per visitor for a minute — the freshness
// authFreshFor gave board access before.

// accessTTL is how long a visitor's answer is trusted before it is asked
// again.
const accessTTL = 60 * time.Second

// domainRights is one visitor's answer: what they may read and write, and
// which domain is the primary (what "" names). A nil *domainRights means no
// restriction — a single-identity server (the local gh mode) or a non-git
// backend.
type domainRights struct {
	primary string
	read    map[string]bool
	write   map[string]bool
}

func (r *domainRights) or(d string) string {
	if d == "" {
		return r.primary
	}
	return d
}

// canRead reports whether the visitor may read a domain; "" is the primary.
func (r *domainRights) canRead(d string) bool {
	if r == nil {
		return true
	}
	return r.read[r.or(d)]
}

// canWrite reports whether the visitor may write a domain; "" is the primary.
func (r *domainRights) canWrite(d string) bool {
	if r == nil {
		return true
	}
	return r.write[r.or(d)]
}

// readable is canRead as the func board.Visible takes.
func (r *domainRights) readable(d string) bool { return r.canRead(d) }

type rightsCtxKey struct{}

// withRights stamps a visitor's rights on the request context, where the
// visible backend reads them.
func withRights(ctx context.Context, r *domainRights) context.Context {
	return context.WithValue(ctx, rightsCtxKey{}, r)
}

// rightsFrom is the visitor's rights, or nil when the request carries none
// (a single-identity server).
func rightsFrom(ctx context.Context) *domainRights {
	r, _ := ctx.Value(rightsCtxKey{}).(*domainRights)
	return r
}

// domainAccess decides a visitor's rights from their token and login.
type domainAccess interface {
	rights(ctx context.Context, token, login string) (*domainRights, error)
}

// openAccess is the single-identity answer: every configured domain, read
// and write — the local mode, where the one visitor is the gh user and the
// forge's own check happens on push.
type openAccess struct {
	domains []string
}

func (o openAccess) rights(context.Context, string, string) (*domainRights, error) {
	r := &domainRights{read: map[string]bool{}, write: map[string]bool{}}
	for i, d := range o.domains {
		if i == 0 {
			r.primary = d
		}
		r.read[d] = true
		r.write[d] = true
	}
	return r, nil
}

// forgeAccess asks the forge — GitHub's repository permissions block — with
// the visitor's token, one request per domain, and caches the answer per
// visitor. A stale answer stands in while the forge is unreachable; without
// one the error is the visitor's.
type forgeAccess struct {
	api     string // REST base, https://api.github.com
	client  *http.Client
	domains []RepoSpec
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedRights // by login
}

type cachedRights struct {
	rights *domainRights
	at     time.Time
}

func newForgeAccess(api string, client *http.Client, domains []RepoSpec) *forgeAccess {
	return &forgeAccess{api: api, client: client, domains: domains, ttl: accessTTL, now: time.Now, cache: map[string]cachedRights{}}
}

func (f *forgeAccess) rights(ctx context.Context, token, login string) (*domainRights, error) {
	f.mu.Lock()
	c, ok := f.cache[login]
	f.mu.Unlock()
	if ok && f.now().Sub(c.at) < f.ttl {
		return c.rights, nil
	}
	r := &domainRights{primary: f.domains[0].Name, read: map[string]bool{}, write: map[string]bool{}}
	for _, d := range f.domains {
		read, write, err := f.probe(ctx, token, d.URL)
		if err != nil {
			if ok {
				return c.rights, nil // the forge is unreachable: the last answer stands
			}
			return nil, fmt.Errorf("access to %s: %w", d.Name, err)
		}
		r.read[d.Name] = read
		r.write[d.Name] = write
	}
	f.mu.Lock()
	f.cache[login] = cachedRights{rights: r, at: f.now()}
	f.mu.Unlock()
	return r, nil
}

// probe asks GET /repos/{owner}/{repo} as the visitor: the permissions block
// says what they may do; a 404 is what GitHub says for a repository the
// visitor cannot see.
func (f *forgeAccess) probe(ctx context.Context, token, repoURL string) (read, write bool, err error) {
	slug, err := repoSlug(repoURL)
	if err != nil {
		return false, false, err
	}
	// The URL is the operator's --repo (owner/repo) under the forge's API
	// base, never anything a visitor supplied.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.api+"/repos/"+slug, nil) //nolint:gosec // operator-configured repository, not visitor input
	if err != nil {
		return false, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := f.client.Do(req) //nolint:gosec // see above
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusForbidden:
		return false, false, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return false, false, errBadVisitorToken
	case resp.StatusCode/100 != 2:
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
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, false, err
	}
	p := body.Permissions
	return p.Pull || p.Triage || p.Push || p.Maintain || p.Admin, p.Push || p.Maintain || p.Admin, nil
}

// errBadVisitorToken is the forge refusing the visitor's own token.
var errBadVisitorToken = errors.New("the forge rejected the visitor's token")

// repoSlug is the owner/repo of a repository URL, for the forge's REST API:
// the last two path segments, .git dropped — https://host/owner/repo.git,
// git@host:owner/repo.git and plain owner/repo all work.
func repoSlug(repoURL string) (string, error) {
	s := strings.TrimSuffix(strings.TrimSuffix(repoURL, "/"), ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.Index(s, ":"); i >= 0 && !strings.Contains(s[:i], "/") {
		s = s[i+1:] // scp-like git@host:owner/repo
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 || parts[len(parts)-1] == "" || parts[len(parts)-2] == "" {
		return "", fmt.Errorf("repository url %q has no owner/repo", repoURL)
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1], nil
}

// accessMiddleware resolves the visitor's rights for an /api/v1 request in
// git mode and stamps them on the context; a visitor the forge does not
// know is refused here, before any handler runs.
func (s *Server) accessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.access != nil && strings.HasPrefix(r.URL.Path, "/api/v1") {
			tok, login, err := s.apiTokens(r)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
				return
			}
			rights, err := s.access.rights(r.Context(), tok, login)
			if err != nil {
				if errors.Is(err, errBadVisitorToken) {
					writeJSONError(w, http.StatusUnauthorized, err.Error())
					return
				}
				writeJSONError(w, http.StatusForbidden, "access could not be decided: "+err.Error())
				return
			}
			r = r.WithContext(withRights(r.Context(), rights))
		}
		next.ServeHTTP(w, r)
	})
}
