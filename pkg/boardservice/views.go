package boardservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrNoSuchView is a board nobody has. The empty string is one of them: a door
// that defaults an unspecified view does so itself and on purpose, and a view
// that arrived empty because something forgot to send it must not become
// "everything" on the way in.
var ErrNoSuchView = errors.New("no such board")

// ErrNotOnThisBoard is a create carrying a field the board it was made on does
// not own — a parked card typed into a day, a type named on the Me board.
// It names the field, because a create that quietly dropped it would answer
// with a card that is not the one the caller described.
var ErrNotOnThisBoard = errors.New("this board does not make cards like that")

// ErrViewNeedsField is the other half: a board whose whole gesture is one
// field, asked to make a card without it — a Roadmap card that is neither a
// plan item nor a task under one.
var ErrViewNeedsField = errors.New("this board needs it to mean anything")

// CreateInView creates a card the way the BOARD it was made on makes them.
//
// A person typing a title does it somewhere: into the Me board, where the card
// is theirs and today's; into a team's backlog, where it is parked, last. The
// field that encodes the last — `parked` — was the whole
// vocabulary the API had, so a caller had to know which COMBINATIONS are
// boards and which are states no board draws (parked with a date is a card in
// the backlog and in work at once). The view is the board, said once.
//
// It is one door onto CreateCard rather than a second create: everything below
// — the title resolve, the roster guard, the sprint arithmetic — is the same,
// and only what the caller may say changes.
func (s *Service) CreateInView(ctx context.Context, boardID string, view board.View, args CreateCardArgs) (board.Card, error) {
	args, err := createArgsFor(ctx, view, args)
	if err != nil {
		return board.Card{}, err
	}
	return s.CreateCard(ctx, boardID, args)
}

// createArgsFor is that mapping, alone and testable: what each board fills in,
// and what it refuses. It is deliberately a pure function of the view and the
// arguments — the same answer for the HTTP door and for an agent, which is the
// whole point of it living here rather than in either.
func createArgsFor(ctx context.Context, view board.View, args CreateCardArgs) (CreateCardArgs, error) {
	if !board.KnownView(string(view)) {
		return args, fmt.Errorf("%w: %q", ErrNoSuchView, view)
	}
	// A card typed into a backlog stands on no day, so that board refuses
	// the fields that would give it one.
	dated := args.Day != "" || args.Start != "" || args.SprintStart != ""
	// A place among the cards is the Backlog's and the day boards': a
	// backlog and a Team or Me cell are slices of the board's order, and the
	// insert "+" in a gap names its neighbour. The Roadmap places a card by
	// its parent.
	placed := args.After != "" || args.Before != ""
	var refusals []viewRefusal
	switch view {
	case board.ViewAll:
		// No board at all: the caller said so, and gets the raw door.
		return args, nil
	case board.ViewStrategy:
		// A card made on the Roadmap is a PLANNING card — a goal, an
		// initiative or an epic — and the type is the whole gesture (H7). It
		// takes its dates and a parent, and none of a task's day-board state,
		// nor an owner or a team (H3). The one task the Roadmap makes is a
		// task UNDER a planning card — the epic's + — born with a title, a
		// parent and, when given, dates: no team, no owner, no band, no
		// size, no sprint. With no dates it is parked in the no-team
		// group's backlog (K3), and it stands on the Roadmap under its
		// parent. A task with no parent is not the Roadmap's.
		if args.Type == board.TypeTask && args.Parent == "" {
			return args, fmt.Errorf("%w: a card of the Strategy board is a goal, an initiative or an epic, or a task under one", ErrViewNeedsField)
		}
		if args.Type == board.TypeTask {
			args.Undated = true
		}
		refusals = []viewRefusal{
			{args.Parked, "parked"},
			{args.ReviewOf != "", "reviewOf"},
			{args.SprintStart != "", "sprintStart"},
			// A planning card may be born with its team, its owner and its
			// customers (O1); the Roadmap's task under one is born with
			// none of a team or an owner.
			{args.Team != "" && args.Type == board.TypeTask, "team"},
			{args.Owner != "" && args.Type == board.TypeTask, "owner"},
			{args.Assignee != "", "assignee"},
			{args.Zone != board.ZoneNone, "zone"}, {args.Size != "", "size"},
			{placed, "a place among the cards"},
		}
	case board.ViewMe, board.ViewTeam:
		refusals = []viewRefusal{
			{args.Parked, "parked"},
			{args.Type != "", "type"},
		}
		// The Me board files for the person reading it — including a lead
		// looking at somebody else's day, who says whose with an assignee.
		// The Team grid does NOT: a card typed there with nobody named lands
		// in the Unassigned column, which is a real place on that board.
		if view == board.ViewMe && args.Assignee == "" {
			args.Assignee = board.ActorFrom(ctx)
		}
		if view == board.ViewMe {
			if err := addsUnplanned(view, &args); err != nil {
				return args, err
			}
		}
	case board.ViewBacklog:
		refusals = []viewRefusal{
			{dated, "a day"}, {args.Type != "", "type"},
		}
		// Born in the backlog rather than born somewhere else and moved: one
		// commit, and no instant in between where the card stands somewhere
		// nobody put it. It lands last, or where After/Before place it (the
		// insert "+" between two cards).
		args.Parked = true
	}
	for _, r := range refusals {
		if r.bad {
			return args, fmt.Errorf("%w: the %s board does not take %s", ErrNotOnThisBoard, view, r.field)
		}
	}
	return args, nil
}

