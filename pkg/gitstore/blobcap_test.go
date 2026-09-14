package gitstore

import (
	"errors"
	"strings"
	"testing"
)

// One oversized committed file must not be pulled whole into memory by every
// replica's load. A card file past the cap is refused at read and recorded as
// broken; the rest of the board loads (security finding: no size cap on blob
// reads).
func TestAnOversizedBlobIsBrokenNotLoaded(t *testing.T) {
	huge := "---\ntitle: heavy\n---\n" + strings.Repeat("x", maxBlobBytes)
	r := repoWith(t, map[string]string{
		BoardPath:               "schema: 1\ntitle: b\n",
		TeamPath("_"):           "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		"cards/a/1/01CARDA1.md": "---\ntitle: fine\nteam: _\nrank: b\n---\n",
		"cards/a/2/01CARDA2.md": huge,
	})
	s, err := Load(r)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var brokeHuge bool
	for _, b := range s.Broken {
		if strings.Contains(b.Path, "01CARDA2") {
			brokeHuge = true
			if !errors.Is(b.Err, ErrBlobTooLarge) {
				t.Fatalf("the huge file broke with %v, want ErrBlobTooLarge", b.Err)
			}
		}
	}
	if !brokeHuge {
		t.Fatal("the oversized file was not recorded as broken")
	}
	// The small card still loaded.
	var loadedFine bool
	for _, c := range s.Cards {
		if c.Title == "fine" {
			loadedFine = true
		}
	}
	if !loadedFine {
		t.Fatal("a normal card did not load past the oversized one")
	}
}
