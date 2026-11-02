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

// refileGuard refuses a re-file that would leave a reference pointing across
// a domain boundary: the COLUMNS OF THE CARDS THAT FOLLOW IT. A subtask's
// file follows its parent and a review card's follows its original
// (MultiBackend cascades the re-file along both links), while each one's
// own column stays behind, naming a repository that no longer holds it —
// the very state SetEpic refuses when the card itself is moved.
func refileGuard(b board.Board, c board.Card, change func(*board.Card)) error {
	return refileGuardOpts(b, c, change, true)
}

// refileGuardOpts is refileGuard with a say over the card's OWN column.
// The grid's × repairs that column itself — ungrouping strands it, and
// refusing would name a column the person did not touch — so it asks
// about everything ELSE first: what a refusal must precede is a write,
// and the repair is one.
func refileGuardOpts(b board.Board, c board.Card, change func(*board.Card), ownColumn bool) error {
	after := c
	change(&after)
	to := board.HomeDomain(b, after)
	// The card's OWN column first, against where the card ends up. This is
	// the one check that matters even when the domain does not change —
	// re-filing a subtask into another column moves no file, and the
	// column still has to name the repository its parent's file lives in.
	if ownColumn && after.Epic != "" {
		if cd, ok := board.ColumnDomain(b, after.Project, after.Epic); ok && cd != to {
			return fmt.Errorf("%w: the column %q is not in the repository that holds this card",
				ErrCrossDomain, after.Epic)
		}
	}
	if to == board.HomeDomain(b, c) {
		return nil
	}
	// Only a card that MOVES can strand what rides it, so the follower
	// scan — the one linear walk here — waits until the move is certain
	// and until the cheaper refusal above has had its say.
	// SetEpicProject calls this once per card of a column.
	followers := board.Followers(b, c.ItemID)
	for _, f := range followers {
		if f.Epic != "" {
			if cd, ok := board.ColumnDomain(b, f.Project, f.Epic); ok && cd != to {
				return fmt.Errorf("%w: %q follows this card and stands in a column of another repository — take it out of the column first",
					ErrCrossDomain, f.Title)
			}
		}
	}
	return nil
}
