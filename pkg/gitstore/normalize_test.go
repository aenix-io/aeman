package gitstore

import (
	"context"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// directWriterBoard is a schema-4 board where a DIRECT GIT WRITER (anything
// that commits files without this server) added tasks to empty epics: one
// file with no `rank`, one with a key no server mints, and one with a valid
// key of its own choosing.
func directWriterBoard(t *testing.T) *Repo {
	t.Helper()
	files := map[string]string{
		BoardPath:              "schema: 4\ntitle: b\n",
		TeamPath("01T_PORTAL"): "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n",
	}
	add := func(id, body string) {
		p, err := CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		files[p] = "---\n" + body + "---\n"
	}
	add("01EA000000000000000000000A", "title: A\ntype: epic\nteam: portal\nrank: a\ncreated: 2026-08-20T09:00:00Z\n")
	add("01EB000000000000000000000A", "title: B\ntype: epic\nteam: portal\nrank: b\ncreated: 2026-08-20T09:00:00Z\n")
	add("01N1000000000000000000000A", "title: N1\nteam: portal\nparked: true\nrank: m\ncreated: 2026-08-20T09:00:00Z\n")
	// The direct writer's files: no rank, an invalid one, a valid one.
	add("01B1000000000000000000000A", "title: B1\nparked: true\nparent: 01EB000000000000000000000A\ncreated: 2026-11-01T09:00:00Z\n")
	add("01A1000000000000000000000A", "title: A1\nparked: true\nparent: 01EA000000000000000000000A\nrank: NOT-A-KEY\ncreated: 2026-11-02T09:00:00Z\n")
	add("01A2000000000000000000000A", "title: A2\nparked: true\nparent: 01EA000000000000000000000A\nrank: z\ncreated: 2026-11-03T09:00:00Z\n")
	return repoWith(t, files)
}

func portalQueue(t *testing.T, r *Repo) (string, map[string]string) {
	t.Helper()
	b, err := NewBackend(r, BackendOptions{}).LoadBoard(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	keys := map[string]string{}
	for _, c := range board.NewIndex(b).Queue("portal") {
		titles = append(titles, c.Title)
		keys[c.ItemID] = c.Rank
	}
	return strings.Join(titles, " "), keys
}

// Q11 — A DIRECT WRITER'S TASK WITHOUT A KEY IS PLACED BY THE DEFAULT RULE.
// Normalisation gives the file with no rank and the one with an invalid
// rank their places in their epics' areas (in created order); the valid key
// the writer chose is kept; two replicas normalising the same files write
// the same keys; and a second pass writes nothing.
func TestADirectWritersTasksArePlacedByNormalisation(t *testing.T) {
	one, two := directWriterBoard(t), directWriterBoard(t)
	for _, r := range []*Repo{one, two} {
		n, err := boardservice.New(NewBackend(r, BackendOptions{})).Normalize(context.Background(), "x")
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("normalised %d tasks, want the two without a valid key", n)
		}
	}
	q1, k1 := portalQueue(t, one)
	q2, k2 := portalQueue(t, two)
	// A2's own key "z" is respected, so it is A's area; B1 (no key, made
	// first) lands where the epic order puts an empty epic — after A's task
	// — and A1 (an invalid key) right after A's last parked task, A2.
	if q1 != "N1 A2 A1 B1" {
		t.Fatalf("queue = %s", q1)
	}
	if q1 != q2 {
		t.Fatalf("two replicas disagree: %s / %s", q1, q2)
	}
	for id, k := range k1 {
		if k2[id] != k {
			t.Fatalf("%s: %q on one replica, %q on the other", id, k, k2[id])
		}
	}
	if k1["01A2000000000000000000000A"] != "z" {
		t.Fatalf("the writer's own key was rewritten: %q", k1["01A2000000000000000000000A"])
	}
	n, err := boardservice.New(NewBackend(one, BackendOptions{})).Normalize(context.Background(), "x")
	if err != nil || n != 0 {
		t.Fatalf("a second pass placed %d (%v)", n, err)
	}
}
