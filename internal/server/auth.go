package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token" //nolint:gosec // OAuth endpoint, not a credential
	sessionCookie      = "aeman_session"
	stateCookie        = "aeman_oauth_state"
	consentCookie      = "aeman_mcp_consent"
	sessionTTL         = 14 * 24 * time.Hour
	consentTTL         = 10 * time.Minute
	// maxClients caps the dynamic MCP client registry: registration is public
	// and unauthenticated, so the cap plus per-IP-agnostic pruning stops an
	// attacker growing it (and the persisted file) without bound.
	maxClients = 512
)

// OAuthConfig enables multi-user mode: each visitor signs in with GitHub and
// the proxy forwards requests with that user's own token.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	// BaseURL is the public origin (e.g. https://aeman.example.com) used to
	// build the OAuth redirect URI.
	BaseURL string
	// Scopes is a space-separated OAuth scope list (defaults to "repo project").
	Scopes string
	// SessionFile, when set, persists sessions to this path so they survive
	// restarts (empty = in-memory only, lost on restart).
	SessionFile string
}

type oauthSession struct {
	token   string
	login   string
	created time.Time
}

// persistedState is the on-disk envelope: user sessions plus the dynamically
// registered MCP OAuth clients (RFC 7591), which must survive restarts — MCP
// clients cache their client_id and a wiped registry strands them on
// "unknown client_id" until they re-register.
type persistedState struct {
	Sessions map[string]persistedSession `json:"sessions"`
	Clients  map[string]persistedClient  `json:"clients,omitempty"`
}

// persistedClient is the on-disk form of an oauthClient.
type persistedClient struct {
	RedirectURIs []string `json:"redirectUris"`
}

// persistedSession is the on-disk form of an oauthSession.
type persistedSession struct {
	Token   string    `json:"token"`
	Login   string    `json:"login"`
	Created time.Time `json:"created"`
}

// authManager runs the GitHub OAuth web flow and keeps per-user sessions. They
// live in memory and, when SessionFile is set, are mirrored to disk so a
// restart doesn't force everyone to sign in again.
//
// It also plays the OAuth 2.0 authorization-server role for the MCP endpoint
// (RFC 7591 dynamic client registration, the PKCE authorization-code grant and
// token issuance). MCP access tokens ARE session ids, so an issued token is a
// live session and the same TTL/persistence applies.
type authManager struct {
	cfg    OAuthConfig
	secure bool
	log    *slog.Logger
	client *http.Client
	path   string

	// GitHub endpoints. They default to the real GitHub URLs and are overridden
	// in tests to point at a stub server.
	authorizeURL string
	tokenURL     string
	apiBase      string

	mu       sync.Mutex
	sessions map[string]oauthSession
	// clients holds dynamically-registered MCP OAuth clients (RFC 7591).
	clients map[string]oauthClient
	// pendingAuth tracks in-flight MCP authorizations, keyed by the GitHub
	// state we generate, so the callback can resume the right client flow.
	pendingAuth map[string]pendingAuth
	// authCodes holds issued single-use MCP authorization codes (5-min TTL).
	authCodes map[string]authCode
	// pendingConsent holds MCP authorizations that have passed GitHub login and
	// now await the user's explicit approval of the client + redirect target,
	// keyed by an unguessable id also bound to the browser via a cookie.
	pendingConsent map[string]pendingConsent
	// registerLimiter rate-limits the public client-registration endpoint.
	registerLimiter *rateLimiter
}

// oauthClient is a dynamically-registered MCP OAuth client.
type oauthClient struct {
	redirectURIs []string
}

// pendingAuth is an in-flight MCP authorization, keyed by the GitHub state.
type pendingAuth struct {
	clientID      string
	redirectURI   string
	codeChallenge string
	clientState   string
	created       time.Time
}

// pendingAuthTTL bounds how long an abandoned authorize flow lingers before it
// is swept, so unfinished flows cannot accumulate in memory.
const pendingAuthTTL = 10 * time.Minute

