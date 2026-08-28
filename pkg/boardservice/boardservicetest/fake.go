// Package boardservicetest provides an in-memory boardservice.Backend for tests
// of the HTTP API and the MCP server, so they exercise the real boardservice
// logic over a seeded board without touching GitHub.
package boardservicetest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// Backend is an in-memory boardservice.Backend. It logs every call and mutates
// its board so the service's views reflect the result of an action.
type Backend struct {
	// mu guards every field: the service resolves link titles on a background
	// goroutine (resolveTitleAsync), so the fake is hit concurrently with the
	// test's own requests and assertions.
	mu      sync.Mutex
	loadErr error
	refs    map[string]board.Link
	board   board.Board
	events  map[string][]board.Event // the history AppendEvent recorded, by card
	log     []string
	creates []board.CreateInput
	nextID  int
}

// New builds a Backend seeded with cards and per-team sprint states.
// Refs configures ResolveIssueRef answers, keyed by link URL (ignoring any
// fragment). URLs absent from the map fail to resolve.
func (f *Backend) SetRefs(refs map[string]board.Link) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refs = refs
}

// ResolveIssueRef satisfies boardservice.LinkResolver from the seeded refs.
func (f *Backend) ResolveIssueRef(_ context.Context, link board.Link) (board.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("ResolveIssueRef %s", link.URL)
	resolved, ok := f.refs[link.URL]
	if !ok {
		return link, fmt.Errorf("unresolvable ref %s", link.URL)
	}
	return resolved, nil
}

func New(cards []board.Card, states map[string]board.SprintState) *Backend {
	if states == nil {
		states = map[string]board.SprintState{}
	}
	return &Backend{board: board.Board{Board: "acme", Cards: cards, SprintStates: states}}
}

// rec appends to the call log. Callers hold f.mu.
func (f *Backend) rec(format string, a ...any) { f.log = append(f.log, fmt.Sprintf(format, a...)) }

// Saw reports whether the call log contains an exact entry.
func (f *Backend) Saw(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.log {
		if e == s {
			return true
		}
	}
	return false
}

// Count returns how many logged calls start with prefix.
func (f *Backend) Count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.log {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// Card returns a pointer to the seeded card with the given item id, or nil.
// Card returns a pointer INTO the fake's board: safe for assertions after the
// exercised call settles, not for concurrent use while async work runs.
func (f *Backend) Card(itemID string) *board.Card {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.card(itemID)
}

// card is the lock-free lookup the mutating methods use. Callers hold f.mu.
func (f *Backend) card(itemID string) *board.Card {
	for i := range f.board.Cards {
		if f.board.Cards[i].ItemID == itemID {
			return &f.board.Cards[i]
		}
	}
	return nil
}

// Creates returns the create inputs the service issued, in order.
func (f *Backend) Creates() []board.CreateInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]board.CreateInput(nil), f.creates...)
}

// FailLoad makes every LoadBoard answer err — the seam for testing how the
// server reacts to an upstream that refuses the caller (a dead token).
func (f *Backend) FailLoad(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadErr = err
}

// LoadBoard returns a copy of the seeded board snapshot.
func (f *Backend) LoadBoard(_ context.Context, _ string) (board.Board, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("LoadBoard")
	if f.loadErr != nil {
		return board.Board{}, f.loadErr
	}
	cards := make([]board.Card, 0, len(f.board.Cards))
	var epics []board.EpicCol
	var projects []string
	var deadlines []board.Deadline
	var processes []board.Process
	var tasks []board.Card
	seenEpic := map[string]bool{}
	projectStates := map[string]string{}
	for _, c := range f.board.Cards {
		if c.Title == board.ProcessStateTitle {
			if c.Process != "" {
				processes = append(processes, board.Process{
					Name: c.Process, Project: c.Project, Paused: c.Paused, ItemID: c.ItemID,
				})
			}
			continue
		}
		if c.Title == board.ProcessTaskTitle {
			tasks = append(tasks, c)
			continue
		}
		if c.Title == board.DeadlineStateTitle {
			if c.Week != "" {
				deadlines = append(deadlines, board.Deadline{
					Week: c.Week, Project: c.Project, ItemID: c.ItemID,
				})
			}
			continue
		}
		if c.Title == board.ProjectStateTitle {
			if c.Project != "" && projectStates[c.Project] == "" {
				projects = append(projects, c.Project)
				projectStates[c.Project] = c.ItemID
			}
			continue
		}
		if c.Title == board.EpicStateTitle {
			if k := c.Project + "\x00" + c.Epic; c.Epic != "" && !seenEpic[k] {
				seenEpic[k] = true
				epics = append(epics, board.EpicCol{
					Name: c.Epic, Project: c.Project, ItemID: c.ItemID,
				})
			}
			continue
		}
		cards = append(cards, c)
	}
	states := map[string]board.SprintState{}
	for k, v := range f.board.SprintStates {
		states[k] = v
	}
	return board.Board{Board: f.board.Board, Cards: cards,
		SprintStates: states, Epics: epics,
		Projects: projects, ProjectStates: projectStates, Deadlines: deadlines,
		Processes: processes, Tasks: tasks}, nil
}

