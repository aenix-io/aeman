package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aenix-org/aeman/pkg/board"
)

// ErrCardNotFound is returned when an item id is not on the loaded board.
var ErrCardNotFound = errors.New("card not found")

// ErrNoteNotFound is returned when a note id is not on the loaded card.
var ErrNoteNotFound = errors.New("note not found")

// ErrInvalidStage is returned when a stage cannot apply to a card — a review
// card cannot be made recurrent (a review is one-off, not a repeating task).
var ErrInvalidStage = errors.New("invalid stage for this card")

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
	// A title that is nothing but a GitHub issue/PR URL turns into that item's
	// real title, with the link moved into the description — a one-time
	// resolution at create, never re-synced. Resolution failures (no access,
	// dead link) keep the URL as the title.
	var linkDescription string
	if ref, ok := board.ParseGitHubRef(strings.TrimSpace(args.Title)); ok {
		if resolver, hasResolver := s.backend.(LinkResolver); hasResolver {
			if resolved, err := resolver.ResolveIssueRef(ctx, ref); err == nil && resolved.Title != "" {
				args.Title = resolved.Title
				linkDescription = ref.URL
			}
		}
	}
	// A weekly-plan card lives in the plan bands, not on the day boards: it gets
	// no dates and joins no sprint. It mirrors handleCreatePlan in TeamBoard.tsx.
	if args.Plan != board.PlanNone {
		week := args.Week
		if week == "" {
			week = board.MondayOf(board.TodayIso())
		}
		card, err := s.backend.CreateCard(ctx, b, board.CreateInput{
			Title:    args.Title,
			Zone:     args.Zone,
			Plan:     args.Plan,
			Week:     week,
			Assignee: args.Assignee,
			Team:     args.Team,
			ReviewOf: args.ReviewOf,
		})
		return s.withLinkDescription(ctx, b, card, err, linkDescription)
	}
	// Start and Day (the end/due date) default to each other so a create with
	// only one of them yields a one-day range — a backdated create must NOT get
	// day = today, or the [start…day] range would stretch it onto today's board.
	day := args.Day
	start := args.Start
	if start == "" {
		if day == "" {
			day = board.TodayIso()
		}
		start = day
	} else if day == "" {
		day = start
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
	card, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:       args.Title,
		Zone:        args.Zone,
		Day:         day,
		Start:       start,
		SprintStart: sprint,
		Assignee:    args.Assignee,
		Team:        args.Team,
		ReviewOf:    args.ReviewOf,
	})
	card, err = s.withLinkDescription(ctx, b, card, err, linkDescription)
	if err == nil {
		s.logEvent(ctx, b, card, board.EventCreated, "", "")
	}
	return card, err
}

// withLinkDescription moves the source URL of a create-by-URL card into its
// description. A failure to write the description is not fatal — the card
// exists with the right title; the link is just not filed.
func (s *Service) withLinkDescription(ctx context.Context, b board.Board, card board.Card, err error, url string) (board.Card, error) {
	if err != nil || url == "" {
		return card, err
	}
	if setErr := s.backend.SetDescription(ctx, b, card, url); setErr == nil {
		card.Description = url
	}
	return card, nil
}

// CarryReport summarizes what a carry pass did — or would do, on a dry run
// (which backs the UI's confirm dialog counts).
type CarryReport struct {
	Carried  int `json:"carried"`
	Reseeded int `json:"reseeded"`
}

