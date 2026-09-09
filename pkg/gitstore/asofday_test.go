package gitstore

import (
	"strconv"
	"testing"
	"time"
)

// A day is everything that STOOD on it, not the state of the tree at
// midnight. The × takes a finished card off the board — the file goes — so a
// card worked and tidied away the same day is absent from the tree the day
// ended with, and reading only that tree loses exactly the work the day is
// remembered for. The day gives it back, in the state it was in when it
// went: done, not as it looked that morning.
func TestADayGivesBackWhatItRemoved(t *testing.T) {
	r := newRepo(t)
	const (
		gone = "01CARD00000000000000000001"
		kept = "01CARD00000000000000000002"
	)
	path := func(id string) string {
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	card := func(id string, progress int) FileWrite {
		return FileWrite{Path: path(id), Data: []byte(
			"---\ntitle: card " + id[len(id)-1:] + "\nteam: portal\nstart: 2026-08-20\nday: 2026-08-20\nprogress: " +
				strconv.Itoa(progress) + "\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")}
	}
	at := func(iso string) time.Time {
		when, err := time.Parse(time.RFC3339, iso)
		if err != nil {
			t.Fatal(err)
		}
		return when
	}
	// Every commit NAMES the cards it touched, as the server's do: that is
	// what the day reads to find what it removed.
	commit := func(iso string, ids []string, writes ...FileWrite) {
		t.Helper()
		if _, err := r.Commit(Action{Name: "write", Actor: "kvaps", Cards: ids,
			Summary: "w", At: at(iso)}, writes); err != nil {
			t.Fatal(err)
		}
	}
	// The day before: both cards stand.
	commit("2026-08-20T09:00:00Z", []string{gone, kept}, card(gone, 30), card(kept, 30))
	// The day itself: one is finished, then taken off; the other works on.
	commit("2026-08-21T14:00:00Z", []string{gone}, card(gone, 100))
	commit("2026-08-21T14:05:00Z", []string{gone}, FileWrite{Path: path(gone)}) // the ×
	commit("2026-08-21T18:00:00Z", []string{kept}, card(kept, 60))
	// And a later day, so the 21st is a day gone by.
	commit("2026-08-22T10:00:00Z", []string{kept}, card(kept, 90))

	s, ok, err := LoadAsOfDay(r, endOf(t, "2026-08-20"), endOf(t, "2026-08-21"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the day is within the history and must answer")
	}
	by := map[string]int{}
	for _, c := range s.Cards {
		by[c.ItemID] = c.Progress
	}
	if p, there := by[gone]; !there || p != 100 {
		t.Fatalf("the card the day removed: present=%v, %d%% — the day keeps it, in the state it went in", there, p)
	}
	if p, there := by[kept]; !there || p != 60 {
		t.Fatalf("the card that stayed: present=%v, %d%%, want the state it ended the day in", there, p)
	}

	// The day BEFORE is untouched by any of this: the card was whole then.
	s, ok, err = LoadAsOfDay(r, endOf(t, "2026-08-19"), endOf(t, "2026-08-20"))
	if err != nil || !ok {
		t.Fatalf("the earlier day: ok=%v err=%v", ok, err)
	}
	by = map[string]int{}
	for _, c := range s.Cards {
		by[c.ItemID] = c.Progress
	}
	if by[gone] != 30 {
		t.Fatalf("on the 20th the card stood at 30%%, got %d%%", by[gone])
	}

	// A card removed on a LATER day is simply there on this one — the tree
	// of the day holds it and nothing needs giving back.
	commit("2026-08-23T09:00:00Z", []string{kept}, FileWrite{Path: path(kept)})
	s, ok, err = LoadAsOfDay(r, endOf(t, "2026-08-21"), endOf(t, "2026-08-22"))
	if err != nil || !ok {
		t.Fatalf("the 22nd: ok=%v err=%v", ok, err)
	}
	found := false
	for _, c := range s.Cards {
		if c.ItemID == kept {
			found = true
			if c.Progress != 90 {
				t.Fatalf("the card on the 22nd stood at 90%%, got %d%%", c.Progress)
			}
		}
	}
	if !found {
		t.Fatal("a card removed a day later still stood on this one")
	}
}

// A card created and removed inside the SAME day is given back too: it stood
// on the board that day, and the day is what stood on it. The rule is one
// rule — the day gives back everything it removed — rather than one that
// weighs how long a card lived.
func TestADayGivesBackACardItBothMadeAndRemoved(t *testing.T) {
	r := newRepo(t)
	const id = "01CARD00000000000000000003"
	p, err := CardPath(id)
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
	write := func(iso string, ids []string, w FileWrite) {
		t.Helper()
		if _, err := r.Commit(Action{Name: "write", Actor: "kvaps", Cards: ids,
			Summary: "w", At: at(iso)}, []FileWrite{w}); err != nil {
			t.Fatal(err)
		}
	}
	body := func(progress int) []byte {
		return []byte("---\ntitle: a card\nteam: portal\nstart: 2026-08-21\nday: 2026-08-21\nprogress: " +
			strconv.Itoa(progress) + "\nrank: a\ncreated: 2026-08-21T10:00:00Z\n---\n")
	}
	// The day before knows nothing of it.
	write("2026-08-20T09:00:00Z", nil, FileWrite{Path: BoardPath, Data: []byte("schema: 1\ntitle: t\n")})
	// Made, worked and taken off, all on the 21st.
	write("2026-08-21T10:00:00Z", []string{id}, FileWrite{Path: p, Data: body(0)})
	write("2026-08-21T16:00:00Z", []string{id}, FileWrite{Path: p, Data: body(100)})
	write("2026-08-21T16:05:00Z", []string{id}, FileWrite{Path: p})
	write("2026-08-22T09:00:00Z", nil, FileWrite{Path: BoardPath, Data: []byte("schema: 1\ntitle: t2\n")})

	s, ok, err := LoadAsOfDay(r, endOf(t, "2026-08-20"), endOf(t, "2026-08-21"))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	found := false
	for _, c := range s.Cards {
		if c.ItemID == id {
			found = true
			if c.Progress != 100 {
				t.Fatalf("the card went at 100%%, the day gives it back at %d%%", c.Progress)
			}
		}
	}
	if !found {
		t.Fatal("a card made and removed on the day still stood on it")
	}

	// The day BEFORE it was made knows nothing of it, then and now.
	s, ok, err = LoadAsOfDay(r, endOf(t, "2026-08-19"), endOf(t, "2026-08-20"))
	if err != nil || !ok {
		t.Fatalf("the 20th: ok=%v err=%v", ok, err)
	}
	for _, c := range s.Cards {
		if c.ItemID == id {
			t.Fatal("the card did not exist yet on the 20th")
		}
	}
}

// A tool writing the repository directly may leave no trailers — the layout
// is public and nothing forces them (docs/design/plugin-impact.md). A card it
// removes is still given back when the day BEGAN with it: the day is bounded
// by two snapshots, and a card in the first and not in the second went during
// it. What such a writer can hide is only a card it both made and removed
// inside one day, which stood on no board anyone opened for long.
func TestADayGivesBackWhatAToolRemovedWithoutSayingSo(t *testing.T) {
	r := newRepo(t)
	const id = "01CARD00000000000000000004"
	p, err := CardPath(id)
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
	// No Cards, no Actor: a bare commit, as another tool would make it.
	bare := func(iso string, w FileWrite) {
		t.Helper()
		if _, err := r.Commit(Action{Name: "write", Summary: "by another tool", At: at(iso)},
			[]FileWrite{w}); err != nil {
			t.Fatal(err)
		}
	}
	bare("2026-08-20T08:00:00Z", FileWrite{Path: BoardPath, Data: []byte("schema: 1\ntitle: t\n")})
	bare("2026-08-20T09:00:00Z", FileWrite{Path: p, Data: []byte(
		"---\ntitle: a card\nteam: portal\nstart: 2026-08-20\nday: 2026-08-20\nprogress: 70\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")})
	bare("2026-08-21T11:00:00Z", FileWrite{Path: p})
	bare("2026-08-22T09:00:00Z", FileWrite{Path: BoardPath, Data: []byte("schema: 1\ntitle: t\n")})

	s, ok, err := LoadAsOfDay(r, endOf(t, "2026-08-20"), endOf(t, "2026-08-21"))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	for _, c := range s.Cards {
		if c.ItemID == id {
			if c.Progress != 70 {
				t.Fatalf("the card stood at 70%% when it went, given back at %d%%", c.Progress)
			}
			return
		}
	}
	t.Fatal("the day began with the card and ended without it: the day gives it back")
}

// A day says which of its cards were FINISHED on it, and it can, because the
// day's commits name the cards they touched. The board draws finished work
// on the day it was finished and no other, and reads that day off the card's
// own doneAt — a young field these open repositories cannot count on. Where
// the domain would have to guess from the card's plan, the history KNOWS: a
// card that is done in the day's tree and was written during the day was
// finished during the day.
//
// A card already done before the day and untouched by it is not the day's,
// and stays unmarked — otherwise every day would end up claiming every
// finished card the tree still carries.
func TestADayNamesTheWorkItFinished(t *testing.T) {
	r := newRepo(t)
	const (
		closed = "01CARD0000000000000CLOSED1"
		before = "01CARD0000000000000BEFORE1"
		open   = "01CARD00000000000000OPEN01"
	)
	path := func(id string) string {
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// No doneAt anywhere: a writer that does not keep the field, which is
	// every writer that closed a card before it existed.
	card := func(id string, progress int) FileWrite {
		return FileWrite{Path: path(id), Data: []byte(
			"---\ntitle: card\nteam: portal\nstart: 2026-08-20\nday: 2026-09-30\nprogress: " +
				strconv.Itoa(progress) + "\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n")}
	}
	at := func(iso string) time.Time {
		when, err := time.Parse(time.RFC3339, iso)
		if err != nil {
			t.Fatal(err)
		}
		return when
	}
	commit := func(iso string, ids []string, writes ...FileWrite) {
		t.Helper()
		if _, err := r.Commit(Action{Name: "write", Actor: "kvaps", Cards: ids,
			Summary: "w", At: at(iso)}, writes); err != nil {
			t.Fatal(err)
		}
	}
	commit("2026-08-19T09:00:00Z", []string{closed, before, open},
		card(closed, 30), card(before, 100), card(open, 30))
	commit("2026-08-20T16:00:00Z", []string{closed, open}, card(closed, 100), card(open, 60))
	commit("2026-08-21T10:00:00Z", []string{open}, card(open, 90))

	s, ok, err := LoadAsOfDay(r, endOf(t, "2026-08-19"), endOf(t, "2026-08-20"))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	done := map[string]string{}
	for _, c := range s.Cards {
		done[c.ItemID] = c.DoneAt
	}
	if done[closed] != "2026-08-20" {
		t.Errorf("a card finished on the day carries that day, got %q", done[closed])
	}
	if done[before] != "" {
		t.Errorf("a day claims no work it did not touch, got %q", done[before])
	}
	if done[open] != "" {
		t.Errorf("open work is not finished work, got %q", done[open])
	}
}

// What the card SAYS wins: a writer that recorded the day is the evidence,
// and a day that overwrote it would move finished work onto itself — the
// card closed on Tuesday and edited on Wednesday would read as Wednesday's.
func TestADayLeavesARecordedDayAlone(t *testing.T) {
	r := newRepo(t)
	const id = "01CARD000000000000RECORD01"
	p, err := CardPath(id)
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
	commit := func(iso, body string) {
		t.Helper()
		if _, err := r.Commit(Action{Name: "write", Actor: "kvaps", Cards: []string{id},
			Summary: "w", At: at(iso)}, []FileWrite{{Path: p, Data: []byte(body)}}); err != nil {
			t.Fatal(err)
		}
	}
	commit("2026-08-19T09:00:00Z", "---\ntitle: a card\nteam: portal\nstart: 2026-08-19\nprogress: 100\ndoneAt: 2026-08-19\nrank: a\ncreated: 2026-08-19T09:00:00Z\n---\n")
	// The next day touches it — a note, a rank, anything — without reopening it.
	commit("2026-08-20T12:00:00Z", "---\ntitle: a card, retitled\nteam: portal\nstart: 2026-08-19\nprogress: 100\ndoneAt: 2026-08-19\nrank: b\ncreated: 2026-08-19T09:00:00Z\n---\n")
	commit("2026-08-21T09:00:00Z", "---\ntitle: a card, retitled\nteam: portal\nstart: 2026-08-19\nprogress: 100\ndoneAt: 2026-08-19\nrank: c\ncreated: 2026-08-19T09:00:00Z\n---\n")

	s, ok, err := LoadAsOfDay(r, endOf(t, "2026-08-19"), endOf(t, "2026-08-20"))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	for _, c := range s.Cards {
		if c.ItemID == id && c.DoneAt != "2026-08-19" {
			t.Fatalf("the day the card records is the day it keeps, got %q", c.DoneAt)
		}
	}
}

// A commit NAMES the cards it touched, and a commit can touch a card without
// finishing it — which is why the day looks at what CHANGED and not at the
// list of names.
//
// Two ordinary actions name cards by the hundred. A rank REBALANCE renumbers
// the whole board: one such commit on the production board named 2471 cards,
// a 66KB trailer, and an ordinary drag is what makes them. A CARRY-OVER names
// every card it moves into the new sprint. A day that read the trailer alone
// stamped every finished card either of them passed over as finished THAT
// DAY: 1712 of them on one production day, and the day board then said a team
// of five had closed 800 cards between the morning and the evening.
func TestADayDoesNotClaimWorkItOnlyTouched(t *testing.T) {
	r := newRepo(t)
	const (
		closed = "01CARD000000000000CLOSED02"
		older  = "01CARD0000000000000OLDER01"
	)
	path := func(id string) string {
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// No doneAt anywhere, and a rank the rebalance rewrites.
	card := func(id string, progress int, rank string) FileWrite {
		return FileWrite{Path: path(id), Data: []byte(
			"---\ntitle: card\nteam: portal\nstart: 2026-08-19\nprogress: " +
				strconv.Itoa(progress) + "\nrank: " + rank + "\ncreated: 2026-08-19T09:00:00Z\n---\n")}
	}
	at := func(iso string) time.Time {
		when, err := time.Parse(time.RFC3339, iso)
		if err != nil {
			t.Fatal(err)
		}
		return when
	}
	commit := func(iso string, ids []string, writes ...FileWrite) {
		t.Helper()
		if _, err := r.Commit(Action{Name: "write", Actor: "kvaps", Cards: ids,
			Summary: "w", At: at(iso)}, writes); err != nil {
			t.Fatal(err)
		}
	}
	// Before the day: one card open, one already finished long ago.
	commit("2026-08-19T09:00:00Z", []string{closed, older}, card(closed, 30, "a"), card(older, 100, "b"))
	// The day itself: real work on one card, and a rebalance that names both.
	commit("2026-08-20T10:00:00Z", []string{closed}, card(closed, 100, "a"))
	commit("2026-08-20T16:00:00Z", []string{closed, older},
		card(closed, 100, "m"), card(older, 100, "n"))
	commit("2026-08-21T09:00:00Z", []string{closed}, card(closed, 100, "p"))

	s, ok, err := LoadAsOfDay(r, endOf(t, "2026-08-19"), endOf(t, "2026-08-20"))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	done := map[string]string{}
	for _, c := range s.Cards {
		done[c.ItemID] = c.DoneAt
	}
	if done[closed] != "2026-08-20" {
		t.Errorf("the card the day actually finished carries the day, got %q", done[closed])
	}
	if done[older] != "" {
		t.Errorf("a card the day only shuffled was finished before it, got %q", done[older])
	}
}
