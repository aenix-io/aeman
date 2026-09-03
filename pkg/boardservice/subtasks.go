package boardservice

import (
	"context"
	"errors"
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

// ErrSubtaskWeek is asking for a card that is both a subtask and scheduled
// for a week of its own. A subtask has no week — grouping hands its week to
// the parent — so the pair is two contradictory requests, and answering it
// by scheduling the PARENT for the week named for the child mutates a card
// nobody asked about.
var ErrSubtaskWeek = errors.New("a subtask has no week of its own")

// ErrNotYoursToRefuse is refusing a card that is not on the person doing the
// refusing. REFUSE is a first-person act — "I am not doing this" — so only
// the person carrying the work may say it; a lead marking somebody else's
// card refused would be putting words in their mouth. The lead's answer to a
// refusal is the × or a stage that puts the card back to work, and CLEARING
// the stage is not guarded: a rule that trapped the card in it would leave
// the lead nothing to do but delete it.
var ErrNotYoursToRefuse = errors.New("only the person a card is on can refuse it")

// ErrNotYoursToPlan is a person filing work for THEMSELVES into one of the
// planned zones. A person adds work to their own board only as unplanned —
// something came up today; the other three zones are the plan, and planning
// is done with the team rather than filed quietly into one's own column.
// Planning somebody ELSE's work is the lead's gesture and passes, as does a
// card placed by the thing it belongs to (a column, a parent, a review).
var ErrNotYoursToPlan = errors.New("work you plan for yourself is unplanned work")

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

// ErrNowhereToLeaveIt is asking to unassign a card that has nowhere to be
// left: no week, no column. Emptying the working area would leave it with no
// person, no dates and no home — alive on no board anyone can open. Taking it
// OFF the board is the answer for such a card, and the caller has to say so.
var ErrNowhereToLeaveIt = errors.New("the card has nowhere to be left: no week, no column")

// ErrOpenSubtasks is returned when a card with unfinished subtasks is being
// completed — closing the parent is the human's final call, made only once
// every subtask is done.
var ErrOpenSubtasks = errors.New("the card still has unfinished subtasks")

// SetParent groups a card under a parent (parent != "") or pulls a subtask
// back out as a standalone card (parent == ""). Grouping a weekly-plan card
// hands its weekly slot to the parent — the parent replaces it in the plan —
// since a subtask is never a plan card; the subtask also joins its parent's
// sprint, so the pair always carries over together.
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
	if !ok || p.Title == board.SprintStateTitle || card.Title == board.SprintStateTitle {
		return ErrParentNotFound
	}
	if p.Parent != "" || len(board.Children(b, card.ItemID)) > 0 {
		return ErrSubtaskDepth
	}
	// Grouping moves the card's file to its parent's repository, so it is a
	// re-file like any other: the same guard, the same three references it
	// could strand. EVERY refusal fires before anything is written — the
	// weekly-plan handover below hands the child's slot to the parent, and
	// a guard speaking after it left the cache holding a state the commit
	// never made (the refusal aborts the write, not the cache mutation).
	// The closure models what grouping PRODUCES, riders and all: it clears
	// the tie and the mirrors (clearRiders below), so the guard must not
	// refuse over either — only over what grouping keeps, the card's own
	// column and the columns of everything that follows it.
	if err := refileGuard(b, card, func(a *board.Card) {
		a.Parent = parent
		a.Process = ""
		a.Mirrors = nil
	}); err != nil {
		return err
	}
	// A card scheduled for a WEEK hands that week to the parent, which stands
	// for it from then on; a parent that has a week of its own keeps it and
	// the subtask's simply clears. A SLOT parent receives nothing: its row is
	// its span already, and writing the subtask's week onto it is the
	// conflicting write SetWeek refuses — the refusal used to kill the whole
	// grouping.
	if card.Week != "" {
		if p.Week == "" && p.Epic == "" {
			if err := s.backend.SetWeek(ctx, b, p, card.Week); err != nil {
				return err
			}
		}
		// The WEEK stays for a card that stands in a COLUMN. There it is the
		// row the Project board draws the card in — its span speaks for it —
		// and clearing it took the card's row away, to be re-derived from
		// dates the client cannot see.
		if !hasColumn(card) {
			if err := s.backend.SetWeek(ctx, b, card, ""); err != nil {
				return err
			}
		}
	}
	// A subtask keeps the ONE column it carries — G14 blesses that, and the
	// Project board draws it there (G57) — but not a SECOND placement or a
	// process tie: its file follows its parent, so both would be stranded
	// the moment the parent changes repository. See clearRiders.
	if err := s.clearRiders(ctx, b, card); err != nil {
		return err
	}
	// A subtask always belongs to its parent's PERSON: grouping hands the
	// child over, so a family is never split across two personal boards.
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
	// The log keeps titles, not item ids — readable for humans and agents.
	// A card born parented (create with parent) has card.Parent == parent
	// already: that is a fresh grouping, not a move.
	oldTitle := ""
	if card.Parent != "" && card.Parent != parent {
		if op, ok := findCard(b, card.Parent); ok {
			oldTitle = op.Title
			s.logEvent(ctx, b, op, board.EventSubtask, card.Title, "")
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

// syncParentProgress recomputes a parent's derived progress after one of its
// subtasks changed: the mean of the children's effective progress scaled into
// 0..90 (board.DerivedProgress). changed overrides that child's stale board
// copy (and is appended when it just joined); removedID excludes a child that
// just left. A complete parent is left alone — done/100% stays the human's
// final call.
func (s *Service) syncParentProgress(ctx context.Context, b board.Board, parentID string, changed *board.Card, removedID string) error {
	p, ok := findCard(b, parentID)
	if !ok {
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

// clearRiders strips what a subtask cannot carry — mirrors and the process
// tie — when a card is grouped. A subtask keeps the ONE column it carries
// (G14, and the Project board draws it there, G57); what it may not keep is
// a SECOND placement or a tie, because its file follows its parent: a
// parent in another repository would carry the card away from both, which
// is the state ErrCrossDomain exists to prevent. The untying is logged;
// the mirrors go silently on purpose — like the plan slot, they are
// placements of the parentless life, not work.
func (s *Service) clearRiders(ctx context.Context, b board.Board, card board.Card) error {
	if len(card.Mirrors) > 0 {
		if err := s.backend.SetMirrors(ctx, b, card, nil); err != nil {
			return err
		}
	}
	if card.Process != "" {
		if err := s.backend.SetProcess(ctx, b, card, ""); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventProcess, card.Process, "")
	}
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
// the way out, and it repairs the column itself — so it calls ungroupWith
// directly rather than through here.
func (s *Service) ungroupKeeping(ctx context.Context, b board.Board, card board.Card) error {
	return s.ungroupWith(ctx, b, card, true, true)
}

// ungroupWith is ungroupKeeping with a say over the card's own column:
// the grid's × repairs that column itself, so it asks the guard about
// everything else and answers for the column afterwards.
func (s *Service) ungroupWith(ctx context.Context, b board.Board, card board.Card, handOver, ownColumn bool) error {
	if card.Parent == "" {
		return nil
	}
	// A pull-out is a re-file too: the card leaves its parent's repository
	// for whatever its own project or team names, and takes its followers'
	// files with it.
	if err := refileGuardOpts(b, card, func(a *board.Card) { a.Parent = "" }, ownColumn); err != nil {
		return err
	}
	if err := s.backend.SetParent(ctx, b, card, ""); err != nil {
		return err
	}
	// A subtask usually has no assignee of its own — it rides the
	// parent's — so a pull-out left it ownerless: gone from every
	// personal board and sitting in Unassigned, which from the person
	// who pulled it looks exactly like the card vanishing.
	// deleteWithCascade hands a released child the parent's person for
	// this same reason.
	if op, ok := findCard(b, card.Parent); ok && handOver &&
		len(card.Assignees) == 0 && len(op.Assignees) > 0 {
		if err := s.backend.SetAssignee(ctx, b, card, op.Assignees[0]); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventAssignee, "", op.Assignees[0])
	}
	// The log keeps titles, not item ids — that is what a human (or an
	// agent reading list_log) can act on. Both sides record the change.
	if op, ok := findCard(b, card.Parent); ok {
		s.logEvent(ctx, b, card, board.EventParent, op.Title, "")
		s.logEvent(ctx, b, op, board.EventSubtask, card.Title, "")
	} else {
		s.logEvent(ctx, b, card, board.EventParent, card.Parent, "")
	}
	return s.syncParentProgress(ctx, b, card.Parent, nil, card.ItemID)
}
