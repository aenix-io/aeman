package board

import "strings"

// StageKey is an explicit per-card status that recolours the progress bar. It
// mirrors the StageKey union in web/src/providers/types.ts and the stage model in
// web/src/stages.ts. StageNone ("") means the card carries no stored stage.
type StageKey string

// The explicit stages a card can carry, mirroring web/src/stages.ts. StageNone
// ("") is the absence of a stage (the implicit In Progress / not-started state).
const (
	StageNone   StageKey = ""
	StageLocked StageKey = "locked"
	StageReview StageKey = "review"
	StageDone   StageKey = "done"
)

// StageOrder is the canonical stage ordering, mirroring STAGE_ORDER.
var StageOrder = []StageKey{StageLocked, StageReview, StageDone}

// DefaultBarColor is the progress-bar colour for a card with no stage, mirroring
// DEFAULT_BAR_COLOR.
const DefaultBarColor = "#3fb950"

// StageDef describes a stage's label and progress-bar colour, mirroring StageDef.
type StageDef struct {
	Key   StageKey `json:"key"`
	Label string   `json:"label"`
	Color string   `json:"color"`
}

// Stages maps each stage to its label and bar colour, mirroring STAGES.
var Stages = map[StageKey]StageDef{
	StageLocked: {Key: StageLocked, Label: "Locked", Color: "#cf222e"},
	StageReview: {Key: StageReview, Label: "Review", Color: "#d4a72c"},
	StageDone:   {Key: StageDone, Label: "Done", Color: "#1f883d"},
}

// StageFromName maps a Stage single-select option name onto a StageKey, mirroring
// stageFromName. An unknown (or empty) name yields StageNone.
func StageFromName(name string) StageKey {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "locked":
		return StageLocked
	case "review":
		return StageReview
	case "done":
		return StageDone
	default:
		return StageNone
	}
}

// IsInProgress reports the implicit "In Progress" status: a card with no stored
// stage whose progress sits in [10, 90] inclusive. It mirrors isInProgress in
// web/src/stages.ts and is deliberately not a StageKey (there is no stored option
// for it).
func IsInProgress(c Card) bool {
	return c.Stage == StageNone && c.Progress >= 10 && c.Progress <= 90
}
