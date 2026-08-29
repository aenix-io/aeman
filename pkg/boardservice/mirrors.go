package boardservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/aenix-io/aeman/pkg/board"
)

// A mirror is the same card standing in a second Project-board column: one
// file, one log, one set of dates — shown in both projects, so shared work
// is one card on one person instead of a duplicate per project drifting
// apart. The card's own (project, epic) pair stays its home: the domain
// rule reads it, promotion hands it over, mirrors only add columns that
// show the card. See board/mirrors.go.

// ErrCrossDomain is a mirror target in another repository: a card is one
// file in one repository, and a column elsewhere cannot show a file its
// readers may not have (G15).
var ErrCrossDomain = errors.New("the target project lives in another repository")

// ErrNotInProject is removing a card from a column it does not stand in.
var ErrNotInProject = errors.New("the card does not stand in that column")

// ErrNoColumn is mirroring a card that stands in no column: there is
// nothing to mirror — attach it to a project first.
var ErrNoColumn = errors.New("the card is in no project column")

// ErrOwnColumn is naming the card's own column as a mirror target.
var ErrOwnColumn = errors.New("the card's own column is not a mirror target")

// ErrSubtaskMirror is mirroring a subtask: it rides its parent and is
// placed nowhere of its own, so a mirror on it would be a placement no
// board ever shows — counted by the guards, seen by nobody.
var ErrSubtaskMirror = errors.New("a subtask rides its parent and cannot be mirrored")

// ErrTurnProcess is re-tying a process turn: a turn belongs to its task,
// and the task names the process — a process written on the turn itself
// would contradict the task and silently lose to it on every read.
var ErrTurnProcess = errors.New("a process turn's process is its task's — it cannot be re-tied")

// ErrNotRecurrent is tying a non-recurrent card to a process: the
// recurring shelf is where a tie shows, and a tie nothing draws is the
// kind of invisible state every guard here exists to refuse.
var ErrNotRecurrent = errors.New("only a recurrent card can be tied to a process")

// Mirror adds the column (project, epic) to the card. The card must already
// have a home column — a card outside every project is attached, not
// mirrored — the target must exist, in the same repository as the home, and
// mirroring where the card already stands is a no-op, not a duplicate.
func (s *Service) Mirror(ctx context.Context, boardID string, itemID, project, epic string) error {
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if c.Parent != "" {
		return ErrSubtaskMirror
	}
	if c.Epic == "" {
		return fmt.Errorf("%w — attach it to one first", ErrNoColumn)
	}
	if c.Project == project && c.Epic == epic {
		return fmt.Errorf("%w: %q / %q", ErrOwnColumn, project, epic)
	}
	if _, ok := board.FindEpic(b, project, epic); !ok {
		return fmt.Errorf("%w %q in project %q", ErrEpicNotFound, epic, project)
	}
	if c.Project == "" {
		// The no-project bucket names no repository, so MirrorAllowed has
		// nothing to compare a target against — said plainly, not as a
		// refusal quoting an empty project name.
		return fmt.Errorf("%w: the card's column belongs to no project — file it under one first", ErrCrossDomain)
	}
	if !board.MirrorAllowed(b, c.Project, project) {
		return fmt.Errorf("%w: %q is not in %q's repository", ErrCrossDomain, project, c.Project)
	}
	// The card's FILE follows a linked original before its project (linked
	// cards first, G14): a review card of an original in another repository
	// lives there, whatever its home column says — and no column of this
	// repository may show a file its readers may not have (G15).
	r := board.Resolver(b, "")
	if hd, ok := r.ProjectDomain(c.Project); ok && board.DomainOf(c, r) != hd {
		return fmt.Errorf("%w: the card's file lives in another repository (its review link decides)", ErrCrossDomain)
	}
	if board.Mirrored(c, project, epic) {
		return nil
	}
	next := append(append([]board.Placement{}, c.Mirrors...), board.Placement{Project: project, Epic: epic})
	if err := s.backend.SetMirrors(ctx, b, c, next); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventMirror, "", project+" / "+epic)
	return nil
}

// Unmirror takes the column (project, epic) away from the card's mirrors;
// its home and everything else stay as they are.
func (s *Service) Unmirror(ctx context.Context, boardID string, itemID, project, epic string) error {
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if !board.Mirrored(c, project, epic) {
		return fmt.Errorf("%w: %q / %q", ErrNotInProject, project, epic)
	}
	next := make([]board.Placement, 0, len(c.Mirrors)-1)
	for _, m := range c.Mirrors {
		if m.Project == project && m.Epic == epic {
			continue
		}
		next = append(next, m)
	}
	if err := s.backend.SetMirrors(ctx, b, c, next); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventMirror, project+" / "+epic, "")
	return nil
}

