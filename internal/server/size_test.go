package server

import (
	"net/http"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// A card's size travels as the letter — `"size":"L"` — on create and on
// patch, in any case a person or a tool types it, and comes back the same
// way. Anything but the four letters is a 400 with a message that names
// them, since a stored size nothing knows would weigh nothing.
func TestAPISizeIsALetterOnTheWayInAndOut(t *testing.T) {
	fake := boardservicetest.New(nil, map[string]board.SprintState{"alpha": {Current: "2026-06-20", ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)

	rec := do(t, srv, http.MethodPost, "/api/v1/views/team/cards",
		`{"title":"Реализация envoy-gateway","team":"alpha","zone":"planned","size":"l"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	c := decodeCard(t, rec)
	if c.Spec.Size != "L" {
		t.Fatalf("size at birth = %q, want L (upper-cased from \"l\")", c.Spec.Size)
	}

	rec = do(t, srv, http.MethodPatch, "/api/v1/cards/"+c.Metadata.UID, `{"size":"XL"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeCard(t, rec).Spec.Size; got != "XL" {
		t.Fatalf("size after patch = %q, want XL", got)
	}

	rec = do(t, srv, http.MethodPatch, "/api/v1/cards/"+c.Metadata.UID, `{"size":"large"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown size answers 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// The empty size takes it back, and the resource then carries no size.
	rec = do(t, srv, http.MethodPatch, "/api/v1/cards/"+c.Metadata.UID, `{"size":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clearing status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeCard(t, rec).Spec.Size; got != "" {
		t.Fatalf("size after clearing = %q, want none", got)
	}
}
