package boardservice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aenix-org/aeman/internal/board"
)

// ErrCardNotFound is returned when an item id is not on the loaded board.
var ErrCardNotFound = errors.New("card not found")

// ErrNoteNotFound is returned when a note id is not on the loaded card.
var ErrNoteNotFound = errors.New("note not found")

// Service performs aeman's board actions. It is stateless: every method loads
// the board through the backend, computes the change with internal/board logic,
// then applies it through the backend setters.
type Service struct {
	backend Backend
}

// New builds a Service over a backend.
func New(backend Backend) *Service {
	return &Service{backend: backend}
}

// findCard returns the card with the given item id, or ok = false.
func findCard(b board.Board, itemID string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ItemID == itemID {
			return c, true
		}
	}
	return board.Card{}, false
}

// findReviewCard returns the review card linked to an original (its ReviewOf is
// the original's item id), or ok = false.
func findReviewCard(b board.Board, originalItemID string) (board.Card, bool) {
	for _, c := range b.Cards {
		if c.ReviewOf == originalItemID {
			return c, true
		}
	}
	return board.Card{}, false
}

// loadCard loads the board and resolves a card by item id.
func (s *Service) loadCard(ctx context.Context, owner string, project int, itemID string) (board.Board, board.Card, error) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return board.Board{}, board.Card{}, err
	}
	card, ok := findCard(b, itemID)
	if !ok {
		return b, board.Card{}, fmt.Errorf("%w: %s", ErrCardNotFound, itemID)
	}
	return b, card, nil
}

// --- Snapshot --------------------------------------------------------------

// Board returns the full board snapshot: identity, fields, the visible cards and
// the per-team sprint pointers. The API/MCP board endpoint builds its meta
// response (fields + sprint states) from it.
func (s *Service) Board(ctx context.Context, owner string, project int) (board.Board, error) {
	return s.backend.LoadBoard(ctx, owner, project)
}

// Card loads the board and returns a single card by item id (ErrCardNotFound
// when absent). The API/MCP layer uses it to return the card resulting from an
// action, mirroring the way the UI re-renders the card it just changed.
func (s *Service) Card(ctx context.Context, owner string, project int, itemID string) (board.Card, error) {
	_, card, err := s.loadCard(ctx, owner, project, itemID)
	return card, err
}

// --- Views -----------------------------------------------------------------

// TeamView returns the Team board's grid cards for a team on a day (day = "" is
// today). It mirrors filteredCards/passesFilter in TeamBoard.tsx via board.TeamGrid.
func (s *Service) TeamView(ctx context.Context, owner string, project int, team, day string) ([]board.Card, error) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return nil, err
	}
	if day == "" {
		day = board.TodayIso()
	}
	return board.TeamGrid(b, team, day), nil
}

// MeView returns the personal day board for a user on a day (user = "" is
// everyone, day = "" is today). It mirrors myCards in MeBoard.tsx via board.MeView.
func (s *Service) MeView(ctx context.Context, owner string, project int, user, day string) ([]board.Card, error) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return nil, err
	}
	if day == "" {
		day = board.TodayIso()
	}
	return board.MeView(b, user, day), nil
}

// WeeklyPlan returns a team's weekly-plan cards for a week (week = "" is the
// Monday of the current week), split into the Wed/Fri bands. It mirrors the
// `weekly` memo in TeamBoard.tsx via board.WeeklyPlan.
func (s *Service) WeeklyPlan(ctx context.Context, owner string, project int, team, week string) (board.WeeklyBands, error) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return board.WeeklyBands{}, err
	}
	if week == "" {
		week = board.MondayOf(board.TodayIso())
	}
	return board.WeeklyPlan(b, team, week), nil
}

// --- Create / sprints ------------------------------------------------------

