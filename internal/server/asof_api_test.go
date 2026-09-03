package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Going back a day on the Me or Team board shows the board OF that day:
// every card as it stood when the day ended, not today's values arranged by
// that day's dates. The storage is the history, so the answer is a tree that
// once was the board.
func TestAPastDayIsServedAsThatDaysOwnBoard(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	id := "01CARD0000000000000000000A"
	p, err := gitstore.CardPath(id)
	if err != nil {
		t.Fatal(err)
	}
	card := func(progress int, title string) []gitstore.FileWrite {
		return []gitstore.FileWrite{{Path: p, Data: fmt.Appendf(nil,
			"---\ntitle: %s\nteam: portal\nassignees:\n  - kvaps\nsprint: 2026-08-20\nstart: 2026-08-20\nday: 2026-08-26\nprogress: %d\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
			title, progress)}}
	}
	seed := []struct {
		at       string
		progress int
		title    string
	}{
		{"2026-08-20T09:00:00Z", 30, "as it began"},
		{"2026-08-21T15:00:00Z", 60, "as it stood"},
		{"2026-08-24T11:00:00Z", 100, "as it is now"},
	}
	first := []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-20\n")},
	}
	for i, s := range seed {
		at, err := time.Parse(time.RFC3339, s.at)
		if err != nil {
			t.Fatal(err)
		}
		writes := card(s.progress, s.title)
		if i == 0 {
			writes = append(first, writes...)
		}
		if i == len(seed)-1 {
			// The sprint moves on: what lies BEFORE the running sprint is
			// the past, and only that is shown as a record (a day inside a
			// running sprint is still being worked — see
			// TestADayInsideTheRunningSprintStaysLive).
			writes = append(writes, gitstore.FileWrite{
				Path: gitstore.TeamPath("01JB4TEAM"),
				Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-20\n"),
			})
		}
		if _, err := r.Commit(gitstore.Action{Name: "progress", Summary: "set progress", At: at}, writes); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)

	read := func(query string) (items []struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
		Spec struct {
			Title    string `json:"title"`
			Progress int    `json:"progress"`
		} `json:"spec"`
	}, asOf string, truncated bool) {
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
				Spec struct {
					Title    string `json:"title"`
					Progress int    `json:"progress"`
				} `json:"spec"`
			} `json:"items"`
			AsOf      string `json:"asOf"`
			Truncated bool   `json:"truncated"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.Items, got.AsOf, got.Truncated
	}

	// The 21st: the card as it was that evening — 60%, under the title it
	// carried then.
	items, asOf, _ := read("view=team&team=portal&day=2026-08-21&snapshot=1")
	if len(items) != 1 || items[0].Spec.Progress != 60 || items[0].Spec.Title != "as it stood" {
		t.Fatalf("the 21st = %+v", items)
	}
	if asOf == "" {
		t.Fatal("a snapshot listing says which moment it reflects")
	}

	// The 20th: the same card earlier in its life.
	items, _, _ = read("view=team&team=portal&day=2026-08-20&snapshot=1")
	if len(items) != 1 || items[0].Spec.Progress != 30 {
		t.Fatalf("the 20th = %+v", items)
	}

	// Without the flag the day is a lens on TODAY's board, as it always was:
	// the card reads 100%, and no moment is claimed.
	items, asOf, _ = read("view=team&team=portal&day=2026-08-21")
	if len(items) != 1 || items[0].Spec.Progress != 100 {
		t.Fatalf("the live listing = %+v", items)
	}
	if asOf != "" {
		t.Fatalf("a live listing claims no moment, got %q", asOf)
	}

	// The BOARD of that day comes with it: the sprint pointers (and the
	// roster) as they stood. The client filters the day's cards with them —
	// with today's pointers it drops nearly all of them, which looked
	// exactly like the feature not working at all.
	rec := do(t, srv, http.MethodGet, "/api/v1/sprints?day=2026-08-21&snapshot=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sprint snapshot: %d %s", rec.Code, rec.Body.String())
	}
	var sprints struct {
		Items []struct {
			Metadata struct {
				Team string `json:"team"`
			} `json:"metadata"`
			Spec struct {
				Current  string `json:"current"`
				Previous string `json:"previous"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sprints); err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, s := range sprints.Items {
		if s.Metadata.Team == "portal" {
			found = s.Spec.Current
		}
	}
	if found != "2026-08-20" {
		t.Fatalf("the sprint pointer of that day = %q, want the one it had (2026-08-20)", found)
	}

	// Only the DAY boards have a day to be a record of. The Project and
	// Process boards lay every week out at once — a day means nothing there —
	// so the flag is ignored and the live board answers, which is what
	// docs/api.md, docs/dates.md and G60 all say.
	for _, view := range []string{"project", "all", "triage"} {
		items, asOf, _ := read("view=" + view + "&day=2026-08-21&snapshot=1")
		if asOf != "" {
			t.Fatalf("view=%s claimed a moment (%s); only Me and Team have days", view, asOf)
		}
		for _, c := range items {
			if c.Spec.Progress != 100 {
				t.Fatalf("view=%s answered with a past board: %+v", view, c)
			}
		}
	}

	// A day the clone's history no longer holds is REFUSED. Answering it
	// with the oldest state at hand would put another day's board under that
	// date, and nothing on the page would say so.
	repo := srv.gitBE.git.domains[0].Repo
	tip, err := repo.CommitObject(repo.Head())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer().SetShallow([]plumbing.Hash{tip.Hash}); err != nil {
		t.Fatal(err)
	}
	rec = do(t, srv, http.MethodGet, "/api/v1/cards?view=team&team=portal&day=2026-08-21&snapshot=1", "")
	if rec.Code != http.StatusGone {
		t.Fatalf("a day behind the horizon answered %d: %s", rec.Code, rec.Body.String())
	}
}
