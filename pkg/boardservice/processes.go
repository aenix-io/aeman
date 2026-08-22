package boardservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrProcessExists guards AddProcess against doubling a process.
var ErrProcessExists = errors.New("process already exists")

// ErrProcessNotFound is naming a process that does not exist — a typo must
// not mint one. A rejected input (422), not an upstream failure.
var ErrProcessNotFound = errors.New("unknown process")

// ErrProcessInUse is deleting a process that still has templates: deleting
// them silently would stop work nobody decided to stop.
var ErrProcessInUse = errors.New("process still has templates")

// ErrTemplateNotFound is naming a template that does not exist.
var ErrTemplateNotFound = errors.New("unknown process template")

// AddProcess declares a process inside a project by creating its hidden state
// card (the team-roster mechanism: the card's position is the order).
func (s *Service) AddProcess(ctx context.Context, owner string, project int, name, projectName string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("process name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	if projectName != "" {
		if err := knownProject(b, projectName); err != nil {
			return err
		}
	}
	for _, p := range b.Processes {
		if strings.EqualFold(p.Name, name) {
			return fmt.Errorf("%w: %q", ErrProcessExists, p.Name)
		}
	}
	_, err = s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:   board.ProcessStateTitle,
		Process: name,
		Project: projectName,
	})
	return err
}

// DeleteProcess removes an EMPTY process. One that still has templates is
// protected (ErrProcessInUse): delete them first, on purpose.
func (s *Service) DeleteProcess(ctx context.Context, owner string, project int, name string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	if n := len(board.TemplatesOf(b, name)); n > 0 {
		return fmt.Errorf("%w: %d template(s) under %q — delete them first", ErrProcessInUse, n, name)
	}
	p, ok := board.FindProcess(b, name)
	if !ok || p.ItemID == "" {
		return nil
	}
	stub := board.Card{ItemID: p.ItemID, Title: board.ProcessStateTitle, Process: name}
	return s.backend.DeleteCard(ctx, b, stub)
}

// RenameProcess renames a process and re-points its templates at the new
// name — the name is the reference, so both move together.
func (s *Service) RenameProcess(ctx context.Context, owner string, project int, from, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("process name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	p, ok := board.FindProcess(b, from)
	if !ok || p.ItemID == "" {
		return fmt.Errorf("%w %q", ErrProcessNotFound, from)
	}
	if from == to {
		return nil
	}
	for _, other := range b.Processes {
		if other.Name != from && strings.EqualFold(other.Name, to) {
			return fmt.Errorf("%w: %q", ErrProcessExists, other.Name)
		}
	}
	stub := board.Card{ItemID: p.ItemID, Title: board.ProcessStateTitle, Process: from}
	if err := s.backend.SetProcess(ctx, b, stub, to); err != nil {
		return err
	}
	for _, t := range board.TemplatesOf(b, from) {
		if err := s.backend.SetProcess(ctx, b, t, to); err != nil {
			return err
		}
	}
	return nil
}

// SetProcessProject moves a process to another project ("" = the no-project
// bucket). Its templates and their iterations are untouched: a process
// belongs to a project, and the work it spawns belongs to the process.
func (s *Service) SetProcessProject(ctx context.Context, owner string, project int, name, projectName string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	if projectName != "" {
		if err := knownProject(b, projectName); err != nil {
			return err
		}
	}
	p, ok := board.FindProcess(b, name)
	if !ok || p.ItemID == "" {
		return fmt.Errorf("%w %q", ErrProcessNotFound, name)
	}
	if p.Project == projectName {
		return nil
	}
	stub := board.Card{ItemID: p.ItemID, Title: board.ProcessStateTitle, Process: name, Project: p.Project}
	return s.backend.SetProject(ctx, b, stub, projectName)
}

// TemplateArgs is what a process template says about the iterations it will
// spawn. Recurrence is the cycle; Start the calendar anchor it is counted
// from (defaults to today); Team the weekly plan the iterations land in;
// Assignee the standing owner, if any.
type TemplateArgs struct {
	Title       string
	Description string
	Recurrence  string
	Start       string
	Team        string
	Assignee    string
	Accumulate  bool
}

// AddProcessTemplate declares what a process iterates on. A template is a
// whole card kept out of the board's rows: its title and description are the
// iteration's, and every iteration is copied from it anew.
func (s *Service) AddProcessTemplate(ctx context.Context, owner string, project int, process string, a TemplateArgs) (board.Card, error) {
	a.Title = strings.TrimSpace(a.Title)
	if a.Title == "" {
		return board.Card{}, fmt.Errorf("template title must not be empty")
	}
	if a.Recurrence == "" || !board.ValidRecurrence(a.Recurrence) {
		return board.Card{}, fmt.Errorf("%w: a template needs a cycle (week | 2weeks | month | quarter)", ErrInvalidStage)
	}
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return board.Card{}, err
	}
	if _, ok := board.FindProcess(b, process); !ok {
		return board.Card{}, fmt.Errorf("%w %q — add it first (add_process)", ErrProcessNotFound, process)
	}
	if a.Start == "" {
		a.Start = board.TodayIso()
	}
	// The card's Title is the marker that hides it; the iteration's title
	// and body travel in the description (see templateBody).
	created, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:      board.ProcessTemplateTitle,
		Process:    process,
		Recurrence: a.Recurrence,
		Start:      a.Start,
		Team:       a.Team,
		Assignee:   a.Assignee,
	})
	if err != nil {
		return board.Card{}, err
	}
	// The iteration's title and body live in the template's description:
	// first line the title, the rest the body. One field, one write, and a
	// template reads like the card it will become.
	if err := s.backend.SetDescription(ctx, b, created, templateBody(a.Title, a.Description)); err != nil {
		return board.Card{}, err
	}
	if a.Accumulate {
		if err := s.backend.SetAccumulate(ctx, b, created, true); err != nil {
			return board.Card{}, err
		}
	}
	created.Description = templateBody(a.Title, a.Description)
	created.Accumulate = a.Accumulate
	// If this week is already owed an iteration, hand it over now: a template
	// added on Monday should show in Monday's plan, not after someone carries
	// the week.
	s.spawnDue(ctx, owner, project, created.ItemID)
	return created, nil
}

