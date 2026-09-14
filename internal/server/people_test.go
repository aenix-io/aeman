package server

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
)

// lookupForge is a forge whose only interesting method is Lookup; the rest are
// never called on the people path.
type lookupForge struct {
	forge.Forge
	calls atomic.Int32
	fn    func(login string) (forge.Person, error)
}

func (f *lookupForge) Lookup(_ context.Context, _ *http.Client, _, login string) (forge.Person, error) {
	f.calls.Add(1)
	return f.fn(login)
}

// A login the forge could not answer for (a rate limit, a timeout) is a board
// committer's to flood: the cards carry the logins, and re-resolving thousands
// of unresolvable ones on every roster fan-out was the DoS. A transient failure
// is remembered briefly so it is not re-asked at once, and re-asked once its
// short TTL passes (security finding: repo-controlled logins stall the board).
func TestATransientLookupFailureIsNotReAskedAtOnce(t *testing.T) {
	f := &lookupForge{fn: func(string) (forge.Person, error) {
		return forge.Person{}, context.DeadlineExceeded // unreachable, not ErrNotFound
	}}
	now := time.Unix(0, 0)
	p := newPeople(f, nil, nil)
	p.now = func() time.Time { return now }

	for range 500 {
		p.person("ghost")
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("a transient miss was asked %d times in a burst, want 1", got)
	}
	// Past the short TTL it is asked again.
	now = now.Add(transientMissTTL + time.Second)
	p.person("ghost")
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("after the transient TTL the login was asked %d times, want 2", got)
	}
}

// An unknown login (the forge says "no such user") keeps the longer miss TTL,
// and a found one the full TTL — the transient path must not shorten those.
func TestFoundAndUnknownKeepTheirOwnTTLs(t *testing.T) {
	f := &lookupForge{fn: func(login string) (forge.Person, error) {
		if login == "real" {
			return forge.Person{Login: "real", Name: "Real"}, nil
		}
		return forge.Person{}, forge.ErrNotFound
	}}
	now := time.Unix(0, 0)
	p := newPeople(f, nil, nil)
	p.now = func() time.Time { return now }

	p.person("real")
	p.person("nobody")
	// Just past the transient TTL: neither is re-asked (they are not transient).
	now = now.Add(transientMissTTL + time.Second)
	p.person("real")
	p.person("nobody")
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("a found and an unknown login were re-asked too soon: %d calls, want 2", got)
	}
}
