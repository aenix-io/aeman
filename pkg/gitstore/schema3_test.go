package gitstore

import (
	"strings"
	"testing"
)

// Schema 3 is the team backlog (docs/design/backlog.md, K8): one commit per
// domain that parks the weekly plan and the Triage strip and takes the week
// off every task, in one idempotent pass.
//
//  1. A task with a week and no date and no sprint is parked; its week goes —
//     a task with a week was the weekly plan: work accepted and not in hand.
//  2. A task with a week and a date or a sprint keeps them and loses the week.
//  3. An open task that would have stood in the strip — no date, no sprint,
//     no week, not parked — is parked.
//
// Nothing else moves: the rank stays (BacklogOrder puts unranked cards after
// ranked ones, oldest first), a subtask or a review card loses a stray week
// and is not parked (it rides the card it belongs to), and a finished card,
// a planning card and a project card are not asked to be placed at all.
func TestSchema3ParksTheWeeklyPlanAndTheStripAndStripsTaskWeeks(t *testing.T) {
	const (
		weekOnly  = "01JB4K2E7QZMX3R8V0N5T9WYE1"
		weekDated = "01JB4K2E7QZMX3R8V0N5T9WYE2"
		strip     = "01JB4K2E7QZMX3R8V0N5T9WYE3"
		stripDone = "01JB4K2E7QZMX3R8V0N5T9WYE4"
		inWork    = "01JB4K2E7QZMX3R8V0N5T9WYE5"
		parked    = "01JB4K2E7QZMX3R8V0N5T9WYE6"
		kidWeek   = "01JB4K2E7QZMX3R8V0N5T9WYE7"
		kid       = "01JB4K2E7QZMX3R8V0N5T9WYE8"
		epic      = "01JB4K2E7QZMX3R8V0N5T9WYE9"
		underEpic = "01JB4K2E7QZMX3R8V0N5T9WYEA"
		review    = "01JB4K2E7QZMX3R8V0N5T9WYEB"
		slot      = "01JB4K2E7QZMX3R8V0N5T9WYEC"
	)
	card := func(front string) string {
		return "---\n" + front + "created: 2026-08-26T09:14:03Z\n---\n\nThe body stays.\n"
	}
	files := map[string]string{
		weekOnly:  card("title: planned for a week\nassignees: [kvaps]\nteam: portal\nweek: 2026-10-05\nrank: a\n"),
		weekDated: card("title: dated and weeked\nteam: portal\nstart: 2026-10-06\nday: 2026-10-06\nsprint: 2026-10-05\nweek: 2026-10-05\nrank: b\n"),
		strip:     card("title: nobody said when\nteam: portal\nprogress: 30\nrank: c\n"),
		stripDone: card("title: finished, never placed\nteam: portal\nprogress: 100\nrank: d\n"),
		inWork:    card("title: in work\nteam: portal\nsprint: 2026-10-05\nrank: e\n"),
		parked:    card("title: already parked\nteam: portal\nparked: true\nrank: f\n"),
		kidWeek:   card("title: a subtask with a stray week\nteam: portal\nweek: 2026-10-05\nparent: " + strip + "\nrank: g\n"),
		kid:       card("title: a subtask\nteam: portal\nparent: " + strip + "\nrank: h\n"),
		epic:      card("title: an epic\ntype: epic\nrank: i\n"),
		underEpic: card("title: a task under the epic\nparent: " + epic + "\nrank: j\n"),
		review:    card("title: review: in work\nteam: portal\nreviewOf: " + inWork + "\nrank: k\n"),
		slot:      card("title: a project card\nproject: engineering\nscope: Cozystack\nstart: 2026-10-05\nday: 2026-10-09\nrank: l\n"),
	}
	tree := map[string]string{
		BoardPath:                     "schema: 2\ntitle: b\n",
		TeamPath("01JB4TEAM"):         "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
		ProjectPath("01JB4PROJ"):      "name: engineering\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		ScopePath("01JB4PROJ", "01C"): "name: Cozystack\nrank: a\ncreated: 2026-06-04T08:00:00Z\n",
	}
	path := func(id string) string { p, _ := CardPath(id); return p }
	for id, body := range files {
		tree[path(id)] = body
	}
	r := repoWith(t, tree)
	head := r.Head()
	migrated, err := MigrateSchema(r)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v, want a migration", migrated, err)
	}
	c, _ := r.CommitObject(r.Head())
	if len(c.ParentHashes) != 1 || c.ParentHashes[0] != head {
		t.Fatalf("the bump is ONE commit on the old tip, got parents %v", c.ParentHashes)
	}
	read := func(id string) string {
		data, err := r.ReadFile(path(id))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return string(data)
	}
	has := func(id string, want ...string) {
		t.Helper()
		got := read(id)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q missing after the bump:\n%s", id, w, got)
			}
		}
	}
	lacks := func(id string, gone ...string) {
		t.Helper()
		got := read(id)
		for _, g := range gone {
			if strings.Contains(got, g) {
				t.Errorf("%s: %q survived the bump:\n%s", id, g, got)
			}
		}
	}
	// Step 1: the weekly plan is parked, its week gone, everything else kept.
	has(weekOnly, "parked: true", "assignees: [kvaps]", "team: portal", "rank: a", "The body stays.")
	lacks(weekOnly, "week:")
	// Step 2: in work, the dates and sprint kept and the week gone.
	has(weekDated, "start: 2026-10-06", "sprint: 2026-10-05", "rank: b")
	lacks(weekDated, "week:", "parked:")
	// Step 3: the strip is parked; a task under an epic is a task (H1).
	has(strip, "parked: true", "progress: 30", "rank: c")
	has(underEpic, "parked: true", "parent: "+epic)
	// A subtask loses a stray week and rides its parent, unparked.
	lacks(kidWeek, "week:", "parked:")
	has(kidWeek, "parent: "+strip)
	// Untouched, byte for byte: finished work, work in hand, the already
	// parked, a subtask with nothing to strip, the planning card, the review
	// and the project card.
	for _, id := range []string{stripDone, inWork, parked, kid, epic, review, slot} {
		if got := read(id); got != files[id] {
			t.Errorf("%s was rewritten:\n%s", id, got)
		}
	}
	if data, _ := r.ReadFile(BoardPath); !strings.Contains(string(data), "schema: 3") {
		t.Fatalf("board.yaml after the bump:\n%s", data)
	}
	// And the tree reads as a board where every open task is in work or in
	// its backlog, and none carries a week.
	snap, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range snap.Cards {
		if card.Week != "" {
			t.Errorf("%s still reads with a week", card.ItemID)
		}
	}
	// Idempotent: the second run finds nothing to do.
	if again, err := MigrateSchema(r); err != nil || again || r.Head() != c.Hash {
		t.Fatalf("second run: migrated=%v err=%v head moved %v", again, err, r.Head() != c.Hash)
	}
}

