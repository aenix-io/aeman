package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// The store over several domains, and a sync that finds its work on disk:
// a write that re-files a card commits to both repositories under one
// action id and both push; another replica adopts both; what a run left
// unpushed when it stopped is pushed — re-applied if need be — by the next.

var gitTestOpts = gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}}

// gitRemoteN is gitRemote with a distinct name, for tests with more than
// one remote.
func gitRemoteN(t *testing.T, name string) gitstore.Remote {
	t.Helper()
	url := "gittest://remotes/" + strings.ReplaceAll(t.Name(), "/", "_") + "-" + name + ".git"
	gitTestRemotes[url] = memory.NewStorage()
	return gitstore.Remote{URL: url}
}

// seedClosedRemote pushes a closed domain: the project "secret" and its
// column "Risk", no teams.
func seedClosedRemote(t *testing.T, remote gitstore.Remote) {
	t.Helper()
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 1\ntitle: closed\n")},
		{Path: gitstore.ProjectPath("01JB4PROJSECRET"), Data: []byte("name: secret\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: gitstore.EpicPath("01JB4PROJSECRET", "01JB4EPICRISK"), Data: []byte("name: Risk\nrank: a\ncreated: 2026-06-01T08:00:00Z\n")},
		{Path: "cards/c/3/01JB4K2E7QZMX3R8V0N5T9WYC3.md", Data: []byte("---\ntitle: three-closed\nteam: portal\nproject: secret\nepic: Risk\nzone: yellow\nprogress: 20\nrank: c\ncreated: 2026-08-26T09:16:03Z\n---\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
}

// seedRemoteFiles pushes an arbitrary set of files as one import commit.
func seedRemoteFiles(t *testing.T, remote gitstore.Remote, files map[string]string) {
	t.Helper()
	r, err := gitstore.Init(memory.NewStorage(), gitTestOpts)
	if err != nil {
		t.Fatal(err)
	}
	writes := make([]gitstore.FileWrite, 0, len(files))
	for p, data := range files {
		writes = append(writes, gitstore.FileWrite{Path: p, Data: []byte(data)})
	}
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, writes); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
}

// tornMoveRemotes seed a shared and a closed domain with the team "portal"
// declared in both (the closed one older: an alias in shared) and the card
// A1 present in both, the closed copy saying it moved from shared (a ghost
// in shared).
func tornMoveRemotes(t *testing.T) (gitstore.Remote, gitstore.Remote) {
	t.Helper()
	shared, closed := gitRemoteN(t, "shared"), gitRemoteN(t, "closed")
	seedRemoteFiles(t, shared, map[string]string{
		gitstore.BoardPath:                        "schema: 1\ntitle: t\n",
		gitstore.TeamPath("_"):                    "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		gitstore.TeamPath("01JB4TEAMNEW"):         "name: portal\nrank: b\ncreated: 2026-07-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		"cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md": "---\ntitle: moving\nteam: portal\nzone: yellow\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n",
	})
	seedRemoteFiles(t, closed, map[string]string{
		gitstore.BoardPath:                        "schema: 1\ntitle: closed\n",
		gitstore.TeamPath("01JB4TEAMOLD"):         "name: portal\nrank: z\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		gitstore.ProjectPath("01JB4PROJSECRET"):   "name: secret\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md": "---\ntitle: moving\nteam: portal\nproject: secret\nzone: yellow\nrank: a\ncreated: 2026-08-26T09:14:03Z\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
	})
	return shared, closed
}

// The activity feed in git mode is read from the commits: a field change is
// an event with the request's actor and the commit's time, notes ride along,
// and a full clone reports no horizon.
func TestCardLogFromCommits(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv := gitModeServer(t, remote)
	uid := cardUID(t, srv, "tester", "one")
	if rec := do(t, srv, "PATCH", "/api/v1/cards/"+uid, `{"progress":90}`); rec.Code != 200 {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, "POST", "/api/v1/cards/"+uid+"/notes", `{"text":"looked into it"}`); rec.Code != 201 {
		t.Fatalf("POST note: %d %s", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	rec := do(t, srv, "GET", "/api/v1/cards/"+uid+"/log", "")
	if rec.Code != 200 {
		t.Fatalf("GET log: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			Type, Kind, From, To, Actor, Text string
		} `json:"items"`
		TruncatedBefore string `json:"truncatedBefore"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var progress, note bool
	for _, it := range got.Items {
		if it.Type == "event" && it.Kind == "progress" && it.From == "40" && it.To == "90" && it.Actor == "tester" {
			progress = true
		}
		if it.Type == "note" && it.Text == "looked into it" {
			note = true
		}
	}
	if !progress || !note {
		t.Fatalf("log = %s", rec.Body.String())
	}
	if got.TruncatedBefore != "" {
		t.Fatalf("a full clone reports a horizon: %s", got.TruncatedBefore)
	}
}

// processRemote seeds a board with the process "weekly" and one task due
// every week from this week's Monday, owned by kvaps.
func processRemote(t *testing.T, remote gitstore.Remote) (taskID, monday string) {
	t.Helper()
	monday = board.MondayOf(board.TodayIso())
	taskID = "01JB4TASK0000000000000000A"
	seedRemoteFiles(t, remote, map[string]string{
		gitstore.BoardPath:                      "schema: 1\ntitle: t\n",
		gitstore.TeamPath("_"):                  "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		gitstore.TeamPath("01JB4TEAM"):          "name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: " + monday + "\n",
		gitstore.ProcessPath("01JB4PROCWEEKLY"): "name: weekly\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		// A task's name is the first line of its body, marked — the title
		// field is the marker that hides it from the card rows.
		gitstore.TaskPath("01JB4PROCWEEKLY", taskID): "---\ntitle: aeman:process-task\nteam: portal\nassignees: [kvaps]\nrecurrence: week\nstart: " + monday + "\nrank: a\ncreated: 2026-06-01T08:00:00Z\n---\n\n# weekly report\n",
	})
	return taskID, monday
}

// The process sweep rides the fetch tick, as the server identity: after a
// sync, the week's due turns are filed through the store — one action, one
// commit, pushed — with deterministic ids, so a second replica sweeping the
// same minute writes the same path and its create is dropped on re-apply:
// one iteration on the remote, not two.
func TestSweepOnFetchTickFilesThisWeeksTurnsOnce(t *testing.T) {
	remote := gitRemote(t)
	taskID, monday := processRemote(t, remote)
	a, _ := gitStore(t, remote)
	b, _ := gitStore(t, remote)
	ctx := context.Background()
	if _, err := a.LoadBoard(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.LoadBoard(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	want := gitstore.IterationID(taskID, monday)
	if err := a.tick(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if err := b.tick(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	for name, be := range map[string]*storeBackend{"a": a, "b": b} {
		bd, _ := be.LoadBoard(ctx, "acme")
		turns := board.Iterations(bd, taskID)
		if len(turns) != 1 || turns[0].ItemID != want || turns[0].Week != monday {
			t.Fatalf("%s serves %d turn(s) %+v, want one with id %s", name, len(turns), turns, want)
		}
		if n, _ := be.git.primary().Unpushed(); n != 0 {
			t.Fatalf("%s left %d unpushed commit(s) after the tick", name, n)
		}
	}
	check, err := gitstore.Clone(ctx, memory.NewStorage(), remote, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := gitstore.CardPath(want)
	data, err := check.ReadFile(p)
	if err != nil {
		t.Fatalf("the turn is not on the remote: %v", err)
	}
	if !strings.Contains(string(data), "task: "+taskID) || !strings.Contains(string(data), "week: "+monday) {
		t.Fatalf("turn file:\n%s", data)
	}
	c, _ := check.CommitObject(check.Head())
	if tr := gitstore.ParseTrailers(c.Message); tr.Action != "sweep" || tr.Actor != "" {
		t.Fatalf("sweep commit trailers = %+v, want action sweep by the server", tr)
	}
	// G6 — the sweep is the server's own work: authored by the server.
	if c.Author.Name != gitTestOpts.Committer.Name || c.Author.Email != gitTestOpts.Committer.Email {
		t.Fatalf("sweep commit author = %s <%s>, want the server", c.Author.Name, c.Author.Email)
	}
	// Another tick files nothing new.
	if err := a.tick(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if bd, _ := a.LoadBoard(ctx, "acme"); len(board.Iterations(bd, taskID)) != 1 {
		t.Fatal("a second tick filed another turn")
	}
}

// On-demand deepening: a log cut by the horizon deepens to the card's
// creation, bounded by --history-max; a log that reaches the card's creation
// needs nothing; a card older than the cap deepens to the cap.
func TestDeepenSinceDecision(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	horizon := now.Add(-8 * 7 * 24 * time.Hour)
	maxBack := 365 * 24 * time.Hour
	if _, ok := deepenSince(time.Time{}, now.Add(-100*24*time.Hour), maxBack, now); ok {
		t.Fatal("a whole history needs no deepening")
	}
	if _, ok := deepenSince(horizon, horizon.Add(24*time.Hour), maxBack, now); ok {
		t.Fatal("a card created inside the horizon needs no deepening")
	}
	created := now.Add(-200 * 24 * time.Hour)
	since, ok := deepenSince(horizon, created, maxBack, now)
	if !ok || !since.Equal(created) {
		t.Fatalf("deepen to the card's creation: %v %v", since, ok)
	}
	ancient := now.Add(-3 * 365 * 24 * time.Hour)
	since, ok = deepenSince(horizon, ancient, maxBack, now)
	if !ok || !since.Equal(now.Add(-maxBack)) {
		t.Fatalf("deepen to the cap: %v %v", since, ok)
	}
	if _, ok := deepenSince(now.Add(-maxBack), ancient, maxBack, now); ok {
		t.Fatal("already at the cap: nothing more to fetch")
	}
}

// G26 — a push that cannot land is visible: past --unpushed-warn the health
// status turns degraded, and clears once the commit is on the remote.
func TestHealthzDegradesPastUnpushedWarn(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	srv, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git: &GitConfig{Repos: []RepoSpec{{Name: "board", URL: remote.URL}}, DataDir: t.TempDir(),
			Committer: gitstore.Identity{Name: "aeman", Email: "aeman@test"}, UnpushedWarn: time.Nanosecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.apiTokens = func(*http.Request) (string, string, error) { return "", "tester", nil }
	srv.gitBE.git.pushDelay = 0 // nothing pushes until asked
	if rec := do(t, srv, "GET", "/api/healthz", ""); !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health before any write: %s", rec.Body.String())
	}
	if rec := do(t, srv, "POST", "/api/v1/cards", `{"title":"late","team":"portal","zone":"planned","dates":{"start":"2026-08-27"}}`); rec.Code != 201 {
		t.Fatalf("POST /cards: %d %s", rec.Code, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	if rec := do(t, srv, "GET", "/api/healthz", ""); !strings.Contains(rec.Body.String(), `"status":"degraded"`) {
		t.Fatalf("health with an old unpushed commit: %s", rec.Body.String())
	}
	if err := srv.drainAndPush(ctx); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, srv, "GET", "/api/healthz", ""); !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health after the push: %s", rec.Body.String())
	}
}

// G18 through the server — a repository written by an older server is
// brought to the current schema at start-up, in one commit, before it is
// served.
func TestGitModeMigratesOlderSchemaAtStartup(t *testing.T) {
	remote := gitRemote(t)
	seedRemoteFiles(t, remote, map[string]string{
		gitstore.BoardPath:     "title: old\n",
		gitstore.TeamPath("_"): "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
	})
	srv := gitModeServer(t, remote)
	primary := srv.gitBE.git.primary()
	data, err := primary.ReadFile(gitstore.BoardPath)
	if err != nil || !strings.Contains(string(data), "schema: 1") || !strings.Contains(string(data), "title: old") {
		t.Fatalf("board.yaml after start-up = %q (%v)", data, err)
	}
	c, _ := primary.CommitObject(primary.Head())
	if gitstore.ParseTrailers(c.Message).Action != "schema" {
		t.Fatalf("head commit = %q, want the schema migration", c.Message)
	}
	if rec := do(t, srv, "GET", "/api/v1/board", ""); rec.Code != 200 {
		t.Fatalf("GET /board after migration: %d %s", rec.Code, rec.Body.String())
	}
}

// Health names what the merge had to resolve: the alias team and the ghost.
func TestHealthzReportsAliasesAndGhosts(t *testing.T) {
	// The torn move is there from the start; the colliding team is not — two
	// repositories that already collide are refused at start-up (G38), so a
	// running server only ever meets a collision that arrives behind its
	// back: a direct push to one of its repositories.
	shared, closed := gitRemoteN(t, "shared"), gitRemoteN(t, "closed")
	seedRemoteFiles(t, shared, map[string]string{
		gitstore.BoardPath:                        "schema: 1\ntitle: t\n",
		gitstore.TeamPath("_"):                    "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		gitstore.TeamPath("01JB4TEAMNEW"):         "name: portal\nrank: b\ncreated: 2026-07-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n",
		"cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md": "---\ntitle: moving\nteam: portal\nzone: yellow\nrank: a\ncreated: 2026-08-26T09:14:03Z\n---\n",
	})
	seedRemoteFiles(t, closed, map[string]string{
		gitstore.BoardPath:                        "schema: 1\ntitle: closed\n",
		gitstore.ProjectPath("01JB4PROJSECRET"):   "name: secret\nrank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md": "---\ntitle: moving\nteam: portal\nproject: secret\nzone: yellow\nrank: a\ncreated: 2026-08-26T09:14:03Z\nmovedFrom: shared\nmovedAt: 2026-08-28T10:00:00Z\n---\n",
	})
	srv := gitModeServerOver(t, fakeAccess{byLogin: map[string]*domainRights{"bob": rightsOn([]string{"shared", "closed"}, []string{"shared", "closed"})}}, shared, closed)
	if rec := doAs(t, srv, "bob", "GET", "/api/v1/board", ""); rec.Code != 200 {
		t.Fatalf("GET /board: %d %s", rec.Code, rec.Body.String())
	}
	// An older "portal" lands in closed by a direct push; the fetch tick
	// brings it in. The merge keeps serving — the older declaration wins,
	// the newer is the alias health names — instead of falling over.
	ctx := context.Background()
	other, err := gitstore.Clone(ctx, memory.NewStorage(), closed, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Commit(gitstore.Action{Name: "import", Summary: "a second portal"}, []gitstore.FileWrite{
		{Path: gitstore.TeamPath("01JB4TEAMOLD"), Data: []byte("name: portal\nrank: z\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := other.Push(ctx, closed); err != nil {
		t.Fatal(err)
	}
	if err := srv.gitBE.syncNow(ctx, storeKey(srv.gitBoard())); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, "GET", "/api/healthz", "")
	body := rec.Body.String()
	for _, want := range []string{`"aliases"`, `"portal"`, `"01JB4TEAMNEW"`, `"ghosts"`, `"01JB4K2E7QZMX3R8V0N5T9WYA1"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("healthz lacks %s: %s", want, body)
		}
	}
}

// G22 — the maintenance tick removes a torn move's ghost once the destination
// has landed, commits the removal in the ghost's domain and pushes it; the
// board keeps serving the card once, from its current domain.
func TestMaintenanceSweepsGhosts(t *testing.T) {
	shared, closed := tornMoveRemotes(t)
	be := gitStoreOver(t, shared, closed)
	ctx := context.Background()
	if _, err := be.LoadBoard(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	n, err := be.maintainNow(ctx, "acme")
	if err != nil || n != 1 {
		t.Fatalf("swept %d (%v), want 1", n, err)
	}
	p, _ := gitstore.CardPath("01JB4K2E7QZMX3R8V0N5T9WYA1")
	if _, err := be.git.domains[0].Repo.ReadFile(p); err == nil {
		t.Fatal("ghost still in the shared clone")
	}
	if n, _ := be.git.domains[0].Repo.Unpushed(); n != 0 {
		t.Fatalf("the sweep left %d unpushed commit(s): maintenance must push what it removed", n)
	}
	check, err := gitstore.Clone(ctx, memory.NewStorage(), shared, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := check.ReadFile(p); err == nil {
		t.Fatal("ghost still on the shared remote")
	}
	bd, _ := be.LoadBoard(ctx, "acme")
	count := 0
	for _, c := range bd.Cards {
		if c.ItemID == "01JB4K2E7QZMX3R8V0N5T9WYA1" {
			count++
			if c.Domain != "closed" {
				t.Fatalf("card served from %q after the sweep", c.Domain)
			}
		}
	}
	if count != 1 {
		t.Fatalf("card served %d times after the sweep", count)
	}
	// Nothing left: the next tick is a no-op.
	if n, _ := be.maintainNow(ctx, "acme"); n != 0 {
		t.Fatalf("second sweep removed %d", n)
	}
}

// gitStoreOver is one replica over several remotes, primary first, named
// shared / closed / third.
func gitStoreOver(t *testing.T, remotes ...gitstore.Remote) *storeBackend {
	t.Helper()
	names := []string{"shared", "closed", "third"}
	domains := make([]gitDomain, 0, len(remotes))
	for i, remote := range remotes {
		repo, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitTestOpts, 0)
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, gitDomain{Domain: gitstore.Domain{Name: names[i], Repo: repo}, remote: remote})
	}
	be := newGitBackend(newBoardStore(), domains, gitOptions{})
	be.git.pushDelay = 0 // tests drive the sync by hand
	return be
}

func actionCtx(id, name string) context.Context {
	return withAction(board.WithActor(context.Background(), "kvaps"), id, name)
}

// G14/G22 through the store — a card filed under the closed project moves:
// both clones commit under the request's action id, the cache knows the new
// domain at once, one sync pushes both, and a replica that was watching
// adopts both and serves the card once, from the closed domain.
func TestGitTwoDomainsMovePushesBothAndIsAdopted(t *testing.T) {
	shared, closed := gitRemoteN(t, "shared"), gitRemoteN(t, "closed")
	seedGitRemote(t, shared)
	seedClosedRemote(t, closed)
	a := gitStoreOver(t, shared, closed)
	watcher := gitStoreOver(t, shared, closed)
	ctx := actionCtx("01JB4KA0M2P4R6T8V0X2Z4B6E1", "update")
	if _, err := watcher.LoadBoard(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	bd, err := a.LoadBoard(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Projects) != 1 || bd.Projects[0] != "secret" {
		t.Fatalf("projects = %v, want the closed domain's project merged in", bd.Projects)
	}
	card := cardByTitle(bd, "one")
	if card.Domain != "shared" {
		t.Fatalf("card domain = %q before the move", card.Domain)
	}
	if err := a.SetProject(ctx, bd, card, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetEpic(ctx, bd, card, "Risk"); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, a)

	sharedRepo, closedRepo := a.git.domains[0].Repo, a.git.domains[1].Repo
	p, _ := gitstore.CardPath(card.ItemID)
	if _, err := closedRepo.ReadFile(p); err != nil {
		t.Fatalf("moved card not in the closed clone: %v", err)
	}
	if _, err := sharedRepo.ReadFile(p); err == nil {
		t.Fatal("moved card still in the shared clone")
	}
	cc, _ := closedRepo.CommitObject(closedRepo.Head())
	sc, _ := sharedRepo.CommitObject(sharedRepo.Head())
	if id := gitstore.ParseTrailers(cc.Message).ActionID; id != "01JB4KA0M2P4R6T8V0X2Z4B6E1" || gitstore.ParseTrailers(sc.Message).ActionID != id {
		t.Fatalf("action ids: closed %q shared %q", id, gitstore.ParseTrailers(sc.Message).ActionID)
	}
	// The cache learnt the new domain from the commit, not from a reload.
	now, _ := a.LoadBoard(ctx, "acme")
	if c := cardByTitle(now, "one"); c.Domain != "closed" || c.Project != "secret" || c.Epic != "Risk" {
		t.Fatalf("cached card after move = domain %q project %q epic %q", c.Domain, c.Project, c.Epic)
	}

	if err := a.syncNow(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	for _, d := range a.git.domains {
		if n, _ := d.Repo.Unpushed(); n != 0 {
			t.Fatalf("%s still has %d unpushed commits", d.Name, n)
		}
	}
	if err := watcher.syncNow(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	seen, _ := watcher.LoadBoard(ctx, "acme")
	count := 0
	for _, c := range seen.Cards {
		if c.Title == "one" {
			count++
			if c.Domain != "closed" || c.Project != "secret" {
				t.Fatalf("adopted card = domain %q project %q", c.Domain, c.Project)
			}
		}
	}
	if count != 1 {
		t.Fatalf("the watcher serves the moved card %d times, want once", count)
	}
}

// A commit the replica made but never pushed — it stopped first — is found
// on the branch by the next run: health counts it, and the sync pushes it,
// re-applied because the remote moved meanwhile.
func TestGitSyncPushesCommitsFromBeforeARestart(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	ctx := actionCtx("01JB4KA0M2P4R6T8V0X2Z4B6F1", "update")

	st := memory.NewStorage()
	repo, err := gitstore.Clone(context.Background(), st, remote, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	first := newGitBackend(newBoardStore(), []gitDomain{{Domain: gitstore.Domain{Name: "board", Repo: repo}, remote: remote}}, gitOptions{})
	first.git.pushDelay = 0
	bd, _ := first.LoadBoard(ctx, "acme")
	if err := first.SetProgress(ctx, bd, cardByTitle(bd, "one"), 90); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, first) // committed, never pushed

	other, _ := gitStore(t, remote)
	bd2, _ := other.LoadBoard(ctx, "acme")
	if err := other.SetProgress(ctx, bd2, cardByTitle(bd2, "two"), 10); err != nil {
		t.Fatal(err)
	}
	waitQueue(t, other)
	if err := other.syncNow(ctx, "acme"); err != nil {
		t.Fatal(err)
	}

	// "Restart": a store over the same clone, with no memory of the commit.
	second := newGitBackend(newBoardStore(), []gitDomain{{Domain: gitstore.Domain{Name: "board", Repo: gitstore.Open(st, gitTestOpts)}, remote: remote}}, gitOptions{})
	second.git.pushDelay = 0
	if age := second.unpushedAge("acme"); age <= 0 {
		t.Fatal("the pre-restart commit must count as unpushed")
	}
	if err := second.syncNow(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if n, _ := second.git.primary().Unpushed(); n != 0 {
		t.Fatalf("%d commits still unpushed after the sync", n)
	}
	if age := second.unpushedAge("acme"); age != 0 {
		t.Fatalf("unpushed age after push = %v", age)
	}
	check, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitTestOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := check.ReadFile("cards/a/1/01JB4K2E7QZMX3R8V0N5T9WYA1.md")
	two, _ := check.ReadFile("cards/b/2/01JB4K2E7QZMX3R8V0N5T9WYB2.md")
	if !strings.Contains(string(one), "progress: 90") || !strings.Contains(string(two), "progress: 10") {
		t.Fatalf("remote lacks a write:\none: %s\ntwo: %s", one, two)
	}
}
