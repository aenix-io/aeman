package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/pkg/apiserver"
)

// people is the board's directory of names and avatars: what the forge
// knows about a login, remembered. GitHub builds an avatar from the login
// with no call, so the directory costs nothing there; GitLab has to be
// asked — once per login, the answer kept for hours, a miss for minutes —
// and the member lists the access layer reads on the way seed it, so the
// boards rarely wait on a lookup at all. Nothing here is the identity: the
// login is; this is what the person looks like.
type people struct {
	forge  forge.Forge
	client *http.Client
	// token is the credential the directory is read with: the server's own
	// in OAuth mode, the forge CLI's on a single-user server. "" asks as
	// nobody, which gitlab.com allows for public profiles.
	token func(ctx context.Context) string
	now   func() time.Time

	mu    sync.Mutex
	known map[string]knownPerson
}

type knownPerson struct {
	person forge.Person
	found  bool
	at     time.Time
}

const (
	// peopleTTL is how long a person's name and avatar are trusted.
	peopleTTL = 12 * time.Hour
	// peopleMissTTL is how long a login the forge does not know stays
	// unknown before it is asked about again.
	peopleMissTTL = 10 * time.Minute
	// peopleLookupTimeout bounds a lookup made on a request's path: a slow
	// forge costs a missing avatar, never a hanging board.
	peopleLookupTimeout = 3 * time.Second
)

func newPeople(f forge.Forge, client *http.Client, token func(ctx context.Context) string) *people {
	if token == nil {
		token = func(context.Context) string { return "" }
	}
	return &people{forge: f, client: client, token: token, now: time.Now, known: map[string]knownPerson{}}
}

// learn remembers what the forge said about these people in passing (a
// member list): fresher than any lookup, and free.
func (p *people) learn(persons map[string]forge.Person) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for login, person := range persons {
		if login == "" {
			continue
		}
		person.Login = login
		p.known[login] = knownPerson{person: person, found: true, at: p.now()}
	}
}

// learnUser remembers the person who just signed in — the forge described
// them in the same breath as the token.
func (p *people) learnUser(u forge.User) {
	if u.Login == "" {
		return
	}
	p.learn(map[string]forge.Person{u.Login: {Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL}})
}

// person is what the forge knows about a login: from memory when fresh,
// else asked (bounded), a miss remembered too. An unknown login is a person
// with no name and no avatar — the boards show initials.
func (p *people) person(login string) forge.Person {
	if login == "" {
		return forge.Person{}
	}
	p.mu.Lock()
	k, ok := p.known[login]
	p.mu.Unlock()
	if ok {
		ttl := peopleTTL
		if !k.found {
			ttl = peopleMissTTL
		}
		if p.now().Sub(k.at) < ttl {
			return k.person
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), peopleLookupTimeout)
	defer cancel()
	found, err := p.forge.Lookup(ctx, p.client, p.token(ctx), login)
	switch {
	case err == nil:
		found.Login = login
		p.mu.Lock()
		p.known[login] = knownPerson{person: found, found: true, at: p.now()}
		p.mu.Unlock()
		return found
	case errors.Is(err, forge.ErrNotFound):
		p.mu.Lock()
		p.known[login] = knownPerson{person: forge.Person{Login: login}, found: false, at: p.now()}
		p.mu.Unlock()
		return forge.Person{Login: login}
	default:
		// The forge is unreachable: the last answer stands, else nothing —
		// and nothing is not remembered, so the next request asks again.
		if ok {
			return k.person
		}
		return forge.Person{Login: login}
	}
}

// member is the person as the board resource carries them.
func (p *people) member(login string) apiserver.Member {
	person := p.person(login)
	return apiserver.Member{Login: login, Name: person.Name, AvatarURL: person.AvatarURL}
}