// authCode is an issued MCP authorization code (single-use, short-lived).
type authCode struct {
	sid           string
	codeChallenge string
	redirectURI   string
	clientID      string
	expiry        time.Time
}

// pendingConsent is an MCP authorization awaiting the user's approval: GitHub
// login already resolved the acting user's token, but no authorization code is
// issued until the user confirms the client and (remote) redirect target, so a
// phished authorize link cannot silently deliver a code to an attacker's URI.
type pendingConsent struct {
	ghToken       string
	login         string
	clientID      string
	redirectURI   string
	codeChallenge string
	clientState   string
	browserToken  string
	expiry        time.Time
}

// rateLimiter is a fixed-window per-key counter. It fronts the unauthenticated
// registration endpoint so a flood cannot spin the O(n) state-file rewrite; the
// window map is reset each period, so it never grows across windows.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	period time.Duration
	window time.Time
	hits   map[string]int
}

func newRateLimiter(limit int, period time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, period: period, hits: map[string]int{}}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if now.Sub(rl.window) >= rl.period {
		rl.hits = map[string]int{}
		rl.window = now
	}
	rl.hits[key]++
	return rl.hits[key] <= rl.limit
}

// clientIP is the best-effort remote address used to key rate limiting: the
// host part of RemoteAddr (behind a proxy every request shares the proxy's
// address, which simply makes the registration limit effectively global).
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func newAuthManager(cfg OAuthConfig, log *slog.Logger) *authManager {
	if cfg.Scopes == "" {
		cfg.Scopes = "repo project"
	}
	a := &authManager{
		cfg:            cfg,
		secure:         strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://"),
		log:            log,
		client:         &http.Client{Timeout: 15 * time.Second},
		path:           cfg.SessionFile,
		authorizeURL:   githubAuthorizeURL,
		tokenURL:       githubTokenURL,
		apiBase:        githubAPIBase,
		sessions:       map[string]oauthSession{},
		clients:        map[string]oauthClient{},
		pendingAuth:    map[string]pendingAuth{},
		authCodes:      map[string]authCode{},
		pendingConsent: map[string]pendingConsent{},
		// 30 registrations per minute per client address: legitimate MCP clients
		// register once, so this only ever bites a flood.
		registerLimiter: newRateLimiter(30, time.Minute),
	}
	a.load()
	return a
}

// load restores persisted sessions (dropping expired ones). No-op without a
// SessionFile or when the file is absent.
func (a *authManager) load() {
	if a.path == "" {
		return
	}
	data, err := os.ReadFile(a.path)
	if err != nil {
		if !os.IsNotExist(err) {
			a.log.Error("session load failed", "err", err)
		}
		return
	}
	var in persistedState
	if err := json.Unmarshal(data, &in); err != nil || in.Sessions == nil {
		// Legacy format: a bare map of sessions with no envelope.
		var legacy map[string]persistedSession
		if err := json.Unmarshal(data, &legacy); err != nil {
			a.log.Error("session load: parse failed", "err", err)
			return
		}
		in = persistedState{Sessions: legacy}
	}
	now := time.Now()
	for sid, s := range in.Sessions {
		if now.Sub(s.Created) > sessionTTL {
			continue
		}
		a.sessions[sid] = oauthSession{token: s.Token, login: s.Login, created: s.Created}
	}
	for id, c := range in.Clients {
		a.clients[id] = oauthClient{redirectURIs: append([]string(nil), c.RedirectURIs...)}
	}
	a.log.Info("sessions restored", "count", len(a.sessions), "clients", len(a.clients))
}

