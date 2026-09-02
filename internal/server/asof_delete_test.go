package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// A card the × takes off today is DELETED, and the day it was worked on
// still holds it. That is the trade the day records made possible: the
// board's history is git, so a card need not be left alive in a past sprint
// — where no live view reaches it and no carry-over ever picks it up — for
// the day to remember it.
func TestADeletedCardStandsOnTheDayItWasWorked(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	const (
		prev = "2026-08-17"
		day  = "2026-08-24"
		gone = "01CARDDELETEDBYTHECROSS01"
		kept = "01CARDSTAYSPUT0000000BBBB2"
	)
	path := func(id string) string {
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	card := func(id string, progress int) gitstore.FileWrite {
		return gitstore.FileWrite{Path: path(id), Data: fmt.Appendf(nil,
			"---\ntitle: card %s\nteam: portal\nassignees:\n  - kvaps\nsprint: %s\nstart: %s\nday: %s\nprogress: %d\nrank: a\ncreated: 2026-08-17T09:00:00Z\n---\n",
			id[len(id)-1:], day, day, day, progress)}
	}
	team := func(current, previous string) gitstore.FileWrite {
		return gitstore.FileWrite{Path: gitstore.TeamPath("01JB4TEAM"), Data: fmt.Appendf(nil,
			"name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: %s\n  previous: %s\n", current, previous)}
	}
	at := func(iso string) time.Time {
		when, err := time.Parse(time.RFC3339, iso)
		if err != nil {
			t.Fatal(err)
		}
		return when
	}
	// Every commit names the cards it touched, as the server's own do: that
	// is what a day reads to find what it removed.
	commit := func(iso, summary string, ids []string, writes ...gitstore.FileWrite) {
		t.Helper()
		if _, err := r.Commit(gitstore.Action{Name: "write", Actor: "kvaps", Cards: ids,
			Summary: summary, At: at(iso)}, writes); err != nil {
			t.Fatal(err)
		}
	}
	commit("2026-08-24T08:00:00Z", "the sprint opens", []string{gone, kept},
		gitstore.FileWrite{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		team(day, prev),
		card(gone, 40), card(kept, 40))
	commit("2026-08-24T15:00:00Z", "done", []string{gone}, card(gone, 100))
	// The × takes it off the board: the file goes (a write with no data).
	commit("2026-08-24T15:05:00Z", "the × takes it off", []string{gone}, gitstore.FileWrite{Path: path(gone)})
	commit("2026-08-24T18:00:00Z", "still going", []string{kept}, card(kept, 60))
	commit("2026-08-31T09:00:00Z", "a new sprint", nil, team("2026-08-31", day))
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)

	// Today's board is without it: taking it off is what the × is for.
	rec := do(t, srv, http.MethodGet, "/api/v1/cards?view=team&team=portal", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), gone) {
		t.Fatalf("today's board still carries the deleted card:\n%s", rec.Body.String())
	}

	// The day it was worked on holds it, done, as a record.
	rec = do(t, srv, http.MethodGet, "/api/v1/cards?view=team&team=portal&day="+day+"&snapshot=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			Metadata struct {
				UID string `json:"uid"`
			} `json:"metadata"`
			Spec struct {
				Progress int `json:"progress"`
			} `json:"spec"`
			Status struct {
				AsOf string `json:"asOf"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	by := map[string]int{}
	for _, c := range got.Items {
		by[c.Metadata.UID] = c.Spec.Progress
		if c.Status.AsOf == "" {
			t.Fatalf("card %s is a record and must say so", c.Metadata.UID)
		}
	}
	if p, there := by[gone]; !there || p != 100 {
		t.Fatalf("the deleted card: present=%v, %d%% — the day it was finished on keeps it, done", there, p)
	}
	if p, there := by[kept]; !there || p != 60 {
		t.Fatalf("the card that stayed: present=%v, %d%%", there, p)
	}
}
