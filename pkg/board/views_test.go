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

// The PLANNING boards are the ones a plan is made on — the lead's grid, the
// backlogs, the hierarchy. A person's own seat (the Me board) is not one, and
// neither is "all", which is no board at all: a caller there has not said it
// is planning anything.
func TestThePlanningBoardsAreWherePlansAreMade(t *testing.T) {
	t.Parallel()
	want := map[View]bool{ViewMe: false, ViewTeam: true, ViewBacklog: true, ViewStrategy: true, ViewAll: false}
	for _, v := range Views() {
		if got := PlansWork(v); got != want[v] {
			t.Errorf("PlansWork(%s) = %v, want %v", v, got, want[v])
		}
	}
	if PlansWork("") {
		t.Error("an empty view is no board, and plans nothing")
	}
}

// THE STRATEGY BOARD WAS RENAMED FROM ROADMAP on 2026-11-08, and the old
// name is no board any more: it is not a view, and Views() names the board
// once, by its name now.
func TestTheOldRoadmapNameIsNoView(t *testing.T) {
	t.Parallel()
	if KnownView("roadmap") {
		t.Fatal("the old roadmap name is still a view")
	}
	for _, v := range Views() {
		if v == "roadmap" {
			t.Fatal("Views() lists roadmap")
		}
	}
	if !KnownView("strategy") {
		t.Fatal("strategy is not a view")
	}
}