// CarryOver advances a team's sprint to today (the old current becomes previous)
// and pulls its unfinished cards forward. It mirrors startSprint in TeamBoard.tsx:
// idempotent (a no-op when the sprint is already today's), and it always advances
// even with nothing to carry. team = "" is the no-team group. With dryRun the
// would-be counts are reported and nothing is written.
func (s *Service) CarryOver(ctx context.Context, owner string, project int, team string, dryRun bool) (CarryReport, error) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return CarryReport{}, err
	}
	old := board.CurrentSprint(b, team)
	today := board.TodayIso()
	if old == today {
		// Already on today's sprint: re-advancing would overwrite previous.
		return CarryReport{}, nil
	}
	// Carry only the unfinished cards of the sprint being closed (sprintStart ==
	// the old current pointer). A card that is NOT on today's sprint — demoted
	// back to an earlier one, or simply old — stays where it is, so removing a
	// card from the current sprint is final and it never boomerangs back. A
	// finished recurrent card stays behind and reseeds a fresh copy instead.
	var carry, reseed, relocate []board.Card
	for _, c := range b.Cards {
		if c.Team != team || old == "" || c.SprintStart != old {
			continue
		}
		if c.Stage == board.StageRecurrent && c.Progress >= 100 {
			reseed = append(reseed, c)
			continue
		}
		if board.Complete(c.Stage, c.Progress) {
			// A completed review card created on a later day of the closing
			// sprint keeps its own start-day (often today), so it would linger on
			// the new sprint's day even though it is not carried. Pull its dates
			// back to the closing sprint so it stays behind; a re-review in the
			// new sprint relocates it forward.
			if c.ReviewOf != "" && (c.StartDate != old || c.Day != old) {
				relocate = append(relocate, c)
			}
			continue
		}
		// A review card crosses sprints only while its review is still on: the
		// original must still sit on the review stage unfinished, and the card
		// must still be the assigned reviewer's. Stale review work (original
		// done, reviewer swapped, link severed) stays behind.
		if c.ReviewOf != "" && !reviewStillRequired(b, c) {
			continue
		}
		carry = append(carry, c)
	}
	rep := CarryReport{Carried: len(carry), Reseeded: len(reseed)}
	if dryRun {
		return rep, nil
	}
	if err := s.backend.SetSprintState(ctx, b, team, today, old); err != nil {
		return CarryReport{}, err
	}
	// Pin completed review cards to the closing sprint's day so they do not
	// linger on the new sprint (best-effort; failures are non-fatal).
	for _, c := range relocate {
		_ = s.backend.SetStart(ctx, b, c, old)
		_ = s.backend.SetDay(ctx, b, c, old)
		_ = s.backend.SetSprintStart(ctx, b, c, old)
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
	// Seed the new sprint with fresh copies of the finished recurrent cards:
	// same title/description/team/zone/assignee at 0%, without the old notes.
	for _, c := range reseed {
		if err := s.reseedRecurrent(ctx, b, c, board.CreateInput{
			Title:       c.Title,
			Zone:        c.Zone,
			Day:         today,
			Start:       today,
			SprintStart: today,
			Team:        c.Team,
		}); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return rep, fmt.Errorf("carried over %d of %d cards, %d failed: %w",
			len(carry)-failed, len(carry), failed, firstErr)
	}
	return rep, nil
}