// CreateCardArgs are the inputs to CreateCard. Day defaults to today.
// StartNewSprint forces the team's sprint pointer: nil = auto (record the day as
// the team's first sprint only when it has none), true = always (re)start the
// pointer on the day, false = same as auto (start one only when there is none).
type CreateCardArgs struct {
	Team     string
	Zone     board.ZoneKey
	Title    string
	Assignee string
	Day      string
	// Start and SprintStart override the scheduled day and the sprint the card
	// joins (defaults: the day, and the team's current sprint).
	Start       string
	SprintStart string
	// Plan/Week create a weekly-plan card instead of a day card: no dates are
	// set and no sprint is joined or started (Week defaults to this Monday).
	Plan board.PlanBand
	Week string
	// ReviewOf marks the new card as the review of the given item.
	ReviewOf       string
	StartNewSprint *bool
}

// CreateCard creates a card with two dates: StartDate (its scheduled day) is the
// requested day (today by default), and SprintStart (the sprint it joins) is the
// team's current sprint — so a card created on a later day of the sprint keeps the
// sprint's start. A team with no sprint yet records its first one off this card on
// its sprint-state card, anchoring the Me view; force-new (re)starts the pointer on
// the day and the card joins that fresh sprint. It mirrors handleCreate in
// TeamBoard.tsx / MeBoard.tsx.
func (s *Service) CreateCard(ctx context.Context, owner string, project int, args CreateCardArgs) (board.Card, error) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return board.Card{}, err
	}
	// A weekly-plan card lives in the plan bands, not on the day boards: it gets
	// no dates and joins no sprint. It mirrors handleCreatePlan in TeamBoard.tsx.
	if args.Plan != board.PlanNone {
		week := args.Week
		if week == "" {
			week = board.MondayOf(board.TodayIso())
		}
		return s.backend.CreateCard(ctx, b, board.CreateInput{
			Title:    args.Title,
			Zone:     args.Zone,
			Plan:     args.Plan,
			Week:     week,
			Assignee: args.Assignee,
			Team:     args.Team,
			ReviewOf: args.ReviewOf,
		})
	}
	day := args.Day
	if day == "" {
		day = board.TodayIso()
	}
	start := args.Start
	if start == "" {
		start = day
	}
	cur := board.CurrentSprint(b, args.Team)
	startNew := cur == ""
	if args.StartNewSprint != nil {
		startNew = *args.StartNewSprint || cur == ""
	}
	sprint := args.SprintStart
	if startNew {
		// Record the new sprint and have the card join it (previous = the old
		// current, which is "" when the team had no sprint yet — matching the
		// frontend's setSprintState(team, day, null)).
		if sprint == "" {
			sprint = day
		}
		if err := s.backend.SetSprintState(ctx, b, args.Team, sprint, cur); err != nil {
			return board.Card{}, err
		}
	} else if sprint == "" {
		sprint = cur
	}
	// Start is the scheduled day; SprintStart is the sprint the card belongs to.
	return s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:       args.Title,
		Zone:        args.Zone,
		Day:         day,
		Start:       start,
		SprintStart: sprint,
		Assignee:    args.Assignee,
		Team:        args.Team,
		ReviewOf:    args.ReviewOf,
	})
}

