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

// NewBoard does not own the cards it is handed: the assembly filter must
// build a fresh slice, not compact into the caller's backing array.
func TestTheAssemblyFilterLeavesTheCallersSliceAlone(t *testing.T) {
	mirrors := []Placement{
		{Project: "strategy", Epic: "Fundraising"}, // dropped: another repository
		{Project: "freedom", Epic: "Launch"},       // kept
	}
	NewBoard([]Card{
		{ItemID: "pr-e", Title: ProjectStateTitle, Project: "engineering"},
		{ItemID: "pr-f", Title: ProjectStateTitle, Project: "freedom"},
		{ItemID: "pr-s", Title: ProjectStateTitle, Project: "strategy", Domain: "founders"},
		{ItemID: "ep-launch", Title: EpicStateTitle, Epic: "Launch", Project: "freedom"},
		{ItemID: "ep-fund", Title: EpicStateTitle, Epic: "Fundraising", Project: "strategy", Domain: "founders"},
		{ItemID: "c1", Title: "hand-edited", Project: "engineering", Epic: "Cozystack",
			Mirrors: mirrors},
	})
	if mirrors[0] != (Placement{Project: "strategy", Epic: "Fundraising"}) {
		t.Fatalf("the caller's slice must survive assembly untouched: %+v", mirrors)
	}
}

// The service admits a mirror and the assembly keeps it, or the card file
// carries a placement that vanishes on the next full load — the invariant
// G15 states in reverse. Both ask the same question, so both must read the
// COLUMN's repository: the no-project bucket has one like any other, and
// the project-based answer said "no such repository" for every card in it.
func TestTheAssemblyKeepsEveryMirrorTheServiceWouldAdmit(t *testing.T) {
	cards := []Card{
		{ItemID: "pr-e", Title: ProjectStateTitle, Project: "engineering"},
		{ItemID: "ep-cozy", Title: EpicStateTitle, Epic: "Cozystack", Project: "engineering"},
		{ItemID: "ep-loose", Title: EpicStateTitle, Epic: "Loose"},
		{ItemID: "into", Title: "home in a project, mirrored into the bucket",
			Project: "engineering", Epic: "Cozystack",
			Mirrors: []Placement{{Epic: "Loose"}}},
		{ItemID: "outof", Title: "home in the bucket, mirrored into a project",
			Epic: "Loose", Mirrors: []Placement{{Project: "engineering", Epic: "Cozystack"}}},
	}
	b := NewBoard(cards)
	for _, id := range []string{"into", "outof"} {
		var got Card
		for _, c := range b.Cards {
			if c.ItemID == id {
				got = c
			}
		}
		if len(got.Mirrors) != 1 {
			t.Fatalf("%s: the assembly dropped a lawful mirror: %+v", id, got.Mirrors)
		}
	}
}

// ColumnDomain answers for the column itself, and says so plainly when the
// roster does not declare one at all.
func TestColumnDomainReadsTheColumnsOwnStub(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "pr-e", Title: ProjectStateTitle, Project: "engineering"},
		{ItemID: "ep-cozy", Title: EpicStateTitle, Epic: "Cozystack", Project: "engineering"},
		{ItemID: "ep-far", Title: EpicStateTitle, Epic: "Far", Project: "engineering", Domain: "founders"},
		{ItemID: "ep-loose", Title: EpicStateTitle, Epic: "Loose"},
	})
	if d, ok := ColumnDomain(b, "engineering", "Cozystack"); !ok || d != "" {
		t.Fatalf("a primary column names no repository: %q %v", d, ok)
	}
	// The same PROJECT name, a column declared elsewhere: the column wins.
	if d, ok := ColumnDomain(b, "engineering", "Far"); !ok || d != "founders" {
		t.Fatalf("the column carries its own repository: %q %v", d, ok)
	}
	if d, ok := ColumnDomain(b, "", "Loose"); !ok || d != "" {
		t.Fatalf("the no-project bucket is a column and has one: %q %v", d, ok)
	}
	if _, ok := ColumnDomain(b, "engineering", "Ghost"); ok {
		t.Fatal("a column nobody declared answers nothing")
	}
}

// Followers is what the guard walks, and the store's cascade recurses — so
// it must too, and survive a hand-written cycle rather than spin on it.
func TestFollowersWalkTheWholeTreeAndSurviveACycle(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "p", Title: "parent"},
		{ItemID: "kid", Title: "child", Parent: "p"},
		{ItemID: "rev", Title: "review of the child", ReviewOf: "kid"},
		{ItemID: "far", Title: "unrelated"},
	})
	var ids []string
	for _, c := range Followers(b, "p") {
		ids = append(ids, c.ItemID)
	}
	if len(ids) != 2 {
		t.Fatalf("the walk reaches the review card two hops down: %v", ids)
	}
	// A hand-written cycle (a card whose parent is its own descendant) must
	// end the walk, not spin it.
	cyc := NewBoard([]Card{
		{ItemID: "a", Title: "a", Parent: "b"},
		{ItemID: "b", Title: "b", Parent: "a"},
	})
	if got := len(Followers(cyc, "a")); got != 1 {
		t.Fatalf("a cycle is walked once: %d", got)
	}
}
