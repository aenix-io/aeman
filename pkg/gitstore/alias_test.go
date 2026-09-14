package gitstore

import (
	"strings"
	"testing"
)

// An unknown front-matter key holding a YAML alias used to arm a landmine: the
// value is marshalled alone on re-encode, so an alias whose anchor sits on
// another key was rewritten dangling, and every later parse of the card failed
// — the card dropped into Broken for good, on every replica, the next time
// anyone edited it. The anchor is resolved and stripped when the key is kept,
// so the card survives a round trip (security finding: YAML alias self-corrupts
// a card).
func TestUnknownAliasKeySurvivesAReEncode(t *testing.T) {
	const id = "01JBOOBYTRAP00000000000AB"
	data := []byte("---\ntitle: &t Hello\nunknown: *t\n---\nbody\n")

	f, err := DecodeCard(id, data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Card.Title != "Hello" {
		t.Fatalf("title = %q", f.Card.Title)
	}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(out), "*t") || strings.Contains(string(out), "&t") {
		t.Fatalf("the re-encoded card still carries an anchor or alias:\n%s", out)
	}
	// The card must parse again — the whole point — and keep the resolved value.
	again, err := DecodeCard(id, out)
	if err != nil {
		t.Fatalf("re-parse of the rewritten card failed (card is broken): %v", err)
	}
	found := false
	for _, x := range again.Extra {
		if x.Key == "unknown" {
			found = true
			if x.Value.Value != "Hello" {
				t.Fatalf("the alias did not resolve to its target: %q", x.Value.Value)
			}
		}
	}
	if !found {
		t.Fatal("the unknown key was dropped rather than kept")
	}
}

// A merge key (<<: *x) is the same class and must not dangle either.
func TestUnknownMergeAliasSurvives(t *testing.T) {
	const id = "01JBOOBYTRAP00000000000CD"
	data := []byte("---\ntitle: T\ndefaults: &d\n  a: 1\nextra:\n  <<: *d\n  b: 2\n---\nbody\n")
	f, err := DecodeCard(id, data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(out), "*d") || strings.Contains(string(out), "&d") {
		t.Fatalf("merge alias/anchor survived:\n%s", out)
	}
	if _, err := DecodeCard(id, out); err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}
}