// CarryOver advances a team's sprint to today (the old current becomes previous)
// and pulls its unfinished cards forward. It mirrors startSprint in TeamBoard.tsx:
// idempotent (a no-op when the sprint is already today's), and it always advances
// even with nothing to carry. team = "" is the no-team group.
func (s *Service) CarryOver(ctx context.Context, owner string, project int, team string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	old := board.CurrentSprint(b, team)
	today := board.TodayIso()
	if old == today {
		// Already on today's sprint: re-advancing would overwrite previous.
		return nil
	}
	if err := s.backend.SetSprintState(ctx, b, team, today, old); err != nil {
		return err
	}
	// Carry every unfinished card whose sprint is before the new day — not just
	// the previous sprint — so cards added on an in-between day (or made directly
	// on the backend) are picked up too. Future-dated cards stay.
	var carry []board.Card
	for _, c := range b.Cards {
		if c.Team != team || c.SprintStart == "" || c.SprintStart >= today || c.Stage == board.StageDone {
			continue
		}
		carry = append(carry, c)
	}
	// The per-card writes are independent, but a burst of concurrent Projects v2
	// mutations trips GitHub's secondary rate limit, so run them at a modest
	// concurrency, retry transient failures, and — crucially — do NOT stop at the
	// first error: a partial carry-over that silently drops a card is worse than
	// a slow one. Only cards that still fail after retries surface as an error.
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed int
	var firstErr error
	for _, c := range carry {
		wg.Add(1)
		sem <- struct{}{}
		go func(c board.Card) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.setSprintStartRetry(ctx, b, c, today); err != nil {
				mu.Lock()
				failed++
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(c)
	}
	wg.Wait()
	if firstErr != nil {
		return fmt.Errorf("carried over %d of %d cards, %d failed: %w",
			len(carry)-failed, len(carry), failed, firstErr)
	}
	return nil
}

// setSprintStartRetry sets a card's Sprint Start, retrying a few times with
// backoff. Carry Over touches many cards at once, and a transient GitHub hiccup
// (secondary rate limit, 502, a token refreshed mid-flight) on one of them must
// not silently leave it behind. The write is idempotent, so retrying is safe.
func (s *Service) setSprintStartRetry(ctx context.Context, b board.Board, c board.Card, date string) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		if err = s.backend.SetSprintStart(ctx, b, c, date); err == nil {
			return nil
		}
	}
	return err
}

// CarryWeek pulls a team's unfinished plan cards from earlier weeks into the
// target week (week = "" is the current week's Monday), returning the cards it
// moved. It mirrors handleCarryWeek in TeamBoard.tsx (nothing to carry yields an
// empty result, not an error). team = "" is the no-team group.
func (s *Service) CarryWeek(ctx context.Context, owner string, project int, team, week string) ([]board.Card, error) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return nil, err
	}
	if week == "" {
		week = board.MondayOf(board.TodayIso())
	}
	var carried []board.Card
	for _, c := range b.Cards {
		if c.Plan == board.PlanNone || c.Week == "" || c.Week >= week {
			continue
		}
		if c.Stage == board.StageDone || c.Team != team {
			continue
		}
		if err := s.backend.SetWeek(ctx, b, c, week); err != nil {
			return carried, err
		}
		c.Week = week
		carried = append(carried, c)
	}
	return carried, nil
}

// --- Stage / progress ------------------------------------------------------

// SetStage moves a card to a stage (stage = "" clears it). It mirrors handleStage
// in TeamBoard.tsx: board.ApplyStage computes the resulting (stage, progress) and
// both are persisted (done fills 100%, review/locked knock a full card to 90%).
func (s *Service) SetStage(ctx context.Context, owner string, project int, itemID string, stage board.StageKey) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.applyStage(ctx, b, card, stage)
}

// applyStage persists a stage change and any coupled progress change.
func (s *Service) applyStage(ctx context.Context, b board.Board, card board.Card, stage board.StageKey) error {
	newStage, newProgress := board.ApplyStage(stage, card.Progress)
	if err := s.backend.SetStage(ctx, b, card, newStage); err != nil {
		return err
	}
	if newProgress != card.Progress {
		if err := s.backend.SetProgress(ctx, b, card, newProgress); err != nil {
			return err
		}
	}
	return nil
}

// SetProgress sets a card's progress. It mirrors handleProgress in TeamBoard.tsx:
// board.ApplyProgress clamps the value and runs the done auto-link, both are
// persisted, and a review card's progress drives its original's review stage.
func (s *Service) SetProgress(ctx context.Context, owner string, project int, itemID string, raw int) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	newStage, newProgress := board.ApplyProgress(card.Stage, raw)
	if err := s.backend.SetProgress(ctx, b, card, newProgress); err != nil {
		return err
	}
	if newStage != card.Stage {
		if err := s.backend.SetStage(ctx, b, card, newStage); err != nil {
			return err
		}
	}
	return s.syncReviewLink(ctx, b, card, newProgress)
}