// saveLocked writes the current sessions to disk. Callers must hold a.mu.
func (a *authManager) saveLocked() {
	if a.path == "" {
		return
	}
	out := persistedState{
		Sessions: make(map[string]persistedSession, len(a.sessions)),
		Clients:  make(map[string]persistedClient, len(a.clients)),
	}
	for sid, s := range a.sessions {
		out.Sessions[sid] = persistedSession{Token: s.token, Login: s.login, Created: s.created}
	}
	for id, c := range a.clients {
		out.Clients[id] = persistedClient{RedirectURIs: append([]string(nil), c.redirectURIs...)}
	}
	data, err := json.Marshal(out)
	if err != nil {
		a.log.Error("session save: marshal failed", "err", err)
		return
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		a.log.Error("session save: write failed", "err", err)
		return
	}
	if err := os.Rename(tmp, a.path); err != nil {
		a.log.Error("session save: rename failed", "err", err)
	}
}

func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *authManager) redirectURI() string {
	return strings.TrimRight(a.cfg.BaseURL, "/") + "/auth/callback"
}

// session resolves the request's session cookie to a live session.
func (a *authManager) session(r *http.Request) (oauthSession, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return oauthSession{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if !ok {
		return oauthSession{}, false
	}
	if time.Since(s.created) > sessionTTL {
		delete(a.sessions, c.Value)
		a.saveLocked()
		return oauthSession{}, false
	}
	return s, true
}

func (a *authManager) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	// Secure is driven by the public scheme: true behind the HTTPS proxy
	// (Cloudflare), false only for plain-HTTP local testing.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure set from deployment scheme
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func (a *authManager) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := randToken()
	a.setCookie(w, stateCookie, state, 600)
	q := url.Values{}
	q.Set("client_id", a.cfg.ClientID)
	q.Set("redirect_uri", a.redirectURI())
	q.Set("scope", a.cfg.Scopes)
	q.Set("state", state)
	http.Redirect(w, r, a.authorizeURL+"?"+q.Encode(), http.StatusFound)
}

func (a *authManager) handleCallback(w http.ResponseWriter, r *http.Request) {
	// An MCP authorization keyed by the GitHub state takes the OAuth-server
	// branch; everything else is the cookie-session web flow below, unchanged.
	state := r.URL.Query().Get("state")
	a.mu.Lock()
	p, isMCP := a.pendingAuth[state]
	if isMCP {
		delete(a.pendingAuth, state)
	}
	a.mu.Unlock()
	if isMCP {
		a.handleMCPCallback(w, r, p)
		return
	}

	sc, err := r.Cookie(stateCookie)
	if err != nil || sc.Value == "" || sc.Value != r.URL.Query().Get("state") {
		writeJSONError(w, http.StatusBadRequest, "invalid OAuth state")
		return
	}
	a.setCookie(w, stateCookie, "", -1)

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSONError(w, http.StatusBadRequest, "missing authorization code")
		return
	}

	token, err := a.exchange(r.Context(), code)
	if err != nil {
		a.log.Error("oauth token exchange failed", "err", err)
		writeJSONError(w, http.StatusBadGateway, "token exchange failed")
		return
	}
	login, err := a.fetchLogin(r.Context(), token)
	if err != nil {
		a.log.Error("oauth user lookup failed", "err", err)
		writeJSONError(w, http.StatusBadGateway, "could not read GitHub user")
		return
	}

	sid := randToken()
	a.mu.Lock()
	a.sessions[sid] = oauthSession{token: token, login: login, created: time.Now()}
	a.saveLocked()
	a.mu.Unlock()
	a.setCookie(w, sessionCookie, sid, int(sessionTTL/time.Second))
	a.log.Info("user signed in", "login", login)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *authManager) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.saveLocked()
		a.mu.Unlock()
	}
	a.setCookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *authManager) exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", a.cfg.ClientID)
	form.Set("client_secret", a.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", a.redirectURI())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var out struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("no access token (%s: %s)", out.Error, out.ErrorDescription)
	}
	return out.AccessToken, nil
}

func (a *authManager) fetchLogin(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.apiBase+"/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aeman")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("user endpoint returned %s", resp.Status)
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", err
	}
	return u.Login, nil
}
