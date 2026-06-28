package ghprojects

import "testing"

func boolPtr(b bool) *bool { return &b }

// sprintBoard builds a board with a Stage field and the given cards, exercising
// the current-sprint computation that backs the startNewSprint create flag.
func sprintBoard() *Board {
	done := map[string]string{"Stage": "Done"}
	return &Board{
		Fields: []ProjectField{
			{ID: "SS", Name: "Sprint Start", DataType: "DATE"},
			{ID: "ST", Name: "Stage", DataType: "SINGLE_SELECT"},
		},
		Cards: []Card{
			// Team A: an older sprint and a later one (06-20), both all done.
			{ItemID: "1", Team: "A", SprintStart: "2026-06-10", Fields: done},
			{ItemID: "2", Team: "A", SprintStart: "2026-06-20", Fields: done},
			// Team B: current sprint still running (a card is not done).
			{ItemID: "3", Team: "B", SprintStart: "2026-06-18", Fields: map[string]string{"Stage": "Review"}},
			// A future sprint must be ignored when resolving "current".
			{ItemID: "4", Team: "B", SprintStart: "2026-07-01", Fields: map[string]string{"Stage": "Review"}},
		},
	}
}

func TestCurrentSprint(t *testing.T) {
	board := sprintBoard()
	const today = "2026-06-25"

	cases := []struct {
		team        string
		wantCur     string
		wantRunning bool
	}{
		{"A", "2026-06-20", false}, // latest sprint all done
		{"B", "2026-06-18", true},  // running; future 07-01 ignored
		{"C", "", false},           // no sprint at all
	}
	for _, c := range cases {
		cur, running := board.currentSprint(c.team, today)
		if cur != c.wantCur || running != c.wantRunning {
			t.Errorf("currentSprint(%q) = (%q,%v), want (%q,%v)",
				c.team, cur, running, c.wantCur, c.wantRunning)
		}
	}
}

func TestSprintStartForNew(t *testing.T) {
	board := sprintBoard()
	const today = "2026-06-25"

	cases := []struct {
		name           string
		team           string
		startNewSprint *bool
		want           string
	}{
		{"A auto starts new (current all done)", "A", nil, today},
		{"A force-join the done sprint", "A", boolPtr(false), "2026-06-20"},
		{"A force new", "A", boolPtr(true), today},
		{"B auto joins running", "B", nil, "2026-06-18"},
		{"B force new", "B", boolPtr(true), today},
		{"C auto with no sprint", "C", nil, today},
		{"C force-join falls back to today", "C", boolPtr(false), today},
	}
	for _, c := range cases {
		if got := board.sprintStartForNew(c.team, c.startNewSprint, today); got != c.want {
			t.Errorf("%s: sprintStartForNew = %q, want %q", c.name, got, c.want)
		}
	}
}
