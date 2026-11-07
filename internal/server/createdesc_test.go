package server

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A create's commit names what was made: a card by its title, a roster
// entry by the entry it declares — never by its kind's marker title
// («aeman:sprint-state»).
func TestACreateCommitNamesWhatWasMade(t *testing.T) {
	if got := createDesc(board.Card{Title: board.SprintStateTitle, Team: "portal"}); got != "create team «portal»" {
		t.Fatalf("a team's create = %q", got)
	}
	if got := createDesc(board.Card{Title: "Receipts"}); got != "create «Receipts»" {
		t.Fatalf("a card's create = %q", got)
	}
}
