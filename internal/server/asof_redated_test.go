package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Dating a card BACKWARDS today does not put it on a day that is already a
// record. The record answers what the board HELD that day, and the card was
// not there — today's dates are today's opinion about it. The same card does
// appear on a past day that is still LIVE for its team, because that day is
// not a record at all: it is the running sprint, read through the ordinary
// date rules.
func TestACardDatedIntoThePastDoesNotEnterARecord(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	const (
		settledDay = "2026-08-20" // portal has moved past this one
		liveDay    = "2026-08-25" // backoffice is still in the sprint that opened here
		late       = "01CARDDATEDBACK000000AAAA1"
		alsoLate   = "01CARDDATEDBACK000000BBBB2"
	)
	card := func(id, team, start, dayDate, sprint string) gitstore.FileWrite {
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return gitstore.FileWrite{Path: p, Data: fmt.Appendf(nil,
			"---\ntitle: dated back (%s)\nteam: %s\nassignees:\n  - kvaps\nsprint: %s\nstart: %s\nday: %s\nprogress: 30\nrank: a\ncreated: 2026-08-28T09:00:00Z\n---\n",
			team, team, sprint, start, dayDate)}
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
	commit := func(iso, summary string, writes ...gitstore.FileWrite) {
		t.Helper()
		if _, err := r.Commit(gitstore.Action{Name: "write", Summary: summary, At: at(iso)}, writes); err != nil {
			t.Fatal(err)
		}
	}
	commit("2026-08-20T08:00:00Z", "the board starts",
		gitstore.FileWrite{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		teamFile("01JB4TEAMA", "portal", settledDay, ""),
		teamFile("01JB4TEAMB", "backoffice", settledDay, ""))
	// Portal opens a new sprint (so the 20th becomes a record for it), and
	// backoffice opens the one that is still running.
	commit("2026-08-25T08:00:00Z", "sprints move",
		teamFile("01JB4TEAMA", "portal", "2026-08-31", settledDay),
		teamFile("01JB4TEAMB", "backoffice", liveDay, settledDay))
	// TODAY: two cards are created and dated backwards — one into a day that
	// is a record, one into a day that is still being worked.
	commit("2026-09-01T09:00:00Z", "dated back",
		card(late, "portal", settledDay, settledDay, settledDay),
		card(alsoLate, "backoffice", liveDay, liveDay, liveDay))
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)

	on := func(query string) map[string]bool {
		t.Helper()
		rec := do(t, srv, http.MethodGet, "/api/v1/cards?"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body.String())
		}
		var got struct {
			Items []struct {
				Metadata struct {
					UID string `json:"uid"`
				} `json:"metadata"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, c := range got.Items {
			out[c.Metadata.UID] = true
		}
		return out
	}

	// The record of the 20th does not take it: nothing of the sort stood
	// there that day.
	if on("view=team&team=portal&day=" + settledDay + "&snapshot=1")[late] {
		t.Fatal("a card dated backwards today entered a day that is already a record")
	}
	// Today's board is where that opinion lives, and it shows there — the
	// day-lens rules place it as they always did.
	if !on("view=team&team=portal&day=" + settledDay)[late] {
		t.Fatal("without the snapshot the same day is a lens on today's board and must show it")
	}
	// A past day that is still LIVE for its team is not a record, so the card
	// dated into it shows — with or without the flag.
	for _, q := range []string{
		"view=team&team=backoffice&day=" + liveDay + "&snapshot=1",
		"view=team&team=backoffice&day=" + liveDay,
	} {
		if !on(q)[alsoLate] {
			t.Fatalf("%s: the day is still the running sprint and must show the card", q)
		}
	}
}
