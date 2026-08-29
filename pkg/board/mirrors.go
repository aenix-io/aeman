package board

// A mirror is the same card standing in a second Project-board column: one
// file, one log, one set of dates — shown in both places. The card's own
// (project, epic) pair stays its home and keeps deciding where it lives
// (DomainOf reads the primary, never a mirror); mirrors only add columns
// that show it. They exist so that work shared by two projects is one card
// on one person, not a duplicate per project drifting apart.

// Placement names a Project-board column: the (project, epic) pair. A
// column is the pair — epic names repeat across projects.
type Placement struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
}

// Mirrored reports whether the pair is one of the card's MIRROR placements
// — not its home. InEpic answers "does the card stand in this column";
// promotion, unmirroring and duplicate checks need the narrower question.
func Mirrored(c Card, project, epic string) bool {
	for _, m := range c.Mirrors {
		if m.Project == project && m.Epic == epic {
			return true
		}
	}
	return false
}

// MirrorAllowed reports whether a card whose home is in `from` may stand in
// a column of `to`: both projects must be declared, and declared in the
// same repository. A card is one file in one repository, and a column
// elsewhere cannot show a file its readers may not have (G15).
func MirrorAllowed(b Board, from, to string) bool {
	if _, ok := b.ProjectStates[from]; !ok {
		return false
	}
	if _, ok := b.ProjectStates[to]; !ok {
		return false
	}
	return ProjectDomain(b, from) == ProjectDomain(b, to)
}
