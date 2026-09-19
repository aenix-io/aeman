package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// A request body is capped before any handler reads it: a handler decoding
// straight into memory would otherwise let an authenticated caller stream
// gigabytes in before validation runs (security finding: no request-body size
// limit). Over the cap the read fails; a JSON handler turns that into 413.
func TestLimitBodyCapsTheRequest(t *testing.T) {
	var read int64
	h := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		read = n
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	under := httptest.NewRecorder()
	h.ServeHTTP(under, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("hello")))
	if under.Code != http.StatusOK || read != 5 {
		t.Fatalf("a small body: code=%d read=%d", under.Code, read)
	}

	over := httptest.NewRecorder()
	big := strings.NewReader(strings.Repeat("x", maxBodyBytes+1))
	h.ServeHTTP(over, httptest.NewRequest(http.MethodPost, "/x", big))
	if over.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an over-cap body: code=%d, want 413", over.Code)
	}
	if read > maxBodyBytes {
		t.Fatalf("the handler read %d bytes past the %d cap", read, maxBodyBytes)
	}
}

// Over the cap, every route answers alike. PATCH /people/{login} did not: it
// decoded straight off the body instead of through the shared helper, so a
// caller who sent too much was told the JSON was invalid (400) where every
// neighbour answered 413. There is one decode for the whole surface now, and
// one answer with it.
func TestAnOversizedBodyIsTooLargeOnEveryRoute(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Team: "t"}}, nil)
	srv := apiServer(t, Options{}, fake)
	pad := `,"pad":"` + strings.Repeat("x", maxBodyBytes) + `"}`
	for _, c := range []struct{ name, method, target, body string }{
		{"a person's capacity", http.MethodPatch, "/api/v1/people/bob", `{"capacity":1` + pad},
		{"a card action", http.MethodPost, "/api/v1/cards/c1/actions/defer", `{"days":1` + pad},
		{"a card patch", http.MethodPatch, "/api/v1/cards/c1", `{"title":"x"` + pad},
	} {
		rec := do(t, srv, c.method, c.target, c.body)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s: %d, want 413 — %s", c.name, rec.Code, rec.Body.String())
			continue
		}
		if code := decodeProblem(t, rec).Code; code != "bodyTooLarge" {
			t.Errorf("%s: code %q, want bodyTooLarge", c.name, code)
		}
	}
}
