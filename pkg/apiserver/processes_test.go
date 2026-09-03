package apiserver

import (
	"fmt"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The history a frame carries is bounded, and the counts still describe every
// turn: a process running for years must not put its whole past into every
// board frame, and must not lie about how it has gone either.
func TestProcessHistoryIsBoundedButCountsAll(t *testing.T) {
	cards := []board.Card{
		{ItemID: "p1", Title: board.ProjectStateTitle, Project: "P"},
		{ItemID: "pr1", Title: board.ProcessStateTitle, Process: "Publishing", Project: "P"},
		{ItemID: "task", Title: board.ProcessTaskTitle, Process: "Publishing",
			Description: "# Article", Recurrence: "week", StartDate: "2026-01-05", Team: "alpha"},
	}
	// A year of weekly turns, every third one finished.
	week := "2026-01-05"
	for i := 0; i < 52; i++ {
		c := board.Card{
			ItemID: fmt.Sprintf("t%d", i), Title: "Article", Task: "task",
			Week: week, Team: "alpha", Stage: board.StageRecurrent,
		}
		if i%3 == 0 {
			c.Progress = 100
		}
		cards = append(cards, c)
		week = board.AddDays(week, 7)
	}
	b := board.NewBoard(cards)
	got := ProcessesResource(b, "")
	if len(got.Items) != 1 || len(got.Items[0].Tasks) != 1 {
		t.Fatalf("resource = %+v", got)
	}
	task := got.Items[0].Tasks[0]
	if len(task.History) != HistoryShown {
		t.Errorf("history = %d turns, want the last %d", len(task.History), HistoryShown)
	}
	if task.Turns != 52 {
		t.Errorf("turns = %d, want every one counted", task.Turns)
	}
	if want := 18; task.Done != want {
		t.Errorf("done = %d, want %d — the counts cover the turns history leaves out", task.Done, want)
	}
	// Oldest first, and the newest turn is the last one.
	if first, last := task.History[0], task.History[len(task.History)-1]; first.Week >= last.Week {
		t.Errorf("history is not chronological: %s … %s", first.Week, last.Week)
	}
	if task.History[len(task.History)-1].Week != board.AddDays("2026-01-05", 51*7) {
		t.Errorf("the last turn is not the newest: %s", task.History[len(task.History)-1].Week)
	}
}
