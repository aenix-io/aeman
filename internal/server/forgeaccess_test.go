package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The forge adapter: GitHub's repository permissions block, read with the
// visitor's token, decides read (pull) and write (push or better) per
// domain; a repository the visitor cannot see is a 404 and means neither;
// the answer is cached per visitor for the TTL, and a stale answer stands in
// while the forge is unreachable.

func fakeForge(t *testing.T, perms map[string]map[string]bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer tok-alice" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		slug := r.URL.Path[len("/repos/"):]
		p, ok := perms[slug]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"permissions":{"admin":` + b(p["admin"]) + `,"push":` + b(p["push"]) + `,"pull":` + b(p["pull"]) + `}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func b(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestForgeAccessReadsPermissionsPerDomain(t *testing.T) {
	var calls atomic.Int32
	forge := fakeForge(t, map[string]map[string]bool{
		"acme/shared": {"pull": true, "push": true},
		"acme/closed": {"pull": true},
	}, &calls)
	fa := newForgeAccess(forge.URL, forge.Client(), []RepoSpec{
		{Name: "shared", URL: "https://github.com/acme/shared.git"},
		{Name: "closed", URL: "git@github.com:acme/closed.git"},
		{Name: "hidden", URL: "https://github.com/acme/hidden"},
	})
	r, err := fa.rights(context.Background(), "tok-alice", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if r.primary != "shared" || !r.canRead("shared") || !r.canWrite("shared") {
		t.Fatalf("shared rights = %+v", r)
	}
	if !r.canRead("closed") || r.canWrite("closed") {
		t.Fatal("pull-only must read and not write")
	}
	if r.canRead("hidden") || r.canWrite("hidden") {
		t.Fatal("a 404 repository must be neither readable nor writable")
	}
	if !r.canRead("") {
		t.Fatal(`"" must name the primary`)
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("%d forge calls, want one per domain", n)
	}
}

func TestForgeAccessCachesPerVisitorAndSurvivesOutage(t *testing.T) {
	var calls atomic.Int32
	forge := fakeForge(t, map[string]map[string]bool{"acme/shared": {"pull": true, "push": true}}, &calls)
	fa := newForgeAccess(forge.URL, forge.Client(), []RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared"}})
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	fa.now = func() time.Time { return now }
	if _, err := fa.rights(context.Background(), "tok-alice", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := fa.rights(context.Background(), "tok-alice", "alice"); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d forge calls within the TTL, want 1", n)
	}
	// Past the TTL the forge is asked again.
	now = now.Add(accessTTL + time.Second)
	if _, err := fa.rights(context.Background(), "tok-alice", "alice"); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("%d forge calls after the TTL, want 2", n)
	}
	// The forge goes away: the stale answer stands for a known visitor, a
	// new visitor gets the error.
	forge.Close()
	now = now.Add(accessTTL + time.Second)
	r, err := fa.rights(context.Background(), "tok-alice", "alice")
	if err != nil || !r.canWrite("shared") {
		t.Fatalf("stale answer during an outage: %+v, %v", r, err)
	}
	if _, err := fa.rights(context.Background(), "tok-bob", "bob"); err == nil {
		t.Fatal("an unknown visitor during an outage must not be admitted")
	}
}

func TestForgeAccessRefusesABadToken(t *testing.T) {
	var calls atomic.Int32
	forge := fakeForge(t, map[string]map[string]bool{"acme/shared": {"pull": true}}, &calls)
	fa := newForgeAccess(forge.URL, forge.Client(), []RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared"}})
	if _, err := fa.rights(context.Background(), "tok-expired", "alice"); err == nil {
		t.Fatal("a rejected token must be an error, not an empty right set")
	}
}

func TestRepoSlugForms(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/shared.git":  "acme/shared",
		"https://github.com/acme/shared":      "acme/shared",
		"https://github.com/acme/shared/":     "acme/shared",
		"git@github.com:acme/shared.git":      "acme/shared",
		"ssh://git@github.com/acme/shared":    "acme/shared",
		"https://gitlab.example/grp/sub/repo": "sub/repo",
		"acme/shared":                         "acme/shared",
	} {
		got, err := repoSlug(in)
		if err != nil || got != want {
			t.Errorf("repoSlug(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "https://github.com/acme", "shared"} {
		if _, err := repoSlug(bad); err == nil {
			t.Errorf("repoSlug(%q) must fail", bad)
		}
	}
}
