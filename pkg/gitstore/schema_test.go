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
	if !strings.Contains(string(data), "schema: 3") || !strings.Contains(string(data), "title: b") {
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
	if err != nil || !strings.Contains(string(data), "schema: 3") {
		t.Fatalf("board.yaml = %q (%v)", data, err)
	}
}

// Schema 2 retired the Process feature (docs/design/hierarchy.md, H8). The
// bump rewrites the tree in the same commit that moves the marker: the
// processes/ directory goes, and the keys a card carried for the feature —
// task, process, accumulate — are stripped, every other key and the body
// untouched. A filed turn is thereby ORPHANED: it keeps its zone and loses
// the link to the task it came from (and, schema 3 composing with it, its
// week — it is parked, K8). The run is
// idempotent — a current repository is left alone — and the cards the
// feature never touched are not rewritten at all.
func TestMigrateSchema2RetiresTheProcessTree(t *testing.T) {
	turn := "---\ntitle: invoice ACME\nteam: portal\nzone: gray\nstage: recurrent\nweek: 2026-08-24\nprocess: Invoicing\ntask: 01JB4TASK\naccumulate: true\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n\nStill the record of that week.\n"
	plain := "---\ntitle: untouched\nteam: portal\nsprint: 2026-08-24\nrank: b\ncreated: 2026-08-26T09:15:03Z\n---\n"
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
	for _, kept := range []string{"title: invoice ACME", "stage: recurrent", "zone: gray", "Still the record of that week."} {
		if !strings.Contains(got, kept) {
			t.Fatalf("%q was lost by the bump:\n%s", kept, got)
		}
	}
	// A card the feature never touched is not rewritten.
	if data, _ := r.ReadFile("cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md"); string(data) != plain {
		t.Fatalf("an untouched card was rewritten:\n%s", data)
	}
	if data, _ := r.ReadFile(BoardPath); !strings.Contains(string(data), "schema: 3") {
		t.Fatalf("board.yaml after migration:\n%s", data)
	}
	// Idempotent: the second run finds nothing to do.
	if again, err := MigrateSchema(r); err != nil || again || r.Head() != c.Hash {
		t.Fatalf("second run: migrated=%v err=%v head moved %v", again, err, r.Head() != c.Hash)
	}
}

