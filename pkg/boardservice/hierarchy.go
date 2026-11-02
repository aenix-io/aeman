package boardservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// The hierarchy's admission (docs/design/hierarchy.md): what a planning card
// may be born as, which parent a card may take, and what a planning card
// never carries. The rules themselves are the domain's (board.Level,
// board.IsSubtask); this is where a write is held to them.

// ErrUnknownType is a card type the board has no such thing as: a task
// (""), a goal, an initiative or an epic, and nothing else.
var ErrUnknownType = errors.New("a card is a task, a goal, an initiative or an epic")

// ErrParentLevel is a parent that is not of a HIGHER level than its child
// (H2): an initiative under an epic, an epic under an epic, a goal under
// anything, a planning card under a task. The link points up, which is what
// makes a cycle impossible to write.
var ErrParentLevel = errors.New("a parent is of a higher level than its child")

// ErrPlanningCard is a goal, an initiative or an epic asked to carry what a
// TASK carries: a stage, a progress, a band, a week, a shelf, a sprint, a
// column, a review. A planning card is drawn on the Roadmap and on no day
// board (H3), and its status is derived from the tasks under it (H5) — a
// write of any of those would be a second answer to a question the tree
// already answers.
var ErrPlanningCard = errors.New("a planning card has no stage, progress, band, size, week, shelf, sprint, column or review of its own")

// notAPlanningCard is the guard every task-only write starts with.
func notAPlanningCard(c board.Card, what string) error {
	if !board.IsPlanning(c) {
		return nil
	}
	return fmt.Errorf("%w: %q is %s and takes no %s", ErrPlanningCard, c.Title, articled(c.Type), what)
}

// articled puts the article on a type for a sentence: "an epic", "a goal".
func articled(typ string) string {
	switch typ {
	case board.TypeEpic, board.TypeInitiative:
		return "an " + typ
	}
	return "a " + typ
}

// planningCardCannotBe is the create-side half of ErrPlanningCard: the
// fields a planning card is never born with.
func planningCardCannotBe(args CreateCardArgs) error {
	var what string
	switch {
	case args.Scope != "" || args.Project != "":
		what = "column"
	case args.Zone != board.ZoneNone:
		what = "band"
	case args.Size != "":
		what = "size"
	case args.Week != "":
		what = "week"
	case args.Parked:
		what = "shelf"
	case args.ReviewOf != "":
		what = "review"
	case args.SprintStart != "" || args.StartNewSprint != nil:
		what = "sprint"
	default:
		return nil
	}
	return fmt.Errorf("%w: %q is %s and is born with no %s", ErrPlanningCard, args.Title, articled(args.Type), what)
}

// hierarchyLinkAllowed is the level rule and the repository rule for a link
// in which at least one side is a planning card (H2, H6). The subtask rules
// — two tasks — are setParentOf's own and are not asked here.
func hierarchyLinkAllowed(b board.Board, child, parent board.Card) error {
	if board.Level(parent.Type) <= board.Level(child.Type) {
		return fmt.Errorf("%w: %s %q cannot sit under %s %q", ErrParentLevel,
			articled(child.Type), child.Title, articled(parent.Type), parent.Title)
	}
	// A project card may sit under a planning card (H4 allows exactly
	// that); a task may, a planning card may. A link never crosses a
	// repository: the child's own home — by its project or its team, since a
	// planning parent does not place it — must be the parent's.
	if from, to := board.HomeDomain(b, child), board.HomeDomain(b, parent); from != to {
		return fmt.Errorf("%w: %q lives in another repository than %q", ErrCrossDomain, parent.Title, child.Title)
	}
	return nil
}

// createPlanningCard makes a goal, an initiative or an epic: a card with a
// type, its dates and its owner as asked, and none of a task's day-board
// state — no sprint, no band, no week. It is drawn on the Roadmap (H3), and
// its parent, when it has one, has already passed linksArePossible.
func (s *Service) createPlanningCard(ctx context.Context, b board.Board, args CreateCardArgs, linkDescription string, pendingRef *board.Link) (board.Card, error) {
	if err := planningCardCannotBe(args); err != nil {
		return board.Card{}, err
	}
	if err := s.declareTeam(ctx, b, args.Team); err != nil {
		return board.Card{}, err
	}
	card, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:    args.Title,
		Type:     args.Type,
		Day:      args.Day,
		Start:    args.Start,
		Assignee: args.Assignee,
		Team:     args.Team,
		Parent:   args.Parent,
	})
	card, err = s.withLinkDescription(ctx, b, card, err, linkDescription)
	if err == nil {
		s.resolveTitleAsync(ctx, b, card, pendingRef)
		s.logEvent(ctx, b, card, board.EventCreated, "", "")
		card, err = s.linkBorn(ctx, b, card, args.Parent)
	}
	return card, err
}

// linkBorn finishes a create under a PLANNING parent. A backend that writes
// the link at create hands the card back already parented; one that cannot
// (an embedder's) gets the link written now. Either way the parent passed
// linksArePossible before anything was written, so only the write itself
// can fail — and then the create is undone, as a failed grouping is.
func (s *Service) linkBorn(ctx context.Context, b board.Board, card board.Card, parent string) (board.Card, error) {
	if parent == "" {
		return card, nil
	}
	if card.Parent != parent {
		if err := s.backend.SetParent(ctx, b, card, parent); err != nil {
			if derr := s.backend.DeleteCard(ctx, b, card); derr != nil {
				return board.Card{}, errors.Join(err, derr)
			}
			return board.Card{}, err
		}
		card.Parent = parent
	}
	if p, ok := findCard(b, parent); ok {
		s.logEvent(ctx, b, card, board.EventParent, "", p.Title)
	}
	return card, nil
}

// reparentChildren hands what sits under a planning card to ITS parent
// before the card goes — or makes them roots when it had none — so a
// goal's tree does not fall apart when an initiative is deleted. The level
// rule holds by construction: the grandparent is above the card, the card
// above its children.
func (s *Service) reparentChildren(ctx context.Context, b board.Board, card board.Card) error {
	for _, c := range board.ChildrenOf(b, card.ItemID) {
		if err := s.backend.SetParent(ctx, b, c, card.Parent); err != nil {
			return err
		}
		to := ""
		if gp, ok := findCard(b, card.Parent); ok {
			to = gp.Title
		}
		s.logEvent(ctx, b, c, board.EventParent, card.Title, to)
	}
	return nil
}
