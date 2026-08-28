package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/pkg/board"
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

// domainAccess decides a visitor's rights from their token and login, and
// which of the board's members can read a domain (G16) — asked with the
// server's credential, since it is about other people.
type domainAccess interface {
	rights(ctx context.Context, token, login string) (*domainRights, error)
	readers(ctx context.Context, domain string, logins []string) ([]string, error)
	// canPush says whether the visitor's token may push to a repository
	// that is not one of the board's — their personal one, on linking. An
	// error is a URL the forge cannot name a repository in.
	canPush(ctx context.Context, token, repoURL string) (bool, error)
}

// openAccess is the single-identity answer: every configured domain, read
// and write, by everyone — the local mode, where the one visitor is the gh
// user and the forge's own check happens on push.
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

// canPush: on a single-user server the forge's own check happens on push.
func (o openAccess) canPush(context.Context, string, string) (bool, error) { return true, nil }

func (o openAccess) readers(_ context.Context, _ string, logins []string) ([]string, error) {
	return append([]string(nil), logins...), nil
}

// membersTTL is how long a login's read access to a domain, asked with the
// server credential, is trusted.
const membersTTL = 5 * time.Minute

// forgeAccess asks the forge — a repository's permissions, in the forge's
// own terms — with the visitor's token, one request per domain, and caches
// the answer per visitor. A stale answer stands in while the forge is
// unreachable; without one the error is the visitor's. Who else can read a
// domain is asked with the server's token and cached longer; what the forge
// says about those people on the way (names, avatars) is handed to the
// board's directory.
type forgeAccess struct {
	forge       forge.Forge
	client      *http.Client
	domains     []RepoSpec
	serverToken string
	people      *people
	ttl         time.Duration
	now         func() time.Time

	mu      sync.Mutex
	cache   map[string]cachedRights // by login
	members map[string]cachedBool   // by domain + login
	// refreshing single-flights the background check per visitor: the
	// requests of one page load share the one it triggered.
	refreshing map[string]bool
	// logger receives the background check's failures; nil discards them.
	logger *slog.Logger
}

// log reports a background failure — the request that triggered it is long
// answered, so there is nobody to return the error to.
func (f *forgeAccess) log(msg, login string, err error) {
	if f.logger != nil {
		f.logger.Warn(msg, "login", login, "err", err)
	}
}

type cachedRights struct {
	rights *domainRights
	at     time.Time
}

type cachedBool struct {
	ok bool
	at time.Time
}

func newForgeAccess(f forge.Forge, client *http.Client, domains []RepoSpec, serverToken string, people *people) *forgeAccess {
	return &forgeAccess{forge: f, client: client, domains: domains, serverToken: serverToken, people: people,
		ttl: accessTTL, now: time.Now, cache: map[string]cachedRights{}, members: map[string]cachedBool{}}
}

// readers is the subset of logins that can read the domain, by the forge's
// answer for each, asked with the server's token — one question per domain
// for the logins whose answer is stale. A login the forge cannot answer for
// (an outage, no server token) is left out — the picker offers fewer
// people, never the wrong ones — unless an earlier answer stands.
func (f *forgeAccess) readers(ctx context.Context, domain string, logins []string) ([]string, error) {
	var url string
	for _, d := range f.domains {
		if d.Name == domain {
			url = d.URL
		}
	}
	if url == "" {
		return nil, fmt.Errorf("unknown domain %q", domain)
	}
	if _, err := f.forge.RepoRef(url); err != nil {
		return nil, err
	}
	stale := make([]string, 0, len(logins))
	f.mu.Lock()
	for _, login := range logins {
		if c, ok := f.members[domain+"\x00"+login]; !ok || f.now().Sub(c.at) >= membersTTL {
			stale = append(stale, login)
		}
	}
	f.mu.Unlock()
	if len(stale) > 0 {
		found, err := f.forge.Readers(ctx, f.client, f.tokenFor(domain), url, stale)
		if err == nil {
			if f.people != nil {
				f.people.learn(found)
			}
			f.mu.Lock()
			for _, login := range stale {
				_, reads := found[login]
				f.members[domain+"\x00"+login] = cachedBool{ok: reads, at: f.now()}
			}
			f.mu.Unlock()
		}
		// On an error the stale answers stand (an outage), and a login never
		// answered for is left out below.
	}
	out := make([]string, 0, len(logins))
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, login := range logins {
		if c, ok := f.members[domain+"\x00"+login]; ok && c.ok {
			out = append(out, login)
		}
	}
	return out, nil
}

