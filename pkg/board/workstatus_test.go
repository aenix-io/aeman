package board

import (
	"encoding/json"
	"os"
	"testing"
)

// O14 — a task's WORK STATUS, the Roadmap's status filter reads it: done
// when complete, backlog when parked, in progress when in work (a date or a
// sprint). Review, locked and refused are in progress — the task is in
// somebody's hands. A task nobody has placed yet (neither in work nor
// parked, nor done) is waiting like the backlog and reads as backlog.
func TestATasksWorkStatus(t *testing.T) {
	t.Parallel()
	const day = "2026-11-02"
	for _, tc := range []struct {
		name string
		card Card
		want string
	}{
		{"finished at 100", Card{Progress: 100, StartDate: day}, WorkStatusDone},
		{"done stage", Card{Stage: StageDone}, WorkStatusDone},
		{"finished while parked is done", Card{Parked: true, Progress: 100}, WorkStatusDone},
		{"parked", Card{Parked: true}, WorkStatusBacklog},
		{"dated", Card{StartDate: day, Day: day}, WorkStatusInProgress},
		{"in a sprint", Card{SprintStart: day}, WorkStatusInProgress},
		{"on review", Card{SprintStart: day, Stage: StageReview, Progress: 50}, WorkStatusInProgress},
		{"locked", Card{SprintStart: day, Stage: StageLocked, Progress: 50}, WorkStatusInProgress},
		{"refused", Card{SprintStart: day, Stage: StageRefuse}, WorkStatusInProgress},
		{"unplaced", Card{}, WorkStatusBacklog},
	} {
		if got := WorkStatus(tc.card); got != tc.want {
			t.Errorf("%s: WorkStatus = %q, want %q", tc.name, got, tc.want)
		}
	}
	for _, s := range []string{"in-progress", "backlog", "done"} {
		if !ValidWorkStatus(s) {
			t.Errorf("%q is a work status", s)
		}
	}
	for _, s := range []string{"", "todo", "Done", "review"} {
		if ValidWorkStatus(s) {
			t.Errorf("%q is not a work status", s)
		}
	}
}

// The same table, as data the TS mirror reads too (web/src/workstatus.test.ts):
// the filter follows PLACEMENT — parked or placed nowhere is backlog whatever
// the stage, and only board.Complete makes done (locked or on review at 100
// is still in progress; recurrent at 100 is done).
func TestTheWorkStatusMatchesTheSharedFixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/work_status.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Cases []struct {
			Name string `json:"name"`
			Card struct {
				Stage    string `json:"stage"`
				Progress int    `json:"progress"`
				Parked   bool   `json:"parked"`
				Start    string `json:"start"`
				Day      string `json:"day"`
				Sprint   string `json:"sprint"`
			} `json:"card"`
			Status string `json:"status"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatal(err)
	}
	if len(fx.Cases) == 0 {
		t.Fatal("the fixture has no cases")
	}
	for _, tc := range fx.Cases {
		c := Card{Stage: StageKey(tc.Card.Stage), Progress: tc.Card.Progress, Parked: tc.Card.Parked,
			StartDate: tc.Card.Start, Day: tc.Card.Day, SprintStart: tc.Card.Sprint}
		if got := WorkStatus(c); got != tc.Status {
			t.Errorf("%s: WorkStatus = %q, want %q", tc.Name, got, tc.Status)
		}
	}
}
