package server

import (
	"context"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// visibleBackend wraps every write door of the board backend with the domain
// rights check, by hand, method by method — and SetBacklog (the park flag) and
// SetDoneAt were left uncovered, served straight from the embedded backend and
// committed with the server's own credential. A read-only visitor could park a
// card in a domain they cannot write, hiding a whole domain's work from the day
// boards. This pattern already failed once for SetSize, so both are checked and
// the embedded backend must not be reached when the check refuses (security
// finding: SetBacklog skips the write check).
func TestVisibleBackendChecksParkAndDoneAt(t *testing.T) {
	fake := boardservicetest.New(nil, nil)
	v := &visibleBackend{Backend: fake, primary: "shared", domains: []string{"shared", "closed"}}
	// Reads the closed domain, cannot write it (dave's shape).
	ctx := withRights(context.Background(), rightsOn([]string{"shared", "closed"}, []string{"shared"}))
	closed := board.Card{ItemID: "x", Domain: "closed", Title: "in closed"}
	writable := board.Card{ItemID: "y", Domain: "shared", Title: "in shared"}
	bd := board.Board{}

	for _, tc := range []struct {
		name string
		call func(board.Card) error
	}{
		{"SetBacklog", func(c board.Card) error { return v.SetBacklog(ctx, bd, c, true) }},
		{"SetDoneAt", func(c board.Card) error { return v.SetDoneAt(ctx, bd, c, "2026-01-01") }},
	} {
		if err := tc.call(closed); !errors.Is(err, boardservice.ErrForbidden) {
			t.Fatalf("%s in an unwritable domain: err = %v, want ErrForbidden", tc.name, err)
		}
		// The writable domain still goes through to the backend.
		if err := tc.call(writable); err != nil {
			t.Fatalf("%s in a writable domain: %v", tc.name, err)
		}
	}
	// The refused calls never reached the backend; the allowed ones did.
	if fake.Saw("SetBacklog x true") || fake.Saw("SetDoneAt x 2026-01-01") {
		t.Fatal("a refused write reached the backend")
	}
	if !fake.Saw("SetBacklog y true") || !fake.Saw("SetDoneAt y 2026-01-01") {
		t.Fatal("an allowed write did not reach the backend")
	}
}
