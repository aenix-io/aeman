package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The day feed is one request for the whole screen: the notes and events of
// one day, per card. It answers only for cards the visitor's board holds —
// a card in a domain they cannot read is absent, not an error — and a card
// that was quiet that day is present and empty, so the client can tell
// "asked and nothing happened" from "not asked".
func TestDayLogsAnswersForTheVisibleCardsOnly(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	seedGitRemote(t, shared)
	// The second domain declares nothing the first does — two repositories
	// naming the same team refuse to be one board (G38).
	closed := gitRemoteN(t, "closed")
	seedRemoteFiles(t, closed, map[string]string{
		gitstore.BoardPath:                      "schema: 1\ntitle: closed\n",
		gitstore.ProjectPath("01JB4PROJSECRET"): "name: secret\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
	})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{
		"kvaps": rightsOn([]string{"shared", "closed"}, []string{"shared", "closed"}),
		"bob":   rightsOn([]string{"shared"}, []string{"shared"}),
	}}, shared, closed)

	create := func(login, body string) string {
		t.Helper()
		rec := doAs(t, srv, login, "POST", "/api/v1/cards", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Metadata struct{ UID string }
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Metadata.UID
	}
	mine := create("kvaps", `{"title":"in shared","zone":"planned"}`)
	// A personal card is the sharpest case of "not yours to read": its
	// domain is served to its owner alone, whatever the forge says.
	personalRemote := gitRemoteN(t, "personal")
	if rec := doAs(t, srv, "kvaps", "PUT", "/api/v1/me/personal", `{"url":"`+personalRemote.URL+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("link personal: %d %s", rec.Code, rec.Body.String())
	}
	hidden := create("kvaps", `{"title":"mine alone","zone":"urgent","personal":true}`)
	if rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+mine+"/notes", `{"text":"a note today"}`); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("note: %d %s", rec.Code, rec.Body.String())
	}

	day := board.TodayIso()
	var got struct {
		Kind  string
		Day   string
		Cards map[string][]struct {
			Type, ID, At, Kind, Text string
		}
	}
	rec := doAs(t, srv, "kvaps", "GET", "/api/v1/logs?day="+day+"&uids="+mine+","+hidden+",01NOSUCHCARD0000000000000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("day logs: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "DayLogList" || got.Day != day {
		t.Fatalf("envelope = %+v", got)
	}
	entries, ok := got.Cards[mine]
	if !ok {
		t.Fatalf("the visitor's own card is missing: %v", got.Cards)
	}
	var notes, events int
	for _, e := range entries {
		switch e.Type {
		case "note":
			notes++
			if e.Text != "a note today" {
				t.Fatalf("note text = %q", e.Text)
			}
		case "event":
			events++
		}
	}
	if notes != 1 || events == 0 {
		t.Fatalf("the day's feed = %d notes, %d events: %+v", notes, events, entries)
	}
	if _, ok := got.Cards["01NOSUCHCARD0000000000000"]; ok {
		t.Fatal("an unknown card must be absent from the answer")
	}
	if _, ok := got.Cards[hidden]; !ok {
		t.Fatal("the owner must be answered for their own personal card")
	}
	// bob cannot read another person's personal board: that card is absent
	// for them, and asking about it is not an error.
	rec = doAs(t, srv, "bob", "GET", "/api/v1/logs?uids="+mine+","+hidden, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("bob: %d %s", rec.Code, rec.Body.String())
	}
	var bobs struct {
		Cards map[string][]json.RawMessage
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bobs); err != nil {
		t.Fatal(err)
	}
	if _, ok := bobs.Cards[hidden]; ok {
		t.Fatalf("bob must not read another person's personal card: %v", bobs.Cards)
	}
	if _, ok := bobs.Cards[mine]; !ok {
		t.Fatal("bob reads shared and must be answered for its card")
	}

	// A day nothing happened on: present and empty, not missing.
	rec = doAs(t, srv, "kvaps", "GET", "/api/v1/logs?day=2020-01-01&uids="+mine, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if entries, ok := got.Cards[mine]; !ok || len(entries) != 0 {
		t.Fatalf("a quiet day = %v (present %v), want present and empty", entries, ok)
	}

	// The request is bounded: a day feed is a screen, not the whole board.
	many := make([]string, 0, 201)
	for i := 0; i < 201; i++ {
		many = append(many, mine)
	}
	rec = doAs(t, srv, "kvaps", "GET", "/api/v1/logs?uids="+strings.Join(many, ","), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("201 uids: %d, want 400", rec.Code)
	}
	// A day that is not a date is a bad request, not a panic.
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/logs?day=someday&uids="+mine, ""); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a bad day: %d %s", rec.Code, rec.Body.String())
	}
}
