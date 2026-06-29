package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// authCodeTTL bounds how long an issued MCP authorization code stays redeemable.
const authCodeTTL = 5 * time.Minute

// githubTokenExtraKey is the key under which the verifier stashes the caller's
// GitHub token in auth.TokenInfo.Extra; the /mcp token-injecting middleware
// reads it back from the per-request TokenInfo.
const githubTokenExtraKey = "github_token"

// registerOAuthServer wires the OAuth 2.0 authorization-server endpoints that
// front the MCP transport: metadata discovery, dynamic client registration, the
// authorize redirect and the token endpoint. It is only mounted in OAuth mode.
func (s *Server) registerOAuthServer(mux *http.ServeMux) {
	a := s.auth
	base := a.baseURL()
	mux.Handle("/.well-known/oauth-protected-resource", auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               base + "/mcp",
		AuthorizationServers:   []string{base},
		ScopesSupported:        a.scopeList(),
		BearerMethodsSupported: []string{"header"},
	}))
	mux.HandleFunc("/.well-known/oauth-authorization-server", a.handleAuthServerMetadata)
	mux.HandleFunc("/oauth/register", a.handleRegister)
	mux.HandleFunc("/oauth/authorize", a.handleAuthorize)
	mux.HandleFunc("/oauth/token", a.handleToken)
}

// baseURL is the public origin with any trailing slash trimmed.
func (a *authManager) baseURL() string {
	return strings.TrimRight(a.cfg.BaseURL, "/")
}

// scopeList splits the configured space-separated scopes into a slice.
func (a *authManager) scopeList() []string {
	return strings.Fields(a.cfg.Scopes)
}

// handleAuthServerMetadata serves RFC 8414 authorization-server metadata. It
// advertises PKCE S256, which MCP clients (e.g. Claude Code) require.
func (a *authManager) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	base := a.baseURL()
	writeJSON(w, http.StatusOK, oauthex.AuthServerMeta{
		Issuer:                            base,
		AuthorizationEndpoint:             base + "/oauth/authorize",
		TokenEndpoint:                     base + "/oauth/token",
		RegistrationEndpoint:              base + "/oauth/register",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                   a.scopeList(),
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
}

// handleRegister implements RFC 7591 dynamic client registration. It is public
// (no auth) and CORS-enabled so browser-based MCP clients can self-register.
func (a *authManager) handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	var meta oauthex.ClientRegistrationMetadata
	if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON body")
		return
	}
	if len(meta.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "redirect_uris is required")
		return
	}
	clientID := randToken()
	a.mu.Lock()
	a.clients[clientID] = oauthClient{redirectURIs: append([]string(nil), meta.RedirectURIs...)}
	a.mu.Unlock()

	resp := oauthex.ClientRegistrationResponse{
		ClientRegistrationMetadata: oauthex.ClientRegistrationMetadata{
			RedirectURIs:            meta.RedirectURIs,
			TokenEndpointAuthMethod: "none",
			GrantTypes:              []string{"authorization_code"},
			ResponseTypes:           []string{"code"},
		},
		ClientID: clientID,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(&resp)
}

// handleAuthorize starts the MCP authorization-code flow. After validating the
// client and PKCE parameters it stashes the request and bounces the browser to
// GitHub; the GitHub callback resumes it in handleMCPCallback.
func (a *authManager) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")

	a.mu.Lock()
	client, ok := a.clients[clientID]
	a.mu.Unlock()
	// A bad client or redirect_uri is a hard 400: redirecting an unverified URI
	// would be an open redirect.
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "unknown client_id")
		return
	}
	if !slices.Contains(client.redirectURIs, redirectURI) {
		writeJSONError(w, http.StatusBadRequest, "redirect_uri is not registered for this client")
		return
	}

	clientState := q.Get("state")
	if q.Get("response_type") != "code" {
		a.redirectError(w, r, redirectURI, clientState, "unsupported_response_type", "response_type must be code")
		return
	}
	if q.Get("code_challenge_method") != "S256" {
		a.redirectError(w, r, redirectURI, clientState, "invalid_request", "code_challenge_method must be S256")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" {
		a.redirectError(w, r, redirectURI, clientState, "invalid_request", "code_challenge is required")
		return
	}

	state := randToken()
	a.mu.Lock()
	a.pendingAuth[state] = pendingAuth{
		clientID:      clientID,
		redirectURI:   redirectURI,
		codeChallenge: challenge,
		clientState:   clientState,
	}
	a.mu.Unlock()

	gh := url.Values{}
	gh.Set("client_id", a.cfg.ClientID)
	gh.Set("redirect_uri", a.redirectURI())
	gh.Set("scope", a.cfg.Scopes)
	gh.Set("state", state)
	http.Redirect(w, r, a.authorizeURL+"?"+gh.Encode(), http.StatusFound)
}

