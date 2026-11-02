package gitstore

import (
	"strings"
	"testing"
)

// The keys of a retired feature are not the codec's any more, and that is
// deliberate: a file written before the schema-2 migration — a record of
// that day, served as it stood — still has to decode, and a reader that
// knew the key would have to keep a code path for a feature that no longer
// exists. So `mirrors` rides through as an unknown key: kept verbatim,
// written back untouched, and stripped once by the migration rather than
// forgotten by every reader.
func TestARetiredMirrorsKeyRidesThroughAsExtra(t *testing.T) {
	in := "---\n" +
		"title: shared work\n" +
		"project: engineering\n" +
		"scope: Cozystack\n" +
		"mirrors:\n" +
		"  - {project: strategy, scope: Fundraising}\n" +
		"rank: a0\n" +
		"---\n\nBody.\n"
	f, err := DecodeCard("01ARZ3NDEKTSV4RRFFQ69G5FAV", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if f.Card.Scope != "Cozystack" || f.Card.Project != "engineering" {
		t.Fatalf("the known keys still read: %+v", f.Card)
	}
	var kept bool
	for _, x := range f.Extra {
		if x.Key == "mirrors" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("the retired key must ride through as Extra, got %+v", f.Extra)
	}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "mirrors:") || !strings.Contains(string(out), "Fundraising") {
		t.Fatalf("the retired key must be written back untouched:\n%s", out)
	}
}

// Before schema 2 the Project-board column was called an epic, and the card
// key was `epic`. The key is read as an alias of `scope` — so a file written
// before the migration, or a pre-migration day served as it stood, still
// files the card under its column — and is written back as `scope`, so one
// edit is enough to bring a card to the current spelling.
func TestAnOldEpicKeyReadsAsScope(t *testing.T) {
	in := "---\ntitle: planned\nproject: engineering\nepic: Cozystack\nrank: a0\n---\n"
	f, err := DecodeCard("01ARZ3NDEKTSV4RRFFQ69G5FAV", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if f.Card.Scope != "Cozystack" {
		t.Fatalf("epic must read as scope, got %q (extra %+v)", f.Card.Scope, f.Extra)
	}
	for _, x := range f.Extra {
		if x.Key == "epic" {
			t.Fatalf("the alias is read, not kept as an unknown key: %+v", f.Extra)
		}
	}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "scope: Cozystack") || strings.Contains(string(out), "epic:") {
		t.Fatalf("written back under the current key:\n%s", out)
	}
}
