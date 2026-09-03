// Package nonet keeps a test binary off the network.
//
// The forge clients in this repository are built with a nil Transport, so
// they fall back to http.DefaultTransport. A test that hands one a forge
// pointed at the real host therefore reaches it — with whatever token the
// machine running the test exports, and with the answer going into the
// test's output. That has happened here. Refusing the request closes the
// route itself, rather than the constructor that happened to take it.
//
// What it does not cover. A subprocess has its own network: `cliFor`
// appends a source that execs the developer's real `gh` or `glab`, and a
// test that reaches it reads the credential of whoever is running the
// tests. Nothing here can see that, so the defence is a stand-in ahead
// of the tool source. And a client carrying its own Transport never
// consults the default one this replaces, so Block does not reach it
// either: those are wrapped with Guard, in the test helpers that hand a
// client to the code under test. One assembled by hand and not wrapped
// stays outside both.
package nonet

import (
	"fmt"
	"net"
	"net/http"
)

// Block makes the default transport refuse every request but one to
// loopback, and returns the func that puts the old one back. Callers are
// TestMains, so the swap is process-wide and lasts the run.
//
// The check is on the URL's host and not on the address dialled: with a
// proxy exported — HTTPS_PROXY at 127.0.0.1 is this repository's own
// development setup — every request in the process dials loopback and
// goes out from there, so a dialler-level guard passes exactly the
// traffic it was written to stop.
func Block() func() {
	prev := http.DefaultTransport
	http.DefaultTransport = Guard(prev)
	return func() { http.DefaultTransport = prev }
}

// Guard wraps one transport so it refuses anything but loopback, for the
// clients that never consult the default: httptest.Server.Client() and
// anything built on it carry their own, which Block does not reach. A helper that hands a fake forge's client to the code
// under test should wrap it, or a case that points that code at a real
// host reaches it through the client the helper supplied.
func Guard(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	if _, done := next.(refusing); done {
		// Idempotent: httptest.Server.Client() hands back the same client
		// every call, so a helper that wraps it would otherwise nest a
		// layer per call against the same server.
		return next
	}
	return refusing{next}
}

type refusing struct{ next http.RoundTripper }

// CloseIdleConnections passes the call on. httptest.Server.Close type
// asserts for this on the client's transport, so swallowing it would
// quietly disable that half of the server's shutdown once Guard wraps
// what Client() returns.
func (r refusing) CloseIdleConnections() {
	if c, ok := r.next.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

func (r refusing) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("this test tried to reach %s; tests talk to httptest.NewServer, never a real forge", host)
	}
	return r.next.RoundTrip(req)
}
