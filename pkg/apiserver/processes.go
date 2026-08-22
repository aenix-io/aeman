package apiserver

import (
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// ProcessList is the Process tab on the wire: every process with its
// templates, each template with its recent history — enough to draw the tab
// and to answer "is this process alive" without a second request.
type ProcessList struct {
	Kind  string    `json:"kind"`
	Items []Process `json:"items"`
}

// Process is one process and the templates it iterates on.
type Process struct {
	Name      string     `json:"name"`
	Project   string     `json:"project,omitempty"`
	Templates []Template `json:"templates"`
}

// Template is what an iteration is copied from, plus how the last few went.
type Template struct {
	UID         string `json:"uid"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Recurrence  string `json:"recurrence"`
	Start       string `json:"start,omitempty"`
	Team        string `json:"team,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	Accumulate  bool   `json:"accumulate,omitempty"`
	// History lists the template's iterations, oldest first, as they went.
	History []Iteration `json:"history"`
}

// Iteration is one spawned card and how it went: "done" when it closed,
// "open" while it still runs inside its cycle, "late" when it is still open
// past the point the next one was due.
type Iteration struct {
	UID   string `json:"uid"`
	Week  string `json:"week"`
	State string `json:"state"`
}

// ProcessesResource builds the Process tab from a board, filtered to one
// project when asked ("" = every project).
func ProcessesResource(b board.Board, project string) ProcessList {
	today := board.TodayIso()
	out := ProcessList{Kind: "ProcessList", Items: []Process{}}
	for _, p := range b.Processes {
		if project != "" && p.Project != project {
			continue
		}
		proc := Process{Name: p.Name, Project: p.Project, Templates: []Template{}}
		for _, t := range board.TemplatesOf(b, p.Name) {
			tpl := Template{
				UID:         t.ItemID,
				Title:       boardservice.TemplateTitle(t),
				Description: boardservice.TemplateDescription(t),
				Recurrence:  t.Recurrence,
				Start:       t.StartDate,
				Team:        t.Team,
				Accumulate:  t.Accumulate,
				History:     []Iteration{},
			}
			if len(t.Assignees) > 0 {
				tpl.Assignee = t.Assignees[0]
			}
			for _, it := range board.Iterations(b, t.ItemID) {
				tpl.History = append(tpl.History, Iteration{
					UID: it.ItemID, Week: it.Week, State: iterationState(it, t, today),
				})
			}
			proc.Templates = append(proc.Templates, tpl)
		}
		out.Items = append(out.Items, proc)
	}
	return out
}

// iterationState: done, or open within its cycle, or late once the cycle has
// rolled past it without it closing.
func iterationState(it, tpl board.Card, today string) string {
	if board.Complete(it.Stage, it.Progress) {
		return "done"
	}
	if it.Week == "" {
		return "open"
	}
	// The next due date after the iteration's week: still open past it means
	// the process has already fallen behind.
	next := board.NextAfter(tpl.Recurrence, tpl.StartDate, board.AddDays(it.Week, 6))
	if next != "" && next <= today {
		return "late"
	}
	return "open"
}
