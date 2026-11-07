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
		"team: portal\n" +
		"mirrors:\n" +
		"  - {project: strategy, scope: Fundraising}\n" +
		"rank: a0\n" +
		"---\n\nBody.\n"
	f, err := DecodeCard("01ARZ3NDEKTSV4RRFFQ69G5FAV", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if f.Card.Team != "portal" {
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

// A card in a Project-board column — `scope`, or `epic` as the key was
// spelled before schema 2 — is a PROJECT CARD, and project cards were
// removed with the Project board (H15): the codec marks the file for every
// reader to skip. An empty column is no column, and a project NAME alone
// filed a card nowhere: that is an ordinary task, the name read and dropped.
func TestAColumnKeyUnderEitherSpellingMarksAProjectCard(t *testing.T) {
	for _, tc := range []struct {
		name, front string
		project     bool
	}{
		{"a scope", "scope: Cozystack\n", true},
		{"the pre-schema-2 epic", "epic: Cozystack\n", true},
		{"a scope and a project", "project: engineering\nscope: Cozystack\n", true},
		{"an empty scope", "scope: \"\"\n", false},
		{"a project name alone", "project: engineering\n", false},
		{"neither", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := DecodeCard("01ARZ3NDEKTSV4RRFFQ69G5FAV", []byte("---\ntitle: x\n"+tc.front+"rank: a0\n---\n"))
			if err != nil {
				t.Fatal(err)
			}
			if f.ProjectCard != tc.project {
				t.Fatalf("ProjectCard = %v, want %v", f.ProjectCard, tc.project)
			}
			if tc.project {
				return
			}
			out, err := EncodeCard(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"project:", "scope:", "epic:"} {
				if strings.Contains(string(out), key) {
					t.Fatalf("an ordinary task is written without %s:\n%s", key, out)
				}
			}
		})
	}
}

// A task has no week (K7): the key a file written before schema 3 still
// carries is read into the FILE — schema 3 and the importer place the task
// by it — and is never written back.
func TestAWeekIsReadIntoTheFileAndNeverWritten(t *testing.T) {
	f, err := DecodeCard("01ARZ3NDEKTSV4RRFFQ69G5FAV", []byte("---\ntitle: x\nweek: 2026-08-24\nrank: a0\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Week != "2026-08-24" {
		t.Fatalf("the file's week = %q", f.Week)
	}
	for _, x := range f.Extra {
		if x.Key == "week" {
			t.Fatalf("the week is read, not kept as an unknown key: %+v", f.Extra)
		}
	}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "week:") {
		t.Fatalf("a week is never written:\n%s", out)
	}
}