// reseedRecurrent creates the fresh copy of a finished recurrent card: the given
// create input plus the original's first assignee, the recurrent stage and the
// original's description (notes are deliberately not copied).
func (s *Service) reseedRecurrent(ctx context.Context, b board.Board, c board.Card, in board.CreateInput) error {
	if len(c.Assignees) > 0 {
		in.Assignee = c.Assignees[0]
	}
	created, err := s.backend.CreateCard(ctx, b, in)
	if err != nil {
		return err
	}
	if err := s.backend.SetStage(ctx, b, created, board.StageRecurrent); err != nil {
		return err
	}
	if c.Description != "" {
		return s.backend.SetDescription(ctx, b, created, c.Description)
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
// moved and reseeded. It mirrors handleCarryWeek in TeamBoard.tsx (nothing to
// carry yields an empty report, not an error). team = "" is the no-team group.
// With dryRun the would-be counts are reported and nothing is written.
func (s *Service) CarryWeek(ctx context.Context, owner string, project int, team, week string, dryRun bool) (CarryReport, error) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return CarryReport{}, err
	}
	if week == "" {
		week = board.MondayOf(board.TodayIso())
	}
	// Titles already planned in the target week, for the recurrent reseed dedup:
	// re-running carry-week must not create a second copy. A plan card with an
	// EMPTY week is a torn reseed from an interrupted earlier run (create landed,
	// the week write did not) — track those to finish them instead of duplicating.
	inTarget := map[string]bool{}
	torn := map[string]board.Card{}
	for _, c := range b.Cards {
		if c.Plan == board.PlanNone || c.Team != team {
			continue
		}
		if c.Week == week {
			inTarget[c.Title] = true
		}
		if c.Week == "" {
			torn[c.Title] = c
		}
	}
	var rep CarryReport
	for _, c := range b.Cards {
		if c.Plan == board.PlanNone || c.Week == "" || c.Week >= week || c.Team != team {
			continue
		}
		// A finished recurrent plan card stays in its week and seeds the target
		// week with a fresh copy (unless one with the same title is already there).
		if c.Stage == board.StageRecurrent && c.Progress >= 100 {
			if inTarget[c.Title] {
				continue
			}
			rep.Reseeded++
			if dryRun {
				continue
			}
			// Finish a torn reseed instead of creating another copy.
			if stray, ok := torn[c.Title]; ok {
				if err := s.backend.SetWeek(ctx, b, stray, week); err != nil {
					return rep, err
				}
				delete(torn, c.Title)
				inTarget[c.Title] = true
				continue
			}
			if err := s.reseedRecurrent(ctx, b, c, board.CreateInput{
				Title: c.Title,
				Zone:  c.Zone,
				Plan:  c.Plan,
				Week:  week,
				Team:  c.Team,
			}); err != nil {
				return rep, err
			}
			inTarget[c.Title] = true
			continue
		}
		if board.Complete(c.Stage, c.Progress) {
			continue
		}
		rep.Carried++
		if dryRun {
			continue
		}
		if err := s.backend.SetWeek(ctx, b, c, week); err != nil {
			return rep, err
		}
		// A carried card is already overdue, so it lands in the target week's
		// earlier half: a by-Friday card tightens to by-Wednesday. (Its past
		// weeks keep showing it in the by-Friday band — the week-history rule.)
		if c.Plan == board.PlanFri {
			if err := s.backend.SetPlan(ctx, b, c, board.PlanWed); err != nil {
				return rep, err
			}
		}
	}
	return rep, nil
}

// --- Defer / dates / remove (the frontend's date logic, moved server-side) --

// Defer pushes a card's scheduled day N days ahead of today — or ahead of its
// already-deferred slot, so presses stack. An old card keeps its history (only
// startDate moves); a card created today has none, so it relocates fully:
// sprint and a stale end date move along. It mirrors moveStart in Card.tsx +
// handleDefer in TeamBoard.tsx.
func (s *Service) Defer(ctx context.Context, owner string, project int, itemID string, days int) error {
	b, c, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	today := board.TodayIso()
	base := today
	if c.StartDate != "" && c.StartDate > today {
		base = c.StartDate
	}
	target := board.AddDays(base, days)
	if err := s.backend.SetStart(ctx, b, c, target); err != nil {
		return err
	}
	if board.LocalDateIso(c.CreatedAt) == today {
		if err := s.backend.SetSprintStart(ctx, b, c, target); err != nil {
			return err
		}
		if c.Day != "" && c.Day < target {
			return s.backend.SetDay(ctx, b, c, target)
		}
	}
	return nil
}

// SetDates applies the calendar: an explicit start…end relocation. The card
// joins the sprint that was active on the start day (falling back to the start
// day itself when no tracked sprint covers it); empty values clear the dates.
// It mirrors handleSetDates in TeamBoard.tsx.
func (s *Service) SetDates(ctx context.Context, owner string, project int, itemID, start, end string) error {
	b, c, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	sprint := ""
	if start != "" {
		sprint = board.ActiveSprint(b, c.Team, start)
		if sprint == "" {
			sprint = start
		}
	}
	if err := s.backend.SetStart(ctx, b, c, start); err != nil {
		return err
	}
	if err := s.backend.SetSprintStart(ctx, b, c, sprint); err != nil {
		return err
	}
	return s.backend.SetDay(ctx, b, c, end)
}

