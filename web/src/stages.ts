import type { ProjectField, StageKey } from "./providers/types";

export type { StageKey };

export interface StageDef {
  key: StageKey;
  label: string;
  /** Colour the progress bar takes when the card is in this stage. */
  color: string;
}

export const STAGES: Record<StageKey, StageDef> = {
  locked: { key: "locked", label: "Locked", color: "#cf222e" },
  review: { key: "review", label: "Review", color: "#d4a72c" },
  done: { key: "done", label: "Done", color: "#1f883d" },
};

export const STAGE_ORDER: StageKey[] = ["locked", "review", "done"];

/** Progress bar colour for a stage; the default (no stage) bar is green. */
export const DEFAULT_BAR_COLOR = "#3fb950";

/** isInProgress reports the implicit "In Progress" status: a card with no stored
 *  stage whose progress sits in [10, 90] inclusive. It is deliberately NOT a
 *  StageKey — there is no stored option for it. */
export function isInProgress(card: {
  stage?: StageKey;
  progress?: number;
}): boolean {
  const p = card.progress ?? 0;
  return !card.stage && p >= 10 && p <= 90;
}

/** stageFromName maps a Stage single-select option name onto a StageKey. */
export function stageFromName(name?: string): StageKey | undefined {
  switch (name?.trim().toLowerCase()) {
    case "locked":
      return "locked";
    case "review":
      return "review";
    case "done":
      return "done";
    default:
      return undefined;
  }
}

/** optionIdForStage finds the option id in the Stage field for a stage. */
export function optionIdForStage(
  field: ProjectField | undefined,
  stage: StageKey,
): string | undefined {
  return field?.options?.find((o) => stageFromName(o.name) === stage)?.id;
}
