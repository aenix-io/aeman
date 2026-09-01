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

// Whether a day is over is a question each TEAM answers for itself: a team
// whose sprint has moved on is done with that day and shows it as a record,
// while a team still inside that sprint is working it and must keep the live
// board. One day, one screen, two moments — which is what a board of several
// teams actually is.
func TestAPastDayIsHistoryPerTeam(t *testing.T) {
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
			"---\ntitle: %s card\nteam: %s\nassignees:\n  - kvaps\nsprint: 2026-08-20\nstart: 2026-08-20\nday: 2026-08-30\nprogress: %d\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
			team, team, progress)}
	}
	teamFile := func(id, name, current, previous string) gitstore.FileWrite {
		return gitstore.FileWrite{Path: gitstore.TeamPath(id), Data: fmt.Appendf(nil,
			"name: %s\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: %s\n  previous: %s\n",
			name, current, previous)}
	}
	const moved, staying = "01CARDMOVEDON00000000000AA", "01CARDSTAYING00000000000BB"

	// The 20th: both teams are in the sprint that opened that day, both cards
	// at 20%.
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
		card(moved, "portal", 20),
		card(staying, "backoffice", 20),
	}); err != nil {
		t.Fatal(err)
	}
	// Since then both cards moved on, and PORTAL opened a new sprint while
	// backoffice stayed in the old one.
	if _, err := r.Commit(gitstore.Action{Name: "progress", Summary: "work", At: at("2026-08-25T09:00:00Z")}, []gitstore.FileWrite{
		card(moved, "portal", 90),
		card(staying, "backoffice", 90),
		teamFile("01JB4TEAMA", "portal", "2026-08-25", "2026-08-20"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)

	rec := do(t, srv, http.MethodGet,
		"/api/v1/cards?view=team&team=portal,backoffice&day=2026-08-20&snapshot=1", "")
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
		AsOf string `json:"asOf"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	by := map[string]struct {
		progress int
		asOf     string
	}{}
	for _, c := range got.Items {
		by[c.Metadata.UID] = struct {
			progress int
			asOf     string
		}{c.Spec.Progress, c.Status.AsOf}
	}
	// Portal has moved on: its card is that day's, and says so.
	if m := by[moved]; m.progress != 20 || m.asOf == "" {
		t.Fatalf("the card of the team that moved on: %d%%, asOf=%q — want the day's own copy, marked", m.progress, m.asOf)
	}
	// Backoffice is still in that sprint: its card is today's, unmarked, and
	// stays editable.
	if s := by[staying]; s.progress != 90 || s.asOf != "" {
		t.Fatalf("the card of the team still in that sprint: %d%%, asOf=%q — want today's copy, unmarked", s.progress, s.asOf)
	}
	// The listing says a moment is on screen, so the client can name it.
	if got.AsOf == "" {
		t.Fatal("a listing carrying a day's own cards must name the moment")
	}

	// The pointers follow the same split: portal's as it was that day, and
	// backoffice's as it is — the view rules compare cards against them.
	rec = do(t, srv, http.MethodGet, "/api/v1/sprints?view=team&team=portal,backoffice&day=2026-08-20&snapshot=1", "")
	var sprints struct {
		Items []struct {
			Metadata struct {
				Team string `json:"team"`
			} `json:"metadata"`
			Spec struct {
				Current string `json:"current"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sprints); err != nil {
		t.Fatal(err)
	}
	cur := map[string]string{}
	for _, s := range sprints.Items {
		cur[s.Metadata.Team] = s.Spec.Current
	}
	if cur["portal"] != "2026-08-20" {
		t.Fatalf("portal's pointer that day = %q, want 2026-08-20", cur["portal"])
	}
	if cur["backoffice"] != "2026-08-20" {
		t.Fatalf("backoffice's pointer = %q, want its live one (2026-08-20)", cur["backoffice"])
	}
}
