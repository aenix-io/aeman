package gitstore

import (
	"bytes"
	"errors"
	"testing"
)

// The roster — teams, projects, epics, deadlines, processes — is
// configuration that used to hide in state cards. Here each is a file: a
// name where it has one, a rank, a created time, and the fields the domain
// gives it. Every path carries identity only.

func TestRosterPaths(t *testing.T) {
	cases := map[string]string{
		TeamPath("01JB4TEAM"):                "teams/01JB4TEAM.yaml",
		ProjectPath("01JB4PROJ"):             "projects/01JB4PROJ/project.yaml",
		EpicPath("01JB4PROJ", "01JB4EPIC"):   "projects/01JB4PROJ/epics/01JB4EPIC.yaml",
		DeadlinePath("01JB4PROJ", "01JB4DL"): "projects/01JB4PROJ/deadlines/01JB4DL.yaml",
		ProcessPath("01JB4PROC"):             "processes/01JB4PROC/process.yaml",
		TaskPath("01JB4PROC", "01JB4TASK"):   "processes/01JB4PROC/tasks/01JB4TASK.md",
		BoardPath:                            "board.yaml",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("path %q, want %q", got, want)
		}
	}
}

// ParsePath is the inverse: from a changed path (a remote commit's diff)
// to what it is, so the cache reloads exactly the touched object.
func TestParsePathClassifies(t *testing.T) {
	cases := []struct {
		path string
		kind PathKind
		ids  []string
	}{
		{"cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md", PathCard, []string{"01JB4K2E7QZMX3R8V0N5T9WYC1"}},
		{"teams/01JB4TEAM.yaml", PathTeam, []string{"01JB4TEAM"}},
		{"teams/_.yaml", PathTeam, []string{"_"}},
		{"projects/01JB4PROJ/project.yaml", PathProject, []string{"01JB4PROJ"}},
		{"projects/01JB4PROJ/epics/01JB4EPIC.yaml", PathEpic, []string{"01JB4PROJ", "01JB4EPIC"}},
		{"projects/01JB4PROJ/deadlines/01JB4DL.yaml", PathDeadline, []string{"01JB4PROJ", "01JB4DL"}},
		{"processes/01JB4PROC/process.yaml", PathProcess, []string{"01JB4PROC"}},
		{"processes/01JB4PROC/tasks/01JB4TASK.md", PathTask, []string{"01JB4PROC", "01JB4TASK"}},
		{"board.yaml", PathBoard, nil},
		{"README.md", PathUnknown, nil},
		{"cards/c/1/notes.txt", PathUnknown, nil},
		{"cards/c/01JB4K2E7QZMX3R8V0N5T9WYC1.md", PathUnknown, nil}, // one level short
	}
	for _, c := range cases {
		kind, ids := ParsePath(c.path)
		if kind != c.kind {
			t.Fatalf("%s: kind %v, want %v", c.path, kind, c.kind)
		}
		if len(ids) != len(c.ids) {
			t.Fatalf("%s: ids %v, want %v", c.path, ids, c.ids)
		}
		for i := range ids {
			if ids[i] != c.ids[i] {
				t.Fatalf("%s: ids %v, want %v", c.path, ids, c.ids)
			}
		}
	}
}

