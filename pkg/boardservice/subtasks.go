package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrSubtaskDepth is returned when grouping would nest deeper than one level:
// a subtask cannot get subtasks of its own, and a card with subtasks cannot
// become one (nor can a card be grouped under itself).
var ErrSubtaskDepth = errors.New("subtasks are one level deep")

// ErrParentNotFound is returned when the requested parent card is not on the
// board (or cannot hold subtasks).
var ErrParentNotFound = errors.New("parent card not found")

// ErrNotYoursToRefuse is refusing a card that is not on the person doing the
// refusing. REFUSE is a first-person act — "I am not doing this" — so only
// the person carrying the work may say it; a lead marking somebody else's
// card refused would be putting words in their mouth. The lead's answer to a
// refusal is the × or a stage that puts the card back to work, and CLEARING
// the stage is not guarded: a rule that trapped the card in it would leave
// the lead nothing to do but delete it.
var ErrNotYoursToRefuse = errors.New("only the person a card is on can refuse it")

// ErrNotYoursToPark is a Project-board slot or a process turn sent to a list.
// Their week is another board's to say — a slot's follows its dates, a turn's
// is its process's record of the week it owed — so parking one here would
// edit a plan this board does not own.
var ErrNotYoursToPark = errors.New("this work is not this board's to park")

// ErrNoPlaceOfItsOwn is a review card or a subtask sent to a backlog. The
// backlog holds tasks with a place of their own; a review follows the card
// it reviews and a subtask stands inside its parent, so parking one
// takes it out of the only place it was ever drawn and puts it somewhere
// nothing looks. Every × on both already withheld the answer (removal.ts);
// the rule is here so the gesture is refused wherever it is made.
var ErrNoPlaceOfItsOwn = errors.New("this card has no place of its own to park")

// ErrNotYoursToRemove is a person taking off the board a card SOMEBODY ELSE
// put on it for them. Their answer to work they will not do is the refused
// stage (ErrNotYoursToRefuse names the other side of the same seat), which
// leaves the card standing where the lead can see it and decide; removing it
// takes the decision away from them.
var ErrNotYoursToRemove = errors.New("only the person who created a card can remove it from their own board")

// ErrNotYoursToDestroy is asking for a card OFF THE BOARD that this board did
// not put there: a Project-board slot is that board's commitment, and a
// process turn is its process's record of what a week was owed. The × empties
// the working area for those and stops. The request is refused rather than
// quietly turned into an unassign — a gesture that does something other than
// what it says is how the × came to be mistrusted.
var ErrNotYoursToDestroy = errors.New("this card is not this board's to destroy")

// ErrTakeIntoWork is taking a task out of its backlog with nothing to put it
// into: parked:false on a task with no date and no sprint would leave it in
// neither place (K1). A task leaves its backlog by being taken into work —
// a date, a sprint, or own with a person — and each of those unparks it.
var ErrTakeIntoWork = errors.New("a task leaves its backlog by being taken into work: give it a date, a sprint or a person")

// ErrOpenSubtasks is returned when a card with unfinished subtasks is being
// completed — closing the parent is the human's final call, made only once
// every subtask is done.
var ErrOpenSubtasks = errors.New("the card still has unfinished subtasks")

// SetParent puts a card under a parent (parent != "") or takes it out from
// under the one it has (parent == ""). What the link MEANS is the parent's
// kind (docs/design/hierarchy.md, H1): under a TASK the card becomes a
// subtask — grouping a weekly-plan card hands its weekly slot to the parent,
// the subtask takes the parent's person, team and sprint so the pair always
// carries over together; under a goal, an initiative or an epic the card is
// filed in the hierarchy and nothing else about it changes. The level rule
// (H2) and the repository rule (H6) hold either way.
func (s *Service) SetParent(ctx context.Context, boardID string, itemID, parent string) error {
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	return s.setParentOf(ctx, b, card, parent)
}

