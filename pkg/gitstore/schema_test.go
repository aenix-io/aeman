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
	if !strings.Contains(string(data), "schema: 1") || !strings.Contains(string(data), "title: b") {
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
	if err != nil || !strings.Contains(string(data), "schema: 1") {
		t.Fatalf("board.yaml = %q (%v)", data, err)
	}
}
