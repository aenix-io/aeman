// Package tokenstoretest provides an in-memory tokenstore.Store for tests
// of the packages that read a credential — the commands and the server —
// so a test never touches the machine's own secret store.
package tokenstoretest

import (
	"strings"
	"sync"

	"github.com/aenix-io/aeman/internal/tokenstore"
)

// Fake is an in-memory tokenstore.Store. Fail makes every later
// operation return an error instead of touching the items, standing in
// for a store that cannot answer. The counters say whether the code under
// test asked at all, which is the point of most of the token-order tests.
type Fake struct {
	// mu guards every field, counters included. The real store is safe to
	// share — the library holds a process-wide lock — and the chain is
	// read from every request, so a double without this cannot stand in
	// for it in any test that runs more than one goroutine. That is also
	// why the counters are read through methods: a bare field is a race
	// the moment a test looks at one while another goroutine is still
	// asking, and it is the concurrent tests that most need the count.
	mu                  sync.Mutex
	items               map[string]string
	err                 error
	gets, sets, deletes int
}

// Gets, Sets and Deletes are how often each operation was called.
func (f *Fake) Gets() int { return f.count(&f.gets) }

// Sets is the number of writes the code under test made.
func (f *Fake) Sets() int { return f.count(&f.sets) }

// Deletes is the number of removals the code under test made.
func (f *Fake) Deletes() int { return f.count(&f.deletes) }

func (f *Fake) count(n *int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *n
}

// Fail makes every later operation return err, safely from another
// goroutine — how a store stops answering under a running reader.
func (f *Fake) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

var _ tokenstore.Store = (*Fake)(nil)

// NewFake returns an empty Fake.
func NewFake() *Fake { return &Fake{items: map[string]string{}} }

// Put seeds a token without counting as a Set, so a test can start from a
// store that already holds one.
func (f *Fake) Put(host, token string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[host] = token
	return f
}

// Get returns the seeded token, or ErrNotFound.
func (f *Fake) Get(host string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.err != nil {
		return "", f.err
	}
	tok, ok := f.items[host]
	if !ok {
		return "", tokenstore.ErrNotFound
	}
	// The same two rules the real store puts on a raw item, so a test
	// cannot pass here against a shape osStore never hands back.
	if tok = strings.TrimSpace(tok); tok == "" {
		return "", tokenstore.ErrNotFound
	}
	return tok, nil
}

// Set stores a token.
func (f *Fake) Set(host, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	if f.err != nil {
		return f.err
	}
	f.items[host] = token
	return nil
}

// Delete removes a token; a missing one is not an error.
func (f *Fake) Delete(host string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	if f.err != nil {
		return f.err
	}
	delete(f.items, host)
	return nil
}