// LoadCards returns the seeded cards matching ids, mirroring a partial reload.
func (f *Backend) LoadCards(_ context.Context, _ board.Board, ids []string) ([]board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("LoadCards")
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []board.Card
	for i := range f.board.Cards {
		if want[f.board.Cards[i].ItemID] {
			out = append(out, f.board.Cards[i])
		}
	}
	return out, nil
}

// CreateCard appends a new card and records the create input.
func (f *Backend) CreateCard(_ context.Context, _ board.Board, in board.CreateInput) (board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	card := board.Card{
		ItemID: fmt.Sprintf("new%d", f.nextID), Title: in.Title, Domain: in.Domain,
		Zone: in.Zone, Day: in.Day, StartDate: in.Start, SprintStart: in.SprintStart,
		Plan: in.Plan, Week: in.Week, Epic: in.Epic, Project: in.Project, Team: in.Team, ReviewOf: in.ReviewOf,
		Process: in.Process, Task: in.Task, Recurrence: in.Recurrence,
		Paused:      in.Paused,
		Description: in.Body,
		Parent:      in.Parent, Assignees: []string{},
	}
	if in.Assignee != "" {
		card.Assignees = []string{in.Assignee}
	}
	f.board.Cards = append(f.board.Cards, card)
	f.creates = append(f.creates, in)
	f.rec("CreateCard %s", card.ItemID)
	return card, nil
}

// DeleteCard removes a card from the board.
func (f *Backend) DeleteCard(_ context.Context, _ board.Board, card board.Card) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("DeleteCard %s", card.ItemID)
	out := f.board.Cards[:0]
	for _, c := range f.board.Cards {
		if c.ItemID != card.ItemID {
			out = append(out, c)
		}
	}
	f.board.Cards = out
	return nil
}

// MoveCard records a reorder (the in-memory order is left unchanged).
func (f *Backend) MoveCard(_ context.Context, _ board.Board, card board.Card, afterID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("MoveCard %s after=%s", card.ItemID, afterID)
	return nil
}

// AppendEvent records an activity event in the card's history.
func (f *Backend) AppendEvent(_ context.Context, _ board.Board, card board.Card, e board.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("AppendEvent %s %s %s->%s", card.ItemID, e.Kind, e.From, e.To)
	if f.card(card.ItemID) != nil {
		f.nextID++
		e.ID = fmt.Sprintf("ev%d", f.nextID)
		if f.events == nil {
			f.events = map[string][]board.Event{}
		}
		f.events[card.ItemID] = append(f.events[card.ItemID], e)
	}
	return nil
}

// CardLog is the history AppendEvent recorded, oldest first. The fake holds
// all of it, so it is never truncated.
func (f *Backend) CardLog(_ context.Context, _ board.Board, id string) ([]board.Event, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]board.Event(nil), f.events[id]...), time.Time{}, nil
}

// CardLogSince keeps the events at or after the boundary, the way the store
// does by stopping its walk there.
func (f *Backend) CardLogSince(_ context.Context, _ board.Board, id string, since time.Time) ([]board.Event, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("CardLogSince %s", id)
	var out []board.Event
	for _, e := range f.events[id] {
		at, err := time.Parse(time.RFC3339, e.At)
		if err != nil || since.IsZero() || !at.Before(since) {
			out = append(out, e)
		}
	}
	return out, time.Time{}, nil
}

// Events is the card's recorded history, for assertions.
func (f *Backend) Events(id string) []board.Event {
	evs, _, _ := f.CardLog(context.Background(), board.Board{}, id)
	return evs
}

