package server

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The cache's own shape of a freshly created card must carry the card's
// TYPE, or a goal made on the Strategy board is a task to everyone who reads the
// board before the commit is reloaded: the create's own response, the
// listing a second later, the level rule refusing an epic under "a  " (the
// blank type of a task) — all seen in the schema-2 rehearsal. The store's
// backend echoes the type (gitstore.CreateCard); this copy has to as well.
func TestCardFromInputKeepsTheType(t *testing.T) {
	t.Parallel()
	for _, typ := range []string{board.TypeGoal, board.TypeInitiative, board.TypeEpic, board.TypeTask} {
		c := cardFromInput(board.CreateInput{Title: "t", Type: typ, Parent: "p"}, "id")
		if c.Type != typ {
			t.Errorf("type %q: the cached card reads as %q", typ, c.Type)
		}
		if c.Parent != "p" {
			t.Errorf("type %q: the parent was dropped", typ)
		}
	}
}
