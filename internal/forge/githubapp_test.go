package forge

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A GitHub App is the server credential without a PAT: the server signs a
// short-lived JWT with the app's private key, asks which installation the
// repository belongs to, and mints an installation token — scoped to the
// repositories the app is installed on, expiring within the hour, renewed
// by the server itself. Nothing to issue by hand, nothing that quietly
// expires in a .env file.

// testAppKey is one RSA key per test binary — generation is the slow part.
var testAppKey = func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}()

func testAppPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testAppKey)})
}

// fakeAppAPI answers the two App endpoints: which installation a repository
// belongs to, and a token for an installation. Every request must carry a
// JWT signed by the app's key with the app id as the issuer — the fake
// verifies the signature for real, so a broken signer cannot pass.
func fakeAppAPI(t *testing.T, mints *atomic.Int32, expiry time.Duration) *httptest.Server {
	t.Helper()
	installations := map[string]int{"acme/shared": 11, "acme/closed": 11, "beta/other": 22}
	mux := http.NewServeMux()
	checkJWT := func(r *http.Request) error {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		parts := strings.Split(raw, ".")
		if len(parts) != 3 {
			return fmt.Errorf("not a JWT: %q", raw)
		}
		sig, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if err := rsa.VerifyPKCS1v15(&testAppKey.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
			return fmt.Errorf("signature: %w", err)
		}
		body, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return err
		}
		var claims struct {
			Iss string `json:"iss"`
			Exp int64  `json:"exp"`
			Iat int64  `json:"iat"`
		}
		if err := json.Unmarshal(body, &claims); err != nil {
			return err
		}
		if claims.Iss != "12345" {
			return fmt.Errorf("iss = %q, want the app id", claims.Iss)
		}
		// The claims' window follows the app's clock, which the refresh
		// tests wind forward — only its shape is checked here.
		if claims.Iat == 0 || claims.Exp <= claims.Iat {
			return fmt.Errorf("claims out of shape: iat %d exp %d", claims.Iat, claims.Exp)
		}
		return nil
	}
	mux.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) {
		if err := checkJWT(r); err != nil {
			t.Errorf("app lookup: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"slug": "aenix-aeman", "html_url": "https://github.com/apps/aenix-aeman"})
	})
	mux.HandleFunc("GET /repos/", func(w http.ResponseWriter, r *http.Request) {
		if err := checkJWT(r); err != nil {
			t.Errorf("installation lookup: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/"), "/installation")
		id, ok := installations[slug]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"id": id})
	})
	mux.HandleFunc("POST /app/installations/", func(w http.ResponseWriter, r *http.Request) {
		if err := checkJWT(r); err != nil {
			t.Errorf("token mint: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		n := mints.Add(1)
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/app/installations/"), "/access_tokens")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":      fmt.Sprintf("ghs_%s_%d", id, n),
			"expires_at": time.Now().Add(expiry).UTC().Format(time.RFC3339),
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAppMintsInstallationTokensPerRepository(t *testing.T) {
	var mints atomic.Int32
	srv := fakeAppAPI(t, &mints, time.Hour)
	app, err := NewGitHubAppAt(srv.URL, srv.Client(), "12345", testAppPEM())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tok, err := app.Token(ctx, "https://github.com/acme/shared.git")
	if err != nil || !strings.HasPrefix(tok, "ghs_11_") {
		t.Fatalf("token = %q, %v; want installation 11's", tok, err)
	}
	// The same installation answers again from the cache — for this
	// repository and for a sibling installed alongside it.
	again, err := app.Token(ctx, "https://github.com/acme/shared.git")
	if err != nil || again != tok {
		t.Fatalf("second call = %q, %v; want the cached %q", again, err, tok)
	}
	sibling, err := app.Token(ctx, "https://github.com/acme/closed")
	if err != nil || sibling != tok {
		t.Fatalf("sibling repo = %q, %v; want the same installation token", sibling, err)
	}
	if n := mints.Load(); n != 1 {
		t.Fatalf("%d mints, want 1", n)
	}
	// Another installation is its own token.
	other, err := app.Token(ctx, "https://github.com/beta/other.git")
	if err != nil || !strings.HasPrefix(other, "ghs_22_") {
		t.Fatalf("other installation = %q, %v", other, err)
	}
	// A repository the app is not installed on is a plain answer carrying
	// the very link that fixes it — nobody should have to construct an
	// install URL by hand from an app id.
	_, err = app.Token(ctx, "https://github.com/no/where.git")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("uninstalled repo: %v, want a 'not installed' error", err)
	}
	if !strings.Contains(err.Error(), "https://github.com/apps/aenix-aeman/installations/new") {
		t.Fatalf("the refusal must carry the install link: %v", err)
	}
}

// An installation token lives about an hour; the app renews it before it
// runs out, so a git push never carries a token about to die mid-request.
func TestAppRefreshesAnExpiringToken(t *testing.T) {
	var mints atomic.Int32
	srv := fakeAppAPI(t, &mints, time.Hour)
	app, err := NewGitHubAppAt(srv.URL, srv.Client(), "12345", testAppPEM())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := app.Token(ctx, "https://github.com/acme/shared.git")
	if err != nil {
		t.Fatal(err)
	}
	// Wind the app's clock to five minutes before the expiry: the cached
	// token is no longer trusted for a whole git push, so a fresh one is
	// minted.
	app.now = func() time.Time { return time.Now().Add(time.Hour - 4*time.Minute) }
	second, err := app.Token(ctx, "https://github.com/acme/shared.git")
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("an expiring token was served instead of a fresh one")
	}
	if n := mints.Load(); n != 2 {
		t.Fatalf("%d mints, want 2", n)
	}
}

// The key is validated when the app is configured — a broken PEM must stop
// the server at startup, not the first push an hour in.
func TestAppRefusesABrokenKey(t *testing.T) {
	if _, err := NewGitHubApp("12345", []byte("not a key")); err == nil {
		t.Fatal("a broken key must be refused at construction")
	}
	if _, err := NewGitHubApp("", testAppPEM()); err == nil {
		t.Fatal("an empty app id must be refused")
	}
}

// The git transport asks the credential per request, so a token renewed
// between two pushes is picked up without anybody re-wiring the remote.
func TestAppGitAuthStampsAFreshTokenPerRequest(t *testing.T) {
	var mints atomic.Int32
	srv := fakeAppAPI(t, &mints, time.Hour)
	app, err := NewGitHubAppAt(srv.URL, srv.Client(), "12345", testAppPEM())
	if err != nil {
		t.Fatal(err)
	}
	auth := app.GitAuthFor("https://github.com/acme/shared.git")
	req := httptest.NewRequest(http.MethodGet, "https://github.com/acme/shared.git/info/refs", nil)
	auth.SetAuth(req)
	user, pass, ok := req.BasicAuth()
	if !ok || user != "x-access-token" || !strings.HasPrefix(pass, "ghs_11_") {
		t.Fatalf("basic auth = %q/%q ok=%v", user, pass, ok)
	}
	// The token rotates; the next request carries the new one.
	app.now = func() time.Time { return time.Now().Add(time.Hour - 4*time.Minute) }
	req2 := httptest.NewRequest(http.MethodGet, "https://github.com/acme/shared.git/info/refs", nil)
	auth.SetAuth(req2)
	if _, pass2, _ := req2.BasicAuth(); pass2 == pass {
		t.Fatal("the rotated token did not reach the request")
	}
}

// A token minted for a person by a GitHub App reaches only the repositories
// the app is installed on — which is why a refusal about someone's own
// repository has to be read as "not installed there", not "no access".
// GitHub says which kind a token is in its prefix.
func TestAUserToServerTokenIsToldApartByItsPrefix(t *testing.T) {
	cases := map[string]bool{
		"ghu_abc": true,  // user-to-server: a GitHub App acting for a person
		"gho_abc": false, // an OAuth App's token: the person's whole account
		"ghp_abc": false, // a classic personal access token
		"ghs_abc": false, // an installation token — the server's own
		"":        false,
	}
	for token, want := range cases {
		if got := IsUserToServerToken(token); got != want {
			t.Errorf("IsUserToServerToken(%q) = %v, want %v", token, got, want)
		}
	}
}

// InstallURL is the page where the app is installed on an account — what a
// refusal points at. It is the app's own page, asked from the forge.
func TestInstallURLNamesTheAppsPage(t *testing.T) {
	var mints atomic.Int32
	srv := fakeAppAPI(t, &mints, time.Hour)
	app, err := NewGitHubAppAt(srv.URL, srv.Client(), "12345", testAppPEM())
	if err != nil {
		t.Fatal(err)
	}
	if got := app.InstallURL(context.Background()); got != "https://github.com/apps/aenix-aeman/installations/new" {
		t.Fatalf("InstallURL = %q", got)
	}
}
