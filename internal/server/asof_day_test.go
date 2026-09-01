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

// A day is everything that STOOD on it, not the state of the board at
// midnight. A card finished during the day and then tidied away with the ×
// — which moves it back into the previous sprint, dates and all — was on
// that day's board and was done there; a snapshot that reads only the last
// moment of the day loses exactly the work the day is remembered for.
func TestAPastDayHoldsWhatStoodOnItThatDay(t *testing.T) {
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	const (
		prev = "2026-08-17"
		day  = "2026-08-24" // the sprint that opened this day
		tidy = "01CARDTIDIEDAWAY00000AAAA1"
		kept = "01CARDSTAYSPUT0000000BBBB2"
	)
	card := func(id, start, dayDate, sprint string, progress int, left string) gitstore.FileWrite {
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(
			"---\ntitle: card %s\nteam: portal\nassignees:\n  - kvaps\nsprint: %s\nstart: %s\nday: %s\nprogress: %d\nrank: a\ncreated: 2026-08-17T09:00:00Z\n",
			id[len(id)-1:], sprint, start, dayDate, progress)
		if left != "" {
			// What the × writes beside the demote: the day it took the card
			// off, which is the only thing that still knows where the card
			// was worked once its dates have moved.
			body += "leftAt: " + left + "\n"
		}
		return gitstore.FileWrite{Path: p, Data: []byte(body + "---\n")}
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
	commit := func(iso, summary string, writes ...gitstore.FileWrite) {
		t.Helper()
		if _, err := r.Commit(gitstore.Action{Name: "write", Summary: summary, At: at(iso)}, writes); err != nil {
			t.Fatal(err)
		}
	}
	// The sprint opens on the 24th with both cards in it.
	commit("2026-08-24T08:00:00Z", "the sprint opens",
		gitstore.FileWrite{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		team(day, prev),
		card(tidy, day, day, day, 40, ""),
		card(kept, day, day, day, 40, ""))
	// During the day one card is finished…
	commit("2026-08-24T15:00:00Z", "done", card(tidy, day, day, day, 100, ""))
	// …and tidied away with the ×: back into the previous sprint, dates and
	// all, so it stays where it was worked rather than on today's board.
	commit("2026-08-24T15:05:00Z", "the × demotes it", card(tidy, prev, prev, prev, 100, day))
	// The other card is still open when the day ends.
	commit("2026-08-24T18:00:00Z", "still going", card(kept, day, day, day, 60, ""))
	// Since then the sprint has moved on, so the 24th is a record.
	commit("2026-08-31T09:00:00Z", "a new sprint", team("2026-08-31", day))
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)

	rec := do(t, srv, http.MethodGet, "/api/v1/cards?view=team&team=portal&day="+day+"&snapshot=1", "")
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
	// The card that was worked and put away is on the day it was worked, and
	// it reads DONE — its last state that day, not its first.
	if p, there := by[tidy]; !there || p != 100 {
		t.Fatalf("the tidied-away card: present=%v, %d%% — the day it was finished must keep it, done", there, p)
	}
	// And the ordinary card of that day is there as it ended it.
	if p, there := by[kept]; !there || p != 60 {
		t.Fatalf("the card that stayed: present=%v, %d%%", there, p)
	}
}