func (f *Backend) AddNote(ctx context.Context, _ board.Board, card board.Card, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("AddNote %s %s", card.ItemID, text)
	if c := f.card(card.ItemID); c != nil {
		f.nextID++
		c.Notes = append(c.Notes, board.Note{
			ID:     fmt.Sprintf("note%d", f.nextID),
			Body:   text,
			Author: board.ActorFrom(ctx),
			Source: "log",
		})
	}
	return nil
}

// EditNote rewrites the note's body on the seeded card.
func (f *Backend) EditNote(_ context.Context, _ board.Board, card board.Card, note board.Note, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("EditNote %s %s %s", card.ItemID, note.ID, text)
	if c := f.card(card.ItemID); c != nil {
		for i := range c.Notes {
			if c.Notes[i].ID == note.ID {
				c.Notes[i].Body = text
			}
		}
	}
	return nil
}

// DeleteNote drops the note from the seeded card.
func (f *Backend) DeleteNote(_ context.Context, _ board.Board, card board.Card, note board.Note) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("DeleteNote %s %s", card.ItemID, note.ID)
	if c := f.card(card.ItemID); c != nil {
		out := c.Notes[:0]
		for _, n := range c.Notes {
			if n.ID != note.ID {
				out = append(out, n)
			}
		}
		c.Notes = out
	}
	return nil
}

// SetDescription replaces the seeded card's description.
func (f *Backend) SetDescription(_ context.Context, _ board.Board, card board.Card, description string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetDescription %s %s", card.ItemID, description)
	if c := f.card(card.ItemID); c != nil {
		c.Description = description
	}
	return nil
}

// RenameCard changes a card's title.
func (f *Backend) RenameCard(_ context.Context, _ board.Board, card board.Card, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("RenameCard %s", card.ItemID)
	if c := f.card(card.ItemID); c != nil {
		c.Title = title
	}
	return nil
}

// SetStage sets a card's stage.
func (f *Backend) SetStage(_ context.Context, _ board.Board, card board.Card, stage board.StageKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetStage %s %s", card.ItemID, stage)
	if c := f.card(card.ItemID); c != nil {
		c.Stage = stage
	}
	return nil
}

// SetProgress sets a card's progress.
func (f *Backend) SetProgress(_ context.Context, _ board.Board, card board.Card, progress int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetProgress %s %d", card.ItemID, progress)
	if c := f.card(card.ItemID); c != nil {
		// The storage's rule (gitstore): reaching 100 remembers where the
		// card came from, dropping below forgets it.
		switch {
		case progress >= 100 && c.Progress < 100:
			c.DoneFrom = c.Progress
		case progress < 100:
			c.DoneFrom = 0
		}
		c.Progress = progress
	}
	return nil
}

// SetZone sets a card's zone.
func (f *Backend) SetZone(_ context.Context, _ board.Board, card board.Card, zone board.ZoneKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetZone %s %s", card.ItemID, zone)
	if c := f.card(card.ItemID); c != nil {
		c.Zone = zone
	}
	return nil
}

// SetDay records a day change.
func (f *Backend) SetDay(_ context.Context, _ board.Board, card board.Card, day string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetDay %s %s", card.ItemID, day)
	if c := f.card(card.ItemID); c != nil {
		c.Day = day
	}
	return nil
}

// SetLeftAt records the day a personal card was left behind on ("" clears).
func (f *Backend) SetLeftAt(_ context.Context, _ board.Board, card board.Card, day string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetLeftAt %s %s", card.ItemID, day)
	if c := f.card(card.ItemID); c != nil {
		c.LeftAt = day
	}
	return nil
}

// SetStart sets a card's start date.
func (f *Backend) SetStart(_ context.Context, _ board.Board, card board.Card, date string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetStart %s %s", card.ItemID, date)
	if c := f.card(card.ItemID); c != nil {
		c.StartDate = date
	}
	return nil
}

// SetSprintStart sets a card's sprint-start date.
func (f *Backend) SetSprintStart(_ context.Context, _ board.Board, card board.Card, date string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetSprintStart %s %s", card.ItemID, date)
	if c := f.card(card.ItemID); c != nil {
		c.SprintStart = date
	}
	return nil
}

// SetPlan sets a card's weekly-plan band.
func (f *Backend) SetPlan(_ context.Context, _ board.Board, card board.Card, plan board.PlanBand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetPlan %s %s", card.ItemID, plan)
	if c := f.card(card.ItemID); c != nil {
		c.Plan = plan
	}
	return nil
}

// SetWeek sets a card's plan week.
func (f *Backend) SetWeek(_ context.Context, _ board.Board, card board.Card, week string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetWeek %s %s", card.ItemID, week)
	if c := f.card(card.ItemID); c != nil {
		c.Week = week
	}
	return nil
}