// templateBody packs an iteration's title and body into one description.
func templateBody(title, description string) string {
	if description == "" {
		return title
	}
	return title + "\n" + description
}

// TemplateTitle and TemplateDescription unpack a template's description.
func TemplateTitle(t board.Card) string {
	title, _, _ := strings.Cut(t.Description, "\n")
	return strings.TrimSpace(title)
}

func TemplateDescription(t board.Card) string {
	_, rest, _ := strings.Cut(t.Description, "\n")
	return strings.TrimSpace(rest)
}

// DeleteProcessTemplate removes a template. Its past iterations are ordinary
// cards and stay — they are the record of what was done.
func (s *Service) DeleteProcessTemplate(ctx context.Context, owner string, project int, templateID string) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	t, ok := findTemplate(b, templateID)
	if !ok {
		return fmt.Errorf("%w %q", ErrTemplateNotFound, templateID)
	}
	return s.backend.DeleteCard(ctx, b, t)
}

// UpdateProcessTemplate changes what the NEXT iterations will be. Only the
// provided fields apply (nil = untouched); the running iteration is left
// exactly as it is — that is the whole point of a template.
type TemplatePatch struct {
	Title       *string
	Description *string
	Recurrence  *string
	Start       *string
	Team        *string
	Assignee    *string
	Accumulate  *bool
}

func (s *Service) UpdateProcessTemplate(ctx context.Context, owner string, project int, templateID string, p TemplatePatch) error {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return err
	}
	t, ok := findTemplate(b, templateID)
	if !ok {
		return fmt.Errorf("%w %q", ErrTemplateNotFound, templateID)
	}
	if p.Title != nil || p.Description != nil {
		title, desc := TemplateTitle(t), TemplateDescription(t)
		if p.Title != nil {
			title = strings.TrimSpace(*p.Title)
		}
		if p.Description != nil {
			desc = *p.Description
		}
		if title == "" {
			return fmt.Errorf("template title must not be empty")
		}
		if err := s.backend.SetDescription(ctx, b, t, templateBody(title, desc)); err != nil {
			return err
		}
	}
	if p.Recurrence != nil {
		if *p.Recurrence == "" || !board.ValidRecurrence(*p.Recurrence) {
			return fmt.Errorf("%w: unknown cycle %q", ErrInvalidStage, *p.Recurrence)
		}
		if err := s.backend.SetRecurrence(ctx, b, t, *p.Recurrence); err != nil {
			return err
		}
	}
	if p.Start != nil {
		if err := s.backend.SetStart(ctx, b, t, *p.Start); err != nil {
			return err
		}
	}
	if p.Team != nil {
		if err := s.backend.SetTeam(ctx, b, t, *p.Team); err != nil {
			return err
		}
	}
	if p.Assignee != nil {
		if err := s.backend.SetAssignee(ctx, b, t, *p.Assignee); err != nil {
			return err
		}
	}
	if p.Accumulate != nil {
		if err := s.backend.SetAccumulate(ctx, b, t, *p.Accumulate); err != nil {
			return err
		}
	}
	// A changed cycle, start, team or title can make this week due when it was
	// not: give it its card now rather than at the next carry.
	s.spawnDue(ctx, owner, project, templateID)
	return nil
}

