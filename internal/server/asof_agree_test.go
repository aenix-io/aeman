package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The listing and the write door answer the same question. What a listing
// marks as a record is refused; what it does not mark is written. Two
// derivations of "is this a record" — one from the card the merge moved, one
// from the team names of the live board — disagree exactly where the two
// boards call a card's team by different names: a card that MOVED between
// teams since. There it came through live, draggable and editable, and every
// write to it answered 409.
func TestTheListingAndTheWriteDoorAgree(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	const day = "2026-08-20"
	const moved = "01CARDMOVEDTEAM0000000AAAA"
	card := func(team string, progress int) gitstore.FileWrite {
		p, err := gitstore.CardPath(moved)
		if err != nil {
			t.Fatal(err)
		}
		return gitstore.FileWrite{Path: p, Data: fmt.Appendf(nil,
			"---\ntitle: it moved\nteam: %s\nassignees:\n  - kvaps\nsprint: %s\nstart: %s\nday: 2026-08-30\nprogress: %d\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\nbody\n",
			team, day, day, progress)}
	}
	teamFile := func(id, name, current, previous string) gitstore.FileWrite {
		return gitstore.FileWrite{Path: gitstore.TeamPath(id), Data: fmt.Appendf(nil,
			"name: %s\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: %s\n  previous: %s\n",
			name, current, previous)}
	}
	at := func(iso string) time.Time {
		when, err := time.Parse(time.RFC3339, iso)
		if err != nil {
			t.Fatal(err)
		}
		return when
	}
	// That evening the card was backoffice's, in the sprint both teams were
	// in.
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed", At: at("2026-08-20T09:00:00Z")}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		teamFile("01JB4TEAMA", "portal", day, ""),
		teamFile("01JB4TEAMB", "backoffice", day, ""),
		card("backoffice", 20),
	}); err != nil {
		t.Fatal(err)
	}
	// Since then it moved to portal, and portal opened a new sprint — so the
	// day is over for the card, under a team it did not have that evening.
	if _, err := r.Commit(gitstore.Action{Name: "team", Summary: "re-team", At: at("2026-08-25T09:00:00Z")}, []gitstore.FileWrite{
		card("portal", 90),
		teamFile("01JB4TEAMA", "portal", "2026-08-31", day),
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)

	rec := do(t, srv, http.MethodGet,
		"/api/v1/cards?view=team&team=portal,backoffice&day="+day+"&snapshot=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			Metadata struct {
				UID string `json:"uid"`
			} `json:"metadata"`
			Status struct {
				AsOf string `json:"asOf"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	marked := map[string]bool{}
	for _, c := range got.Items {
		marked[c.Metadata.UID] = c.Status.AsOf != ""
	}
	if !marked[moved] {
		t.Fatal("the card came through live: the listing must mark what it took from that evening, whatever team the two boards call it")
	}

	// And the write door refuses exactly what the listing marked.
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cards/"+moved,
		strings.NewReader(`{"description":"typed into a picture"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Aeman-As-Of", day)
	w := httptest.NewRecorder()
	srv.handler.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("the write answered %d, want 409 — the listing marked this card a record", w.Code)
	}
}
