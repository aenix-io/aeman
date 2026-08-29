package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The mirror actions end to end over the git store: mirror a card into a
// second column, see both placements on the resource, remove it from one
// column and watch the home hand over — the whole trip through HTTP, the
// service, the store and the repository files.
func TestMirrorActionsOverTheGitStore(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: platform\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
		{Path: gitstore.ProjectPath("01JB4PROJE"), Data: []byte("name: engineering\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.ProjectPath("01JB4PROJF"), Data: []byte("name: freedom\nrank: b\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.EpicPath("01JB4PROJE", "01JB4EPICC"), Data: []byte("name: Cozystack\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.EpicPath("01JB4PROJF", "01JB4EPICL"), Data: []byte("name: Launch\nrank: b\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: "cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md", Data: []byte("---\ntitle: shared work\nteam: platform\nproject: engineering\nepic: Cozystack\nstart: 2026-08-24\nday: 2026-08-28\nweek: 2026-08-24\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	rights := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, remote)
	const uid = "01JB4K2E7QZMX3R8V0N5T9WYA1"

	rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/mirror", `{"project":"freedom","epic":"Launch"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("mirror: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, "")
	var got struct {
		Spec struct {
			Project string `json:"project"`
			Epic    string `json:"epic"`
			Mirrors []struct {
				Project string `json:"project"`
				Epic    string `json:"epic"`
			} `json:"mirrors"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Spec.Mirrors) != 1 || got.Spec.Mirrors[0].Project != "freedom" || got.Spec.Mirrors[0].Epic != "Launch" {
		t.Fatalf("the resource carries the mirror: %s", rec.Body.String())
	}

	// The guards land as 422, the way a refused user input must — a card
	// without a column mirrored, or mirrored onto its own column, is the
	// caller's mistake, not a gateway failure.
	rec = doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/mirror", `{"project":"engineering","epic":"Cozystack"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mirroring the card onto its own column must be a 422, got %d %s", rec.Code, rec.Body.String())
	}

	// A half-named column is refused: the pair is the identity.
	if rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/mirror", `{"project":"freedom"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("half a column: %d", rec.Code)
	}

	// Remove from the home column: the mirror is promoted to the home.
	rec = doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/remove-from-project", `{"project":"engineering","epic":"Cozystack"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove-from-project: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, "")
	got.Spec.Mirrors = nil // Unmarshal leaves an absent key's old value in place
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Project != "freedom" || got.Spec.Epic != "Launch" || len(got.Spec.Mirrors) != 0 {
		t.Fatalf("the first mirror becomes the home: %s", rec.Body.String())
	}

	// And removing the last column deletes an untouched card outright.
	rec = doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/remove-from-project", `{"project":"freedom","epic":"Launch"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("last column: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("an untouched card with no other column is gone: %d", rec.Code)
	}
}
