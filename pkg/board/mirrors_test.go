package board

import "testing"

// A mirror is the same card standing in a second Project-board column: one
// file, one log, one set of dates — shown in both places. The card's own
// (project, epic) pair stays its home and keeps deciding where it lives
// (the domain rule reads the primary, never a mirror); mirrors only add
// columns that SHOW it.

// InEpic answers "does this card stand in this column" — DeleteEpic's
// occupancy check, RenameEpic and SetEpicProject all ask it. A mirrored card stands
// in its own column and in every mirror column alike.
func TestInEpicSeesMirrors(t *testing.T) {
	c := Card{
		Project: "engineering", Epic: "Cozystack",
		Mirrors: []Placement{{Project: "freedom", Epic: "Launch"}},
	}
	if !InEpic(c, "engineering", "Cozystack") {
		t.Fatal("the card stands in its own column")
	}
	if !InEpic(c, "freedom", "Launch") {
		t.Fatal("the card stands in its mirror column too")
	}
	if InEpic(c, "freedom", "Cozystack") || InEpic(c, "engineering", "Launch") {
		t.Fatal("a column is the PAIR: mixing halves of two placements matches nothing")
	}
	if InEpic(Card{Project: "engineering", Epic: "Cozystack"}, "freedom", "Launch") {
		t.Fatal("a card without mirrors stands only in its own column")
	}
}

// Mirrored reports whether a (project, epic) pair is one of the card's
// mirror placements — NOT its home: the callers that promote, unmirror or
// forbid duplicates need to tell the two apart, which InEpic cannot.
func TestMirroredTellsAMirrorFromTheHome(t *testing.T) {
	c := Card{
		Project: "engineering", Epic: "Cozystack",
		Mirrors: []Placement{{Project: "freedom", Epic: "Launch"}},
	}
	if !Mirrored(c, "freedom", "Launch") {
		t.Fatal("the mirror placement is a mirror")
	}
	if Mirrored(c, "engineering", "Cozystack") {
		t.Fatal("the home placement is not a mirror")
	}
}

// A mirror may only stand in a column of the card's own repository: a card
// is one file in one repository, and a column elsewhere cannot show a file
// its readers may not have (G15's ErrCrossDomain, pinned here at last). The
// same-domain check is the roster's: where was the target project declared,
// against where the card's home project was.
func TestAMirrorTargetMustShareTheCardsRepository(t *testing.T) {
	b := Board{
		Projects:      []string{"engineering", "freedom", "strategy"},
		ProjectStates: map[string]string{"engineering": "pr-e", "freedom": "pr-f", "strategy": "pr-s"},
		Domains:       map[string]string{"pr-s": "founders"},
	}
	if !MirrorAllowed(b, "engineering", "freedom") {
		t.Fatal("two projects of the primary repository mirror freely")
	}
	if MirrorAllowed(b, "engineering", "strategy") {
		t.Fatal("a column in another repository cannot show this card")
	}
	if MirrorAllowed(b, "strategy", "engineering") {
		t.Fatal("nor the other way round")
	}
	// A project the roster does not declare is no target at all; the caller
	// refuses it before asking about domains.
	if MirrorAllowed(b, "engineering", "ghost") {
		t.Fatal("an undeclared project is not a lawful target")
	}
}

// The decoder judges what one file can prove; a mirror into another
// repository or onto a column nobody declared needs the roster, so the
// board assembly drops it — a writer producing one is silently corrected,
// and the x's promotion never inherits a home the service would have
// refused to mirror to.
func TestNewBoardDropsMirrorsTheRosterDisowns(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "pr-e", Title: ProjectStateTitle, Project: "engineering"},
		{ItemID: "pr-f", Title: ProjectStateTitle, Project: "freedom"},
		{ItemID: "pr-s", Title: ProjectStateTitle, Project: "strategy", Domain: "founders"},
		{ItemID: "ep-cozy", Title: EpicStateTitle, Epic: "Cozystack", Project: "engineering"},
		{ItemID: "ep-launch", Title: EpicStateTitle, Epic: "Launch", Project: "freedom"},
		{ItemID: "ep-fund", Title: EpicStateTitle, Epic: "Fundraising", Project: "strategy", Domain: "founders"},
		{ItemID: "c1", Title: "hand-edited", Project: "engineering", Epic: "Cozystack",
			Mirrors: []Placement{
				{Project: "freedom", Epic: "Launch"},       // lawful: declared, same repository
				{Project: "strategy", Epic: "Fundraising"}, // another repository
				{Project: "freedom", Epic: "Ghost"},        // a column nobody declared
			}},
		{ItemID: "c2", Title: "no-project home", Epic: "Inbox",
			Mirrors: []Placement{{Project: "freedom", Epic: "Launch"}}},
	})
	var c1, c2 Card
	for _, c := range b.Cards {
		switch c.ItemID {
		case "c1":
			c1 = c
		case "c2":
			c2 = c
		}
	}
	if len(c1.Mirrors) != 1 || c1.Mirrors[0] != (Placement{Project: "freedom", Epic: "Launch"}) {
		t.Fatalf("only the lawful mirror survives assembly: %+v", c1.Mirrors)
	}
	if c2.Mirrors != nil {
		t.Fatalf("a no-project home names no repository — its mirrors are disowned: %+v", c2.Mirrors)
	}
}