// setParentOf is SetParent with the card IN HAND. A create groups the card
// it has just written, and a re-read there answers differently for an
// embedder than for the server: inside a gitstore scope the staged file is
// invisible to a bare store, so the load fails and the create undoes
// itself — deleting a card the caller was never told about. The same
// reason Remove carries its card through the pull-out.
func (s *Service) setParentOf(ctx context.Context, b board.Board, card board.Card, parent string) error {
	if parent == "" {
		return s.ungroup(ctx, b, card)
	}
	if parent == card.ItemID {
		return ErrSubtaskDepth
	}
	p, ok := findCard(b, parent)
	if !ok || board.IsStateTitle(p.Title) || board.IsStateTitle(card.Title) {
		return ErrParentNotFound
	}
	// A planning card on either side: the hierarchy's link, which changes
	// nothing but the parent.
	if board.IsPlanning(p) || board.IsPlanning(card) {
		return s.linkInHierarchy(ctx, b, card, p)
	}
	// Two tasks: a SUBTASK, one level deep. The parent may itself sit under
	// an epic — that is not being a subtask.
	if board.IsSubtask(b, p) || len(board.Children(b, card.ItemID)) > 0 {
		return ErrSubtaskDepth
	}
	// A card in a column is nobody's subtask and has none (H4): a subtask
	// rides its parent's team, person and sprint onto the day boards, and a
	// project card has none of those to give or to take. EVERY refusal fires
	// before anything is written — the handover below writes on
	// the parent, and a refusal after it would leave the cache holding a
	// state the commit never made.
	if card.Scope != "" {
		return fmt.Errorf("%w: %q stands in a column and is nobody's subtask", ErrProjectCard, card.Title)
	}
	if p.Scope != "" {
		return fmt.Errorf("%w: %q stands in a column and groups nothing under it", ErrProjectCard, p.Title)
	}
	// A subtask always belongs to its parent's PERSON: grouping hands the
	// child over, so a family is never split across two people's Me boards.
	pLogin := ""
	if len(p.Assignees) > 0 {
		pLogin = p.Assignees[0]
	}
	cLogin := ""
	if len(card.Assignees) > 0 {
		cLogin = card.Assignees[0]
	}
	if cLogin != pLogin {
		if err := s.backend.SetAssignee(ctx, b, card, pLogin); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventAssignee, cLogin, pLogin)
	}
	// A subtask always belongs to its parent's team.
	if card.Team != p.Team {
		if err := s.backend.SetTeam(ctx, b, card, p.Team); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventTeam, card.Team, p.Team)
		card.Team = p.Team
	}
	// The subtask rides its parent's sprint from now on.
	if card.SprintStart != p.SprintStart {
		if err := s.backend.SetSprintStart(ctx, b, card, p.SprintStart); err != nil {
			return err
		}
	}
	if card.Parent != parent {
		if err := s.backend.SetParent(ctx, b, card, parent); err != nil {
			return err
		}
	}
	// A subtask rides its parent and is never parked on its own: grouped,
	// a parked task leaves its backlog and stands wherever its parent does.
	if card.Parked {
		if err := s.backend.SetBacklog(ctx, b, card, false); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventBacklog, backlogLabel(card), "")
	}
	// The log keeps titles, not item ids — readable for humans and agents.
	// A card born parented (create with parent) has card.Parent == parent
	// already: that is a fresh grouping, not a move.
	oldTitle := ""
	if card.Parent != "" && card.Parent != parent {
		if op, ok := findCard(b, card.Parent); ok {
			oldTitle = op.Title
			// Leaving a TASK parent: that parent loses a subtask. Leaving an
			// epic for a task: the epic lost nothing it counted.
			if !board.IsPlanning(op) {
				s.logEvent(ctx, b, op, board.EventSubtask, card.Title, "")
			}
		}
	}
	s.logEvent(ctx, b, card, board.EventParent, oldTitle, p.Title)
	s.logEvent(ctx, b, p, board.EventSubtask, "", card.Title)
	// Moving between parents: the old parent's derived bar loses this child.
	if card.Parent != "" && card.Parent != parent {
		if err := s.syncParentProgress(ctx, b, card.Parent, nil, card.ItemID); err != nil {
			return err
		}
	}
	child := card
	child.Parent = parent
	return s.syncParentProgress(ctx, b, parent, &child, "")
}

