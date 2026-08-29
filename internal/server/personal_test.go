package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	forgepkg "github.com/aenix-io/aeman/internal/forge"
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

// noPushAccess refuses every push check — the forge's answer for a
// repository a token cannot reach.
type noPushAccess struct{ fakeAccess }

func (noPushAccess) canPush(context.Context, string, string) (bool, error) { return false, nil }

// When people sign in through a GitHub App, their token reaches only the
// repositories the app is installed on — so the forge refuses a personal
// repository the app was never installed on, even though its owner can
// obviously push to it. "You need push access to your personal repository"
// would be a lie there, and one nobody can act on: the refusal names the
// real cause and the link that fixes it.
func TestLinkingAPersonalBoardExplainsAMissingInstallation(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	seedGitRemote(t, shared)
	rights := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, shared)
	srv.access = noPushAccess{fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}}

	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"slug":"aenix-aeman","html_url":"https://github.com/apps/aenix-aeman"}`))
	}))
	t.Cleanup(appSrv.Close)
	app, err := forgepkg.NewGitHubAppAt(appSrv.URL, appSrv.Client(), "12345", testServerAppPEM(t))
	if err != nil {
		t.Fatal(err)
	}
	srv.gitCfg.App = app

	// A token a GitHub App minted for this person.
	srv.apiTokens = func(*http.Request) (string, string, error) { return "ghu_visitor", "kvaps", nil }
	rec := doAs(t, srv, "kvaps", "PUT", "/api/v1/me/personal", `{"url":"https://github.com/kvaps/aeman-personal-db.git"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("link refused with %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https://github.com/apps/aenix-aeman/installations/new") {
		t.Fatalf("the refusal must carry the install link: %s", rec.Body.String())
	}
	// And as a field of its own: a URL buried in prose is not a button.
	var refusal struct {
		ActionURL string `json:"actionUrl"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &refusal) != nil || refusal.ActionURL != "https://github.com/apps/aenix-aeman/installations/new" {
		t.Fatalf("the refusal must carry actionUrl for the UI's button: %s", rec.Body.String())
	}

	// Signed in through a plain OAuth App instead, the token reaches every
	// repository its owner can: a refusal there really is about access, and
	// must not send anyone off to install anything.
	srv.apiTokens = func(*http.Request) (string, string, error) { return "gho_visitor", "kvaps", nil }
	rec = doAs(t, srv, "kvaps", "PUT", "/api/v1/me/personal", `{"url":"https://github.com/kvaps/aeman-personal-db.git"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("oauth-app link: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "installations/new") {
		t.Fatalf("an OAuth App token needs no installation: %s", rec.Body.String())
	}
}

// testServerAppPEM is a throwaway RSA key in the shape GitHub hands out.
func testServerAppPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// A personal repository the server cannot reach — the board's GitHub App was
// never installed on it — is the shape of trouble a person meets on a real
// deployment. Two things must not happen: the failure must not be retried
// against the forge on every single request (a failing clone inside the
// request path cost twelve seconds on a live board), and the person must not
// be told "forbidden: no write access to the card's domain" about a
// repository they own, with the real reason left in the server's log.
func TestAPersonalRepositoryTheServerCannotReachIsExplainedOnce(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	seedGitRemote(t, shared)
	rights := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, shared)

	// A remote that refuses every git request, and counts the attempts.
	var hits atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(remote.Close)
	if err := srv.gitBE.linkPersonal(context.Background(), "kvaps", remote.URL+"/kvaps/personal.git"); err != nil {
		t.Fatal(err)
	}

	// The first request tries, and fails.
	if rec := doAs(t, srv, "kvaps", "GET", "/api/v1/board", ""); rec.Code != http.StatusOK {
		t.Fatalf("board: %d %s", rec.Code, rec.Body.String())
	}
	tried := hits.Load()
	if tried == 0 {
		t.Fatal("the personal repository was never tried at all")
	}

	// The next requests do not go back to the forge for the same failure:
	// a broken personal link must not cost a round trip per request.
	for i := 0; i < 5; i++ {
		doAs(t, srv, "kvaps", "GET", "/api/v1/board", "")
	}
	if again := hits.Load(); again != tried {
		t.Fatalf("%d attempts after five more requests, want the first %d — the failure is retried per request", again, tried)
	}

	// And the person is told what is actually wrong, where they meet it.
	rec := doAs(t, srv, "kvaps", "POST", "/api/v1/cards", `{"title":"mine","personal":true,"zone":"planned"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("personal create: %d %s, want 403", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "no write access to the card's domain") {
		t.Fatalf("the generic refusal hides the cause: %s", body)
	}
	if !strings.Contains(body, "personal") || !strings.Contains(body, "could not be") {
		t.Fatalf("the refusal must name the personal repository as the trouble: %s", body)
	}
}

// A linked personal board that would not attach is a state the UI must be
// able to draw — a banner with the reason and a button — not an error the
// person meets only when they try to write. GET /me/personal and the board
// metadata carry the problem and the action URL; GitHub's post-install
// redirect (the app's Setup URL, <base>/auth/setup) forgets the remembered
// failure and tries again at once, so installing the app fixes the board
// without anyone finding a retry button.
func TestABrokenPersonalAttachIsServedAsStateAndRetriedBySetup(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	seedGitRemote(t, shared)
	rights := rightsOn([]string{"shared"}, []string{"shared"})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, shared)

	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"slug":"aenix-aeman","html_url":"https://github.com/apps/aenix-aeman"}`))
	}))
	t.Cleanup(appSrv.Close)
	app, err := forgepkg.NewGitHubAppAt(appSrv.URL, appSrv.Client(), "12345", testServerAppPEM(t))
	if err != nil {
		t.Fatal(err)
	}
	srv.gitCfg.App = app

	var hits atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(remote.Close)
	if err := srv.gitBE.linkPersonal(context.Background(), "kvaps", remote.URL+"/kvaps/personal.git"); err != nil {
		t.Fatal(err)
	}
	doAs(t, srv, "kvaps", "GET", "/api/v1/board", "") // the failed attach, remembered

	var info struct {
		Domain    string `json:"domain"`
		Problem   string `json:"problem"`
		ActionURL string `json:"actionUrl"`
	}
	rec := doAs(t, srv, "kvaps", "GET", "/api/v1/me/personal", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me/personal: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Problem == "" || info.ActionURL != "https://github.com/apps/aenix-aeman/installations/new" {
		t.Fatalf("me/personal must carry the trouble and the action: %s", rec.Body.String())
	}

	rec = doAs(t, srv, "kvaps", "GET", "/api/v1/board", "")
	var meta struct {
		Metadata struct {
			Personal *struct {
				Problem   string `json:"problem"`
				ActionURL string `json:"actionUrl"`
			} `json:"personal"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Metadata.Personal == nil || meta.Metadata.Personal.Problem == "" || meta.Metadata.Personal.ActionURL == "" {
		t.Fatalf("the board metadata must carry the trouble: %s", rec.Body.String())
	}

	// The failure is remembered: more requests do not retry the clone.
	tried := hits.Load()
	doAs(t, srv, "kvaps", "GET", "/api/v1/board", "")
	if hits.Load() != tried {
		t.Fatal("a remembered failure went back to the forge anyway")
	}

	// GitHub sends the person to /auth/setup after they install the app:
	// the memory is dropped and the attach genuinely retried.
	rec = doAs(t, srv, "kvaps", "GET", "/auth/setup?setup_action=install", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("setup: %d, want a redirect home", rec.Code)
	}
	if hits.Load() == tried {
		t.Fatal("the setup callback must retry the attach")
	}
}

// The REST permissions probe does not answer for a GitHub App's user token
// the way it does for an OAuth token — live, it refused a repository the
// very same token had just cloned. Whether a token can push is the git
// transport's question, so for a ghu_ token the transport is asked when
// REST says no: GET /info/refs?service=git-receive-pack, 200 meaning push.
func TestLinkingFallsBackToTheGitTransportForAnAppUserToken(t *testing.T) {
	shared := gitRemoteN(t, "shared")
	seedGitRemote(t, shared)
	rights := rightsOn([]string{"shared"}, []string{"shared"})

	// The forge: REST answers the repository without a usable permissions
	// block; the git transport accepts the token for receive-pack.
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/kvaps/personal", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id": 1}`)) // no permissions block at all
	})
	gitHits := 0
	mux.HandleFunc("/kvaps/personal.git/info/refs", func(w http.ResponseWriter, r *http.Request) {
		gitHits++
		if _, pass, _ := r.BasicAuth(); pass != "ghu_visitor" || r.URL.Query().Get("service") != "git-receive-pack" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	forgeSrv := httptest.NewServer(mux)
	t.Cleanup(forgeSrv.Close)

	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, shared)
	fa := newForgeAccess(forgepkg.NewGitHubAt(forgeSrv.URL), forgeSrv.Client(),
		[]RepoSpec{{Name: "shared", URL: shared.URL}}, "srv", nil)
	// rights still come from the fake; only canPush goes through the forge.
	srv.access = pushThrough{fakeAccess{byLogin: map[string]*domainRights{"kvaps": rights}}, fa}
	srv.apiTokens = func(*http.Request) (string, string, error) { return "ghu_visitor", "kvaps", nil }

	rec := doAs(t, srv, "kvaps", "PUT", "/api/v1/me/personal", `{"url":"`+forgeSrv.URL+`/kvaps/personal.git"}`)
	// The link is admitted (the attach that follows may fail on the fake
	// remote — that part has its own tests); what must NOT happen is the
	// "you need push access" refusal for a token the transport accepts.
	if rec.Code == http.StatusForbidden {
		t.Fatalf("the transport accepts this token; the link must not be refused: %s", rec.Body.String())
	}
	if gitHits == 0 {
		t.Fatal("the git transport was never asked")
	}
}

// pushThrough answers rights from the fake and canPush from the real
// forge-access implementation.
type pushThrough struct {
	fakeAccess
	fa *forgeAccess
}

func (p pushThrough) canPush(ctx context.Context, token, repoURL string) (bool, error) {
	return p.fa.canPush(ctx, token, repoURL)
}
