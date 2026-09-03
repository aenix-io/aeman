package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// fakeGitLabForge is enough of a GitLab instance for the sign-in flow: the
// OAuth token endpoint (which insists on GitLab's dialect), who-am-i, and
// the user directory. It records the token form it was posted.
func fakeGitLabForge(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var forms []string
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth/token":
			body, _ := io.ReadAll(r.Body)
			forms = append(forms, string(body))
			if !strings.Contains(string(body), "grant_type=authorization_code") || !strings.Contains(string(body), "redirect_uri=") {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request", "error_description": "GitLab wants grant_type and redirect_uri"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "gl-token-xyz", "refresh_token": "gl-refresh", "expires_in": 7200})
		case "/api/v4/user":
			if r.Header.Get("Authorization") != "Bearer gl-token-xyz" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"username": "kvaps", "name": "Andrei Kvapil", "avatar_url": "https://gitlab.example/uploads/kvaps.png"})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(gl.Close)
	return gl, &forms
}

// tokenAccess is a domainAccess that trusts some tokens and refuses the rest
// the way the forge does (errBadVisitorToken).
type tokenAccess struct {
	good  map[string]bool
	grant *domainRights
}

func (t tokenAccess) rights(_ context.Context, token, _ string) (*domainRights, error) {
	if !t.good[token] {
		return nil, errBadVisitorToken
	}
	return t.grant, nil
}
func (t tokenAccess) readers(_ context.Context, _ string, logins []string) ([]string, error) {
	return logins, nil
}
func (t tokenAccess) canPush(context.Context, string, string) (bool, error) { return true, nil }

// signInWithGitLab drives the browser flow against the fake and returns the
// session cookie.
func signInWithGitLab(t *testing.T, srv *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	var state string
	for _, c := range rec.Result().Cookies() {
		if c.Name == stateCookie {
			state = c.Value
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookie, Value: state})
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

// A token the forge refuses is not the end of the session while a refresh
// token can buy a fresh one: the server renews once and asks again — a
// GitLab token lives two hours, and a token issued a moment ago has been
// seen refused by a lagging replica. Only when the refresh is refused too is
// the session dropped and the visitor sent to sign in again.
func TestARefusedSessionTokenIsRenewedBeforeTheSessionIsDropped(t *testing.T) {
	newServer := func(t *testing.T, refreshOK bool) (*Server, *int) {
		t.Helper()
		refreshes := 0
		gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/oauth/token":
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), "grant_type=refresh_token") {
					refreshes++
					if !refreshOK || !strings.Contains(string(body), "redirect_uri=") {
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "gl-token-2", "refresh_token": "gl-refresh-2", "expires_in": 7200})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "gl-token-1", "refresh_token": "gl-refresh-1", "expires_in": 7200})
			case "/api/v4/user":
				_ = json.NewEncoder(w).Encode(map[string]any{"username": "kvaps", "name": "Andrei Kvapil"})
			default:
				http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			}
		}))
		t.Cleanup(gl.Close)
		shared := gitRemoteN(t, "shared")
		seedGitRemote(t, shared)
		srv, err := New(Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Forge:  forge.NewGitLab(gl.URL),
			Auth:   &OAuthConfig{ClientID: "gl-client", ClientSecret: "gl-secret", BaseURL: testBaseURL},
			Git:    &GitConfig{Repos: []RepoSpec{{Name: "shared", URL: shared.URL}}, DataDir: t.TempDir(), Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"}},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = srv.Close() })
		srv.gitBE.git.pushDelay = 0
		// The forge refuses the first token and trusts the renewed one.
		srv.access = tokenAccess{good: map[string]bool{"gl-token-2": true}, grant: rightsOn([]string{"shared"}, []string{"shared"})}
		return srv, &refreshes
	}
	board := func(srv *Server, session string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/board", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
		rec := httptest.NewRecorder()
		srv.handler.ServeHTTP(rec, req)
		return rec
	}

	srv, refreshes := newServer(t, true)
	session := signInWithGitLab(t, srv)
	if rec := board(srv, session); rec.Code != http.StatusOK {
		t.Fatalf("board after the forge refused the first token: %d %s (want a renewal and a retry)", rec.Code, rec.Body.String())
	}
	if *refreshes != 1 {
		t.Fatalf("refreshes = %d, want exactly one", *refreshes)
	}
	// The same session keeps working on the renewed token.
	if rec := board(srv, session); rec.Code != http.StatusOK {
		t.Fatalf("board on the renewed token: %d %s", rec.Code, rec.Body.String())
	}
	if *refreshes != 1 {
		t.Fatalf("a working token is not renewed again; refreshes = %d", *refreshes)
	}

	// The refresh is refused too: the session is dropped, the visitor signs in again.
	srv, refreshes = newServer(t, false)
	session = signInWithGitLab(t, srv)
	if rec := board(srv, session); rec.Code != http.StatusUnauthorized {
		t.Fatalf("board with an unrenewable token: %d %s (want 401)", rec.Code, rec.Body.String())
	}
	if *refreshes != 1 {
		t.Fatalf("refreshes = %d, want one attempt", *refreshes)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), `"authenticated":true`) {
		t.Fatalf("the session must be gone after the refresh was refused: %s", rec.Body.String())
	}
}

