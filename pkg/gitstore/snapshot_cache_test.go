package gitstore

import (
	"fmt"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// bigRepo is a board of n cards, the shape a real one has: a team, a
// project with a column, and n cards spread over it.
func bigRepo(t *testing.T, n int) *Repo {
	t.Helper()
	files := map[string]string{
		BoardPath:                         "schema: 1\ntitle: big\n",
		TeamPath("01T_PORTAL"):            "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-17\n",
		ProjectPath("01P_PORTAL"):         "name: portal\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		EpicPath("01P_PORTAL", "01E_BUG"): "name: Bugs\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
	}
	for i := range n {
		id := fmt.Sprintf("01CARD%018d", i)
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		files[p] = fmt.Sprintf("---\ntitle: card %d\nteam: portal\nsprint: 2026-08-24\nstart: 2026-08-24\nrank: a%d\ncreated: 2026-08-20T09:00:00Z\n---\nbody\n", i, i)
	}
	return repoWith(t, files)
}

// A snapshot is read once per TIP, not once per write. Every setter asks
// where its card belongs, and asking meant decoding every card file on the
// board again: one carry-over over a real board did that hundreds of times
// and took minutes to write what it decided in a second.
func TestASnapshotIsReadOncePerTip(t *testing.T) {
	r := bigRepo(t, 400)

	start := time.Now()
	first, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	cold := time.Since(start)
	if len(first.Cards) != 400 {
		t.Fatalf("the board is %d cards", len(first.Cards))
	}

	start = time.Now()
	for range 50 {
		if _, err := Load(r); err != nil {
			t.Fatal(err)
		}
	}
	warm := time.Since(start) / 50
	t.Logf("cold read %v, memoized read %v (%.0fx)", cold, warm, float64(cold)/float64(max(warm, 1)))
	if warm*4 > cold {
		t.Fatalf("a memoized read must be a fraction of the tree walk: cold %v, warm %v", cold, warm)
	}

	// The memo is per TIP: a write moves it, and the next read sees the
	// write rather than the board as it was.
	if _, err := r.Commit(Action{Name: "update", Summary: "one more"},
		[]FileWrite{{Path: "cards/z/z/01CARDZZ.md", Data: []byte("---\ntitle: new\nteam: portal\n---\n")}}); err != nil {
		t.Fatal(err)
	}
	after, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Cards) != 401 {
		t.Fatalf("the next read sees the write: %d cards", len(after.Cards))
	}

	// And the copy handed out is the caller's: stamping one must not stamp
	// the next reader's board (LoadAll stamps every entry with its domain).
	after.Cards[0].Domain = "somebody-elses"
	again, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	if again.Cards[0].Domain != "" {
		t.Fatalf("a reader's stamp leaked into the memo: %q", again.Cards[0].Domain)
	}
}

// What the memo is for: an action that writes many cards — a carry-over is
// the big one — asks for a snapshot per field it writes.
func TestACarryOverOverABigBoardDoesNotReReadItPerWrite(t *testing.T) {
	r := bigRepo(t, 400)
	mb := NewMultiBackend([]Domain{{Name: "board", Repo: r}},
		BackendOptions{Now: time.Now})
	svc := boardservice.New(mb)
	ctx, flush := WithScope(ctxAs("kvaps"), Action{Name: "carry-over", ID: NewID(time.Now())})
	start := time.Now()
	rep, err := svc.CarryOver(ctx, "acme", "portal", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flush(); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("carry-over of %d cards over a 400-card board: %v", rep.Carried, took)
	if rep.Carried == 0 {
		t.Fatal("the fixture must have something to carry")
	}
	// Generous by design: the point is that it is not minutes.
	if took > 20*time.Second {
		t.Fatalf("a carry-over must not re-read the board per write: %v", took)
	}
	_ = board.TodayIso()
}