// SetInProgress moves a card to the implicit "In Progress" status (no stage,
// progress nudged into [10, 90]). It mirrors handleInProgress in TeamBoard.tsx
// via board.ApplyInProgress, keeping a review card's review-link in sync.
func (s *Service) SetInProgress(ctx context.Context, owner string, project int, itemID string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	newStage, newProgress := board.ApplyInProgress(card.Stage, card.Progress)
	if err := s.backend.SetStage(ctx, b, card, newStage); err != nil {
		return err
	}
	if newProgress == card.Progress {
		return nil
	}
	if err := s.backend.SetProgress(ctx, b, card, newProgress); err != nil {
		return err
	}
	return s.syncReviewLink(ctx, b, card, newProgress)
}

// SetZone sets a card's colour zone (zone = "" clears it).
func (s *Service) SetZone(ctx context.Context, owner string, project int, itemID string, zone board.ZoneKey) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetZone(ctx, b, card, zone)
}

// SetDay sets a card's scheduled day (day = "" clears it).
func (s *Service) SetDay(ctx context.Context, owner string, project int, itemID, day string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetDay(ctx, b, card, day)
}

// SetStart sets a card's start date (date = "" clears it).
func (s *Service) SetStart(ctx context.Context, owner string, project int, itemID, date string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetStart(ctx, b, card, date)
}

// SetSprintStart sets the start day of the sprint a card belongs to (date = ""
// clears it).
func (s *Service) SetSprintStart(ctx context.Context, owner string, project int, itemID, date string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetSprintStart(ctx, b, card, date)
}

// SetPlan sets a card's weekly-plan band (plan = "" clears it).
func (s *Service) SetPlan(ctx context.Context, owner string, project int, itemID string, plan board.PlanBand) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetPlan(ctx, b, card, plan)
}

// SetWeek sets a card's plan week, a Monday (week = "" clears it).
func (s *Service) SetWeek(ctx context.Context, owner string, project int, itemID, week string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetWeek(ctx, b, card, week)
}

// SetSprintState sets a team's sprint pointer directly (current/previous sprint
// start dates; "" clears them). team = "" is the no-team group. It backs the
// frontend's client-side Carry Over, which advances the pointer then re-dates the
// unfinished cards.
func (s *Service) SetSprintState(ctx context.Context, owner string, project int, team, current, previous string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	return s.backend.SetSprintState(ctx, b, team, current, previous)
}

// syncReviewLink, when card is a review card, drives its original's review stage
// from the review card's progress. It mirrors syncOriginalReview.
func (s *Service) syncReviewLink(ctx context.Context, b board.Board, card board.Card, reviewProgress int) error {
	if card.ReviewOf == "" {
		return nil
	}
	original, ok := findCard(b, card.ReviewOf)
	if !ok {
		return nil
	}
	var next board.StageKey
	switch {
	case reviewProgress == 100 && original.Stage == board.StageReview:
		next = board.StageNone
	case reviewProgress < 100 && original.Stage != board.StageReview:
		next = board.StageReview
	default:
		return nil
	}
	return s.backend.SetStage(ctx, b, original, next)
}

// --- Review linkage --------------------------------------------------------

// SendToReview creates a linked review card for a reviewer (in the original's
// zone/team) and puts the original on the review stage, returning the review
// card. day = "" is today. It mirrors handleSendToReview in TeamBoard.tsx.
func (s *Service) SendToReview(ctx context.Context, owner string, project int, itemID, reviewer, day string) (board.Card, error) {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return board.Card{}, err
	}
	return s.sendToReview(ctx, b, card, reviewer, day)
}

