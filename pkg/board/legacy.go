package board

import "strings"

// legacyIDPrefix is what every GitHub Projects v2 item id started with.
const legacyIDPrefix = "PVTI_"

// IsLegacyID reports whether id is a Projects v2 item id — the form cards had
// before the move to git. A migrated card keeps its old id as GitHubID, and
// the service resolves such an id to the card for one major version.
func IsLegacyID(id string) bool {
	return strings.HasPrefix(id, legacyIDPrefix)
}