// A repository more than one version behind gets every step in the ONE
// commit, and the steps compose on the same card: schema 2's rewrite and
// schema 3's both land, rather than the later step's write — made from the
// tree as it stood before the earlier one — undoing the earlier one's.
func TestSchema3ComposesWithTheStepsBeforeIt(t *testing.T) {
	const id = "01JB4K2E7QZMX3R8V0N5T9WYF1"
	p, _ := CardPath(id)
	r := repoWith(t, map[string]string{
		BoardPath: "schema: 1\ntitle: b\n",
		p:         "---\ntitle: a filed turn\nteam: portal\nweek: 2026-08-24\nprocess: Invoicing\ntask: 01JB4TASK\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n",
	})
	if migrated, err := MigrateSchema(r); err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	data, _ := r.ReadFile(p)
	got := string(data)
	for _, gone := range []string{"process:", "task:", "week:"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s survived the composed bump:\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "parked: true") {
		t.Errorf("schema 3 did not park the weekly-plan card:\n%s", got)
	}
}

// The card codec no longer WRITES a week (K7): a file read with one is
// rewritten without it on its next edit, so no write path can carry a task's
// week forward. The reader still understands the key, which is what lets the
// bump above find the weekly plan.
func TestTheCardCodecDropsAWeek(t *testing.T) {
	in := "---\ntitle: x\nteam: portal\nweek: 2026-10-05\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n"
	cf, err := DecodeCard("01JB4K2E7QZMX3R8V0N5T9WYF2", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if cf.Card.Week != "2026-10-05" {
		t.Fatalf("the reader lost the week it needs for the bump: %+v", cf.Card)
	}
	out, err := EncodeCard(cf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "week:") {
		t.Fatalf("the writer kept a task's week:\n%s", out)
	}
}
