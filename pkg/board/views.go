package board

// A VIEW is a board somebody opens. The domain names them because the name is
// an address: the API scopes a listing, a create and a board gesture by it
// (docs/design/view-scoped-api.md), so "me" and "backlog" are values that
// travel, not strings a client makes up.
//
// A view is not a place a card LIVES — the same card is drawn on its owner's
// Me board, on the team's grid and on the Roadmap under its epic, all at
// once. It says where the CALLER is standing, which is what makes a gesture
// mean something ("remove it from here") and a create mean something ("a card
// of this board").
type View string

const (
	// ViewMe is the Me day board: one person's own cards on a day.
	ViewMe View = "me"
	// ViewTeam is the lead's day grid: a team's cards on a day, everyone's.
	ViewTeam View = "team"
	// ViewBacklog is the teams' backlogs: each team's parked tasks, ranked by
	// hand, top is next (docs/design/backlog.md). It replaced the Triage
	// board, whose drawer it used to be (K9).
	ViewBacklog View = "backlog"
	// ViewStrategy is the hierarchy: every goal, initiative and epic, and
	// every card with one of those above it (docs/design/hierarchy.md). A
	// task with no parent is not on it — it is on the day boards. It was
	// called the Roadmap until 2026-11; `roadmap` is still accepted for it
	// (CanonicalView).
	ViewStrategy View = "strategy"
	// ViewRoadmap is ViewStrategy under the name the code grew up with.
	//
	// Deprecated: the board is the Strategy board; use ViewStrategy.
	ViewRoadmap = ViewStrategy
	// ViewAll is no board at all: everything the caller may read. It is the
	// escape hatch for a tool that has no board to stand on — a migration, a
	// sweep, an embedder — and it is never a default, because a caller acting
	// board-wide should have said so.
	ViewAll View = "all"
)

// Views lists them in the order the SPA's own tabs run, the escape hatch last.
func Views() []View {
	return []View{ViewMe, ViewTeam, ViewBacklog, ViewStrategy, ViewAll}
}

// legacyRoadmap is the Strategy board's old address. Scripts and the stats
// pipeline still ask by it, so every door takes it and answers as Strategy.
const legacyRoadmap = "roadmap"

// CanonicalView is a view name as the board names it: the deprecated
// `roadmap` reads as `strategy`; every other name is returned as it came.
// A door canonicalizes before it decides anything, and answers with the
// canonical name.
func CanonicalView(name string) string {
	if name == legacyRoadmap {
		return string(ViewStrategy)
	}
	return name
}

// KnownView reports whether a name is one of them, the deprecated `roadmap`
// included (CanonicalView). The empty string is NOT: a door that defaults an
// unspecified view does so itself, deliberately, and a view that arrived
// empty because something forgot to send it must not quietly become
// "everything".
func KnownView(name string) bool {
	name = CanonicalView(name)
	for _, v := range Views() {
		if string(v) == name {
			return true
		}
	}
	return false
}

// ScopedByTeam reports the boards whose LISTING is narrowed by a team: the
// lead's grid and the backlog both ask for one (a comma-separated set of
// them). The Me board and
// the Strategy board list every team, and "" is the answer they give.
//
// It is a fact about a BOARD, which is why it lives here rather than in either
// door: a gesture is judged against the listing its board answered, and a gate
// that pinned a team the listing never asked for would refuse work the board
// is drawing (a subtask of another team rides into the Me view on its parent).
// The two doors asked it separately once, held together by a comment.
func ScopedByTeam(v View) bool {
	return v == ViewTeam || v == ViewBacklog
}
