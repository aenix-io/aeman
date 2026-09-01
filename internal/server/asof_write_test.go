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

// A card shown as a RECORD cannot be written to, and the server is what says
// so. The client hides every control on such a card, but a UI path that
// forgot — the detail pane's description box did — would otherwise send a
// change that lands on TODAY's card while the person is looking at a picture
// of a day that ended: it "saves", and the text is gone when they open the
// card again.
//
// A request made while looking at a past day says which day (X-Aeman-As-Of);
// the same question the listing answered decides the write.
func TestAWriteFromAPastDayIsRefused(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	card := func(id, team string, progress int) gitstore.FileWrite {
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return gitstore.FileWrite{Path: p, Data: fmt.Appendf(nil,
			"---\ntitle: %s card\nteam: %s\nassignees:\n  - kvaps\nsprint: 2026-08-20\nstart: 2026-08-20\nday: 2026-08-30\nprogress: %d\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\nbody\n",
			team, team, progress)}
	}
	teamFile := func(id, name, current, previous string) gitstore.FileWrite {
		return gitstore.FileWrite{Path: gitstore.TeamPath(id), Data: fmt.Appendf(nil,
			"name: %s\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: %s\n  previous: %s\n",
			name, current, previous)}
	}
	const settled, working = "01CARDSETTLED000000000AAAA", "01CARDWORKING000000000BBBB"
	at := func(iso string) time.Time {
		when, err := time.Parse(time.RFC3339, iso)
		if err != nil {
			t.Fatal(err)
		}
		return when
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed", At: at("2026-08-20T09:00:00Z")}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		teamFile("01JB4TEAMA", "portal", "2026-08-20", ""),
		teamFile("01JB4TEAMB", "backoffice", "2026-08-20", ""),
		card(settled, "portal", 20),
		card(working, "backoffice", 20),
	}); err != nil {
		t.Fatal(err)
	}
	// Portal has opened a new sprint since; backoffice is still in that one.
	if _, err := r.Commit(gitstore.Action{Name: "carry-over", Summary: "advance", At: at("2026-08-25T09:00:00Z")}, []gitstore.FileWrite{
		teamFile("01JB4TEAMA", "portal", "2026-08-25", "2026-08-20"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)

	patch := func(uid, body, asOf string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/cards/"+uid, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if asOf != "" {
			// What the client says it is looking at: the board of that day.
			req.Header.Set("X-Aeman-As-Of", asOf)
		}
		rec := httptest.NewRecorder()
		srv.handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// The record of a day portal has moved past: refused, and the card keeps
	// what it says today.
	if code := patch(settled, `{"description":"typed into a picture"}`, "2026-08-20"); code != http.StatusConflict {
		t.Fatalf("a write from a past day answered %d, want 409", code)
	}
	rec := do(t, srv, http.MethodGet, "/api/v1/cards/"+settled, "")
	var got struct {
		Spec struct {
			Description string `json:"description"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Description == "typed into a picture" {
		t.Fatal("the refused write reached the card")
	}

	// The same day is NOT over for backoffice — that card is live on the same
	// screen and stays writable.
	if code := patch(working, `{"description":"still working"}`, "2026-08-20"); code != http.StatusOK {
		t.Fatalf("a write to a live card on a mixed board answered %d, want 200", code)
	}

	// A NOTE is a write like any other: the day is over for this card, so
	// the note would be filed on today's card while the person is reading a
	// picture of that day.
	note := httptest.NewRequest(http.MethodPost, "/api/v1/cards/"+settled+"/notes",
		strings.NewReader(`{"text":"written into a picture"}`))
	note.Header.Set("Content-Type", "application/json")
	note.Header.Set("X-Aeman-As-Of", "2026-08-20")
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, note)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a note written from a past day answered %d, want 409", rec.Code)
	}

	// And a write with no day claimed is an ordinary write, as every other
	// client makes.
	if code := patch(settled, `{"description":"from today"}`, ""); code != http.StatusOK {
		t.Fatalf("an ordinary write answered %d", code)
	}
}
