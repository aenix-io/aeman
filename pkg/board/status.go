package board

// ClampProgress clamps a raw progress value for a card in the given stage. A
// review or locked card is held within the 10–90% band (never 0% or 100%); any
// other stage, including StageNone, keeps the value unchanged. It mirrors the
// slider clamp shared by handleProgress and the Card component.
func ClampProgress(stage StageKey, value int) int {
	if stage == StageReview || stage == StageLocked {
		if value < 10 {
			return 10
		}
		if value > 90 {
			return 90
		}
	}
	return value
}

// Complete reports whether a card counts as finished — an explicit done stage,
// or 100% readiness with no stage (done is derived) or on the recurrent stage
// (a finished recurrent card stays behind; Carry Over reseeds a fresh copy). A
// 100% card that is on review or locked is still unfinished, so it is NOT
// complete. Carry Over and Carry Week use this so finished cards are not
// dragged forward.
// Workable reports whether a card can be picked up and worked on right now: it
// is neither finished nor parked awaiting someone else. It excludes done
// (complete) cards, cards on review (waiting on a reviewer) and locked
// (blocked) cards, keeping in-progress, not-yet-started and recurrent ones.
func Workable(c Card) bool {
	if Complete(c.Stage, c.Progress) {
		return false
	}
	return c.Stage != StageLocked && c.Stage != StageReview
}

func Complete(stage StageKey, progress int) bool {
	if stage == StageDone {
		return true
	}
	return progress >= 100 && (stage == StageNone || stage == StageRecurrent)
}

// ApplyProgress computes the (stage, progress) resulting from setting a card's
// progress to raw. The value is first clamped for the current stage. Done is
// derived (no stage + 100%, see Complete), never stored: reaching 100% with no
// stage simply stays stage-less, and a legacy stored done clears itself when
// progress drops below full. review/locked stages are left as-is. It mirrors
// handleProgress.
func ApplyProgress(stage StageKey, raw int) (StageKey, int) {
	value := ClampProgress(stage, raw)
	if value < 100 && stage == StageDone {
		return StageNone, value
	}
	return stage, value
}

// ApplyStage computes the (stage, progress) resulting from moving a card to the
// given stage. Done is derived, never stored: choosing it clears the stage and
// fills progress to 100% (Complete then reports the card finished). Choosing
// review or locked knocks a full (100%) card down to 90%, since those stages can
// never sit at full. Clearing the stage (StageNone) or any other case keeps
// progress. It mirrors handleStage.
func ApplyStage(stage StageKey, currentProgress int) (StageKey, int) {
	if stage == StageDone {
		return StageNone, 100
	}
	// Entering review/locked stores the 10-90 clamp on both edges, so the
	// stored value matches what the band displays (a 0% card becomes 10%, a
	// full card drops to 90%) and survives a later switch to an unclamped stage.
	return stage, ClampProgress(stage, currentProgress)
}

// ApplyInProgress computes the (stage, progress) for moving a card to the
// implicit In Progress status: the stage is cleared and progress is nudged into
// the [10, 90] band only at its edges — under 10 becomes 10, a done or full
// (>=100) card drops to 90, otherwise the value is kept as-is (so a 91–99 value
// is left untouched, matching the frontend). It mirrors handleInProgress.
func ApplyInProgress(stage StageKey, progress int) (StageKey, int) {
	value := progress
	switch {
	case progress < 10:
		value = 10
	case stage == StageDone || progress >= 100:
		value = 90
	}
	return StageNone, value
}
