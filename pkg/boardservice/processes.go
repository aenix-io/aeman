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

// ErrProcessInUse is deleting a process that still has tasks: deleting
// them silently would stop work nobody decided to stop.
var ErrProcessInUse = errors.New("process still has tasks")

// ErrTaskNotFound is naming a task that does not exist.
var ErrTaskNotFound = errors.New("unknown process task")

// AddProcess declares a process inside a project by creating its hidden state
// card (the team-roster mechanism: the card's position is the order).
func (s *Service) AddProcess(ctx context.Context, boardID string, name, projectName string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("process name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
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

// DeleteProcess removes an EMPTY process. One that still has tasks is
// protected (ErrProcessInUse): delete them first, on purpose.
func (s *Service) DeleteProcess(ctx context.Context, boardID string, name string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if n := len(board.TasksOf(b, name)); n > 0 {
		return fmt.Errorf("%w: %d template(s) under %q — delete them first", ErrProcessInUse, n, name)
	}
	p, ok := board.FindProcess(b, name)
	if !ok || p.ItemID == "" {
		return nil
	}
	stub := board.Card{ItemID: p.ItemID, Title: board.ProcessStateTitle, Process: name}
	return s.backend.DeleteCard(ctx, b, stub)
}

// RenameProcess renames a process and re-points its tasks at the new
// name — the name is the reference, so both move together.
func (s *Service) RenameProcess(ctx context.Context, boardID string, from, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("process name must not be empty")
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
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
	for _, t := range board.TasksOf(b, from) {
		if err := s.backend.SetProcess(ctx, b, t, to); err != nil {
			return err
		}
	}
	return nil
}

// SetProcessProject moves a process to another project ("" = the no-project
// bucket). Its tasks and their iterations are untouched: a process
// belongs to a project, and the work it spawns belongs to the process.
func (s *Service) SetProcessProject(ctx context.Context, boardID string, name, projectName string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
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

// SetProcessPaused stops a process spawning, or starts it again. Its
// tasks and their history are untouched: pausing is not deleting, and a
// process nobody can pause gets deleted instead.
func (s *Service) SetProcessPaused(ctx context.Context, boardID string, name string, paused bool) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	p, ok := board.FindProcess(b, name)
	if !ok || p.ItemID == "" {
		return fmt.Errorf("%w %q", ErrProcessNotFound, name)
	}
	if p.Paused == paused {
		return nil
	}
	stub := board.Card{
		ItemID: p.ItemID, Title: board.ProcessStateTitle,
		Process: name, Project: p.Project, Paused: p.Paused,
	}
	if err := s.backend.SetPaused(ctx, b, stub, paused); err != nil {
		return err
	}
	// Resuming files what this week is already owed, so a process picks up
	// where it left off rather than at the next carry.
	if !paused {
		for _, t := range board.TasksOf(b, name) {
			s.spawnDue(ctx, boardID, t.ItemID)
		}
	}
	return nil
}

// ReorderProcesses applies a shared process order: the hidden state cards
// are moved into the given sequence, and their board position IS the order
// every client reads back. Names missing from the board are skipped; ones
// missing from the list keep their positions after the reordered block.
func (s *Service) ReorderProcesses(ctx context.Context, boardID string, names []string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	// The block starts where the first process sits today, so reordering the
	// processes does not hoist them above unrelated state cards.
	prev := ""
	for _, name := range names {
		p, ok := board.FindProcess(b, name)
		if !ok || p.ItemID == "" {
			continue
		}
		stub := board.Card{ItemID: p.ItemID, Title: board.ProcessStateTitle, Process: name}
		if err := s.backend.MoveCard(ctx, b, stub, prev); err != nil {
			return err
		}
		prev = p.ItemID
	}
	return nil
}

// ReorderProcessTasks applies one process's task order — and adopts any task
// the order names that belongs to ANOTHER process, which is how a drop from
// one process into another lands: the final order of the target is the whole
// instruction. A task's turns keep their history; only future ones follow
// the new process (they name the task, and the task now names this process).
func (s *Service) ReorderProcessTasks(ctx context.Context, boardID string, process string, uids []string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if _, ok := board.FindProcess(b, process); !ok {
		return fmt.Errorf("%w %q", ErrProcessNotFound, process)
	}
	prev := ""
	for _, uid := range uids {
		t, ok := findTask(b, uid)
		if !ok {
			continue
		}
		if t.Process != process {
			if err := s.backend.SetProcess(ctx, b, t, process); err != nil {
				return err
			}
		}
		if err := s.backend.MoveCard(ctx, b, t, prev); err != nil {
			return err
		}
		prev = t.ItemID
	}
	return nil
}

// TaskArgs is what a process task says about the iterations it will
// spawn. Recurrence is the cycle; Start the calendar anchor it is counted
// from (defaults to today); Team the weekly plan the iterations land in;
// Assignee the standing owner, if any.
type TaskArgs struct {
	Title       string
	Description string
	Recurrence  string
	Start       string
	Team        string
	Assignee    string
	Accumulate  bool
}

// AddProcessTask declares what a process iterates on. A task is a
// whole card kept out of the board's rows: its title and description are the
// iteration's, and every iteration is copied from it anew.
func (s *Service) AddProcessTask(ctx context.Context, boardID string, process string, a TaskArgs) (board.Card, error) {
	a.Title = oneLine(a.Title)
	if a.Title == "" {
		return board.Card{}, fmt.Errorf("task title must not be empty")
	}
	if a.Recurrence == "" || !board.ValidRecurrence(a.Recurrence) {
		return board.Card{}, fmt.Errorf("%w: a task needs a cycle (week | 2weeks | month | quarter)", ErrInvalidStage)
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
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
	// and body travel in the description (see taskBody).
	// The iteration's title and body live in the task's description —
	// first line the title, the rest the body — and they are written WITH the
	// create: a task that appeared nameless and filled in a second later
	// looked like a board that had lost the name.
	created, err := s.backend.CreateCard(ctx, b, board.CreateInput{
		Title:      board.ProcessTaskTitle,
		Process:    process,
		Recurrence: a.Recurrence,
		Start:      a.Start,
		Team:       a.Team,
		Assignee:   a.Assignee,
		Body:       taskBody(a.Title, a.Description),
	})
	if err != nil {
		return board.Card{}, err
	}
	if a.Accumulate {
		if err := s.backend.SetAccumulate(ctx, b, created, true); err != nil {
			return board.Card{}, err
		}
	}
	created.Description = taskBody(a.Title, a.Description)
	created.Accumulate = a.Accumulate
	// If this week is already owed an iteration, hand it over now: a task
	// added on Monday should show in Monday's plan, not after someone carries
	// the week.
	s.spawnDue(ctx, boardID, created.ItemID)
	return created, nil
}

// taskBody packs an iteration's title and body into one description.
// oneLine folds a title onto the single line the body format gives it. A
// title is one line by definition, and a pasted newline used to split it
// silently: the tail became the description, and the task came back named
// after its first word.
func oneLine(title string) string {
	return strings.Join(strings.Fields(title), " ")
}

// taskBody packs an iteration's title and body into one description. The
// title is written as a heading — not because it is styled, but because a
// bare "[Urgent] Invoice" is note-shaped ("[timestamp] text") and the draft
// body parser would file it as a log line, leaving the task nameless.
func taskBody(title, description string) string {
	if description == "" {
		return taskTitleMark + title
	}
	return taskTitleMark + title + "\n" + description
}

// taskTitleMark leads the title line of a task's body.
const taskTitleMark = "# "

// TaskTitle and TaskDescription unpack a task's description.
func TaskTitle(t board.Card) string {
	title, _, _ := strings.Cut(t.Description, "\n")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(title), taskTitleMark))
}

func TaskDescription(t board.Card) string {
	_, rest, _ := strings.Cut(t.Description, "\n")
	return strings.TrimSpace(rest)
}

// DeleteProcessTask removes a task. Its past iterations are ordinary
// cards and stay — they are the record of what was done.
func (s *Service) DeleteProcessTask(ctx context.Context, boardID string, taskID string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	t, ok := findTask(b, taskID)
	if !ok {
		return fmt.Errorf("%w %q", ErrTaskNotFound, taskID)
	}
	// The turns stay — they are the record of work that was done — but they
	// stop pointing at a task that no longer exists. A dangling link left
	// them out of every process's history while still forbidding them to
	// leave the recurrent stage: stranded, and unfixable from the UI.
	for _, it := range board.Iterations(b, taskID) {
		if err := s.backend.SetTask(ctx, b, it, ""); err != nil {
			return err
		}
		if it.Recurrence != "" {
			if err := s.backend.SetRecurrence(ctx, b, it, ""); err != nil {
				return err
			}
		}
	}
	return s.backend.DeleteCard(ctx, b, t)
}

// UpdateProcessTask changes what the NEXT iterations will be. Only the
// provided fields apply (nil = untouched); the running iteration is left
// exactly as it is — that is the whole point of a task.
type TaskPatch struct {
	Title       *string
	Description *string
	Recurrence  *string
	Start       *string
	Team        *string
	Assignee    *string
	Accumulate  *bool
}

func (s *Service) UpdateProcessTask(ctx context.Context, boardID string, taskID string, p TaskPatch) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	t, ok := findTask(b, taskID)
	if !ok {
		return fmt.Errorf("%w %q", ErrTaskNotFound, taskID)
	}
	if p.Title != nil || p.Description != nil {
		title, desc := TaskTitle(t), TaskDescription(t)
		if p.Title != nil {
			title = oneLine(*p.Title)
		}
		if p.Description != nil {
			desc = *p.Description
		}
		if title == "" {
			return fmt.Errorf("task title must not be empty")
		}
		if err := s.backend.SetDescription(ctx, b, t, taskBody(title, desc)); err != nil {
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
	// Routing follows to the turn already running; content does not. The
	// title and the body of a live card may have been edited by the people
	// doing the work and are theirs, but the team and the owner say WHO does
	// it — fixing that a minute after creating the task has to take
	// effect on the card in front of them, not only on next month's.
	if p.Team != nil || p.Assignee != nil {
		if err := s.routeOpenIterations(ctx, boardID, taskID); err != nil {
			return err
		}
	}
	// A changed cycle, start, team or title can make this week due when it was
	// not: give it its card now rather than at the next carry.
	s.spawnDue(ctx, boardID, taskID)
	return nil
}

// routeOpenIterations points a task's unfinished turns at its current team
// and owner, and dates an owned one across its week so it reaches that
// person's day board. A finished turn is history and is left alone.
func (s *Service) routeOpenIterations(ctx context.Context, boardID string, taskID string) error {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	t, ok := findTask(b, taskID)
	if !ok {
		return nil
	}
	who := ""
	if len(t.Assignees) > 0 {
		who = t.Assignees[0]
	}
	week := board.MondayOf(board.TodayIso())
	moved := false
	for _, it := range board.Iterations(b, taskID) {
		if board.Complete(it.Stage, it.Progress) || it.Week != week {
			continue // history, and other weeks, are not re-routed
		}
		mine := len(it.Assignees) == 1 && it.Assignees[0] == who
		if who == "" {
			mine = len(it.Assignees) == 0
		}
		if mine && it.Team == t.Team {
			continue
		}
		moved = true
		// The rule reviews already use: a card nobody has touched is not
		// worth handing over — it is deleted and the new person gets a fresh
		// one. A card with work in it stays with whoever did that work.
		if it.Progress == 0 {
			if err := s.backend.DeleteCard(ctx, b, it); err != nil {
				return err
			}
		}
	}
	if !moved {
		return nil
	}
	// The new owner always gets this week's turn, whether the old card was
	// deleted or left standing with someone's work in it.
	b, err = s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return err
	}
	if t, ok = findTask(b, taskID); !ok {
		return nil
	}
	return s.spawnIteration(ctx, b, t, week)
}

func findTask(b board.Board, id string) (board.Card, bool) {
	for _, t := range b.Tasks {
		if t.ItemID == id {
			return t, true
		}
	}
	return board.Card{}, false
}

// SpawnIterations files, into the weekly plan of `week`, one iteration for
// every task whose cycle puts a due date inside that week. It is what
// makes a process run without anyone pressing anything per template:
// carry_week calls it for the week it carries into.
//
// A task whose previous iteration is still open does NOT spawn — the open
// card IS the process, and it simply goes overdue — unless the task
// accumulates, in which case unpaid months pile up as separate cards. And a
// week that already holds an iteration of a task never gets a second
// (re-running carry_week must be idempotent).
func (s *Service) SpawnIterations(ctx context.Context, b board.Board, team, week string, dryRun bool) (int, error) {
	spawned := 0
	for _, t := range b.Tasks {
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

// SpawnDue files, for every team on the board, the turns the CURRENT week is
// owed. It is what makes a process run by itself: the server sweeps after
// each background refresh, so a turn appears when its week arrives, not when
// somebody remembers to press something. Idempotent — a week that already
// holds a task's turn is skipped (spawnIfDue).
func (s *Service) SpawnDue(ctx context.Context, boardID string) (int, error) {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return 0, err
	}
	week := board.MondayOf(board.TodayIso())
	teams := map[string]bool{}
	for _, t := range b.Tasks {
		teams[t.Team] = true
	}
	total := 0
	for team := range teams {
		n, err := s.SpawnIterations(ctx, b, team, week, false)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// spawnIfDue files one task's iteration for a week, if that week is owed
// one. It reports whether an iteration was (or would be) spawned.
func (s *Service) spawnIfDue(ctx context.Context, b board.Board, t board.Card, week string, dryRun bool) (bool, error) {
	// A task with no title is a torn create (the card landed, the
	// description did not): spawning a nameless card from it helps nobody.
	if t.Recurrence == "" || TaskTitle(t) == "" {
		return false, nil
	}
	// A paused process files nothing — and a task whose process is not on the
	// board files nothing either. The only way to orphan one is to delete the
	// process's card by hand upstream (the service refuses while tasks
	// remain), and a task nobody can see, pause or delete must not keep
	// filing work into people's weeks.
	p, ok := board.FindProcess(b, t.Process)
	if !ok || p.Paused {
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

// spawnDue files the CURRENT week's iteration for one task, so a task
// that is due now produces its card the moment it is written rather than
// waiting for someone to carry the week. Failing to spawn does not fail the
// write: the task is saved either way, and the sweep will catch it.
func (s *Service) spawnDue(ctx context.Context, boardID string, taskID string) {
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return
	}
	t, ok := findTask(b, taskID)
	if !ok {
		return
	}
	if _, err := s.spawnIfDue(ctx, b, t, board.MondayOf(board.TodayIso()), false); err != nil {
		slog.Warn("process iteration not spawned", "task", taskID, "err", err)
	}
}

// spawnIteration copies a task into one weekly-plan card.
func (s *Service) spawnIteration(ctx context.Context, b board.Board, t board.Card, week string) error {
	in := board.CreateInput{
		Title:      TaskTitle(t),
		Plan:       board.PlanFri,
		Week:       week,
		Team:       t.Team,
		Task:       t.ItemID,
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
	if desc := TaskDescription(t); desc != "" {
		return s.backend.SetDescription(ctx, b, created, desc)
	}
	return nil
}
