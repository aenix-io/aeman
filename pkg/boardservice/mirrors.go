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

// ErrSubtaskMirror is mirroring a subtask: it carries the ONE column of
// its own that G14 allows (and G57 draws), but its file rides its parent —
// a second placement would be stranded the moment the parent changes
// repository, naming a repository that no longer holds the card.
var ErrSubtaskMirror = errors.New("a subtask rides its parent and cannot be mirrored")

// Mirror adds the column (project, epic) to the card. The card must already
// stand in a column — one outside every column is attached, not mirrored —
// and the target must exist in the repository that holds the CARD'S FILE
// (DomainOf: linked cards first, G14), which is not always its home
// column's: that column is checked separately, so a card whose own
// placement is already wrong is told to fix that rather than adding a
// second. A column of no project is a home like any other. Mirroring where
// the card already stands is a no-op, not a duplicate.
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
	// One question, one answer: may this card stand in this column? The
	// COLUMN says which repository it belongs to (its own, never its
	// project's — a project name may be declared in two repositories with
	// its columns merged, G13), and the card's FILE says which repository
	// holds it (linked cards first, G14: a review card lives with its
	// original whatever its column says).
	mine := board.HomeDomain(b, c)
	if cd, ok := board.ColumnDomain(b, project, epic); !ok || cd != mine {
		return fmt.Errorf("%w: the column %q is not in the repository that holds this card",
			ErrCrossDomain, epic)
	}
	if hd, ok := board.ColumnDomain(b, c.Project, c.Epic); ok && hd != mine {
		return fmt.Errorf("%w: the card's own column is not in the repository that holds it — fix that first",
			ErrCrossDomain)
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
//   - A SUBTASK is never deleted here, however untouched: its other home
//     is its parent, so the × only takes the column away (G57).
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
	// The last column. A SUBTASK has another home — its parent — so the ×
	// only takes the column away, however untouched the card is: deleting
	// would destroy work that still rides the parent on every other board
	// (the two-homes rule the × exists to honour).
	worked := len(c.Assignees) > 0 && c.Progress > 0
	if !worked && c.Parent == "" {
		// The whole card goes — a deletion moves nothing, so the tie
		// guard has no say here (delete_card removes tied cards the same
		// way) — and clearing its plan first would be a dead write into
		// the very commit that removes the file.
		return s.deleteWithCascade(ctx, b, c)
	}
	// ORPHANING clears the pair, and for a teamless card that is a
	// repository move (the domain falls through to the primary) — refused
	// while a tie stands, before anything is written, like every other
	// door.
	if err := refileGuard(b, c, func(a *board.Card) { a.Project = ""; a.Epic = "" }); err != nil {
		return err
	}
	// The week goes with it, always: it was the slot's row, not a placement
	// anyone made for the card itself.
	if c.Week != "" {
		if err := s.backend.SetWeek(ctx, b, c, ""); err != nil {
			return err
		}
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

// refileGuard refuses a re-file that would leave a reference pointing across
// a domain boundary: the COLUMNS OF THE CARDS THAT FOLLOW IT. A subtask's
// file follows its parent and a review card's follows its original
// (MultiBackend cascades the re-file along both links), while each one's
// own column stays behind, naming a repository that no longer holds it —
// the very state SetEpic refuses when the card itself is moved.
//
// Explicit re-files refuse; grouping clears the mirrors instead (SetParent).
func refileGuard(b board.Board, c board.Card, change func(*board.Card)) error {
	return refileGuardOpts(b, c, change, true)
}

// refileGuardOpts is refileGuard with a say over the card's OWN column.
// The grid's × repairs that column itself — ungrouping strands it, and
// refusing would name a column the person did not touch — so it asks
// about everything ELSE first: what a refusal must precede is a write,
// and the repair is one.
func refileGuardOpts(b board.Board, c board.Card, change func(*board.Card), ownColumn bool) error {
	after := c
	change(&after)
	to := board.HomeDomain(b, after)
	// The card's OWN column first, against where the card ends up. This is
	// the one check that matters even when the domain does not change —
	// re-filing a subtask into another column moves no file, and the
	// column still has to name the repository its parent's file lives in.
	if ownColumn && after.Epic != "" {
		if cd, ok := board.ColumnDomain(b, after.Project, after.Epic); ok && cd != to {
			return fmt.Errorf("%w: the column %q is not in the repository that holds this card",
				ErrCrossDomain, after.Epic)
		}
	}
	if to == board.HomeDomain(b, c) {
		return nil
	}
	// Only a card that MOVES can strand what rides it, so the follower
	// scan — the one linear walk here — waits until the move is certain
	// and until the cheaper refusal above has had its say.
	// SetEpicProject calls this once per card of a column.
	followers := board.Followers(b, c.ItemID)
	for _, f := range followers {
		if f.Epic != "" {
			if cd, ok := board.ColumnDomain(b, f.Project, f.Epic); ok && cd != to {
				return fmt.Errorf("%w: %q follows this card and stands in a column of another repository — take it out of the column first",
					ErrCrossDomain, f.Title)
			}
		}
	}
	return nil
}

// columnsAgree reports whether two columns belong to the same repository —
// the question every "may these placements coexist" rule asks. It is the
// COLUMN's answer, never its project's: one project name may be declared
// in two repositories with its columns merged under a single entry (G13),
// and an undeclared column agrees with nothing.
func columnsAgree(b board.Board, aProject, aEpic, bProject, bEpic string) bool {
	ad, aok := board.ColumnDomain(b, aProject, aEpic)
	bd, bok := board.ColumnDomain(b, bProject, bEpic)
	return aok && bok && ad == bd
}
