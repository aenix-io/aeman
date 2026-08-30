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
	// A weekly-plan card grouped under a parent hands its slot to the parent
	// (the parent replaces it in the Weekly plan); a parent already in the
	// plan keeps its own slot and the subtask's simply clears. A SLOT parent
	// receives nothing: it is on the Weekly panel by its span already, and
	// writing the subtask's week onto it is the conflicting write SetWeek
	// refuses — the refusal used to kill the whole grouping.
	if card.Plan != board.PlanNone {
		if p.Plan == board.PlanNone && p.Epic == "" {
			if err := s.backend.SetPlan(ctx, b, p, card.Plan); err != nil {
				return err
			}
			if err := s.backend.SetWeek(ctx, b, p, card.Week); err != nil {
				return err
			}
		}
		if err := s.backend.SetPlan(ctx, b, card, board.PlanNone); err != nil {
			return err
		}
		if err := s.backend.SetWeek(ctx, b, card, ""); err != nil {
			return err
		}
	}
	// A subtask is placed nowhere of its own: its mirrors go with the
	// grouping the way its plan slot does. Left on, they were placements no
	// board showed — the Project grid skips subtasks — yet InEpic counted
	// them, so DeleteEpic refused for cards nobody could see; and a parent
	// in another repository would carry the file away from them entirely.
	// The home column stays: G14 blesses a subtask carrying its own column.
	if err := groupingKeepsTheColumn(b, card, parent); err != nil {
		return err
	}
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
// (G14, and the Project board draws it there, S4); what it may not keep is
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

// groupingKeepsTheColumn refuses a grouping that would strand the ONE
// column a subtask may keep (G14). Its file follows its parent, so a
// parent in another repository leaves that column naming a repository
// that no longer holds the card — exactly what SetEpic refuses when the
// card itself is moved. The column is the card's own, not a placement of
// the parentless life that grouping may clear, so this refuses rather
// than clearing.
func groupingKeepsTheColumn(b board.Board, card board.Card, parent string) error {
	if card.Epic == "" {
		return nil
	}
	r := board.Resolver(b, "")
	after := card
	after.Parent = parent
	if pd, ok := r.ProjectDomain(card.Project); !ok || pd != board.DomainOf(after, r) {
		return fmt.Errorf("%w: %q stands in a column of another repository — take it out of the column first",
			ErrCrossDomain, card.Title)
	}
	return nil
}

// ungroup pulls a subtask back out as a standalone card: the parent link
// goes, the child keeps what it has, and both sides record the change.
func (s *Service) ungroup(ctx context.Context, b board.Board, card board.Card) error {
	if card.Parent == "" {
		return nil
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
	if op, ok := findCard(b, card.Parent); ok &&
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