// tokenFor is the server credential a domain is asked about — its own when
// it names one (a board spanning two organisations holds a token per
// organisation), else the shared one.
func (f *forgeAccess) tokenFor(domain string) string {
	for _, d := range f.domains {
		if d.Name == domain {
			return d.token(f.serverToken)
		}
	}
	return f.serverToken
}

// canPush asks the forge, as the visitor, whether they may write the
// repository — for linking a personal board. A URL the forge cannot name a
// repository in is errNotARepository; anything else is the forge's answer.
func (f *forgeAccess) canPush(ctx context.Context, token, repoURL string) (bool, error) {
	if _, err := f.forge.RepoRef(repoURL); err != nil {
		return false, fmt.Errorf("%w: %w", errNotARepository, err)
	}
	_, write, err := f.probe(ctx, token, repoURL)
	return write, err
}

// errNotARepository is a URL the forge cannot name a repository in.
var errNotARepository = errors.New("not a repository URL")

// collaboratorReads asks GET /repos/{slug}/collaborators/{login}/permission
// with the server token: any permission but "none" reads.
func (f *forgeAccess) rights(ctx context.Context, token, login string) (*domainRights, error) {
	f.mu.Lock()
	c, ok := f.cache[login]
	f.mu.Unlock()
	if ok && f.now().Sub(c.at) < f.ttl {
		return c.rights, nil
	}
	// A visitor we know, whose answer has merely gone stale, waits for
	// nothing: what we know stands while the forge is asked behind them.
	// Coming back after a break used to cost a round trip per repository on
	// the first request — and the four a page load fires beside it each
	// started their own, which is also how a quiet morning turned into a
	// burst of forge calls.
	if ok {
		f.revalidate(token, login)
		return c.rights, nil
	}
	r, err := f.probeAll(ctx, token, login)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// revalidate refreshes a visitor's rights in the background, one at a time
// per visitor: a page load's requests share the check they triggered.
func (f *forgeAccess) revalidate(token, login string) {
	f.mu.Lock()
	if f.refreshing == nil {
		f.refreshing = map[string]bool{}
	}
	if f.refreshing[login] {
		f.mu.Unlock()
		return
	}
	f.refreshing[login] = true
	f.mu.Unlock()
	go func() {
		// Detached from the request that noticed: it is already answered,
		// and the check must land whether or not that request is still there.
		ctx, cancel := context.WithTimeout(context.Background(), rightsRevalidateTimeout)
		defer cancel()
		if _, err := f.probeAll(ctx, token, login); err != nil {
			// The stale answer stands until the forge can be reached; the
			// next request tries again.
			f.log("rights revalidation failed", login, err)
		}
		f.mu.Lock()
		delete(f.refreshing, login)
		f.mu.Unlock()
	}()
}

// rightsRevalidateTimeout bounds a background rights check.
const rightsRevalidateTimeout = 30 * time.Second

// probeAll asks the forge about every domain and records the answer.
func (f *forgeAccess) probeAll(ctx context.Context, token, login string) (*domainRights, error) {
	f.mu.Lock()
	c, ok := f.cache[login]
	f.mu.Unlock()
	r := &domainRights{primary: f.domains[0].Name, read: map[string]bool{}, write: map[string]bool{}}
	for _, d := range f.domains {
		read, write, err := f.probe(ctx, token, d.URL)
		if err != nil {
			if errors.Is(err, errBadVisitorToken) {
				// The authorization itself is gone (revoked, expired, or
				// minted before the scopes this board needs). A stale answer
				// must not paper over that: forget it, so the next request
				// asks and the visitor is sent to sign in again.
				f.mu.Lock()
				delete(f.cache, login)
				f.mu.Unlock()
				return nil, err
			}
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

// probe asks the forge, as the visitor, what they may do with the repository
// — the forge's own permission model, translated to read and write. A
// repository the visitor cannot see is neither; a rejected token is
// errBadVisitorToken.
func (f *forgeAccess) probe(ctx context.Context, token, repoURL string) (read, write bool, err error) {
	return f.forge.Access(ctx, f.client, token, repoURL)
}

// errBadVisitorToken is the forge refusing the visitor's own token.
var errBadVisitorToken = forge.ErrBadToken

// accessMiddleware resolves the visitor's rights for an /api/v1 request in
// git mode and stamps them on the context; a visitor the forge does not
// know is refused here, before any handler runs.
func (s *Server) accessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1") {
			next.ServeHTTP(w, r)
			return
		}
		tok, login, err := s.apiTokens(r)
		if s.access == nil {
			// Open access (the local server): no rights to decide, but the
			// local person's personal board is theirs to have here too.
			if err == nil && s.gitBE != nil {
				if aerr := s.gitBE.attachPersonal(r.Context(), login, tok); aerr != nil {
					s.log.Warn("personal board", "login", login, "err", aerr)
				}
			}
			next.ServeHTTP(w, r)
			return
		}
		{
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "not authenticated: "+err.Error())
				return
			}
			rights, err := s.access.rights(r.Context(), tok, login)
			if errors.Is(err, errBadVisitorToken) && s.auth != nil {
				// The forge refused the session's token. The refresh token
				// may still buy a fresh one — a GitLab token lives two hours,
				// and a token issued a moment ago has been seen refused by a
				// lagging replica — so renew once and ask again before the
				// session is given up.
				if sess, ok := s.auth.renewNow(r.Context(), s.auth.sessionID(r)); ok {
					tok = sess.token
					rights, err = s.access.rights(r.Context(), tok, login)
					s.log.Info("session token renewed after the forge refused it", "login", login)
				}
			}
			if err != nil {
				if errors.Is(err, errBadVisitorToken) {
					// The session was built on a token the forge now refuses:
					// drop it, so /api/config reports the visitor signed out
					// and the SPA offers the sign-in instead of failing every
					// request behind a session that still looks valid.
					if s.auth != nil {
						if dropped := s.auth.dropSession(s.auth.sessionID(r)); dropped != "" {
							s.log.Warn("the forge rejected a session token; session dropped", "login", dropped, "path", r.URL.Path)
						}
						s.auth.setCookie(w, sessionCookie, "", -1)
					}
					writeJSONError(w, http.StatusUnauthorized, "your authorization is no longer valid: "+err.Error())
					return
				}
				writeJSONError(w, http.StatusForbidden, "access could not be decided: "+err.Error())
				return
			}
			// The visitor's personal board: attached with their credential
			// the first time they arrive, theirs alone to read and write —
			// whatever the forge says about who else can see the repository.
			if s.gitBE != nil {
				if aerr := s.gitBE.attachPersonal(r.Context(), login, tok); aerr != nil {
					s.log.Warn("personal board", "login", login, "err", aerr)
				}
				if s.gitBE.hasPersonal(login) {
					rights = rights.withPersonal(login)
				}
			}
			r = r.WithContext(withRights(r.Context(), rights))
		}
		next.ServeHTTP(w, r)
	})
}

// withPersonal is the rights plus the login's own personal domain, readable
// and writable — a copy, since the forge's answer is cached and shared.
func (r *domainRights) withPersonal(login string) *domainRights {
	out := &domainRights{primary: r.primary, read: make(map[string]bool, len(r.read)+1), write: make(map[string]bool, len(r.write)+1)}
	for k, v := range r.read {
		out.read[k] = v
	}
	for k, v := range r.write {
		out.write[k] = v
	}
	d := board.PersonalDomain(login)
	out.read[d], out.write[d] = true, true
	return out
}
