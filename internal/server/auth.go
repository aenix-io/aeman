package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token" //nolint:gosec // OAuth endpoint, not a credential
	sessionCookie      = "aeman_session"
	stateCookie        = "aeman_oauth_state"
	sessionTTL         = 14 * 24 * time.Hour
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
}

type oauthSession struct {
	token   string
	login   string
	created time.Time
}

// authManager runs the GitHub OAuth web flow and keeps per-user sessions in
// memory (sessions are lost on restart, which only forces a re-login).
type authManager struct {
	cfg    OAuthConfig
	secure bool
	log    *slog.Logger
	client *http.Client

	mu       sync.Mutex
	sessions map[string]oauthSession
}

func newAuthManager(cfg OAuthConfig, log *slog.Logger) *authManager {
	if cfg.Scopes == "" {
		cfg.Scopes = "repo project"
	}
	return &authManager{
		cfg:      cfg,
		secure:   strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://"),
		log:      log,
		client:   &http.Client{Timeout: 15 * time.Second},
		sessions: map[string]oauthSession{},
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
	http.Redirect(w, r, githubAuthorizeURL+"?"+q.Encode(), http.StatusFound)
}

func (a *authManager) handleCallback(w http.ResponseWriter, r *http.Request) {
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
	a.mu.Unlock()
	a.setCookie(w, sessionCookie, sid, int(sessionTTL/time.Second))
	a.log.Info("user signed in", "login", login)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *authManager) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		a.mu.Lock()
		delete(a.sessions, c.Value)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(form.Encode()))
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIBase+"/user", nil)
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