// viewRefusal is one field a board does not own, with whether the caller sent
// it. A slice of them per board is the whole difference between the add-boxes.
type viewRefusal struct {
	bad   bool
	field string
}

// A GESTURE is a board's own press: the ×, "whose is this", "this was
// finished in the sprint before". What it means
// depends on the board it was made on, which is why it is addressed through
// one (`/api/v1/views/{view}/cards/{uid}/actions/{gesture}`) — and a board
// that does not draw it does not answer it.
//
// The pane actions are NOT here. Deferring a card, putting it in progress,
// sending it to review: those mean the same
// thing wherever the card was opened from, so they stay addressed as the card
// itself.
type Gesture string

const (
	// GestureRemove is the ×, whose meaning the intent carries (RemoveIntent)
	// and whose OFFER is the board's: every board draws one.
	GestureRemove Gesture = "remove"
	// GestureFinishedEarlier is the day boards': work done in the sprint
	// before this one and only marked done now.
	GestureFinishedEarlier Gesture = "finished-earlier"
	// GestureOwn is the Roadmap's "whose is this": a team and a person, a
	// team alone, or nobody — and what that does to a card with no plan yet
	// (own.go, H10). Every board that draws a task offers it, so the drawer
	// asks the same question from wherever the card was opened.
	GestureOwn Gesture = "own"
	// GestureTake is the Backlog's "I take this into work": the card becomes
	// the actor's, on its own team, and leaves the backlog (take.go, K11).
	GestureTake Gesture = "take"
)

// gestureBoards is which boards draw each one. The escape hatch is in every
// list: a caller that said "all" has no board to be refused by.
var gestureBoards = map[Gesture][]board.View{
	GestureRemove: {board.ViewMe, board.ViewTeam, board.ViewBacklog,
		board.ViewStrategy, board.ViewAll},
	GestureFinishedEarlier: {board.ViewMe, board.ViewTeam, board.ViewAll},
	// The Backlog's owner picker is how a task is taken into work from
	// there (K4).
	GestureOwn: {board.ViewStrategy, board.ViewMe, board.ViewTeam,
		board.ViewBacklog, board.ViewAll},
	GestureTake: {board.ViewBacklog, board.ViewAll},
}

// Offers reports whether a board draws a gesture. A gesture asked of a board
// that does not is answered as a route that is not there, because that is what
// it is: there is no "finished earlier" on the Backlog.
func Offers(view board.View, g Gesture) bool {
	for _, v := range gestureBoards[g] {
		if v == view {
			return true
		}
	}
	return false
}

// Gestures lists the ones a board draws, in a stable order, for the catalog a
// client reads instead of being told in a description.
func Gestures(view board.View) []Gesture {
	var out []Gesture
	for _, g := range []Gesture{GestureRemove, GestureFinishedEarlier, GestureOwn, GestureTake} {
		if Offers(view, g) {
			out = append(out, g)
		}
	}
	return out
}

// addsUnplanned holds a create on the Me board to the UNPLANNED band.
//
// Something that came up today is unplanned by definition. The other three
// zones are the PLAN — critical means "today, before anything else", planned
// means somebody weighed it into the week, "if time left" is a day's spare
// capacity — and the plan is the lead's to make on the Team board. A person
// filing their own work under Urgent or Planned is planning, on a board with
// no room to argue with it: the Me board has no lead's seat.
//
// The browser has drawn its add form in that one band all along
// (web/src/meboard.ts, ADD_ZONE) and nothing else knew. The band a card ends
// up in is still anybody's to change afterwards — the card's own zone picker
// is a card-level gesture, made from any board — so this is about where work
// is FILED, not about where it may stand.
func addsUnplanned(view board.View, args *CreateCardArgs) error {
	// A SUBTASK is not the board's add form: it is a piece of the card it
	// hangs under and takes that card's band, which is what the subtask form
	// under a planned parent passes (MeBoard.tsx). Holding it to the
	// unplanned band would mean a piece of planned work could not be written
	// down on the board the work is being done from.
	if args.Parent != "" {
		return nil
	}
	if args.Zone == board.ZoneNone {
		args.Zone = board.ZoneYellow
		return nil
	}
	if args.Zone == board.ZoneYellow {
		return nil
	}
	return fmt.Errorf("%w: the %s board adds work as unplanned — the other bands are the plan, and the plan is made on the team's grid", ErrNotOnThisBoard, view)
}
