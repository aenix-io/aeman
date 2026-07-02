package board

import "testing"

func TestClampProgress(t *testing.T) {
	cases := []struct {
		name  string
		stage StageKey
		value int
		want  int
	}{
		{"review under band", StageReview, 5, 10},
		{"review over band", StageReview, 95, 90},
		{"review in band", StageReview, 50, 50},
		{"review at lower edge", StageReview, 10, 10},
		{"review at upper edge", StageReview, 90, 90},
		{"locked under band", StageLocked, 0, 10},
		{"locked over band", StageLocked, 100, 90},
		{"none passes through low", StageNone, 0, 0},
		{"none passes through full", StageNone, 100, 100},
		{"done passes through full", StageDone, 100, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClampProgress(c.stage, c.value); got != c.want {
				t.Errorf("ClampProgress(%q,%d) = %d, want %d", c.stage, c.value, got, c.want)
			}
		})
	}
}

func TestApplyProgress(t *testing.T) {
	cases := []struct {
		name      string
		stage     StageKey
		raw       int
		wantStage StageKey
		wantValue int
	}{
		{"full with no stage stays stage-less (done is derived)", StageNone, 100, StageNone, 100},
		{"below full clears done", StageDone, 50, StageNone, 50},
		{"mid with no stage unchanged", StageNone, 50, StageNone, 50},
		{"review clamps full to 90, no link", StageReview, 100, StageReview, 90},
		{"locked clamps under to 10", StageLocked, 5, StageLocked, 10},
		{"done at full stays done", StageDone, 100, StageDone, 100},
		{"review mid unchanged", StageReview, 50, StageReview, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotStage, gotValue := ApplyProgress(c.stage, c.raw)
			if gotStage != c.wantStage || gotValue != c.wantValue {
				t.Errorf("ApplyProgress(%q,%d) = (%q,%d), want (%q,%d)",
					c.stage, c.raw, gotStage, gotValue, c.wantStage, c.wantValue)
			}
		})
	}
}

func TestApplyStage(t *testing.T) {
	cases := []struct {
		name      string
		stage     StageKey
		progress  int
		wantStage StageKey
		wantValue int
	}{
		{"done clears the stage and fills to full (derived)", StageDone, 50, StageNone, 100},
		{"review drops full to 90", StageReview, 100, StageReview, 90},
		{"locked drops full to 90", StageLocked, 100, StageLocked, 90},
		{"review below full kept", StageReview, 50, StageReview, 50},
		{"locked below full kept", StageLocked, 50, StageLocked, 50},
		{"review lifts an empty card to 10", StageReview, 0, StageReview, 10},
		{"locked lifts an empty card to 10", StageLocked, 0, StageLocked, 10},
		{"recurrent keeps progress unclamped", StageRecurrent, 0, StageRecurrent, 0},
		{"clearing keeps progress", StageNone, 100, StageNone, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotStage, gotValue := ApplyStage(c.stage, c.progress)
			if gotStage != c.wantStage || gotValue != c.wantValue {
				t.Errorf("ApplyStage(%q,%d) = (%q,%d), want (%q,%d)",
					c.stage, c.progress, gotStage, gotValue, c.wantStage, c.wantValue)
			}
		})
	}
}

func TestApplyInProgress(t *testing.T) {
	cases := []struct {
		name      string
		stage     StageKey
		progress  int
		wantValue int
	}{
		{"under band raised to 10", StageNone, 5, 10},
		{"zero raised to 10", StageNone, 0, 10},
		{"done dropped to 90", StageDone, 100, 90},
		{"full review dropped to 90", StageReview, 100, 90},
		{"mid kept", StageNone, 50, 50},
		{"review mid kept, stage cleared", StageReview, 50, 50},
		{"91-99 left untouched (quirk)", StageNone, 95, 95},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotStage, gotValue := ApplyInProgress(c.stage, c.progress)
			if gotStage != StageNone || gotValue != c.wantValue {
				t.Errorf("ApplyInProgress(%q,%d) = (%q,%d), want (%q,%d)",
					c.stage, c.progress, gotStage, gotValue, StageNone, c.wantValue)
			}
		})
	}
}

func TestComplete(t *testing.T) {
	cases := []struct {
		name     string
		stage    StageKey
		progress int
		want     bool
	}{
		{"explicit done", StageDone, 100, true},
		{"100% with no stage is the done auto-link", StageNone, 100, true},
		{"100% on review is still unfinished", StageReview, 100, false},
		{"100% locked is still unfinished", StageLocked, 100, false},
		{"in progress", StageNone, 40, false},
		{"review below full", StageReview, 90, false},
	}
	for _, c := range cases {
		if got := Complete(c.stage, c.progress); got != c.want {
			t.Errorf("%s: Complete(%q,%d) = %v, want %v", c.name, c.stage, c.progress, got, c.want)
		}
	}
}
