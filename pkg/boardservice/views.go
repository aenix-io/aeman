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
// not own — a parked card typed into a day, a column named on the Me board.
// It names the field, because a create that quietly dropped it would answer
// with a card that is not the one the caller described.
var ErrNotOnThisBoard = errors.New("this board does not make cards like that")

// ErrViewNeedsField is the other half: a board whose whole gesture is one
// field, asked to make a card without it — a Triage card with no week is in no
// column, a Project card with no column is on no row.
var ErrViewNeedsField = errors.New("this board needs it to mean anything")

// CreateInView creates a card the way the BOARD it was made on makes them.
//
// A person typing a title does it somewhere: into the Me board, where the card
// is theirs and today's; into a Triage week, where it is scheduled for that
// week and stands on no day; into the drawer, where it is parked; into a
// Project column, where it is a slot whose row is its start date. The fields
// that encode those four — `personal`, `week`, `parked`, `epic` — were the
// whole vocabulary the API had, so a caller had to know which COMBINATIONS are
// boards and which are states no board draws (parked with a week is a card in
// the drawer and in the plan at once). The view is the board, said once.
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
	// A card typed into a week, a shelf or a column stands on no day, so the
	// three boards that make one all refuse the same three fields.
	dated := args.Day != "" || args.Start != "" || args.SprintStart != ""
	var refusals []viewRefusal
	switch view {
	case board.ViewAll:
		// No board at all: the caller said so, and gets the raw door.
		return args, nil
	case board.ViewMe, board.ViewTeam:
		refusals = []viewRefusal{
			{args.Parked, "parked"}, {args.Epic != "", "epic"},
			{args.Personal, "personal"}, {args.Week != "", "week"},
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
	case board.ViewTriage:
		refusals = []viewRefusal{
			{args.Parked, "parked"}, {args.Personal, "personal"},
			{args.Epic != "", "epic"}, {dated, "a day"},
		}
		if args.Week == "" {
			return args, fmt.Errorf("%w: a card of the Triage board is a card of a week", ErrViewNeedsField)
		}
	case board.ViewBacklog:
		refusals = []viewRefusal{
			{args.Week != "", "week"}, {args.Personal, "personal"},
			{args.Epic != "", "epic"}, {dated, "a day"},
		}
		// Born on the shelf rather than born in the strip and moved: one
		// commit, and no instant in between where the card stands somewhere
		// nobody put it.
		args.Parked = true
	case board.ViewProject:
		refusals = []viewRefusal{{args.Parked, "parked"}, {args.Personal, "personal"}}
		if args.Epic == "" {
			return args, fmt.Errorf("%w: a card of the Project board stands in a column", ErrViewNeedsField)
		}
	case board.ViewPersonal:
		refusals = []viewRefusal{
			{args.Team != "", "team"}, {args.Epic != "", "epic"},
			{args.Week != "", "week"}, {args.Parked, "parked"},
		}
		// The personal column stands beside the Me day and shares its add
		// form, so it shares the band it adds in.
		if err := addsUnplanned(view, &args); err != nil {
			return args, err
		}
		args.Personal = true
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

// A GESTURE is a board's own press: the ×, the drop into a week, the pull back
// into the strip, "this was finished in the sprint before". What it means
// depends on the board it was made on, which is why it is addressed through
// one (`/api/v1/views/{view}/cards/{uid}/actions/{gesture}`) — and a board
// that does not draw it does not answer it.
//
// The pane actions are NOT here. Deferring a card, putting it in progress,
// sending it to review, mirroring it into a second column: those mean the same
// thing wherever the card was opened from, so they stay addressed as the card
// itself.
type Gesture string

const (
	// GestureRemove is the ×, whose meaning the intent carries (RemoveIntent)
	// and whose OFFER is the board's: every board draws one.
	GestureRemove Gesture = "remove"
	// GesturePlace is the Triage board's drop: give the card a week.
	GesturePlace Gesture = "place"
	// GestureUntriage is its opposite, on the same board: take the week back
	// and put the card in the strip.
	GestureUntriage Gesture = "untriage"
	// GestureFinishedEarlier is the day boards': work done in the sprint
	// before this one and only marked done now.
	GestureFinishedEarlier Gesture = "finished-earlier"
)

// gestureBoards is which boards draw each one. The escape hatch is in every
// list: a caller that said "all" has no board to be refused by.
var gestureBoards = map[Gesture][]board.View{
	GestureRemove: {board.ViewMe, board.ViewTeam, board.ViewTriage, board.ViewBacklog,
		board.ViewProject, board.ViewPersonal, board.ViewAll},
	GesturePlace:           {board.ViewTriage, board.ViewAll},
	GestureUntriage:        {board.ViewTriage, board.ViewAll},
	GestureFinishedEarlier: {board.ViewMe, board.ViewTeam, board.ViewAll},
}

// Offers reports whether a board draws a gesture. A gesture asked of a board
// that does not is answered as a route that is not there, because that is what
// it is: there is no × on the Project board's drawer and no week to drop into
// on the Me board.
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
	for _, g := range []Gesture{GestureRemove, GesturePlace, GestureUntriage, GestureFinishedEarlier} {
		if Offers(view, g) {
			out = append(out, g)
		}
	}
	return out
}

// addsUnplanned holds a create on the Me board (and the personal column beside
// it) to the UNPLANNED band.
//
// Something that came up today is unplanned by definition. The other three
// zones are the PLAN — critical means "today, before anything else", planned
// means somebody weighed it into the week, "if time left" is a day's spare
// capacity — and the plan is the lead's to make on the Team board. A person
// filing their own work under Urgent or Planned is planning, on a board with
// no room to argue with it: the Me board has no lead's seat and no week to
// weigh it against.
//
// The browser has drawn its add form in that one band all along
// (web/src/meboard.ts, ADD_ZONE) and nothing else knew. The band a card ends
// up in is still anybody's to change afterwards — the card's own zone picker
// is a card-level gesture, made from any board — so this is about where work
// is FILED, not about where it may stand.
func addsUnplanned(view board.View, args *CreateCardArgs) error {
	if args.Zone == board.ZoneNone {
		args.Zone = board.ZoneYellow
		return nil
	}
	if args.Zone == board.ZoneYellow {
		return nil
	}
	return fmt.Errorf("%w: the %s board adds work as unplanned — the other bands are the plan, and the plan is made on the team's grid", ErrNotOnThisBoard, view)
}
