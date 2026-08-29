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

// Mirror adds the column (project, epic) to the card. The card must already
// have a home column — a card outside every project is attached, not
// mirrored — the target must exist, in the same repository as the home, and
// mirroring where the card already stands is a no-op, not a duplicate.
func (s *Service) Mirror(ctx context.Context, boardID string, itemID, project, epic string) error {
	b, c, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if c.Epic == "" {
		return fmt.Errorf("a card outside every project cannot be mirrored — attach it to one first")
	}
	if c.Project == project && c.Epic == epic {
		return fmt.Errorf("%q / %q is the card's own column", project, epic)
	}
	if _, ok := board.FindEpic(b, project, epic); !ok {
		return fmt.Errorf("%w %q in project %q", ErrEpicNotFound, epic, project)
	}
	if !board.MirrorAllowed(b, c.Project, project) {
		return fmt.Errorf("%w: %q is not in %q's repository", ErrCrossDomain, project, c.Project)
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
	if process != "" {
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
	return s.backend.SetMirrors(ctx, b, c, next)
}
