package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

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
