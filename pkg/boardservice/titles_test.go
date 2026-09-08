package boardservice

import (
	"context"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// A create-by-URL card is born under a readable fallback — "Issue:
// owner/repo#N" — and a background goroutine fetches the real title and
// renames it. That goroutine gets ONE try, in a process that may be restarted,
// deployed over or unable to write (the two days the storer was blind to its
// own packs), and it reports nothing when it fails. Five cards on the
// production board wore a fallback title for a week that way, three of them
// somebody's open work, and no gesture in the UI would ever finish the job.
//
// The sweep is the second try: a card whose title is EXACTLY the fallback of a
// reference in its own body is a job the create left unfinished, and finishing
// it is a rename away.
func TestTheSweepFinishesATitleTheCreateLost(t *testing.T) {
	fake := newFake([]board.Card{{
		ItemID: "c1", Team: "alpha", Title: "Issue: cozystack/cozystack#4059",
		Description: "https://github.com/cozystack/cozystack/issues/4059",
	}}, nil)
	fake.refs = map[string]board.Link{
		"https://github.com/cozystack/cozystack/issues/4059": {
			URL:   "https://github.com/cozystack/cozystack/issues/4059",
			Kind:  "issue",
			Owner: "cozystack", Repo: "cozystack", Number: 4059,
			Title: "VMDisk import with invalid URL stays in progress forever", State: "open"},
	}
	svc := New(fake)

	if _, _, err := svc.ResolveOpenTitles(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("c1").Title; got != "VMDisk import with invalid URL stays in progress forever" {
		t.Fatalf("title = %q, want the issue's own", got)
	}
}

// Only the untouched fallback is replaced. Somebody's own words — including
// words that merely MENTION the issue — are theirs, exactly as the create's
// own resolve already had it.
func TestTheSweepLeavesEveryTitleSomebodyWrote(t *testing.T) {
	url := "https://github.com/cozystack/cozystack/issues/4059"
	fake := newFake([]board.Card{
		{ItemID: "mine", Team: "alpha", Title: "разобраться с VMDisk", Description: url},
		{ItemID: "near", Team: "alpha", Title: "Issue: cozystack/cozystack#4060", Description: url},
		{ItemID: "plain", Team: "alpha", Title: "Issue: cozystack/cozystack#4059", Description: "no link here"},
	}, nil)
	fake.refs = map[string]board.Link{url: {
		URL: url, Kind: "issue", Owner: "cozystack", Repo: "cozystack", Number: 4059,
		Title: "VMDisk import with invalid URL stays in progress forever", State: "open"}}
	svc := New(fake)

	if _, _, err := svc.ResolveOpenTitles(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"mine", "near", "plain"} {
		if fake.saw("RenameCard " + id) {
			t.Errorf("%s must be left alone; log=%v", id, fake.log)
		}
	}
}

// A reference that will never resolve — a deleted issue, a private repository,
// a forge that is down — must not become a GitHub request every fifteen
// seconds for the life of the process. It is tried again, but on a clock.
func TestTheSweepDoesNotHammerARefThatWillNotResolve(t *testing.T) {
	fake := newFake([]board.Card{{
		ItemID: "c1", Team: "alpha", Title: "Issue: acme/private#9",
		Description: "https://github.com/acme/private/issues/9",
	}}, nil)
	svc := New(fake)
	now := time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if _, _, err := svc.ResolveOpenTitles(context.Background(), "acme"); err != nil {
			t.Fatal(err)
		}
	}
	if n := fake.count("ResolveIssueRef https://github.com/acme/private/issues/9"); n != 1 {
		t.Fatalf("tried %d times in one window, want 1", n)
	}

	now = now.Add(titleRetryEvery + time.Minute)
	if _, _, err := svc.ResolveOpenTitles(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	if n := fake.count("ResolveIssueRef https://github.com/acme/private/issues/9"); n != 2 {
		t.Fatalf("tried %d times after the window, want 2", n)
	}
}

// The sweep is housekeeping, not a board load: a backend that cannot resolve
// anything at all (no forge credential) must not turn every tick into an
// error, and a card it cannot finish stays exactly as it is.
func TestTheSweepIsQuietWhenNothingCanBeResolved(t *testing.T) {
	fake := newFake([]board.Card{{
		ItemID: "c1", Team: "alpha", Title: "Issue: acme/private#9",
		Description: "https://github.com/acme/private/issues/9",
	}}, nil)
	svc := New(fake)

	if _, waiting, err := svc.ResolveOpenTitles(context.Background(), "acme"); err != nil {
		t.Fatalf("a sweep that resolves nothing is not an error: %v", err)
	} else if waiting != 1 {
		t.Fatalf("waiting = %d, want the one card still counted as unfinished", waiting)
	}
	if got := fake.get("c1").Title; got != "Issue: acme/private#9" {
		t.Fatalf("title = %q, want the fallback kept", got)
	}
}
