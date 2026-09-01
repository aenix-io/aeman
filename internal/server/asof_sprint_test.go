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

// A day inside the RUNNING sprint is not history. A team's sprint lays itself
// out on its own day — that is where the lead works the whole sprint from,
// and where a card created today lands (its sprint pointer may be days old) —
// so showing that day as a frozen record would take the board away from the
// people still standing in it. The past begins where the running sprint does.
func TestADayInsideTheRunningSprintStaysLive(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	id := "01CARD0000000000000000000B"
	p, err := gitstore.CardPath(id)
	if err != nil {
		t.Fatal(err)
	}
	// The sprint opened on the 24th and is still running; the card has been
	// worked on since.
	team := []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-17\n")
	card := func(progress int) []gitstore.FileWrite {
		return []gitstore.FileWrite{{Path: p, Data: fmt.Appendf(nil,
			"---\ntitle: the card\nteam: portal\nassignees:\n  - kvaps\nsprint: 2026-08-24\nstart: 2026-08-20\nday: 2026-08-30\nprogress: %d\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
			progress)}}
	}
	seed := []struct {
		at       string
		progress int
	}{
		{"2026-08-20T09:00:00Z", 20},
		{"2026-08-24T10:00:00Z", 50},
		{"2026-08-30T10:00:00Z", 90},
	}
	for i, s := range seed {
		at, err := time.Parse(time.RFC3339, s.at)
		if err != nil {
			t.Fatal(err)
		}
		writes := card(s.progress)
		if i == 0 {
			writes = append([]gitstore.FileWrite{
				{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
				{Path: gitstore.TeamPath("01JB4TEAM"), Data: team},
			}, writes...)
		}
		if _, err := r.Commit(gitstore.Action{Name: "progress", Summary: "set progress", At: at}, writes); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)

	read := func(query string) (progress int, asOf string) {
		t.Helper()
		rec := do(t, srv, http.MethodGet, "/api/v1/cards?"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body.String())
		}
		var got struct {
			Items []struct {
				Spec struct {
					Progress int `json:"progress"`
				} `json:"spec"`
			} `json:"items"`
			AsOf string `json:"asOf"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Items) != 1 {
			t.Fatalf("%s: %d cards", query, len(got.Items))
		}
		return got.Items[0].Spec.Progress, got.AsOf
	}

	// The sprint's own day is past on the calendar and LIVE on the board:
	// the card reads what it reads now, and no moment is claimed.
	if progress, asOf := read("view=team&team=portal&day=2026-08-24&snapshot=1"); progress != 90 || asOf != "" {
		t.Fatalf("the running sprint's own day: %d%%, asOf=%q — it must answer live", progress, asOf)
	}
	// So is a later day of the same running sprint.
	if progress, asOf := read("view=team&team=portal&day=2026-08-30&snapshot=1"); progress != 90 || asOf != "" {
		t.Fatalf("a later day of the running sprint: %d%%, asOf=%q", progress, asOf)
	}
	// Before the sprint opened, the board is a record.
	if progress, asOf := read("view=team&team=portal&day=2026-08-21&snapshot=1"); progress != 20 || asOf == "" {
		t.Fatalf("a day before the running sprint: %d%%, asOf=%q — it must be the snapshot", progress, asOf)
	}
	// The Me board draws the same line.
	if progress, asOf := read("view=me&user=kvaps&day=2026-08-24&snapshot=1"); progress != 90 || asOf != "" {
		t.Fatalf("Me on the running sprint's own day: %d%%, asOf=%q", progress, asOf)
	}
	if progress, asOf := read("view=me&user=kvaps&day=2026-08-21&snapshot=1"); progress != 20 || asOf == "" {
		t.Fatalf("Me before the running sprint: %d%%, asOf=%q", progress, asOf)
	}
}
