package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The no-project bucket is a full column with a working ×: removing a card
// from it sends project: "" — a pair the HTTP guard must let through (the
// column is named by its scope) all the way to the last-column outcome.
func TestRemoveFromANoProjectColumnOverHTTP(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 2\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: platform\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
		{Path: "cards/d/4/01JB4K2E7QZMX3R8V0N5T9WYD4.md", Data: []byte("---\ntitle: unbound\nteam: platform\nscope: Inbox\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	rights := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, remote)
	const uid = "01JB4K2E7QZMX3R8V0N5T9WYD4"

	rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/remove-from-project", `{"project":"","scope":"Inbox"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the no-project x must work: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("an untouched card in its last column is deleted: %d", rec.Code)
	}
	// The scope half stays required — and the refusal must name what THIS
	// endpoint requires: project is legally empty here, so a message
	// demanding both halves would send the caller fixing the wrong one.
	rec = doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/remove-from-project", `{"project":"x","scope":""}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty scope names no column: %d", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "project and scope are required") {
		t.Fatalf("the refusal must not demand the project this endpoint does not need: %s", body)
	}
}
