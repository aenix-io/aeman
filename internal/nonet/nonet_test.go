package nonet

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recorder struct{ reached bool }

func (r *recorder) RoundTrip(*http.Request) (*http.Response, error) {
	r.reached = true
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

// The guard fails closed. Only loopback goes through, and every way of
// spelling a host that is not loopback — a decimal integer, an octal
// quad, a credential before an @, a suffix that merely starts with a
// loopback name, a resolver that would answer 127.0.0.1 — is refused
// without being asked, because the alternative is a test reaching a real
// forge with whatever token the machine exports.
func TestOnlyLoopbackGoesThrough(t *testing.T) {
	for _, tc := range []struct {
		url     string
		allowed bool
	}{
		{"http://127.0.0.1:8080/user", true},
		{"http://[::1]:8080/user", true},
		{"http://localhost:8080/user", true},
		{"http://127.1.2.3/user", true},
		{"https://192.0.2.1/user", false},
		{"https://gitlab.com/api/v4/user", false},
		{"http://2130706433/user", false},
		{"http://0177.0.0.1/user", false},
		{"http://user@127.0.0.1@evil.example/user", false},
		{"http://localhost.evil.example/user", false},
		{"http://127.0.0.1.nip.io/user", false},
	} {
		rec := &recorder{}
		req, err := http.NewRequest(http.MethodGet, tc.url, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.url, err)
		}
		got, err := refusing{rec}.RoundTrip(req)
		if got != nil {
			_ = got.Body.Close()
		}
		switch {
		case tc.allowed && (err != nil || !rec.reached):
			t.Errorf("%s: refused, want allowed: %v", tc.url, err)
		case !tc.allowed && rec.reached:
			t.Errorf("%s: reached the network, want refused", tc.url)
		case !tc.allowed && !strings.Contains(err.Error(), "tried to reach"):
			t.Errorf("%s: error = %v; want one saying what was blocked", tc.url, err)
		}
	}
}

// Block covers a client built before it — the ones in this repository
// have a nil Transport, and net/http reads the default per request — and
// restore puts back exactly what was there.
func TestBlockCoversAnAlreadyBuiltClientAndRestores(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := &http.Client{}
	before := http.DefaultTransport

	restore := Block()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("loopback must still work: %v", err)
	}
	_ = resp.Body.Close()
	// A documentation address (RFC 5737): if the guard ever stops
	// refusing, the regression is a timeout rather than a request to
	// somebody's server.
	blocked, err := client.Get("http://192.0.2.1/user")
	if err == nil {
		_ = blocked.Body.Close()
		t.Fatal("a client built before Block reached the network")
	}

	restore()
	if http.DefaultTransport != before {
		t.Fatal("restore left a different transport in place")
	}
}

// Guard is the half Block cannot reach: a client that carries its own
// transport never consults the default one. It refuses the same hosts,
// and it leaves the wrapped transport to answer for loopback so an
// httptest server still works through it.
func TestGuardRefusesTheSameHostsAsBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := srv.Client()
	client.Transport = Guard(client.Transport)

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("loopback must still work through the guard: %v", err)
	}
	_ = resp.Body.Close()

	out, err := client.Get("http://192.0.2.1/user")
	if err == nil {
		_ = out.Body.Close()
		t.Fatal("a guarded client reached the network")
	}
	if !strings.Contains(err.Error(), "tried to reach") {
		t.Fatalf("error = %v; want the guard's refusal", err)
	}

	// A nil transport is the default one, not a hole.
	if Guard(nil) == nil {
		t.Fatal("Guard(nil) must still refuse")
	}
}

// Wrapping twice is wrapping once, and the wrap does not swallow the
// shutdown call httptest.Server.Close looks for.
func TestGuardIsIdempotentAndPassesShutdownOn(t *testing.T) {
	base := http.DefaultTransport
	once := Guard(base)
	if twice := Guard(once); twice != once {
		t.Fatal("Guard nested a second layer around an already guarded transport")
	}
	closer, ok := once.(interface{ CloseIdleConnections() })
	if !ok {
		t.Fatal("the guard hides CloseIdleConnections, which httptest.Server.Close needs")
	}
	closer.CloseIdleConnections()
}
