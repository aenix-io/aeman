package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/internal/forge"
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
	if !strings.HasPrefix(loc, gl.URL+"/oauth/authorize?") || !strings.Contains(loc, "scope=read_user+read_api+write_repository") {
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
