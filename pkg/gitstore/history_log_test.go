package gitstore

import "testing"

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
