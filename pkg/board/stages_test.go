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
	want := []StageKey{StageLocked, StageReview, StageRecurrent, StageRefuse, StageDone}
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

// REFUSE is the answer a person gives on their own board: "not me, not
// this". It is not a delete and not a hiding place — the card goes back to
// the team's grid for the lead to answer — so it must behave like the other
// parked stages and like none of the finished ones.
func TestRefusedIsParkedWork(t *testing.T) {
	refused := Card{Stage: StageRefuse, Progress: 40}
	if Workable(refused) {
		t.Fatal("a refused card is not work to pick up: it waits on the lead")
	}
	// Not finished, at any progress: refusing is not doing.
	if Complete(StageRefuse, 100) || Complete(StageRefuse, 0) {
		t.Fatal("a refused card is not complete")
	}
	// It holds the 10–90 band, like the other two parked stages: work waiting
	// on somebody else's answer is neither untouched nor finished, and a bar
	// at either end would read as one of those two.
	if got := ClampProgress(StageRefuse, 0); got != 10 {
		t.Fatalf("ClampProgress(refuse, 0) = %d, want 10", got)
	}
	if got := ClampProgress(StageRefuse, 100); got != 90 {
		t.Fatalf("ClampProgress(refuse, 100) = %d, want 90", got)
	}
	if got := ClampProgress(StageRefuse, 40); got != 40 {
		t.Fatalf("ClampProgress(refuse, 40) = %d, want it left alone", got)
	}
	// And entering the stage STORES the clamp, so the value survives a later
	// switch to an unclamped one (S7).
	if stage, p := ApplyStage(StageRefuse, 0); stage != StageRefuse || p != 10 {
		t.Fatalf("ApplyStage(refuse, 0) = %q/%d, want refuse/10", stage, p)
	}
	// And it is a stage like any other on the way in.
	if StageFromName("Refuse") != StageRefuse {
		t.Fatal("the stage is read back by name")
	}
	if def := Stages[StageRefuse]; def.Label != "Refuse" || def.Color == "" {
		t.Fatalf("the stage needs a label and a colour: %+v", def)
	}
}
