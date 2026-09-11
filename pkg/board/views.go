package board

// A VIEW is a board somebody opens. The domain names them because the name is
// an address: the API scopes a listing, a create and a board gesture by it
// (docs/design/view-scoped-api.md), so "me" and "triage" are values that
// travel, not strings a client makes up.
//
// A view is not a place a card LIVES — the same card is drawn on its owner's
// Me board, on the team's grid and in the Triage week it is scheduled for, all
// at once. It says where the CALLER is standing, which is what makes a gesture
// mean something ("remove it from here") and a create mean something ("a card
// of this board").
type View string

const (
	// ViewMe is the Me day board: one person's own cards on a day.
	ViewMe View = "me"
	// ViewTeam is the lead's day grid: a team's cards on a day, everyone's.
	ViewTeam View = "team"
	// ViewTriage is the weeks grid: the weeks work is scheduled in, plus the
	// strip of cards nobody has scheduled at all.
	ViewTriage View = "triage"
	// ViewBacklog is the Triage board's drawer: a team's parked work, which
	// is a place of its own beside a week and the strip (B11).
	ViewBacklog View = "backlog"
	// ViewProject is the Project board: every card filed under a column.
	ViewProject View = "project"
	// ViewPersonal is the caller's own repository, read as a board.
	ViewPersonal View = "personal"
	// ViewAll is no board at all: everything the caller may read. It is the
	// escape hatch for a tool that has no board to stand on — a migration, a
	// sweep, an embedder — and it is never a default, because a caller acting
	// board-wide should have said so.
	ViewAll View = "all"
)

// Views lists them in the order the SPA's own tabs run, the escape hatch last.
func Views() []View {
	return []View{ViewMe, ViewTeam, ViewTriage, ViewBacklog, ViewProject, ViewPersonal, ViewAll}
}

// KnownView reports whether a name is one of them. The empty string is NOT:
// a door that defaults an unspecified view does so itself, deliberately, and
// a view that arrived empty because something forgot to send it must not
// quietly become "everything".
func KnownView(name string) bool {
	for _, v := range Views() {
		if string(v) == name {
			return true
		}
	}
	return false
}

// Panes are the listings a BOARD is drawn from. Most boards are one listing,
// and two are not: the Triage grid stands beside its DRAWER of parked work,
// and the Me day board beside the PERSONAL column, each fetched separately
// because they are different questions (and, for the personal one, a different
// repository with different rights).
//
// It matters wherever a gesture is judged by the board it was made on: the ×
// in the drawer is the Triage board's × — the reader is looking at one screen
// — and a gate that knew only the grid would refuse it on every parked card.
func Panes(v View) []View {
	switch v {
	case ViewTriage:
		return []View{ViewTriage, ViewBacklog}
	case ViewMe:
		return []View{ViewMe, ViewPersonal}
	default:
		return []View{v}
	}
}

// ScopedByTeam reports the boards whose LISTING is narrowed by a team: the
// lead's grid, the weeks and the drawer all ask for one. The Me board, the
// personal column and the Project grid list every team, and "" is the answer
// they give.
//
// It is a fact about a BOARD, which is why it lives here rather than in either
// door: a gesture is judged against the listing its board answered, and a gate
// that pinned a team the listing never asked for would refuse work the board
// is drawing (a subtask of another team rides into the Me view on its parent).
// The two doors asked it separately once, held together by a comment.
func ScopedByTeam(v View) bool {
	return v == ViewTeam || v == ViewTriage || v == ViewBacklog
}