// handleMCPCallback resumes an MCP authorization after GitHub redirects back: it
// exchanges the GitHub code for a token, opens a session, mints a single-use MCP
// code and bounces the browser back to the client's redirect_uri.
func (a *authManager) handleMCPCallback(w http.ResponseWriter, r *http.Request, p pendingAuth) {
	code := r.URL.Query().Get("code")
	if code == "" {
		a.redirectError(w, r, p.redirectURI, p.clientState, "invalid_request", "missing authorization code")
		return
	}
	ghToken, err := a.exchange(r.Context(), code)
	if err != nil {
		a.log.Error("mcp oauth token exchange failed", "err", err)
		a.redirectError(w, r, p.redirectURI, p.clientState, "server_error", "token exchange failed")
		return
	}
	login, err := a.fetchLogin(r.Context(), ghToken)
	if err != nil {
		a.log.Error("mcp oauth user lookup failed", "err", err)
		a.redirectError(w, r, p.redirectURI, p.clientState, "server_error", "could not read GitHub user")
		return
	}

	now := time.Now()
	sid := randToken()
	mcpCode := randToken()
	a.mu.Lock()
	a.sessions[sid] = oauthSession{token: ghToken, login: login, created: now}
	a.saveLocked()
	a.authCodes[mcpCode] = authCode{
		sid:           sid,
		codeChallenge: p.codeChallenge,
		redirectURI:   p.redirectURI,
		clientID:      p.clientID,
		expiry:        now.Add(authCodeTTL),
	}
	a.mu.Unlock()
	a.log.Info("mcp user signed in", "login", login)

	dest, err := url.Parse(p.redirectURI)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "invalid redirect_uri")
		return
	}
	rq := dest.Query()
	rq.Set("code", mcpCode)
	if p.clientState != "" {
		rq.Set("state", p.clientState)
	}
	dest.RawQuery = rq.Encode()
	http.Redirect(w, r, dest.String(), http.StatusFound)
}

// handleToken redeems an MCP authorization code for an access token. The token
// is the session id, so its lifetime is the session's. Codes are single-use.
func (a *authManager) handleToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	p := parseTokenParams(r)
	if p.grantType != "authorization_code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code")
		return
	}

	// Pop the code first: a code is single-use, so any redemption attempt (even a
	// failing one) burns it, which blocks PKCE brute-forcing and replays.
	a.mu.Lock()
	c, ok := a.authCodes[p.code]
	if ok {
		delete(a.authCodes, p.code)
	}
	a.mu.Unlock()
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown or already-used code")
		return
	}
	if time.Now().After(c.expiry) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code has expired")
		return
	}
	if p.redirectURI != c.redirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if p.clientID != c.clientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	if !verifyPKCE(p.codeVerifier, c.codeChallenge) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}

	a.mu.Lock()
	sess, live := a.sessions[c.sid]
	a.mu.Unlock()
	if !live || time.Since(sess.created) > sessionTTL {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "session is no longer valid")
		return
	}
	expiresIn := int(time.Until(sess.created.Add(sessionTTL)).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": c.sid,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
	})
}

// verifyToken is the auth.TokenVerifier backing /mcp: it maps a bearer token
// (= session id) to its TokenInfo, carrying the user's GitHub token in Extra.
func (a *authManager) verifyToken(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	a.mu.Lock()
	s, ok := a.sessions[token]
	if ok && time.Since(s.created) > sessionTTL {
		delete(a.sessions, token)
		a.saveLocked()
		ok = false
	}
	a.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown or expired session: %w", auth.ErrInvalidToken)
	}
	return &auth.TokenInfo{
		UserID:     s.login,
		Expiration: s.created.Add(sessionTTL),
		Extra:      map[string]any{githubTokenExtraKey: s.token},
	}, nil
}

// tokenParams holds the fields the token endpoint reads from form or JSON.
type tokenParams struct {
	grantType    string
	code         string
	codeVerifier string
	redirectURI  string
	clientID     string
}

// parseTokenParams reads the token request from a JSON or form-encoded body.
func parseTokenParams(r *http.Request) tokenParams {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var in struct {
			GrantType    string `json:"grant_type"`
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
			RedirectURI  string `json:"redirect_uri"`
			ClientID     string `json:"client_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		return tokenParams{in.GrantType, in.Code, in.CodeVerifier, in.RedirectURI, in.ClientID}
	}
	_ = r.ParseForm()
	return tokenParams{
		grantType:    r.PostFormValue("grant_type"),
		code:         r.PostFormValue("code"),
		codeVerifier: r.PostFormValue("code_verifier"),
		redirectURI:  r.PostFormValue("redirect_uri"),
		clientID:     r.PostFormValue("client_id"),
	}
}

// verifyPKCE checks an S256 code_verifier against the stored challenge.
func verifyPKCE(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// redirectError bounces an OAuth error back to a (validated) client redirect_uri.
func (a *authManager) redirectError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, desc string) {
	dest, err := url.Parse(redirectURI)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, code+": "+desc)
		return
	}
	q := dest.Query()
	q.Set("error", code)
	if desc != "" {
		q.Set("error_description", desc)
	}
	if state != "" {
		q.Set("state", state)
	}
	dest.RawQuery = q.Encode()
	// redirectURI is always a redirect URI the client pre-registered (validated
	// in handleAuthorize before this is reached), so this is not an open redirect.
	http.Redirect(w, r, dest.String(), http.StatusFound) //nolint:gosec // redirect_uri validated against the registered client
}

// writeOAuthError writes an RFC 6749 token-endpoint error response.
func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}
