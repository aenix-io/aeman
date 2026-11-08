package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The Project board is removed (H15): its routes, its board and its roster
// are no part of the surface. A client that still asks is told the route is
// not there — the same answer as any path nothing serves — rather than
// handed an empty Project board.
func TestTheRetiredProjectRoutesAreGone(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Team: "test"}}, map[string]board.SprintState{
		"test": {Current: "2026-08-24", ItemID: "st1"},
	})
	srv := apiServer(t, Options{}, fake)
	for _, tc := range []struct{ target, body string }{
		{"/api/v1/projects", `{"name":"engineering"}`},
		{"/api/v1/projects/actions/rename", `{"project":"engineering","to":"x"}`},
		{"/api/v1/projects/actions/delete-project", `{"project":"engineering"}`},
		{"/api/v1/projects/actions/reorder-projects", `{"projects":["engineering"]}`},
		{"/api/v1/scopes", `{"name":"Launch","project":"engineering"}`},
		{"/api/v1/scopes/actions/rename", `{"project":"engineering","scope":"Launch","to":"Ship"}`},
		{"/api/v1/scopes/actions/set-project", `{"scope":"Launch","project":"x"}`},
		{"/api/v1/scopes/actions/delete-scope", `{"scope":"Launch"}`},
		{"/api/v1/scopes/actions/reorder-scopes", `{"scopes":["Launch"]}`},
		{"/api/v1/deadlines", `{"week":"2026-08-24","project":"engineering"}`},
		{"/api/v1/deadlines/actions/move", `{"from":"2026-08-24","to":"2026-08-31"}`},
		{"/api/v1/deadlines/actions/delete", `{"week":"2026-08-24"}`},
	} {
		rec := do(t, srv, http.MethodPost, tc.target, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("POST %s = %d, want 404 (%s)", tc.target, rec.Code, rec.Body.String())
		}
		if p := decodeProblem(t, rec); p.Code != "unknownRoute" {
			t.Fatalf("POST %s code = %q, want unknownRoute", tc.target, p.Code)
		}
	}
	// The board itself is a board nobody has.
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/v1/views/project/cards", ""},
		{http.MethodPost, "/api/v1/views/project/cards", `{"title":"a slot"}`},
	} {
		rec := do(t, srv, tc.method, tc.target, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404 (%s)", tc.method, tc.target, rec.Code, rec.Body.String())
		}
		if p := decodeProblem(t, rec); p.Code != "noSuchView" {
			t.Fatalf("%s %s code = %q, want noSuchView", tc.method, tc.target, p.Code)
		}
	}
	// And the board's metadata names no project roster at all.
	rec := do(t, srv, http.MethodGet, "/api/v1/board", "")
	var out struct {
		Metadata map[string]json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	for _, gone := range []string{"projects", "scopes", "deadlines", "projectDomains"} {
		if _, ok := out.Metadata[gone]; ok {
			t.Errorf("metadata still carries %q: %s", gone, rec.Body.String())
		}
	}
}

// A repository may still hold what the Project board wrote — a card in a
// column, the projects/ roster — and the owner chose to leave it there (H15).
// The server serves none of it: the card is on no listing and its address is
// a card nobody has, a write to it is refused the same way, and after the
// board has been written to and synced every one of those files is on the
// remote byte for byte as it was.
func TestTheServerServesNoProjectBoardFiles(t *testing.T) {
	const slot = "01JB4K2E7QZMX3R8V0N5T9WYD4"
	slotPath, err := gitstore.CardPath(slot)
	if err != nil {
		t.Fatal(err)
	}
	retired := map[string]string{
		slotPath:                              "---\ntitle: a plan\nscope: Inbox\nproject: portal\nprogress: 40\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
		"projects/01JB4PROJ/project.yaml":     "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"projects/01JB4PROJ/scopes/01S.yaml":  "name: Inbox\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"projects/01JB4PROJ/deadlines/1.yaml": "week: 2026-09-07\ncreated: 2026-06-01T08:00:00Z\n",
	}
	files := map[string]string{
		gitstore.BoardPath:                        "schema: 3\ntitle: t\n",
		gitstore.TeamPath("_"):                    "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		gitstore.TeamPath("01JB4TEAM"):            "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		"cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md": "---\ntitle: one\nteam: portal\nzone: yellow\nprogress: 40\nrank: b\ncreated: 2026-08-26T09:14:03Z\n---\n",
	}
	for p, data := range retired {
		files[p] = data
	}
	remote := gitRemoteN(t, "board")
	seedRemoteFiles(t, remote, files)
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{
		"kvaps": rightsOn([]string{"shared"}, []string{"shared"}),
	}}, remote)

	// The team boards are asked for the no-team group, where a teamless
	// card would stand; the others for every team.
	for _, view := range []string{"all", "me", "team?team=", "backlog?team=", "strategy"} {
		path, query, _ := strings.Cut(view, "?")
		rec := doAs(t, srv, "kvaps", http.MethodGet, "/api/v1/views/"+path+"/cards?"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("view=%s: %d %s", view, rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); strings.Contains(body, slot) || strings.Contains(body, "a plan") {
			t.Fatalf("view=%s serves the project card: %s", view, body)
		}
	}
	for _, tc := range []struct{ method, body string }{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"progress":100}`},
		{http.MethodDelete, ""},
	} {
		if rec := doAs(t, srv, "kvaps", tc.method, "/api/v1/cards/"+slot, tc.body); rec.Code != http.StatusNotFound {
			t.Fatalf("%s the project card: %d %s, want 404", tc.method, rec.Code, rec.Body.String())
		}
	}

	// A write to the board, and a sync that pushes it.
	uid := cardUID(t, srv, "kvaps", "one")
	if rec := doAs(t, srv, "kvaps", http.MethodPatch, "/api/v1/cards/"+uid, `{"progress":90}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	if err := srv.drainAndPush(ctx); err != nil {
		t.Fatal(err)
	}
	check, err := gitstore.Clone(ctx, memory.NewStorage(), remote, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if one, err := check.ReadFile("cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md"); err != nil || !strings.Contains(string(one), "progress: 90") {
		t.Fatalf("the write did not reach the remote: %q (%v)", one, err)
	}
	for p, want := range retired {
		got, err := check.ReadFile(p)
		if err != nil {
			t.Fatalf("%s is gone from the remote: %v", p, err)
		}
		if !bytes.Equal(got, []byte(want)) {
			t.Fatalf("%s was rewritten:\n%s\nwant\n%s", p, got, want)
		}
	}
}
