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
	for _, name := range []string{"me", "team", "triage", "backlog", "project", "personal", "all"} {
		if !KnownView(name) {
			t.Fatalf("%q is a board a person opens and the domain does not know it", name)
		}
	}
	// The Process board draws no cards of its own — it reads the Project
	// view's — so it is not one of these, and neither is a typo.
	for _, name := range []string{"process", "", "Me", "week"} {
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
	for _, v := range []View{ViewTeam, ViewTriage, ViewBacklog} {
		if !ScopedByTeam(v) {
			t.Errorf("%s lists one team's work and says it does not", v)
		}
	}
	for _, v := range []View{ViewMe, ViewPersonal, ViewProject, ViewAll} {
		if ScopedByTeam(v) {
			t.Errorf("%s lists every team and says it is narrowed by one", v)
		}
	}
}