func TestTeamFileRoundTrip(t *testing.T) {
	in := TeamFile{Name: "portal", Rank: "a0", Created: "2026-06-01T08:00:00Z", Sprint: SprintPointer{Current: "2026-08-24", Previous: "2026-08-21"}}
	data, err := EncodeTeam(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "name: portal\nrank: a0\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-21\n"
	if string(data) != want {
		t.Fatalf("team file:\n%s\nwant:\n%s", data, want)
	}
	out, err := DecodeTeam(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Rank != in.Rank || out.Sprint != in.Sprint || out.Created != in.Created {
		t.Fatalf("round trip: %+v", out)
	}
	// The no-team group: no name, no sprint yet — nothing but rank and created.
	bare, _ := EncodeTeam(TeamFile{Rank: "a", Created: "2026-06-01T08:00:00Z"})
	if bytes.Contains(bare, []byte("name:")) || bytes.Contains(bare, []byte("sprint:")) {
		t.Fatalf("empty fields written:\n%s", bare)
	}
}

func TestProjectEpicDeadlineProcessRoundTrip(t *testing.T) {
	p, _ := EncodeProject(ProjectFile{Name: "portal", Rank: "b", Created: "2026-06-01T08:00:00Z"})
	if string(p) != "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\n" {
		t.Fatalf("project:\n%s", p)
	}
	pf, err := DecodeProject(p)
	if err != nil || pf.Name != "portal" || pf.Rank != "b" {
		t.Fatalf("project decode: %+v %v", pf, err)
	}

	e, _ := EncodeEpic(EpicFile{Name: "Bugs", Rank: "c", Created: "2026-06-02T08:00:00Z"})
	ef, err := DecodeEpic(e)
	if err != nil || ef.Name != "Bugs" || ef.Rank != "c" {
		t.Fatalf("epic decode: %+v %v", ef, err)
	}

	d, _ := EncodeDeadline(DeadlineFile{Week: "2026-09-07", Created: "2026-06-03T08:00:00Z"})
	if string(d) != "week: 2026-09-07\ncreated: 2026-06-03T08:00:00Z\n" {
		t.Fatalf("deadline:\n%s", d)
	}
	df, err := DecodeDeadline(d)
	if err != nil || df.Week != "2026-09-07" {
		t.Fatalf("deadline decode: %+v %v", df, err)
	}

	pr, _ := EncodeProcess(ProcessFile{Name: "Invoicing", Project: "portal", Paused: true, Rank: "d", Created: "2026-06-04T08:00:00Z"})
	if !bytes.Contains(pr, []byte("paused: true\n")) || !bytes.Contains(pr, []byte("project: portal\n")) {
		t.Fatalf("process:\n%s", pr)
	}
	prf, err := DecodeProcess(pr)
	if err != nil || !prf.Paused || prf.Project != "portal" || prf.Name != "Invoicing" {
		t.Fatalf("process decode: %+v %v", prf, err)
	}
	running, _ := EncodeProcess(ProcessFile{Name: "Blog", Rank: "e", Created: "2026-06-04T08:00:00Z"})
	if bytes.Contains(running, []byte("paused")) || bytes.Contains(running, []byte("project")) {
		t.Fatalf("false/empty written:\n%s", running)
	}
}

// board.yaml carries the layout version. A newer schema than this server
// knows is refused outright; an older (or missing) one is reported so the
// server can migrate it in a commit.
func TestBoardFileSchema(t *testing.T) {
	data, err := EncodeBoard(BoardFile{Schema: SchemaVersion, Title: "aeman board"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "schema: 1\ntitle: aeman board\n" {
		t.Fatalf("board file:\n%s", data)
	}
	b, err := DecodeBoard(data)
	if err != nil || b.Title != "aeman board" || b.Schema != SchemaVersion {
		t.Fatalf("decode: %+v %v", b, err)
	}
	if _, err := DecodeBoard([]byte("schema: 99\ntitle: future\n")); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("newer schema: err = %v, want ErrSchemaNewer", err)
	}
	old, err := DecodeBoard([]byte("title: legacy\n"))
	if err != nil || old.Schema != 0 {
		t.Fatalf("missing schema must decode as 0 for migration: %+v %v", old, err)
	}
}

// The same rule as for cards: a key this server does not know survives a
// rewrite untouched.
func TestRosterKeepsUnknownKeys(t *testing.T) {
	in := []byte("name: portal\nrank: a0\nfutureKey: keep me\ncreated: 2026-06-01T08:00:00Z\n")
	tf, err := DecodeTeam(in)
	if err != nil {
		t.Fatal(err)
	}
	tf.Rank = "a1"
	out, err := EncodeTeam(tf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("futureKey: keep me\n")) || !bytes.Contains(out, []byte("rank: a1\n")) {
		t.Fatalf("rewrite lost something:\n%s", out)
	}
}
