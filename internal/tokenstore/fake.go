package tokenstore

// Fake is an in-memory Store for tests in this package and in the commands
// that read it. Err is returned by every operation instead of touching the
// items — a store that is missing, locked or denying. The counters say
// whether the code under test asked at all, which is the point of most of
// the token-order tests.
type Fake struct {
	items               map[string]string
	Err                 error
	Gets, Sets, Deletes int
}

// NewFake returns an empty Fake.
func NewFake() *Fake { return &Fake{items: map[string]string{}} }

// Put seeds a token without counting as a Set, so a test can start from a
// store that already holds one.
func (f *Fake) Put(host, token string) *Fake {
	f.items[host] = token
	return f
}

// Get returns the seeded token, or ErrNotFound.
func (f *Fake) Get(host string) (string, error) {
	f.Gets++
	if f.Err != nil {
		return "", f.Err
	}
	tok, ok := f.items[host]
	if !ok {
		return "", ErrNotFound
	}
	return tok, nil
}

// Set stores a token.
func (f *Fake) Set(host, token string) error {
	f.Sets++
	if f.Err != nil {
		return f.Err
	}
	f.items[host] = token
	return nil
}

// Delete removes a token; a missing one is not an error.
func (f *Fake) Delete(host string) error {
	f.Deletes++
	if f.Err != nil {
		return f.Err
	}
	delete(f.items, host)
	return nil
}
