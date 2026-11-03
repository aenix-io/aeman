package boardservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrNobodyToTake is a Take with no actor: the gesture is "I take this", and
// a request that names nobody has nobody to give the card to.
var ErrNobodyToTake = errors.New("take needs a person to take the card: the request names nobody")

// ErrAlreadyInWork is a Take on a task already in work: Take is the backlog's
// gesture, and handing work somebody is doing to somebody else is the owner
// picker's (own with a person).
var ErrAlreadyInWork = errors.New("this task is already in work; hand it over with the owner picker")

// ErrAlreadyFinished is a Take on finished work: there is nothing left to
// take into work.
var ErrAlreadyFinished = errors.New("this card is finished; there is nothing to take into work")

// Take is the backlog's own gesture, "I take this into work" (K11,
// docs/design/backlog.md "Taking"). The ACTOR — whoever makes the request,
// the identity every write records as its author — gets the card on its OWN
// team (a card of the no-team group stays in it): a parked task is taken
// into work the way `own` with a person takes it (today, the unplanned band,
// the team's sprint, out of the backlog), and a review nobody has taken makes
// the actor its reviewer (K10). Every refusal fires before anything is
// written: a planning card, a project card, a subtask and a review somebody
// already has are not the actor's to take; finished work has nothing left to
// take; a task already in work is handed over with the owner picker.
func (s *Service) Take(ctx context.Context, boardID string, itemID string) error {
	actor := board.ActorFrom(ctx)
	if actor == "" {
		return ErrNobodyToTake
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if err := notAPlanningCard(card, "owner"); err != nil {
		return err
	}
	if card.Scope != "" {
		return fmt.Errorf("%w: %q stands in a column and has no owner", ErrProjectCard, card.Title)
	}
	if board.Complete(card.Stage, card.Progress) {
		return fmt.Errorf("%w: %q", ErrAlreadyFinished, card.Title)
	}
	if card.ReviewOf != "" {
		if !board.NeedsReviewer(card) {
			return fmt.Errorf("%w: %q", ErrOwnerFollowsLink, card.Title)
		}
		return s.takeReview(ctx, b, card, actor)
	}
	if board.IsSubtask(b, card) {
		return fmt.Errorf("%w: %q", ErrOwnerFollowsLink, card.Title)
	}
	if board.InWork(card) {
		return fmt.Errorf("%w: %q", ErrAlreadyInWork, card.Title)
	}
	return s.ownByPerson(ctx, boardID, b, card, card.Team, actor)
}
