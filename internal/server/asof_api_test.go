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
	rec := do(t, srv, http.MethodGet, "/api/v1/cards?view=team&team=portal&day=2026-08-21&snapshot=1", "")
	if rec.Code != http.StatusGone {
		t.Fatalf("a day behind the horizon answered %d: %s", rec.Code, rec.Body.String())
	}
}
