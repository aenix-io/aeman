package migrate

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The migration reads a Projects v2 board through the same loader the server
// uses and writes the git layout: the snapshot is the truth, the event log
// becomes annotation commits, ids are deterministic, and the report names
// everything that did not carry over.

var remotes = server.MapLoader{}

func init() {
	client.InstallProtocol("migtest", server.NewClient(remotes))
}

func newRemote(t *testing.T, suffix string) gitstore.Remote {
	t.Helper()
	url := "migtest://remotes/" + strings.ReplaceAll(t.Name(), "/", "_") + suffix + ".git"
	remotes[url] = memory.NewStorage()
	return gitstore.Remote{URL: url}
}

// fixture is a small board with everything the migration has to handle: a
// team with a sprint pointer, a project with a column and a deadline, a
// process with a task, cards with events (one done, one done without a
// recorded jump), an issue card, a subtask, a review pair, a note without an
// author, and a dangling parent.
func fixture() *boardservicetest.Backend {
	cards := []board.Card{
		{ItemID: "PVTI_proj", Title: board.ProjectStateTitle, Project: "portal", CreatedAt: "2026-06-01T08:00:00Z"},
		{ItemID: "PVTI_epic", Title: board.EpicStateTitle, Epic: "Bugs", Project: "portal", CreatedAt: "2026-06-02T08:00:00Z"},
		{ItemID: "PVTI_dl", Title: board.DeadlineStateTitle, Week: "2026-09-07", Project: "portal", CreatedAt: "2026-06-03T08:00:00Z"},
		{ItemID: "PVTI_proc", Title: board.ProcessStateTitle, Process: "Invoicing", Project: "portal", CreatedAt: "2026-06-04T08:00:00Z"},
		{ItemID: "PVTI_task", Title: board.ProcessTaskTitle, Process: "Invoicing", Recurrence: "month", StartDate: "2026-06-01", Team: "portal",
			Description: "Invoice ACME\nmonthly", CreatedAt: "2026-06-04T09:00:00Z"},
		{ItemID: "PVTI_a", Title: "leak", Team: "portal", Zone: board.ZoneYellow, Progress: 100, Epic: "Bugs", Project: "portal",
			StartDate: "2026-08-24", Day: "2026-08-26", SprintStart: "2026-08-24", Assignees: []string{"kitsunoff"}, Author: "kvaps",
			CreatedAt: "2026-08-24T09:00:00Z", Description: "Memory leak in the portal.",
			Notes: []board.Note{
				{ID: "PVTI_a:5", Body: "reproduced", CreatedAt: "2026-08-24T10:00:00Z", Author: "kitsunoff", Source: "draft"},
				{ID: "PVTI_a:6", Body: "legacy note, nobody signed it", CreatedAt: "2026-08-24T11:00:00Z", Source: "draft"},
			},
			Events: []board.Event{
				{ID: "PVTI_a:1", Kind: board.EventCreated, Actor: "kvaps", At: "2026-08-24T09:00:00Z"},
				{ID: "PVTI_a:2", Kind: board.EventProgress, Actor: "kitsunoff", From: "0", To: "40", At: "2026-08-25T09:00:00Z"},
				{ID: "PVTI_a:3", Kind: board.EventProgress, Actor: "kitsunoff", From: "40", To: "100", At: "2026-08-26T09:00:00Z"},
				{ID: "PVTI_a:4", Kind: board.EventReviewSent, Actor: "kvaps", To: "timur", At: "2026-08-26T10:00:00Z"},
			}},
		{ItemID: "PVTI_b", Title: "done without a jump", Team: "portal", Zone: board.ZoneGray, Progress: 100, CreatedAt: "2026-08-20T09:00:00Z",
			Events: []board.Event{{ID: "PVTI_b:1", Kind: board.EventCreated, Actor: "kvaps", At: "2026-08-20T09:00:00Z"}}},
		{ItemID: "PVTI_sub", Title: "a subtask", Team: "portal", Parent: "PVTI_a", Zone: board.ZoneYellow, CreatedAt: "2026-08-24T12:00:00Z"},
		{ItemID: "PVTI_rev", Title: "review: leak", Team: "portal", ReviewOf: "PVTI_a", Assignees: []string{"timur"}, Zone: board.ZoneRed, CreatedAt: "2026-08-26T10:00:00Z"},
		{ItemID: "PVTI_orphan", Title: "points at a deleted parent", Team: "portal", Parent: "PVTI_gone", CreatedAt: "2026-08-25T09:00:00Z"},
		{ItemID: "PVTI_issue", Title: "an issue card", Team: "portal", URL: "https://github.com/acme/repo/issues/7", Number: 7, Repository: "acme/repo", CreatedAt: "2026-08-22T09:00:00Z"},
	}
	return boardservicetest.New(cards, map[string]board.SprintState{
		"portal": {Current: "2026-08-24", Previous: "2026-08-21", ItemID: "PVTI_team"},
		"":       {ItemID: "PVTI_noteam"},
	})
}

