package server

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSessionEncryptRoundTrip(t *testing.T) {
	key := deriveSessionKey("a-strong-secret")
	in := map[string]persistedSession{
		"sid1": {Token: "gh-tok-1", Login: "alice", Created: time.Now().UTC().Truncate(time.Second)},
	}
	enc, err := encryptSessions(key, in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := decryptSessions(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if out["sid1"].Token != "gh-tok-1" || out["sid1"].Login != "alice" {
		t.Fatalf("round-trip lost data: %+v", out)
	}
	// A different key must not decrypt.
	if _, err := decryptSessions(deriveSessionKey("other-secret"), enc); err == nil {
		t.Fatal("a wrong key must fail to decrypt")
	}
}

func quietAuth(t *testing.T, cfg OAuthConfig) *authManager {
	t.Helper()
	cfg.ClientID, cfg.ClientSecret, cfg.BaseURL = "id", "sec", "https://aeman.test"
	return newAuthManager(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// With a SessionKey, sessions survive a restart (a fresh authManager over the
// same file); without the key they do not, and a wrong key drops them safely.
func TestSessionPersistenceAcrossRestart(t *testing.T) {
	path := t.TempDir() + "/sessions.json"

	a := quietAuth(t, OAuthConfig{SessionFile: path, SessionKey: "the-key"})
	a.mu.Lock()
	a.sessions["sid1"] = oauthSession{token: "gh-tok", login: "octocat", created: time.Now()}
	a.saveLocked()
	a.mu.Unlock()

	// Restart with the same key → session restored.
	b := quietAuth(t, OAuthConfig{SessionFile: path, SessionKey: "the-key"})
	if s, ok := b.sessions["sid1"]; !ok || s.token != "gh-tok" || s.login != "octocat" {
		t.Fatalf("session not restored with the right key: %+v", b.sessions)
	}

	// Restart with a wrong key → dropped, no crash.
	c := quietAuth(t, OAuthConfig{SessionFile: path, SessionKey: "wrong-key"})
	if _, ok := c.sessions["sid1"]; ok {
		t.Fatal("a wrong key must not restore sessions")
	}

	// Restart with no key → sessions are not persisted/restored.
	d := quietAuth(t, OAuthConfig{SessionFile: path})
	if _, ok := d.sessions["sid1"]; ok {
		t.Fatal("without a key sessions must stay in memory only")
	}
}

// Without a SessionKey the file must never contain a session token.
func TestNoKeyLeavesNoTokenOnDisk(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	a := quietAuth(t, OAuthConfig{SessionFile: path})
	a.mu.Lock()
	a.sessions["sid1"] = oauthSession{token: "secret-token-xyz", login: "octocat", created: time.Now()}
	a.saveLocked()
	a.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-token-xyz") {
		t.Fatalf("token leaked to disk without a key: %s", data)
	}
}
