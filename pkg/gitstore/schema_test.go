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

// Schema 2 also renames the Project-board column from epic to scope and
// retires card mirrors (docs/design/hierarchy.md): the column stubs move
// from projects/<id>/epics/ to projects/<id>/scopes/ with their content
// untouched, the card key `epic` is rewritten as `scope`, and the `mirrors`
// key is stripped. Every step of the bump rewrites the SAME cards, so they
// compose: a card the Process feature, the rename and the mirrors all
// touched comes out with all three applied, in one write.
func TestMigrateSchema2ComposesEveryCardRewrite(t *testing.T) {
	shared := "---\ntitle: shared work\nteam: portal\nproject: engineering\nepic: Cozystack\nmirrors:\n  - {project: strategy, epic: Fundraising}\nprocess: Invoicing\ntask: 01JB4TASK\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n\nThe body stays.\n"
	columnOnly := "---\ntitle: in a column\nproject: engineering\nepic: Cozystack\nrank: b\ncreated: 2026-08-26T09:15:03Z\n---\n"
	plain := "---\ntitle: untouched\nteam: portal\nrank: c\ncreated: 2026-08-26T09:16:03Z\n---\n"
	stub := "name: Cozystack\nrank: a\ncreated: 2026-06-04T08:00:00Z\n"
	r := repoWith(t, map[string]string{
		BoardPath:                                 "schema: 1\ntitle: b\n",
		TeamPath("01JB4TEAM"):                     "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
		ProjectPath("01JB4PROJ"):                  "name: engineering\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
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
	// Step 3: the column stub moved, content untouched; the old path is gone.
	if data, err := r.ReadFile(ScopePath("01JB4PROJ", "01JB4COL")); err != nil || string(data) != stub {
		t.Fatalf("scopes/01JB4COL.yaml = %q (%v), want the stub as it was", data, err)
	}
	if _, err := r.ReadFile("projects/01JB4PROJ/epics/01JB4COL.yaml"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old column path should be gone, err = %v", err)
	}
	// Steps 2, 3, 4 and 5 on ONE card, composed: the retired keys go, the
	// column key is respelled, and — a card in a column being a project card
	// (H4) — the team it carried goes with them.
	data, err := r.ReadFile("cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, gone := range []string{"epic:", "mirrors:", "process:", "task:", "team:"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%s survived the bump:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"title: shared work", "project: engineering", "scope: Cozystack", "The body stays."} {
		if !strings.Contains(got, kept) {
			t.Fatalf("%q was lost by the bump:\n%s", kept, got)
		}
	}
	// Step 3 alone on a card nothing else touched.
	if data, _ := r.ReadFile("cards/c/2/01JB4K2E7QZMX3R8V0N5T9WYC2.md"); !strings.Contains(string(data), "scope: Cozystack") || strings.Contains(string(data), "epic:") {
		t.Fatalf("the column key must be rewritten:\n%s", data)
	}
	// A card none of the steps touched is not rewritten.
	if data, _ := r.ReadFile("cards/c/3/01JB4K2E7QZMX3R8V0N5T9WYC3.md"); string(data) != plain {
		t.Fatalf("an untouched card was rewritten:\n%s", data)
	}
	// And the migrated tree loads as a board whose column holds both cards.
	if again, err := MigrateSchema(r); err != nil || again || r.Head() != c.Hash {
		t.Fatalf("second run: migrated=%v err=%v head moved %v", again, err, r.Head() != c.Hash)
	}
}

// Schema 2's last step (docs/design/hierarchy.md, H4): a card in a column is
// a PROJECT CARD and stands there alone, so the bump takes off it the team,
// the owner and the parent it may carry from before — a slot used to be
// handed to a team and a person, and could be a subtask — and ungroups a
// card that was grouped UNDER one, since a project card has no subtasks
// either. A task keeps its team and owner untouched, and a card already in
// that shape is not rewritten.
func TestMigrateSchema2TakesAProjectCardOutOfEverybodysHands(t *testing.T) {
	const planID, pieceID, taskID, cleanID = "01JB4K2E7QZMX3R8V0N5T9WYC1", "01JB4K2E7QZMX3R8V0N5T9WYC2", "01JB4K2E7QZMX3R8V0N5T9WYC3", "01JB4K2E7QZMX3R8V0N5T9WYC4"
	plan := "---\ntitle: a plan\nassignees:\n  - kvaps\nteam: portal\nzone: yellow\nsize: M\nstart: 2026-08-24\nday: 2026-08-28\nproject: engineering\nepic: Cozystack\nparent: " + taskID + "\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n\nThe body stays.\n"
	piece := "---\ntitle: a piece of the plan\nteam: portal\nparent: " + planID + "\nrank: b\ncreated: 2026-08-26T09:15:03Z\n---\n"
	task := "---\ntitle: a task\nassignees:\n  - kvaps\nteam: portal\nrank: c\ncreated: 2026-08-26T09:16:03Z\n---\n"
	clean := "---\ntitle: already a plan\nproject: engineering\nscope: Cozystack\nrank: d\ncreated: 2026-08-26T09:17:03Z\n---\n"
	r := repoWith(t, map[string]string{
		BoardPath:                                "schema: 1\ntitle: b\n",
		TeamPath("01JB4TEAM"):                    "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
		ProjectPath("01JB4PROJ"):                 "name: engineering\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"projects/01JB4PROJ/epics/01JB4COL.yaml": "name: Cozystack\nrank: a\ncreated: 2026-06-04T08:00:00Z\n",
		"cards/c/1/" + planID + ".md":            plan,
		"cards/c/2/" + pieceID + ".md":           piece,
		"cards/c/3/" + taskID + ".md":            task,
		"cards/c/4/" + cleanID + ".md":           clean,
	})
	migrated, err := MigrateSchema(r)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v, want a migration", migrated, err)
	}
	c, _ := r.CommitObject(r.Head())
	// Step 5 on the card in the column: team, owner, parent, band and size
	// gone, the rest as it was — and step 3 composed with it.
	data, err := r.ReadFile("cards/c/1/" + planID + ".md")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, gone := range []string{"team:", "assignees:", "kvaps", "parent:", "epic:", "zone:", "size:"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%s survived the bump on a project card:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"title: a plan", "scope: Cozystack", "project: engineering", "start: 2026-08-24", "day: 2026-08-28", "The body stays."} {
		if !strings.Contains(got, kept) {
			t.Fatalf("%q was lost by the bump:\n%s", kept, got)
		}
	}
	// The card grouped under it is ungrouped, and keeps everything else.
	data, _ = r.ReadFile("cards/c/2/" + pieceID + ".md")
	if got := string(data); strings.Contains(got, "parent:") || !strings.Contains(got, "team: portal") {
		t.Fatalf("a card grouped under a project card is ungrouped and nothing more:\n%s", got)
	}
	// A task keeps its team and owner, byte for byte; a card already in
	// shape is not rewritten.
	if data, _ := r.ReadFile("cards/c/3/" + taskID + ".md"); string(data) != task {
		t.Fatalf("a task was rewritten:\n%s", data)
	}
	if data, _ := r.ReadFile("cards/c/4/" + cleanID + ".md"); string(data) != clean {
		t.Fatalf("a project card already in shape was rewritten:\n%s", data)
	}
	// And the migrated tree reads as a board on which the plan has no team.
	snap, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range snap.Cards {
		if card.ItemID == planID && (card.Team != "" || len(card.Assignees) != 0 || card.Parent != "" || card.Zone != "" || card.Size != "") {
			t.Fatalf("the loaded project card still carries %+v", card)
		}
	}
	if again, err := MigrateSchema(r); err != nil || again || r.Head() != c.Hash {
		t.Fatalf("second run: migrated=%v err=%v head moved %v", again, err, r.Head() != c.Hash)
	}
}
