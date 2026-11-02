package gitstore

import (
	"slices"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// Own goes through five doors one after another, and every door reads the
// board back. Inside ONE action that read has to be the board the action
// has already written — staged, not yet committed — or a later door decides
// on a card the action no longer has: SetDates read a team-less card off the
// committed tree and gave it the no-team group's sprint over the team's
// current one that SetTeam had just staged. The server's cache applies every
// write at once and never showed it; the store on its own, which an
// embedder uses directly (docs/embedding.md), did. One action, one commit,
// one answer — the same the fake backend gives in own_test.go.
func TestServiceOwnThroughTheBackend(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Commit(Action{Name: "init", Summary: "init"}, []FileWrite{
		{Path: BoardPath, Data: []byte("schema: 2\ntitle: conformance\n")},
		{Path: TeamPath("_"), Data: []byte("rank: a\ncreated: 2026-06-01T08:00:00Z\n")},
	}); err != nil {
		t.Fatal(err)
	}
	mb := NewMultiBackend([]Domain{{Name: "board", Repo: r}}, BackendOptions{})
	svc := boardservice.New(mb)
	ctx := ctxAs("kvaps")
	today := board.TodayIso()
	// alpha's sprint began before today: the sprint the card must join is
	// that one, and a door reading a stale, team-less card would seed today
	// instead — the two answers differ, which is what the test is for.
	opened := board.AddDays(today, -3)
	if err := svc.SetSprintState(ctx, "acme", "alpha", opened, ""); err != nil {
		t.Fatal(err)
	}
	// A strip card: no team, no plan (the raw door dates a create unless told
	// not to). The team arrives with the owner, and that is the write every
	// later door has to see.
	made, err := svc.CreateCard(ctx, "acme", boardservice.CreateCardArgs{Title: "Someday", Undated: true})
	if err != nil {
		t.Fatal(err)
	}

	head := r.Head()
	sctx, flush := WithScope(ctx, Action{Name: "own", Actor: "kvaps", At: time.Now(), Summary: "own"})
	if err := svc.Own(sctx, "acme", made.ItemID, "alpha", "ann"); err != nil {
		t.Fatal(err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	if n := len(commitsBetween(t, r, head)); n != 1 {
		t.Fatalf("own made %d commits, want one", n)
	}

	b, err := svc.Board(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	var c board.Card
	for _, x := range b.Cards {
		if x.ItemID == made.ItemID {
			c = x
		}
	}
	if c.ItemID == "" {
		t.Fatal("the card is gone")
	}
	if c.Team != "alpha" || !slices.Equal(c.Assignees, []string{"ann"}) {
		t.Fatalf("owner = %s/%v, want alpha/ann", c.Team, c.Assignees)
	}
	if c.StartDate != today || c.Day != today || c.Zone != board.ZoneYellow || c.Parked {
		t.Fatalf("filed as %s..%s zone=%s parked=%t, want today, unplanned, on no shelf", c.StartDate, c.Day, c.Zone, c.Parked)
	}
	if c.SprintStart != opened {
		t.Fatalf("sprint = %q, want alpha's current %q: a door read the committed tree, not the action's", c.SprintStart, opened)
	}
}

// The overlay itself: a staged write reads as written, a staged delete as
// gone, a staged new file as there — and the tip is untouched until flush.
func TestLoadOverlaidReadsTheActionsOwnWrites(t *testing.T) {
	r := newRepo(t)
	const kept, edited, doomed, fresh = "01KEPT000000000000000000KP", "01ED1T000000000000000000ED", "01G0NE000000000000000000GN", "01NEW0000000000000000000NW"
	path := func(id string) string {
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := r.Commit(Action{Name: "init", Summary: "init"}, []FileWrite{
		{Path: BoardPath, Data: []byte("schema: 2\ntitle: t\n")},
		{Path: path(kept), Data: []byte("---\ntitle: kept\n---\n")},
		{Path: path(edited), Data: []byte("---\ntitle: before\n---\n")},
		{Path: path(doomed), Data: []byte("---\ntitle: doomed\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	s, err := loadOverlaid(r, map[string][]byte{
		path(edited): []byte("---\ntitle: after\n---\n"),
		path(doomed): nil,
		path(fresh):  []byte("---\ntitle: new\n---\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, c := range s.Cards {
		titles[c.ItemID] = c.Title
	}
	want := map[string]string{kept: "kept", edited: "after", fresh: "new"}
	if len(titles) != len(want) {
		t.Fatalf("cards = %v, want %v", titles, want)
	}
	for id, title := range want {
		if titles[id] != title {
			t.Errorf("%s = %q, want %q", id, titles[id], title)
		}
	}
	// Nothing staged: the committed tree, as Load reads it.
	plain, err := loadOverlaid(r, nil)
	if err != nil || len(plain.Cards) != 3 {
		t.Fatalf("no overlay: %d cards, %v; want the three committed", len(plain.Cards), err)
	}
}
