package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const testRemoteRedirectURI = "https://client.example/cb"

var consentIDRe = regexp.MustCompile(`name="consent_id" value="([^"]+)"`)

const testBaseURL = "https://aeman.test"

const testRedirectURI = "http://127.0.0.1:9999/callback"

// newOAuthServer builds an OAuth-mode aeman server whose GitHub endpoints point
// at a stub, so the whole authorization-code flow runs without the network.
func newOAuthServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gh-token-xyz"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]string{"login": "octocat"})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(gh.Close)

	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &OAuthConfig{
			ClientID:     "gh-client",
			ClientSecret: "gh-secret",
			BaseURL:      testBaseURL,
			Scopes:       "repo project",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.auth.authorizeURL = gh.URL + "/login/oauth/authorize"
	srv.auth.tokenURL = gh.URL + "/login/oauth/access_token"
	srv.auth.apiBase = gh.URL
	return srv, gh
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func postForm(t *testing.T, srv *Server, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, r)
	return rec
}

func registerClient(t *testing.T, srv *Server, redirectURI string) string {
	t.Helper()
	rec := do(t, srv, http.MethodPost, "/oauth/register", `{"redirect_uris":["`+redirectURI+`"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp oauthex.ClientRegistrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	if resp.ClientID == "" {
		t.Fatal("register returned no client_id")
	}
	return resp.ClientID
}

// mintCode drives register → authorize → GitHub callback and returns a fresh,
// redeemable MCP authorization code together with its PKCE verifier.
func mintCode(t *testing.T, srv *Server) (code, clientID, redirectURI, verifier string) {
	t.Helper()
	redirectURI = testRedirectURI
	clientID = registerClient(t, srv, redirectURI)
	verifier = "pkce-verifier-sufficiently-long-0123456789"
	aq := url.Values{}
	aq.Set("client_id", clientID)
	aq.Set("redirect_uri", redirectURI)
	aq.Set("response_type", "code")
	aq.Set("code_challenge", pkceChallenge(verifier))
	aq.Set("code_challenge_method", "S256")
	aq.Set("state", "client-state")
	rec := do(t, srv, http.MethodGet, "/oauth/authorize?"+aq.Encode(), "")
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize status = %d, body = %s", rec.Code, rec.Body.String())
	}
	ghLoc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize redirect: %v", err)
	}
	cb := do(t, srv, http.MethodGet, "/auth/callback?code=gh-code&state="+ghLoc.Query().Get("state"), "")
	if cb.Code != http.StatusFound {
		t.Fatalf("callback status = %d, body = %s", cb.Code, cb.Body.String())
	}
	cbLoc, err := url.Parse(cb.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback redirect: %v", err)
	}
	code = cbLoc.Query().Get("code")
	if code == "" {
		t.Fatalf("callback produced no code: %s", cb.Header().Get("Location"))
	}
	return code, clientID, redirectURI, verifier
}

func redeemToken(t *testing.T, srv *Server, code, verifier, redirectURI, clientID string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	return postForm(t, srv, "/oauth/token", form)
}

func assertOAuthError(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode oauth error: %v (body=%s)", err, rec.Body.String())
	}
	if e.Error != want {
		t.Fatalf("error = %q, want %q", e.Error, want)
	}
}

func TestOAuthProtectedResourceMetadata(t *testing.T) {
	srv, _ := newOAuthServer(t)
	rec := do(t, srv, http.MethodGet, "/.well-known/oauth-protected-resource", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var m oauthex.ProtectedResourceMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Resource != testBaseURL+"/mcp" {
		t.Fatalf("resource = %q", m.Resource)
	}
	if len(m.AuthorizationServers) != 1 || m.AuthorizationServers[0] != testBaseURL {
		t.Fatalf("authorization_servers = %v", m.AuthorizationServers)
	}
	if len(m.BearerMethodsSupported) != 1 || m.BearerMethodsSupported[0] != "header" {
		t.Fatalf("bearer_methods_supported = %v", m.BearerMethodsSupported)
	}
}

func TestOAuthAuthServerMetadata(t *testing.T) {
	srv, _ := newOAuthServer(t)
	rec := do(t, srv, http.MethodGet, "/.well-known/oauth-authorization-server", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS = %q, want *", got)
	}
	if !strings.Contains(rec.Body.String(), `"code_challenge_methods_supported":["S256"]`) {
		t.Fatalf("body must advertise S256: %s", rec.Body.String())
	}
	var m oauthex.AuthServerMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Issuer != testBaseURL ||
		m.AuthorizationEndpoint != testBaseURL+"/oauth/authorize" ||
		m.TokenEndpoint != testBaseURL+"/oauth/token" ||
		m.RegistrationEndpoint != testBaseURL+"/oauth/register" {
		t.Fatalf("endpoints = %+v", m)
	}
	if len(m.CodeChallengeMethodsSupported) != 1 || m.CodeChallengeMethodsSupported[0] != "S256" {
		t.Fatalf("code_challenge_methods_supported = %v", m.CodeChallengeMethodsSupported)
	}
	if len(m.TokenEndpointAuthMethodsSupported) != 1 || m.TokenEndpointAuthMethodsSupported[0] != "none" {
		t.Fatalf("token_endpoint_auth_methods_supported = %v", m.TokenEndpointAuthMethodsSupported)
	}
}

func TestOAuthRegister(t *testing.T) {
	srv, _ := newOAuthServer(t)
	rec := do(t, srv, http.MethodPost, "/oauth/register", `{"redirect_uris":["`+testRedirectURI+`"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp oauthex.ClientRegistrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ClientID == "" {
		t.Fatal("no client_id")
	}
	if resp.TokenEndpointAuthMethod != "none" {
		t.Fatalf("token_endpoint_auth_method = %q, want none", resp.TokenEndpointAuthMethod)
	}
	if len(resp.RedirectURIs) != 1 || resp.RedirectURIs[0] != testRedirectURI {
		t.Fatalf("redirect_uris = %v", resp.RedirectURIs)
	}
}

func TestOAuthRegisterRequiresRedirectURIs(t *testing.T) {
	srv, _ := newOAuthServer(t)
	rec := do(t, srv, http.MethodPost, "/oauth/register", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestOAuthAuthorizeUnregisteredRedirect(t *testing.T) {
	srv, _ := newOAuthServer(t)
	clientID := registerClient(t, srv, testRedirectURI)
	aq := url.Values{}
	aq.Set("client_id", clientID)
	aq.Set("redirect_uri", "http://evil.example/callback")
	aq.Set("response_type", "code")
	aq.Set("code_challenge", "x")
	aq.Set("code_challenge_method", "S256")
	rec := do(t, srv, http.MethodGet, "/oauth/authorize?"+aq.Encode(), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestOAuthFullFlow(t *testing.T) {
	srv, gh := newOAuthServer(t)
	redirectURI := testRedirectURI
	clientID := registerClient(t, srv, redirectURI)

	verifier := "verifier-abc123-long-enough-for-pkce-000000"
	clientState := "client-state-zzz"

	aq := url.Values{}
	aq.Set("client_id", clientID)
	aq.Set("redirect_uri", redirectURI)
	aq.Set("response_type", "code")
	aq.Set("code_challenge", pkceChallenge(verifier))
	aq.Set("code_challenge_method", "S256")
	aq.Set("state", clientState)
	rec := do(t, srv, http.MethodGet, "/oauth/authorize?"+aq.Encode(), "")
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize status = %d, body = %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, gh.URL+"/login/oauth/authorize") {
		t.Fatalf("authorize must redirect to the stub GitHub: %s", loc)
	}
	ghLoc, _ := url.Parse(loc)
	ghState := ghLoc.Query().Get("state")
	if ghState == "" {
		t.Fatalf("no GitHub state in %s", loc)
	}
	if got := ghLoc.Query().Get("redirect_uri"); got != testBaseURL+"/auth/callback" {
		t.Fatalf("GitHub redirect_uri = %q", got)
	}

	cb := do(t, srv, http.MethodGet, "/auth/callback?code=gh-code&state="+ghState, "")
	if cb.Code != http.StatusFound {
		t.Fatalf("callback status = %d, body = %s", cb.Code, cb.Body.String())
	}
	cbLoc, err := url.Parse(cb.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback redirect: %v", err)
	}
	if cbLoc.Scheme+"://"+cbLoc.Host+cbLoc.Path != redirectURI {
		t.Fatalf("callback redirect = %q, want %q", cb.Header().Get("Location"), redirectURI)
	}
	if cbLoc.Query().Get("state") != clientState {
		t.Fatalf("client state = %q, want %q", cbLoc.Query().Get("state"), clientState)
	}
	code := cbLoc.Query().Get("code")
	if code == "" {
		t.Fatalf("no MCP code in %s", cb.Header().Get("Location"))
	}

	tok := redeemToken(t, srv, code, verifier, redirectURI, clientID)
	if tok.Code != http.StatusOK {
		t.Fatalf("token status = %d, body = %s", tok.Code, tok.Body.String())
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(tok.Body.Bytes(), &tr); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if tr.AccessToken == "" || tr.TokenType != "Bearer" || tr.ExpiresIn <= 0 {
		t.Fatalf("token response = %+v", tr)
	}

	ti, err := srv.auth.verifyToken(context.Background(), tr.AccessToken, nil)
	if err != nil {
		t.Fatalf("verifyToken: %v", err)
	}
	if ti.UserID != "octocat" {
		t.Fatalf("UserID = %q, want octocat", ti.UserID)
	}
	if got, _ := ti.Extra[githubTokenExtraKey].(string); got != "gh-token-xyz" {
		t.Fatalf("github token = %v, want gh-token-xyz", ti.Extra[githubTokenExtraKey])
	}
}

func TestOAuthTokenWrongVerifier(t *testing.T) {
	srv, _ := newOAuthServer(t)
	code, clientID, redirectURI, _ := mintCode(t, srv)
	rec := redeemToken(t, srv, code, "wrong-verifier", redirectURI, clientID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertOAuthError(t, rec, "invalid_grant")
}

func TestOAuthCodeSingleUse(t *testing.T) {
	srv, _ := newOAuthServer(t)
	code, clientID, redirectURI, verifier := mintCode(t, srv)
	first := redeemToken(t, srv, code, verifier, redirectURI, clientID)
	if first.Code != http.StatusOK {
		t.Fatalf("first redeem = %d, body = %s", first.Code, first.Body.String())
	}
	second := redeemToken(t, srv, code, verifier, redirectURI, clientID)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("second redeem = %d, want 400", second.Code)
	}
	assertOAuthError(t, second, "invalid_grant")
}

func TestOAuthCodeExpired(t *testing.T) {
	srv, _ := newOAuthServer(t)
	code, clientID, redirectURI, verifier := mintCode(t, srv)
	srv.auth.mu.Lock()
	c := srv.auth.authCodes[code]
	c.expiry = time.Now().Add(-time.Minute)
	srv.auth.authCodes[code] = c
	srv.auth.mu.Unlock()
	rec := redeemToken(t, srv, code, verifier, redirectURI, clientID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertOAuthError(t, rec, "invalid_grant")
}

func TestVerifyTokenUnknown(t *testing.T) {
	srv, _ := newOAuthServer(t)
	if _, err := srv.auth.verifyToken(context.Background(), "no-such-session", nil); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyTokenExpired(t *testing.T) {
	srv, _ := newOAuthServer(t)
	sid := randToken()
	srv.auth.mu.Lock()
	srv.auth.sessions[sid] = oauthSession{token: "x", login: "y", created: time.Now().Add(-sessionTTL - time.Hour)}
	srv.auth.mu.Unlock()
	if _, err := srv.auth.verifyToken(context.Background(), sid, nil); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestMCPInitializeWithBearer(t *testing.T) {
	srv, _ := newOAuthServer(t)
	code, clientID, redirectURI, verifier := mintCode(t, srv)
	tok := redeemToken(t, srv, code, verifier, redirectURI, clientID)
	if tok.Code != http.StatusOK {
		t.Fatalf("token status = %d, body = %s", tok.Code, tok.Body.String())
	}
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tok.Body.Bytes(), &tr); err != nil {
		t.Fatalf("decode token: %v", err)
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("initialize status = %d (auth must pass and MCP must init), body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Mcp-Session-Id") == "" {
		t.Fatalf("expected an Mcp-Session-Id header for a fresh session")
	}
}

func TestMCPRequiresBearer(t *testing.T) {
	srv, _ := newOAuthServer(t)
	rec := do(t, srv, http.MethodPost, "/mcp", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	wa := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(wa, "resource_metadata") {
		t.Fatalf("WWW-Authenticate = %q, want resource_metadata", wa)
	}
	if !strings.Contains(wa, testBaseURL+"/.well-known/oauth-protected-resource") {
		t.Fatalf("WWW-Authenticate = %q, missing metadata URL", wa)
	}
}

// A dynamically-registered MCP client must survive a server restart: MCP
// clients cache their client_id, and a wiped registry would strand them on
// invalid_client until a manual re-registration.
func TestOAuthClientRegistrationPersists(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	mk := func() *Server {
		srv, err := New(Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Auth: &OAuthConfig{
				ClientID:     "gh-client",
				ClientSecret: "gh-secret",
				BaseURL:      testBaseURL,
				SessionFile:  path,
			},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return srv
	}
	srv := mk()
	rec := do(t, srv, http.MethodPost, "/oauth/register", `{"redirect_uris":["`+testRedirectURI+`"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d", rec.Code)
	}
	var resp oauthex.ClientRegistrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// A fresh server over the same file must know the client: /oauth/authorize
	// gets past client validation (302 to GitHub, not a 400 invalid_client).
	srv2 := mk()
	aq := url.Values{}
	aq.Set("client_id", resp.ClientID)
	aq.Set("redirect_uri", testRedirectURI)
	aq.Set("response_type", "code")
	aq.Set("code_challenge", pkceChallenge("v"))
	aq.Set("code_challenge_method", "S256")
	rec = do(t, srv2, http.MethodGet, "/oauth/authorize?"+aq.Encode(), "")
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize after restart = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// GitHub tokens are not persisted: a file written by an older version that
// stored sessions must NOT restore them, and the plaintext tokens must be
// scrubbed from disk on load — while a registered client in the same file is
// still restored.
func TestSessionTokensNotPersisted(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	legacy := `{"sessions":{"sid1":{"token":"gh-tok","login":"octocat","created":"` +
		time.Now().Format(time.RFC3339Nano) + `"}},"clients":{"cid1":{"redirectUris":["` +
		testRedirectURI + `"]}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &OAuthConfig{
			ClientID:     "gh-client",
			ClientSecret: "gh-secret",
			BaseURL:      testBaseURL,
			SessionFile:  path,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := srv.auth.sessions["sid1"]; ok {
		t.Fatal("a persisted GitHub token was restored; tokens must never survive a restart")
	}
	if _, ok := srv.auth.clients["cid1"]; !ok {
		t.Fatal("the registered client was not restored")
	}
	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(on), "gh-tok") {
		t.Fatalf("plaintext token was not scrubbed from disk: %s", on)
	}
	if !strings.Contains(string(on), "cid1") {
		t.Fatalf("client registry was lost from disk: %s", on)
	}
}

// An unknown client with a loopback redirect self-heals: the authorize step
// runs in the browser, so the MCP client cannot see the error and re-register
// on its own — the server registers it on the fly instead (safe per RFC 8252:
// the code can only land on the initiator's own machine).
func TestOAuthAuthorizeAutoRegistersLoopbackClient(t *testing.T) {
	srv, _ := newOAuthServer(t)
	aq := url.Values{}
	aq.Set("client_id", "stale-cached-client")
	aq.Set("redirect_uri", "http://localhost:3118/callback")
	aq.Set("response_type", "code")
	aq.Set("code_challenge", pkceChallenge("v"))
	aq.Set("code_challenge_method", "S256")
	rec := do(t, srv, http.MethodGet, "/oauth/authorize?"+aq.Encode(), "")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s (loopback client must auto-register)", rec.Code, rec.Body.String())
	}
	srv.auth.mu.Lock()
	_, ok := srv.auth.clients["stale-cached-client"]
	srv.auth.mu.Unlock()
	if !ok {
		t.Fatal("client not persisted in the registry")
	}
}

// A non-loopback redirect for an unknown client keeps the hard error: blindly
// registering a remote redirect target would hand authorization codes to
// whoever crafted the link.
func TestOAuthAuthorizeUnknownClientRemoteRedirect(t *testing.T) {
	srv, _ := newOAuthServer(t)
	aq := url.Values{}
	aq.Set("client_id", "stale-cached-client")
	aq.Set("redirect_uri", "https://evil.example/callback")
	aq.Set("response_type", "code")
	aq.Set("code_challenge", pkceChallenge("v"))
	aq.Set("code_challenge_method", "S256")
	rec := do(t, srv, http.MethodGet, "/oauth/authorize?"+aq.Encode(), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// driveToConsent runs register(remote) → authorize → GitHub callback for a
// non-loopback redirect and returns the consent page: the callback must render
// the approval screen (not deliver a code), the consent id embedded in it, and
// the browser-binding cookie the server set.
func driveToConsent(t *testing.T, srv *Server, redirectURI string) (consentID, cookie, verifier, clientID string) {
	t.Helper()
	clientID = registerClient(t, srv, redirectURI)
	verifier = "pkce-verifier-sufficiently-long-0123456789"
	aq := url.Values{}
	aq.Set("client_id", clientID)
	aq.Set("redirect_uri", redirectURI)
	aq.Set("response_type", "code")
	aq.Set("code_challenge", pkceChallenge(verifier))
	aq.Set("code_challenge_method", "S256")
	aq.Set("state", "client-state")
	rec := do(t, srv, http.MethodGet, "/oauth/authorize?"+aq.Encode(), "")
	ghLoc, _ := url.Parse(rec.Header().Get("Location"))

	cb := do(t, srv, http.MethodGet, "/auth/callback?code=gh-code&state="+ghLoc.Query().Get("state"), "")
	if cb.Code != http.StatusOK {
		t.Fatalf("remote-redirect callback status = %d, want 200 consent page (no code delivered)", cb.Code)
	}
	if loc := cb.Header().Get("Location"); loc != "" {
		t.Fatalf("consent must not redirect yet; got Location=%s", loc)
	}
	if !strings.Contains(cb.Body.String(), redirectURI) {
		t.Fatal("consent page must show the redirect target to the user")
	}
	m := consentIDRe.FindStringSubmatch(cb.Body.String())
	if m == nil {
		t.Fatalf("no consent_id in page: %s", cb.Body.String())
	}
	for _, c := range cb.Result().Cookies() {
		if c.Name == consentCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("consent flow set no browser-binding cookie")
	}
	return m[1], cookie, verifier, clientID
}

func postConsent(t *testing.T, srv *Server, consentID, action, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("consent_id", consentID)
	form.Set("action", action)
	r := httptest.NewRequest(http.MethodPost, "/oauth/consent", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: consentCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, r)
	return rec
}

// A remote (non-loopback) redirect must go through the consent screen, and
// approving it (from the same browser) then delivers a redeemable code. This is
// the account-takeover fix: no code reaches a remote URI without the user
// having seen and approved that URI.
func TestOAuthConsentApproveDeliversCode(t *testing.T) {
	srv, _ := newOAuthServer(t)
	consentID, cookie, verifier, clientID := driveToConsent(t, srv, testRemoteRedirectURI)

	rec := postConsent(t, srv, consentID, "approve", cookie)
	if rec.Code != http.StatusFound {
		t.Fatalf("approve status = %d, want 302", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Scheme+"://"+loc.Host+loc.Path != testRemoteRedirectURI {
		t.Fatalf("approve redirected to %s, want the client redirect_uri", loc)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("approve delivered no authorization code")
	}
	// The delivered code redeems for an access token, completing the flow.
	tok := redeemToken(t, srv, code, verifier, testRemoteRedirectURI, clientID)
	if tok.Code != http.StatusOK {
		t.Fatalf("redeem status = %d, body = %s", tok.Code, tok.Body.String())
	}
	if !strings.Contains(tok.Body.String(), "access_token") {
		t.Fatalf("no access_token in redeem response: %s", tok.Body.String())
	}
}

// Approving from a different browser (no matching consent cookie) is rejected —
// a forged or cross-site submission cannot complete the authorization.
func TestOAuthConsentRejectsWrongBrowser(t *testing.T) {
	srv, _ := newOAuthServer(t)
	consentID, _, _, _ := driveToConsent(t, srv, testRemoteRedirectURI)

	if rec := postConsent(t, srv, consentID, "approve", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("approve without the browser cookie status = %d, want 400", rec.Code)
	}
	if rec := postConsent(t, srv, consentID, "approve", "wrong-cookie-value"); rec.Code != http.StatusBadRequest {
		t.Fatalf("approve with a mismatched cookie status = %d, want 400", rec.Code)
	}
}

// Denying redirects back with an OAuth error and hands out no code.
func TestOAuthConsentDeny(t *testing.T) {
	srv, _ := newOAuthServer(t)
	consentID, cookie, _, _ := driveToConsent(t, srv, testRemoteRedirectURI)

	rec := postConsent(t, srv, consentID, "deny", cookie)
	if rec.Code != http.StatusFound {
		t.Fatalf("deny status = %d, want 302", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Query().Get("error") != "access_denied" {
		t.Fatalf("deny error = %q, want access_denied", loc.Query().Get("error"))
	}
	if loc.Query().Get("code") != "" {
		t.Fatal("deny must not deliver a code")
	}
}

// Registration rejects redirect URIs that could leak a code: non-https remote
// schemes and cleartext http to a remote host.
func TestOAuthRegisterRejectsUnsafeRedirects(t *testing.T) {
	srv, _ := newOAuthServer(t)
	for _, bad := range []string{
		"javascript:alert(1)",
		"http://evil.example/cb",
		"data:text/html,x",
	} {
		rec := do(t, srv, http.MethodPost, "/oauth/register", `{"redirect_uris":["`+bad+`"]}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("register %q status = %d, want 400", bad, rec.Code)
		}
	}
}

// Registration is rate-limited so an unauthenticated flood cannot grow the
// registry / rewrite the state file without bound.
func TestOAuthRegisterRateLimited(t *testing.T) {
	srv, _ := newOAuthServer(t)
	srv.auth.registerLimiter = newRateLimiter(3, time.Minute)
	limited := false
	for i := 0; i < 6; i++ {
		rec := do(t, srv, http.MethodPost, "/oauth/register", `{"redirect_uris":["`+testRedirectURI+`"]}`)
		if rec.Code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("registration flood was never rate-limited")
	}
}

// pruneEphemeralLocked clears abandoned authorize flows, expired codes, and
// stale consents so none of them accumulate.
func TestPruneEphemeral(t *testing.T) {
	srv, _ := newOAuthServer(t)
	a := srv.auth
	a.mu.Lock()
	a.pendingAuth["stale"] = pendingAuth{created: time.Now().Add(-2 * pendingAuthTTL)}
	a.pendingAuth["fresh"] = pendingAuth{created: time.Now()}
	a.authCodes["expired"] = authCode{expiry: time.Now().Add(-time.Minute)}
	a.authCodes["live"] = authCode{expiry: time.Now().Add(time.Minute)}
	a.pendingConsent["old"] = pendingConsent{expiry: time.Now().Add(-time.Minute)}
	a.pruneEphemeralLocked()
	_, staleGone := a.pendingAuth["stale"]
	_, freshKept := a.pendingAuth["fresh"]
	_, expiredGone := a.authCodes["expired"]
	_, liveKept := a.authCodes["live"]
	_, oldGone := a.pendingConsent["old"]
	a.mu.Unlock()
	if staleGone || expiredGone || oldGone {
		t.Fatal("expired ephemeral entries were not swept")
	}
	if !freshKept || !liveKept {
		t.Fatal("live ephemeral entries were wrongly swept")
	}
}

// A session is only as good as the GitHub token inside it. When GitHub starts
// rejecting that token, keeping the session alive for its full TTL is the
// worst answer: the client's own credentials still verify, so it retries a
// dead token forever and cannot tell whose authorization broke. Dropping the
// session forces the one thing that fixes it — signing in again.
func TestDropSessionForgetsIt(t *testing.T) {
	a := quietAuth(t, OAuthConfig{SessionFile: t.TempDir() + "/sessions.json"})
	a.sessions["sid"] = oauthSession{token: "dead-gh-token", login: "octocat", created: time.Now()}

	// While it exists, /mcp accepts the bearer and hands out the token.
	if _, err := a.verifyToken(context.Background(), "sid", nil); err != nil {
		t.Fatalf("a live session must verify: %v", err)
	}

	if login := a.dropSession("sid"); login != "octocat" {
		t.Fatalf("dropSession must report whose session it was, got %q", login)
	}
	if _, ok := a.sessions["sid"]; ok {
		t.Fatal("the session must be gone")
	}
	// Now the MCP client is told to authenticate again, instead of retrying.
	if _, err := a.verifyToken(context.Background(), "sid", nil); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("a dropped session must fail verification with ErrInvalidToken, got %v", err)
	}
	// Dropping again (a burst of failing calls) is harmless.
	if login := a.dropSession("sid"); login != "" {
		t.Fatalf("dropping an unknown session must be a no-op, got %q", login)
	}
}

// The MCP verifier hands the session id down with the token, so a handler
// that sees GitHub reject the call knows exactly which session to drop.
func TestVerifyTokenCarriesSessionID(t *testing.T) {
	a := quietAuth(t, OAuthConfig{SessionFile: t.TempDir() + "/sessions.json"})
	a.sessions["sid"] = oauthSession{token: "gh", login: "octocat", created: time.Now()}
	info, err := a.verifyToken(context.Background(), "sid", nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Extra[sessionIDExtraKey] != "sid" || info.Extra[githubTokenExtraKey] != "gh" {
		t.Fatalf("extras = %+v", info.Extra)
	}
}

// newestSessionFor picks the login's freshest live session.
func TestNewestSessionFor(t *testing.T) {
	a := newAuthManager(OAuthConfig{ClientID: "id", ClientSecret: "s", BaseURL: "http://x"}, nil)
	now := time.Now()
	a.sessions["old"] = oauthSession{token: "t-old", login: "kvaps", created: now.Add(-2 * time.Hour)}
	a.sessions["new"] = oauthSession{token: "t-new", login: "kvaps", created: now.Add(-time.Minute)}
	a.sessions["other"] = oauthSession{token: "t-x", login: "tym83", created: now}
	sid, s, ok := a.newestSessionFor(context.Background(), "kvaps")
	if !ok || sid != "new" || s.token != "t-new" {
		t.Fatalf("got %q %q %v, want the freshest kvaps session", sid, s.token, ok)
	}
	if _, _, ok := a.newestSessionFor(context.Background(), "nobody"); ok {
		t.Fatal("a login with no sessions must not resolve")
	}
}
