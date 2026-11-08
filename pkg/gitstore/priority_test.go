package gitstore

import (
	"strings"
	"testing"
)

// Priorities were removed (2026-11-08, docs/design/backlog.md): the backlog's
// order is the place in the Strategy board tree, and a priority beside it was a
// second answer to the same question. A file that still says `priority:` —
// written while they existed, or by hand — is read with the key as an
// UNKNOWN key: kept, written back byte for byte, and meaning nothing.
func TestAPriorityKeyIsKeptAsAnUnknownKeyAndIgnored(t *testing.T) {
	data := []byte("---\ntitle: matters\ntype: epic\npriority: P1\nrank: a\n---\n")
	f, err := DecodeCard("01JB4PRIO", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Extra) != 1 || f.Extra[0].Key != "priority" || f.Extra[0].Value.Value != "P1" {
		t.Fatalf("the priority is an unknown key: extra %+v", f.Extra)
	}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	again, err := DecodeCard("01JB4PRIO", out)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Extra) != 1 || again.Extra[0].Key != "priority" || again.Extra[0].Value.Value != "P1" {
		t.Fatalf("the unknown key is written back as it was:\n%s", out)
	}
	if again.Card.Title != "matters" || again.Card.Rank != "a" {
		t.Fatalf("the card reads as before: %+v", again.Card)
	}
	// An already canonical file re-encodes to the same bytes.
	if out2, _ := EncodeCard(again); string(out2) != string(out) {
		t.Fatalf("re-encoding is not stable:\n%s\n---\n%s", out, out2)
	}
}

// `todo` was a flag of an unreleased build. A file that carries one is read
// with the key as an UNKNOWN key — kept, written back, and meaning nothing.
func TestAStrayTodoKeyIsKeptAndIgnored(t *testing.T) {
	data := []byte("---\ntitle: old\nparked: true\ntodo: true\nrank: a\n---\n")
	f, err := DecodeCard("01JB4TODO", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Extra) != 1 || f.Extra[0].Key != "todo" {
		t.Fatalf("the stray key is an unknown key: extra %+v", f.Extra)
	}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "todo: true") {
		t.Fatalf("the unknown key is written back:\n%s", out)
	}
}
