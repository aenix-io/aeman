package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
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
	// The action answers with the card resource, like the other card
	// actions — an MCP or external caller sees the new placement without a
	// second request.
	if body := rec.Body.String(); !strings.Contains(body, `"mirrors"`) {
		t.Fatalf("the mirror action must answer with the card resource: %s", body)
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

// PATCHing process must round-trip: the write lands as Card.Process, and the
// resource must serve it back — before this test, processOf answered only
// for process TURNS (cards carrying a task id), so an ordinary card's tie
// vanished on the next GET: the picker flickered, a reload erased the
// choice, and the current process never dropped out of the targets list.
func TestProcessPatchRoundTripsOverTheGitStore(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: platform\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
		{Path: gitstore.ProcessPath("01JB4PROC1"), Data: []byte("name: Invoicing\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: "cards/c/3/01JB4K2E7QZMX3R8V0N5T9WYC3.md", Data: []byte("---\ntitle: weekly chore\nteam: platform\nstage: recurrent\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	rights := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, remote)
	const uid = "01JB4K2E7QZMX3R8V0N5T9WYC3"

	spec := func() string {
		rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, "")
		var got struct {
			Spec struct {
				Process string `json:"process"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.Spec.Process
	}
	if rec := doAs(t, srv, "kvaps", "PATCH", "/api/v1/cards/"+uid, `{"process":"Invoicing"}`); rec.Code != http.StatusOK {
		t.Fatalf("patch process: %d %s", rec.Code, rec.Body.String())
	}
	if got := spec(); got != "Invoicing" {
		t.Fatalf("the tie must survive the round trip, spec.process = %q", got)
	}
	// "" unties, and the resource says so.
	if rec := doAs(t, srv, "kvaps", "PATCH", "/api/v1/cards/"+uid, `{"process":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clear process: %d %s", rec.Code, rec.Body.String())
	}
	if got := spec(); got != "" {
		t.Fatalf("clearing must round-trip too, spec.process = %q", got)
	}
}

// The no-project bucket is a full column with a working ×: removing a card
// from it sends project: "" — a pair the HTTP guard must let through (the
// column is named by its epic) all the way to the last-column outcome.
func TestRemoveFromANoProjectColumnOverHTTP(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: platform\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
		{Path: "cards/d/4/01JB4K2E7QZMX3R8V0N5T9WYD4.md", Data: []byte("---\ntitle: unbound\nteam: platform\nepic: Inbox\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")},
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

	rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/remove-from-project", `{"project":"","epic":"Inbox"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("the no-project x must work: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("an untouched card in its last column is deleted: %d", rec.Code)
	}
	// The epic half stays required — and the refusal must name what THIS
	// endpoint requires: project is legally empty here, so a message
	// demanding both halves would send the caller fixing the wrong one.
	rec = doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/remove-from-project", `{"project":"x","epic":""}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty epic names no column: %d", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "project and epic are required") {
		t.Fatalf("the refusal must not demand the project this endpoint does not need: %s", body)
	}
}

// The queued write's description follows its direction: the sync log must
// not report an unmirror as "mirror".
func TestMirrorsDescTellsAddsFromRemovals(t *testing.T) {
	card := board.Card{ItemID: "c1", Title: "shared",
		Mirrors: []board.Placement{{Project: "freedom", Epic: "Launch"}}}
	if got := mirrorsDesc(card, nil); !strings.HasPrefix(got, "unmirror ") {
		t.Fatalf("a shrinking list is an unmirror: %q", got)
	}
	if got := mirrorsDesc(card, append(card.Mirrors, board.Placement{Project: "freedom", Epic: "Ship"})); !strings.HasPrefix(got, "mirror ") {
		t.Fatalf("a growing list is a mirror: %q", got)
	}
	// A same-length replacement is a rename's rewrite, not an addition.
	if got := mirrorsDesc(card, []board.Placement{{Project: "freedom", Epic: "Liftoff"}}); !strings.HasPrefix(got, "rewrite ") {
		t.Fatalf("a same-length list is a rewrite: %q", got)
	}
	// The tie's description follows its direction the same way.
	if got := tieDesc(card, "Invoicing"); !strings.HasPrefix(got, "tie ") {
		t.Fatalf("a tie is a tie: %q", got)
	}
	if got := tieDesc(card, ""); !strings.HasPrefix(got, "untie ") {
		t.Fatalf("clearing is an untie, not a tie: %q", got)
	}
}

// PATCH may carry parent and process together — grouping clears the tie,
// so the process half must not re-tie the fresh subtask in the same
// request: the service refuses it, and the whole PATCH answers 422.
func TestGroupingAndTyingInOnePatchIsRefused(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: platform\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
		{Path: gitstore.ProcessPath("01JB4PROC1"), Data: []byte("name: Invoicing\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: "cards/e/5/01JB4K2E7QZMX3R8V0N5T9WYE5.md", Data: []byte("---\ntitle: parent\nteam: platform\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")},
		{Path: "cards/f/6/01JB4K2E7QZMX3R8V0N5T9WYF6.md", Data: []byte("---\ntitle: child\nteam: platform\nstage: recurrent\nrank: b\ncreated: 2026-08-20T09:00:00Z\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	rights := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, remote)

	rec := doAs(t, srv, "kvaps", "PATCH", "/api/v1/cards/01JB4K2E7QZMX3R8V0N5T9WYF6",
		`{"parent":"01JB4K2E7QZMX3R8V0N5T9WYE5","process":"Invoicing"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the re-tie of a fresh subtask must be refused: %d %s", rec.Code, rec.Body.String())
	}
}

// A hand-written mirror onto a column nobody declared must not be
// honoured: the board assembly drops it, so the resource never shows it
// and the x's promotion never re-files the card into a ghost pair — the
// last column simply behaves as the last column.
func TestAHandWrittenGhostMirrorNeverPromotes(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: platform\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
		{Path: gitstore.ProjectPath("01JB4PROJE"), Data: []byte("name: engineering\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.EpicPath("01JB4PROJE", "01JB4EPICC"), Data: []byte("name: Cozystack\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: "cards/g/7/01JB4K2E7QZMX3R8V0N5T9WYG7.md", Data: []byte("---\ntitle: hand-edited\nteam: platform\nproject: engineering\nepic: Cozystack\nmirrors:\n  - project: engineering\n    epic: Ghost\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	rights := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, remote)
	const uid = "01JB4K2E7QZMX3R8V0N5T9WYG7"

	// The ghost is silently corrected, never served.
	rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, "")
	if body := rec.Body.String(); strings.Contains(body, "Ghost") {
		t.Fatalf("the resource must not carry the ghost mirror: %s", body)
	}
	// And the x treats the home as the LAST column: the untouched card is
	// deleted, not re-filed into a pair nobody declared.
	rec = doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/remove-from-project", `{"project":"engineering","epic":"Cozystack"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+uid, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("no promotion into a ghost pair — the card is gone: %d %s", rec.Code, rec.Body.String())
	}
}

// The no-project bucket is a mirror home like any other (G15): a column's
// repository is read off the COLUMN, never off a project, so the pair
// {project: "", epic: X} names a real target. The service, the codec, MCP
// and the docs all accept it — this door refused it, and the SPA's own
// picker offers the bucket, so the entry was a 422 with a friendly label.
func TestMirroringIntoTheNoProjectBucketOverHTTP(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: platform\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
		{Path: gitstore.ProjectPath("01JB4PROJ"), Data: []byte("name: engineering\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.EpicPath("01JB4PROJ", "01JB4EPIC"), Data: []byte("name: Cozystack\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		// The bucket: a nameless project stub with a column of its own.
		{Path: gitstore.ProjectPath("_"), Data: []byte("rank: b\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.EpicPath("_", "01JB4INBX"), Data: []byte("name: Inbox\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: "cards/d/4/01JB4K2E7QZMX3R8V0N5T9WYD4.md", Data: []byte("---\ntitle: shared work\nteam: platform\nproject: engineering\nepic: Cozystack\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")},
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

	rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/mirror", `{"project":"","epic":"Inbox"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a bucket column is a mirror target: %d %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Inbox") {
		t.Fatalf("and the card comes back standing in it: %s", body)
	}
	// …and the × on it is the same pair, which is how it is taken away.
	rec = doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+uid+"/actions/unmirror", `{"project":"","epic":"Inbox"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unmirroring the same pair: %d %s", rec.Code, rec.Body.String())
	}
}
