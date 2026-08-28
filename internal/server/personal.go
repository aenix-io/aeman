package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// A personal board is a person's own repository attached to the board as a
// domain — ~<login> — for them alone. The link is a file in the primary
// (users/<login>.yaml); the clone, its commits and its pushes use the owner's
// own credential, since the server's token has no business in someone's
// private repository. Credentials are not kept across restarts, so the domain
// is attached the first time its owner shows up after a start, and detached
// when they unlink it — the repository itself is never touched by that.

// personalLink is the repository the primary records for a login, if any.
func (b *storeBackend) personalLink(login string) (string, bool) {
	if b.git == nil || login == "" {
		return "", false
	}
	data, err := b.git.primary().ReadFile(gitstore.UserPath(login))
	if err != nil {
		return "", false
	}
	f, err := gitstore.DecodeUser(data)
	if err != nil || f.Personal == "" {
		return "", false
	}
	return f.Personal, true
}

// hasPersonal reports whether the login's personal domain is attached.
func (b *storeBackend) hasPersonal(login string) bool {
	return b.git != nil && b.git.domain(board.PersonalDomain(login)) != nil
}

// attachPersonal brings the login's personal domain in line with the primary's
// link: attached when linked and not yet attached — cloned with the visitor's
// credential, an unborn repository first given a board — re-credentialed when
// the token changed, detached when the link is gone. Safe to call on every
// request; it does nothing when nothing changed.
func (b *storeBackend) attachPersonal(ctx context.Context, login, token string) error {
	if b.git == nil || login == "" {
		return nil
	}
	g := b.git
	g.pmu.Lock()
	defer g.pmu.Unlock()
	name := board.PersonalDomain(login)
	url, linked := b.personalLink(login)
	attached := g.domain(name) != nil
	switch {
	case !linked && attached:
		return b.detachPersonal(ctx, name)
	case !linked:
		return nil
	case attached:
		g.setAuth(name, token)
		return nil
	}
	remote := gitstore.Remote{URL: url}
	if token != "" {
		remote.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: token}
	}
	dir := filepath.Join(g.dataDir, "repos", "personal", login)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	repo, err := cloneOrOpen(dir, remote, g.repoOpts, url)
	if errors.Is(err, errUnbornRemote) {
		// A freshly created, empty repository: give it a board first.
		if err = gitstore.InitBoard(ctx, memory.NewStorage(), remote, g.repoOpts, login+"'s board"); err != nil {
			return fmt.Errorf("initialise %s: %w", url, err)
		}
		repo, err = cloneOrOpen(dir, remote, g.repoOpts, url)
	}
	if err != nil {
		return err
	}
	if err := g.mb.AddDomain(gitstore.Domain{Name: name, Repo: repo}); err != nil {
		return err
	}
	g.applyMu.Lock()
	g.domains = append(g.domains, gitDomain{Domain: gitstore.Domain{Name: name, Repo: repo}, remote: remote})
	g.applyMu.Unlock()
	b.reloadBoard(ctx)
	g.log.Info("personal board attached", "login", login, "repo", url)
	return nil
}

// detachPersonal takes a personal domain off the board; its clone stays on
// disk (reattaching reopens it) and its repository is untouched. Callers
// hold pmu.
func (b *storeBackend) detachPersonal(ctx context.Context, name string) error {
	g := b.git
	g.applyMu.Lock()
	for i, d := range g.domains {
		if d.Name == name {
			g.domains = append(g.domains[:i:i], g.domains[i+1:]...)
			break
		}
	}
	g.applyMu.Unlock()
	if err := g.mb.RemoveDomain(name); err != nil {
		return err
	}
	b.reloadBoard(ctx)
	g.log.Info("personal board detached", "domain", name)
	return nil
}

// linkPersonal records the login's personal repository in the primary — one
// commit by the person, pushed like any other — and unlinkPersonal removes it.
func (b *storeBackend) linkPersonal(ctx context.Context, login, url string) error {
	now := time.Now().UTC()
	data, err := gitstore.EncodeUser(gitstore.UserFile{Personal: url, Created: now.Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return b.writeUserFile(ctx, login, data, "link "+login+"'s personal board")
}

func (b *storeBackend) unlinkPersonal(ctx context.Context, login string) error {
	return b.writeUserFile(ctx, login, nil, "unlink "+login+"'s personal board")
}

func (b *storeBackend) writeUserFile(ctx context.Context, login string, data []byte, summary string) error {
	g := b.git
	if g == nil {
		return errors.New("no git store")
	}
	g.applyMu.Lock()
	_, err := g.primary().Commit(gitstore.Action{Name: "personal", Actor: login, At: time.Now().UTC(), Summary: summary},
		[]gitstore.FileWrite{{Path: gitstore.UserPath(login), Data: data}})
	g.applyMu.Unlock()
	if err != nil {
		return err
	}
	b.committed(b.store.entry(storeKey(g.domains[0].Name)))
	_ = ctx
	return nil
}

// reloadBoard makes the next read re-read every domain: the set of domains
// just changed, and the cache was built over the old one.
func (b *storeBackend) reloadBoard(ctx context.Context) {
	key := storeKey(b.git.domains[0].Name)
	e := b.store.entry(key)
	e.mu.Lock()
	e.loaded = false
	e.mu.Unlock()
	if _, err := b.LoadBoard(ctx, key); err != nil {
		b.git.log.Warn("reload after a domain change failed", "err", err)
	}
}

// domain finds an attached domain by name; nil when it is not attached.
func (g *gitSync) domain(name string) *gitDomain {
	g.applyMu.Lock()
	defer g.applyMu.Unlock()
	for i := range g.domains {
		if g.domains[i].Name == name {
			return &g.domains[i]
		}
	}
	return nil
}

// setAuth gives a domain's remote the credential its owner showed most
// recently — a session token is renewed now and then, and the push must use
// the live one.
func (g *gitSync) setAuth(name, token string) {
	if token == "" {
		return
	}
	g.applyMu.Lock()
	defer g.applyMu.Unlock()
	for i := range g.domains {
		if g.domains[i].Name == name {
			if cur, ok := g.domains[i].remote.Auth.(*githttp.BasicAuth); ok && cur.Password == token {
				return
			}
			g.domains[i].remote.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: token}
			return
		}
	}
}
