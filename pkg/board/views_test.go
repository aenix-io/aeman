package board

import "testing"

// The views are a closed set and a named one, because the name is now an
// ADDRESS: it is a segment of the path a caller writes through, so a name the
// board does not have must be answered as such rather than falling through to
// "everything".
func TestTheViewsAreTheBoardsAPersonOpens(t *testing.T) {
	t.Parallel()
	for _, v := range Views() {
		if !KnownView(string(v)) {
			t.Fatalf("Views() lists %q and KnownView refuses it", v)
		}
	}
	// Every board of the SPA is here, under the name the SPA uses.
	for _, name := range []string{"me", "team", "backlog", "strategy", "all"} {
		if !KnownView(name) {
			t.Fatalf("%q is a board a person opens and the domain does not know it", name)
		}
	}
	// The Process board is gone (it never was a view: it read the Project
	// view's cards); so is the personal board; and a typo is not a view.
	// The Triage board is retired (K9): its address is no board, and so is
	// the Project board's, removed with project cards (H15).
	for _, name := range []string{"process", "personal", "triage", "project", "", "Me", "week"} {
		if KnownView(name) {
			t.Fatalf("%q is not a view and was accepted", name)
		}
	}
}

// ViewAll is the only one that is not a board, and a caller has to say it out
// loud: it is the escape hatch for a tool with no board to stand on, never a
// default anywhere.
func TestTheEscapeHatchIsNamed(t *testing.T) {
	t.Parallel()
	if ViewAll != "all" {
		t.Fatalf("ViewAll = %q", ViewAll)
	}
	for _, v := range Views() {
		if v == ViewAll {
			return
		}
	}
	t.Fatal("Views() must list the escape hatch too — it is a value the API takes")
}

// Which boards a listing narrows by TEAM. A gesture is judged against the
// listing its board answered, so a gate that pinned a team where the board
// asked for none would refuse work the board is drawing.
func TestOnlySomeBoardsAreScopedByTeam(t *testing.T) {
	t.Parallel()
	for _, v := range []View{ViewTeam, ViewBacklog} {
		if !ScopedByTeam(v) {
			t.Errorf("%s lists one team's work and says it does not", v)
		}
	}
	for _, v := range []View{ViewMe, ViewStrategy, ViewAll} {
		if ScopedByTeam(v) {
			t.Errorf("%s lists every team and says it is narrowed by one", v)
		}
	}
}

// THE STRATEGY BOARD WAS THE ROADMAP. Its old address is still a door to it
// — scripts and the stats pipeline ask by it — but only an alias: it reads
// as `strategy`, and the boards a client is told about name it once, by its
// name now.
func TestTheRoadmapIsAnAliasOfTheStrategyBoard(t *testing.T) {
	t.Parallel()
	if !KnownView("roadmap") {
		t.Fatal("the old roadmap address is refused")
	}
	if got := CanonicalView("roadmap"); got != string(ViewStrategy) {
		t.Fatalf("roadmap reads as %q, want strategy", got)
	}
	for _, name := range []string{"strategy", "me", "all", "", "Roadmap"} {
		if got := CanonicalView(name); got != name {
			t.Fatalf("CanonicalView(%q) = %q, want it unchanged", name, got)
		}
	}
	for _, v := range Views() {
		if v == "roadmap" {
			t.Fatal("Views() lists roadmap beside strategy")
		}
	}
}
