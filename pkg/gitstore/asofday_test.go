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
	commit := func(iso string, writes ...FileWrite) {
		t.Helper()
		if _, err := r.Commit(Action{Name: "write", Summary: "w", At: at(iso)}, writes); err != nil {
			t.Fatal(err)
		}
	}
	// The day before: both cards stand.
	commit("2026-08-20T09:00:00Z", card(gone, 30), card(kept, 30))
	// The day itself: one is finished, then taken off; the other works on.
	commit("2026-08-21T14:00:00Z", card(gone, 100))
	commit("2026-08-21T14:05:00Z", FileWrite{Path: path(gone)}) // the ×
	commit("2026-08-21T18:00:00Z", card(kept, 60))
	// And a later day, so the 21st is a day gone by.
	commit("2026-08-22T10:00:00Z", card(kept, 90))

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
	commit("2026-08-23T09:00:00Z", FileWrite{Path: path(kept)})
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
	write := func(iso string, w FileWrite) {
		t.Helper()
		if _, err := r.Commit(Action{Name: "write", Summary: "w", At: at(iso)}, []FileWrite{w}); err != nil {
			t.Fatal(err)
		}
	}
	body := func(progress int) []byte {
		return []byte("---\ntitle: a card\nteam: portal\nstart: 2026-08-21\nday: 2026-08-21\nprogress: " +
			strconv.Itoa(progress) + "\nrank: a\ncreated: 2026-08-21T10:00:00Z\n---\n")
	}
	// The day before knows nothing of it.
	write("2026-08-20T09:00:00Z", FileWrite{Path: BoardPath, Data: []byte("schema: 1\ntitle: t\n")})
	// Made, worked and taken off, all on the 21st.
	write("2026-08-21T10:00:00Z", FileWrite{Path: p, Data: body(0)})
	write("2026-08-21T16:00:00Z", FileWrite{Path: p, Data: body(100)})
	write("2026-08-21T16:05:00Z", FileWrite{Path: p})
	write("2026-08-22T09:00:00Z", FileWrite{Path: BoardPath, Data: []byte("schema: 1\ntitle: t2\n")})

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
