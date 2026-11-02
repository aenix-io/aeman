package boardservice

import (
	"errors"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrCrossDomain is a placement that would leave a reference pointing across
// a repository boundary: a card is one file in one repository, and a column
// elsewhere cannot show a file its readers may not have.
var ErrCrossDomain = errors.New("the target project lives in another repository")

// refileGuard refuses a re-file that would leave the card's COLUMN naming a
// repository that does not hold the card. A project card lives where its
// project lives (the domain rule, docs/design/hierarchy.md), so the two agree
// wherever a project owns the column; the NO-PROJECT bucket is the case this
// exists for — a real column with no project to ask, declared in one
// repository, that a card with no project (and so of the primary) cannot
// stand in.
//
// It used to walk the cards that FOLLOW the one being re-filed as well — a
// subtask's file follows its parent, a review card's its original — for the
// columns they carried. A card in a column is nobody's subtask and never a
// review now (H4), so there is nothing behind the card to strand.
func refileGuard(b board.Board, c board.Card, change func(*board.Card)) error {
	after := c
	change(&after)
	if after.Scope == "" {
		return nil
	}
	to := board.HomeDomain(b, after)
	if cd, ok := board.ColumnDomain(b, after.Project, after.Scope); ok && cd != to {
		return fmt.Errorf("%w: the column %q is not in the repository that holds this card",
			ErrCrossDomain, after.Scope)
	}
	return nil
}
