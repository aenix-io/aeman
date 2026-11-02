package board

import "testing"

// A mirror is the same card standing in a second Project-board column: one
// file, one log, one set of dates — shown in both places. The card's own
// (project, epic) pair stays its home and keeps deciding where it lives
// (the domain rule reads the primary, never a mirror); mirrors only add
// columns that SHOW it.

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
	// A card with no id of its OWN must not join the frontier either: as
	// "" it would adopt every root card on the next pass, and the guard
	// that walks followers would then refuse the whole board.
	idless := NewBoard([]Card{
		{ItemID: "p", Title: "parent"},
		{Title: "a child with no id", Parent: "p"},
		{ItemID: "root", Title: "unrelated, follows nobody"},
	})
	if got := Followers(idless, "p"); len(got) != 0 {
		t.Fatalf("a card with no id follows nothing and leads nowhere: %+v", got)
	}
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