// linkInHierarchy files a card under a goal, an initiative or an epic — or a
// planning card under a higher one. The level rule and the repository rule
// are the whole admission; the card keeps its team, its person, its sprint,
// its week and its column, because none of those is the parent's to give
// (H1). Every refusal fires before anything is written.
func (s *Service) linkInHierarchy(ctx context.Context, b board.Board, card, p board.Card) error {
	if err := hierarchyLinkAllowed(b, card, p); err != nil {
		return err
	}
	// A project card may sit under a planning card and never under a task;
	// a planning card is never a subtask — both follow from the levels, so
	// only the plain fact is left to refuse: a project card is nobody's
	// parent (H4).
	if p.Scope != "" {
		return fmt.Errorf("%w: %q stands in a column and groups nothing under it", ErrProjectCard, p.Title)
	}
	if card.Parent == p.ItemID {
		return nil
	}
	// Leaving a TASK parent for the hierarchy is a pull-out of the group as
	// well: the old parent loses a subtask and its derived bar — and the
	// person is handed over as the ungroup gesture hands it (S8): a subtask
	// usually rides its task's person and has none of its own, and one that
	// left ownerless stood in Unassigned and on nobody's Me board.
	wasSubtask := board.IsSubtask(b, card)
	oldTitle := ""
	op, hadParent := findCard(b, card.Parent)
	if hadParent {
		oldTitle = op.Title
	}
	if wasSubtask && hadParent && len(card.Assignees) == 0 && len(op.Assignees) > 0 {
		if err := s.backend.SetAssignee(ctx, b, card, op.Assignees[0]); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventAssignee, "", op.Assignees[0])
	}
	if err := s.backend.SetParent(ctx, b, card, p.ItemID); err != nil {
		return err
	}
	if wasSubtask {
		if op, ok := findCard(b, card.Parent); ok {
			s.logEvent(ctx, b, op, board.EventSubtask, card.Title, "")
		}
		if err := s.syncParentProgress(ctx, b, card.Parent, nil, card.ItemID); err != nil {
			return err
		}
	}
	s.logEvent(ctx, b, card, board.EventParent, oldTitle, p.Title)
	// A subtask filed under the plan is a task of its own now (H1); with no
	// place of its own it is parked (K1).
	filed := card
	filed.Parent = p.ItemID
	return s.keepPlaced(ctx, b, filed, false)
}

// syncParentProgress recomputes a parent's derived progress after one of its
// subtasks changed: the mean of the children's effective progress scaled into
// 0..90 (board.DerivedProgress). changed overrides that child's stale board
// copy (and is appended when it just joined); removedID excludes a child that
// just left. A complete parent is left alone — done/100% stays the human's
// final call.
func (s *Service) syncParentProgress(ctx context.Context, b board.Board, parentID string, changed *board.Card, removedID string) error {
	p, ok := findCard(b, parentID)
	// A PLANNING parent derives nothing here: its status is read off the
	// tasks under it on every read and never written (H5).
	if !ok || board.IsPlanning(p) {
		return nil
	}
	if board.Complete(p.Stage, p.Progress) {
		// A complete parent stays the human's final call — unless an
		// UNFINISHED subtask just joined or reopened: done cannot stand on
		// top of open subtasks, so the parent reopens and derives again.
		if changed == nil || board.Complete(changed.Stage, changed.Progress) {
			return nil
		}
		if p.Stage == board.StageDone {
			if err := s.backend.SetStage(ctx, b, p, board.StageNone); err != nil {
				return err
			}
			s.logEvent(ctx, b, p, board.EventStage, string(board.StageDone), "")
			p.Stage = board.StageNone
		}
	}
	var children []board.Card
	seen := false
	for _, c := range b.Cards {
		if c.Parent != parentID || c.ItemID == removedID {
			continue
		}
		if changed != nil && c.ItemID == changed.ItemID {
			c = *changed
			seen = true
		}
		children = append(children, c)
	}
	if changed != nil && !seen && changed.Parent == parentID {
		children = append(children, *changed)
	}
	if len(children) == 0 {
		return nil
	}
	derived := board.DerivedProgress(children)
	if derived == p.Progress {
		return nil
	}
	if err := s.backend.SetProgress(ctx, b, p, derived); err != nil {
		return err
	}
	s.logEvent(ctx, b, p, board.EventProgress,
		strconv.Itoa(p.Progress), strconv.Itoa(derived))
	return nil
}

