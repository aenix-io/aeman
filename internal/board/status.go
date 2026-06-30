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

// ApplyProgress computes the (stage, progress) resulting from setting a card's
// progress to raw. The value is first clamped for the current stage, then the
// done auto-link runs: reaching 100% with no stage moves the card to done, and
// dropping below 100% clears a done stage. review/locked stages are left as-is.
// It mirrors handleProgress (the done-link assumes the board has a Stage field,
// the `if (roles.stage)` guard there; aeman boards always do).
func ApplyProgress(stage StageKey, raw int) (StageKey, int) {
	value := ClampProgress(stage, raw)
	switch {
	case value == 100 && stage == StageNone:
		return StageDone, value
	case value < 100 && stage == StageDone:
		return StageNone, value
	default:
		return stage, value
	}
}

// ApplyStage computes the (stage, progress) resulting from moving a card to the
// given stage. Choosing done fills progress to 100%; choosing review or locked
// knocks a full (100%) card down to 90%, since those stages can never sit at
// full. Clearing the stage (StageNone) or any other case keeps progress. It
// mirrors handleStage.
func ApplyStage(stage StageKey, currentProgress int) (StageKey, int) {
	progress := currentProgress
	switch {
	case stage == StageDone:
		progress = 100
	case (stage == StageReview || stage == StageLocked) && currentProgress == 100:
		progress = 90
	}
	return stage, progress
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
