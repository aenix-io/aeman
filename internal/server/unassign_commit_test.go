package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// UNASSIGNED on a plan item (K16) is ONE request, so ONE commit — the tasks
// it takes out of work and the reviews that go with them (a review nobody
// has started leaves the board when its task is parked) — carrying the
// request's Action-Id. The reviews used to be deleted synchronously,
// a commit of their own with no action, landing ahead of the request.
func TestUnassigningAPlanItemIsOneCommit(t *testing.T) {
	const today = "2026-08-31"
	remote := gitRemoteN(t, "board")
	r, err := gitstore.Init(memory.NewStorage(), gitstore.Options{Committer: gitstore.Identity{Name: "aeman", Email: "a@x"}})
	if err != nil {
		t.Fatal(err)
	}
	card := func(id, body string) gitstore.FileWrite {
		p, err := gitstore.CardPath(id)
		if err != nil {
			t.Fatal(err)
		}
		return gitstore.FileWrite{Path: p, Data: []byte(body)}
	}
	const epic = "01UNEPIC000000000000000001"
	files := []gitstore.FileWrite{
		{Path: gitstore.BoardPath, Data: []byte("schema: 3\ntitle: t\n")},
		{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: " + today + "\n")},
		card(epic, "---\ntitle: ТС-2\ntype: epic\nrank: a\ncreated: 2026-08-20T09:00:00Z\n---\n"),
	}
	const tasks = 3
	for i := range tasks {
		id := fmt.Sprintf("01UNTASK%018d", i)
		extra := ""
		if i == 0 {
			extra = "stage: review\nprogress: 40\n"
		}
		files = append(files, card(id, fmt.Sprintf(
			"---\ntitle: task %d\nteam: portal\nassignees: [kvaps]\nparent: %s\nsprint: %s\nstart: %s\nday: %s\n%srank: b%d\ncreated: 2026-08-20T09:00:00Z\n---\n",
			i, epic, today, today, today, extra, i)))
	}
	files = append(files, card("01UNREVIEW0000000000000001", fmt.Sprintf(
		"---\ntitle: \"review: task 0\"\nteam: portal\nassignees: [bob]\nreviewOf: 01UNTASK%018d\nsprint: %s\nstart: %s\nday: %s\nrank: c\ncreated: 2026-08-21T09:00:00Z\n---\n",
		0, today, today, today)))
	if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, files); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	srv := gitModeServer(t, remote)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv.store.waitDrained(ctx)
	before := headCount(t, srv)

	rec := do(t, srv, http.MethodPost, "/api/v1/cards/"+epic+"/actions/unassign-work", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unassign-work: %d %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, fmt.Sprintf(`"changed":%d`, tasks)) {
		t.Fatalf("every task must go, or the count below proves nothing: %s", body)
	}
	srv.store.waitDrained(ctx)

	if got := headCount(t, srv) - before; got != 1 {
		t.Fatalf("one request is one commit; UNASSIGNED made %d", got)
	}
	repo := srv.gitBE.git.domains[0].Repo
	head, err := repo.CommitObject(repo.Head())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(head.Message, "Aeman-Action-Id: ") {
		t.Fatalf("the commit carries no action id:\n%s", head.Message)
	}
	tree, err := head.Tree()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := gitstore.CardPath("01UNREVIEW0000000000000001")
	if _, err := tree.File(p); err == nil {
		t.Fatal("the review nobody started is still on the board")
	} else if err != object.ErrFileNotFound {
		t.Fatal(err)
	}
}