// ungroup pulls a subtask back out as a standalone card: the parent link
// goes, the child keeps what it has, and both sides record the change.
func (s *Service) ungroup(ctx context.Context, b board.Board, card board.Card) error {
	return s.ungroupKeeping(ctx, b, card)
}

// ungroupKeeping is the plain pull-out: the person is handed over (an
// ownerless child takes its parent's login, S8) and the card's own column
// must survive it. The grid's × wants neither — it clears the assignee on
// the way out — so it calls ungroupWith directly rather than through here.
func (s *Service) ungroupKeeping(ctx context.Context, b board.Board, card board.Card) error {
	return s.ungroupWith(ctx, b, card, true)
}

// ungroupWith is ungroupKeeping with a say over the hand-over of the
// parent's person. A pull-out is a re-file — the card leaves its parent's
// repository for whatever its own project or team names — and strands
// nothing on the way: a subtask carries no column (H4).
func (s *Service) ungroupWith(ctx context.Context, b board.Board, card board.Card, handOver bool) error {
	if card.Parent == "" {
		return nil
	}
	if err := s.backend.SetParent(ctx, b, card, ""); err != nil {
		return err
	}
	op, hadParent := findCard(b, card.Parent)
	// Taking a card out from under a PLANNING card is the plain unlink: it
	// was never riding that parent, so there is no person to hand over and
	// no group to record leaving (H1).
	freed := card
	freed.Parent = ""
	if hadParent && board.IsPlanning(op) {
		s.logEvent(ctx, b, card, board.EventParent, op.Title, "")
		return s.keepPlaced(ctx, b, freed, false)
	}
	// A subtask usually has no assignee of its own — it rides the
	// parent's — so a pull-out left it ownerless: gone from every
	// Me board and sitting in Unassigned, which from the person
	// who pulled it looks exactly like the card vanishing.
	// deleteWithCascade hands a released child the parent's person for
	// this same reason.
	if hadParent && handOver && len(card.Assignees) == 0 && len(op.Assignees) > 0 {
		if err := s.backend.SetAssignee(ctx, b, card, op.Assignees[0]); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventAssignee, "", op.Assignees[0])
	}
	// The log keeps titles, not item ids — that is what a human (or an
	// agent reading list_log) can act on. Both sides record the change.
	if hadParent {
		s.logEvent(ctx, b, card, board.EventParent, op.Title, "")
		s.logEvent(ctx, b, op, board.EventSubtask, card.Title, "")
	} else {
		s.logEvent(ctx, b, card, board.EventParent, card.Parent, "")
	}
	if err := s.syncParentProgress(ctx, b, card.Parent, nil, card.ItemID); err != nil {
		return err
	}
	// Out of the group with no place of its own (its parent was parked, and
	// it rode it out of every day): it lands in its team's backlog (K1).
	return s.keepPlaced(ctx, b, freed, false)
}