// Schema 2 also retires card mirrors (docs/design/hierarchy.md): the
// `mirrors` key is stripped. Every step of the bump rewrites the SAME cards,
// so they compose: a card the Process feature and the mirrors both touched
// comes out with both applied, in one write. The column rename the bump
// once made (step 3) is retired with the Project board (H15): the projects/
// tree and every card in a column — under either spelling of the key — are
// left byte for byte as they were.
func TestMigrateSchema2ComposesEveryCardRewrite(t *testing.T) {
	shared := "---\ntitle: shared work\nteam: portal\nmirrors:\n  - {project: strategy, epic: Fundraising}\nprocess: Invoicing\ntask: 01JB4TASK\nsprint: 2026-08-24\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n\nThe body stays.\n"
	columnOnly := "---\ntitle: in a column\nproject: engineering\nepic: Cozystack\nmirrors:\n  - {project: strategy, epic: Fundraising}\nrank: b\ncreated: 2026-08-26T09:15:03Z\n---\n"
	plain := "---\ntitle: untouched\nteam: portal\nsprint: 2026-08-24\nrank: c\ncreated: 2026-08-26T09:16:03Z\n---\n"
	stub := "name: Cozystack\nrank: a\ncreated: 2026-06-04T08:00:00Z\n"
	project := "name: engineering\nrank: a\ncreated: 2026-06-01T08:00:00Z\n"
	r := repoWith(t, map[string]string{
		BoardPath:                                 "schema: 1\ntitle: b\n",
		TeamPath("01JB4TEAM"):                     "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
		"projects/01JB4PROJ/project.yaml":         project,
		"projects/01JB4PROJ/epics/01JB4COL.yaml":  stub,
		"cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md": shared,
		"cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md": columnOnly,
		"cards/c/3/01JB4K2E7QZMX3R8V0N5T9WYC3.md": plain,
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
	// Steps 2 and 4 on ONE card, composed.
	data, err := r.ReadFile("cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, gone := range []string{"mirrors:", "process:", "task:"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%s survived the bump:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"title: shared work", "team: portal", "The body stays."} {
		if !strings.Contains(got, kept) {
			t.Fatalf("%q was lost by the bump:\n%s", kept, got)
		}
	}
	// The retired Project board is left exactly as it was: the column stub
	// under its old directory, the project file, the card in the column.
	for p, want := range map[string]string{
		"projects/01JB4PROJ/project.yaml":         project,
		"projects/01JB4PROJ/epics/01JB4COL.yaml":  stub,
		"cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md": columnOnly,
	} {
		if data, err := r.ReadFile(p); err != nil || string(data) != want {
			t.Fatalf("%s was touched by the bump: %q %v", p, data, err)
		}
	}
	if _, err := r.ReadFile("projects/01JB4PROJ/scopes/01JB4COL.yaml"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the column stub is not moved any more, err = %v", err)
	}
	// A card none of the steps touched is not rewritten.
	if data, _ := r.ReadFile("cards/c/3/01JB4K2E7QZMX3R8V0N5T9WYC3.md"); string(data) != plain {
		t.Fatalf("an untouched card was rewritten:\n%s", data)
	}
	if again, err := MigrateSchema(r); err != nil || again || r.Head() != c.Hash {
		t.Fatalf("second run: migrated=%v err=%v head moved %v", again, err, r.Head() != c.Hash)
	}
}

// Schema 2's last step, as it stands since the Project board went (H15): a
// card in a column is a project card, which no board reads — so the bump
// leaves it byte for byte as it is, team, parent, week and all, and ungroups
// a card that was grouped UNDER one, since that link names nothing a board
// can show. A task keeps its team and owner untouched.
func TestMigrateSchema2LeavesAProjectCardAsItIs(t *testing.T) {
	const planID, pieceID, taskID, cleanID = "01JB4K2E7QZMX3R8V0N5T9WYC1", "01JB4K2E7QZMX3R8V0N5T9WYC2", "01JB4K2E7QZMX3R8V0N5T9WYC3", "01JB4K2E7QZMX3R8V0N5T9WYC4"
	plan := "---\ntitle: a plan\nassignees:\n  - kvaps\nteam: portal\nzone: yellow\nsize: M\nstart: 2026-08-24\nday: 2026-08-28\nsprint: 2026-08-24\nweek: 2026-08-24\nproject: engineering\nepic: Cozystack\nparent: " + taskID + "\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n\nThe body stays.\n"
	piece := "---\ntitle: a piece of the plan\nteam: portal\nsprint: 2026-08-24\nparent: " + planID + "\nrank: b\ncreated: 2026-08-26T09:15:03Z\n---\n"
	task := "---\ntitle: a task\nassignees:\n  - kvaps\nteam: portal\nsprint: 2026-08-24\nrank: c\ncreated: 2026-08-26T09:16:03Z\n---\n"
	clean := "---\ntitle: already a plan\nproject: engineering\nscope: Cozystack\nrank: d\ncreated: 2026-08-26T09:17:03Z\n---\n"
	r := repoWith(t, map[string]string{
		BoardPath:                      "schema: 1\ntitle: b\n",
		TeamPath("01JB4TEAM"):          "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/c/1/" + planID + ".md":  plan,
		"cards/c/2/" + pieceID + ".md": piece,
		"cards/c/3/" + taskID + ".md":  task,
		"cards/c/4/" + cleanID + ".md": clean,
	})
	migrated, err := MigrateSchema(r)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v, want a migration", migrated, err)
	}
	c, _ := r.CommitObject(r.Head())
	for p, want := range map[string]string{
		"cards/c/1/" + planID + ".md":  plan,
		"cards/c/3/" + taskID + ".md":  task,
		"cards/c/4/" + cleanID + ".md": clean,
	} {
		if data, _ := r.ReadFile(p); string(data) != want {
			t.Fatalf("%s was rewritten:\n%s", p, data)
		}
	}
	// The card grouped under it is ungrouped, and keeps everything else.
	data, _ := r.ReadFile("cards/c/2/" + pieceID + ".md")
	if got := string(data); strings.Contains(got, "parent:") || !strings.Contains(got, "team: portal") {
		t.Fatalf("a card grouped under a project card is ungrouped and nothing more:\n%s", got)
	}
	// And the migrated tree reads as a board without either project card.
	snap, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range snap.Cards {
		if card.ItemID == planID || card.ItemID == cleanID {
			t.Fatalf("a project card is read as a card: %+v", card)
		}
	}
	if again, err := MigrateSchema(r); err != nil || again || r.Head() != c.Hash {
		t.Fatalf("second run: migrated=%v err=%v head moved %v", again, err, r.Head() != c.Hash)
	}
}