// Signing in with GitLab: the login redirect goes to the instance's own
// authorize endpoint with GitLab's scopes, the callback exchanges the code in
// GitLab's dialect, the session is the GitLab username, and the config tells
// the SPA which forge it is talking to. The person who signed in is known to
// the board's directory by name and avatar without a further call.
func TestGitLabSignInFlow(t *testing.T) {
	gl, forms := fakeGitLabForge(t)
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Forge:  forge.NewGitLab(gl.URL),
		Auth: &OAuthConfig{
			ClientID:     "gl-client",
			ClientSecret: "gl-secret",
			BaseURL:      testBaseURL,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Anonymous: the config names the forge so the SPA's copy is right.
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfg struct {
		Mode, Forge, ForgeLabel, CLI, Login string
		Authenticated                       bool
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "oauth" || cfg.Forge != "gitlab" || cfg.ForgeLabel != "GitLab" || cfg.CLI != "glab" || cfg.Authenticated {
		t.Fatalf("config before sign-in = %+v", cfg)
	}

	// /auth/login sends the browser to GitLab with GitLab's default scopes.
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login: %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	// GitLab refuses an authorize request without response_type (GitHub
	// never minded its absence, which is how it went missing).
	if !strings.HasPrefix(loc, gl.URL+"/oauth/authorize?") || !strings.Contains(loc, "scope=read_user+read_api+write_repository") ||
		!strings.Contains(loc, "response_type=code") {
		t.Fatalf("login redirect = %s", loc)
	}
	var state string
	for _, c := range rec.Result().Cookies() {
		if c.Name == stateCookie {
			state = c.Value
		}
	}
	if state == "" {
		t.Fatal("no state cookie")
	}

	// The callback: code exchanged in GitLab's dialect, session established.
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookie, Value: state})
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback: %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if len(*forms) != 1 || !strings.Contains((*forms)[0], "client_id=gl-client") || !strings.Contains((*forms)[0], "code=abc") {
		t.Fatalf("token forms = %v", *forms)
	}
	var session string
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("no session cookie")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Authenticated || cfg.Login != "kvaps" {
		t.Fatalf("config after sign-in = %+v", cfg)
	}
	// The sign-in taught the directory who kvaps is — no lookup needed.
	if m := srv.people.member("kvaps"); m.Name != "Andrei Kvapil" || m.AvatarURL != "https://gitlab.example/uploads/kvaps.png" {
		t.Fatalf("the signed-in person in the directory = %+v", m)
	}
}
