package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Problem is what the JSON surface under /api refuses with: RFC 9457 problem
// details, with two extension members. Code names the rule that refused for a program to branch
// on; Detail is the sentence a person reads; ActionURL, when set, is where the
// person can go to fix it.
type Problem struct {
	// Type is always "about:blank": Code carries the identity, and there is
	// no page per problem to point at (RFC 9457 §4.2.1).
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	Code      string `json:"code"`
	ActionURL string `json:"actionUrl,omitempty"`
}

func problem(status int, code, detail string) Problem {
	return Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	}
}

func writeProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// sentinels answers each refusal the service or the storage can hand back. A
// row's code is its sentinel's name — ErrCardNotFound is cardNotFound — and a
// rule that refused a change is 422.
var sentinels = []struct {
	err    error
	status int
	code   string
}{
	{boardservice.ErrCardNotFound, http.StatusNotFound, "cardNotFound"},
	{boardservice.ErrNoteNotFound, http.StatusNotFound, "noteNotFound"},
	// A board nobody has is a route that is not there — the same answer
	// the door itself gives before the service is reached at all.
	{boardservice.ErrNoSuchView, http.StatusNotFound, "noSuchView"},

	{boardservice.ErrForbidden, http.StatusForbidden, "forbidden"},
	// The Me board's narrower seat: another caller is not wrong about
	// the card, they are the wrong person to be doing this to it.
	{boardservice.ErrNotYoursToRefuse, http.StatusForbidden, "notYoursToRefuse"},
	{boardservice.ErrNotYoursToRemove, http.StatusForbidden, "notYoursToRemove"},

	{gitstore.ErrUnknownDomain, http.StatusBadRequest, "unknownDomain"},

	// The day was there and is not any more — the clone's horizon moved
	// past it. Gone says exactly that, and a client can offer to widen
	// the horizon rather than retry.
	{boardservice.ErrHistoryTruncated, http.StatusGone, "historyTruncated"},
	// Storage that keeps no past cannot be asked about one; another
	// board (or another backend) can.
	{boardservice.ErrNoHistory, http.StatusNotImplemented, "noHistory"},

	{gitstore.ErrNameTaken, http.StatusUnprocessableEntity, "nameTaken"},
	{boardservice.ErrTeamExists, http.StatusUnprocessableEntity, "teamExists"},
	{boardservice.ErrTeamNotFound, http.StatusUnprocessableEntity, "teamNotFound"},
	{boardservice.ErrInvalidStage, http.StatusUnprocessableEntity, "invalidStage"},
	{boardservice.ErrDescriptionTooLong, http.StatusUnprocessableEntity, "descriptionTooLong"},
	{boardservice.ErrNoteTooLong, http.StatusUnprocessableEntity, "noteTooLong"},
	{boardservice.ErrTitleTooLong, http.StatusUnprocessableEntity, "titleTooLong"},
	{boardservice.ErrBadDay, http.StatusUnprocessableEntity, "badDay"},
	{boardservice.ErrSubtaskDepth, http.StatusUnprocessableEntity, "subtaskDepth"},
	{boardservice.ErrSubtaskWeek, http.StatusUnprocessableEntity, "subtaskWeek"},
	// The × was told to do something the card does not allow: a slot or a
	// turn taken off the board, or a card unassigned into nowhere.
	{boardservice.ErrNotYoursToDestroy, http.StatusUnprocessableEntity, "notYoursToDestroy"},
	{boardservice.ErrNowhereToLeaveIt, http.StatusUnprocessableEntity, "nowhereToLeaveIt"},
	// A list the team does not have, and work another board owns: both
	// are rules refusing a change, not the forge failing.
	{boardservice.ErrNotYoursToPark, http.StatusUnprocessableEntity, "notYoursToPark"},
	// A shelf for a card that has no place of its own, and a process turn
	// carried out of the occurrence it is a turn of: the board's own
	// rules, answered as such.
	{boardservice.ErrNoPlaceOfItsOwn, http.StatusUnprocessableEntity, "noPlaceOfItsOwn"},
	// A create carrying a field the board it was made on does not own,
	// or missing the one that board is: the view refused the change.
	{boardservice.ErrNotOnThisBoard, http.StatusUnprocessableEntity, "notOnThisBoard"},
	{boardservice.ErrViewNeedsField, http.StatusUnprocessableEntity, "viewNeedsField"},
	{boardservice.ErrOutsideCycle, http.StatusUnprocessableEntity, "outsideCycle"},
	// Work sent back to the sprint it was done in, where there is nothing
	// to send or nowhere to send it: rules refusing a change, not a forge
	// failure.
	{boardservice.ErrNotFinished, http.StatusUnprocessableEntity, "notFinished"},
	{boardservice.ErrNoEarlierSprint, http.StatusUnprocessableEntity, "noEarlierSprint"},
	{boardservice.ErrParentNotFound, http.StatusUnprocessableEntity, "parentNotFound"},
	{boardservice.ErrOpenSubtasks, http.StatusUnprocessableEntity, "openSubtasks"},
	{boardservice.ErrTeamInUse, http.StatusUnprocessableEntity, "teamInUse"},
	{boardservice.ErrEpicInUse, http.StatusUnprocessableEntity, "epicInUse"},
	{boardservice.ErrEpicExists, http.StatusUnprocessableEntity, "epicExists"},
	{boardservice.ErrEpicNotFound, http.StatusUnprocessableEntity, "epicNotFound"},
	{boardservice.ErrProjectInUse, http.StatusUnprocessableEntity, "projectInUse"},
	{boardservice.ErrProjectExists, http.StatusUnprocessableEntity, "projectExists"},
	{boardservice.ErrProjectNotFound, http.StatusUnprocessableEntity, "projectNotFound"},
	{boardservice.ErrWeekDerived, http.StatusUnprocessableEntity, "weekDerived"},
	{boardservice.ErrNotAMonday, http.StatusUnprocessableEntity, "notAMonday"},
	{boardservice.ErrUnknownSize, http.StatusUnprocessableEntity, "unknownSize"},
	{boardservice.ErrBadCapacity, http.StatusUnprocessableEntity, "badCapacity"},
	// Written in the handler until they were found missing from the
	// other door — the service holds them now, and the answer a caller
	// gets must not change with the move.
	{boardservice.ErrEmptyTitle, http.StatusUnprocessableEntity, "emptyTitle"},
	{boardservice.ErrBackwardsDefer, http.StatusUnprocessableEntity, "backwardsDefer"},
	{boardservice.ErrNoReviewer, http.StatusUnprocessableEntity, "noReviewer"},
	{boardservice.ErrEmptyNote, http.StatusUnprocessableEntity, "emptyNote"},
	{boardservice.ErrEndBeforeStart, http.StatusUnprocessableEntity, "endBeforeStart"},
	{boardservice.ErrProcessExists, http.StatusUnprocessableEntity, "processExists"},
	{boardservice.ErrProcessNotFound, http.StatusUnprocessableEntity, "processNotFound"},
	{boardservice.ErrTurnProcess, http.StatusUnprocessableEntity, "turnProcess"},
	{boardservice.ErrNotRecurrent, http.StatusUnprocessableEntity, "notRecurrent"},
	{boardservice.ErrSubtaskTie, http.StatusUnprocessableEntity, "subtaskTie"},
	{boardservice.ErrProcessInUse, http.StatusUnprocessableEntity, "processInUse"},
	{boardservice.ErrTaskNotFound, http.StatusUnprocessableEntity, "taskNotFound"},
	{boardservice.ErrDomainConflict, http.StatusUnprocessableEntity, "domainConflict"},
	{boardservice.ErrCrossDomain, http.StatusUnprocessableEntity, "crossDomain"},
	{boardservice.ErrNoColumn, http.StatusUnprocessableEntity, "noColumn"},
	{boardservice.ErrOwnColumn, http.StatusUnprocessableEntity, "ownColumn"},
	{boardservice.ErrSubtaskMirror, http.StatusUnprocessableEntity, "subtaskMirror"},
	{boardservice.ErrNotInProject, http.StatusUnprocessableEntity, "notInProject"},
}

// problemFor answers a service error. One the table does not name is not a
// rule refusing a change: it is the forge failing, and a caller may retry it.
func problemFor(err error) Problem {
	for _, s := range sentinels {
		if errors.Is(err, s.err) {
			return problem(s.status, s.code, err.Error())
		}
	}
	return problem(http.StatusBadGateway, "upstreamFailed", err.Error())
}
