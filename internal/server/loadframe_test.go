package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// awaitLoad reads a subscription until a Load frame arrives and returns the
// people it carries.
func awaitLoad(t *testing.T, ch <-chan []byte) []apiserver.Member {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case data := <-ch:
			var frame struct {
				Kind   string `json:"kind"`
				Object struct {
					Members []apiserver.Member `json:"members"`
				} `json:"object"`
			}
			if json.Unmarshal(data, &frame) == nil && frame.Kind == "Load" {
				return frame.Object.Members
			}
		case <-deadline:
			t.Fatal("no Load frame: the numbers beside a person would stay as they were")
		}
	}
}

func memberIn(people []apiserver.Member, login string) apiserver.Member {
	for _, m := range people {
		if m.Login == login {
			return m
		}
	}
	return apiserver.Member{}
}

// seedOneCard puts a board of one open card on kvaps into a remote.
func seedOneCard(t *testing.T, uid string) gitstore.Remote {
	t.Helper()
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	today := board.TodayIso()
	p, err := gitstore.CardPath(uid)
	if err != nil {
		t.Fatal(err)
	}
	files := []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAMA"), Data: fmt.Appendf(nil,
			"name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: %s\n", today)},
		{Path: p, Data: fmt.Appendf(nil,
			"---\ntitle: a card\nteam: portal\nassignees:\n  - kvaps\nsize: S\nsprint: %s\nstart: %s\nprogress: 20\nrank: a\ncreated: %sT09:00:00Z\n---\nbody\n",
			today, today, today)},
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, files); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	return remote
}

// The numbers beside a person — cards carried, points carried, points a week
// — are summed over cards from EVERY team, so a tab holding one view's cards
// can neither compute them nor learn them from the card events it is already
// receiving: a size set on a card the tab is not showing still moves the
// number over its owner, and a card closed anywhere moves the capacity read
// off the record. So the server announces the PEOPLE, and a board somebody
// has just re-sized reads correctly without a reload.
//
// It went the other way first: sizes and capacities were added, the number
// was drawn from the board resource, and nothing ever told a tab to fetch it
// again — the load only moved when something else reloaded the board.
func TestSizingACardAnnouncesTheNewLoad(t *testing.T) {
	const uid = "01CARDSIZED0000000000AAAAA"
	srv := gitModeServer(t, seedOneCard(t, uid))
	// The board must be read before it can be watched, exactly as a tab does:
	// the first read is what fills the cache the frames are built from.
	if rec := do(t, srv, http.MethodGet, "/api/v1/board", ""); rec.Code != http.StatusOK {
		t.Fatalf("board: %d %s", rec.Code, rec.Body.String())
	}
	sub, cancel := srv.store.subscribe(storeKey(srv.boardRef(nil)), "tab", nil,
		map[string]bool{"cards": true})
	defer cancel()

	if rec := do(t, srv, http.MethodPatch, "/api/v1/cards/"+uid, `{"size":"XL"}`); rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if got := memberIn(awaitLoad(t, sub.ch), "kvaps").Load; got != 8 {
		t.Fatalf("the announced load = %d, want 8 — the card is XL now", got)
	}
}

// A capacity a lead sets is the other half of the same number, and it moves
// for every tab rather than only the one that typed it.
func TestSettingACapacityAnnouncesIt(t *testing.T) {
	const uid = "01CARDCAPPED0000000000BBBB"
	srv := gitModeServer(t, seedOneCard(t, uid))
	if rec := do(t, srv, http.MethodGet, "/api/v1/board", ""); rec.Code != http.StatusOK {
		t.Fatalf("board: %d %s", rec.Code, rec.Body.String())
	}
	sub, cancel := srv.store.subscribe(storeKey(srv.boardRef(nil)), "tab", nil,
		map[string]bool{"cards": true})
	defer cancel()

	if rec := do(t, srv, http.MethodPatch, "/api/v1/people/kvaps", `{"capacity":40}`); rec.Code != http.StatusOK {
		t.Fatalf("capacity: %d %s", rec.Code, rec.Body.String())
	}
	m := memberIn(awaitLoad(t, sub.ch), "kvaps")
	if m.Capacity != 40 {
		t.Fatalf("the announced member = %+v, want the set capacity 40", m)
	}
}

