package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// A project card lives and dies in its column: there is no door that takes
// the column away. The × that once removed a card from its project is gone
// — the path answers like any unknown route — and a patch that empties the
// scope is refused with the project-card problem, so the only way out of a
// column is to delete the card.
func TestAProjectCardLivesAndDiesInItsColumnOverHTTP(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 2\ntitle: t\n")},
		{Path: "cards/d/4/01JB4K2E7QZMX3R8V0N5T9WYD4.md", Data: []byte("---\ntitle: a plan\nscope: Inbox\nprogress: 40\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")},
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
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the remove-from-project door is gone: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAs(t, srv, "kvaps", "PATCH", "/api/v1/cards/"+uid, `{"scope":""}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("emptying a project card's scope is refused: %d %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"projectCard"`) {
		t.Fatalf("the refusal is the project card's: %s", body)
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"scope":"Inbox"`) {
		t.Fatalf("the card still stands in its column: %d %s", rec.Code, rec.Body.String())
	}
}
