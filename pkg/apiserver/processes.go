package apiserver

import (
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// ProcessList is the Process tab on the wire: every process with its
// tasks, each task with its recent history — enough to draw the tab
// and to answer "is this process alive" without a second request.
type ProcessList struct {
	Kind  string    `json:"kind"`
	Items []Process `json:"items"`
}

// Process is one process and the tasks it iterates on.
type Process struct {
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
	Paused  bool   `json:"paused,omitempty"`
	Tasks   []Task `json:"tasks"`
}

// Task is what an iteration is copied from, plus how the last few went.
type Task struct {
	UID         string `json:"uid"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Recurrence  string `json:"recurrence"`
	Start       string `json:"start,omitempty"`
	Team        string `json:"team,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	Accumulate  bool   `json:"accumulate,omitempty"`
	// History lists the task's recent turns, oldest first, as they went —
	// the last HistoryShown of them. A process that has run for a year has
	// fifty, nobody reads more than the recent ones, and this list rides in
	// every board frame.
	History []Iteration `json:"history"`
	// Turns, Done and Late count ALL of them, so the tail that is not sent
	// still counts. Turns == len(History) means nothing was left out.
	Turns int `json:"turns"`
	Done  int `json:"done"`
	Late  int `json:"late"`
}

// HistoryShown bounds the turns carried per task.
const HistoryShown = 12

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
		proc := Process{Name: p.Name, Project: p.Project, Paused: p.Paused, Tasks: []Task{}}
		for _, t := range board.TasksOf(b, p.Name) {
			task := Task{
				UID:         t.ItemID,
				Title:       boardservice.TaskTitle(t),
				Description: boardservice.TaskDescription(t),
				Recurrence:  t.Recurrence,
				Start:       t.StartDate,
				Team:        t.Team,
				Accumulate:  t.Accumulate,
				History:     []Iteration{},
			}
			if len(t.Assignees) > 0 {
				task.Assignee = t.Assignees[0]
			}
			for _, it := range board.Iterations(b, t.ItemID) {
				turn := Iteration{UID: it.ItemID, Week: it.Week, State: iterationState(it, t, today)}
				task.Turns++
				switch turn.State {
				case "done":
					task.Done++
				case "late":
					task.Late++
				}
				task.History = append(task.History, turn)
			}
			// Oldest first, and only the tail: turns interleave in time only
			// within a task, but a task written long ago can still hold more
			// than anyone will read.
			if len(task.History) > HistoryShown {
				task.History = task.History[len(task.History)-HistoryShown:]
			}
			proc.Tasks = append(proc.Tasks, task)
		}
		out.Items = append(out.Items, proc)
	}
	return out
}

// iterationState: done, or open within its cycle, or late once the cycle has
// rolled past it without it closing.
// iterationState: done, or late once the week it was owed in has passed with
// it still open, or open. One definition with the rest of the board
// (board.Overdue): a turn used to count as late only when the NEXT turn came
// due, so a monthly task went red a month after its week, not a day after.
func iterationState(it, _ board.Card, today string) string {
	if board.Complete(it.Stage, it.Progress) {
		return "done"
	}
	if board.Overdue(it, today) {
		return "late"
	}
	return "open"
}