// awaitSprint reads a subscription until a Sprint frame for the team arrives.
func awaitSprint(t *testing.T, ch <-chan []byte, team string) apiserver.Sprint {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case data := <-ch:
			var frame struct {
				Kind   string           `json:"kind"`
				Object apiserver.Sprint `json:"object"`
			}
			if json.Unmarshal(data, &frame) == nil && frame.Kind == "Sprint" &&
				frame.Object.Metadata.Team == team {
				return frame.Object
			}
		case <-deadline:
			t.Fatalf("no Sprint frame for %q", team)
		}
	}
}

// A watch frame is MERGED into the state a client holds, so a field the frame
// leaves out is a field the client LOSES. The Sprint frame used to carry the
// pointers alone, which meant every carry-over — every sprint pointer move —
// silently wiped the capacity each open Triage board measures its weeks
// against, until somebody reloaded the page. The frame is shaped by the same
// function as the listing now, so it cannot say less than the listing does.
func TestASprintFrameCarriesTheTeamsCapacity(t *testing.T) {
	const uid = "01CARDSPRINTCAP000000AAAAA"
	srv := gitModeServer(t, seedOneCard(t, uid))
	if rec := do(t, srv, http.MethodGet, "/api/v1/board", ""); rec.Code != http.StatusOK {
		t.Fatalf("board: %d", rec.Code)
	}
	sub, cancel := srv.store.subscribe(storeKey(srv.boardRef(nil)), "tab", nil,
		map[string]bool{"sprints": true})
	defer cancel()

	if rec := do(t, srv, http.MethodPost, "/api/v1/teams/actions/capacity",
		`{"team":"portal","points":40}`); rec.Code != http.StatusOK {
		t.Fatalf("capacity: %d %s", rec.Code, rec.Body.String())
	}
	got := awaitSprint(t, sub.ch, "portal")
	// Both halves, in one frame: the capacity that was just set, and the
	// pointers the frame used to carry alone. Either missing is a field the
	// client that merges this frame would lose — which is what happened to
	// the capacity on every carry-over, since that goes through this same
	// announcement.
	if got.Spec.Capacity == nil || got.Spec.Capacity.Points != 40 {
		t.Fatalf("the announced sprint = %+v, want the team's 40 points a week", got.Spec)
	}
	if got.Spec.Current == "" {
		t.Fatalf("the announced sprint lost its pointer: %+v", got.Spec)
	}
}

// A capacity is not a card and not a sprint pointer, so the diff a full
// reload runs — external edits, another replica's push, a queued write the
// backend refused — noticed nothing about it, and every tab went on showing
// a number the board no longer held. The reload announces the people now.
func TestAReloadAnnouncesACapacityChangedElsewhere(t *testing.T) {
	const uid = "01CARDRELOADCAP000000AAAAA"
	srv := gitModeServer(t, seedOneCard(t, uid))
	if rec := do(t, srv, http.MethodGet, "/api/v1/board", ""); rec.Code != http.StatusOK {
		t.Fatalf("board: %d", rec.Code)
	}
	sub, cancel := srv.store.subscribe(storeKey(srv.boardRef(nil)), "tab", nil,
		map[string]bool{"cards": true})
	defer cancel()

	// A capacity that appears in the tree without this server writing it —
	// what another replica's push looks like from here.
	e := srv.store.entry(storeKey(srv.boardRef(nil)))
	e.mu.Lock()
	old := e.board
	next := old
	next.People = map[string]board.Person{"kvaps": {Capacity: 33}}
	e.board = next
	e.diffNotify(old)
	e.mu.Unlock()

	if got := memberIn(awaitLoad(t, sub.ch), "kvaps"); got.Capacity != 33 {
		t.Fatalf("the announced member = %+v, want the capacity the reload brought in", got)
	}
}
