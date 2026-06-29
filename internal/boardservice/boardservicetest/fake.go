// Package boardservicetest provides an in-memory boardservice.Backend for tests
// of the HTTP API and the MCP server, so they exercise the real boardservice
// logic over a seeded board without touching GitHub.
package boardservicetest

import (
	"context"
	"fmt"
	"strings"

	"github.com/aenix-org/aeman/internal/board"
	"github.com/aenix-org/aeman/internal/boardservice"
)

// Backend is an in-memory boardservice.Backend. It logs every call and mutates
// its board so the service's views reflect the result of an action.
type Backend struct {
	board   board.Board
	log     []string
	creates []board.CreateInput
	nextID  int
}

// New builds a Backend seeded with cards and per-team sprint states.
func New(cards []board.Card, states map[string]board.SprintState) *Backend {
	if states == nil {
		states = map[string]board.SprintState{}
	}
	return &Backend{board: board.Board{ID: "B1", Number: 1, Owner: "acme", Cards: cards, SprintStates: states}}
}

func (f *Backend) rec(format string, a ...any) { f.log = append(f.log, fmt.Sprintf(format, a...)) }

// Saw reports whether the call log contains an exact entry.
func (f *Backend) Saw(s string) bool {
	for _, e := range f.log {
		if e == s {
			return true
		}
	}
	return false
}

// Count returns how many logged calls start with prefix.
func (f *Backend) Count(prefix string) int {
	n := 0
	for _, e := range f.log {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// Card returns a pointer to the seeded card with the given item id, or nil.
func (f *Backend) Card(itemID string) *board.Card {
	for i := range f.board.Cards {
		if f.board.Cards[i].ItemID == itemID {
			return &f.board.Cards[i]
		}
	}
	return nil
}

// Creates returns the create inputs the service issued, in order.
func (f *Backend) Creates() []board.CreateInput { return f.creates }

// LoadBoard returns a copy of the seeded board snapshot.
func (f *Backend) LoadBoard(_ context.Context, _ string, _ int) (board.Board, error) {
	f.rec("LoadBoard")
	cards := make([]board.Card, len(f.board.Cards))
	copy(cards, f.board.Cards)
	states := map[string]board.SprintState{}
	for k, v := range f.board.SprintStates {
		states[k] = v
	}
	return board.Board{ID: f.board.ID, Number: f.board.Number, Owner: f.board.Owner, Cards: cards, SprintStates: states}, nil
}

// CreateCard appends a new card and records the create input.
func (f *Backend) CreateCard(_ context.Context, _ board.Board, in board.CreateInput) (board.Card, error) {
	f.nextID++
	card := board.Card{
		ItemID: fmt.Sprintf("new%d", f.nextID), Title: in.Title, IsDraft: true,
		Zone: in.Zone, StartDate: in.Start, SprintStart: in.SprintStart,
		Plan: in.Plan, Week: in.Week, Team: in.Team, ReviewOf: in.ReviewOf,
		Assignees: []string{},
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
	f.rec("MoveCard %s after=%s", card.ItemID, afterID)
	return nil
}

// AddNote records a note on a card.
func (f *Backend) AddNote(_ context.Context, _ board.Board, card board.Card, text string) error {
	f.rec("AddNote %s %s", card.ItemID, text)
	return nil
}

// RenameCard changes a card's title.
func (f *Backend) RenameCard(_ context.Context, _ board.Board, card board.Card, title string) error {
	f.rec("RenameCard %s", card.ItemID)
	if c := f.Card(card.ItemID); c != nil {
		c.Title = title
	}
	return nil
}

// SetStage sets a card's stage.
func (f *Backend) SetStage(_ context.Context, _ board.Board, card board.Card, stage board.StageKey) error {
	f.rec("SetStage %s %s", card.ItemID, stage)
	if c := f.Card(card.ItemID); c != nil {
		c.Stage = stage
	}
	return nil
}

// SetProgress sets a card's progress.
func (f *Backend) SetProgress(_ context.Context, _ board.Board, card board.Card, progress int) error {
	f.rec("SetProgress %s %d", card.ItemID, progress)
	if c := f.Card(card.ItemID); c != nil {
		c.Progress = progress
	}
	return nil
}

// SetZone sets a card's zone.
func (f *Backend) SetZone(_ context.Context, _ board.Board, card board.Card, zone board.ZoneKey) error {
	f.rec("SetZone %s %s", card.ItemID, zone)
	if c := f.Card(card.ItemID); c != nil {
		c.Zone = zone
	}
	return nil
}

// SetDay records a day change.
func (f *Backend) SetDay(_ context.Context, _ board.Board, card board.Card, day string) error {
	f.rec("SetDay %s %s", card.ItemID, day)
	return nil
}

// SetStart sets a card's start date.
func (f *Backend) SetStart(_ context.Context, _ board.Board, card board.Card, date string) error {
	f.rec("SetStart %s %s", card.ItemID, date)
	if c := f.Card(card.ItemID); c != nil {
		c.StartDate = date
	}
	return nil
}

// SetSprintStart sets a card's sprint-start date.
func (f *Backend) SetSprintStart(_ context.Context, _ board.Board, card board.Card, date string) error {
	f.rec("SetSprintStart %s %s", card.ItemID, date)
	if c := f.Card(card.ItemID); c != nil {
		c.SprintStart = date
	}
	return nil
}

// SetPlan sets a card's weekly-plan band.
func (f *Backend) SetPlan(_ context.Context, _ board.Board, card board.Card, plan board.PlanBand) error {
	f.rec("SetPlan %s %s", card.ItemID, plan)
	if c := f.Card(card.ItemID); c != nil {
		c.Plan = plan
	}
	return nil
}

// SetWeek sets a card's plan week.
func (f *Backend) SetWeek(_ context.Context, _ board.Board, card board.Card, week string) error {
	f.rec("SetWeek %s %s", card.ItemID, week)
	if c := f.Card(card.ItemID); c != nil {
		c.Week = week
	}
	return nil
}

// SetTeam sets a card's team.
func (f *Backend) SetTeam(_ context.Context, _ board.Board, card board.Card, team string) error {
	f.rec("SetTeam %s %s", card.ItemID, team)
	if c := f.Card(card.ItemID); c != nil {
		c.Team = team
	}
	return nil
}

// SetAssignee replaces a card's assignee.
func (f *Backend) SetAssignee(_ context.Context, _ board.Board, card board.Card, login string) error {
	f.rec("SetAssignee %s %s", card.ItemID, login)
	if c := f.Card(card.ItemID); c != nil {
		if login == "" {
			c.Assignees = []string{}
		} else {
			c.Assignees = []string{login}
		}
	}
	return nil
}

// SetReviewOf sets a card's review-of link.
func (f *Backend) SetReviewOf(_ context.Context, _ board.Board, card board.Card, reviewOf string) error {
	f.rec("SetReviewOf %s %s", card.ItemID, reviewOf)
	if c := f.Card(card.ItemID); c != nil {
		c.ReviewOf = reviewOf
	}
	return nil
}

// SetSprintState creates or updates a team's sprint pointer.
func (f *Backend) SetSprintState(_ context.Context, _ board.Board, team, current, previous string) error {
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
