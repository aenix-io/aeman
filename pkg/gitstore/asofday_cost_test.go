package gitstore

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/memory"
)

// counting is a storer that says how many objects were read through it.
type counting struct {
	*memory.Storage
	reads int
}

func (c *counting) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	c.reads++
	return c.Storage.EncodedObject(t, h)
}

var _ storer.EncodedObjectStorer = (*counting)(nil)

// Reading a day must not cost the whole board, once per commit. The first
// version of LoadAsOfDay diffed every commit of the day against its parent to
// find what the day removed: on the production board — 2400 cards, some three
// hundred commits a day — that walked millions of objects and took 75 seconds
// per request, with the day's three requests (cards, board, sprints) each
// paying it in parallel. The board hung on every past day until it was rolled
// back.
//
// The answer comes from what the commits already say (their trailers name the
// cards they touched) and from the two snapshots the day is bounded by, so the
// tree is walked for the few cards that actually went — not for every card on
// every commit. This pins that: the reads must stay far below "a tree per
// commit", and the bound is generous enough to survive an honest refactor.
func TestReadingADayDoesNotCostTheWholeBoardPerCommit(t *testing.T) {
	store := &counting{Storage: memory.NewStorage()}
	r, err := Init(store, Options{Committer: Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	at := func(iso string) time.Time {
		when, err := time.Parse(time.RFC3339, iso)
		if err != nil {
			t.Fatal(err)
		}
		return when
	}
	// A board of some size: 300 cards, spread across the shards.
	const cards = 300
	ids := make([]string, cards)
	writes := make([]FileWrite, 0, cards+1)
	writes = append(writes, FileWrite{Path: BoardPath, Data: []byte("schema: 1\ntitle: t\n")})
	for i := range cards {
		id := fmt.Sprintf("01CARD%020d", i)
		ids[i] = id
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		writes = append(writes, FileWrite{Path: p, Data: fmt.Appendf(nil,
			"---\ntitle: card %d\nteam: portal\nstart: 2026-08-20\nday: 2026-08-20\nprogress: 10\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n", i)})
	}
	if _, err := r.Commit(Action{Name: "import", Summary: "the board", At: at("2026-08-20T09:00:00Z")}, writes); err != nil {
		t.Fatal(err)
	}

	// A day of ordinary work: one commit per action, as the server writes
	// them, and one × among them.
	const commits = 120
	for i := range commits {
		id := ids[i%cards]
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		when := at("2026-08-21T09:00:00Z").Add(time.Duration(i) * time.Minute)
		if _, err := r.Commit(Action{Name: "progress", Actor: "kvaps", Cards: []string{id},
			Summary: "set progress", At: when}, []FileWrite{{Path: p, Data: fmt.Appendf(nil,
			"---\ntitle: card %d\nteam: portal\nstart: 2026-08-20\nday: 2026-08-20\nprogress: %d\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n",
			i%cards, 20+i%70)}}); err != nil {
			t.Fatal(err)
		}
	}
	// The × takes one card off, mid-day.
	gone := ids[7]
	goneP, err := CardPath(gone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(Action{Name: "delete", Actor: "kvaps", Cards: []string{gone},
		Summary: "take it off the board", At: at("2026-08-21T15:00:00Z")}, []FileWrite{{Path: goneP}}); err != nil {
		t.Fatal(err)
	}
	// And the next day happens, so the 21st is a day gone by.
	if _, err := r.Commit(Action{Name: "progress", Actor: "kvaps", Cards: []string{ids[0]},
		Summary: "next day", At: at("2026-08-22T09:00:00Z")}, []FileWrite{{Path: writes[1].Path, Data: writes[1].Data}}); err != nil {
		t.Fatal(err)
	}

	store.reads = 0
	s, ok, err := LoadAsOfDay(r, endOf(t, "2026-08-20"), endOf(t, "2026-08-21"))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	reads := store.reads

	// The day still answers: the card the × took off is on it.
	found := false
	for _, c := range s.Cards {
		if c.ItemID == gone {
			found = true
		}
	}
	if !found {
		t.Fatal("the day gives back what it removed")
	}

	// Two snapshots of a 300-card board plus a walk of 120 commits is a few
	// thousand objects. A tree per commit is a hundred thousand.
	const bound = 12000
	if reads > bound {
		t.Fatalf("reading one day read %d objects; more than %d means the cost grew with the board on every commit", reads, bound)
	}
	t.Logf("objects read for one day: %d", reads)
}
