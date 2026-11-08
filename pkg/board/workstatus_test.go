package board

import (
	"encoding/json"
	"os"
	"slices"
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
	for _, s := range []string{"", "todo", "Done", "review", "locked", "no-items"} {
		if ValidWorkStatus(s) {
			t.Errorf("%q is not a work status", s)
		}
	}
}

// O14 — the status FILTER reads more than the work status: locked (the
// derived lock, N3) and no-items (a plan item with nothing under it). Every
// work status is one of its values; so are those two; nothing else is.
func TestTheStatusFilterValues(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"in-progress", "backlog", "done", "locked", "no-items"} {
		if !ValidStatusFilter(s) {
			t.Errorf("%q is a status filter value", s)
		}
	}
	for _, s := range []string{"", "todo", "Done", "review", "refused", "no items"} {
		if ValidStatusFilter(s) {
			t.Errorf("%q is not a status filter value", s)
		}
	}
	if !slices.Equal(StatusFilters, []string{"in-progress", "backlog", "locked", "no-items", "done"}) {
		t.Errorf("StatusFilters = %v", StatusFilters)
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
				Locked   bool   `json:"locked"`
			} `json:"card"`
			Status  string   `json:"status"`
			Matches []string `json:"matches"`
		} `json:"cases"`
		Plans []struct {
			Name      string   `json:"name"`
			Hierarchy string   `json:"hierarchy"`
			Locked    bool     `json:"locked"`
			Matches   []string `json:"matches"`
		} `json:"plans"`
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
		want := tc.Matches
		if want == nil {
			want = []string{tc.Status}
		}
		got := TaskStatuses(c, tc.Card.Locked)
		if !slices.Equal(sorted(got), sorted(want)) {
			t.Errorf("%s: TaskStatuses = %v, want %v", tc.Name, got, want)
		}
		// Hide done — every value but done — keeps exactly the open work.
		if hidesDone(got) != (tc.Status == WorkStatusDone) {
			t.Errorf("%s: Hide done keeps %v, status %q", tc.Name, got, tc.Status)
		}
	}
	if len(fx.Plans) == 0 {
		t.Fatal("the fixture has no plan cases")
	}
	for _, tc := range fx.Plans {
		got := PlanStatuses(tc.Hierarchy, tc.Locked)
		if !slices.Equal(sorted(got), sorted(tc.Matches)) {
			t.Errorf("plan %s: PlanStatuses = %v, want %v", tc.Name, got, tc.Matches)
		}
		// A finished plan item never answers Hide done by itself; one with
		// nothing under it always does.
		if tc.Hierarchy == HierarchyDone && !hidesDone(got) {
			t.Errorf("plan %s: a finished plan item answers Hide done", tc.Name)
		}
		if tc.Hierarchy == HierarchyNoItems && hidesDone(got) {
			t.Errorf("plan %s: a plan item with nothing under it is hidden by Hide done", tc.Name)
		}
	}
}

func sorted(v []string) []string {
	out := slices.Clone(v)
	slices.Sort(out)
	return out
}

// hidesDone reports whether Hide done (every status filter value but done)
// leaves out a card answering these values.
func hidesDone(matches []string) bool {
	for _, m := range matches {
		if m != WorkStatusDone {
			return false
		}
	}
	return true
}
