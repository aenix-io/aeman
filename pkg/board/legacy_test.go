package board

import "testing"

// A legacy id is a Projects v2 item id; nothing else — a ULID, a note id, an
// empty string — is looked up through the migration's githubId key.
func TestIsLegacyID(t *testing.T) {
	cases := map[string]bool{
		"PVTI_lADOB1234":             true,
		"PVTI_":                      true,
		"01JB4KA0M2P4R6T8V0X2Z4B6D8": false,
		"pvti_lower":                 false,
		"":                           false,
	}
	for id, want := range cases {
		if got := IsLegacyID(id); got != want {
			t.Errorf("IsLegacyID(%q) = %v, want %v", id, got, want)
		}
	}
}
