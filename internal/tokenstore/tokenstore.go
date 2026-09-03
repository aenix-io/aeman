// Package tokenstore keeps the forge credential in the operating system's
// secret store, keyed by the forge's host, so a local aeman — `aeman mcp`
// under an MCP client, `aeman serve` on a laptop — needs no token in its
// environment or in a client configuration file. `aeman login` writes the
// item; every command reads it as one source among several.
package tokenstore

import (
	"log/slog"
	"strings"

	"github.com/lexfrei/keychain"
)

// Service is the secret store's service name for every item aeman writes.
// The account is the forge host, so one machine holds one token per forge
// instance.
const Service = "aeman"

// Store is the secret store as aeman uses it: one token per forge host.
// Get reports a host with no token as ErrNotFound; Delete is idempotent.
type Store interface {
	Get(host string) (string, error)
	Set(host, token string) error
	Delete(host string) error
}

// ErrNotFound is a host with no token stored, re-exported so a caller
// tells that apart from every other failure without importing the library
// behind this package.
//
// It is the only distinction the store can actually make. On macOS every
// operation goes through /usr/bin/security (see open), whose only
// classified outcome is exit 44 for a missing item; everything else — a
// locked keychain over SSH, a denied partition — arrives as the tool's own
// error text. So a caller has two cases, "nothing stored here" and
// "something went wrong, and here is what the tool said".
var ErrNotFound = keychain.ErrNotFound

// Open returns the operating system's secret store.
func Open(log *slog.Logger) Store { return open(Service, log) }

// open is Open with the service name free, so a test exercises the real
// store without touching the item aeman itself uses.
//
// macOS goes through /usr/bin/security: an item written through the native
// API is bound to the creating binary's code identity, and aeman's own
// builds are unsigned, so a rebuilt binary could no longer read what it
// stored. That route has two prices, both spelled out under "aeman login"
// in docs/api.md: the item lives in the stable "apple-tool" partition and
// is readable without a prompt by any process of the same user, and the
// secret is passed to the tool as a command-line argument, so an
// endpoint-security agent recording argv records it. WithLabel and
// WithAccessMode do nothing in this mode and are not passed.
func open(service string, log *slog.Logger) Store {
	kc := keychain.New(keychain.WithSecurityCLI(), keychain.WithLogger(log))
	return &osStore{service: service, kc: kc, read: kc.Get}
}

type osStore struct {
	service string
	kc      *keychain.Keychain

	// read is the raw lookup, so the rules Get puts on top of it — the
	// trim, and a blank item counting as absent — can be exercised without
	// a secret store to write to. Those rules decide whether the chain
	// falls through to the next source, and the test that runs them
	// against the real store is opt-in, so CI would otherwise see neither.
	read func(service, account string) ([]byte, error)
}

// Get returns the stored token without the whitespace a pipe or an editor
// left on it. An item holding nothing else is not a token: it reads as
// absent so the caller falls through, rather than offering the forge an
// empty credential it would reject.
func (s *osStore) Get(host string) (string, error) {
	secret, err := s.read(s.service, host)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(secret))
	if tok == "" {
		return "", ErrNotFound
	}
	return tok, nil
}

func (s *osStore) Set(host, token string) error { return s.kc.Set(s.service, host, []byte(token)) }

func (s *osStore) Delete(host string) error { return s.kc.Delete(s.service, host) }