// Remove is the smart × — one method, the backend decides the outcome. It
// mirrors handleGridDelete/removeFromPlan in TeamBoard.tsx:
//
//   - from the grid (from = "" or "grid"): a plan-taken card is released back
//     to plan-only (assignee + sprint cleared); a card still in the team's
//     current sprint demotes to the previous one (all dates pulled along);
//     anything else is deleted for real (cascading its review card).
//   - from the plan band (from = "plan"): an assigned card keeps working and
//     only loses the weekly marker; a pure plan card demotes to its team's
//     previous week, or is deleted when there is none.
func (s *Service) Remove(ctx context.Context, owner string, project int, itemID, from string) error {
	b, c, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	if from == "plan" {
		// A card someone is (or was) working on is never deleted by the plan ×:
		// it sheds only its weekly membership and stays with the person and on
		// the sprint days it passed through.
		if len(c.Assignees) > 0 || c.Progress > 0 {
			if err := s.backend.SetPlan(ctx, b, c, board.PlanNone); err != nil {
				return err
			}
			if err := s.backend.SetWeek(ctx, b, c, ""); err != nil {
				return err
			}
			s.logEvent(ctx, b, c, board.EventPlanReleased, string(c.Plan), "")
			return nil
		}
		// A pure plan card — unassigned, never worked — is deleted for real.
		// (Demoting it to an earlier week only made carry-week boomerang it
		// back into the plan.)
		return s.deleteWithCascade(ctx, b, c)
	}
	if c.Plan != board.PlanNone {
		// The grid × on an untouched taken plan card undoes the take: it releases
		// back to the plan pool. A card already worked on (progress > 0) must NOT
		// lose its person or sprint history — it sheds the plan membership
		// instead and stays a regular sprint card.
		if c.Progress > 0 {
			if err := s.backend.SetPlan(ctx, b, c, board.PlanNone); err != nil {
				return err
			}
			if err := s.backend.SetWeek(ctx, b, c, ""); err != nil {
				return err
			}
			s.logEvent(ctx, b, c, board.EventPlanReleased, string(c.Plan), "")
			return nil
		}
		if err := s.backend.SetAssignee(ctx, b, c, ""); err != nil {
			return err
		}
		return s.backend.SetSprintStart(ctx, b, c, "")
	}
	cur := board.CurrentSprint(b, c.Team)
	prev := board.PreviousSprint(b, c.Team)
	if c.SprintStart != "" && cur != "" && c.SprintStart == cur && prev != "" && prev < cur {
		if err := s.backend.SetStart(ctx, b, c, prev); err != nil {
			return err
		}
		if err := s.backend.SetSprintStart(ctx, b, c, prev); err != nil {
			return err
		}
		if c.Day != "" && c.Day != prev {
			return s.backend.SetDay(ctx, b, c, prev)
		}
		return nil
	}
	return s.deleteWithCascade(ctx, b, c)
}

// LinkResolver resolves a GitHub issue/PR link to its live title and state.
// Backends that can talk to GitHub implement it; the service degrades to
// unresolved links otherwise.
type LinkResolver interface {
	ResolveIssueRef(ctx context.Context, link board.Link) (board.Link, error)
}

// CardLinks extracts every URL from a card's description — GitHub issue/PR
// references first, plain links after — and resolves the references to their
// titles when the backend can. A reference that fails to resolve is returned
// as-is rather than dropped.
func (s *Service) CardLinks(ctx context.Context, owner string, project int, itemID string) ([]board.Link, error) {
	_, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return nil, err
	}
	links := board.ExtractLinks(card.Description)
	resolver, ok := s.backend.(LinkResolver)
	if !ok {
		return links, nil
	}
	for i, link := range links {
		if !link.IsGitHubRef() {
			continue
		}
		if resolved, err := resolver.ResolveIssueRef(ctx, link); err == nil {
			links[i] = resolved
		}
	}
	return links, nil
}

