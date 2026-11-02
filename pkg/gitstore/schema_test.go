package gitstore

import (
	"errors"
	"strings"
	"testing"
)

// G18 — the schema is a contract between server and repository: a
// repository written by a NEWER server is refused (this server would write
// states it does not understand); one written by an OLDER server is brought
// to the current schema in one commit at start-up, and a current one is left
// alone.

func TestMigrateSchemaOlderInOneCommit(t *testing.T) {
	r := repoWith(t, map[string]string{BoardPath: "title: b\n"}) // no schema key: version 0
	head := r.Head()
	migrated, err := MigrateSchema(r)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v, want a migration", migrated, err)
	}
	data, _ := r.ReadFile(BoardPath)
	if !strings.Contains(string(data), "schema: 2") || !strings.Contains(string(data), "title: b") {
		t.Fatalf("board.yaml after migration:\n%s", data)
	}
	c, _ := r.CommitObject(r.Head())
	if c.ParentHashes[0] != head || ParseTrailers(c.Message).Action != "schema" {
		t.Fatalf("migration commit = %q on %v", c.Message, c.ParentHashes)
	}
	// G6 — the server's own work is authored by the server.
	if c.Author.Name != serverID.Name || c.Author.Email != serverID.Email {
		t.Fatalf("schema commit author = %s <%s>, want the server", c.Author.Name, c.Author.Email)
	}
	// Idempotent: a current repository is left alone.
	again, err := MigrateSchema(r)
	if err != nil || again || r.Head() != c.Hash {
		t.Fatalf("second run: migrated=%v err=%v head moved %v", again, err, r.Head() != c.Hash)
	}
}

func TestMigrateSchemaNewerRefused(t *testing.T) {
	r := repoWith(t, map[string]string{BoardPath: "schema: 99\ntitle: b\n"})
	head := r.Head()
	if _, err := MigrateSchema(r); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("err = %v, want ErrSchemaNewer", err)
	}
	if r.Head() != head {
		t.Fatal("a refused repository must not be written")
	}
}

// A repository without board.yaml at all (a secondary domain seeded by hand)
// gets one, at the current schema.
func TestMigrateSchemaWritesMissingBoardFile(t *testing.T) {
	r := repoWith(t, map[string]string{TeamPath("01T_X"): "name: x\nrank: a\ncreated: 2026-06-01T08:00:00Z\n"})
	migrated, err := MigrateSchema(r)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	data, err := r.ReadFile(BoardPath)
	if err != nil || !strings.Contains(string(data), "schema: 2") {
		t.Fatalf("board.yaml = %q (%v)", data, err)
	}
}

// Schema 2 retired the Process feature (docs/design/hierarchy.md, H8). The
// bump rewrites the tree in the same commit that moves the marker: the
// processes/ directory goes, and the keys a card carried for the feature —
// task, process, accumulate — are stripped, every other key and the body
// untouched. A filed turn is thereby ORPHANED: it stays a card of its week
// and its zone, and loses only the link to the task it came from. The run is
// idempotent — a current repository is left alone — and the cards the
// feature never touched are not rewritten at all.
func TestMigrateSchema2RetiresTheProcessTree(t *testing.T) {
	turn := "---\ntitle: invoice ACME\nteam: portal\nzone: gray\nstage: recurrent\nweek: 2026-08-24\nprocess: Invoicing\ntask: 01JB4TASK\naccumulate: true\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n\nStill the record of that week.\n"
	plain := "---\ntitle: untouched\nteam: portal\nrank: b\ncreated: 2026-08-26T09:15:03Z\n---\n"
	r := repoWith(t, map[string]string{
		BoardPath:                                 "schema: 1\ntitle: b\n",
		TeamPath("01JB4TEAM"):                     "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
		"processes/01JB4PROC/process.yaml":        "name: Invoicing\nproject: portal\nrank: a\ncreated: 2026-06-04T08:00:00Z\n",
		"processes/01JB4PROC/tasks/01JB4TASK.md":  "---\ntitle: Invoice ACME\nteam: portal\nrecurrence: month\n---\n",
		"cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md": turn,
		"cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md": plain,
	})
	head := r.Head()
	migrated, err := MigrateSchema(r)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v, want a migration", migrated, err)
	}
	c, _ := r.CommitObject(r.Head())
	if len(c.ParentHashes) != 1 || c.ParentHashes[0] != head {
		t.Fatalf("the whole bump is ONE commit on the old tip, got parents %v", c.ParentHashes)
	}
	// Step 1: the retired tree is gone.
	for _, p := range []string{"processes/01JB4PROC/process.yaml", "processes/01JB4PROC/tasks/01JB4TASK.md"} {
		if _, err := r.ReadFile(p); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s should be gone after the bump, err = %v", p, err)
		}
	}
	// Step 2: the keys are stripped and nothing else moved.
	data, err := r.ReadFile("cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, key := range []string{"process:", "task:", "accumulate:"} {
		if strings.Contains(got, key) {
			t.Fatalf("%s survived the bump:\n%s", key, got)
		}
	}
	for _, kept := range []string{"title: invoice ACME", "stage: recurrent", "week: 2026-08-24", "zone: gray", "Still the record of that week."} {
		if !strings.Contains(got, kept) {
			t.Fatalf("%q was lost by the bump:\n%s", kept, got)
		}
	}
	// A card the feature never touched is not rewritten.
	if data, _ := r.ReadFile("cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md"); string(data) != plain {
		t.Fatalf("an untouched card was rewritten:\n%s", data)
	}
	if data, _ := r.ReadFile(BoardPath); !strings.Contains(string(data), "schema: 2") {
		t.Fatalf("board.yaml after migration:\n%s", data)
	}
	// Idempotent: the second run finds nothing to do.
	if again, err := MigrateSchema(r); err != nil || again || r.Head() != c.Hash {
		t.Fatalf("second run: migrated=%v err=%v head moved %v", again, err, r.Head() != c.Hash)
	}
}
