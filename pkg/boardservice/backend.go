// Package boardservice is the single entry point for aeman's board actions. It
// ports the action logic that lives in the web frontend's board components
// (TeamBoard.tsx, MeBoard.tsx) onto the pure domain in internal/board, applying
// each change through a Backend. The HTTP API and the MCP server call this
// service in Phase 3 instead of re-implementing the handlers.
package boardservice

import (
	"context"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// Backend abstracts the storage backend a board lives in (GitHub Projects v2
// today, a git backend later). It mirrors the frontend Provider in
// web/src/providers/types.ts, expressed in the domain types from internal/board.
// *ghprojects.Client satisfies it structurally. Setter zero values clear the
// field (an empty ZoneKey/StageKey/PlanBand/date), matching the frontend's
// `null` argument; team = "" is the no-team group.
type Backend interface {
	// LoadBoard fetches a project and returns the parsed domain snapshot (cards +
	// fields, with the hidden per-team sprint-state cards split into SprintStates).
	LoadBoard(ctx context.Context, boardID string) (board.Board, error)

	// LoadCards fetches specific cards by item id (a fast partial read the
	// live-update path uses instead of reloading the whole board). Deleted ids are
	// omitted from the result.
	LoadCards(ctx context.Context, b board.Board, ids []string) ([]board.Card, error)

	// CreateCard creates a card on the board and returns it.
	CreateCard(ctx context.Context, b board.Board, in board.CreateInput) (board.Card, error)
	// DeleteCard removes a card from the board.
	DeleteCard(ctx context.Context, b board.Board, card board.Card) error
	// MoveCard reorders card to sit after afterID ("" = move to the top).
	MoveCard(ctx context.Context, b board.Board, card board.Card, afterID string) error
	// AddNote appends a work note to a card.
	AddNote(ctx context.Context, b board.Board, card board.Card, text string) error
	// AppendEvent records one activity event on a card (see board.Event). The
	// service calls it best-effort: an error must not fail the mutation.
	AppendEvent(ctx context.Context, b board.Board, card board.Card, e board.Event) error
	// CardLog is the card's recorded history, oldest first, with the time it
	// is cut at when the backend holds only part of it (a shallow clone);
	// zero when it is whole.
	CardLog(ctx context.Context, b board.Board, id string) (events []board.Event, truncatedBefore time.Time, err error)
	// EditNote rewrites one of a card's work notes.
	EditNote(ctx context.Context, b board.Board, card board.Card, note board.Note, text string) error
	// DeleteNote removes one of a card's work notes.
	DeleteNote(ctx context.Context, b board.Board, card board.Card, note board.Note) error
	// SetDescription replaces a card's free-form description.
	SetDescription(ctx context.Context, b board.Board, card board.Card, description string) error
	// RenameCard changes a card's title.
	RenameCard(ctx context.Context, b board.Board, card board.Card, title string) error

	// Field setters mirroring the Provider. A zero value clears the field.
	SetStage(ctx context.Context, b board.Board, card board.Card, stage board.StageKey) error
	SetProgress(ctx context.Context, b board.Board, card board.Card, progress int) error
	SetZone(ctx context.Context, b board.Board, card board.Card, zone board.ZoneKey) error
	SetDay(ctx context.Context, b board.Board, card board.Card, day string) error
	// SetLeftAt writes (or clears, with "") the board day a personal card
	// was left behind on by the ×.
	SetLeftAt(ctx context.Context, b board.Board, card board.Card, day string) error
	// CardLogSince is CardLog cut at a boundary: the card's events at or
	// after it. The day feed asks about one day over many cards and has no
	// use for their whole histories.
	CardLogSince(ctx context.Context, b board.Board, id string, since time.Time) ([]board.Event, time.Time, error)
	SetStart(ctx context.Context, b board.Board, card board.Card, date string) error
	SetSprintStart(ctx context.Context, b board.Board, card board.Card, date string) error
	SetPlan(ctx context.Context, b board.Board, card board.Card, plan board.PlanBand) error
	SetWeek(ctx context.Context, b board.Board, card board.Card, week string) error
	// SetLane sets or clears the stored lane (docs/design/backlog.md).
	SetLane(ctx context.Context, b board.Board, card board.Card, lane board.Lane) error
	SetTeam(ctx context.Context, b board.Board, card board.Card, team string) error
	// SetEpic files the card under a Project-board column ("" clears).
	SetEpic(ctx context.Context, b board.Board, card board.Card, epic string) error
	// SetMirrors replaces the card's mirror placements — the additional
	// Project-board columns the card stands in (board/mirrors.go).
	SetMirrors(ctx context.Context, b board.Board, card board.Card, mirrors []board.Placement) error
	// SetProcess writes the Process field of a process state card.
	SetProcess(ctx context.Context, b board.Board, card board.Card, process string) error
	// SetTask links an iteration to the task it came from.
	SetTask(ctx context.Context, b board.Board, card board.Card, task string) error
	// SetPaused stops (or restarts) a process spawning iterations.
	SetPaused(ctx context.Context, b board.Board, card board.Card, paused bool) error
	// SetAccumulate sets a task's accumulate flag.
	SetAccumulate(ctx context.Context, b board.Board, card board.Card, on bool) error
	// SetProject writes the Project field of a hidden state card: a
	// project-state card's own name, or the project an epic belongs to.
	SetProject(ctx context.Context, b board.Board, card board.Card, project string) error
	// SetRecurrence sets a recurrent card's reseed cycle ("" = every sprint).
	SetRecurrence(ctx context.Context, b board.Board, card board.Card, cycle string) error
	SetAssignee(ctx context.Context, b board.Board, card board.Card, login string) error
	// SetParent sets or clears ("") the parent link making a card a subtask.
	SetParent(ctx context.Context, b board.Board, card board.Card, parent string) error
	SetReviewOf(ctx context.Context, b board.Board, card board.Card, reviewOf string) error
	SetReviewRound(ctx context.Context, b board.Board, card board.Card, round int) error

	// SetSprintState creates or updates a team's hidden sprint-state card.
	// current/previous are the team's current and previous sprint start dates
	// ("" clears them); team = "" is the no-team group.
	SetSprintState(ctx context.Context, b board.Board, team, current, previous string) error
}
