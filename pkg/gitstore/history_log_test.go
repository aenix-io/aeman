package gitstore

import (
	"testing"
	"time"
)

// A root commit (an import, the first commit of a repository) is in a card's
// log only when the card is in its tree: a card created later never inherits
// the import as its oldest event. Live smoke found every new card's feed
// opening with the migration's import.
func TestCardLogRootCommitOnlyForCardsInIt(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Commit(Action{Name: "import", Summary: "seed", At: at("2026-08-01T09:00:00Z")}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(Action{Name: "create", Actor: "kvaps", At: at("2026-08-02T09:00:00Z"), Cards: []string{"B2"}, Summary: "create b"}, []FileWrite{
		{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	b, err := r.CardLog("B2", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Entries) != 1 || b.Entries[0].Action != "create" || len(b.Entries[0].Changes) != 1 || b.Entries[0].Changes[0].Kind != "created" {
		t.Fatalf("B2's log = %+v, want only its own create", b.Entries)
	}
	a, err := r.CardLog("A1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Entries) != 1 || a.Entries[0].Action != "import" || len(a.Entries[0].Changes) != 1 || a.Entries[0].Changes[0].Kind != "created" {
		t.Fatalf("A1's log = %+v, want the import as its creation", a.Entries)
	}
}

// A create whose commit also carries the `created` change as a trailer (the
// service records the event, the file diff shows the file appearing) yields
// ONE created event, not two; the same for a delete.
func TestCardLogCreatedAndDeletedOnceWhenTheTrailerSaysSoToo(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Commit(Action{Name: "init", Summary: "board", At: at("2026-08-01T09:00:00Z")}, []FileWrite{
		{Path: "board.yaml", Data: []byte("schema: 1\ntitle: t\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(Action{Name: "create", Actor: "kvaps", At: at("2026-08-02T09:00:00Z"), Cards: []string{"A1"}, Summary: "create",
		Changes: []Change{{Card: "A1", Kind: "created"}}}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\n---\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(Action{Name: "delete", Actor: "kvaps", At: at("2026-08-03T09:00:00Z"), Cards: []string{"A1"}, Summary: "delete",
		Changes: []Change{{Card: "A1", Kind: "deleted"}}}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: nil},
	}); err != nil {
		t.Fatal(err)
	}
	log, err := r.CardLog("A1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Entries) != 2 {
		t.Fatalf("entries = %+v, want create and delete", log.Entries)
	}
	deleted, created := log.Entries[0], log.Entries[1]
	if len(created.Changes) != 1 || created.Changes[0].Kind != "created" {
		t.Fatalf("create entry changes = %+v, want exactly one created", created.Changes)
	}
	if len(deleted.Changes) != 1 || deleted.Changes[0].Kind != "deleted" {
		t.Fatalf("delete entry changes = %+v, want exactly one deleted", deleted.Changes)
	}
}

// The day feed asks what happened on ONE day, over every card it shows.
// Reading each card's whole history for that is what made a page load fire
// dozens of second-long requests: a card carries a decade of commits and the
// feed keeps one day of them. CardLogSince walks only as far as it must —
// entries at or after the boundary, and the walk stops at the first commit
// older than it, so a quiet card costs almost nothing.
func TestCardLogSinceStopsAtTheBoundary(t *testing.T) {
	r := newRepo(t)
	days := []string{"2026-08-01", "2026-08-02", "2026-08-03", "2026-08-04"}
	for i, d := range days {
		if _, err := r.Commit(Action{Name: "progress", Actor: "kvaps", At: at(d + "T09:00:00Z"), Cards: []string{"A1"},
			Summary: "day " + d}, []FileWrite{
			{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: " + string(rune('1'+i)) + "0\n---\n")},
		}); err != nil {
			t.Fatal(err)
		}
	}
	whole, err := r.CardLog("A1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(whole.Entries) != 4 {
		t.Fatalf("the whole log = %d entries, want 4", len(whole.Entries))
	}
	// From the third day on: the two newest entries, the older two untouched.
	got, err := r.CardLogSince("A1", at("2026-08-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 || got.Entries[0].Summary != "day 2026-08-04" || got.Entries[1].Summary != "day 2026-08-03" {
		t.Fatalf("since 2026-08-03 = %d entries %v", len(got.Entries), summaries(got))
	}
	// A boundary newer than everything: nothing, and the truncation is still
	// reported (the caller must not read "no entries" as "no history").
	quiet, err := r.CardLogSince("A1", at("2026-08-05T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(quiet.Entries) != 0 {
		t.Fatalf("since tomorrow = %v", summaries(quiet))
	}
	// A zero boundary is the whole log — the same answer CardLog gives.
	all, err := r.CardLogSince("A1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 4 {
		t.Fatalf("since zero = %d entries, want the whole log", len(all.Entries))
	}
	// A card the day never touched costs no commit reads at all.
	if _, err := r.Commit(Action{Name: "create", Actor: "kvaps", At: at("2026-08-01T10:00:00Z"), Cards: []string{"B2"},
		Summary: "create b"}, []FileWrite{{Path: "cards/b/2/B2.md", Data: []byte("---\ntitle: b\n---\n")}}); err != nil {
		t.Fatal(err)
	}
	b, err := r.CardLogSince("B2", at("2026-08-04T00:00:00Z"))
	if err != nil || len(b.Entries) != 0 {
		t.Fatalf("a quiet card = %v, %v", summaries(b), err)
	}
}

func summaries(l Log) []string {
	out := make([]string, 0, len(l.Entries))
	for _, e := range l.Entries {
		out = append(out, e.Summary)
	}
	return out
}