func run(t *testing.T, remote gitstore.Remote, opts Options) Report {
	t.Helper()
	rep, err := Run(context.Background(), fixture(), memory.NewStorage(), remote, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func clone(t *testing.T, remote gitstore.Remote) *gitstore.Repo {
	t.Helper()
	r, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitstore.Options{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

var base = Options{Owner: "acme", Board: 37, Title: "aeman board", Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}}

// M1 — the final tree is the snapshot, whatever the events said.
func TestFinalTreeEqualsSnapshot(t *testing.T) {
	remote := newRemote(t, "")
	rep := run(t, remote, base)
	if !rep.Verified {
		t.Fatal("the migration did not verify its own end state")
	}
	r := clone(t, remote)
	s, err := gitstore.Load(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Cards) != 6 {
		t.Fatalf("cards = %d, want 6 (issue card included, state cards excluded)", len(s.Cards))
	}
	var leak board.Card
	for _, c := range s.Cards {
		if c.Title == "leak" {
			leak = c
		}
	}
	if leak.Progress != 100 || leak.Zone != board.ZoneYellow || leak.Epic != "Bugs" || leak.Project != "portal" || leak.GitHubID != "PVTI_a" {
		t.Fatalf("leak = %+v", leak)
	}
	// doneFrom seeded by the walk Reopen does today: the last jump to ≥100
	// came from 40.
	if leak.DoneFrom != 40 {
		t.Fatalf("doneFrom = %d, want 40", leak.DoneFrom)
	}
	if len(leak.Notes) != 2 || leak.Notes[0].Body != "reproduced" || len(leak.Notes[0].ID) != 26 || leak.Notes[1].Author != "" {
		t.Fatalf("notes = %+v", leak.Notes)
	}
	if len(s.Teams) != 2 || len(s.Projects) != 1 || len(s.Projects[0].Epics) != 1 || len(s.Projects[0].Deadlines) != 1 || len(s.Processes) != 1 || len(s.Processes[0].Tasks) != 1 {
		t.Fatalf("roster: teams=%d projects=%+v processes=%+v", len(s.Teams), s.Projects, s.Processes)
	}
	if s.Board.Title != "aeman board" || s.Board.Schema != gitstore.SchemaVersion {
		t.Fatalf("board = %+v", s.Board)
	}
	if rep.Cards != 6 || rep.Events != 5 || rep.DoneFromSeeded != 1 || rep.DoneWithoutJump != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

// The event log becomes history: one commit per event, dated and authored
// from it, plus the import and the reconcile; the review's reviewer rides an
// Aeman-Change trailer.
func TestEventsBecomeCommits(t *testing.T) {
	remote := newRemote(t, "")
	rep := run(t, remote, base)
	r := clone(t, remote)
	var commits []*object.Commit
	if err := r.Walk(r.Head(), func(c *object.Commit) (bool, error) { commits = append(commits, c); return true, nil }); err != nil {
		t.Fatal(err)
	}
	// import + 5 events + reconcile
	if len(commits) != 7 || rep.Commits != 7 {
		t.Fatalf("%d commits (report %d), want 7", len(commits), rep.Commits)
	}
	// Newest first: reconcile, review-sent, progress 100, progress 40, created(a)... created(b) is earlier.
	if !strings.Contains(commits[0].Message, "Aeman-Migration: acme/37") {
		t.Fatalf("the reconcile commit must carry the migration marker:\n%s", commits[0].Message)
	}
	review := commits[1]
	tr := gitstore.ParseTrailers(review.Message)
	if tr.Action != board.EventReviewSent || tr.Actor != "kvaps" || len(tr.Changes) != 1 || tr.Changes[0].To != "timur" {
		t.Fatalf("review commit trailers = %+v", tr)
	}
	if review.Author.Name != "kvaps" || !review.Author.When.Equal(time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("review commit author = %s at %v", review.Author.Name, review.Author.When)
	}
	if commits[2].Author.Name != "kitsunoff" {
		t.Fatalf("progress commit author = %s", commits[2].Author.Name)
	}
	// The oldest is the import, authored by the server.
	if imp := commits[len(commits)-1]; gitstore.ParseTrailers(imp.Message).Action != "import" || imp.Author.Name != "aeman" {
		t.Fatalf("import commit = %+v", imp.Message)
	}
}

// M2 — a second run against a migrated repository is a no-op without
// --force, and says so.
func TestSecondRunIsNoop(t *testing.T) {
	remote := newRemote(t, "")
	run(t, remote, base)
	head := clone(t, remote).Head()
	rep := run(t, remote, base)
	if !rep.AlreadyMigrated {
		t.Fatal("second run must report the board as already migrated")
	}
	if clone(t, remote).Head() != head {
		t.Fatal("second run changed the remote")
	}
}

// M3 — ids are a function of the source: two runs into two remotes produce
// byte-identical trees; every id-valued field is remapped; a reference to a
// card that is not on the board is cleared and reported.
func TestDeterministicIDsAndRemappedReferences(t *testing.T) {
	a := clone(t, func() gitstore.Remote { r := newRemote(t, "-a"); run(t, r, base); return r }())
	b := clone(t, func() gitstore.Remote { r := newRemote(t, "-b"); run(t, r, base); return r }())
	ca, _ := a.CommitObject(a.Head())
	cb, _ := b.CommitObject(b.Head())
	if ca.TreeHash != cb.TreeHash {
		t.Fatalf("trees differ between runs: %v vs %v", ca.TreeHash, cb.TreeHash)
	}
	s, _ := gitstore.Load(a)
	byGH := map[string]board.Card{}
	for _, c := range s.Cards {
		byGH[c.GitHubID] = c
	}
	if byGH["PVTI_sub"].Parent != byGH["PVTI_a"].ItemID || byGH["PVTI_rev"].ReviewOf != byGH["PVTI_a"].ItemID {
		t.Fatalf("references not remapped: sub.parent=%s rev.reviewOf=%s a=%s", byGH["PVTI_sub"].Parent, byGH["PVTI_rev"].ReviewOf, byGH["PVTI_a"].ItemID)
	}
	if byGH["PVTI_orphan"].Parent != "" {
		t.Fatalf("dangling parent kept: %q", byGH["PVTI_orphan"].Parent)
	}
	for _, c := range s.Cards {
		if len(c.ItemID) != 26 || strings.HasPrefix(c.ItemID, "PVTI") {
			t.Fatalf("id not a ULID: %s", c.ItemID)
		}
	}
	// The task points at its process by name and lives under its directory.
	if s.Processes[0].Tasks[0].Card.Process != "Invoicing" {
		t.Fatalf("task = %+v", s.Processes[0].Tasks[0].Card)
	}
}

// M4 — the report names what was dropped or approximated, and carries the
// id table.
func TestReportNamesDrops(t *testing.T) {
	remote := newRemote(t, "")
	rep := run(t, remote, base)
	if len(rep.IssueCards) != 1 || rep.IssueCards[0] != "PVTI_issue" {
		t.Fatalf("issue cards = %v", rep.IssueCards)
	}
	if rep.UnattributedNotes != 1 {
		t.Fatalf("unattributed notes = %d", rep.UnattributedNotes)
	}
	if len(rep.Dangling) != 1 || !strings.Contains(rep.Dangling[0], "PVTI_gone") {
		t.Fatalf("dangling = %v", rep.Dangling)
	}
	if rep.IDMap["PVTI_a"] == "" || len(rep.IDMap) < 10 {
		t.Fatalf("id map = %d entries", len(rep.IDMap))
	}
	text := rep.String()
	for _, want := range []string{"6 cards", "PVTI_issue", "PVTI_gone", "doneFrom", "unattributed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report text lacks %q:\n%s", want, text)
		}
	}
	// The issue card kept its URL as a link and nothing else of the issue.
	s, _ := gitstore.Load(clone(t, remote))
	for _, c := range s.Cards {
		if c.GitHubID == "PVTI_issue" && (c.Link != "https://github.com/acme/repo/issues/7" || c.URL != "" || c.Number != 0) {
			t.Fatalf("issue card = %+v", c)
		}
	}
}

// --dry-run writes nothing anywhere and still reports.
func TestDryRunWritesNothing(t *testing.T) {
	remote := newRemote(t, "")
	opts := base
	opts.DryRun = true
	rep := run(t, remote, opts)
	if rep.Cards != 6 || rep.Commits == 0 {
		t.Fatalf("dry run report = %+v", rep)
	}
	if _, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitstore.Options{}, 0); err == nil {
		t.Fatal("dry run pushed to the remote")
	}
}
