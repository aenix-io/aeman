// Package nonet keeps a test binary off the network.
//
// The forge clients in this repository are built with a nil Transport, so
// they fall back to http.DefaultTransport. A test that hands one a forge
// pointed at the real host therefore reaches it — with whatever token the
// machine running the test exports, and with the answer going into the
// test's output. That has happened here. Refusing the request closes the
// route itself, rather than the constructor that happened to take it.
//
// Two things it does not cover. A client carrying its own Transport goes
// around it, which today is only httptest's. And a subprocess has its own
// network: `cliFor` appends a source that execs the developer's real `gh`
// or `glab`, and a test that reaches it reads the credential of whoever
// is running the tests. Nothing in this package can see that — the
// defence there is to put a fake ahead of the CLI source, which every
// test that could reach one does.
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
	http.DefaultTransport = refusing{prev}
	return func() { http.DefaultTransport = prev }
}

type refusing struct{ next http.RoundTripper }

func (r refusing) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("this test tried to reach %s; tests talk to httptest.NewServer, never a real forge", host)
	}
	return r.next.RoundTrip(req)
}