// SetReviewOf sets or clears (reviewOf = "") the link marking a card as the
// review of another.
func (s *Service) SetReviewOf(ctx context.Context, owner string, project int, itemID, reviewOf string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	return s.backend.SetReviewOf(ctx, b, card, reviewOf)
}

// cancelLinkedReview demotes or deletes the unfinished review card linked to an
// original that just left the review stage, mirroring the × logic; the demote
// also breaks the reviewOf link, or the original would keep showing "On review".
// A finished review card stays as a record of the work. It mirrors
// cancelLinkedReview in TeamBoard.tsx / MeBoard.tsx.
func (s *Service) cancelLinkedReview(ctx context.Context, b board.Board, original board.Card) error {
	review, ok := findReviewCard(b, original.ItemID)
	if !ok || board.Complete(review.Stage, review.Progress) {
		return nil
	}
	// The reviewer already put work into it (progress > 0): never auto-remove
	// that work. The card stays in place, link intact — the next carry-over
	// decides its fate (it only travels while the review is still required).
	if review.Progress > 0 {
		return nil
	}
	cur := board.CurrentSprint(b, review.Team)
	prev := board.PreviousSprint(b, review.Team)
	if review.SprintStart != "" && cur != "" && review.SprintStart == cur && prev != "" && prev < cur {
		if err := s.backend.SetStart(ctx, b, review, prev); err != nil {
			return err
		}
		if err := s.backend.SetSprintStart(ctx, b, review, prev); err != nil {
			return err
		}
		if err := s.backend.SetReviewOf(ctx, b, review, ""); err != nil {
			return err
		}
		if review.Day != "" && review.Day != prev {
			return s.backend.SetDay(ctx, b, review, prev)
		}
		return nil
	}
	return s.backend.DeleteCard(ctx, b, review)
}

// --- Stage / progress ------------------------------------------------------

// SetStage moves a card to a stage (stage = "" clears it). It mirrors handleStage
// in TeamBoard.tsx: board.ApplyStage computes the resulting (stage, progress) and
// both are persisted (done fills 100%, review/locked knock a full card to 90%).
// Taking a card off review cancels its unfinished linked review card.
func (s *Service) SetStage(ctx context.Context, owner string, project int, itemID string, stage board.StageKey) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	// A review card is auxiliary and one-off: it cannot be made recurrent.
	if stage == board.StageRecurrent && card.ReviewOf != "" {
		return fmt.Errorf("%w: a review card cannot be recurrent", ErrInvalidStage)
	}
	if err := s.applyStage(ctx, b, card, stage); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventStage, string(card.Stage), string(stage))
	// Leaving review cancels the unfinished linked review card.
	if card.Stage == board.StageReview && stage != board.StageReview {
		return s.cancelLinkedReview(ctx, b, card)
	}
	// Entering review re-review: a passed card put back on review reactivates
	// its completed review card for a fresh round (the reverse of the review
	// done → off-review forward sync).
	if stage == board.StageReview && card.Stage != board.StageReview {
		return s.reactivateReviewCard(ctx, b, card)
	}
	return nil
}