// sendToReview is the create-review-card half of SendToReview over a loaded board.
func (s *Service) sendToReview(ctx context.Context, b board.Board, card board.Card, reviewer, day string) (board.Card, error) {
	if day == "" {
		day = board.TodayIso()
	}
	zone := card.Zone
	if zone == "" {
		zone = board.ZoneGray
	}
	sprintStart := board.CurrentSprint(b, card.Team)
	if sprintStart == "" {
		sprintStart = day
	}
	created, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:       "review: " + card.Title,
		Zone:        zone,
		Day:         day,
		Start:       day,
		SprintStart: sprintStart,
		Assignee:    reviewer,
		Team:        card.Team,
		ReviewOf:    card.ItemID,
	})
	if err != nil {
		return board.Card{}, err
	}
	// Put the original on review (ApplyStage also drops a 100% card to 90%).
	if err := s.applyStage(ctx, b, card, board.StageReview); err != nil {
		return created, err
	}
	return created, nil
}

// ReassignReviewer points a card's linked review card at another reviewer, or
// sends the card to review when it has none yet (day = "" is today). It mirrors
// handleSetReviewAssignee (non-null login) in TeamBoard.tsx.
func (s *Service) ReassignReviewer(ctx context.Context, owner string, project int, itemID, reviewer, day string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	reviewCard, ok := findReviewCard(b, card.ItemID)
	if !ok {
		_, err := s.sendToReview(ctx, b, card, reviewer, day)
		return err
	}
	return s.backend.SetAssignee(ctx, b, reviewCard, reviewer)
}

// RemoveReviewer deletes a card's linked review card (a no-op when there is
// none). It mirrors handleSetReviewAssignee(null) in TeamBoard.tsx.
func (s *Service) RemoveReviewer(ctx context.Context, owner string, project int, itemID string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	reviewCard, ok := findReviewCard(b, itemID)
	if !ok {
		return nil
	}
	return s.backend.DeleteCard(ctx, b, reviewCard)
}

// --- Card field actions ----------------------------------------------------

// SetAssignee replaces a card's assignee (login = "" unassigns). It mirrors
// handleSetAssignee in TeamBoard.tsx.
func (s *Service) SetAssignee(ctx context.Context, owner string, project int, itemID, login string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetAssignee(ctx, b, card, login)
}

// SetTeam moves a card to a team and joins that team's current sprint (team = ""
// is the no-team group, day = "" is today). It mirrors handleSetTeam in
// TeamBoard.tsx.
func (s *Service) SetTeam(ctx context.Context, owner string, project int, itemID, team, day string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	if day == "" {
		day = board.TodayIso()
	}
	sprintStart := board.CurrentSprint(b, team)
	if sprintStart == "" {
		sprintStart = day
	}
	if err := s.backend.SetTeam(ctx, b, card, team); err != nil {
		return err
	}
	if sprintStart != card.SprintStart {
		return s.backend.SetSprintStart(ctx, b, card, sprintStart)
	}
	return nil
}

// Rename changes a card's title. It mirrors handleRename in TeamBoard.tsx.
func (s *Service) Rename(ctx context.Context, owner string, project int, itemID, title string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.RenameCard(ctx, b, card, title)
}

// SetDescription replaces a card's free-form description (the body text above
// the note log).
func (s *Service) SetDescription(ctx context.Context, owner string, project int, itemID, description string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetDescription(ctx, b, card, description)
}

// findNote resolves a note on a card by its id.
func findNote(card board.Card, noteID string) (board.Note, bool) {
	for _, n := range card.Notes {
		if n.ID == noteID {
			return n, true
		}
	}
	return board.Note{}, false
}

// EditNote rewrites one of a card's work notes (ErrNoteNotFound when the note id
// is not on the card).
func (s *Service) EditNote(ctx context.Context, owner string, project int, itemID, noteID, text string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	note, ok := findNote(card, noteID)
	if !ok {
		return fmt.Errorf("%w: %s on %s", ErrNoteNotFound, noteID, itemID)
	}
	return s.backend.EditNote(ctx, b, card, note, text)
}

