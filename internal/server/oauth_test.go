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
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

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

func TestOAuthAuthorizeUnknownClient(t *testing.T) {
	srv, _ := newOAuthServer(t)
	aq := url.Values{}
	aq.Set("client_id", "does-not-exist")
	aq.Set("redirect_uri", testRedirectURI)
	aq.Set("response_type", "code")
	aq.Set("code_challenge", "x")
	aq.Set("code_challenge_method", "S256")
	rec := do(t, srv, http.MethodGet, "/oauth/authorize?"+aq.Encode(), "")
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

// The pre-envelope session file (a bare map of sessions) still loads.
func TestSessionFileLegacyFormat(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	legacy := `{"sid1":{"token":"gh-tok","login":"octocat","created":"` +
		time.Now().Format(time.RFC3339Nano) + `"}}`
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
	if _, ok := srv.auth.sessions["sid1"]; !ok {
		t.Fatal("legacy session not restored")
	}
}