// reactivateReviewCard resets a completed linked review card for a new review
// round: its progress back to 0 (no longer done) and the round counter bumped.
// A no-op when the original has no linked review card, or the review is still
// in progress. It is called whenever an already-passed original is put back on
// review — from the stage menu or by re-sending it to the same reviewer.
func (s *Service) reactivateReviewCard(ctx context.Context, b board.Board, original board.Card) error {
	review, ok := findReviewCard(b, original.ItemID)
	if !ok || !board.Complete(review.Stage, review.Progress) {
		return nil
	}
	// Bring the review card into the original's current sprint and onto today,
	// so a re-review in a NEW sprint makes it appear there (it may have been
	// left behind in the closing sprint by carry-over).
	today := board.TodayIso()
	if review.SprintStart != original.SprintStart {
		if err := s.backend.SetSprintStart(ctx, b, review, original.SprintStart); err != nil {
			return err
		}
	}
	if review.StartDate != today {
		if err := s.backend.SetStart(ctx, b, review, today); err != nil {
			return err
		}
	}
	if review.Day != today {
		if err := s.backend.SetDay(ctx, b, review, today); err != nil {
			return err
		}
	}
	if err := s.backend.SetProgress(ctx, b, review, 0); err != nil {
		return err
	}
	round := review.ReviewRound
	if round < 1 {
		round = 1
	}
	return s.backend.SetReviewRound(ctx, b, review, round+1)
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
	s.logEvent(ctx, b, card, board.EventProgress,
		strconv.Itoa(card.Progress), strconv.Itoa(newProgress))
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
	s.logEvent(ctx, b, card, board.EventStage, string(card.Stage), "in-progress")
	// In Progress leaves the review stage too: cancel the linked review card.
	if card.Stage == board.StageReview {
		if err := s.cancelLinkedReview(ctx, b, card); err != nil {
			return err
		}
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
	if err := s.backend.SetZone(ctx, b, card, zone); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventZone, string(card.Zone), string(zone))
	return nil
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
	if err := s.backend.SetStage(ctx, b, original, next); err != nil {
		return err
	}
	reviewer := ""
	if len(card.Assignees) > 0 {
		reviewer = card.Assignees[0]
	}
	if next == board.StageNone {
		s.logEvent(ctx, b, original, board.EventReviewPassed, reviewer, "")
	} else {
		s.logEvent(ctx, b, original, board.EventStage, string(original.Stage), string(board.StageReview))
	}
	return nil
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
	// A review card belongs to the SAME sprint as the card it reviews, not
	// merely the team's current pointer — otherwise a card being reviewed in an
	// older, not-yet-carried sprint would get a review card in a different
	// sprint, and carry-over would split them.
	sprintStart := card.SprintStart
	if sprintStart == "" {
		sprintStart = board.CurrentSprint(b, card.Team)
	}
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
	// The review card carries a copy of the original's description, so the
	// reviewer sees the same context (and its links). A one-time copy at
	// create — the reviewer may edit their own copy freely afterwards.
	if card.Description != "" {
		if setErr := s.backend.SetDescription(ctx, b, created, card.Description); setErr == nil {
			created.Description = card.Description
		}
	}
	// Put the original on review (ApplyStage also drops a 100% card to 90%).
	if err := s.applyStage(ctx, b, card, board.StageReview); err != nil {
		return created, err
	}
	s.logEvent(ctx, b, created, board.EventCreated, "", "")
	s.logEvent(ctx, b, card, board.EventReviewSent, "", reviewer)
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
	// Re-review: sending an already-passed card back to the SAME reviewer
	// reactivates their finished review card instead of orphaning it — the
	// reverse of "review done → off review". The original goes back on the
	// review stage (which clamps its progress off 100%), the review card's
	// progress resets to 0 for the fresh round, and its round counter ticks
	// up (the first review is round 1, implicit; the first re-review is 2).
	sameReviewer := len(reviewCard.Assignees) > 0 && reviewCard.Assignees[0] == reviewer
	if sameReviewer && board.Complete(reviewCard.Stage, reviewCard.Progress) {
		if err := s.applyStage(ctx, b, card, board.StageReview); err != nil {
			return err
		}
		if err := s.reactivateReviewCard(ctx, b, card); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventReviewSent, "", reviewer)
		return nil
	}
	// An untouched review card is simply handed to the new reviewer. One the
	// old reviewer already worked on (progress > 0) keeps their work: it is
	// released from the link (a standalone card now — the next carry-over
	// leaves it behind) and a fresh review card is created for the new
	// reviewer.
	if reviewCard.Progress == 0 {
		prev := ""
		if len(reviewCard.Assignees) > 0 {
			prev = reviewCard.Assignees[0]
		}
		if err := s.backend.SetAssignee(ctx, b, reviewCard, reviewer); err != nil {
			return err
		}
		s.logEvent(ctx, b, card, board.EventReviewSent, prev, reviewer)
		return nil
	}
	if err := s.backend.SetReviewOf(ctx, b, reviewCard, ""); err != nil {
		return err
	}
	_, err = s.sendToReview(ctx, b, card, reviewer, day)
	return err
}