// DeleteNote removes one of a card's work notes (ErrNoteNotFound when the note
// id is not on the card).
func (s *Service) DeleteNote(ctx context.Context, owner string, project int, itemID, noteID string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	note, ok := findNote(card, noteID)
	if !ok {
		return fmt.Errorf("%w: %s on %s", ErrNoteNotFound, noteID, itemID)
	}
	return s.backend.DeleteNote(ctx, b, card, note)
}

// AddNote appends a work note to a card.
func (s *Service) AddNote(ctx context.Context, owner string, project int, itemID, text string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.AddNote(ctx, b, card, text)
}

// MoveCard reorders a card to sit after afterID ("" = top of the board). It
// mirrors the moveCard calls behind drag-and-drop in TeamBoard.tsx.
func (s *Service) MoveCard(ctx context.Context, owner string, project int, itemID, afterID string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.MoveCard(ctx, b, card, afterID)
}

// DeleteCard deletes a card, cascading to its linked review card. It mirrors
// handleDelete in TeamBoard.tsx (deleting a reviewed card removes both).
func (s *Service) DeleteCard(ctx context.Context, owner string, project int, itemID string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.deleteWithCascade(ctx, b, card)
}

// deleteWithCascade deletes a card and any review card linked to it.
func (s *Service) deleteWithCascade(ctx context.Context, b board.Board, card board.Card) error {
	if reviewCard, ok := findReviewCard(b, card.ItemID); ok {
		if err := s.backend.DeleteCard(ctx, b, reviewCard); err != nil {
			return err
		}
	}
	return s.backend.DeleteCard(ctx, b, card)
}

// --- Weekly plan -----------------------------------------------------------

// TakeIntoPlan takes a weekly-plan card into work: it assigns the card to an
// engineer (engineer = "" unassigns), sets its zone (zone = "" keeps the card's
// own zone, defaulting to gray) and joins the card's team's current sprint
// (day = "" is today). It mirrors takePlanCard in TeamBoard.tsx.
func (s *Service) TakeIntoPlan(ctx context.Context, owner string, project int, itemID, engineer string, zone board.ZoneKey, day string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	if day == "" {
		day = board.TodayIso()
	}
	if zone == "" {
		zone = card.Zone
		if zone == "" {
			zone = board.ZoneGray
		}
	}
	sprintStart := board.CurrentSprint(b, card.Team)
	if sprintStart == "" {
		sprintStart = day
	}
	if err := s.backend.SetAssignee(ctx, b, card, engineer); err != nil {
		return err
	}
	if zone != card.Zone {
		if err := s.backend.SetZone(ctx, b, card, zone); err != nil {
			return err
		}
	}
	return s.backend.SetSprintStart(ctx, b, card, sprintStart)
}

// ReleaseFromPlan removes a card from the weekly plan. It mirrors removeFromPlan
// in TeamBoard.tsx: a taken-into-work card (it has an assignee) just loses its
// Plan + Week markers; a pure plan card is demoted to its previous plan week, or
// deleted (cascading to a linked review card) when there is none.
func (s *Service) ReleaseFromPlan(ctx context.Context, owner string, project int, itemID string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	if len(card.Assignees) == 0 {
		if prevWeek := previousWeekFor(b, card); prevWeek != "" {
			return s.backend.SetWeek(ctx, b, card, prevWeek)
		}
		return s.deleteWithCascade(ctx, b, card)
	}
	if err := s.backend.SetPlan(ctx, b, card, board.PlanNone); err != nil {
		return err
	}
	return s.backend.SetWeek(ctx, b, card, "")
}

// previousWeekFor returns the latest plan week before a card's week among the
// same team's cards, or "". It mirrors previousWeekFor in TeamBoard.tsx.
func previousWeekFor(b board.Board, card board.Card) string {
	if card.Week == "" {
		return ""
	}
	prev := ""
	for _, c := range b.Cards {
		if c.ItemID == card.ItemID || c.Team != card.Team {
			continue
		}
		if c.Week == "" || c.Week >= card.Week {
			continue
		}
		if prev == "" || c.Week > prev {
			prev = c.Week
		}
	}
	return prev
}
