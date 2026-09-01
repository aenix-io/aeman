package gitstore

import (
	"strconv"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

// day is a board day's last moment in UTC — what "the board on the 21st"
// means when the question is asked of the history.
func endOf(t *testing.T, iso string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", iso)
	if err != nil {
		t.Fatal(err)
	}
	return d.Add(24*time.Hour - time.Nanosecond)
}

// Going back a day on the board must show what the board SHOWED that day,
// not today's values on that day's cards. The storage is git, so that state
// is not reconstructed from anything: it is the tree as it stood when the
// day ended.
func TestTheBoardAsOfADayIsTheTreeThatDayEndedWith(t *testing.T) {
	r := newRepo(t)
	p, err := CardPath("01CARD00000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	card := func(progress int) []FileWrite {
		return []FileWrite{{Path: p, Data: []byte(
			"---\ntitle: the card\nteam: portal\nstart: 2026-08-20\nday: 2026-08-20\nprogress: " +
				strconv.Itoa(progress) + "\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")}}
	}
	seed := []struct {
		at       string
		progress int
	}{
		{"2026-08-20T09:00:00Z", 30},
		{"2026-08-21T15:00:00Z", 60},
		{"2026-08-22T11:00:00Z", 100},
	}
	for _, s := range seed {
		at, err := time.Parse(time.RFC3339, s.at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Commit(Action{Name: "progress", Summary: "set progress", At: at}, card(s.progress)); err != nil {
			t.Fatal(err)
		}
	}

	for _, want := range []struct {
		day      string
		progress int
	}{
		{"2026-08-20", 30},
		{"2026-08-21", 60},
		{"2026-08-22", 100},
		{"2026-08-25", 100}, // a quiet day since: the last state stands
	} {
		s, ok, err := LoadAsOf(r, endOf(t, want.day))
		if err != nil || !ok {
			t.Fatalf("as of %s: ok=%v err=%v", want.day, ok, err)
		}
		if len(s.Cards) != 1 || s.Cards[0].Progress != want.progress {
			t.Fatalf("as of %s the card was at %d%%, want %d%%", want.day, s.Cards[0].Progress, want.progress)
		}
	}

	// Before the board had anything: an empty board, not an error — the day
	// existed, the card did not.
	s, ok, err := LoadAsOf(r, endOf(t, "2026-08-19"))
	if err != nil || !ok {
		t.Fatalf("the day before: ok=%v err=%v", ok, err)
	}
	if len(s.Cards) != 0 {
		t.Fatalf("the board had %d cards the day before it started", len(s.Cards))
	}
}

// A history the clone does not hold cannot be answered from — and must say
// so rather than answer with the oldest state it happens to have, which
// would put today's values on a day nobody can see.
func TestABoardAsOfADayBeyondTheHorizonIsRefused(t *testing.T) {
	r := newRepo(t)
	p, err := CardPath("01CARD00000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	var hs []plumbing.Hash
	for _, at := range []string{"2026-08-01T09:00:00Z", "2026-08-20T09:00:00Z", "2026-08-25T09:00:00Z"} {
		when, err := time.Parse(time.RFC3339, at)
		if err != nil {
			t.Fatal(err)
		}
		h, err := r.Commit(Action{Name: "progress", Summary: "set progress", At: when},
			[]FileWrite{{Path: p, Data: []byte("---\ntitle: the card\nteam: portal\nprogress: 30\nrank: a\ncreated: 2026-08-01T09:00:00Z\n---\n" + at)}})
		if err != nil {
			t.Fatal(err)
		}
		hs = append(hs, h)
	}
	// The clone was cut at the middle commit: what is behind it is a
	// boundary, not an absence.
	if err := r.Storer().SetShallow([]plumbing.Hash{hs[1]}); err != nil {
		t.Fatal(err)
	}

	// A day the clone covers is answerable…
	if _, ok, err := LoadAsOf(r, endOf(t, "2026-08-25")); err != nil || !ok {
		t.Fatalf("the tip's own day: ok=%v err=%v", ok, err)
	}
	// …and a day behind the boundary is refused rather than answered with
	// the boundary's own state.
	if _, ok, err := LoadAsOf(r, endOf(t, "2026-08-10")); err != nil || ok {
		t.Fatalf("a day behind the shallow boundary answered: ok=%v err=%v", ok, err)
	}
	// The boundary's own day still answers — it is the last state we hold.
	if _, ok, err := LoadAsOf(r, endOf(t, "2026-08-20")); err != nil || !ok {
		t.Fatalf("the boundary's own day: ok=%v err=%v", ok, err)
	}
}
