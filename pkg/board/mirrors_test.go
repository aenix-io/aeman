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
		// A no-project home is of the primary repository like any other
		// column of it, so a primary mirror on this card is lawful.
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
	if len(c2.Mirrors) != 1 {
		t.Fatalf("a lawful mirror of the primary repository survives: %+v", c2.Mirrors)
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
	// The EMPTY id follows nothing. Every root card has an empty parent and
	// an empty reviewOf, so the walk adopted the whole board — and this is
	// an exported helper an outside tool can call with an unset field.
	if got := Followers(b, ""); got != nil {
		t.Fatalf("nothing follows nobody: %+v", got)
	}
}

// "Both ask the same question" means the same question: which repository
// holds this CARD. Reading the home COLUMN instead answers differently for
// a card whose column is not in its own repository — a state older writes
// could produce (a no-project card re-teamed across repositories) and an
// external writer still can. There the assembly kept a mirror Mirror
// would refuse, which is G15 inverted just as surely as dropping one.
func TestTheAssemblyAsksWhichRepositoryHoldsTheCard(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "st-f", Title: SprintStateTitle, Team: "founders", Domain: "founders"},
		{ItemID: "pr-e", Title: ProjectStateTitle, Project: "engineering"},
		{ItemID: "ep-cozy", Title: EpicStateTitle, Epic: "Cozystack", Project: "engineering"},
		{ItemID: "ep-loose", Title: EpicStateTitle, Epic: "Loose"},
		// The team holds this card in founders; its column is of the primary.
		{ItemID: "odd", Title: "legacy", Team: "founders", Epic: "Loose",
			Mirrors: []Placement{{Project: "engineering", Epic: "Cozystack"}}},
	})
	for _, c := range b.Cards {
		if c.ItemID == "odd" && len(c.Mirrors) != 0 {
			t.Fatalf("the card's file is in founders; a primary column cannot show it: %+v", c.Mirrors)
		}
	}
}

// A card whose home column the roster does not declare still has lawful
// mirrors — the home check is the one to skip, not every placement.
func TestAnUndeclaredHomeDoesNotDiscardTheOtherPlacements(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "pr-e", Title: ProjectStateTitle, Project: "engineering"},
		{ItemID: "ep-cozy", Title: EpicStateTitle, Epic: "Cozystack", Project: "engineering"},
		{ItemID: "c1", Title: "home in a ghost column", Project: "engineering", Epic: "Ghost",
			Mirrors: []Placement{{Project: "engineering", Epic: "Cozystack"}}},
	})
	for _, c := range b.Cards {
		if c.ItemID == "c1" && len(c.Mirrors) != 1 {
			t.Fatalf("the declared mirror survives an undeclared home: %+v", c.Mirrors)
		}
	}
}

// The assembly reads one namespace. A board from the store names its
// primary and stamps every entry with a domain NAME, so a card that
// nothing places still belongs to that primary — and its mirrors are
// lawful, where comparing a stamped column against an unstamped placement
// dropped them on every load.
func TestTheAssemblyKeepsMirrorsOnAStampedBoard(t *testing.T) {
	b := NewBoardIn("aeman-db", []Card{
		{ItemID: "pr-e", Title: ProjectStateTitle, Project: "engineering", Domain: "aeman-db"},
		{ItemID: "ep-cozy", Title: EpicStateTitle, Epic: "Cozystack", Project: "engineering", Domain: "aeman-db"},
		{ItemID: "ep-inbox", Title: EpicStateTitle, Epic: "Inbox", Domain: "aeman-db"},
		{ItemID: "c1", Title: "in the bucket", Epic: "Inbox",
			Mirrors: []Placement{{Project: "engineering", Epic: "Cozystack"}}},
	})
	for _, c := range b.Cards {
		if c.ItemID == "c1" && len(c.Mirrors) != 1 {
			t.Fatalf("a lawful mirror of the primary must survive: %+v", c.Mirrors)
		}
	}
}

// And ColumnDomain answers in that namespace: an entry the store stamped
// with the primary's name, and one left unstamped by a hand-built board,
// are the same repository.
func TestColumnDomainAnswersInThePrimarysName(t *testing.T) {
	b := NewBoardIn("aeman-db", []Card{
		{ItemID: "ep-inbox", Title: EpicStateTitle, Epic: "Inbox", Domain: "aeman-db"},
		{ItemID: "ep-bare", Title: EpicStateTitle, Epic: "Bare"},
	})
	stamped, _ := ColumnDomain(b, "", "Inbox")
	bare, _ := ColumnDomain(b, "", "Bare")
	if stamped != "aeman-db" || bare != "aeman-db" {
		t.Fatalf("both are of the primary: %q and %q", stamped, bare)
	}
	if got := HomeDomain(b, Card{Epic: "Inbox"}); got != "aeman-db" {
		t.Fatalf("a card nothing places belongs to the primary too: %q", got)
	}
}