// SetTeam sets a card's team.
func (f *Backend) SetTeam(_ context.Context, _ board.Board, card board.Card, team string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetTeam %s %s", card.ItemID, team)
	if card.Title == board.SprintStateTitle {
		// The team's own stub: the declaration moves to the new name.
		if st, ok := f.board.SprintStates[card.Team]; ok {
			delete(f.board.SprintStates, card.Team)
			f.board.SprintStates[team] = st
		}
		return nil
	}
	if c := f.card(card.ItemID); c != nil {
		c.Team = team
	}
	return nil
}

// SetEpic files the card under a Project-board column.
func (f *Backend) SetEpic(_ context.Context, _ board.Board, card board.Card, epic string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetEpic %s %s", card.ItemID, epic)
	if c := f.card(card.ItemID); c != nil {
		c.Epic = epic
	}
	return nil
}

// SetProject writes a state card's Project field.
func (f *Backend) SetProject(_ context.Context, _ board.Board, card board.Card, project string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetProject %s %s", card.ItemID, project)
	if c := f.card(card.ItemID); c != nil {
		c.Project = project
	}
	return nil
}

// SetRecurrence sets a recurrent card's reseed cycle.
func (f *Backend) SetRecurrence(_ context.Context, _ board.Board, card board.Card, cycle string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetRecurrence %s %s", card.ItemID, cycle)
	if c := f.card(card.ItemID); c != nil {
		c.Recurrence = cycle
	}
	return nil
}

// SetAssignee replaces a card's assignee.
func (f *Backend) SetAssignee(_ context.Context, _ board.Board, card board.Card, login string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetAssignee %s %s", card.ItemID, login)
	if c := f.card(card.ItemID); c != nil {
		if login == "" {
			c.Assignees = []string{}
		} else {
			c.Assignees = []string{login}
		}
	}
	return nil
}

// SetParent sets a card's parent link.
func (f *Backend) SetParent(_ context.Context, _ board.Board, card board.Card, parent string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetParent %s %s", card.ItemID, parent)
	if c := f.card(card.ItemID); c != nil {
		c.Parent = parent
	}
	return nil
}

// SetReviewOf sets a card's review-of link.
func (f *Backend) SetReviewOf(_ context.Context, _ board.Board, card board.Card, reviewOf string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetReviewOf %s %s", card.ItemID, reviewOf)
	if c := f.card(card.ItemID); c != nil {
		c.ReviewOf = reviewOf
	}
	return nil
}

// SetReviewRound records a review card's round counter.
func (f *Backend) SetReviewRound(_ context.Context, _ board.Board, card board.Card, round int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetReviewRound %s %d", card.ItemID, round)
	if c := f.card(card.ItemID); c != nil {
		c.ReviewRound = round
	}
	return nil
}

// SetSprintState creates or updates a team's sprint pointer.
func (f *Backend) SetSprintState(_ context.Context, _ board.Board, team, current, previous string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetSprintState %s cur=%s prev=%s", team, current, previous)
	st := f.board.SprintStates[team]
	st.Current = current
	st.Previous = previous
	if st.ItemID == "" {
		f.nextID++
		st.ItemID = fmt.Sprintf("state%d", f.nextID)
	}
	f.board.SprintStates[team] = st
	return nil
}

// Backend must satisfy boardservice.Backend.
var _ boardservice.Backend = (*Backend)(nil)

func (f *Backend) SetProcess(_ context.Context, _ board.Board, card board.Card, process string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetProcess %s %s", card.ItemID, process)
	if c := f.card(card.ItemID); c != nil {
		c.Process = process
	}
	return nil
}

func (f *Backend) SetTask(_ context.Context, _ board.Board, card board.Card, task string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetTask %s %s", card.ItemID, task)
	if c := f.card(card.ItemID); c != nil {
		c.Task = task
	}
	return nil
}

func (f *Backend) SetAccumulate(_ context.Context, _ board.Board, card board.Card, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetAccumulate %s %v", card.ItemID, on)
	if c := f.card(card.ItemID); c != nil {
		c.Accumulate = on
	}
	return nil
}

func (f *Backend) SetPaused(_ context.Context, _ board.Board, card board.Card, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetPaused %s %v", card.ItemID, paused)
	if c := f.card(card.ItemID); c != nil {
		c.Paused = paused
	}
	return nil
}