// reviewStillRequired reports whether a review card's review is still on: its
// original is on the board, unfinished, on the review stage, and the review
// card still has a reviewer assigned.
func reviewStillRequired(b board.Board, review board.Card) bool {
	if len(review.Assignees) == 0 {
		return false
	}
	for _, c := range b.Cards {
		if c.ItemID == review.ReviewOf {
			return c.Stage == board.StageReview && !board.Complete(c.Stage, c.Progress)
		}
	}
	return false
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
	if err := s.backend.DeleteCard(ctx, b, reviewCard); err != nil {
		return err
	}
	if original, found := findCard(b, itemID); found {
		reviewer := ""
		if len(reviewCard.Assignees) > 0 {
			reviewer = reviewCard.Assignees[0]
		}
		s.logEvent(ctx, b, original, board.EventReviewerRemoved, reviewer, "")
	}
	return nil
}

// --- Card field actions ----------------------------------------------------

// SetAssignee replaces a card's assignee (login = "" unassigns). It mirrors
// handleSetAssignee in TeamBoard.tsx.
func (s *Service) SetAssignee(ctx context.Context, owner string, project int, itemID, login string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	if err := s.backend.SetAssignee(ctx, b, card, login); err != nil {
		return err
	}
	prev := ""
	if len(card.Assignees) > 0 {
		prev = card.Assignees[0]
	}
	s.logEvent(ctx, b, card, board.EventAssignee, prev, login)
	return nil
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
	s.logEvent(ctx, b, card, board.EventTeam, card.Team, team)
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
// SetDescription sets a card's description. The description live-syncs with
// the linked counterpart — editing the original updates its review card and
// vice versa, so both always show the same context. Notes stay per-card.
func (s *Service) SetDescription(ctx context.Context, owner string, project int, itemID, description string) error {
	b, card, err := s.loadCard(ctx, owner, project, itemID)
	if err != nil {
		return err
	}
	if err := s.backend.SetDescription(ctx, b, card, description); err != nil {
		return err
	}
	if other, ok := reviewCounterpart(b, card); ok {
		return s.backend.SetDescription(ctx, b, other, description)
	}
	return nil
}

// reviewCounterpart finds the card linked to this one through the review
// relation: the original for a review card, or the review card of an original.
func reviewCounterpart(b board.Board, card board.Card) (board.Card, bool) {
	for _, c := range b.Cards {
		if card.ReviewOf != "" && c.ItemID == card.ReviewOf {
			return c, true
		}
		if card.ReviewOf == "" && c.ReviewOf == card.ItemID {
			return c, true
		}
	}
	return board.Card{}, false
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
	// A review card belongs to the SAME sprint as the card it reviews, not
	// merely the team's current pointer — otherwise a card being reviewed in an
	// older, not-yet-carried sprint would get a review card in a different
	// sprint, and carry-over would split them.
	sprintStart := card.SprintStart
	if sprintStart == "" {
		sprintStart = board.CurrentSprint(b, card.Team)
	}
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
	if err := s.backend.SetSprintStart(ctx, b, card, sprintStart); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventPlanTaken, "", engineer)
	return nil
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
	if len(card.Assignees) == 0 && card.Progress == 0 {
		// A pure plan card is deleted for real (an earlier-week demote would
		// boomerang back on the next carry-week).
		return s.deleteWithCascade(ctx, b, card)
	}
	if err := s.backend.SetPlan(ctx, b, card, board.PlanNone); err != nil {
		return err
	}
	if err := s.backend.SetWeek(ctx, b, card, ""); err != nil {
		return err
	}
	s.logEvent(ctx, b, card, board.EventPlanReleased, string(card.Plan), "")
	return nil
}
