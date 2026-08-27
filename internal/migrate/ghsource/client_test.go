package ghsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordedReq struct {
	query string
	vars  map[string]any
}

// fakeGitHub is a stub GraphQL endpoint that records requests and dispatches to
// a handler returning the data payload (raw JSON) for each operation.
type fakeGitHub struct {
	reqs    []recordedReq
	respond func(query string, vars map[string]any) string
}

func newClientWithFake(t *testing.T, respond func(query string, vars map[string]any) string) (*Client, *fakeGitHub) {
	t.Helper()
	fake := &fakeGitHub{respond: respond}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		fake.reqs = append(fake.reqs, recordedReq{query: body.Query, vars: body.Variables})
		data := fake.respond(body.Query, body.Variables)
		if data == "" {
			data = "{}"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":` + data + `}`))
	}))
	t.Cleanup(srv.Close)
	return New("test-token", WithEndpoint(srv.URL)), fake
}

func TestLoadBoardNotFound(t *testing.T) {
	client, _ := newClientWithFake(t, func(query string, _ map[string]any) string {
		// Both org and user queries return null projects.
		if strings.Contains(query, "organization(login:") {
			return `{"organization":null}`
		}
		return `{"user":null}`
	})
	_, err := client.LoadBoard(context.Background(), "ghost", 99)
	if !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("err = %v, want ErrBoardNotFound", err)
	}
}

// TestLoadBoardOrgMissingProject covers a confirmed organization owner whose
// project number does not exist. It reproduces the REAL GitHub response shape:
// data with organization present but projectV2 null, AND a non-empty errors
// array carrying a NOT_FOUND type. The client must map this to ErrBoardNotFound
// without falling through to the user-project query (which would error on an
// org login and mask the clean not-found).
func TestLoadBoardOrgMissingProject(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		queries = append(queries, body.Query)
		// Exactly what api.github.com returns for a missing project on an org.
		_, _ = w.Write([]byte(`{"data":{"organization":{"projectV2":null}},` +
			`"errors":[{"type":"NOT_FOUND","path":["organization","projectV2"],` +
			`"message":"Could not resolve to a ProjectV2 with the number 999999."}]}`))
	}))
	t.Cleanup(srv.Close)

	client := New("test-token", WithEndpoint(srv.URL))
	_, err := client.LoadBoard(context.Background(), "acme", 999999)
	if !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("err = %v, want ErrBoardNotFound", err)
	}
	if len(queries) != 1 {
		t.Fatalf("issued %d queries, want only the org query", len(queries))
	}
	if strings.Contains(queries[0], "user(login:") {
		t.Fatalf("user query should not run for a confirmed org owner: %q", queries[0])
	}
}

// TestLoadBoardMissingOwnerNotFound covers an owner that is neither an org nor a
// user: both queries return null data with a NOT_FOUND error, which must map to
// ErrBoardNotFound, not an opaque upstream failure.
func TestLoadBoardMissingOwnerNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		field := "organization"
		if strings.Contains(body.Query, "user(login:") {
			field = "user"
		}
		_, _ = w.Write([]byte(`{"data":{"` + field + `":null},` +
			`"errors":[{"type":"NOT_FOUND","path":["` + field + `"],` +
			`"message":"Could not resolve to a ` + field + `."}]}`))
	}))
	t.Cleanup(srv.Close)

	client := New("test-token", WithEndpoint(srv.URL))
	_, err := client.LoadBoard(context.Background(), "nobody-xyz", 1)
	if !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("err = %v, want ErrBoardNotFound", err)
	}
}

// TestLoadBoardSurfacesNonNotFoundError ensures a genuine upstream failure (e.g.
// rate limiting) is not masked as a missing board.
func TestLoadBoardSurfacesNonNotFoundError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`))
	}))
	t.Cleanup(srv.Close)

	client := New("test-token", WithEndpoint(srv.URL))
	_, err := client.LoadBoard(context.Background(), "acme", 7)
	if err == nil || errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("err = %v, want a non-not-found error", err)
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("err = %v, want it to surface the rate-limit message", err)
	}
}

// TestLoadBoardPaginatesItems covers the items() pagination: a board larger than
// one GraphQL page must be loaded in full, following pageInfo.endCursor, so the
// newest cards do not silently fall off the migration.
func TestLoadBoardPaginatesItems(t *testing.T) {
	page1 := `{"organization":{"projectV2":{
	  "id":"PVT_1","number":7,"title":"Board","url":"u",
	  "fields":{"nodes":[]},
	  "items":{"pageInfo":{"hasNextPage":true,"endCursor":"CURSOR1"},"nodes":[
	    {"id":"I_P1","type":"DRAFT_ISSUE","content":{"__typename":"DraftIssue","id":"D1","title":"one","assignees":{"nodes":[]}},"fieldValues":{"nodes":[]}}
	  ]}
	}}}`
	page2 := `{"organization":{"projectV2":{
	  "id":"PVT_1","number":7,"title":"Board","url":"u",
	  "fields":{"nodes":[]},
	  "items":{"pageInfo":{"hasNextPage":false,"endCursor":null},"nodes":[
	    {"id":"I_P2","type":"DRAFT_ISSUE","content":{"__typename":"DraftIssue","id":"D2","title":"two","assignees":{"nodes":[]}},"fieldValues":{"nodes":[]}}
	  ]}
	}}}`
	client, fake := newClientWithFake(t, func(query string, vars map[string]any) string {
		if strings.Contains(query, "organization(login:") && strings.Contains(query, "projectV2(number:") {
			if vars["after"] == "CURSOR1" {
				return page2
			}
			return page1
		}
		return "{}"
	})

	b, err := client.LoadBoard(context.Background(), "acme", 7)
	if err != nil {
		t.Fatalf("LoadBoard: %v", err)
	}
	if len(b.Cards) != 2 {
		t.Fatalf("want 2 cards across 2 pages, got %d", len(b.Cards))
	}
	cardOf(t, b, "I_P1")
	cardOf(t, b, "I_P2")

	projectQueries := 0
	for _, r := range fake.reqs {
		if strings.Contains(r.query, "projectV2(number:") {
			projectQueries++
		}
	}
	if projectQueries != 2 {
		t.Fatalf("want 2 project page queries (first + one follow-up), got %d", projectQueries)
	}
}

// GitHub rejecting the caller's token is its own condition, not a generic
// upstream failure: no retry with that token will help, so the migration must
// be able to say so instead of reporting an opaque error.
func TestGraphQLBadCredentialsIsTyped(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
		}))
		var out struct{}
		err := New("tok", WithEndpoint(srv.URL)).graphql(context.Background(), `query{x}`, nil, &out)
		srv.Close()
		if !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("HTTP %d must be ErrBadCredentials, got %v", code, err)
		}
		if !strings.Contains(err.Error(), "Bad credentials") {
			t.Fatalf("the upstream message must survive for the log: %v", err)
		}
	}

	// An ordinary upstream failure stays untyped — it is retryable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	var out struct{}
	err := New("tok", WithEndpoint(srv.URL)).graphql(context.Background(), `query{x}`, nil, &out)
	if errors.Is(err, ErrBadCredentials) {
		t.Fatalf("a 502 is not a credentials problem: %v", err)
	}
}
