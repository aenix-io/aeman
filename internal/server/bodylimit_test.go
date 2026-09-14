package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