// RemoveFromProject is the Project board's ×: it removes the card from ONE
// column, and what that means depends on which column it is.
//
//   - A mirror simply goes; the card stays everywhere else.
//   - The home, with mirrors left, hands the home role to the first mirror
//     — the card's file, log and dates are shared and stay.
//   - The last column takes the card off the Project board entirely, and
//     off the weekly plan ALWAYS — a plan entry was this project's work,
//     and the project just let go of it. The card survives only in the
//     working area, and only when it was worked on (someone had it and
//     moved it); an untouched card is deleted outright. The UI asks first
//     when work would go (deleteWarning).
func (s *Service) RemoveFromProject(ctx context.Context, boardID string, itemID, project, epic string) error {
	// A column is named by its EPIC — an empty epic is no column: without
	// this, a card standing in no column "matched" ("", "") and fell
	// through to the last-column branch, deleted outright by the very call
	// that asked to remove it from nowhere. The PROJECT half may be empty:
	// the no-project bucket is a real column with a working ×, and a card
	// outside every column has an empty epic, so ("", epic) on it honestly
	// mismatches below. This package is a public contract and the MCP tool
	// feeds it directly, so the guard lives here.
	if epic == "" {
		return fmt.Errorf("%w: a column is named by its epic — the epic half is required", ErrNotInProject)
	}
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if board.Mirrored(c, project, epic) {
		return s.Unmirror(ctx, boardID, itemID, project, epic)
	}
	if c.Project != project || c.Epic != epic {
		return fmt.Errorf("%w: %q / %q", ErrNotInProject, project, epic)
	}
	if len(c.Mirrors) > 0 {
		// Promote: the first mirror becomes the home. Same repository by
		// construction (Mirror admits nothing else), so the card does not
		// move between domains.
		heir := c.Mirrors[0]
		if err := s.backend.SetMirrors(ctx, b, c, append([]board.Placement{}, c.Mirrors[1:]...)); err != nil {
			return err
		}
		if err := s.backend.SetProject(ctx, b, c, heir.Project); err != nil {
			return err
		}
		if err := s.backend.SetEpic(ctx, b, c, heir.Epic); err != nil {
			return err
		}
		s.logEvent(ctx, b, c, board.EventEpic, project+" / "+epic, heir.Project+" / "+heir.Epic)
		return nil
	}
	// The last column. The weekly plan goes with it, always.
	if c.Plan != board.PlanNone {
		if err := s.backend.SetPlan(ctx, b, c, board.PlanNone); err != nil {
			return err
		}
	}
	if c.Week != "" {
		if err := s.backend.SetWeek(ctx, b, c, ""); err != nil {
			return err
		}
	}
	worked := len(c.Assignees) > 0 && c.Progress > 0
	if !worked {
		return s.deleteWithCascade(ctx, b, c)
	}
	if err := s.backend.SetEpic(ctx, b, c, ""); err != nil {
		return err
	}
	if err := s.backend.SetProject(ctx, b, c, ""); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventEpic, project+" / "+epic, "")
	return nil
}

// SetCardProcess names the process a card belongs to — the recurring
// shelf's counterpart of attaching a card to a project. The process must
// exist (a typo is not a new process); "" clears the tie.
func (s *Service) SetCardProcess(ctx context.Context, boardID string, itemID, process string) error {
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	// A turn is refused before anything else: processOf resolves its task
	// first, so a process: key written here would never be read back — the
	// silent no-op this guard turns into an honest error.
	if c.Task != "" {
		return ErrTurnProcess
	}
	if process != "" {
		// The UI offers the picker to recurrent cards alone; the service
		// holds the same line for the callers that skip the UI (MCP, a
		// plain PATCH) — clearing stays free, whatever the stage is now.
		if c.Stage != board.StageRecurrent {
			return ErrNotRecurrent
		}
		found := false
		for _, p := range b.Processes {
			if p.Name == process {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w %q", ErrProcessNotFound, process)
		}
		// The tie is a stored reference, and references never cross a
		// domain boundary (git-backend.md): a card of the closed repository
		// naming a process declared in the shared one would hand the closed
		// card's existence to readers who may not have it.
		if board.ProcessDomain(b, process) != c.Domain {
			return fmt.Errorf("%w: the process %q lives in another repository", ErrCrossDomain, process)
		}
	}
	if c.Process == process {
		return nil
	}
	if err := s.backend.SetProcess(ctx, b, c, process); err != nil {
		return err
	}
	s.logEvent(ctx, b, c, board.EventProcess, c.Process, process)
	return nil
}

// renameMirror rewrites the card's mirror entry (fromProject, fromEpic) to
// the new pair — the rename flows call it so a renamed column does not
// strand the mirrors that point at it (issue #124's lesson).
func (s *Service) renameMirror(ctx context.Context, b board.Board, c board.Card, fromProject, fromEpic, toProject, toEpic string) error {
	next := make([]board.Placement, len(c.Mirrors))
	for i, m := range c.Mirrors {
		if m.Project == fromProject && m.Epic == fromEpic {
			m = board.Placement{Project: toProject, Epic: toEpic}
		} else if fromEpic == "" && m.Project == fromProject {
			// A project rename: every mirror under it follows, whatever
			// column it names.
			m.Project = toProject
		}
		next[i] = m
	}
	if err := s.backend.SetMirrors(ctx, b, c, next); err != nil {
		return err
	}
	// A COLUMN rename or move logs on the home side (EventEpic), so the
	// mirror entry's rewrite gets the same trace — an unlogged rewrite
	// read as a mirror appearing from nowhere. A PROJECT rename is roster
	// metadata: the home side of it writes no per-card line, and the
	// mirror side stays symmetric — quiet.
	if fromEpic != "" {
		s.logEvent(ctx, b, c, board.EventMirror,
			fromProject+" / "+fromEpic, toProject+" / "+toEpic)
	}
	return nil
}
