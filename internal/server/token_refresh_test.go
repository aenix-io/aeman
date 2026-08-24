package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A GitHub stub whose refresh tokens are single-use, like the real one's.
type ghStub struct {
	mu       sync.Mutex
	refreshN atomic.Int32
	valid    map[string]bool // refresh tokens still redeemable
}

func (g *ghStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			n := g.refreshN.Add(1)
			g.mu.Lock()
			ok := g.valid[r.Form.Get("refresh_token")]
			if ok {
				delete(g.valid, r.Form.Get("refresh_token")) // single-use
				g.valid[fmt.Sprintf("refresh-%d", n)] = true
			}
			g.mu.Unlock()
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_refresh_token"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fmt.Sprintf("gh-token-%d", n),
				"refresh_token": fmt.Sprintf("refresh-%d", n),
				"expires_in":    28800,
			})
			return
		}
		http.Error(w, "unexpected grant", http.StatusBadRequest)
	})
	return mux
}

// An expired GitHub token renews on the next session read — web or MCP — and
// two concurrent reads burn the single-use refresh token exactly once.
func TestSessionRenewsItsGitHubToken(t *testing.T) {
	stub := &ghStub{valid: map[string]bool{"refresh-0": true}}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	a := newAuthManager(OAuthConfig{ClientID: "id", ClientSecret: "sec", BaseURL: "http://localhost"}, slog.Default())
	a.tokenURL = srv.URL + "/token"
	a.client = srv.Client()

	a.mu.Lock()
	a.sessions["sid1"] = oauthSession{
		token: "gh-token-0", login: "kvaps", created: time.Now(),
		refresh: "refresh-0", tokenExpiry: time.Now().Add(-time.Minute), // already expired
	}
	a.mu.Unlock()

	var wg sync.WaitGroup
	tokens := make([]string, 4)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, ok := a.sessionByID(context.Background(), "sid1")
			if !ok {
				t.Errorf("reader %d lost the session", i)
				return
			}
			tokens[i] = s.token
		}(i)
	}
	wg.Wait()
	if n := stub.refreshN.Load(); n != 1 {
		t.Fatalf("the single-use refresh token was redeemed %d times", n)
	}
	for i, tok := range tokens {
		if tok != "gh-token-1" {
			t.Fatalf("reader %d got token %q, want the renewed one", i, tok)
		}
	}
	// The stored refresh token is the NEW one; the next expiry renews again.
	a.mu.Lock()
	s := a.sessions["sid1"]
	s.tokenExpiry = time.Now().Add(-time.Second)
	a.sessions["sid1"] = s
	a.mu.Unlock()
	if s.refresh != "refresh-1" {
		t.Fatalf("the replacement refresh token was not kept: %q", s.refresh)
	}
	if got, ok := a.sessionByID(context.Background(), "sid1"); !ok || got.token != "gh-token-2" {
		t.Fatalf("second renewal: %v %q", ok, got.token)
	}
}

// A renewal failure while the access token is still alive must not sign the
// user out — a network blip is not a logout.
func TestRenewalFailureKeepsALiveToken(t *testing.T) {
	stub := &ghStub{valid: map[string]bool{}} // every refresh is refused
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	a := newAuthManager(OAuthConfig{ClientID: "id", ClientSecret: "sec", BaseURL: "http://localhost"}, slog.Default())
	a.tokenURL = srv.URL + "/token"
	a.client = srv.Client()
	a.mu.Lock()
	a.sessions["sid1"] = oauthSession{
		token: "gh-token-0", login: "kvaps", created: time.Now(),
		refresh: "refresh-dead", tokenExpiry: time.Now().Add(5 * time.Minute), // inside renew-ahead, still valid
	}
	a.mu.Unlock()
	s, ok := a.sessionByID(context.Background(), "sid1")
	if !ok || s.token != "gh-token-0" {
		t.Fatalf("a failed renewal dropped a session whose token still works: %v %q", ok, s.token)
	}
	// Once the token is dead too, the session goes.
	a.mu.Lock()
	cur := a.sessions["sid1"]
	cur.tokenExpiry = time.Now().Add(-time.Minute)
	a.sessions["sid1"] = cur
	a.mu.Unlock()
	if _, ok := a.sessionByID(context.Background(), "sid1"); ok {
		t.Fatal("a session with a dead token and a dead refresh token must end")
	}
}
