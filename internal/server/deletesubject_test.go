package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// A delete is one request and one commit, named for the card it deletes —
// not for the first write it happened to make on the way: deleting a card
// frees its subtasks first, and withdrawing a review puts its task back In
// progress first, and the commit used to read "regroup «child»" or "set
// stage of «task»" for what was a delete.
func TestADeleteIsNamedForTheCardItDeletes(t *testing.T) {
	const today = "2026-08-31"
	const (
		parent = "01DELPARENT000000000000001"
		child  = "01DELCHILD0000000000000001"
		task   = "01DELTASK00000000000000001"
		review = "01DELREVIEW000000000000001"
	)
	for _, tc := range []struct{ name, uid, path, body, want string }{
		{"a card with a subtask", parent, "/api/v1/cards/" + parent, "", "delete «parent»"},
		{"a review card", review, "/api/v1/cards/" + review, "", "delete «review: task»"},
		{"the × on a card with a subtask", parent, "/api/v1/views/all/cards/" + parent + "/actions/remove", `{"intent":"off-board"}`, "delete «parent»"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			dated := fmt.Sprintf("sprint: %s\nstart: %s\nday: %s\n", today, today, today)
			if _, err := r.Commit(gitstore.Action{Name: "import", Summary: "seed"}, []gitstore.FileWrite{
				{Path: gitstore.BoardPath, Data: []byte("schema: 3\ntitle: t\n")},
				{Path: gitstore.TeamPath("01JB4TEAM"), Data: []byte("name: portal\nrank: b\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: " + today + "\n")},
				card(parent, "---\ntitle: parent\nteam: portal\n"+dated+"rank: a\ncreated: 2026-08-20T09:00:00Z\n---\n"),
				card(child, "---\ntitle: child\nteam: portal\nparent: "+parent+"\n"+dated+"rank: b\ncreated: 2026-08-20T09:00:00Z\n---\n"),
				card(task, "---\ntitle: task\nteam: portal\nstage: review\nprogress: 40\n"+dated+"rank: c\ncreated: 2026-08-20T09:00:00Z\n---\n"),
				card(review, "---\ntitle: \"review: task\"\nteam: portal\nassignees: [bob]\nreviewOf: "+task+"\n"+dated+"rank: d\ncreated: 2026-08-21T09:00:00Z\n---\n"),
			}); err != nil {
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

			method := http.MethodDelete
			if tc.body != "" {
				method = http.MethodPost
			}
			if rec := do(t, srv, method, tc.path, tc.body); rec.Code != http.StatusNoContent {
				t.Fatalf("%s: %d %s", tc.path, rec.Code, rec.Body.String())
			}
			srv.store.waitDrained(ctx)
			if got := headCount(t, srv) - before; got != 1 {
				t.Fatalf("one request is one commit; the delete made %d", got)
			}
			repo := srv.gitBE.git.domains[0].Repo
			head, err := repo.CommitObject(repo.Head())
			if err != nil {
				t.Fatal(err)
			}
			if subject, _, _ := strings.Cut(head.Message, "\n"); !strings.Contains(subject, tc.want) {
				t.Fatalf("subject = %q, want it to say %q", subject, tc.want)
			}
		})
	}
}
