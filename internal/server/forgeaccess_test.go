package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	forgepkg "github.com/aenix-io/aeman/internal/forge"
)

// The forge adapter: GitHub's repository permissions block, read with the
// visitor's token, decides read (pull) and write (push or better) per
// domain; a repository the visitor cannot see is a 404 and means neither;
// the answer is cached per visitor for the TTL, and a stale answer stands in
// while the forge is unreachable.

// collaborators is what the forge answers for
// /repos/{slug}/collaborators/{login}/permission, asked with the server
// token: login → permission.
var collaborators = map[string]string{"alice": "write", "bob": "read", "carol": "none"}

func fakeForge(t *testing.T, perms map[string]map[string]bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if i := strings.Index(r.URL.Path, "/collaborators/"); i >= 0 {
			if r.Header.Get("Authorization") != "Bearer srv-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			login := strings.TrimSuffix(r.URL.Path[i+len("/collaborators/"):], "/permission")
			p, ok := collaborators[login]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"permission":"` + p + `"}`))
			return
		}
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
	fa := newForgeAccess(forgepkg.NewGitHubAt(forge.URL), forge.Client(), []RepoSpec{
		{Name: "shared", URL: "https://github.com/acme/shared.git"},
		{Name: "closed", URL: "git@github.com:acme/closed.git"},
		{Name: "hidden", URL: "https://github.com/acme/hidden"},
	}, "srv-token", nil)
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
	fa := newForgeAccess(forgepkg.NewGitHubAt(forge.URL), forge.Client(), []RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared"}}, "srv-token", nil)
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
	fa := newForgeAccess(forgepkg.NewGitHubAt(forge.URL), forge.Client(), []RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared"}}, "srv-token", nil)
	if _, err := fa.rights(context.Background(), "tok-expired", "alice"); err == nil {
		t.Fatal("a rejected token must be an error, not an empty right set")
	}
}

// G16 — who can read a domain is the forge's collaborator permission for
// each board member, asked with the server token: any permission but "none"
// reads; someone the forge does not know is left out; answers are cached.
func TestForgeAccessReadersByCollaboratorPermission(t *testing.T) {
	var calls atomic.Int32
	forge := fakeForge(t, map[string]map[string]bool{"acme/shared": {"pull": true}}, &calls)
	fa := newForgeAccess(forgepkg.NewGitHubAt(forge.URL), forge.Client(), []RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared"}}, "srv-token", nil)
	got, err := fa.readers(context.Background(), "shared", []string{"alice", "bob", "carol", "stranger"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "alice,bob" {
		t.Fatalf("readers = %v, want alice,bob (write and read; none and unknown left out)", got)
	}
	if _, err := fa.readers(context.Background(), "shared", []string{"alice", "bob", "carol", "stranger"}); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 4 {
		t.Fatalf("%d forge calls, want 4 (one per login, then cached)", n)
	}
	if _, err := fa.readers(context.Background(), "nope", []string{"alice"}); err == nil {
		t.Fatal("an unknown domain must be an error")
	}
	// No server credential: nobody can be vouched for, and that is not an error.
	bare := newForgeAccess(forgepkg.NewGitHubAt(forge.URL), forge.Client(), []RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared"}}, "", nil)
	if got, err := bare.readers(context.Background(), "shared", []string{"alice"}); err != nil || len(got) != 0 {
		t.Fatalf("without a server token: %v, %v", got, err)
	}
}
