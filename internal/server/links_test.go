package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// Link titles in git mode: a GitHub issue or PR referenced in a card's
// description resolves to its live title and state through the forge's REST
// API with the SERVER credential — the store keeps no per-visitor token. A
// PR's state is "merged" once merged; a reference the forge cannot show is
// left as it was; answers are cached; without a server credential nothing
// resolves and that is not an error the caller has to handle.

func fakeIssueForge(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer srv-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/app/issues/7":
			_, _ = w.Write([]byte(`{"title":"Login breaks on Safari","state":"open"}`))
		case "/repos/acme/app/issues/8":
			_, _ = w.Write([]byte(`{"title":"Fix the Safari login","state":"closed","pull_request":{"merged_at":"2026-08-20T10:00:00Z"}}`))
		case "/repos/acme/app/issues/9":
			_, _ = w.Write([]byte(`{"title":"Abandoned","state":"closed","pull_request":{"merged_at":null}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestForgeLinksResolveIssueAndPullState(t *testing.T) {
	var calls atomic.Int32
	forge := fakeIssueForge(t, &calls)
	fl := newForgeLinks(forge.URL, forge.Client(), "srv-token")
	issue, err := fl.ResolveIssueRef(context.Background(), board.Link{URL: "https://github.com/acme/app/issues/7", Kind: "issue", Owner: "acme", Repo: "app", Number: 7})
	if err != nil || issue.Title != "Login breaks on Safari" || issue.State != "open" {
		t.Fatalf("issue = %+v, %v", issue, err)
	}
	merged, err := fl.ResolveIssueRef(context.Background(), board.Link{URL: "https://github.com/acme/app/pull/8", Kind: "pull", Owner: "acme", Repo: "app", Number: 8})
	if err != nil || merged.Title != "Fix the Safari login" || merged.State != "merged" {
		t.Fatalf("merged pull = %+v, %v", merged, err)
	}
	closed, err := fl.ResolveIssueRef(context.Background(), board.Link{URL: "https://github.com/acme/app/pull/9", Kind: "pull", Owner: "acme", Repo: "app", Number: 9})
	if err != nil || closed.State != "closed" {
		t.Fatalf("closed pull = %+v, %v", closed, err)
	}
	// The link keeps everything it came with.
	if issue.URL == "" || issue.Kind != "issue" || issue.Owner != "acme" || issue.Repo != "app" || issue.Number != 7 {
		t.Fatalf("resolved link lost its identity: %+v", issue)
	}
	// Cached: the same reference again asks the forge nothing.
	before := calls.Load()
	if _, err := fl.ResolveIssueRef(context.Background(), board.Link{URL: "https://github.com/acme/app/issues/7", Kind: "issue", Owner: "acme", Repo: "app", Number: 7}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("a resolved reference was asked again within the TTL")
	}
}

func TestForgeLinksUnknownReferenceIsUnresolved(t *testing.T) {
	var calls atomic.Int32
	forge := fakeIssueForge(t, &calls)
	fl := newForgeLinks(forge.URL, forge.Client(), "srv-token")
	link := board.Link{URL: "https://github.com/acme/app/issues/404", Kind: "issue", Owner: "acme", Repo: "app", Number: 404}
	if _, err := fl.ResolveIssueRef(context.Background(), link); err == nil {
		t.Fatal("a reference the forge cannot show must stay unresolved (an error the service swallows)")
	}
	plain := board.Link{URL: "https://example.com/doc", Kind: "link"}
	if got, err := fl.ResolveIssueRef(context.Background(), plain); err != nil || got != plain {
		t.Fatalf("a plain link must pass through unchanged: %+v, %v", got, err)
	}
}

func TestForgeLinksWithoutServerCredentialResolveNothing(t *testing.T) {
	var calls atomic.Int32
	forge := fakeIssueForge(t, &calls)
	fl := newForgeLinks(forge.URL, forge.Client(), "")
	if _, err := fl.ResolveIssueRef(context.Background(), board.Link{URL: "https://github.com/acme/app/issues/7", Kind: "issue", Owner: "acme", Repo: "app", Number: 7}); err == nil {
		t.Fatal("without a credential nothing can be vouched for")
	}
	if calls.Load() != 0 {
		t.Fatal("no credential, no request")
	}
}

func TestForgeLinksCacheExpires(t *testing.T) {
	var calls atomic.Int32
	forge := fakeIssueForge(t, &calls)
	fl := newForgeLinks(forge.URL, forge.Client(), "srv-token")
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	fl.now = func() time.Time { return now }
	link := board.Link{URL: "https://github.com/acme/app/issues/7", Kind: "issue", Owner: "acme", Repo: "app", Number: 7}
	for range 2 {
		if _, err := fl.ResolveIssueRef(context.Background(), link); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("%d calls within the TTL, want 1", calls.Load())
	}
	now = now.Add(linksTTL + time.Second)
	if _, err := fl.ResolveIssueRef(context.Background(), link); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("%d calls after the TTL, want 2", calls.Load())
	}
	if !strings.HasPrefix(forge.URL, "http") {
		t.Fatal("sanity")
	}
}