func findTemplate(b board.Board, id string) (board.Card, bool) {
	for _, t := range b.Templates {
		if t.ItemID == id {
			return t, true
		}
	}
	return board.Card{}, false
}

// SpawnIterations files, into the weekly plan of `week`, one iteration for
// every template whose cycle puts a due date inside that week. It is what
// makes a process run without anyone pressing anything per template:
// carry_week calls it for the week it carries into.
//
// A template whose previous iteration is still open does NOT spawn — the open
// card IS the process, and it simply goes overdue — unless the template
// accumulates, in which case unpaid months pile up as separate cards. And a
// week that already holds an iteration of a template never gets a second
// (re-running carry_week must be idempotent).
func (s *Service) SpawnIterations(ctx context.Context, b board.Board, team, week string, dryRun bool) (int, error) {
	spawned := 0
	for _, t := range b.Templates {
		if t.Team != team {
			continue
		}
		ok, err := s.spawnIfDue(ctx, b, t, week, dryRun)
		if err != nil {
			return spawned, err
		}
		if ok {
			spawned++
		}
	}
	return spawned, nil
}

// spawnIfDue files one template's iteration for a week, if that week is owed
// one. It reports whether an iteration was (or would be) spawned.
func (s *Service) spawnIfDue(ctx context.Context, b board.Board, t board.Card, week string, dryRun bool) (bool, error) {
	// A template with no title is a torn create (the card landed, the
	// description did not): spawning a nameless card from it helps nobody.
	if t.Recurrence == "" || TemplateTitle(t) == "" {
		return false, nil
	}
	// Due inside this week? The first due date after the day before the week,
	// if it falls before the week ends.
	due := board.NextAfter(t.Recurrence, t.StartDate, board.AddDays(week, -1))
	if due == "" || due > board.AddDays(week, 6) {
		return false, nil
	}
	open := false
	for _, it := range board.Iterations(b, t.ItemID) {
		if it.Week == week {
			return false, nil // this week already has its iteration
		}
		if !board.Complete(it.Stage, it.Progress) {
			open = true
		}
	}
	if open && !t.Accumulate {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	return true, s.spawnIteration(ctx, b, t, week)
}

// spawnDue files the CURRENT week's iteration for one template, so a template
// that is due now produces its card the moment it is written rather than
// waiting for someone to carry the week. Failing to spawn does not fail the
// write: the template is saved either way, and the sweep will catch it.
func (s *Service) spawnDue(ctx context.Context, owner string, project int, templateID string) {
	b, err := s.backend.LoadBoard(ctx, owner, project)
	if err != nil {
		return
	}
	t, ok := findTemplate(b, templateID)
	if !ok {
		return
	}
	if _, err := s.spawnIfDue(ctx, b, t, board.MondayOf(board.TodayIso()), false); err != nil {
		slog.Warn("process iteration not spawned", "template", templateID, "err", err)
	}
}

// spawnIteration copies a template into one weekly-plan card.
func (s *Service) spawnIteration(ctx context.Context, b board.Board, t board.Card, week string) error {
	in := board.CreateInput{
		Title:      TemplateTitle(t),
		Plan:       board.PlanFri,
		Week:       week,
		Team:       t.Team,
		Template:   t.ItemID,
		Recurrence: t.Recurrence,
	}
	if len(t.Assignees) > 0 {
		in.Assignee = t.Assignees[0]
		// Somebody owns this turn, so it is work with a name on it, not a
		// line in a plan waiting to be claimed: dating it across its week
		// puts it on that person's day board, which is where they look for
		// what to do. An unowned iteration stays dateless and waits in the
		// team's plan for whoever takes it.
		in.Start = week
		// The whole week, weekend included: a turn that is still open on
		// Saturday has not stopped being this week's work, and a card that
		// vanishes from the day board on Friday evening is a card nobody
		// finishes.
		in.Day = board.AddDays(week, 6)
	}
	created, err := s.backend.CreateCard(ctx, b, in)
	if err != nil {
		return err
	}
	s.logEvent(ctx, b, created, board.EventCreated, "", "")
	if err := s.backend.SetStage(ctx, b, created, board.StageRecurrent); err != nil {
		return err
	}
	if desc := TemplateDescription(t); desc != "" {
		return s.backend.SetDescription(ctx, b, created, desc)
	}
	return nil
}
