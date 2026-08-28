package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// A personal board end to end: a person links their own repository, the
// link lands in the primary, the repository is attached as their domain — for
// them alone — and their personal cards live there; unlinking detaches it and
// leaves the repository as it is.
func TestPersonalBoardLinkCreateListUnlink(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	seedGitRemote(t, shared)
	mine := gitRemoteN(t, "mine") // unborn: linking initialises it
	both := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": both, "bob": both}}, shared)
	ctx := context.Background()

	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/me/personal", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("no personal board yet: %d %s", rec.Code, rec.Body.String())
	}
	rec := doAs(t, srv, "kvaps", "PUT", "/api/v1/me/personal", `{"url":"`+mine.URL+`"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"~kvaps"`) {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}
	// The link is a file in the primary, committed and pushed.
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	srv.store.waitDrained(waitCtx)
	if err := srv.gitBE.syncNow(ctx, storeKey(srv.gitBoard())); err != nil {
		t.Fatal(err)
	}
	check, err := gitstore.Clone(ctx, memory.NewStorage(), shared, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := check.ReadFile(gitstore.UserPath("kvaps"))
	if err != nil || !strings.Contains(string(data), mine.URL) {
		t.Fatalf("users/kvaps.yaml on the primary: %v\n%s", err, data)
	}

	// The board tells the owner — and only the owner.
	var info struct {
		Metadata struct {
			Domains []struct {
				Name     string `json:"name"`
				Personal bool   `json:"personal"`
				Writable bool   `json:"writable"`
			} `json:"domains"`
			Personal *struct{ Domain, URL string } `json:"personal"`
		} `json:"metadata"`
	}
	rec = doAs(t, srv, "kvaps", "GET", "/api/v1/board", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range info.Metadata.Domains {
		if d.Name == "~kvaps" && d.Personal && d.Writable {
			found = true
		}
	}
	if !found || info.Metadata.Personal == nil || info.Metadata.Personal.URL != mine.URL || info.Metadata.Personal.Domain != "~kvaps" {
		t.Fatalf("kvaps's board metadata = %s", rec.Body.String())
	}
	if rec := doAs(t, srv, "bob", "GET", "/api/v1/board", ""); strings.Contains(rec.Body.String(), "~kvaps") {
		t.Fatalf("bob sees kvaps's personal domain: %s", rec.Body.String())
	}

	// A personal card: created into the personal domain, listed by view=personal,
	// invisible to anyone else even on view=all.
	rec = doAs(t, srv, "kvaps", "POST", "/api/v1/cards", `{"title":"read the paper","zone":"unplanned","personal":true}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"domain":"~kvaps"`) {
		t.Fatalf("create personal: %d %s", rec.Code, rec.Body.String())
	}
	count := func(login, query string) int {
		rec := doAs(t, srv, login, "GET", "/api/v1/cards?"+query, "")
		var list struct{ Items []json.RawMessage }
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("%s %s: %d %s", login, query, rec.Code, rec.Body.String())
		}
		return len(list.Items)
	}
	if n := count("kvaps", "view=personal"); n != 1 {
		t.Fatalf("kvaps's personal view has %d cards, want 1", n)
	}
	if n := count("bob", "view=personal"); n != 0 {
		t.Fatalf("bob's personal view has %d cards, want none", n)
	}
	if rec := doAs(t, srv, "bob", "GET", "/api/v1/cards?view=all", ""); strings.Contains(rec.Body.String(), "read the paper") {
		t.Fatal("bob sees kvaps's personal card on view=all")
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards?view=all", ""); !strings.Contains(rec.Body.String(), "read the paper") {
		t.Fatal("kvaps does not see their own personal card on view=all")
	}
	// A team on a personal card is refused: it is not a team board card.
	if rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards", `{"title":"x","team":"portal","personal":true}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("personal with a team: %d %s", rec.Code, rec.Body.String())
	}
	// The card went to the personal repository.
	srv.store.waitDrained(waitCtx)
	if err := srv.gitBE.syncNow(ctx, storeKey(srv.gitBoard())); err != nil {
		t.Fatal(err)
	}
	pers, err := gitstore.Clone(ctx, memory.NewStorage(), mine, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := gitstore.Load(pers)
	if err != nil || len(snap.Cards) != 1 || snap.Cards[0].Title != "read the paper" {
		t.Fatalf("personal repository: %v %+v", err, snap.Cards)
	}

	// Unlink: the domain goes, the repository stays.
	if rec := doAs(t, srv, "kvaps", "DELETE", "/api/v1/me/personal", ""); rec.Code != http.StatusOK {
		t.Fatalf("unlink: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/board", ""); strings.Contains(rec.Body.String(), "~kvaps") {
		t.Fatalf("still attached after unlink: %s", rec.Body.String())
	}
	if n := count("kvaps", "view=personal"); n != 0 {
		t.Fatalf("personal view after unlink has %d cards", n)
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/me/personal", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("after unlink: %d", rec.Code)
	}
}

// A link outlives the server: the users file is in the primary, and the
// owner's repository is attached again the first time they show up — that is
// when their credential is at hand.
// Reading the personal view is what turns the day over on a personal board —
// as of the real today: a recurrent card finished yesterday is listed today
// as a fresh copy at 0%, the finished one gone; reading again adds nothing;
// and looking at tomorrow (`day=`) is a lens, not a turn of the day — a card
// finished today gets no copy early.
func TestPersonalViewReseedsARecurrentCardTheNextDay(t *testing.T) {
	today := board.TodayIso()
	yesterday, tomorrow := board.AddDays(today, -1), board.AddDays(today, 1)
	shared := gitRemoteN(t, "shared")
	mine := gitRemoteN(t, "mine")
	seedRemoteFiles(t, shared, map[string]string{
		gitstore.BoardPath:         "schema: 1\ntitle: t\n",
		gitstore.TeamPath("_"):     "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		gitstore.UserPath("kvaps"): "personal: " + mine.URL + "\ncreated: 2026-08-28T10:00:00Z\n",
	})
	encode := func(c board.Card) string {
		data, err := gitstore.EncodeCard(gitstore.CardFile{Card: c})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	const finishedYesterday, finishedToday = "01JB4K2E7QZMX3R8V0N5T9WYP1", "01JB4K2E7QZMX3R8V0N5T9WYP2"
	seedRemoteFiles(t, mine, map[string]string{
		gitstore.BoardPath: "schema: 1\ntitle: kvaps\n",
		cardPathOf(t, finishedYesterday): encode(board.Card{Title: "inbox zero", Zone: board.ZoneGreen,
			Stage: board.StageRecurrent, Progress: 100, StartDate: yesterday, Day: yesterday, DoneAt: yesterday,
			Assignees: []string{"kvaps"}, Rank: "a", CreatedAt: yesterday + "T09:00:00Z", Description: "clear the inbox"}),
		cardPathOf(t, finishedToday): encode(board.Card{Title: "stretch", Zone: board.ZoneGreen,
			Stage: board.StageRecurrent, Progress: 100, StartDate: today, Day: today, DoneAt: today,
			Assignees: []string{"kvaps"}, Rank: "b", CreatedAt: today + "T09:00:00Z"}),
	})
	both := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": both}}, shared)

	type row struct {
		UID, Title, Stage string
		Progress          int
	}
	list := func(day string) []row {
		rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards?view=personal&day="+day, "")
		var l struct {
			Items []struct {
				Metadata struct{ UID string }
				Spec     struct {
					Title    string
					Progress int
					Stage    string
				}
			}
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
			t.Fatalf("%s: %d %s", day, rec.Code, rec.Body.String())
		}
		out := make([]row, 0, len(l.Items))
		for _, it := range l.Items {
			out = append(out, row{it.Metadata.UID, it.Spec.Title, it.Spec.Stage, it.Spec.Progress})
		}
		return out
	}
	// Today: the card finished yesterday has turned — its fresh copy at 0% is
	// on the board, itself gone; the one finished today is seen as done.
	rows := list(today)
	if len(rows) != 2 {
		t.Fatalf("today: %+v", rows)
	}
	var fresh row
	for _, r := range rows {
		switch r.Title {
		case "inbox zero":
			if r.UID == finishedYesterday || r.Progress != 0 || r.Stage != "recurrent" {
				t.Fatalf("today: want a fresh recurrent copy of the card finished yesterday, got %+v", r)
			}
			fresh = r
		case "stretch":
			if r.UID != finishedToday || r.Progress != 100 {
				t.Fatalf("today: the card finished today is seen as done, got %+v", r)
			}
		default:
			t.Fatalf("today: %+v", rows)
		}
	}
	// Looking at tomorrow turns nothing over: the fresh copy (started today,
	// open) is there, the card finished today has left — and got no copy early.
	if rows := list(tomorrow); len(rows) != 1 || rows[0].UID != fresh.UID {
		t.Fatalf("tomorrow: %+v (want the fresh copy alone, no early copy of %q)", rows, "stretch")
	}
	// Reading today again reseeds nothing more.
	if again := list(today); len(again) != 2 {
		t.Fatalf("reading again must not reseed twice: %+v", again)
	}
}

// The × on a personal card over the API: a worked-on card is left behind on
// yesterday's board — off today's view, on yesterday's, status.leftAt says
// so — and an untouched one is deleted.
func TestRemovingAWorkedPersonalCardLeavesItOnYesterday(t *testing.T) {
	today := board.TodayIso()
	yesterday := board.AddDays(today, -1)
	shared := gitRemoteN(t, "shared")
	mine := gitRemoteN(t, "mine")
	seedRemoteFiles(t, shared, map[string]string{
		gitstore.BoardPath:         "schema: 1\ntitle: t\n",
		gitstore.TeamPath("_"):     "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		gitstore.UserPath("kvaps"): "personal: " + mine.URL + "\ncreated: 2026-08-28T10:00:00Z\n",
	})
	encode := func(c board.Card) string {
		data, err := gitstore.EncodeCard(gitstore.CardFile{Card: c})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	const worked, untouched = "01JB4K2E7QZMX3R8V0N5T9WYP3", "01JB4K2E7QZMX3R8V0N5T9WYP4"
	seedRemoteFiles(t, mine, map[string]string{
		gitstore.BoardPath: "schema: 1\ntitle: kvaps\n",
		cardPathOf(t, worked): encode(board.Card{Title: "half done", Zone: board.ZoneGreen, Progress: 40,
			StartDate: "2026-08-20", Assignees: []string{"kvaps"}, Rank: "a", CreatedAt: "2026-08-20T09:00:00Z"}),
		cardPathOf(t, untouched): encode(board.Card{Title: "untouched", Zone: board.ZoneGreen,
			StartDate: "2026-08-20", Assignees: []string{"kvaps"}, Rank: "b", CreatedAt: "2026-08-20T09:00:00Z"}),
	})
	both := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": both}}, shared)

	uids := func(day string) []string {
		rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards?view=personal&day="+day, "")
		var l struct {
			Items []struct{ Metadata struct{ UID string } }
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
			t.Fatalf("%s: %d %s", day, rec.Code, rec.Body.String())
		}
		out := make([]string, 0, len(l.Items))
		for _, it := range l.Items {
			out = append(out, it.Metadata.UID)
		}
		return out
	}
	if got := uids(today); len(got) != 2 {
		t.Fatalf("before: %v", got)
	}
	if rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+worked+"/actions/remove", `{}`); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("remove worked: %d %s", rec.Code, rec.Body.String())
	}
	rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+worked, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"leftAt":"`+yesterday+`"`) {
		t.Fatalf("the worked card after ×: %d %s (want kept, leftAt %s)", rec.Code, rec.Body.String(), yesterday)
	}
	if got := uids(today); len(got) != 1 || got[0] != untouched {
		t.Fatalf("today after ×: %v (want the untouched card alone)", got)
	}
	if got := uids(yesterday); !slices.Contains(got, worked) {
		t.Fatalf("yesterday after ×: %v (want the left-behind card there)", got)
	}
	if rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards/"+untouched+"/actions/remove", `{}`); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("remove untouched: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards/"+untouched, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("the untouched card after ×: %d (want deleted)", rec.Code)
	}
}

// cardPathOf is where the store files (and looks for) a card: a seed placed
// anywhere else loads, but no write finds it.
func cardPathOf(t *testing.T, id string) string {
	t.Helper()
	p, err := gitstore.CardPath(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPersonalBoardIsAttachedWhenTheOwnerReturns(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	mine := gitRemoteN(t, "mine")
	seedRemoteFiles(t, shared, map[string]string{
		gitstore.BoardPath:         "schema: 1\ntitle: t\n",
		gitstore.TeamPath("_"):     "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		gitstore.UserPath("kvaps"): "personal: " + mine.URL + "\ncreated: 2026-08-28T10:00:00Z\n",
	})
	seedRemoteFiles(t, mine, map[string]string{
		gitstore.BoardPath:                        "schema: 1\ntitle: kvaps\n",
		"cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYP1.md": "---\ntitle: my note\nzone: yellow\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n",
	})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{
		"kvaps": rightsOn([]string{"shared"}, []string{"shared"}),
		"bob":   rightsOn([]string{"shared"}, []string{"shared"}),
	}}, shared)
	rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards?view=personal", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "my note") {
		t.Fatalf("the returning owner's personal view: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "bob", "GET", "/api/v1/cards?view=all", ""); strings.Contains(rec.Body.String(), "my note") {
		t.Fatal("bob sees a personal card")
	}
}

// Linking a different repository replaces the personal domain: the old clone
// is detached, the new one attached, and the personal view is the new
// repository's — not a token refresh on the old one.
func TestPersonalBoardRelinkSwitchesRepositories(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	seedGitRemote(t, shared)
	first, second := gitRemoteN(t, "first"), gitRemoteN(t, "second")
	seedRemoteFiles(t, first, map[string]string{
		gitstore.BoardPath:                        "schema: 1\ntitle: first\n",
		"cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYF1.md": "---\ntitle: from the first\nzone: yellow\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n",
	})
	seedRemoteFiles(t, second, map[string]string{
		gitstore.BoardPath:                        "schema: 1\ntitle: second\n",
		"cards/a/2/01JB4K2E7QZMX3R8V0N5T9WYF2.md": "---\ntitle: from the second\nzone: red\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n",
	})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rightsOn([]string{"shared"}, []string{"shared"})}}, shared)
	if rec := doAs(t, srv, "kvaps", "PUT", "/api/v1/me/personal", `{"url":"`+first.URL+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("link first: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards?view=personal", ""); !strings.Contains(rec.Body.String(), "from the first") {
		t.Fatalf("first repository not served: %s", rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "PUT", "/api/v1/me/personal", `{"url":"`+second.URL+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("link second: %d %s", rec.Code, rec.Body.String())
	}
	rec := doAs(t, srv, "kvaps", "GET", "/api/v1/cards?view=personal", "")
	if !strings.Contains(rec.Body.String(), "from the second") || strings.Contains(rec.Body.String(), "from the first") {
		t.Fatalf("after relinking the personal view must be the second repository's: %s", rec.Body.String())
	}
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/me/personal", ""); !strings.Contains(rec.Body.String(), second.URL) {
		t.Fatalf("link = %s", rec.Body.String())
	}
}
