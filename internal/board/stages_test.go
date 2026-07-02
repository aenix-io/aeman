package board

import "testing"

func TestIsInProgress(t *testing.T) {
	cases := []struct {
		name string
		card Card
		want bool
	}{
		{"no stage, zero progress", Card{Stage: StageNone, Progress: 0}, false},
		{"no stage, at lower bound", Card{Stage: StageNone, Progress: 10}, true},
		{"no stage, mid", Card{Stage: StageNone, Progress: 50}, true},
		{"no stage, at upper bound", Card{Stage: StageNone, Progress: 90}, true},
		{"no stage, just under", Card{Stage: StageNone, Progress: 9}, false},
		{"no stage, just over", Card{Stage: StageNone, Progress: 91}, false},
		{"no stage, full", Card{Stage: StageNone, Progress: 100}, false},
		{"review stage in band", Card{Stage: StageReview, Progress: 50}, false},
		{"locked stage in band", Card{Stage: StageLocked, Progress: 50}, false},
		{"done stage in band", Card{Stage: StageDone, Progress: 50}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsInProgress(c.card); got != c.want {
				t.Errorf("IsInProgress(%+v) = %v, want %v", c.card, got, c.want)
			}
		})
	}
}

func TestStageFromName(t *testing.T) {
	cases := []struct {
		in   string
		want StageKey
	}{
		{"Locked", StageLocked},
		{"  review ", StageReview},
		{"DONE", StageDone},
		{"unknown", StageNone},
		{"", StageNone},
	}
	for _, c := range cases {
		if got := StageFromName(c.in); got != c.want {
			t.Errorf("StageFromName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStageOrderAndDefs(t *testing.T) {
	want := []StageKey{StageLocked, StageReview, StageRecurrent, StageDone}
	if len(StageOrder) != len(want) {
		t.Fatalf("StageOrder = %v, want %v", StageOrder, want)
	}
	for i, s := range want {
		if StageOrder[i] != s {
			t.Errorf("StageOrder[%d] = %q, want %q", i, StageOrder[i], s)
		}
		if def, ok := Stages[s]; !ok || def.Key != s || def.Label == "" {
			t.Errorf("Stages[%q] = %+v, want a labelled def", s, def)
		}
	}
}
