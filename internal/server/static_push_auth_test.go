package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitserver "github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

type changingCredential struct {
	mu           sync.Mutex
	token, login string
}

func (c *changingCredential) set(token, login string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.login = token, login
}

func (c *changingCredential) Token(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token, nil
}

func (c *changingCredential) Login(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.login, nil
}

func (c *changingCredential) TokenAndLogin(context.Context) (string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token, c.login, nil
}

type receivePackAuthRecorder struct {
	transport.Transport
	mu     sync.Mutex
	tokens []string
}

func (r *receivePackAuthRecorder) NewReceivePackSession(ep *transport.Endpoint, auth transport.AuthMethod) (transport.ReceivePackSession, error) {
	token := ""
	if basic, ok := auth.(*githttp.BasicAuth); ok {
		token = basic.Password
	}
	r.mu.Lock()
	r.tokens = append(r.tokens, token)
	r.mu.Unlock()
	return r.Transport.NewReceivePackSession(ep, auth)
}

func (r *receivePackAuthRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens = nil
}

func (r *receivePackAuthRecorder) pushedTokens() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.tokens...)
}

func TestStaticPushAuthAndChangingRequestActorStayIndependent(t *testing.T) {
	const (
		tokenA = "test-token-a"
		tokenB = "test-token-b"
	)
	url := "authgittest://remotes/" + strings.ReplaceAll(t.Name(), "/", "_") + ".git"
	loader := gitserver.MapLoader{url: memory.NewStorage()}
	recorder := &receivePackAuthRecorder{Transport: gitserver.NewClient(loader)}
	client.InstallProtocol("authgittest", recorder)
	remote := gitstore.Remote{URL: url}
	seedGitRemote(t, remote)
	recorder.reset()

	credential := &changingCredential{}
	credential.set(tokenA, "alice")
	dataDir := t.TempDir()
	committer := gitstore.Identity{Name: "server-committer", Email: "server@example.test"}

	start := func() *Server {
		t.Helper()
		startupToken, err := credential.Token(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		srv, err := New(Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			CLI:    credential,
			Git: &GitConfig{
				Repos:     []RepoSpec{{Name: "board", URL: url}},
				Token:     startupToken,
				DataDir:   dataDir,
				Committer: committer,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		srv.gitBE.git.pushDelay = 0
		srv.handler = conforms(t, srv.handler)
		return srv
	}

	mutateAndPush := func(srv *Server, uid string, progress int) {
		t.Helper()
		rec := do(t, srv, http.MethodPatch, "/api/v1/cards/"+uid, "{\"progress\":"+strconv.Itoa(progress)+"}")
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH progress %d: %d %s", progress, rec.Code, rec.Body.String())
		}
		if err := srv.drainAndPush(context.Background()); err != nil {
			t.Fatalf("push progress %d: %v", progress, err)
		}
	}

	first := start()
	uid := cardUID(t, first, "alice", "one")
	mutateAndPush(first, uid, 51)
	credential.set(tokenB, "bob")
	mutateAndPush(first, uid, 62)
	if got := recorder.pushedTokens(); len(got) != 2 || got[0] != tokenA || got[1] != tokenA {
		t.Fatalf("push credentials before restart = %q, want [%q %q]", got, tokenA, tokenA)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := start()
	mutateAndPush(second, uid, 73)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if got := recorder.pushedTokens(); len(got) != 3 || got[2] != tokenB {
		t.Fatalf("push credentials after restart = %q, want the third to be %q", got, tokenB)
	}

	check, err := gitstore.Clone(context.Background(), memory.NewStorage(), remote, gitstore.Options{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantActors := map[string]string{
		"51": "alice",
		"62": "bob",
		"73": "bob",
	}
	seen := map[string]bool{}
	err = check.Walk(check.Head(), func(c *object.Commit) (bool, error) {
		tr := gitstore.ParseTrailers(c.Message)
		for _, change := range tr.Changes {
			want, ok := wantActors[change.To]
			if !ok || change.Kind != "progress" {
				continue
			}
			seen[change.To] = true
			if tr.Actor != want || c.Author.Name != want {
				t.Errorf("progress %s actor/author = %q/%q, want %q", change.To, tr.Actor, c.Author.Name, want)
			}
			if c.Committer.Name != committer.Name || c.Committer.Email != committer.Email {
				t.Errorf("progress %s committer = %s <%s>, want configured %s <%s>",
					change.To, c.Committer.Name, c.Committer.Email, committer.Name, committer.Email)
			}
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for progress := range wantActors {
		if !seen[progress] {
			t.Errorf("remote history has no persisted progress %s commit", progress)
		}
	}
}
