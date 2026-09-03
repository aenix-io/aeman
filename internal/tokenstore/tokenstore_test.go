package tokenstore

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

// throwaway is a Store on the real OS secret store under a service name
// nobody else uses, so a test never reads, writes or deletes aeman's own
// item. It is opt-in: `go test ./...` on a developer's machine must not
// put anything in their login keychain uninvited, and a run killed by a
// timeout would leave what it wrote behind. A machine whose store is
// missing, locked or unreachable — a CI container, a headless session —
// has nothing to exercise and skips too.
func throwaway(t *testing.T) Store {
	t.Helper()
	if os.Getenv("AEMAN_TEST_KEYCHAIN") != "1" {
		t.Skip("writes to the real OS secret store; set AEMAN_TEST_KEYCHAIN=1 to run it")
	}
	s := open(fmt.Sprintf("aeman-test-%d-%d", os.Getpid(), time.Now().UnixNano()), slog.New(slog.DiscardHandler))
	if err := s.Set("probe.example", "probe"); err != nil {
		t.Skipf("no usable secret store here: %v", err)
	}
	t.Cleanup(func() { _ = s.Delete("probe.example") })
	return s
}

// The item is keyed by aeman's service and the forge's host, so a machine
// holds one token per forge instance: signing in to a self-hosted GitLab
// does not overwrite the github.com token, a host nobody stored anything
// for reads as absent rather than as somebody else's token, and a logout
// takes one host and leaves the other.
func TestStoreKeyIsTheServiceAndTheForgeHost(t *testing.T) {
	s := throwaway(t)
	t.Cleanup(func() {
		_ = s.Delete("github.com")
		_ = s.Delete("gitlab.example.org")
	})

	if err := s.Set("github.com", "ghp_one"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("gitlab.example.org", "glpat_two"); err != nil {
		t.Fatal(err)
	}
	if tok, err := s.Get("github.com"); err != nil || tok != "ghp_one" {
		t.Fatalf("github.com = %q, %v", tok, err)
	}
	if tok, err := s.Get("gitlab.example.org"); err != nil || tok != "glpat_two" {
		t.Fatalf("gitlab.example.org = %q, %v", tok, err)
	}
	if _, err := s.Get("git.example.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a host with no item = %v, want ErrNotFound", err)
	}

	if err := s.Delete("github.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("github.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete github.com = %v, want ErrNotFound", err)
	}
	if tok, err := s.Get("gitlab.example.org"); err != nil || tok != "glpat_two" {
		t.Fatalf("the other host after a delete = %q, %v", tok, err)
	}
}

// A token arrives with whatever the shell left on it — `gh auth token |
// aeman login` ends in a newline — and comes back without it. An item
// holding nothing but whitespace is not a token: reported absent, it falls
// through to the next source, where handing "" to the forge would instead
// come back as a rejected credential.
func TestGetTrimsTheStoredTokenAndRefusesABlankItem(t *testing.T) {
	s := throwaway(t)
	t.Cleanup(func() {
		_ = s.Delete("github.com")
		_ = s.Delete("gitlab.example.org")
	})

	if err := s.Set("github.com", "  ghp_padded\n"); err != nil {
		t.Fatal(err)
	}
	if tok, err := s.Get("github.com"); err != nil || tok != "ghp_padded" {
		t.Fatalf("padded token = %q, %v; want it trimmed", tok, err)
	}
	if err := s.Set("gitlab.example.org", " \n\t "); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("gitlab.example.org"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a blank item = %v, want ErrNotFound", err)
	}
}
